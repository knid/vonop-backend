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

	// Verify the project is still active (not deleted)
	project, err := c.DB.GetAppProjectByID(gwKey.ProjectID.String())
	if err != nil || project == nil {
		return nil, "", fmt.Errorf("project not found")
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

// GatewayChatCreditsOwed returns the fractional credit amount a gateway
// chat call should cost, computed from upstream USD prices in the model
// catalog, a remote-config markup percent, and a remote-config reference
// credit price. Returns 0 for unknown models or zero-token usage so
// callers can safely no-op.
//
// Formula:
//
//	upstreamUSD = (prompt*inPerM + completion*outPerM) / 1_000_000
//	userCostUSD = upstreamUSD * (1 + markup%)
//	owed        = userCostUSD / creditPriceUSD
//
// The caller is responsible for feeding owed into
// DB.AtomicAccumulateGatewayPending so the fractional debt accrues
// across calls and whole credits only get deducted when it crosses an
// integer.
func (c *Controller) GatewayChatCreditsOwed(modelID string, promptTokens, completionTokens int) float64 {
	if promptTokens <= 0 && completionTokens <= 0 {
		return 0
	}
	m := constants.GatewayChatModelByID(modelID)
	if m == nil || (m.InputUSDPerM <= 0 && m.OutputUSDPerM <= 0) {
		// Unknown model or missing price data — refuse to guess rather
		// than deduct a wrong amount. The upstream call already ran;
		// we'll fix the catalog and this user gets a free call.
		return 0
	}
	upstreamUSD := (float64(promptTokens)*m.InputUSDPerM + float64(completionTokens)*m.OutputUSDPerM) / 1_000_000.0

	markupPct := c.RemoteConfig.GetInt(remoteconfig.GatewayMarkupPercent, remoteconfig.Global)
	markup := float64(markupPct) / 100.0
	if markup < 0 {
		markup = 0
	}
	userCostUSD := upstreamUSD * (1.0 + markup)

	priceCents := c.RemoteConfig.GetInt(remoteconfig.GatewayCreditPriceUSDCents, remoteconfig.Global)
	if priceCents <= 0 {
		priceCents = 20 // fallback to $0.20 if remote config returns 0
	}
	creditPriceUSD := float64(priceCents) / 100.0

	return userCostUSD / creditPriceUSD
}

// AccumulateGatewayChatCredits feeds the fractional owed amount into the
// database accumulator and, if the accumulator crossed an integer, emits
// an ai_credit_transactions audit row for the whole credits that were
// moved out of pending_gateway onto balance. Returns the number of whole
// credits deducted (0 if the call only increased pending).
func (c *Controller) AccumulateGatewayChatCredits(user *models.User, owed float64, action, reference string) (int, error) {
	if owed <= 0 {
		return 0, nil
	}
	rolled, err := c.DB.AtomicAccumulateGatewayPending(user.ID.String(), owed)
	if err != nil {
		return 0, fmt.Errorf("accumulate gateway pending: %w", err)
	}
	if rolled > 0 {
		if _, terr := c.DB.CreateAICreditTransaction(&models.AICreditTransaction{
			UserID:    user.ID,
			Amount:    -rolled,
			Action:    action,
			Reference: reference,
		}); terr != nil {
			slog.Warn("accumulate gateway pending: failed to emit transaction row",
				"error", terr, "user_id", user.ID, "rolled", rolled)
		}
	}
	return rolled, nil
}

// ResolveGatewayProvider returns the LLMProvider for gateway chat traffic.
// All chat models flow through OpenRouter — one upstream, one OpenAI-
// compatible contract. Model IDs must already be in OpenRouter's
// provider/model format (e.g. "anthropic/claude-sonnet-4").
func (c *Controller) ResolveGatewayProvider(modelID string) *utils.LLMProvider {
	return &utils.LLMProvider{
		Name:    "openrouter",
		BaseURL: "https://openrouter.ai/api/v1",
		APIKey:  c.SecretManager.GetSecret("OPENROUTER_API_KEY"),
	}
}
