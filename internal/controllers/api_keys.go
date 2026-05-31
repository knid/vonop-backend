package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

const apiKeyPrefix = "wemob_"

func (c *Controller) CreateAPIKey(user *models.User, orgID uuid.UUID, req requests.CreateAPIKeyRequest) (*responses.APIKeyCreatedResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.CreateAPIKeyRequest](req, validate); err != nil {
		return nil, err
	}

	// Validate scopes
	validScopes := map[string]bool{
		"builds:read": true, "builds:create": true,
		"projects:read": true, "projects:write": true,
		"apps:read": true, "apps:write": true,
	}
	for _, scope := range strings.Split(req.Scopes, ",") {
		if !validScopes[strings.TrimSpace(scope)] {
			return nil, errors.New("invalid scope: " + scope)
		}
	}

	// Generate a random key
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		return nil, errors.New("failed to generate key")
	}
	fullKey := apiKeyPrefix + hex.EncodeToString(rawKey)
	keyHash := utils.GetSHA256Hash(fullKey)

	apiKey := models.APIKey{
		UserID:         user.ID,
		OrganizationID: orgID,
		Name:           req.Name,
		KeyHash:        keyHash,
		KeyPrefix:      fullKey[:len(apiKeyPrefix)+8],
		Scopes:         req.Scopes,
		ExpiresAt:      time.Now().AddDate(1, 0, 0), // 1 year default expiry
	}

	created, err := c.DB.CreateAPIKey(&apiKey)
	if err != nil {
		return nil, err
	}

	return &responses.APIKeyCreatedResponse{
		APIKeyResponse: responses.APIKeyResponse{
			ID:        created.ID,
			Name:      created.Name,
			KeyPrefix: created.KeyPrefix,
			Scopes:    created.Scopes,
			ExpiresAt: created.ExpiresAt,
			CreatedAt: created.CreatedAt,
		},
		Key: fullKey,
	}, nil
}

func (c *Controller) ListAPIKeys(user *models.User, reqOrgID *uuid.UUID) ([]responses.APIKeyResponse, error) {
	var orgID uuid.UUID
	if reqOrgID != nil && *reqOrgID != uuid.Nil {
		orgID = *reqOrgID
	} else {
		personal, err := c.EnsurePersonalOrg(user)
		if err != nil {
			return nil, err
		}
		orgID = personal.ID
	}

	member, err := c.DB.GetOrganizationMember(orgID.String(), user.ID.String())
	if err != nil {
		return nil, errors.New("organization not found or access denied")
	}

	// Admins/owners see every key in the org; members see only their own so
	// keys minted by other org members aren't exposed.
	var keys []models.APIKey
	if member.Role == "owner" || member.Role == "admin" {
		keys, err = c.DB.GetAPIKeysByOrgID(orgID.String())
	} else {
		keys, err = c.DB.GetAPIKeysByUserAndOrgID(user.ID.String(), orgID.String())
	}
	if err != nil {
		return nil, err
	}

	var result []responses.APIKeyResponse
	for _, k := range keys {
		result = append(result, responses.APIKeyResponse{
			ID:        k.ID,
			Name:      k.Name,
			KeyPrefix: k.KeyPrefix,
			Scopes:    k.Scopes,
			LastUsed:  k.LastUsed,
			ExpiresAt: k.ExpiresAt,
			CreatedAt: k.CreatedAt,
		})
	}
	return result, nil
}

func (c *Controller) DeleteAPIKey(user *models.User, keyID string) error {
	key, err := c.DB.GetAPIKeyByID(keyID)
	if err != nil {
		return errors.New("API key not found")
	}
	member, err := c.DB.GetOrganizationMember(key.OrganizationID.String(), user.ID.String())
	if err != nil {
		return errors.New("API key not found")
	}
	if !HasPermission(member, constants.PermOrgSettings) && key.UserID != user.ID {
		return errors.New("API key not found")
	}
	return c.DB.DeleteAPIKey(key)
}

// AuthenticateAPIKey validates an API key and returns the associated user.
func (c *Controller) AuthenticateAPIKey(rawKey string) (*models.User, *models.APIKey, error) {
	if !strings.HasPrefix(rawKey, apiKeyPrefix) {
		return nil, nil, errors.New("invalid API key format")
	}

	keyHash := utils.GetSHA256Hash(rawKey)
	apiKey, err := c.DB.GetAPIKeyByHash(keyHash)
	if err != nil {
		return nil, nil, errors.New("invalid API key")
	}

	if !apiKey.ExpiresAt.IsZero() && apiKey.ExpiresAt.Before(time.Now()) {
		return nil, nil, errors.New("API key has expired")
	}

	// Update last used (fire-and-forget)
	go c.DB.UpdateAPIKeyLastUsed(apiKey)

	return apiKey.User, apiKey, nil
}
