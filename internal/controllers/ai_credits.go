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
func (c *Controller) ComputeCreditBreakdown(user *models.User, orgID uuid.UUID) (*CreditBreakdown, error) {
	balance, err := c.GetAICreditsBalanceNoReset(user, orgID)
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
func (c *Controller) GetAICreditsBalanceNoReset(user *models.User, orgID uuid.UUID) (*models.AICreditsBalance, error) {
	balance, err := c.DB.GetAICreditsBalanceByOrg(orgID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.GetOrCreateCreditsBalance(user, orgID)
		}
		return nil, err
	}
	return balance, nil
}

// GetOrCreateCreditsBalance returns the org's credit balance, creating one if needed.
// Period resets use atomic SQL to prevent duplicate monthly_grant transactions.
func (c *Controller) GetOrCreateCreditsBalance(user *models.User, orgID uuid.UUID) (*models.AICreditsBalance, error) {
	balance, err := c.DB.GetAICreditsBalanceByOrg(orgID.String())
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		// Subscription billing period is anchored on the organization (Plan 5).
		org, orgErr := c.DB.GetOrganizationByID(orgID.String())
		if orgErr != nil {
			return nil, orgErr
		}

		// Create a new balance aligned with the subscription billing period
		periodStart, periodEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())
		balance = &models.AICreditsBalance{
			OrganizationID: orgID,
			UserID:         user.ID,
			Balance:        0,
			PeriodStart:    periodStart,
			PeriodEnd:      periodEnd,
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
				OrganizationID: orgID,
				UserID:         user.ID,
				Amount:         initialGrant,
				Action:         "initial_grant",
				Reference:      balance.ID.String(),
			}); txErr != nil {
				slog.Error("failed to log initial credit grant", "error", txErr, "user_id", user.ID, "org_id", orgID)
			}
		}
	}

	// Check if period has expired and reset atomically
	if time.Now().After(balance.PeriodEnd) {
		// Subscription billing period is anchored on the organization (Plan 5).
		org, orgErr := c.DB.GetOrganizationByID(orgID.String())
		if orgErr != nil {
			return nil, orgErr
		}
		periodStart, periodEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())

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
				balance, err = c.DB.GetAICreditsBalanceByOrg(orgID.String())
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
		breakdown, bdErr := c.ComputeCreditBreakdown(user, orgID)
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
				OrganizationID: orgID,
				UserID:         user.ID,
				Amount:         monthlyCredits,
				Action:         "monthly_grant",
				Reference:      balance.ID.String(),
			}); err != nil {
				slog.Error("failed to log monthly_grant transaction", "error", err, "user_id", user.ID, "org_id", orgID)
			}
		} else {
			// Another request already reset the period — re-read the current balance
			balance, err = c.DB.GetAICreditsBalanceByOrg(orgID.String())
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
func (c *Controller) GrantInitialFreeCreditsIfNeeded(user *models.User, orgID uuid.UUID) error {
	if !user.Verified {
		return nil
	}

	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
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
			OrganizationID: orgID,
			UserID:         user.ID,
			Amount:         limits.MonthlyAICredits,
			Action:         "initial_grant",
			Reference:      balance.ID.String(),
		}); err != nil {
			return fmt.Errorf("failed to log initial grant: %w", err)
		}
		return nil
	})
}

// TokensToCredits converts token counts to credits using remote-config values
// for tokens-per-credit, output multiplier, and cache-read divisor. Falls back
// to hardcoded defaults if remote config is unavailable.
func (c *Controller) TokensToCredits(promptTokens, completionTokens, cacheReadTokens int) int {
	tpc, om, crd := c.creditFormulaParams()
	return models.TokensToCreditsWithConfig(promptTokens, completionTokens, cacheReadTokens, tpc, om, crd)
}

// creditFormulaParams resolves the three remote-config knobs that drive the
// credit formula, falling back to hardcoded defaults when RC returns
// non-positive values (treated as "unset").
func (c *Controller) creditFormulaParams() (tokensPerCredit, outputMultiplier, cacheReadDivisor int) {
	tokensPerCredit = c.RemoteConfig.GetInt(remoteconfig.AICreditsTokensPerCredit, remoteconfig.Global)
	outputMultiplier = c.RemoteConfig.GetInt(remoteconfig.AICreditsOutputMultiplier, remoteconfig.Global)
	cacheReadDivisor = c.RemoteConfig.GetInt(remoteconfig.AICreditsCacheReadDivisor, remoteconfig.Global)
	if tokensPerCredit <= 0 {
		tokensPerCredit, _ = remoteconfig.AICreditsTokensPerCredit.Fallback.(int)
	}
	if outputMultiplier <= 0 {
		outputMultiplier, _ = remoteconfig.AICreditsOutputMultiplier.Fallback.(int)
	}
	if cacheReadDivisor <= 0 {
		cacheReadDivisor, _ = remoteconfig.AICreditsCacheReadDivisor.Fallback.(int)
	}
	return
}

// CreditsForTracker totals the credit cost across every recorded LLM call in
// the tracker, rounding each call up independently. Per-call rounding is what
// users are actually charged — aggregating tokens first and rounding once
// systematically under-bills (e.g. a 35-call run was billed 53 credits with
// aggregate-then-round but should have been 71 with per-call rounding).
func (c *Controller) CreditsForTracker(tracker *utils.TokenTracker) int {
	if tracker == nil {
		return 0
	}
	tpc, om, crd := c.creditFormulaParams()
	total := 0
	for _, e := range tracker.Entries() {
		total += models.TokensToCreditsWithConfig(e.PromptTokens, e.CompletionTokens, e.CacheReadInputTokens, tpc, om, crd)
	}
	return total
}

// CreditBudget returns the maximum credits the user can spend in a single session.
// Budget = current balance - floor (floor is negative, so this is balance + |floor|).
// For example: balance=1, floor=-20 → budget=21.
func (c *Controller) CreditBudget(user *models.User, orgID uuid.UUID) (int, error) {
	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
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
// has reached the user's budget. When exhausted, it logs a warning and returns
// false. Spent credits are summed per-call (matches what DeductCredits will
// charge at run end) so a cache-heavy run can't slip past the check by way of
// aggregate-then-round under-counting.
func (c *Controller) MakeCreditBudgetChecker(user *models.User, orgID uuid.UUID, tracker *utils.TokenTracker) func() bool {
	budget, err := c.CreditBudget(user, orgID)
	if err != nil {
		return nil
	}
	return func() bool {
		spent := c.CreditsForTracker(tracker)
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
func (c *Controller) DeductAICreditsUnchecked(user *models.User, orgID uuid.UUID, cost int, action string, reference string) error {
	if org, orgErr := c.DB.GetOrganizationByID(orgID.String()); orgErr == nil && IsOrgInDunning(org) {
		return ErrOrgInDunning
	}
	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		return err
	}
	prevBalance := balance.Balance

	// Cap check: if the member has a monthly_credit_cap, verify their period spend + cost ≤ cap.
	if member, mErr := c.DB.GetOrganizationMember(orgID.String(), user.ID.String()); mErr == nil && member != nil && member.MonthlyCreditCap != nil {
		if bal, _ := c.DB.GetAICreditsBalanceByOrg(orgID.String()); bal != nil {
			spent, _ := c.DB.MemberPeriodSpent(orgID.String(), user.ID.String(), bal.PeriodStart, bal.PeriodEnd)
			if spent+cost > *member.MonthlyCreditCap {
				return &MemberCreditCapError{Cap: *member.MonthlyCreditCap, Spent: spent, Requested: cost}
			}
		}
	}

	var newBalance int
	txErr := c.DB.RunInTx(func(tx bun.Tx) error {
		var err error
		minBalance := c.RemoteConfig.GetInt(remoteconfig.AICreditsMinBalance, user.ID.String())
		newBalance, err = c.DB.AtomicDeductCreditsUnchecked(tx, balance.ID.String(), cost, minBalance)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w (floor: %d)", ErrAICreditsFloorReached, minBalance)
			}
			return err
		}

		_, err = c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			OrganizationID: orgID,
			UserID:         user.ID,
			Amount:         -cost,
			Action:         action,
			Reference:      reference,
		})
		return err
	})

	if txErr == nil {
		c.TrackUserEvent(user, "ai_credits_deducted", posthog.NewProperties().
			Set("action", action).
			Set("cost", cost).
			Set("remaining_balance", newBalance).
			Set("reference", reference))

		c.emitCreditThresholdEvents(user, orgID, prevBalance, newBalance, action, reference)
	}

	return txErr
}

// DeductAICreditsToFloor deducts credits, clamping at the overdraft floor instead
// of failing. Always deducts something if balance is above floor. Used as the
// primary deduction method for generation sessions where the cost may exceed budget.
func (c *Controller) DeductAICreditsToFloor(user *models.User, orgID uuid.UUID, cost int, action string, reference string) error {
	if cost <= 0 {
		return nil
	}
	if org, orgErr := c.DB.GetOrganizationByID(orgID.String()); orgErr == nil && IsOrgInDunning(org) {
		return ErrOrgInDunning
	}
	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		return err
	}
	prevBalance := balance.Balance

	// Cap check: if the member has a monthly_credit_cap, verify their period spend + cost ≤ cap.
	if member, mErr := c.DB.GetOrganizationMember(orgID.String(), user.ID.String()); mErr == nil && member != nil && member.MonthlyCreditCap != nil {
		if bal, _ := c.DB.GetAICreditsBalanceByOrg(orgID.String()); bal != nil {
			spent, _ := c.DB.MemberPeriodSpent(orgID.String(), user.ID.String(), bal.PeriodStart, bal.PeriodEnd)
			if spent+cost > *member.MonthlyCreditCap {
				return &MemberCreditCapError{Cap: *member.MonthlyCreditCap, Spent: spent, Requested: cost}
			}
		}
	}

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
			OrganizationID: orgID,
			UserID:         user.ID,
			Amount:         -deducted,
			Action:         action,
			Reference:      reference,
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

		c.emitCreditThresholdEvents(user, orgID, prevBalance, newBalance, action, reference)
	}

	return txErr
}

// emitCreditThresholdEvents fires lifecycle events when a deduction crosses a
// percentage-of-monthly-grant threshold (50%, 25%, 10%, 0%) or triggers
// overdraft. Each crossing fires exactly once per deduction — if a large
// deduction skips over multiple thresholds, the lowest crossed threshold is
// used. Designed to feed marketing workflows without spamming CRM on every
// deduct, and to fire immediately instead of waiting for the hourly job.
//
// orgID is the org the deduction was charged against — used to resolve the
// org-scoped billing URL we ship to the email templates.
func (c *Controller) emitCreditThresholdEvents(user *models.User, orgID uuid.UUID, prevBalance, newBalance int, action, reference string) {
	// Resolve org once for the billing URL injected into every event below.
	var billingURL string
	if org, err := c.DB.GetOrganizationByID(orgID.String()); err == nil && org != nil {
		billingURL = c.BillingURLForOrg(org)
	} else {
		billingURL = c.BillingURLForUser(user)
	}

	// Overdraft transition (balance went from >= 0 to < 0)
	if prevBalance >= 0 && newBalance < 0 {
		go c.EventService.SendEvent(user, constants.EVENT_CREDITS_OVERDRAFT_STARTED, map[string]any{
			"previous_balance": prevBalance,
			"new_balance":      newBalance,
			"action":           action,
			"reference":        reference,
			"billing_url":      billingURL,
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
				"billing_url":       billingURL,
			})
			return // only fire the lowest newly-crossed threshold
		}
	}
}

// AddAICredits adds credits to a user's balance (for purchases or grants).
// Uses atomic SQL + DB transaction to ensure balance and audit log stay consistent.
func (c *Controller) AddAICredits(user *models.User, orgID uuid.UUID, amount int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		return err
	}

	return c.DB.RunInTx(func(tx bun.Tx) error {
		if _, err := c.DB.AtomicAddCredits(tx, balance.ID.String(), amount); err != nil {
			return err
		}
		_, err := c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			OrganizationID: orgID,
			UserID:         user.ID,
			Amount:         amount,
			Action:         action,
			Reference:      reference,
		})
		return err
	})
}

// AddAICreditsInTx is a tx-aware variant of AddAICredits for retryable callers
// that need their money-write to share an outer transaction with other writes
// (e.g. the Stripe webhook dedup row + the credit grant must commit atomically).
//
// Idempotent on (action, reference) when reference is non-empty: if a credit
// transaction with the same action+reference already exists, the call is a
// no-op. The caller must resolve the credits balance via GetOrCreateCreditsBalance
// before opening the tx — balance creation involves period-reset logic that
// can't easily share an outer tx.
func (c *Controller) AddAICreditsInTx(tx bun.Tx, user *models.User, orgID uuid.UUID, balanceID uuid.UUID, amount int, action string, reference string) error {
	if reference != "" {
		exists, err := c.DB.HasAICreditTransactionByRefTx(tx, action, reference)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	if _, err := c.DB.AtomicAddCredits(tx, balanceID.String(), amount); err != nil {
		return err
	}

	_, err := c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
		OrganizationID: orgID,
		UserID:         user.ID,
		Amount:         amount,
		Action:         action,
		Reference:      reference,
	})
	return err
}

// GrantSubscriptionCredits grants the plan's monthly credits when a user first subscribes.
// If a credits balance already exists (created before subscribing), it resets the period
// to align with the new subscription and adds the plan's monthly credits.
// Safe to call multiple times — uses a transaction with FOR UPDATE to prevent double grants.
func (c *Controller) GrantSubscriptionCredits(user *models.User, orgID uuid.UUID) {
	granted, credits, subRef, _ := c.grantSubscriptionCreditsCore(nil, user, orgID)
	if !granted {
		return
	}
	go c.EventService.SendEvent(user, constants.EVENT_CREDITS_GRANTED, map[string]any{
		"credits":    credits,
		"grant_type": "subscription",
		"reference":  subRef,
	})
}

// GrantSubscriptionCreditsInTx is a tx-aware variant of GrantSubscriptionCredits
// for the Stripe webhook dedup path, where the grant must commit atomically
// with the webhook_events insert. Returns (post-commit hook, error). If err is
// non-nil the outer tx must be rolled back; if hook is nil the grant was a
// no-op (already granted) but the tx is still valid to commit.
func (c *Controller) GrantSubscriptionCreditsInTx(tx bun.Tx, user *models.User, orgID uuid.UUID) (func(), error) {
	granted, credits, subRef, err := c.grantSubscriptionCreditsCore(&tx, user, orgID)
	if err != nil {
		return nil, err
	}
	if !granted {
		return nil, nil
	}
	return func() {
		c.EventService.SendEvent(user, constants.EVENT_CREDITS_GRANTED, map[string]any{
			"credits":    credits,
			"grant_type": "subscription",
			"reference":  subRef,
		})
	}, nil
}

// grantSubscriptionCreditsCore does the actual grant work, optionally inside an
// outer tx. Returns (granted, credits, subscriptionID, err). When outerTx is
// non-nil, errors propagate so the caller can roll back; granted=false with
// err=nil means the grant was a legitimate no-op (already granted, no plan).
func (c *Controller) grantSubscriptionCreditsCore(outerTx *bun.Tx, user *models.User, orgID uuid.UUID) (bool, int, string, error) {
	limits, err := c.GetUserLimits(user)
	if err != nil || limits.MonthlyAICredits <= 0 {
		return false, 0, "", nil
	}

	// Subscription state (start, ID) lives on the organization (Plan 5).
	org, err := c.DB.GetOrganizationByID(orgID.String())
	if err != nil {
		slog.Error("failed to load organization for subscription grant", "error", err, "user_id", user.ID, "org_id", orgID)
		if outerTx != nil {
			return false, 0, "", fmt.Errorf("load organization for subscription grant: %w", err)
		}
		return false, 0, "", nil
	}

	balance, err := c.GetOrCreateCreditsBalance(user, orgID)
	if err != nil {
		slog.Error("failed to get/create credits balance for subscription grant", "error", err, "user_id", user.ID, "org_id", orgID)
		if outerTx != nil {
			return false, 0, "", fmt.Errorf("get/create credits balance for subscription grant: %w", err)
		}
		return false, 0, "", nil
	}

	work := func(tx bun.Tx) (bool, error) {
		// Lock the balance row to serialize concurrent grant attempts
		var locked models.AICreditsBalance
		if err := tx.NewRaw(
			"SELECT * FROM ai_credits_balances WHERE id = ? FOR UPDATE",
			balance.ID,
		).Scan(context.Background(), &locked); err != nil {
			return false, fmt.Errorf("lock balance: %w", err)
		}

		// Check for existing subscription_grant inside the transaction (scoped to this org)
		var count int
		if err := tx.NewRaw(
			"SELECT COUNT(*) FROM ai_credit_transactions WHERE organization_id = ? AND action = 'subscription_grant'",
			orgID,
		).Scan(context.Background(), &count); err != nil {
			return false, fmt.Errorf("check existing grant: %w", err)
		}
		if count > 0 {
			return false, nil // already granted
		}

		// Reset period to align with the new subscription start and grant credits
		periodStart, periodEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())
		if _, err := tx.NewRaw(
			"UPDATE ai_credits_balances SET balance = balance + ?, period_start = ?, period_end = ?, updated_at = NOW() WHERE id = ?",
			limits.MonthlyAICredits, periodStart, periodEnd, balance.ID,
		).Exec(context.Background()); err != nil {
			return false, fmt.Errorf("update balance: %w", err)
		}

		if _, err := c.DB.CreateAICreditTransactionTx(tx, &models.AICreditTransaction{
			OrganizationID: orgID,
			UserID:         user.ID,
			Amount:         limits.MonthlyAICredits,
			Action:         "subscription_grant",
			Reference:      org.SubscriptionID.String(),
		}); err != nil {
			return false, fmt.Errorf("log grant: %w", err)
		}

		return true, nil
	}

	var granted bool
	if outerTx != nil {
		var werr error
		granted, werr = work(*outerTx)
		if werr != nil {
			slog.Error("failed to grant subscription credits", "error", werr, "user_id", user.ID, "org_id", orgID)
			return false, 0, "", werr
		}
	} else {
		txErr := c.DB.RunInTx(func(tx bun.Tx) error {
			var werr error
			granted, werr = work(tx)
			return werr
		})
		if txErr != nil {
			slog.Error("failed to grant subscription credits", "error", txErr, "user_id", user.ID, "org_id", orgID)
			return false, 0, "", nil
		}
	}

	if !granted {
		return false, 0, "", nil
	}
	slog.Info("subscription credits granted", "user_id", user.ID, "credits", limits.MonthlyAICredits)
	return true, limits.MonthlyAICredits, org.SubscriptionID.String(), nil
}

// messagesToPostHogInput converts []utils.Message to the format PostHog expects
// for $ai_input: [{role, content: [{type: "text", text: "..."}]}].
// Caps the payload at 50 messages × 10K chars each. When the conversation is
// longer than the message cap, the leading system prompt is pinned and the
// remainder is filled from the tail — a straight tail-slice was dropping the
// system block on long chats, which made traces useless for debugging cache
// behaviour and prompt issues.
func messagesToPostHogInput(msgs []utils.Message) []map[string]interface{} {
	const maxMessages = 50
	const maxChars = 10000

	pickIndices := func() []int {
		if len(msgs) <= maxMessages {
			indices := make([]int, len(msgs))
			for i := range msgs {
				indices[i] = i
			}
			return indices
		}
		// Long chat: keep the system prompt (always msgs[0] when present)
		// + last (maxMessages-1) messages.
		systemHead := 0
		if msgs[0].Role == "system" {
			systemHead = 1
		}
		tailStart := len(msgs) - (maxMessages - systemHead)
		if tailStart < systemHead {
			tailStart = systemHead
		}
		indices := make([]int, 0, maxMessages)
		for i := 0; i < systemHead; i++ {
			indices = append(indices, i)
		}
		for i := tailStart; i < len(msgs); i++ {
			indices = append(indices, i)
		}
		return indices
	}

	indices := pickIndices()
	out := make([]map[string]interface{}, 0, len(indices))
	for _, i := range indices {
		content := msgs[i].Content
		if len(content) > maxChars {
			content = content[:maxChars] + "..."
		}
		out = append(out, map[string]interface{}{
			"role": msgs[i].Role,
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

	// creditsSpent matches what DeductCredits charges: per-call rounded with
	// cache-read tokens included. Used only as a PostHog property on the
	// grouping trace event for ops visibility — actual deduction happens via
	// DeductCredits at the agent.Run finalisation site.
	creditsSpent := c.CreditsForTracker(tracker)

	if c.PostHog != nil {
		// Caller-supplied trace id takes precedence so error events emitted
		// separately via RecordLLMError land on the same trace, and so the
		// PostHog trace can be pivoted to/from AppAgentRun.ID.
		traceID := tracker.TraceID
		if traceID == "" {
			traceID = uuid.New().String()
		}

		// $ai_session_id groups events from one conversational session in
		// PostHog's LLM dashboards. Use RunID when the caller has one (agent
		// runs); fall back to the trace id so single-trace events still
		// group (instead of every chat-turn for the same project being
		// collapsed into a project-id "session", which is what reference=projectID
		// caused before).
		sessionID := tracker.RunID
		if sessionID == "" {
			sessionID = traceID
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
				Set("$ai_session_id", sessionID).
				Set("$ai_latency", float64(entry.LatencyMs)/1000.0).
				Set("$ai_is_error", false).
				Set("$ai_stream", entry.IsStreaming).
				Set("app_type", appType).
				Set("app_id", reference).
				Set("project_id", reference).
				Set("credits_estimated", c.TokensToCredits(entry.PromptTokens, entry.CompletionTokens, entry.CacheReadInputTokens))
			if tracker.OrganizationID != "" {
				props.Set("org_id", tracker.OrganizationID)
			}
			if tracker.RunID != "" {
				props.Set("run_id", tracker.RunID)
				// Bind PostHog session-replay correlation to the agent run so a
				// single chat thread's worth of LLM calls cluster as one
				// $session_id in dashboards.
				props.Set("$session_id", tracker.RunID)
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
			}
			// Always compute the catalog-based input/output split so PostHog's
			// built-in cost panels work for every model in modelCosts. Returns
			// (0, 0) for unknown models — emit only the non-zero leg so we don't
			// distort dashboards by claiming we know a cost we don't.
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
			Set("$ai_session_id", sessionID).
			Set("$ai_model", model).
			Set("$ai_provider", provider).
			Set("$ai_latency", float64(wallClockMs)/1000.0).
			Set("$ai_compute_time", float64(totalComputeMs)/1000.0).
			Set("$ai_input_tokens", prompt).
			Set("$ai_output_tokens", completion).
			Set("app_type", appType).
			Set("app_id", reference).
			Set("project_id", reference).
			Set("credits_estimated", creditsSpent)
		if tracker.OrganizationID != "" {
			traceProps.Set("org_id", tracker.OrganizationID)
		}
		if tracker.RunID != "" {
			traceProps.Set("run_id", tracker.RunID)
			traceProps.Set("$session_id", tracker.RunID)
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

// OrgCreditsView is the admin/owner view of an org's credit pool.
type OrgCreditsView struct {
	OrganizationID uuid.UUID              `json:"organization_id"`
	Balance        int                    `json:"balance"`
	PendingGateway float64                `json:"pending_gateway"`
	PeriodStart    time.Time              `json:"period_start"`
	PeriodEnd      time.Time              `json:"period_end"`
	Members        []OrgCreditsMemberView `json:"members"`
}

type OrgCreditsMemberView struct {
	MemberID         uuid.UUID `json:"member_id"`
	UserID           uuid.UUID `json:"user_id"`
	UserEmail        string    `json:"user_email"`
	UserName         string    `json:"user_name"`
	Role             string    `json:"role"`
	MonthlyCreditCap *int      `json:"monthly_credit_cap,omitempty"`
	PeriodSpent      int       `json:"period_spent"`
}

// GetOrgCreditsViewForAdmin returns the admin/owner view of an org's credits:
// pool balance, period dates, and per-member usage breakdown with caps.
// Caller must be owner or admin of the org.
func (c *Controller) GetOrgCreditsViewForAdmin(user *models.User, orgID string) (*OrgCreditsView, error) {
	caller, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if caller.Role != "owner" && caller.Role != "admin" {
		return nil, errors.New("insufficient permissions")
	}

	bal, err := c.DB.GetAICreditsBalanceByOrg(orgID)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	members, err := c.DB.GetOrganizationMembers(orgID)
	if err != nil {
		return nil, fmt.Errorf("get members: %w", err)
	}

	breakdown, err := c.DB.OrgUsageBreakdown(orgID, bal.PeriodStart, bal.PeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("usage breakdown: %w", err)
	}
	spentByUser := make(map[uuid.UUID]int, len(breakdown))
	for _, r := range breakdown {
		spentByUser[r.UserID] = r.Spent
	}

	views := make([]OrgCreditsMemberView, 0, len(members))
	for _, m := range members {
		mv := OrgCreditsMemberView{
			MemberID:         m.ID,
			UserID:           m.UserID,
			Role:             m.Role,
			MonthlyCreditCap: m.MonthlyCreditCap,
			PeriodSpent:      spentByUser[m.UserID],
		}
		if m.User != nil {
			mv.UserEmail = m.User.Email
			mv.UserName = m.User.Name
		}
		views = append(views, mv)
	}

	return &OrgCreditsView{
		OrganizationID: bal.OrganizationID,
		Balance:        bal.Balance,
		PendingGateway: bal.PendingGateway,
		PeriodStart:    bal.PeriodStart,
		PeriodEnd:      bal.PeriodEnd,
		Members:        views,
	}, nil
}

// MemberCreditsView is what a non-admin member sees: their own cap + own usage.
// Pool total NOT included by design.
type MemberCreditsView struct {
	OrganizationID   uuid.UUID `json:"organization_id"`
	PeriodStart      time.Time `json:"period_start"`
	PeriodEnd        time.Time `json:"period_end"`
	MonthlyCreditCap *int      `json:"monthly_credit_cap,omitempty"`
	PeriodSpent      int       `json:"period_spent"`
}

// GetOrgCreditsViewForMember returns the caller's own cap + own period spend
// in an org. No pool total exposed.
func (c *Controller) GetOrgCreditsViewForMember(user *models.User, orgID string) (*MemberCreditsView, error) {
	member, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	bal, err := c.DB.GetAICreditsBalanceByOrg(orgID)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	spent, err := c.DB.MemberPeriodSpent(orgID, user.ID.String(), bal.PeriodStart, bal.PeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("member spent: %w", err)
	}
	return &MemberCreditsView{
		OrganizationID:   bal.OrganizationID,
		PeriodStart:      bal.PeriodStart,
		PeriodEnd:        bal.PeriodEnd,
		MonthlyCreditCap: member.MonthlyCreditCap,
		PeriodSpent:      spent,
	}, nil
}

// SetMemberCreditCap sets or clears a member's monthly credit cap. cap=nil
// clears (unlimited within pool). Caller must be owner/admin of the org.
func (c *Controller) SetMemberCreditCap(user *models.User, orgID, memberID string, cap *int) (*models.OrganizationMember, error) {
	caller, err := c.DB.GetOrganizationMember(orgID, user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}
	if caller.Role != "owner" && caller.Role != "admin" {
		return nil, errors.New("insufficient permissions")
	}
	return c.DB.UpdateMemberCap(orgID, memberID, cap)
}

// RecordLLMError emits a synthetic error $ai_generation span so the failure
// shows up as a child node in the PostHog trace timeline alongside the run's
// successful generations. Skipped when the tracker has zero entries — the
// $ai_trace event already carries TraceErrorCode/Message in that case (set by
// agent.Run before invoking RecordLLMUsage), so emitting an extra
// $ai_generation here would only inflate error-rate panels with a synthetic
// 0-token row that filters like `input_tokens > 0` would silently miss anyway.
//
// The tracker's TraceID/RunID/OrgID link this error to the run's other events
// and to the AppAgentRun row.
func (c *Controller) RecordLLMError(user *models.User, tracker *utils.TokenTracker, action, model, provider, reference, appType string, latencyMs int64, errCode, errMsg string) {
	if c.PostHog == nil || user == nil {
		return
	}
	// No entries → the $ai_trace event already represents the failure. Don't
	// double-emit a synthetic generation that distorts error counts.
	if tracker == nil || len(tracker.Entries()) == 0 {
		return
	}

	// Normalize model name: strip provider prefix (e.g. "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
	// so PostHog doesn't fragment cost data across what is logically the same model.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	traceID := tracker.TraceID
	runID := tracker.RunID

	// Mirror RecordTokenUsage: $ai_session_id = runID when present, otherwise traceID.
	sessionID := runID
	if sessionID == "" {
		sessionID = traceID
	}

	props := posthog.NewProperties().
		Set("$ai_provider", provider).
		Set("$ai_model", model).
		Set("$ai_is_error", true).
		Set("$ai_latency", float64(latencyMs)/1000.0).
		Set("$ai_session_id", sessionID).
		// PostHog LLM Analytics dashboards key off $ai_error (the message
		// string); $ai_error_message stays in parallel for any HogQL queries
		// that already reference the legacy name.
		Set("$ai_error", errMsg).
		Set("$ai_error_message", errMsg).
		Set("$ai_span_name", "agent_error").
		Set("app_type", appType).
		Set("app_id", reference).
		Set("project_id", reference)
	if errCode != "" {
		props.Set("$ai_error_code", errCode)
		props.Set("$ai_error_type", errCode)
	}
	if tracker.OrganizationID != "" {
		props.Set("org_id", tracker.OrganizationID)
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
		props.Set("$session_id", runID)
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
