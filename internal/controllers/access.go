package controllers

import (
	"errors"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) CreateAndroidStoreAccess(user *models.User, createAndroidStoreAccessReq requests.AndroidStoreAccessRequest) (*responses.SuccessResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.AndroidStoreAccessRequest](createAndroidStoreAccessReq, validate); err != nil {
		return nil, err
	}

	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		return nil, err
	}

	androidStoreAccessList, err := c.DB.GetAndroidStoreAccessByUser(user.ID.String())
	if err != nil {
		return nil, err
	}

	iosStoreAccessList, err := c.DB.GetIOSStoreAccessByUser(user.ID.String())
	if err != nil {
		return nil, err
	}

	if len(androidStoreAccessList)+len(iosStoreAccessList) >= limits.MaximumCredentialCount {
		return nil, errors.New("android store access limit reached")
	}

	androidStoreAccess := &models.AndroidStoreAccess{
		UserID:                user.ID,
		Name:                  createAndroidStoreAccessReq.Name,
		ServiceAccountKeyHash: utils.GetSHA256Hash(createAndroidStoreAccessReq.ServiceAccountKey),
	}

	_, err = c.DB.CreateAndroidStoreAccess(androidStoreAccess)
	if err != nil {
		return nil, err
	}

	return &responses.SuccessResponse{
		Success: true,
	}, nil
}

func (c *Controller) GetAndroidStoreAccessById(user *models.User, accessId string) (*models.AndroidStoreAccess, error) {
	access, err := c.DB.GetAndroidStoreAccessById(accessId)
	if err != nil {
		return nil, err
	}

	if access.UserID != user.ID {
		return nil, errors.New("access not found")
	}

	return access, nil
}

func (c *Controller) DeleteAndroidStoreAccess(user *models.User, accessId string) (*responses.SuccessResponse, error) {
	access, err := c.DB.GetAndroidStoreAccessById(accessId)
	if err != nil {
		return nil, err
	}

	if access.UserID != user.ID {
		return nil, errors.New("access not found")
	}

	err = c.DB.DeleteAndroidStoreAccess(access)
	if err != nil {
		return nil, err
	}

	return &responses.SuccessResponse{
		Success: true,
	}, nil
}

func (c *Controller) CreateIOSStoreAccess(user *models.User, createIOSStoreAccessReq requests.IOSStoreAccessRequest) (*responses.SuccessResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.IOSStoreAccessRequest](createIOSStoreAccessReq, validate); err != nil {
		return nil, err
	}

	limits, err := c.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		return nil, err
	}

	iosStoreAccessList, err := c.DB.GetIOSStoreAccessByUser(user.ID.String())
	if err != nil {
		return nil, err
	}

	androidStoreAccessList, err := c.DB.GetAndroidStoreAccessByUser(user.ID.String())
	if err != nil {
		return nil, err
	}

	if len(iosStoreAccessList)+len(androidStoreAccessList) >= limits.MaximumCredentialCount {
		return nil, errors.New("ios store access limit reached")
	}

	iosStoreAccess := &models.IOSStoreAccess{
		UserID:           user.ID,
		Name:             createIOSStoreAccessReq.Name,
		TeamID:           createIOSStoreAccessReq.TeamID,
		IssuerID:         createIOSStoreAccessReq.IssuerID,
		PrivateP8KeyHash: utils.GetSHA256Hash(createIOSStoreAccessReq.PrivateP8Key),
	}

	_, err = c.DB.CreateIOSStoreAccess(iosStoreAccess)
	if err != nil {
		return nil, err
	}

	return &responses.SuccessResponse{
		Success: true,
	}, nil
}

func (c *Controller) GetIOSStoreAccessById(user *models.User, accessId string) (*models.IOSStoreAccess, error) {
	access, err := c.DB.GetIOSStoreAccessById(accessId)
	if err != nil {
		return nil, err
	}

	if access.UserID != user.ID {
		return nil, errors.New("access not found")
	}

	return access, nil
}

func (c *Controller) DeleteIOSStoreAccess(user *models.User, accessId string) (*responses.SuccessResponse, error) {
	access, err := c.DB.GetIOSStoreAccessById(accessId)
	if err != nil {
		return nil, err
	}

	if access.UserID != user.ID {
		return nil, errors.New("access not found")
	}

	err = c.DB.DeleteIOSStoreAccess(access)
	if err != nil {
		return nil, err
	}

	return &responses.SuccessResponse{
		Success: true,
	}, nil
}
