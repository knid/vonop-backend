package controllers

import (
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/remoteconfig"
)

// GetUserLimits returns the user's plan limits with PostHog remote config overrides.
// DB values serve as fallback; PostHog flags (rc-plan-{name}-{field}) take precedence.
func (c *Controller) GetUserLimits(user *models.User) (*models.StandardUserLimits, error) {
	limits, _, err := c.GetUserLimitsWithPlan(user)
	return limits, err
}

// GetUserLimitsWithPlan is identical to GetUserLimits but also returns the subscription plan name.
func (c *Controller) GetUserLimitsWithPlan(user *models.User) (*models.StandardUserLimits, string, error) {
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		return nil, "", err
	}

	subs, err := c.DB.GetStandardUserSubscriptionByOrg(org)
	if err != nil {
		return nil, "", err
	}

	limits, err := c.DB.GetStandardUserLimitsById(subs.LimitsID.String())
	if err != nil {
		return nil, "", err
	}

	rc := c.RemoteConfig
	pn := strings.ToLower(subs.Name)

	limits.MaxDeployedWebApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-web-apps", limits.MaxDeployedWebApps), remoteconfig.Global)
	limits.MaxDeployedMobileApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-mobile-apps", limits.MaxDeployedMobileApps), remoteconfig.Global)
	limits.CanDeployMobile = rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-mobile", limits.CanDeployMobile), remoteconfig.Global)
	limits.MonthlyAICredits = rc.GetInt(remoteconfig.PlanInt(pn, "monthly-ai-credits", limits.MonthlyAICredits), remoteconfig.Global)
	limits.CreditRolloverMonths = rc.GetInt(remoteconfig.PlanInt(pn, "credit-rollover-months", limits.CreditRolloverMonths), remoteconfig.Global)
	limits.MaxWebApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-web-apps", limits.MaxWebApps), remoteconfig.Global)
	limits.MaxMobileApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-mobile-apps", limits.MaxMobileApps), remoteconfig.Global)
	limits.MontlyBuildCount = rc.GetInt(remoteconfig.PlanInt(pn, "montly-build-count", limits.MontlyBuildCount), remoteconfig.Global)
	limits.MontlyNotificationCount = rc.GetInt(remoteconfig.PlanInt(pn, "montly-notification-count", limits.MontlyNotificationCount), remoteconfig.Global)
	limits.CanBuildMobile = rc.GetBool(remoteconfig.PlanBool(pn, "can-build-mobile", limits.CanBuildMobile), remoteconfig.Global)
	limits.CanUseAnalytics = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-analytics", limits.CanUseAnalytics), remoteconfig.Global)
	limits.CanAccessCode = rc.GetBool(remoteconfig.PlanBool(pn, "can-access-code", limits.CanAccessCode), remoteconfig.Global)
	limits.CanBuyDomain = rc.GetBool(remoteconfig.PlanBool(pn, "can-buy-domain", limits.CanBuyDomain), remoteconfig.Global)
	limits.CanUseEmailService = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-email-service", limits.CanUseEmailService), remoteconfig.Global)
	limits.CanUseEcommerce = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-ecommerce", limits.CanUseEcommerce), remoteconfig.Global)
	limits.CanExportToGitHub = rc.GetBool(remoteconfig.PlanBool(pn, "can-export-to-github", limits.CanExportToGitHub), remoteconfig.Global)
	limits.CanDeployWeb = rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-web", limits.CanDeployWeb), remoteconfig.Global)
	limits.CanRemoveBrandLogo = rc.GetBool(remoteconfig.PlanBool(pn, "can-remove-brand-logo", limits.CanRemoveBrandLogo), remoteconfig.Global)

	return limits, subs.Name, nil
}

func (c *Controller) GetSubscription(user *models.User) (*responses.UserSubscriptionInformationResponse, error) {
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		return nil, err
	}

	subs, err := c.DB.GetStandardUserSubscriptionByOrg(org)
	if err != nil {
		return nil, err
	}

	// get limits (with remote config overrides)
	limits, err := c.GetUserLimits(user)
	if err != nil {
		return nil, err
	}

	limitsResponse := responses.LimitsResponse{
		ID:                      limits.ID,
		MaximumAccessIdCount:    limits.MaximumCredentialCount,
		MontlyBuildCount:        limits.MontlyBuildCount,
		MontlyNotificationCount: limits.MontlyNotificationCount,
		CanBuildToAndroid:       limits.CanBuildToAndroid,
		CanBuildToIOS:           limits.CanBuildToIOS,
		CanUseAnalytics:         limits.CanUseAnalytics,
		CanRemoveBrandLogo:      limits.CanRemoveBrandLogo,
		MaxDeployedWebApps:      limits.MaxDeployedWebApps,
		MaxDeployedMobileApps:   limits.MaxDeployedMobileApps,
		CanDeployMobile:         limits.CanDeployMobile,
		CreditRolloverMonths:    limits.CreditRolloverMonths,
		CanAccessCode:           limits.CanAccessCode,
		CanBuyDomain:            limits.CanBuyDomain,
		CanUseEmailService:      limits.CanUseEmailService,
		CanBuildMobile:          limits.CanBuildMobile,
		MonthlyAICredits:        limits.MonthlyAICredits,
		MaxWebApps:              limits.MaxWebApps,
		MaxMobileApps:           limits.MaxMobileApps,
		CanUseEcommerce:         limits.CanUseEcommerce,
		CanDeployWeb:            limits.CanDeployWeb,
		CanExportToGitHub:       limits.CanExportToGitHub,
	}

	return &responses.UserSubscriptionInformationResponse{
		ID:     subs.ID,
		Name:   subs.Name,
		Price:  subs.MonthlyPrice,
		Limits: limitsResponse,
	}, nil

}

func (c *Controller) UpdateSubscription(user *models.User, subscriptionId string) error {
	subs, err := c.DB.GetSubscriptionById(subscriptionId)
	if err != nil {
		return err
	}

	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		return err
	}

	_, err = c.DB.SetStandardUserSubscription(org, subs)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) CancelSubscription(user *models.User) error {
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		return err
	}

	_, err = c.DB.DeleteStandardUserSubscription(org)
	if err != nil {
		return err
	}

	// Mark the user's personal org as canceled (Plan 5 — billing on org).
	org.SubscriptionStatus = "canceled"
	if _, err := c.DB.UpdateOrganization(org); err != nil {
		slog.Error("failed to mark org subscription canceled", "error", err, "org_id", org.ID)
	}

	// Legacy dual-write to user (cleanup later).
	user.SubscriptionStatus = "canceled"
	_, err = c.DB.UpdateUser(user)
	return err
}
