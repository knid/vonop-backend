package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// applyP8Prefix turns a bare UUID into the canonical S3 key for an Apple
// .p8 file. Pass-through if already prefixed. Empty in → empty out.
//
// The .p8 lands in the user-keys bucket via PutP8KeyToS3Presign as a
// staging artifact. encryptAndStoreCredential reads from this path, encrypts
// via CredStore, and returns a CredRef the connection row stores. The legacy
// plaintext-pointer column was dropped 2026-05-07 — this prefix only exists
// to derive the staging path on read-back.
func applyP8Prefix(s string) string {
	if s == "" || strings.HasPrefix(s, "p8_keys/") {
		return s
	}
	return "p8_keys/" + s + ".p8"
}

// applyServiceAccountPrefix is for Google Play service-account JSONs.
func applyServiceAccountPrefix(s string) string {
	if s == "" || strings.HasPrefix(s, "service_accounts/") {
		return s
	}
	return "service_accounts/" + s + ".json"
}

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
		TeamID:    cred.TeamID,
		IssuerID:  cred.IssuerID,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil

}

func (c *Controller) CreateAppleConnection(user *models.User, orgID uuid.UUID, req requests.AppleConnectionCreateRequest) (*responses.AppleConnectionResponse, error) {
	// Encrypt the just-uploaded staging blob up front. If encryption fails the
	// row never lands so we don't end up with a connection whose credential
	// can't be read back.
	ctx := context.Background()
	p8StagingPath := applyP8Prefix(req.P8Key)
	p8Ref, err := c.encryptAndStoreCredential(ctx, orgID, "apple-p8", p8StagingPath)
	if err != nil {
		return nil, fmt.Errorf("encrypt p8: %w", err)
	}
	if p8Ref == nil {
		return nil, errors.New("encrypt p8: empty staging path")
	}

	credential := &models.AppleConnection{
		UserID:         user.ID,
		OrganizationID: orgID,
		Name:           req.Name,
		TeamID:         req.TeamID,
		KeyID:          req.KeyID,
		IssuerID:       req.IssuerID,
		P8KeyCredRef:   p8Ref,
	}
	cred, err := c.DB.CreateAppleConnection(credential)
	if err != nil {
		return nil, err
	}

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

	// If the user rotated the .p8, encrypt the new staging blob first; only
	// commit the row update once the new ref is in hand.
	ctx := context.Background()
	p8Ref := cred.P8KeyCredRef
	if req.P8Key != "" {
		newRef, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "apple-p8", applyP8Prefix(req.P8Key))
		if err != nil {
			return nil, fmt.Errorf("encrypt p8: %w", err)
		}
		if newRef != nil {
			p8Ref = newRef
		}
	}

	credentials := &models.AppleConnection{
		ID:             cred.ID,
		UserID:         user.ID,
		OrganizationID: cred.OrganizationID, // preserved — Bun does whole-row update
		Name:           req.Name,
		KeyID:          req.KeyID,
		TeamID:         req.TeamID,
		IssuerID:       req.IssuerID,
		P8KeyCredRef:   p8Ref,
		CreatedAt:      cred.CreatedAt, // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:      time.Now(),     // bump on every update
	}

	saved, err := c.DB.UpdateAppleConnection(credentials)
	if err != nil {
		return nil, err
	}

	return &responses.AppleConnectionResponse{
		ID:        saved.ID,
		Name:      saved.Name,
		KeyID:     saved.KeyID,
		TeamID:    saved.TeamID,
		IssuerID:  saved.IssuerID,
		CreatedAt: saved.CreatedAt,
		UpdatedAt: saved.UpdatedAt,
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
	// Encrypt up front so the row only lands once the credential is durable
	// in CredStore.
	ctx := context.Background()
	saStagingPath := applyServiceAccountPrefix(req.ServiceAccountKey)
	saRef, err := c.encryptAndStoreCredential(ctx, orgID, "google-sa", saStagingPath)
	if err != nil {
		return nil, fmt.Errorf("encrypt service account: %w", err)
	}
	if saRef == nil {
		return nil, errors.New("encrypt service account: empty staging path")
	}

	credential := &models.GooglePlayConnection{
		UserID:                user.ID,
		OrganizationID:        orgID,
		Name:                  req.Name,
		ServiceAccountCredRef: saRef,
	}
	cred, err := c.DB.CreateGooglePlayConnection(credential)
	if err != nil {
		return nil, err
	}
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

	ctx := context.Background()
	saRef := cred.ServiceAccountCredRef
	if req.ServiceAccountKey != "" {
		newRef, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "google-sa", applyServiceAccountPrefix(req.ServiceAccountKey))
		if err != nil {
			return nil, fmt.Errorf("encrypt service account: %w", err)
		}
		if newRef != nil {
			saRef = newRef
		}
	}

	credentials := &models.GooglePlayConnection{
		ID:                    cred.ID,
		UserID:                user.ID,
		OrganizationID:        cred.OrganizationID, // preserved — Bun does whole-row update
		Name:                  req.Name,
		ServiceAccountCredRef: saRef,
		CreatedAt:             cred.CreatedAt, // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:             time.Now(),     // bump on every update
	}
	saved, err := c.DB.UpdateGooglePlayConnection(credentials)
	if err != nil {
		return nil, err
	}

	return saved, nil
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

// ---- Org-scoped credential methods ----
//
// These replace the user-scoped methods above for HTTP handler use.
// The user-scoped methods are kept for system callers (cron) that don't
// carry a user context.

// GetAppleConnectionForOrg loads an Apple connection scoped to the given org.
// The requesting user must be a member of orgID (any role).
func (c *Controller) GetAppleConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string) (*responses.AppleConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}
	cred, err := c.DB.GetAppleConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("not found")
	}
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

// ListAppleConnectionsForOrg returns all Apple connections owned by an org.
// Requesting user must be an org member.
func (c *Controller) ListAppleConnectionsForOrg(ctx context.Context, requestingUser *models.User, orgIDStr string) ([]responses.AppleConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	creds, err := c.DB.ListAppleConnectionsByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]responses.AppleConnectionResponse, 0, len(creds))
	for _, cred := range creds {
		out = append(out, responses.AppleConnectionResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			KeyID:     cred.KeyID,
			TeamID:    cred.TeamID,
			IssuerID:  cred.IssuerID,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}
	return out, nil
}

// UpdateAppleConnectionForOrg updates an Apple connection. Requesting user
// must be an org member; the original UserID (creator) is preserved.
func (c *Controller) UpdateAppleConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string, req requests.AppleConnectionUpdateRequest) (*responses.AppleConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}
	cred, err := c.DB.GetAppleConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("not found")
	}

	p8Ref := cred.P8KeyCredRef
	if req.P8Key != "" {
		newRef, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "apple-p8", applyP8Prefix(req.P8Key))
		if err != nil {
			return nil, fmt.Errorf("encrypt p8: %w", err)
		}
		if newRef != nil {
			p8Ref = newRef
		}
	}

	updated := &models.AppleConnection{
		ID:             cred.ID,
		UserID:         cred.UserID,         // preserved — do not overwrite with requesting user
		OrganizationID: cred.OrganizationID, // preserved
		Name:           req.Name,
		KeyID:          req.KeyID,
		TeamID:         req.TeamID,
		IssuerID:       req.IssuerID,
		P8KeyCredRef:   p8Ref,
		CreatedAt:      cred.CreatedAt, // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:      time.Now(),     // bump on every update
	}
	saved, err := c.DB.UpdateAppleConnection(updated)
	if err != nil {
		return nil, err
	}

	return &responses.AppleConnectionResponse{
		ID:        saved.ID,
		Name:      saved.Name,
		KeyID:     saved.KeyID,
		TeamID:    saved.TeamID,
		IssuerID:  saved.IssuerID,
		CreatedAt: saved.CreatedAt,
		UpdatedAt: saved.UpdatedAt,
	}, nil
}

// DeleteAppleConnectionForOrg deletes an Apple connection. Requesting user
// must be an org member.
func (c *Controller) DeleteAppleConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string) error {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return errors.New("invalid credential id")
	}
	cred, err := c.DB.GetAppleConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return err
	}
	if cred == nil {
		return errors.New("not found")
	}
	return c.DB.DeleteAppleConnection(cred)
}

// GetGooglePlayConnectionForOrg loads a Google Play connection scoped to the
// given org. The requesting user must be an org member (any role).
func (c *Controller) GetGooglePlayConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string) (*responses.GooglePlayConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}
	cred, err := c.DB.GetGooglePlayConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("not found")
	}
	return &responses.GooglePlayConnectionResponse{
		ID:        cred.ID,
		Name:      cred.Name,
		CreatedAt: cred.CreatedAt,
		UpdatedAt: cred.UpdatedAt,
	}, nil
}

// ListGooglePlayConnectionsForOrg returns all Google Play connections owned by
// an org. Requesting user must be an org member.
func (c *Controller) ListGooglePlayConnectionsForOrg(ctx context.Context, requestingUser *models.User, orgIDStr string) ([]responses.GooglePlayConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	creds, err := c.DB.ListGooglePlayConnectionsByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]responses.GooglePlayConnectionResponse, 0, len(creds))
	for _, cred := range creds {
		out = append(out, responses.GooglePlayConnectionResponse{
			ID:        cred.ID,
			Name:      cred.Name,
			CreatedAt: cred.CreatedAt,
			UpdatedAt: cred.UpdatedAt,
		})
	}
	return out, nil
}

// UpdateGooglePlayConnectionForOrg updates a Google Play connection. Requesting
// user must be an org member; the original UserID (creator) is preserved.
func (c *Controller) UpdateGooglePlayConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string, req requests.GooglePlayConnectionUpdateRequest) (*responses.GooglePlayConnectionResponse, error) {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return nil, err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return nil, errors.New("invalid credential id")
	}
	cred, err := c.DB.GetGooglePlayConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, errors.New("not found")
	}

	saRef := cred.ServiceAccountCredRef
	if req.ServiceAccountKey != "" {
		newRef, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "google-sa", applyServiceAccountPrefix(req.ServiceAccountKey))
		if err != nil {
			return nil, fmt.Errorf("encrypt service account: %w", err)
		}
		if newRef != nil {
			saRef = newRef
		}
	}

	updated := &models.GooglePlayConnection{
		ID:                    cred.ID,
		UserID:                cred.UserID,         // preserved — do not overwrite with requesting user
		OrganizationID:        cred.OrganizationID, // preserved
		Name:                  req.Name,
		ServiceAccountCredRef: saRef,
		CreatedAt:             cred.CreatedAt, // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:             time.Now(),     // bump on every update
	}
	saved, err := c.DB.UpdateGooglePlayConnection(updated)
	if err != nil {
		return nil, err
	}

	return &responses.GooglePlayConnectionResponse{
		ID:        saved.ID,
		Name:      saved.Name,
		CreatedAt: saved.CreatedAt,
		UpdatedAt: saved.UpdatedAt,
	}, nil
}

// DeleteGooglePlayConnectionForOrg deletes a Google Play connection. Requesting
// user must be an org member.
func (c *Controller) DeleteGooglePlayConnectionForOrg(ctx context.Context, requestingUser *models.User, orgIDStr, credID string) error {
	orgID, err := c.RequireOrgMembership(requestingUser, orgIDStr)
	if err != nil {
		return err
	}
	credUUID, err := uuid.Parse(credID)
	if err != nil {
		return errors.New("invalid credential id")
	}
	cred, err := c.DB.GetGooglePlayConnectionByOrg(ctx, orgID, credUUID)
	if err != nil {
		return err
	}
	if cred == nil {
		return errors.New("not found")
	}
	return c.DB.DeleteGooglePlayConnection(cred)
}
