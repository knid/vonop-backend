package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
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

		// Create a new balance for this billing period
		now := time.Now()
		balance = &models.AICreditsBalance{
			UserID:      user.ID,
			Balance:     0,
			PeriodStart: now,
			PeriodEnd:   now.AddDate(0, 1, 0),
		}

		// Grant monthly credits from plan
		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
		if limitsErr == nil && limits.MonthlyAICredits > 0 {
			balance.Balance = limits.MonthlyAICredits
		}

		balance, err = c.DB.CreateAICreditsBalance(balance)
		if err != nil {
			return nil, err
		}
	}

	// Check if period has expired and reset atomically
	if time.Now().After(balance.PeriodEnd) {
		now := time.Now()
		periodStart := now
		periodEnd := now.AddDate(0, 1, 0)

		monthlyCredits := 0
		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
		if limitsErr == nil && limits.MonthlyAICredits > 0 {
			monthlyCredits = limits.MonthlyAICredits
		}

		newBal, didReset, resetErr := c.DB.AtomicResetPeriod(balance.ID.String(), monthlyCredits, periodStart, periodEnd)
		if resetErr != nil {
			return nil, resetErr
		}

		if didReset {
			balance.Balance = newBal
			balance.PeriodStart = periodStart
			balance.PeriodEnd = periodEnd

			// Log the monthly grant (only the winner of the race creates this)
			c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
				UserID:    user.ID,
				Amount:    monthlyCredits,
				Action:    "monthly_grant",
				Reference: balance.ID.String(),
			})
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
		newBalance, err = c.DB.AtomicDeductCreditsUnchecked(tx, balance.ID.String(), cost, models.AICreditsMinBalance)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("AI credits overdraft limit reached (floor: %d)", models.AICreditsMinBalance)
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

// RecordTokenUsage persists LLM token usage to the DB and sends PostHog
// $ai_generation (per LLM call) + $ai_trace (grouping) events for the
// built-in LLM Analytics dashboard.
func (c *Controller) RecordTokenUsage(userID uuid.UUID, action, model, provider, reference string, tracker *utils.TokenTracker) {
	prompt, completion, total, callCount := tracker.Totals()
	if total == 0 {
		return
	}

	if err := c.DB.CreateLLMTokenUsage(&models.LLMTokenUsage{
		UserID:           userID,
		Action:           action,
		Model:            model,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CallCount:        callCount,
		Reference:        reference,
	}); err != nil {
		slog.Error("failed to record LLM token usage", "error", err, "user_id", userID, "action", action)
	}

	if c.PostHog != nil {
		traceID := uuid.New().String()

		// Resolve email for PostHog user attribution
		var email string
		if user, err := c.DB.GetUserById(userID.String()); err == nil && user != nil {
			email = user.Email
		}

		// Per-call $ai_generation events
		for _, entry := range tracker.Entries() {
			props := posthog.NewProperties().
				Set("$ai_provider", provider).
				Set("$ai_model", entry.Model).
				Set("$ai_input_tokens", entry.PromptTokens).
				Set("$ai_output_tokens", entry.CompletionTokens).
				Set("$ai_trace_id", traceID).
				Set("$ai_latency", float64(entry.LatencyMs)/1000.0).
				Set("$ai_is_error", false).
				Set("$ai_stream", entry.IsStreaming)
			if email != "" {
				props.Set("$email", email)
			}
			c.PostHog.Enqueue(posthog.Capture{
				DistinctId: userID.String(),
				Event:      "$ai_generation",
				Properties: props,
			})
		}

		// Grouping $ai_trace event
		traceProps := posthog.NewProperties().
			Set("$ai_trace_id", traceID).
			Set("$ai_trace_name", action).
			Set("$ai_input_tokens", prompt).
			Set("$ai_output_tokens", completion)
		if email != "" {
			traceProps.Set("$email", email)
		}
		c.PostHog.Enqueue(posthog.Capture{
			DistinctId: userID.String(),
			Event:      "$ai_trace",
			Properties: traceProps,
		})
	}
}
