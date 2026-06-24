package controllers

import (
	"log/slog"
	"strings"

	"github.com/google/uuid"
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

// PlanMonthlyAICredits returns a plan's monthly AI credit grant, applying the
// same remote-config override (rc-plan-{name}-monthly-ai-credits) that
// GetUserLimits uses. Returns 0 when the plan or its limits row cannot be
// resolved — callers in the webhook path treat 0 as "no grant".
func (c *Controller) PlanMonthlyAICredits(subs *models.StandardUserSubscription) int {
	if subs == nil {
		return 0
	}
	limits, err := c.DB.GetStandardUserLimitsById(subs.LimitsID.String())
	if err != nil {
		return 0
	}
	pn := strings.ToLower(subs.Name)
	return c.RemoteConfig.GetInt(remoteconfig.PlanInt(pn, "monthly-ai-credits", limits.MonthlyAICredits), remoteconfig.Global)
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

	c.applyPlanRemoteConfigOverrides(limits, subs.Name)

	return limits, subs.Name, nil
}

// GetOrgLimits returns the organization's plan limits with remote config overrides.
func (c *Controller) GetOrgLimits(orgID string) (*models.StandardUserLimits, error) {
	limits, _, err := c.GetOrgLimitsWithPlan(orgID)
	return limits, err
}

// GetOrgLimitsWithPlan is identical to GetOrgLimits but also returns the subscription plan name.
func (c *Controller) GetOrgLimitsWithPlan(orgID string) (*models.StandardUserLimits, string, error) {
	org, err := c.DB.GetOrganizationByID(orgID)
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

	c.applyPlanRemoteConfigOverrides(limits, subs.Name)
	return limits, subs.Name, nil
}

func (c *Controller) applyPlanRemoteConfigOverrides(limits *models.StandardUserLimits, planName string) {
	rc := c.RemoteConfig
	pn := strings.ToLower(planName)

	limits.MaxDeployedWebApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-web-apps", limits.MaxDeployedWebApps), remoteconfig.Global)
	limits.MaxDeployedMobileApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-deployed-mobile-apps", limits.MaxDeployedMobileApps), remoteconfig.Global)
	limits.CanDeployMobile = rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-mobile", limits.CanDeployMobile), remoteconfig.Global)
	limits.MonthlyAICredits = rc.GetInt(remoteconfig.PlanInt(pn, "monthly-ai-credits", limits.MonthlyAICredits), remoteconfig.Global)
	limits.CreditRolloverMonths = rc.GetInt(remoteconfig.PlanInt(pn, "credit-rollover-months", limits.CreditRolloverMonths), remoteconfig.Global)
	limits.MaxWebApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-web-apps", limits.MaxWebApps), remoteconfig.Global)
	limits.MaxMobileApps = rc.GetInt(remoteconfig.PlanInt(pn, "max-mobile-apps", limits.MaxMobileApps), remoteconfig.Global)
	limits.MontlyNotificationCount = rc.GetInt(remoteconfig.PlanInt(pn, "montly-notification-count", limits.MontlyNotificationCount), remoteconfig.Global)
	limits.CanUseAnalytics = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-analytics", limits.CanUseAnalytics), remoteconfig.Global)
	limits.CanAccessCode = rc.GetBool(remoteconfig.PlanBool(pn, "can-access-code", limits.CanAccessCode), remoteconfig.Global)
	limits.CanBuyDomain = rc.GetBool(remoteconfig.PlanBool(pn, "can-buy-domain", limits.CanBuyDomain), remoteconfig.Global)
	limits.CanUseEmailService = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-email-service", limits.CanUseEmailService), remoteconfig.Global)
	limits.CanUseEcommerce = rc.GetBool(remoteconfig.PlanBool(pn, "can-use-ecommerce", limits.CanUseEcommerce), remoteconfig.Global)
	limits.CanExportToGitHub = rc.GetBool(remoteconfig.PlanBool(pn, "can-export-to-github", limits.CanExportToGitHub), remoteconfig.Global)
	limits.CanDeployWeb = rc.GetBool(remoteconfig.PlanBool(pn, "can-deploy-web", limits.CanDeployWeb), remoteconfig.Global)
	limits.CanRemoveBrandLogo = rc.GetBool(remoteconfig.PlanBool(pn, "can-remove-brand-logo", limits.CanRemoveBrandLogo), remoteconfig.Global)
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
		ID:                        limits.ID,
		MaximumAccessIdCount:      limits.MaximumCredentialCount,
		MontlyNotificationCount:   limits.MontlyNotificationCount,
		CanUseAnalytics:           limits.CanUseAnalytics,
		CanRemoveBrandLogo:        limits.CanRemoveBrandLogo,
		MaxDeployedWebApps:        limits.MaxDeployedWebApps,
		MaxDeployedMobileApps:     limits.MaxDeployedMobileApps,
		CanDeployMobile:           limits.CanDeployMobile,
		CreditRolloverMonths:      limits.CreditRolloverMonths,
		CanAccessCode:             limits.CanAccessCode,
		CanBuyDomain:              limits.CanBuyDomain,
		CanUseEmailService:        limits.CanUseEmailService,
		MonthlyAICredits:          limits.MonthlyAICredits,
		MaxWebApps:                limits.MaxWebApps,
		MaxMobileApps:             limits.MaxMobileApps,
		CanUseEcommerce:           limits.CanUseEcommerce,
		CanDeployWeb:              limits.CanDeployWeb,
		CanExportToGitHub:         limits.CanExportToGitHub,
		DeployMemoryMB:            limits.DeployMemoryMB,
		DeployCPUs:                limits.DeployCPUs,
		DeployDBMaxCU:             limits.DeployDBMaxCU,
		DeployDBStorageMB:         limits.DeployDBStorageMB,
		DeployDBActiveTimeSec:     limits.DeployDBActiveTimeSec,
		DeployDBDataTransferBytes: limits.DeployDBDataTransferBytes,
		DeployDBComputeTimeSec:    limits.DeployDBComputeTimeSec,
		DeployDBSuspendTimeoutSec: limits.DeployDBSuspendTimeoutSec,
		DeployDBOrgTier:           limits.DeployDBOrgTier,
		MaxSeats:                  limits.MaxSeats,
	}

	return &responses.UserSubscriptionInformationResponse{
		ID:     subs.ID,
		Name:   subs.Name,
		Price:  subs.MonthlyPrice,
		Limits: limitsResponse,
	}, nil

}

func (c *Controller) UpdateSubscription(user *models.User, orgID *uuid.UUID, subscriptionId string) error {
	subs, err := c.DB.GetSubscriptionById(subscriptionId)
	if err != nil {
		return err
	}

	resolvedOrgID, err := c.RequireOrgIDForUser(user, orgID)
	if err != nil {
		return err
	}

	org, err := c.DB.GetOrganizationByID(resolvedOrgID.String())
	if err != nil {
		return err
	}

	_, err = c.DB.SetStandardUserSubscription(org, subs)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) CancelSubscription(user *models.User, orgID *uuid.UUID) error {
	resolvedOrgID, err := c.RequireOrgIDForUser(user, orgID)
	if err != nil {
		return err
	}

	org, err := c.DB.GetOrganizationByID(resolvedOrgID.String())
	if err != nil {
		return err
	}

	_, err = c.DB.DeleteStandardUserSubscription(org)
	if err != nil {
		return err
	}

	org.SubscriptionStatus = "canceled"
	if _, err := c.DB.UpdateOrganization(org); err != nil {
		slog.Error("failed to mark org subscription canceled", "error", err, "org_id", org.ID)
	}

	// Legacy dual-write to user (cleanup later).
	if org.IsPersonal {
		user.SubscriptionStatus = "canceled"
		if _, err := c.DB.UpdateUser(user); err != nil {
			return err
		}
	}
	return nil
}
