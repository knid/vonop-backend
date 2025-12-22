package controllers

import (
	"errors"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

func (c *Controller) GetAppStoreCredentials(user *models.User) ([]responses.AppStoreCredentialsResponse, error) {
	creds, err := c.DB.GetAppStoreCredentialsByUser(*user)
	if err != nil {
		return nil, err
	}

	var credsResponse []responses.AppStoreCredentialsResponse
	for _, cred := range creds {
		credsResponse = append(credsResponse, responses.AppStoreCredentialsResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			KeyID:     cred.KeyID,
			TeamID:    cred.TeamID,
			IssuerID:  cred.IssuerID,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}

	return credsResponse, nil
}

func (c *Controller) GetAppStoreCredentialById(user *models.User, id string) (*responses.AppStoreCredentialsResponse, error) {
	cred, err := c.DB.GetAppStoreCredentialById(id)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	return &responses.AppStoreCredentialsResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		TeamID:    cred.TeamID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil

}

func (c *Controller) CreateAppStoreCredential(user *models.User, credentialCreateReq requests.AppStoreCredentialsCreateRequest) (*responses.AppStoreCredentialsResponse, error) {
	credential := &models.AppStoreCredentials{
		UserID:        user.ID,
		Name:          credentialCreateReq.Name,
		TeamID:        credentialCreateReq.TeamID,
		P8KeyPath:     "p8_keys/" + credentialCreateReq.P8Key + ".p8",
		APNSP8KeyPath: "apns_keys/" + credentialCreateReq.APNSP8Key + ".p8",
		KeyID:         credentialCreateReq.KeyID,
		IssuerID:      credentialCreateReq.IssuerID,
	}
	cred, err := c.DB.CreateAppStoreCredentials(credential)
	if err != nil {
		return nil, err
	}

	return &responses.AppStoreCredentialsResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		TeamID:    cred.TeamID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) UpdateAppStoreCredential(user *models.User, credID string, credentialCreateReq requests.AppStoreCredentialsCreateRequest) (*responses.AppStoreCredentialsResponse, error) {
	cred, err := c.DB.GetAppStoreCredentialById(credID)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	p8KeyPath := cred.P8KeyPath
	if credentialCreateReq.P8Key != "" {
		// upload new p8 key to storage
		p8KeyPath = credentialCreateReq.P8Key
	}

	apnsP8KeyPath := cred.APNSP8KeyPath
	if credentialCreateReq.APNSP8Key != "" {
		// upload new apns p8 key to storage
		apnsP8KeyPath = credentialCreateReq.APNSP8Key
	}

	credentials := &models.AppStoreCredentials{
		ID:            cred.ID,
		UserID:        user.ID,
		Name:          credentialCreateReq.Name,
		P8KeyPath:     p8KeyPath,
		APNSP8KeyPath: apnsP8KeyPath,
		KeyID:         credentialCreateReq.KeyID,
		TeamID:        credentialCreateReq.TeamID,
		IssuerID:      credentialCreateReq.IssuerID,
	}

	cred, err = c.DB.UpdateAppStoreCredentials(credentials)
	if err != nil {
		return nil, err
	}

	return &responses.AppStoreCredentialsResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		KeyID:     cred.KeyID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) DeleteAppStoreCredential(user *models.User, credID string) error {
	cred, err := c.DB.GetAppStoreCredentialById(credID)
	if err != nil {
		return err
	}

	if cred.UserID.String() != user.ID.String() {
		return errors.New("you do not have access this resources")
	}

	return c.DB.DeleteAppStoreCredentials(cred)
}

func (c *Controller) GetGooglePlayCredentials(user *models.User) ([]responses.GooglePlayCredentialsResponse, error) {

	creds, err := c.DB.GetGooglePlayCredentialsByUser(*user)
	if err != nil {
		return nil, err
	}

	var credsResponse []responses.GooglePlayCredentialsResponse
	for _, cred := range creds {
		credsResponse = append(credsResponse, responses.GooglePlayCredentialsResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}

	return credsResponse, nil
}

func (c *Controller) GetGooglePlayCredentialById(user *models.User, id string) (*responses.GooglePlayCredentialsResponse, error) {
	cred, err := c.DB.GetGooglePlayCredentialById(id)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	return &responses.GooglePlayCredentialsResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

func (c *Controller) CreateGooglePlayCredential(user *models.User, credentialCreateReq requests.GooglePlayCredentialsCreateRequest) (*models.GooglePlayCredentials, error) {
	credential := &models.GooglePlayCredentials{
		UserID:             user.ID,
		Name:               credentialCreateReq.Name,
		ServiceAccountPath: "service_accounts/" + credentialCreateReq.ServiceAccountKey + ".json",
	}
	return c.DB.CreateGooglePlayCredentials(credential)
}

func (c *Controller) UpdateGooglePlayCredential(user *models.User, credID string, credentialCreateReq requests.GooglePlayCredentialsCreateRequest) (*models.GooglePlayCredentials, error) {
	cred, err := c.DB.GetGooglePlayCredentialById(credID)
	if err != nil {
		return nil, err
	}

	if cred.UserID.String() != user.ID.String() {
		return nil, errors.New("you do not have access this resources")
	}

	serviceAccountPath := cred.ServiceAccountPath
	if credentialCreateReq.ServiceAccountKey != "" {
		// upload new service account key to storage
		serviceAccountPath = credentialCreateReq.ServiceAccountKey
	}

	credentials := &models.GooglePlayCredentials{
		ID:                 cred.ID,
		UserID:             user.ID,
		Name:               credentialCreateReq.Name,
		ServiceAccountPath: serviceAccountPath,
	}
	return c.DB.UpdateGooglePlayCredentials(credentials)
}

func (c *Controller) DeleteGooglePlayCredential(user *models.User, credID string) error {
	cred, err := c.DB.GetGooglePlayCredentialById(credID)
	if err != nil {
		return err
	}

	if cred.UserID.String() != user.ID.String() {
		return errors.New("you do not have access this resources")
	}

	return c.DB.DeleteGooglePlayCredentials(cred)
}
