package controllers

import (
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetSubscription(user *models.User) (*responses.UserSubscriptionInformationResponse, error) {
	subs, err := c.DB.GetStandartUserSubscriptionByUser(user)
	if err != nil {
		return nil, err
	}

	// get limits
	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		return nil, err
	}

	limitsResponse := responses.LimitsResponse{
		ID:                      limits.ID,
		MaximumProjectCount:     limits.MaximumProjectCount,
		MontlyBuildCount:        limits.MontlyBuildCount,
		MontlyNotificationCount: limits.MontlyNotificationCount,
		CanBuildToAndroid:       limits.CanBuildToAndroid,
		CanBuildToIOS:           limits.CanBuildToIOS,
		CanUseAnalytics:         limits.CanUseAnalytics,
		CanRemoveBrandLogo:      limits.CanRemoveBrandLogo,
		CanSeeActiveUsers:       limits.CanSeeActiveUsers,
		MaxDeployedWebApps:      limits.MaxDeployedWebApps,
		CreditRolloverMonths:    limits.CreditRolloverMonths,
		CanAccessCode:           limits.CanAccessCode,
		CanBuyDomain:            limits.CanBuyDomain,
		CanUseEmailService:      limits.CanUseEmailService,
		CanBuildMobile:          limits.CanBuildMobile,
		MonthlyAICredits:        limits.MonthlyAICredits,
		MonthlyAppCount:         limits.MonthlyAppCount,
		MonthlyDeployCount:      limits.MonthlyDeployCount,
		CanUseEcommerce:         limits.CanUseEcommerce,
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

	_, err = c.DB.SetStandartUserSubscription(user, subs)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) CancelSubscription(user *models.User) error {
	_, err := c.DB.DeleteStandartUserSubscription(user)
	if err != nil {
		return err
	}

	user.SubscriptionStatus = "canceled"
	_, err = c.DB.UpdateUser(user)
	return err
}
