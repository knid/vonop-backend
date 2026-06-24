package controllers

import (
	"fmt"
	"time"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// AdminListSubscriptions returns all subscription plans with user counts and limits.
func (c *Controller) AdminListSubscriptions() ([]responses.AdminSubscriptionPlanResponse, error) {
	subs, err := c.DB.GetAllSubscriptions()
	if err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}

	items := make([]responses.AdminSubscriptionPlanResponse, len(subs))
	for i, s := range subs {
		userCount, _ := c.DB.CountUsersBySubscriptionID(s.ID.String())
		items[i] = responses.AdminSubscriptionPlanResponse{
			ID:           s.ID,
			Name:         s.Name,
			MonthlyPrice: s.MonthlyPrice,
			AnnualPrice:  s.AnnualPrice,
			UserCount:    userCount,
			LimitsID:     s.LimitsID,
			Limits:       s.Limits,
			CreatedAt:    s.CreatedAt,
		}
	}

	return items, nil
}

// AdminUpdateSubscription updates a subscription plan's fields and its associated limits.
func (c *Controller) AdminUpdateSubscription(subID string, req requests.AdminUpdateSubscriptionPlanRequest) (*responses.AdminSubscriptionPlanResponse, error) {
	sub, err := c.DB.GetSubscriptionById(subID)
	if err != nil {
		return nil, fmt.Errorf("subscription not found: %w", err)
	}

	// Update plan-level fields
	if req.Name != nil {
		sub.Name = *req.Name
	}
	if req.MonthlyPrice != nil {
		sub.MonthlyPrice = *req.MonthlyPrice
	}
	if req.AnnualPrice != nil {
		sub.AnnualPrice = *req.AnnualPrice
	}
	sub.UpdatedAt = time.Now()

	if _, err := c.DB.UpdateSubscription(sub); err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	// Update associated limits
	limits, err := c.DB.GetStandardUserLimitsById(sub.LimitsID.String())
	if err != nil {
		return nil, fmt.Errorf("limits not found: %w", err)
	}

	if req.MaximumCredentialCount != nil {
		limits.MaximumCredentialCount = *req.MaximumCredentialCount
	}
	if req.MontlyNotificationCount != nil {
		limits.MontlyNotificationCount = *req.MontlyNotificationCount
	}
	if req.MonthlyAICredits != nil {
		limits.MonthlyAICredits = *req.MonthlyAICredits
	}
	if req.MaxWebApps != nil {
		limits.MaxWebApps = *req.MaxWebApps
	}
	if req.MaxMobileApps != nil {
		limits.MaxMobileApps = *req.MaxMobileApps
	}
	if req.MaxDeployedWebApps != nil {
		limits.MaxDeployedWebApps = *req.MaxDeployedWebApps
	}
	if req.MaxDeployedMobileApps != nil {
		limits.MaxDeployedMobileApps = *req.MaxDeployedMobileApps
	}
	if req.CanDeployMobile != nil {
		limits.CanDeployMobile = *req.CanDeployMobile
	}
	if req.CanUseAnalytics != nil {
		limits.CanUseAnalytics = *req.CanUseAnalytics
	}
	if req.CanRemoveBrandLogo != nil {
		limits.CanRemoveBrandLogo = *req.CanRemoveBrandLogo
	}
	if req.CanUseEcommerce != nil {
		limits.CanUseEcommerce = *req.CanUseEcommerce
	}
	if req.CanDeployWeb != nil {
		limits.CanDeployWeb = *req.CanDeployWeb
	}
	if req.CanAccessCode != nil {
		limits.CanAccessCode = *req.CanAccessCode
	}
	if req.CanBuyDomain != nil {
		limits.CanBuyDomain = *req.CanBuyDomain
	}
	if req.CanExportToGitHub != nil {
		limits.CanExportToGitHub = *req.CanExportToGitHub
	}
	if req.CanUseEmailService != nil {
		limits.CanUseEmailService = *req.CanUseEmailService
	}
	if req.MinMachinesRunning != nil {
		limits.MinMachinesRunning = *req.MinMachinesRunning
	}
	if req.MonthlyBandwidthGB != nil {
		limits.MonthlyBandwidthGB = *req.MonthlyBandwidthGB
	}
	if req.CanChooseRegion != nil {
		limits.CanChooseRegion = *req.CanChooseRegion
	}
	limits.UpdatedAt = time.Now()

	if _, err := c.DB.UpdateStandardUserLimits(limits); err != nil {
		return nil, fmt.Errorf("failed to update limits: %w", err)
	}

	userCount, _ := c.DB.CountUsersBySubscriptionID(sub.ID.String())

	return &responses.AdminSubscriptionPlanResponse{
		ID:           sub.ID,
		Name:         sub.Name,
		MonthlyPrice: sub.MonthlyPrice,
		AnnualPrice:  sub.AnnualPrice,
		UserCount:    userCount,
		LimitsID:     sub.LimitsID,
		Limits:       limits,
		CreatedAt:    sub.CreatedAt,
	}, nil
}
