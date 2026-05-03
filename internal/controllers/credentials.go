package controllers

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
)

// applyP8Prefix turns a bare UUID into the canonical S3 key for an Apple
// .p8 file. Pass-through if already prefixed. Empty in → empty out.
func applyP8Prefix(s string) string {
	if s == "" || strings.HasPrefix(s, "p8_keys/") {
		return s
	}
	return "p8_keys/" + s + ".p8"
}

// applyAPNSP8Prefix is the APNs .p8 key equivalent.
func applyAPNSP8Prefix(s string) string {
	if s == "" || strings.HasPrefix(s, "apns_keys/") {
		return s
	}
	return "apns_keys/" + s + ".p8"
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
		P8KeyPath:      applyP8Prefix(req.P8Key),
		APNSP8KeyPath:  applyAPNSP8Prefix(req.APNSP8Key),
		APNSP8KeyID:    req.APNSKeyID,
		KeyID:          req.KeyID,
		IssuerID:       req.IssuerID,
	}
	cred, err := c.DB.CreateAppleConnection(credential)
	if err != nil {
		return nil, err
	}

	// Encrypt at rest via CredStore. Legacy plaintext path stays for dual-read
	// during the transition; backfillEncryptCredentials handles pre-existing rows.
	ctx := context.Background()
	if ref, err := c.encryptAndStoreCredential(ctx, orgID, "apple-p8", cred.P8KeyPath); err != nil {
		slog.Warn("CreateAppleConnection: p8 encrypt failed (legacy path stays usable)", "conn", cred.ID, "err", err)
	} else if ref != nil {
		cred.P8KeyCredRef = ref
		if _, err := c.DB.UpdateAppleConnection(cred); err != nil {
			slog.Warn("CreateAppleConnection: persist p8 ref failed", "conn", cred.ID, "err", err)
		}
	}
	if ref, err := c.encryptAndStoreCredential(ctx, orgID, "apple-apns", cred.APNSP8KeyPath); err != nil {
		slog.Warn("CreateAppleConnection: apns encrypt failed", "conn", cred.ID, "err", err)
	} else if ref != nil {
		cred.APNSP8KeyCredRef = ref
		if _, err := c.DB.UpdateAppleConnection(cred); err != nil {
			slog.Warn("CreateAppleConnection: persist apns ref failed", "conn", cred.ID, "err", err)
		}
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
		p8KeyPath = applyP8Prefix(req.P8Key)
	}

	apnsP8KeyPath := cred.APNSP8KeyPath
	if req.APNSP8Key != "" {
		// upload new apns p8 key to storage
		apnsP8KeyPath = applyAPNSP8Prefix(req.APNSP8Key)
	}

	credentials := &models.AppleConnection{
		ID:               cred.ID,
		UserID:           user.ID,
		OrganizationID:   cred.OrganizationID,   // preserved — Bun does whole-row update
		Name:             req.Name,
		P8KeyPath:        p8KeyPath,
		APNSP8KeyPath:    apnsP8KeyPath,
		APNSP8KeyID:      req.APNSKeyID,
		KeyID:            req.KeyID,
		TeamID:           req.TeamID,
		IssuerID:         req.IssuerID,
		P8KeyCredRef:     cred.P8KeyCredRef,     // preserved; replaced below if key rotated
		APNSP8KeyCredRef: cred.APNSP8KeyCredRef, // preserved; replaced below if key rotated
		CreatedAt:        cred.CreatedAt,        // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:        time.Now(),            // bump on every update
	}

	cred, err = c.DB.UpdateAppleConnection(credentials)
	if err != nil {
		return nil, err
	}

	// Dual-write: when the user rotated a key, sweep the new plaintext through
	// CredStore and persist a fresh CredRef. Legacy path remains source of truth
	// until backfill + dual-read phase-out.
	ctx := context.Background()
	if req.P8Key != "" {
		if ref, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "apple-p8", cred.P8KeyPath); err != nil {
			slog.Warn("UpdateAppleConnection: p8 encrypt failed", "conn", cred.ID, "err", err)
		} else if ref != nil {
			cred.P8KeyCredRef = ref
			if _, err := c.DB.UpdateAppleConnection(cred); err != nil {
				slog.Warn("UpdateAppleConnection: persist p8 ref failed", "conn", cred.ID, "err", err)
			}
		}
	}
	if req.APNSP8Key != "" {
		if ref, err := c.encryptAndStoreCredential(ctx, cred.OrganizationID, "apple-apns", cred.APNSP8KeyPath); err != nil {
			slog.Warn("UpdateAppleConnection: apns encrypt failed", "conn", cred.ID, "err", err)
		} else if ref != nil {
			cred.APNSP8KeyCredRef = ref
			if _, err := c.DB.UpdateAppleConnection(cred); err != nil {
				slog.Warn("UpdateAppleConnection: persist apns ref failed", "conn", cred.ID, "err", err)
			}
		}
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
		ServiceAccountPath: applyServiceAccountPrefix(req.ServiceAccountKey),
	}
	cred, err := c.DB.CreateGooglePlayConnection(credential)
	if err != nil {
		return nil, err
	}

	// Encrypt at rest via CredStore; legacy path stays for dual-read transition.
	ctx := context.Background()
	if ref, err := c.encryptAndStoreCredential(ctx, orgID, "google-sa", cred.ServiceAccountPath); err != nil {
		slog.Warn("CreateGooglePlayConnection: sa encrypt failed", "conn", cred.ID, "err", err)
	} else if ref != nil {
		cred.ServiceAccountCredRef = ref
		if _, err := c.DB.UpdateGooglePlayConnection(cred); err != nil {
			slog.Warn("CreateGooglePlayConnection: persist sa ref failed", "conn", cred.ID, "err", err)
		}
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
		serviceAccountPath = applyServiceAccountPrefix(req.ServiceAccountKey)
	}

	credentials := &models.GooglePlayConnection{
		ID:                    cred.ID,
		UserID:                user.ID,
		OrganizationID:        cred.OrganizationID,        // preserved — Bun does whole-row update
		Name:                  req.Name,
		ServiceAccountPath:    serviceAccountPath,
		ServiceAccountCredRef: cred.ServiceAccountCredRef, // preserved; replaced below if key rotated
		CreatedAt:             cred.CreatedAt,             // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:             time.Now(),                 // bump on every update
	}
	saved, err := c.DB.UpdateGooglePlayConnection(credentials)
	if err != nil {
		return nil, err
	}

	// Dual-write when the user rotated the SA key.
	if req.ServiceAccountKey != "" {
		ctx := context.Background()
		if ref, err := c.encryptAndStoreCredential(ctx, saved.OrganizationID, "google-sa", saved.ServiceAccountPath); err != nil {
			slog.Warn("UpdateGooglePlayConnection: sa encrypt failed", "conn", saved.ID, "err", err)
		} else if ref != nil {
			saved.ServiceAccountCredRef = ref
			if _, err := c.DB.UpdateGooglePlayConnection(saved); err != nil {
				slog.Warn("UpdateGooglePlayConnection: persist sa ref failed", "conn", saved.ID, "err", err)
			}
		}
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
// The user-scoped methods are kept for system callers (cron,
// AutoSignIOSBuildIfPossible, deployment, pending_connection) that don't
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
		APNSKeyID: cred.APNSP8KeyID,
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
			APNSKeyID: cred.APNSP8KeyID,
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

	p8KeyPath := cred.P8KeyPath
	if req.P8Key != "" {
		p8KeyPath = applyP8Prefix(req.P8Key)
	}
	apnsP8KeyPath := cred.APNSP8KeyPath
	if req.APNSP8Key != "" {
		apnsP8KeyPath = applyAPNSP8Prefix(req.APNSP8Key)
	}

	updated := &models.AppleConnection{
		ID:               cred.ID,
		UserID:           cred.UserID,           // preserved — do not overwrite with requesting user
		OrganizationID:   cred.OrganizationID,   // preserved
		Name:             req.Name,
		P8KeyPath:        p8KeyPath,
		APNSP8KeyPath:    apnsP8KeyPath,
		APNSP8KeyID:      req.APNSKeyID,
		KeyID:            req.KeyID,
		TeamID:           req.TeamID,
		IssuerID:         req.IssuerID,
		P8KeyCredRef:     cred.P8KeyCredRef,     // preserved; replaced below if key rotated
		APNSP8KeyCredRef: cred.APNSP8KeyCredRef, // preserved; replaced below if key rotated
		CreatedAt:        cred.CreatedAt,        // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:        time.Now(),            // bump on every update
	}
	saved, err := c.DB.UpdateAppleConnection(updated)
	if err != nil {
		return nil, err
	}

	// Dual-write: re-encrypt rotated keys via CredStore and persist a fresh ref.
	if req.P8Key != "" {
		if ref, err := c.encryptAndStoreCredential(ctx, saved.OrganizationID, "apple-p8", saved.P8KeyPath); err != nil {
			slog.Warn("UpdateAppleConnectionForOrg: p8 encrypt failed", "conn", saved.ID, "err", err)
		} else if ref != nil {
			saved.P8KeyCredRef = ref
			if _, err := c.DB.UpdateAppleConnection(saved); err != nil {
				slog.Warn("UpdateAppleConnectionForOrg: persist p8 ref failed", "conn", saved.ID, "err", err)
			}
		}
	}
	if req.APNSP8Key != "" {
		if ref, err := c.encryptAndStoreCredential(ctx, saved.OrganizationID, "apple-apns", saved.APNSP8KeyPath); err != nil {
			slog.Warn("UpdateAppleConnectionForOrg: apns encrypt failed", "conn", saved.ID, "err", err)
		} else if ref != nil {
			saved.APNSP8KeyCredRef = ref
			if _, err := c.DB.UpdateAppleConnection(saved); err != nil {
				slog.Warn("UpdateAppleConnectionForOrg: persist apns ref failed", "conn", saved.ID, "err", err)
			}
		}
	}

	return &responses.AppleConnectionResponse{
		ID:        saved.ID,
		Name:      saved.Name,
		KeyID:     saved.KeyID,
		APNSKeyID: saved.APNSP8KeyID,
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

	serviceAccountPath := cred.ServiceAccountPath
	if req.ServiceAccountKey != "" {
		serviceAccountPath = applyServiceAccountPrefix(req.ServiceAccountKey)
	}

	updated := &models.GooglePlayConnection{
		ID:                    cred.ID,
		UserID:                cred.UserID,                // preserved — do not overwrite with requesting user
		OrganizationID:        cred.OrganizationID,        // preserved
		Name:                  req.Name,
		ServiceAccountPath:    serviceAccountPath,
		ServiceAccountCredRef: cred.ServiceAccountCredRef, // preserved; replaced below if key rotated
		CreatedAt:             cred.CreatedAt,             // Bun whole-row UPDATE + nullzero would emit NULL otherwise
		UpdatedAt:             time.Now(),                 // bump on every update
	}
	saved, err := c.DB.UpdateGooglePlayConnection(updated)
	if err != nil {
		return nil, err
	}

	// Dual-write: re-encrypt rotated SA via CredStore and persist a fresh ref.
	if req.ServiceAccountKey != "" {
		if ref, err := c.encryptAndStoreCredential(ctx, saved.OrganizationID, "google-sa", saved.ServiceAccountPath); err != nil {
			slog.Warn("UpdateGooglePlayConnectionForOrg: sa encrypt failed", "conn", saved.ID, "err", err)
		} else if ref != nil {
			saved.ServiceAccountCredRef = ref
			if _, err := c.DB.UpdateGooglePlayConnection(saved); err != nil {
				slog.Warn("UpdateGooglePlayConnectionForOrg: persist sa ref failed", "conn", saved.ID, "err", err)
			}
		}
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
