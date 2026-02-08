package controllers

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/knid/wemob/internal/models"
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
