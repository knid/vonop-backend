package controllers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) ListAppleConnections(user *models.User) ([]responses.AppleConnectionResponse, error) {
	creds, err := c.DB.GetAppleConnectionsByUser(*user)
	if err != nil {
		return nil, err
	}

	var credsResponse []responses.AppleConnectionResponse
	for _, cred := range creds {
		credsResponse = append(credsResponse, responses.AppleConnectionResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			KeyID:     cred.KeyID,
			APNSKeyID: cred.APNSP8KeyID,
			TeamID:    cred.TeamID,
			IssuerID:  cred.IssuerID,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}

	return credsResponse, nil
}

func (c *Controller) GetAppleConnectionById(user *models.User, id string) (*responses.AppleConnectionResponse, error) {
	cred, err := c.DB.GetAppleConnectionById(id)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	return &responses.AppleConnectionResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		APNSKeyID: cred.APNSP8KeyID,
		TeamID:    cred.TeamID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil

}

func (c *Controller) CreateAppleConnection(user *models.User, orgID uuid.UUID, req requests.AppleConnectionCreateRequest) (*responses.AppleConnectionResponse, error) {
	credential := &models.AppleConnection{
		UserID:         user.ID,
		OrganizationID: orgID,
		Name:           req.Name,
		TeamID:         req.TeamID,
		P8KeyPath:      "p8_keys/" + req.P8Key + ".p8",
		APNSP8KeyPath:  "apns_keys/" + req.APNSP8Key + ".p8",
		APNSP8KeyID:    req.APNSKeyID,
		KeyID:          req.KeyID,
		IssuerID:       req.IssuerID,
	}
	cred, err := c.DB.CreateAppleConnection(credential)
	if err != nil {
		return nil, err
	}

	go c.ResumePendingDeployments(context.Background(), user, "ios", cred.ID)

	return &responses.AppleConnectionResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		TeamID:    cred.TeamID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateAppleConnection(user *models.User, credID string, req requests.AppleConnectionUpdateRequest) (*responses.AppleConnectionResponse, error) {
	cred, err := c.DB.GetAppleConnectionById(credID)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	p8KeyPath := cred.P8KeyPath
	if req.P8Key != "" {
		// upload new p8 key to storage
		p8KeyPath = req.P8Key
	}

	apnsP8KeyPath := cred.APNSP8KeyPath
	if req.APNSP8Key != "" {
		// upload new apns p8 key to storage
		apnsP8KeyPath = req.APNSP8Key
	}

	credentials := &models.AppleConnection{
		ID:            cred.ID,
		UserID:        user.ID,
		Name:          req.Name,
		P8KeyPath:     p8KeyPath,
		APNSP8KeyPath: apnsP8KeyPath,
		APNSP8KeyID:   req.APNSKeyID,
		KeyID:         req.KeyID,
		TeamID:        req.TeamID,
		IssuerID:      req.IssuerID,
	}

	cred, err = c.DB.UpdateAppleConnection(credentials)
	if err != nil {
		return nil, err
	}

	return &responses.AppleConnectionResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) DeleteAppleConnection(user *models.User, credID string) error {
	cred, err := c.DB.GetAppleConnectionById(credID)
	if err != nil {
		return err
	}

	if cred.UserID.String() != user.ID.String() {
		return errors.New("you do not have access to this resource")
	}

	return c.DB.DeleteAppleConnection(cred)
}

func (c *Controller) ListGooglePlayConnections(user *models.User) ([]responses.GooglePlayConnectionResponse, error) {

	creds, err := c.DB.GetGooglePlayConnectionsByUser(*user)
	if err != nil {
		return nil, err
	}

	var credsResponse []responses.GooglePlayConnectionResponse
	for _, cred := range creds {
		credsResponse = append(credsResponse, responses.GooglePlayConnectionResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}

	return credsResponse, nil
}

func (c *Controller) GetGooglePlayConnectionById(user *models.User, id string) (*responses.GooglePlayConnectionResponse, error) {
	cred, err := c.DB.GetGooglePlayConnectionById(id)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	return &responses.GooglePlayConnectionResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) CreateGooglePlayConnection(user *models.User, orgID uuid.UUID, req requests.GooglePlayConnectionCreateRequest) (*models.GooglePlayConnection, error) {
	credential := &models.GooglePlayConnection{
		UserID:             user.ID,
		OrganizationID:     orgID,
		Name:               req.Name,
		ServiceAccountPath: "service_accounts/" + req.ServiceAccountKey + ".json",
	}
	cred, err := c.DB.CreateGooglePlayConnection(credential)
	if err != nil {
		return nil, err
	}

	go c.ResumePendingDeployments(context.Background(), user, "android", cred.ID)

	return cred, nil
}

func (c *Controller) UpdateGooglePlayConnection(user *models.User, credID string, req requests.GooglePlayConnectionUpdateRequest) (*models.GooglePlayConnection, error) {
	cred, err := c.DB.GetGooglePlayConnectionById(credID)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access to this resource")
	}

	serviceAccountPath := cred.ServiceAccountPath
	if req.ServiceAccountKey != "" {
		// upload new service account key to storage
		serviceAccountPath = req.ServiceAccountKey
	}

	credentials := &models.GooglePlayConnection{
		ID:                 cred.ID,
		UserID:             user.ID,
		Name:               req.Name,
		ServiceAccountPath: serviceAccountPath,
	}
	return c.DB.UpdateGooglePlayConnection(credentials)
}

func (c *Controller) DeleteGooglePlayConnection(user *models.User, credID string) error {
	cred, err := c.DB.GetGooglePlayConnectionById(credID)
	if err != nil {
		return err
	}

	if cred.UserID.String() != user.ID.String() {
		return errors.New("you do not have access to this resource")
	}

	return c.DB.DeleteGooglePlayConnection(cred)
}
