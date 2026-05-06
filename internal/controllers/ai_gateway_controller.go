package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
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
	case "fal-ai/flux/schnell":
		rcKey = remoteconfig.GatewayImageCostFluxSchnell
	case "fal-ai/flux-pro/v1.1":
		rcKey = remoteconfig.GatewayImageCostFluxPro
	case "fal-ai/flux-pro/v1.1-ultra":
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

// GetUserTierName returns a normalized tier name from the user's
// subscription. Reads SubscriptionStatus / SubscriptionID from the user's
// personal Organization (Plan 5). Falls back to "free" on any lookup
// failure to keep gating decisions strict.
func (c *Controller) GetUserTierName(user *models.User) string {
	org, err := c.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil || org == nil {
		return "free"
	}
	// F3: dropped 'trial' from the whitelist — no Trial plan row exists.
	if org.SubscriptionStatus != "active" {
		return "free"
	}
	sub, err := c.DB.GetSubscriptionById(org.SubscriptionID.String())
	if err != nil {
		return "free"
	}
	return strings.ToLower(sub.Name)
}

// upstreamCostSafetyFactor caps trusted upstream usage.Cost at this
// multiple of catalog-derived USD — absorbs dynamic pricing & cache-shape
// drift without blanket-trusting upstream.
const upstreamCostSafetyFactor = 2.0

// GatewayChatCreditsOwed returns the fractional credit amount a gateway
// chat call should cost. Prefers the upstream-billed USD cost that
// OpenRouter returns in usage.cost (when the request sent
// usage.include=true), falling back to catalog-based math only when
// that cost is missing or zero. Returns 0 for unknown models with
// no cost AND no catalog data so callers can safely no-op.
//
// Formula:
//
//	upstreamUSD =
//	    usage.Cost                               (preferred — real billed cost)
//	    or (prompt*inPerM + completion*outPerM) / 1_000_000  (catalog fallback)
//	userCostUSD = upstreamUSD * (1 + markup%)
//	owed        = userCostUSD / creditPriceUSD
//
// The caller feeds owed into DB.AtomicAccumulateGatewayPending so the
// fractional debt accrues across calls and whole credits only get
// deducted when it crosses an integer.
func (c *Controller) GatewayChatCreditsOwed(modelID string, usage *utils.UsageInfo) float64 {
	if usage == nil {
		return 0
	}

	upstreamUSD := usage.Cost
	if upstreamUSD <= 0 {
		// Fallback: catalog-based math. Kept as a safety net for calls
		// where the upstream omitted cost (old OpenRouter responses,
		// non-OpenRouter upstreams, etc.) and so new models without
		// catalog entries still charge something reasonable during the
		// window between "model allowlisted" and "catalog updated."
		if usage.PromptTokens <= 0 && usage.CompletionTokens <= 0 {
			return 0
		}
		m := constants.GatewayChatModelByID(modelID)
		if m == nil || (m.InputUSDPerM <= 0 && m.OutputUSDPerM <= 0) {
			return 0
		}
		upstreamUSD = (float64(usage.PromptTokens)*m.InputUSDPerM +
			float64(usage.CompletionTokens)*m.OutputUSDPerM) / 1_000_000.0
	} else {
		m := constants.GatewayChatModelByID(modelID)
		switch {
		case m == nil:
			slog.Warn("ai_gateway: upstream cost present for model missing from catalog",
				"model", modelID, "upstream_cost_usd", upstreamUSD)
		case usage.PromptTokens+usage.CompletionTokens == 0:
			slog.Warn("ai_gateway: upstream cost present with zero token totals; skipping ceiling cap",
				"model", modelID, "upstream_cost_usd", upstreamUSD)
		default:
			catalogUSD := (float64(usage.PromptTokens)*m.InputUSDPerM +
				float64(usage.CompletionTokens)*m.OutputUSDPerM) / 1_000_000.0
			ceiling := catalogUSD * upstreamCostSafetyFactor
			if upstreamUSD > ceiling {
				slog.Warn("ai_gateway: upstream cost exceeded catalog ceiling",
					"model", modelID, "upstream_cost_usd", upstreamUSD,
					"catalog_max_usd", catalogUSD, "ceiling_usd", ceiling,
					"input_tokens", usage.PromptTokens, "output_tokens", usage.CompletionTokens)
				upstreamUSD = ceiling
			}
		}
	}

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
// database accumulator and emits an ai_credit_transactions audit row for
// every successful call. When the accumulator crosses an integer boundary
// the row uses Action=action with Amount=-rolled (the whole credits moved
// out of pending_gateway onto balance). When only pending_gateway grew
// the row uses Action=action+"_pending" with Amount=0 and PendingAmount=owed
// — the sub-credit cost the user was charged. Returns the number of whole
// credits deducted (0 if the call only increased pending).
//
// The action-name distinction (e.g. gateway_chat vs gateway_chat_pending)
// lets the dashboard hide sub-credit rows by default while keeping them
// queryable for operator-side reconciliation against gateway request logs.
func (c *Controller) AccumulateGatewayChatCredits(user *models.User, orgID uuid.UUID, owed float64, action, reference string) (int, error) {
	if owed <= 0 {
		return 0, nil
	}
	if org, orgErr := c.DB.GetOrganizationByID(orgID.String()); orgErr == nil && IsOrgInDunning(org) {
		return 0, ErrOrgInDunning
	}
	rolled, err := c.DB.AtomicAccumulateGatewayPending(orgID, owed)
	if err != nil {
		return 0, fmt.Errorf("accumulate gateway pending: %w", err)
	}
	row := &models.AICreditTransaction{
		OrganizationID: orgID,
		UserID:         user.ID,
		Reference:      reference,
	}
	if rolled > 0 {
		row.Amount = -rolled
		row.Action = action
	} else {
		row.Amount = 0
		row.Action = action + "_pending"
		row.PendingAmount = owed
	}
	if _, terr := c.DB.CreateAICreditTransaction(row); terr != nil {
		slog.Warn("accumulate gateway pending: failed to emit transaction row",
			"error", terr, "user_id", user.ID, "org_id", orgID, "rolled", rolled, "owed", owed)
	}
	return rolled, nil
}

// ResolveGatewayProvider returns the LLMProvider for gateway chat traffic.
// All chat models flow through OpenRouter — one upstream, one OpenAI-
// compatible contract. Model IDs must already be in OpenRouter's
// provider/model format (e.g. "anthropic/claude-sonnet-4.6").
func (c *Controller) ResolveGatewayProvider(modelID string) *utils.LLMProvider {
	return &utils.LLMProvider{
		Name:    "openrouter",
		BaseURL: "https://openrouter.ai/api/v1",
		APIKey:  c.SecretManager.GetSecret("OPENROUTER_API_KEY"),
	}
}
