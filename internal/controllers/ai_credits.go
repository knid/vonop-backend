package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/uptrace/bun"
)

// CreditBreakdown is the derived subscription-vs-purchased split of a user's
// credit pool, computed by replaying the ledger. The backend stores a single
// undifferentiated Balance column — this struct is the logical projection the
// UI (and the period reset) wants.
//
//   - SubBalance: subscription credits remaining this period (bounded by the
//     rollover cap); drained first when the user consumes credits.
//   - PurchasedBalance: top-up credits remaining; carry over across periods
//     and drain only after SubBalance is exhausted.
//   - SubUsedThisPeriod: how many credits have been charged against the
//     subscription bucket since PeriodStart. Used for the "X of Y used"
//     progress bar.
//   - SubLimit: nominal monthly allowance from the user's plan.
//   - Balance: ground truth from ai_credits_balances — SubBalance +
//     PurchasedBalance is reconciled against this before returning.
type CreditBreakdown struct {
	Balance           int
	SubBalance        int
	PurchasedBalance  int
	SubLimit          int
	SubUsedThisPeriod int
	PeriodStart       time.Time
	PeriodEnd         time.Time
}

// classifyLedgerAction mirrors views.categorizeTxType so both the display
// endpoint and the replay agree on how to bucket a transaction. Returns one
// of "allocation", "purchase", "refund", "usage".
func classifyLedgerAction(action string, amount int) string {
	lower := strings.ToLower(action)
	if strings.Contains(lower, "refund") {
		return "refund"
	}
	if strings.Contains(lower, "grant") || strings.Contains(lower, "allocation") || strings.Contains(lower, "bonus") {
		return "allocation"
	}
	if strings.Contains(lower, "purchase") || strings.Contains(lower, "purchased") {
		return "purchase"
	}
	if amount < 0 {
		return "usage"
	}
	if amount > 0 {
		return "allocation"
	}
	return "usage"
}

// ComputeCreditBreakdown replays the entire ledger in chronological order to
// derive the subscription/purchased split using a "drain subscription first"
// policy, then reconciles the result against the ground-truth balance.
//
// Why replay instead of two separate DB columns: avoids a migration, keeps
// the single-column debit path (AtomicDeductCreditsToFloor) unchanged, and
// works retroactively on existing ledgers. The replay is O(ledger size) but
// per-user ledgers stay small.
//
// Reconciliation handles both directions of drift:
//   - Replay > balance: historical period resets (see GetOrCreateCreditsBalance)
//     destroyed credits without logging a ledger row. Clamp purchased down
//     first, then sub — prefer showing "lost" credits as lost purchased
//     credits since that's the bucket the old reset bug actually hurt.
//   - Replay < balance: unlogged adjustments (rare). Assume they were sub.
func (c *Controller) ComputeCreditBreakdown(user *models.User) (*CreditBreakdown, error) {
	balance, err := c.GetAICreditsBalanceNoReset(user)
	if err != nil {
		return nil, err
	}
	limits, err := c.GetUserLimits(user)
	if err != nil {
		return nil, err
	}
	txs, err := c.DB.GetAllAICreditTransactionsAsc(user.ID.String())
	if err != nil {
		return nil, err
	}

	var sub, purchased, subUsedInPeriod int
	for _, tx := range txs {
		inPeriod := !tx.CreatedAt.Before(balance.PeriodStart)
		switch classifyLedgerAction(tx.Action, tx.Amount) {
		case "allocation":
			sub += tx.Amount
		case "purchase":
			purchased += tx.Amount
		case "refund":
			// Refunds go back to the purchased bucket: refunds are almost
			// always for purchase flows, and the original sub credits may
			// already have been reset away.
			purchased += tx.Amount
		case "usage":
			need := -tx.Amount
			if need <= 0 {
				continue
			}
			drainedFromSub := need
			if drainedFromSub > sub {
				drainedFromSub = sub
			}
			sub -= drainedFromSub
			purchased -= (need - drainedFromSub)
			if inPeriod {
				subUsedInPeriod += drainedFromSub
			}
		}
	}
	if sub < 0 {
		sub = 0
	}
	if purchased < 0 {
		purchased = 0
	}

	total := sub + purchased
	if total > balance.Balance {
		excess := total - balance.Balance
		if purchased >= excess {
			purchased -= excess
		} else {
			remaining := excess - purchased
			purchased = 0
			if sub >= remaining {
				sub -= remaining
			} else {
				sub = 0
			}
		}
	} else if total < balance.Balance {
		sub += balance.Balance - total
	}

	return &CreditBreakdown{
		Balance:           balance.Balance,
		SubBalance:        sub,
		PurchasedBalance:  purchased,
		SubLimit:          limits.MonthlyAICredits,
		SubUsedThisPeriod: subUsedInPeriod,
		PeriodStart:       balance.PeriodStart,
		PeriodEnd:         balance.PeriodEnd,
	}, nil
}

// GetAICreditsBalanceNoReset fetches the current balance row without running
// the period-reset branch. ComputeCreditBreakdown needs this because the
// reset logic itself calls ComputeCreditBreakdown — going through
// GetOrCreateCreditsBalance would recurse. New-user creation still goes
// through the full path.
func (c *Controller) GetAICreditsBalanceNoReset(user *models.User) (*models.AICreditsBalance, error) {
	balance, err := c.DB.GetAICreditsBalance(user.ID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.GetOrCreateCreditsBalance(user)
		}
		return nil, err
	}
	return balance, nil
}

// GetOrCreateCreditsBalance returns the user's credit balance, creating one if needed.
// Period resets use atomic SQL to prevent duplicate monthly_grant transactions.
func (c *Controller) GetOrCreateCreditsBalance(user *models.User) (*models.AICreditsBalance, error) {
	balance, err := c.DB.GetAICreditsBalance(user.ID.String())
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		// Create a new balance aligned with the subscription billing period
		periodStart, periodEnd := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())
		balance = &models.AICreditsBalance{
			UserID:      user.ID,
			Balance:     0,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
		}

		// Grant monthly credits from plan only for verified users. Unverified
		// email/password signups receive 0 credits until they click their
		// verification link; see GrantInitialFreeCreditsIfNeeded.
		initialGrant := 0
		if user.Verified {
			limits, limitsErr := c.GetUserLimits(user)
			if limitsErr == nil && limits.MonthlyAICredits > 0 {
				balance.Balance = limits.MonthlyAICredits
				initialGrant = limits.MonthlyAICredits
			}
		}

		balance, err = c.DB.CreateAICreditsBalance(balance)
		if err != nil {
			return nil, err
		}

		// Audit the initial credit grant (only if we created the balance recently)
		if initialGrant > 0 && time.Since(balance.CreatedAt) < time.Second {
			if _, txErr := c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
				UserID:    user.ID,
				Amount:    initialGrant,
				Action:    "initial_grant",
				Reference: balance.ID.String(),
			}); txErr != nil {
				slog.Error("failed to log initial credit grant", "error", txErr, "user_id", user.ID)
			}
		}
	}

	// Check if period has expired and reset atomically
	if time.Now().After(balance.PeriodEnd) {
		periodStart, periodEnd := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())

		monthlyCredits := 0
		rolloverMonths := 0
		limits, limitsErr := c.GetUserLimits(user)
		if limitsErr == nil {
			monthlyCredits = limits.MonthlyAICredits
			rolloverMonths = limits.CreditRolloverMonths
		}

		// Free plan (monthlyCredits == 0): skip reset entirely, just extend the period
		// This preserves one-time credits for free plan users
		if monthlyCredits == 0 {
			periodEnd = time.Now().AddDate(100, 0, 0) // extend far into the future
			_, didReset, resetErr := c.DB.AtomicResetPeriod(balance.ID.String(), balance.Balance, periodStart, periodEnd)
			if resetErr != nil {
				return nil, resetErr
			}
			if didReset {
				balance.PeriodStart = periodStart
				balance.PeriodEnd = periodEnd
			} else {
				balance, err = c.DB.GetAICreditsBalance(user.ID.String())
				if err != nil {
					return nil, err
				}
			}
			return balance, nil
		}

		// Calculate new balance with rollover.
		//
		// Previous behavior (buggy): the rollover cap was applied to the total
		// balance, silently destroying purchased top-up credits. For a Starter
		// user on 200/mo with rollover=1, a 1000-credit top-up would be
		// clamped to 400 at the next reset — losing 600 real credits the user
		// paid for.
		//
		// Fix: split the pre-reset balance into sub + purchased via ledger
		// replay, apply the cap ONLY to the sub portion, then add purchased
		// back untouched. Purchased credits never roll over into the cap
		// because they aren't part of the monthly allowance.
		breakdown, bdErr := c.ComputeCreditBreakdown(user)
		purchasedCarry := 0
		subCarry := 0
		if bdErr == nil && breakdown != nil {
			purchasedCarry = breakdown.PurchasedBalance
			subCarry = breakdown.SubBalance
		}

		var newBalance int
		if balance.Balance < 0 {
			// Overdraft always carries forward regardless of rollover setting.
			// Apply it to the sub bucket — purchased carry (if any from
			// reconciliation) rides along.
			newBalance = monthlyCredits + balance.Balance
			if newBalance < 0 {
				newBalance = 0
			}
			newBalance += purchasedCarry
		} else if rolloverMonths > 0 {
			subCap := monthlyCredits * (rolloverMonths + 1)
			newSub := subCarry + monthlyCredits
			if newSub > subCap {
				newSub = subCap
			}
			newBalance = newSub + purchasedCarry
		} else {
			// No rollover: sub resets flat to this month's grant, purchased
			// carries over untouched.
			newBalance = monthlyCredits + purchasedCarry
		}

		newBal, didReset, resetErr := c.DB.AtomicResetPeriod(balance.ID.String(), newBalance, periodStart, periodEnd)
		if resetErr != nil {
			return nil, resetErr
		}

		if didReset {
			balance.Balance = newBal
			balance.PeriodStart = periodStart
			balance.PeriodEnd = periodEnd

			// Log the monthly grant (only the winner of the race creates this)
			if _, err := c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
				UserID:    user.ID,
				Amount:    monthlyCredits,
				Action:    "monthly_grant",
				Reference: balance.ID.String(),
			}); err != nil {
				slog.Error("failed to log monthly_grant transaction", "error", err, "user_id", user.ID)
			}
		} else {
			// Another request already reset the period — re-read the current balance
			balance, err = c.DB.GetAICreditsBalance(user.ID.String())
			if err != nil {
				return nil, err
			}
		}
	}

	return balance, nil
}

// GrantInitialFreeCreditsIfNeeded grants the plan's monthly credits to a
// verified user who has never received them. Idempotent: subsequent calls
// are no-ops once an initial_grant transaction exists. Called from the email
// verification flow so unverified signups cannot collect free credits.
func (c *Controller) GrantInitialFreeCreditsIfNeeded(user *models.User) error {
	if !user.Verified {
		return nil
	}

	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return fmt.Errorf("failed to get credits balance: %w", err)
	}

	hasGrant, err := c.DB.HasInitialGrant(user.ID.String())
	if err != nil {
		return fmt.Errorf("failed to check initial grant: %w", err)
	}
	if hasGrant {
		return nil
	}

	limits, err := c.GetUserLimits(user)
	if err != nil || limits.MonthlyAICredits <= 0 {
		return nil
	}

	return c.DB.RunInTx(func(tx bun.Tx) error {
		if _, err := c.DB.AtomicAddCredits(tx, balance.ID.String(), limits.MonthlyAICredits); err != nil {
			return fmt.Errorf("failed to add initial credits: %w", err)
		}
		if _, err := c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    limits.MonthlyAICredits,
			Action:    "initial_grant",
			Reference: balance.ID.String(),
		}); err != nil {
			return fmt.Errorf("failed to log initial grant: %w", err)
		}
		return nil
	})
}

// TokensToCredits converts token counts to credits using remote-config values
// for tokens-per-credit and output multiplier. Falls back to hardcoded defaults
// if remote config is unavailable.
func (c *Controller) TokensToCredits(promptTokens, completionTokens int) int {
	tpc := c.RemoteConfig.GetInt(remoteconfig.AICreditsTokensPerCredit, remoteconfig.Global)
	om := c.RemoteConfig.GetInt(remoteconfig.AICreditsOutputMultiplier, remoteconfig.Global)
	return models.TokensToCreditsWithConfig(promptTokens, completionTokens, tpc, om)
}

// CreditBudget returns the maximum credits the user can spend in a single session.
// Budget = current balance - floor (floor is negative, so this is balance + |floor|).
// For example: balance=1, floor=-20 → budget=21.
func (c *Controller) CreditBudget(user *models.User) (int, error) {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return 0, err
	}
	floor := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
	budget := balance.Balance - floor
	if budget < 0 {
		budget = 0
	}
	return budget, nil
}

// MakeCreditBudgetChecker returns a ShouldContinue-compatible function for
// StreamWithTools. It checks whether the accumulated credit cost in the tracker
// has reached the user's budget. When exhausted, it logs a warning and returns false.
func (c *Controller) MakeCreditBudgetChecker(user *models.User, tracker *utils.TokenTracker) func() bool {
	budget, err := c.CreditBudget(user)
	if err != nil {
		return nil
	}
	return func() bool {
		prompt, completion, _, _ := tracker.Totals()
		spent := c.TokensToCredits(prompt, completion)
		if spent >= budget {
			slog.Warn("credit budget exhausted mid-generation",
				"user_id", user.ID, "spent", spent, "budget", budget)
			return false
		}
		return true
	}
}

// DeductAICreditsUnchecked deducts credits without checking sufficiency (balance may go negative
// down to AICreditsMinBalance). Used for post-hoc token-based billing where the pre-check already ran.
func (c *Controller) DeductAICreditsUnchecked(user *models.User, cost int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}
	prevBalance := balance.Balance

	var newBalance int
	txErr := c.DB.RunInTx(func(tx bun.Tx) error {
		var err error
		minBalance := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
		newBalance, err = c.DB.AtomicDeductCreditsUnchecked(tx, balance.ID.String(), cost, minBalance)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("AI credits overdraft limit reached (floor: %d)", minBalance)
			}
			return err
		}

		_, err = c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    -cost,
			Action:    action,
			Reference: reference,
		})
		return err
	})

	if txErr == nil {
		c.TrackUserEvent(user, "ai_credits_deducted", posthog.NewProperties().
			Set("action", action).
			Set("cost", cost).
			Set("remaining_balance", newBalance).
			Set("reference", reference))

		c.emitCreditThresholdEvents(user, prevBalance, newBalance, action, reference)
	}

	return txErr
}

// DeductAICreditsToFloor deducts credits, clamping at the overdraft floor instead
// of failing. Always deducts something if balance is above floor. Used as the
// primary deduction method for generation sessions where the cost may exceed budget.
func (c *Controller) DeductAICreditsToFloor(user *models.User, cost int, action string, reference string) error {
	if cost <= 0 {
		return nil
	}
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}
	prevBalance := balance.Balance

	var newBalance, deducted int
	txErr := c.DB.RunInTx(func(tx bun.Tx) error {
		floor := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
		var err error
		newBalance, deducted, err = c.DB.AtomicDeductCreditsToFloor(tx, balance.ID.String(), cost, floor)
		if err != nil {
			return err
		}
		if deducted == 0 {
			return nil
		}
		_, err = c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    -deducted,
			Action:    action,
			Reference: reference,
		})
		return err
	})

	if txErr == nil && deducted > 0 {
		c.TrackUserEvent(user, "ai_credits_deducted", posthog.NewProperties().
			Set("action", action).
			Set("cost", cost).
			Set("deducted", deducted).
			Set("remaining_balance", newBalance).
			Set("reference", reference))
		if deducted < cost {
			slog.Warn("partial credit deduction (clamped at floor)",
				"user_id", user.ID, "requested", cost, "deducted", deducted,
				"remaining", newBalance)
		}

		c.emitCreditThresholdEvents(user, prevBalance, newBalance, action, reference)
	}

	return txErr
}

// emitCreditThresholdEvents fires lifecycle events when a deduction crosses a
// percentage-of-monthly-grant threshold (50%, 25%, 10%, 0%) or triggers
// overdraft. Each crossing fires exactly once per deduction — if a large
// deduction skips over multiple thresholds, the lowest crossed threshold is
// used. Designed to feed marketing workflows without spamming CRM on every
// deduct, and to fire immediately instead of waiting for the hourly job.
func (c *Controller) emitCreditThresholdEvents(user *models.User, prevBalance, newBalance int, action, reference string) {
	// Overdraft transition (balance went from >= 0 to < 0)
	if prevBalance >= 0 && newBalance < 0 {
		go c.EventService.SendEvent(user, constants.EVENT_CREDITS_OVERDRAFT_STARTED, map[string]any{
			"previous_balance": prevBalance,
			"new_balance":      newBalance,
			"action":           action,
			"reference":        reference,
		})
	}

	// Percentage thresholds — only meaningful when user has a monthly grant
	limits, err := c.GetUserLimits(user)
	if err != nil || limits == nil || limits.MonthlyAICredits <= 0 {
		return
	}
	monthly := limits.MonthlyAICredits

	// Thresholds in descending order so we report the lowest newly-crossed one.
	type threshold struct {
		percent int
		event   string
		level   string
	}
	thresholds := []threshold{
		{0, constants.EVENT_AI_CREDITS_DEPLETED, "depleted"},
		{10, constants.EVENT_CREDITS_THRESHOLD_CROSSED, "10pct"},
		{25, constants.EVENT_CREDITS_THRESHOLD_CROSSED, "25pct"},
		{50, constants.EVENT_CREDITS_THRESHOLD_CROSSED, "50pct"},
	}

	for _, t := range thresholds {
		cutoff := (monthly * t.percent) / 100
		if prevBalance > cutoff && newBalance <= cutoff {
			go c.EventService.SendEvent(user, t.event, map[string]any{
				"level":             t.level,
				"threshold_percent": t.percent,
				"credits_remaining": newBalance,
				"credits_total":     monthly,
				"action":            action,
				"reference":         reference,
			})
			return // only fire the lowest newly-crossed threshold
		}
	}
}

// AddAICredits adds credits to a user's balance (for purchases or grants).
// Uses atomic SQL + DB transaction to ensure balance and audit log stay consistent.
func (c *Controller) AddAICredits(user *models.User, amount int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

	return c.DB.RunInTx(func(tx bun.Tx) error {
		_, err := c.DB.AtomicAddCredits(tx, balance.ID.String(), amount)
		if err != nil {
			return err
		}

		_, err = c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    amount,
			Action:    action,
			Reference: reference,
		})
		return err
	})
}

// GrantSubscriptionCredits grants the plan's monthly credits when a user first subscribes.
// If a credits balance already exists (created before subscribing), it resets the period
// to align with the new subscription and adds the plan's monthly credits.
// Safe to call multiple times — uses a transaction with FOR UPDATE to prevent double grants.
func (c *Controller) GrantSubscriptionCredits(user *models.User) {
	limits, err := c.GetUserLimits(user)
	if err != nil || limits.MonthlyAICredits <= 0 {
		return
	}

	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		slog.Error("failed to get/create credits balance for subscription grant", "error", err, "user_id", user.ID)
		return
	}

	err = c.DB.RunInTx(func(tx bun.Tx) error {
		// Lock the balance row to serialize concurrent grant attempts
		var locked models.AICreditsBalance
		if err := tx.NewRaw(
			"SELECT * FROM ai_credits_balances WHERE id = ? FOR UPDATE",
			balance.ID,
		).Scan(context.Background(), &locked); err != nil {
			return fmt.Errorf("lock balance: %w", err)
		}

		// Check for existing subscription_grant inside the transaction
		var count int
		if err := tx.NewRaw(
			"SELECT COUNT(*) FROM ai_credit_transactions WHERE user_id = ? AND action = 'subscription_grant'",
			user.ID,
		).Scan(context.Background(), &count); err != nil {
			return fmt.Errorf("check existing grant: %w", err)
		}
		if count > 0 {
			return nil // already granted
		}

		// Reset period to align with the new subscription start and grant credits
		periodStart, periodEnd := utils.BillingPeriod(user.SubscriptionStartAt, time.Now())
		if _, err := tx.NewRaw(
			"UPDATE ai_credits_balances SET balance = balance + ?, period_start = ?, period_end = ?, updated_at = NOW() WHERE id = ?",
			limits.MonthlyAICredits, periodStart, periodEnd, balance.ID,
		).Exec(context.Background()); err != nil {
			return fmt.Errorf("update balance: %w", err)
		}

		if _, err := c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    limits.MonthlyAICredits,
			Action:    "subscription_grant",
			Reference: user.SubscriptionID.String(),
		}); err != nil {
			return fmt.Errorf("log grant: %w", err)
		}

		return nil
	})
	if err != nil {
		slog.Error("failed to grant subscription credits", "error", err, "user_id", user.ID)
		return
	}

	slog.Info("subscription credits granted", "user_id", user.ID, "credits", limits.MonthlyAICredits)

	go c.EventService.SendEvent(user, constants.EVENT_CREDITS_GRANTED, map[string]any{
		"credits":    limits.MonthlyAICredits,
		"grant_type": "subscription",
		"reference":  user.SubscriptionID.String(),
	})
}

// messagesToPostHogInput converts []utils.Message to the format PostHog expects
// for $ai_input: [{role, content: [{type: "text", text: "..."}]}].
// Truncates to last 50 messages and 10K chars per message.
func messagesToPostHogInput(msgs []utils.Message) []map[string]interface{} {
	const maxMessages = 50
	const maxChars = 10000
	start := 0
	if len(msgs) > maxMessages {
		start = len(msgs) - maxMessages
	}
	out := make([]map[string]interface{}, 0, len(msgs)-start)
	for _, m := range msgs[start:] {
		content := m.Content
		if len(content) > maxChars {
			content = content[:maxChars] + "..."
		}
		out = append(out, map[string]interface{}{
			"role": m.Role,
			"content": []map[string]string{
				{"type": "text", "text": content},
			},
		})
	}
	return out
}

// textToOutputChoices wraps a plain text string into the PostHog $ai_output_choices
// format: [{"role": "assistant", "content": [{"type": "text", "text": "..."}]}].
func textToOutputChoices(text string) []map[string]interface{} {
	const maxChars = 10000
	if len(text) > maxChars {
		text = text[:maxChars] + "..."
	}
	return []map[string]interface{}{
		{
			"role": "assistant",
			"content": []map[string]string{
				{"type": "text", "text": text},
			},
		},
	}
}

// RecordTokenUsage persists LLM token usage to the DB and sends PostHog
// $ai_generation (per LLM call) + $ai_trace (grouping) events for the
// built-in LLM Analytics dashboard.
//
// When tracker.TraceID is set, it is used as the $ai_trace_id so callers
// (e.g. agent.Run) can bind the PostHog trace to an external identifier
// like AppAgentRun.ID — this lets RecordLLMError emit error events on the
// same trace, and lets operators pivot from a failed run row straight to
// the PostHog trace view.
//
// When tracker totals are zero (e.g. a run that aborted before any LLM
// round completed), the per-call $ai_generation loop is skipped but we
// still emit a grouping $ai_trace so every agent run is represented in
// PostHog. Error classification is layered on by the caller via
// RecordLLMError using the same trace id.
func (c *Controller) RecordTokenUsage(user *models.User, action, model, provider, reference, appType string, tracker *utils.TokenTracker) {
	prompt, completion, total, callCount := tracker.Totals()

	// Normalize model name: strip provider prefix (e.g. "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
	// so PostHog doesn't fragment cost data across what is logically the same model.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	// Only persist LLM token usage when real tokens flowed — zero-token runs
	// (e.g. aborted before any round completed) are a trace-only concern and
	// shouldn't pollute the usage table.
	if total > 0 {
		if err := c.DB.CreateLLMTokenUsage(&models.LLMTokenUsage{
			UserID:           user.ID,
			Action:           action,
			Model:            model,
			PromptTokens:     prompt,
			CompletionTokens: completion,
			TotalTokens:      total,
			CallCount:        callCount,
			Reference:        reference,
		}); err != nil {
			slog.Error("failed to record LLM token usage", "error", err, "user_id", user.ID, "action", action)
		}
	}

	creditsSpent := c.TokensToCredits(prompt, completion)

	if c.PostHog != nil {
		// Caller-supplied trace id takes precedence so error events emitted
		// separately via RecordLLMError land on the same trace, and so the
		// PostHog trace can be pivoted to/from AppAgentRun.ID.
		traceID := tracker.TraceID
		if traceID == "" {
			traceID = uuid.New().String()
		}

		// Per-call $ai_generation events
		for _, entry := range tracker.Entries() {
			// Normalize per-entry model name too (tracker records raw model from each call).
			entryModel := entry.Model
			if idx := strings.LastIndex(entryModel, "/"); idx >= 0 {
				entryModel = entryModel[idx+1:]
			}
			spanID := uuid.New().String()
			props := posthog.NewProperties().
				Set("$ai_provider", provider).
				Set("$ai_model", entryModel).
				Set("$ai_input_tokens", entry.PromptTokens).
				Set("$ai_output_tokens", entry.CompletionTokens).
				Set("$ai_trace_id", traceID).
				Set("$ai_span_id", spanID).
				Set("$ai_parent_id", traceID).
				Set("$ai_session_id", reference).
				Set("$ai_latency", float64(entry.LatencyMs)/1000.0).
				Set("$ai_is_error", false).
				Set("$ai_stream", entry.IsStreaming).
				Set("app_type", appType).
				Set("project_id", reference).
				Set("credits_estimated", c.TokensToCredits(entry.PromptTokens, entry.CompletionTokens))
			if tracker.RunID != "" {
				props.Set("run_id", tracker.RunID)
			}
			if entry.CacheCreationInputTokens > 0 {
				props.Set("$ai_cache_creation_input_tokens", entry.CacheCreationInputTokens)
			}
			if entry.CacheReadInputTokens > 0 {
				props.Set("$ai_cache_read_input_tokens", entry.CacheReadInputTokens)
			}
			if entry.SpanName != "" {
				props.Set("$ai_span_name", entry.SpanName)
			}
			if user.Email != "" {
				props.Set("$email", user.Email)
			}
			if entry.CostUSD > 0 {
				props.Set("$ai_total_cost_usd", entry.CostUSD)
				// Standard PostHog LLM Analytics properties for built-in dashboard
				inputCost, outputCost := utils.EstimateInputOutputCostUSD(
					entryModel, entry.PromptTokens, entry.CompletionTokens,
					entry.CacheCreationInputTokens, entry.CacheReadInputTokens,
				)
				if inputCost > 0 {
					props.Set("$ai_input_cost", inputCost)
				}
				if outputCost > 0 {
					props.Set("$ai_output_cost", outputCost)
				}
			}
			if entry.InputMessages != nil {
				props.Set("$ai_input", messagesToPostHogInput(entry.InputMessages))
			}
			if entry.OutputText != "" {
				props.Set("$ai_output_choices", textToOutputChoices(entry.OutputText))
				// Also send truncated $ai_output for HogQL queryability
				outputPreview := entry.OutputText
				if len(outputPreview) > 1000 {
					outputPreview = outputPreview[:1000] + "..."
				}
				props.Set("$ai_output", outputPreview)
			}
			capture := posthog.Capture{
				DistinctId: user.ID.String(),
				Event:      "$ai_generation",
				Properties: props,
			}
			if !entry.RecordedAt.IsZero() {
				capture.Timestamp = entry.RecordedAt
			}
			c.PostHog.Enqueue(capture)
		}

		// Compute wall-clock latency and total cost across all LLM calls for the trace event.
		var totalComputeMs int64
		var totalCost float64
		for _, e := range tracker.Entries() {
			totalComputeMs += e.LatencyMs
			totalCost += e.CostUSD
		}
		// Use wall-clock elapsed time for $ai_latency (accurate when LLM calls run in parallel).
		// Keep sum-of-calls as $ai_compute_time for cost analysis.
		wallClockMs := time.Since(tracker.StartedAt).Milliseconds()

		// Grouping $ai_trace event
		traceProps := posthog.NewProperties().
			Set("$ai_trace_id", traceID).
			Set("$ai_trace_name", action).
			Set("$ai_session_id", reference).
			Set("$ai_model", model).
			Set("$ai_provider", provider).
			Set("$ai_latency", float64(wallClockMs)/1000.0).
			Set("$ai_compute_time", float64(totalComputeMs)/1000.0).
			Set("$ai_input_tokens", prompt).
			Set("$ai_output_tokens", completion).
			Set("app_type", appType).
			Set("project_id", reference).
			Set("credits_estimated", creditsSpent)
		if tracker.RunID != "" {
			traceProps.Set("run_id", tracker.RunID)
		}
		if totalCost > 0 {
			traceProps.Set("$ai_total_cost_usd", totalCost)
		}
		if user.Email != "" {
			traceProps.Set("$email", user.Email)
		}
		if tracker.TraceInput != "" {
			traceProps.Set("$ai_input_state", tracker.TraceInput)
		}
		if tracker.TraceOutput != "" {
			traceProps.Set("$ai_output_state", tracker.TraceOutput)
		}
		if tools := tracker.ToolsUsed(); len(tools) > 0 {
			traceProps.Set("$ai_tools", tools)
		}
		if tracker.TraceErrorCode != "" {
			traceProps.Set("$ai_is_error", true)
			traceProps.Set("$ai_error_code", tracker.TraceErrorCode)
			if tracker.TraceErrorMessage != "" {
				traceProps.Set("$ai_error_message", tracker.TraceErrorMessage)
			}
		}
		c.PostHog.Enqueue(posthog.Capture{
			DistinctId: user.ID.String(),
			Event:      "$ai_trace",
			Properties: traceProps,
		})
	}
}

// RecordLLMError sends a PostHog $ai_generation event for a failed LLM call
// and — when the tracker is non-nil — a matching $ai_trace grouping event so
// failures always surface on the LLM Analytics dashboard even when no round
// completed. The tracker's TraceID/RunID link this error to the run's other
// events and to the AppAgentRun row.
func (c *Controller) RecordLLMError(user *models.User, tracker *utils.TokenTracker, action, model, provider, reference, appType string, latencyMs int64, errCode, errMsg string) {
	if c.PostHog == nil || user == nil {
		return
	}

	// Normalize model name: strip provider prefix (e.g. "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
	// so PostHog doesn't fragment cost data across what is logically the same model.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	traceID := ""
	runID := ""
	if tracker != nil {
		traceID = tracker.TraceID
		runID = tracker.RunID
	}

	props := posthog.NewProperties().
		Set("$ai_provider", provider).
		Set("$ai_model", model).
		Set("$ai_is_error", true).
		Set("$ai_latency", float64(latencyMs)/1000.0).
		Set("$ai_session_id", reference).
		Set("$ai_error_message", errMsg).
		Set("$ai_span_name", "agent_error").
		Set("app_type", appType).
		Set("project_id", reference)
	if errCode != "" {
		props.Set("$ai_error_code", errCode)
	}
	if traceID != "" {
		props.Set("$ai_trace_id", traceID)
		// Synthesize a span id for the error event so it appears as a proper
		// child in the trace timeline instead of collapsing onto the trace root.
		props.Set("$ai_span_id", uuid.New().String())
		props.Set("$ai_parent_id", traceID)
	}
	if runID != "" {
		props.Set("run_id", runID)
	}
	if user.Email != "" {
		props.Set("$email", user.Email)
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: user.ID.String(),
		Event:      "$ai_generation",
		Properties: props,
	})
}
