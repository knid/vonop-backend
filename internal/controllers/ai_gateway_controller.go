package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/utils"
)

// CreateProjectGatewayKey generates a new gateway key for a project.
// Returns the raw key (only available at creation time).
func (c *Controller) CreateProjectGatewayKey(project *models.AppProject) (string, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", fmt.Errorf("failed to generate gateway key: %w", err)
	}
	rawKey := constants.GatewayKeyPrefix + hex.EncodeToString(rawBytes)
	keyHash := utils.GetSHA256Hash(rawKey)

	encKey := c.SecretManager.GetSecret("ENCRYPTION_KEY")
	encrypted, err := utils.EncryptToken(rawKey, encKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt gateway key: %w", err)
	}

	gwKey := &models.ProjectGatewayKey{
		ProjectID:    project.ID,
		UserID:       project.UserID,
		KeyHash:      keyHash,
		KeyPrefix:    rawKey[:len(constants.GatewayKeyPrefix)+8],
		EncryptedKey: encrypted,
	}

	if _, err := c.DB.CreateProjectGatewayKey(gwKey); err != nil {
		return "", fmt.Errorf("failed to save gateway key: %w", err)
	}

	return rawKey, nil
}

// RotateProjectGatewayKey soft-deletes the current key and creates a new one.
func (c *Controller) RotateProjectGatewayKey(project *models.AppProject) (string, error) {
	existing, err := c.DB.GetProjectGatewayKeyByProjectID(project.ID.String())
	if err == nil && existing != nil {
		if err := c.DB.SoftDeleteProjectGatewayKey(existing); err != nil {
			slog.Warn("Failed to soft-delete old gateway key", "error", err, "project_id", project.ID)
		}
	}
	return c.CreateProjectGatewayKey(project)
}

// GetDecryptedGatewayKey returns the raw gateway key for env var injection.
func (c *Controller) GetDecryptedGatewayKey(projectID string) (string, error) {
	gwKey, err := c.DB.GetProjectGatewayKeyByProjectID(projectID)
	if err != nil {
		return "", fmt.Errorf("gateway key not found for project")
	}
	encKey := c.SecretManager.GetSecret("ENCRYPTION_KEY")
	return utils.DecryptToken(gwKey.EncryptedKey, encKey)
}

// AuthenticateGatewayKey validates a gateway key and returns the user and project ID.
func (c *Controller) AuthenticateGatewayKey(rawKey string) (*models.User, string, error) {
	if !strings.HasPrefix(rawKey, constants.GatewayKeyPrefix) {
		return nil, "", fmt.Errorf("invalid gateway key format")
	}

	keyHash := utils.GetSHA256Hash(rawKey)
	gwKey, err := c.DB.GetProjectGatewayKeyByHash(keyHash)
	if err != nil {
		return nil, "", fmt.Errorf("invalid gateway key")
	}

	return gwKey.User, gwKey.ProjectID.String(), nil
}

// GatewayAllowedModels returns the list of models the user's tier can access.
func (c *Controller) GatewayAllowedModels(user *models.User) []string {
	tier := c.GetUserTierName(user)

	var rcKey remoteconfig.Key
	switch tier {
	case "starter":
		rcKey = remoteconfig.GatewayModelsStarter
	case "pro":
		rcKey = remoteconfig.GatewayModelsPro
	case "business":
		rcKey = remoteconfig.GatewayModelsBusiness
	default:
		rcKey = remoteconfig.GatewayModelsFree
	}

	csv := c.RemoteConfig.GetString(rcKey, user.ID.String())
	if csv == "" {
		if fallback, ok := constants.DefaultGatewayModels[tier]; ok {
			return fallback
		}
		return constants.DefaultGatewayModels["free"]
	}

	var result []string
	for _, m := range strings.Split(csv, ",") {
		m = strings.TrimSpace(m)
		if m != "" {
			result = append(result, m)
		}
	}
	return result
}

// GatewayRPMLimit returns the RPM rate limit for the user's subscription tier.
func (c *Controller) GatewayRPMLimit(user *models.User) int {
	tier := c.GetUserTierName(user)

	var rcKey remoteconfig.Key
	switch tier {
	case "starter":
		rcKey = remoteconfig.GatewayRPMStarter
	case "pro":
		rcKey = remoteconfig.GatewayRPMPro
	case "business":
		rcKey = remoteconfig.GatewayRPMBusiness
	default:
		rcKey = remoteconfig.GatewayRPMFree
	}

	rpm := c.RemoteConfig.GetInt(rcKey, user.ID.String())
	if rpm <= 0 {
		if fallback, ok := constants.DefaultGatewayRPM[tier]; ok {
			return fallback
		}
		return constants.DefaultGatewayRPM["free"]
	}
	return rpm
}

// GatewayImageCreditCost returns the credit cost per image for a given model.
func (c *Controller) GatewayImageCreditCost(modelID string) int {
	var rcKey remoteconfig.Key
	switch modelID {
	case "fal-ai/flux-schnell":
		rcKey = remoteconfig.GatewayImageCostFluxSchnell
	case "fal-ai/flux-pro/v1.1":
		rcKey = remoteconfig.GatewayImageCostFluxPro
	case "fal-ai/flux-ultra":
		rcKey = remoteconfig.GatewayImageCostFluxUltra
	default:
		if cost, ok := constants.DefaultGatewayImageCosts[modelID]; ok {
			return cost
		}
		return 10
	}

	cost := c.RemoteConfig.GetInt(rcKey, remoteconfig.Global)
	if cost <= 0 {
		if fallback, ok := constants.DefaultGatewayImageCosts[modelID]; ok {
			return fallback
		}
		return 10
	}
	return cost
}

// GetUserTierName returns a normalized tier name from the user's subscription.
func (c *Controller) GetUserTierName(user *models.User) string {
	if user.SubscriptionStatus != "active" && user.SubscriptionStatus != "trial" {
		return "free"
	}
	sub, err := c.DB.GetSubscriptionById(user.SubscriptionID.String())
	if err != nil {
		return "free"
	}
	return strings.ToLower(sub.Name)
}

// ResolveGatewayProvider returns an LLMProvider for the given model ID.
func (c *Controller) ResolveGatewayProvider(modelID string) *utils.LLMProvider {
	switch {
	case strings.HasPrefix(modelID, "claude-"):
		return &utils.LLMProvider{
			Name:    "anthropic",
			BaseURL: "https://api.anthropic.com/v1",
			APIKey:  c.SecretManager.GetSecret("ANTHROPIC_API_KEY"),
		}
	case strings.HasPrefix(modelID, "gemini-"):
		return &utils.LLMProvider{
			Name:    "google",
			BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
			APIKey:  c.SecretManager.GetSecret("GOOGLE_API_KEY"),
		}
	default:
		return &utils.LLMProvider{
			Name:    "openrouter",
			BaseURL: "https://openrouter.ai/api/v1",
			APIKey:  c.SecretManager.GetSecret("OPENROUTER_API_KEY"),
		}
	}
}
