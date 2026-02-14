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
)

// GetOrCreateCreditsBalance returns the user's credit balance, creating one if needed.
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

	// Check if period has expired and reset
	if time.Now().After(balance.PeriodEnd) {
		balance.PeriodStart = time.Now()
		balance.PeriodEnd = time.Now().AddDate(0, 1, 0)

		limits, limitsErr := c.DB.GetStandartUserLimitsByUser(user)
		if limitsErr == nil && limits.MonthlyAICredits > 0 {
			balance.Balance = limits.MonthlyAICredits
		} else {
			balance.Balance = 0
		}

		balance, err = c.DB.UpdateAICreditsBalance(balance)
		if err != nil {
			return nil, err
		}

		// Log the monthly grant
		c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    balance.Balance,
			Action:    "monthly_grant",
			Reference: balance.ID.String(),
		})
	}

	return balance, nil
}

// DeductAICredits deducts credits for an AI action. Returns error if insufficient.
func (c *Controller) DeductAICredits(user *models.User, cost int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

	if balance.Balance < cost {
		return fmt.Errorf("insufficient AI credits: have %d, need %d", balance.Balance, cost)
	}

	balance.Balance -= cost
	_, err = c.DB.UpdateAICreditsBalance(balance)
	if err != nil {
		return err
	}

	_, err = c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
		UserID:    user.ID,
		Amount:    -cost,
		Action:    action,
		Reference: reference,
	})
	return err
}

// DeductAICreditsUnchecked deducts credits without checking sufficiency (balance may go negative).
// Used for post-hoc token-based billing where the pre-check already ran.
func (c *Controller) DeductAICreditsUnchecked(user *models.User, cost int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

	balance.Balance -= cost
	_, err = c.DB.UpdateAICreditsBalance(balance)
	if err != nil {
		return err
	}

	_, err = c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
		UserID:    user.ID,
		Amount:    -cost,
		Action:    action,
		Reference: reference,
	})
	return err
}

// AddAICredits adds credits to a user's balance (for purchases or grants).
func (c *Controller) AddAICredits(user *models.User, amount int, action string, reference string) error {
	balance, err := c.GetOrCreateCreditsBalance(user)
	if err != nil {
		return err
	}

	balance.Balance += amount
	_, err = c.DB.UpdateAICreditsBalance(balance)
	if err != nil {
		return err
	}

	_, err = c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
		UserID:    user.ID,
		Amount:    amount,
		Action:    action,
		Reference: reference,
	})
	return err
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

		// Per-call $ai_generation events
		for _, entry := range tracker.Entries() {
			c.PostHog.Enqueue(posthog.Capture{
				DistinctId: userID.String(),
				Event:      "$ai_generation",
				Properties: posthog.NewProperties().
					Set("$ai_provider", provider).
					Set("$ai_model", entry.Model).
					Set("$ai_input_tokens", entry.PromptTokens).
					Set("$ai_output_tokens", entry.CompletionTokens).
					Set("$ai_trace_id", traceID).
					Set("$ai_latency", float64(entry.LatencyMs)/1000.0).
					Set("$ai_is_error", false).
					Set("$ai_stream", entry.IsStreaming),
			})
		}

		// Grouping $ai_trace event
		c.PostHog.Enqueue(posthog.Capture{
			DistinctId: userID.String(),
			Event:      "$ai_trace",
			Properties: posthog.NewProperties().
				Set("$ai_trace_id", traceID).
				Set("$ai_trace_name", action).
				Set("$ai_input_tokens", prompt).
				Set("$ai_output_tokens", completion),
		})
	}
}
