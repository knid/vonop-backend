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

	return &responses.UserSubscriptionInformationResponse{
		ID:     subs.ID,
		Name:   subs.Name,
		Price:  subs.MonthlyPrice,
		Limits: *limits,
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

	return nil
}
