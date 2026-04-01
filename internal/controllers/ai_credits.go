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
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/uptrace/bun"
)

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

		// Grant monthly credits from plan
		initialGrant := 0
		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
		if limitsErr == nil && limits.MonthlyAICredits > 0 {
			balance.Balance = limits.MonthlyAICredits
			initialGrant = limits.MonthlyAICredits
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
		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
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

		// Calculate new balance with rollover
		newBalance := monthlyCredits
		if balance.Balance < 0 {
			// Overdraft always carries forward regardless of rollover setting
			newBalance = monthlyCredits + balance.Balance
			if newBalance < 0 {
				newBalance = 0
			}
		} else if rolloverMonths > 0 {
			cap := monthlyCredits * (rolloverMonths + 1)
			newBalance = balance.Balance + monthlyCredits
			if newBalance > cap {
				newBalance = cap
			}
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

// DeductAICreditsUnchecked deducts credits without checking sufficiency (balance may go negative
// down to AICreditsMinBalance). Used for post-hoc token-based billing where the pre-check already ran.
func (c *Controller) DeductAICreditsUnchecked(user *models.User, cost int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

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
	}

	return txErr
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
	limits, err := c.DB.GetStandartUserLimitsByUser(user)
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
func (c *Controller) RecordTokenUsage(user *models.User, action, model, provider, reference, appType string, tracker *utils.TokenTracker) {
	prompt, completion, total, callCount := tracker.Totals()
	if total == 0 {
		return
	}

	// Normalize model name: strip provider prefix (e.g. "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
	// so PostHog doesn't fragment cost data across what is logically the same model.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

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

	creditsSpent := models.TokensToCredits(prompt, completion)

	if c.PostHog != nil {
		traceID := uuid.New().String()

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
				Set("credits_estimated", models.TokensToCredits(entry.PromptTokens, entry.CompletionTokens))
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
			Set("credits_estimated", creditsSpent)
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
		c.PostHog.Enqueue(posthog.Capture{
			DistinctId: user.ID.String(),
			Event:      "$ai_trace",
			Properties: traceProps,
		})
	}
}

// RecordLLMError sends a PostHog $ai_generation event for a failed LLM call.
// An optional traceID links the error to an existing trace.
func (c *Controller) RecordLLMError(user *models.User, action, model, provider, reference, appType string, latencyMs int64, errMsg string, traceID ...string) {
	if c.PostHog == nil || user == nil {
		return
	}

	// Normalize model name: strip provider prefix (e.g. "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
	// so PostHog doesn't fragment cost data across what is logically the same model.
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	props := posthog.NewProperties().
		Set("$ai_provider", provider).
		Set("$ai_model", model).
		Set("$ai_is_error", true).
		Set("$ai_latency", float64(latencyMs)/1000.0).
		Set("$ai_session_id", reference).
		Set("$ai_error_message", errMsg).
		Set("app_type", appType)
	if len(traceID) > 0 && traceID[0] != "" {
		props.Set("$ai_trace_id", traceID[0])
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
