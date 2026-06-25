package controllers

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services"
	caddyService "github.com/knid/wemob/internal/services/caddy"
	"github.com/knid/wemob/internal/services/events"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/meta"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	sandboxService "github.com/knid/wemob/internal/services/sandbox"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/redis/go-redis/v9"
)

type Controller struct {
	DB                 *database.Database
	SecretManager      secret.SecretManager
	SlackService       *services.SlackService
	EventService       events.EventService
	TaskService        *asynq.Client
	AsynqInspector     *asynq.Inspector
	RedisPubSub        *redis.Client // raw client for pub/sub channels (job events, cancel signals from worker ↔ API)
	AssetsStorage      *storage.S3Storage
	UserApiKeysStorage *storage.S3Storage
	CredStore          *storage.CredStore
	// MetaCAPI is nil when META_PIXEL_ID or META_CAPI_ACCESS_TOKEN is unset.
	// Conversion events skip silently in that case; client-side Pixel still
	// fires PageView from the front-end if NEXT_PUBLIC_META_PIXEL_ID is set.
	MetaCAPI     *meta.Service
	FlyService   *fly.Service
	FlyInspector FlyInspector
	GiteaService *gitea.Service
	NeonService  *neon.Service
	// NeonRegistry routes Neon API calls to the right org-scoped service
	// (free vs paid) based on a project's tier. NeonService remains a
	// back-compat pointer to the paid-tier service for callers that don't
	// need tier routing (e.g. project deletion: the registry's For() handles
	// any tier so the legacy field is fine for one-shot DELETEs against the
	// project's current org — see app deletion path).
	NeonRegistry          *neon.Registry
	GitRepoService        *gitrepo.Service
	MCPService            atomic.Pointer[mcpclient.Service]
	PostHog               posthog.Client // nil = disabled
	RemoteConfig          *remoteconfig.Service
	SandboxManager        *sandboxService.Manager
	RunningJobs           sync.Map // map[string]context.CancelFunc for in-flight background jobs
	PendingClarifications sync.Map // map[string]chan ClarificationResult for jobs awaiting user input
	JobEventBuses         sync.Map // map[string]*EventBus for live SSE fan-out to multiple clients
	identifiedUsers       sync.Map // user.ID.String() → time.Time of last $identify (TTL-gated, see identifyCacheTTL)
	personalOrgIDs        sync.Map // user.ID.String() → cachedBilling (org_id + plan + subscription_status with TTL)
	EdgeIPs               []string // known Fly edge IPs for DNS verification
	CaddyAdmin            *caddyService.Admin
}

func NewController(db *database.Database,
	slackService *services.SlackService, eventService events.EventService,
	taskService *asynq.Client, asynqInspector *asynq.Inspector,
	redisClient *redis.Client,
	assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage,
	credStore *storage.CredStore,
	secretManager secret.SecretManager,
	flyService *fly.Service, giteaService *gitea.Service,
	neonService *neon.Service,
	neonRegistry *neon.Registry,
	posthogClient posthog.Client,
	gitRepoService *gitrepo.Service,
	metaCAPI *meta.Service,
	remoteConfigService *remoteconfig.Service,
	sandboxManager *sandboxService.Manager,
	edgeIPs []string,
	caddyAdmin *caddyService.Admin) *Controller {

	return &Controller{
		DB:                 db,
		SlackService:       slackService,
		EventService:       eventService,
		TaskService:        taskService,
		AsynqInspector:     asynqInspector,
		RedisPubSub:        redisClient,
		AssetsStorage:      assetsStorage,
		UserApiKeysStorage: userApiKeysStorage,
		CredStore:          credStore,
		SecretManager:      secretManager,
		MetaCAPI:           metaCAPI,
		FlyService:         flyService,
		FlyInspector:       flyService,
		GiteaService:       giteaService,
		NeonService:        neonService,
		NeonRegistry:       neonRegistry,
		GitRepoService:     gitRepoService,
		PostHog:            posthogClient,
		RemoteConfig:       remoteConfigService,
		SandboxManager:     sandboxManager,
		EdgeIPs:            edgeIPs,
		CaddyAdmin:         caddyAdmin,
	}
}

// llmProvider resolves the active LLM provider from the LLM_PROVIDER secret.
// Supported values: "openrouter" (default), "anthropic", "moonshot".
// Google models (e.g. "google/gemini-2.5-flash-lite") are routed through OpenRouter.
//
// When the primary is "anthropic" and OPENROUTER_API_KEY is set, an OpenRouter
// fallback is attached. On Anthropic overload (529 / overloaded_error) the call
// retries once via OpenRouter, which automatically routes the same Claude model
// through Bedrock or Vertex (independent capacity pools).
func (c *Controller) llmProvider() *utils.LLMProvider {
	raw := c.SecretManager.GetSecret("LLM_PROVIDER")
	provider := strings.TrimSpace(strings.ToLower(raw))
	fastModel := c.SecretManager.GetSecret("WEBSITE_BUILDER_FAST_MODEL")

	slog.Info("LLM provider resolved", "raw", raw, "normalized", provider)

	switch provider {
	case "anthropic":
		p := &utils.LLMProvider{Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", APIKey: c.SecretManager.GetSecret("ANTHROPIC_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel}
		c.attachOpenRouterFallback(p, fastModel)
		return p
	case "moonshot":
		moonshotReasoning := c.reasoningConfig()
		if c.RemoteConfig != nil && !c.RemoteConfig.GetBool(remoteconfig.AIMoonshotThinkingEnabled, remoteconfig.Global) {
			moonshotReasoning = nil
		}
		p := &utils.LLMProvider{Name: "moonshot", BaseURL: "https://api.moonshot.ai/v1", APIKey: c.SecretManager.GetSecret("MOONSHOT_API_KEY"), DefaultReasoning: moonshotReasoning, FastModelOverride: fastModel}
		c.attachOpenRouterFallback(p, fastModel)
		return p
	default:
		slog.Warn("Unknown or empty LLM_PROVIDER, falling back to openrouter", "raw", raw)
		return &utils.LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: c.SecretManager.GetSecret("OPENROUTER_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel}
	}
}

// llmProviderForUser resolves the LLM provider + model for a specific user based
// on their subscription tier and PostHog feature flags.
// Falls back to the global llmProvider() when the user is nil or the flag is empty.
func (c *Controller) llmProviderForUser(user *models.User) *utils.LLMProvider {
	if user == nil || c.RemoteConfig == nil {
		return c.llmProvider()
	}

	tier := c.GetUserTierName(user)
	var flagKey remoteconfig.Key
	switch tier {
	case "starter", "free":
		flagKey = remoteconfig.BuilderProviderModelStarter
	case "hobby":
		flagKey = remoteconfig.BuilderProviderModelHobby
	case "pro":
		flagKey = remoteconfig.BuilderProviderModelPro
	case "max":
		flagKey = remoteconfig.BuilderProviderModelMax
	default:
		return c.llmProvider()
	}

	providerModel := strings.TrimSpace(c.RemoteConfig.GetString(flagKey, user.ID.String()))
	if providerModel == "" {
		return c.llmProvider()
	}

	providerName, model, flagReasoning, providerPin := c.parseProviderModel(providerModel)
	reasoning := c.reasoningConfig()
	if flagReasoning != nil {
		reasoning = flagReasoning
	}
	fastModel := c.SecretManager.GetSecret("WEBSITE_BUILDER_FAST_MODEL")

	var p *utils.LLMProvider
	switch providerName {
	case "anthropic":
		p = &utils.LLMProvider{Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", APIKey: c.SecretManager.GetSecret("ANTHROPIC_API_KEY"), DefaultReasoning: reasoning, FastModelOverride: fastModel, ConfiguredModel: model}
	case "moonshot":
		moonshotReasoning := reasoning
		if !c.RemoteConfig.GetBool(remoteconfig.AIMoonshotThinkingEnabled, remoteconfig.Global) {
			moonshotReasoning = nil
		}
		p = &utils.LLMProvider{Name: "moonshot", BaseURL: "https://api.moonshot.ai/v1", APIKey: c.SecretManager.GetSecret("MOONSHOT_API_KEY"), DefaultReasoning: moonshotReasoning, FastModelOverride: fastModel, ConfiguredModel: model}
	default:
		p = &utils.LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: c.SecretManager.GetSecret("OPENROUTER_API_KEY"), DefaultReasoning: reasoning, FastModelOverride: fastModel, ConfiguredModel: model, OpenRouterProviderPin: providerPin}
	}
	if providerPin != "" && !p.IsOpenRouter() {
		slog.Warn("Ignoring OpenRouter provider pin for non-OpenRouter builder model", "provider", p.Name, "model", model, "provider_pin", providerPin)
	}

	// Every non-OpenRouter primary gets an OpenRouter fallback.
	if !p.IsOpenRouter() {
		c.attachOpenRouterFallbackWithReasoning(p, fastModel, reasoning)
	}

	reasoningEffort := ""
	if reasoning != nil {
		reasoningEffort = reasoning.Effort
	}
	slog.Info("LLM provider resolved per user", "user_id", user.ID, "tier", tier, "provider", p.Name, "model", model, "reasoning_effort", reasoningEffort, "provider_pin", providerPin)
	return p
}

// parseProviderModel splits a "provider/model[;reasoning_effort][;openrouter_provider_pin]"
// string into provider name, bare model ID, and optional OpenRouter controls. Known
// providers: anthropic, moonshot, openrouter.
// If the first segment is not a known provider, the whole string is treated as
// an OpenRouter model ID (e.g. "deepseek/deepseek-v4-flash", "google/gemini-2.5-flash-lite").
func (c *Controller) parseProviderModel(providerModel string) (provider, model string, reasoning *utils.ReasoningConfig, providerPin string) {
	providerModel = strings.TrimSpace(strings.ToLower(providerModel))
	segments := strings.Split(providerModel, ";")
	if len(segments) > 1 {
		providerModel = strings.TrimSpace(segments[0])
		effort := strings.TrimSpace(segments[1])
		if isReasoningEffort(effort) {
			reasoning = &utils.ReasoningConfig{Effort: effort}
		} else if effort != "" {
			slog.Warn("Ignoring invalid builder model reasoning effort", "provider_model", providerModel, "reasoning_effort", effort)
		}
	}
	if len(segments) > 2 {
		providerPin = strings.TrimSpace(segments[2])
	}
	if len(segments) > 3 {
		slog.Warn("Ignoring extra builder model config segments", "provider_model", providerModel, "extra_segments", len(segments)-3)
	}

	parts := strings.SplitN(providerModel, "/", 2)
	if len(parts) != 2 {
		return "openrouter", providerModel, reasoning, providerPin
	}

	switch parts[0] {
	case "anthropic", "moonshot", "openrouter":
		return parts[0], parts[1], reasoning, providerPin
	default:
		// First segment is a vendor namespace on OpenRouter (e.g. "deepseek/...")
		return "openrouter", providerModel, reasoning, providerPin
	}
}

func isReasoningEffort(effort string) bool {
	switch effort {
	case "xhigh", "high", "medium", "low", "minimal", "none":
		return true
	default:
		return false
	}
}

// attachOpenRouterFallback wires an OpenRouter fallback onto any primary
// provider when OPENROUTER_API_KEY is configured. The fallback model is read
// from LLM_FALLBACK_MODEL (default: anthropic/claude-sonnet-4.6).
func (c *Controller) attachOpenRouterFallback(primary *utils.LLMProvider, fastModel string) {
	c.attachOpenRouterFallbackWithReasoning(primary, fastModel, c.reasoningConfig())
}

func (c *Controller) attachOpenRouterFallbackWithReasoning(primary *utils.LLMProvider, fastModel string, reasoning *utils.ReasoningConfig) {
	openRouterKey := c.SecretManager.GetSecret("OPENROUTER_API_KEY")
	if openRouterKey == "" {
		slog.Info("LLM overload fallback disabled — OPENROUTER_API_KEY not set")
		return
	}
	fallbackModel := strings.TrimSpace(c.SecretManager.GetSecret("LLM_FALLBACK_MODEL"))
	if fallbackModel == "" {
		fallbackModel = "anthropic/claude-sonnet-4.6"
	}
	primary.Fallback = &utils.LLMProvider{
		Name:              "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		APIKey:            openRouterKey,
		DefaultReasoning:  reasoning,
		FastModelOverride: fastModel,
	}
	primary.FallbackModel = fallbackModel
	slog.Info("LLM overload fallback attached",
		"primary", primary.Name, "fallback_provider", "openrouter", "fallback_model", fallbackModel)
}

// LLMProvider is the exported version of llmProvider for use in views.
func (c *Controller) LLMProvider() *utils.LLMProvider {
	return c.llmProvider()
}

// LLMProviderForUser returns the per-user LLM provider for the given user.
func (c *Controller) LLMProviderForUser(user *models.User) *utils.LLMProvider {
	return c.llmProviderForUser(user)
}

// AnthropicProvider returns an Anthropic-only LLM provider for lightweight
// tasks (screenshots, autofill) that always use the native Anthropic API.
func (c *Controller) AnthropicProvider() *utils.LLMProvider {
	return &utils.LLMProvider{
		Name:    "anthropic",
		BaseURL: "https://api.anthropic.com/v1",
		APIKey:  c.SecretManager.GetSecret("ANTHROPIC_API_KEY"),
	}
}

// reasoningConfig returns the ReasoningConfig from the LLM_REASONING_EFFORT secret.
// Set it in Infisical to control thinking mode: "none", "minimal", "low",
// "medium", "high", or "xhigh".
// Leave empty/unset to let the model use its default behavior.
func (c *Controller) reasoningConfig() *utils.ReasoningConfig {
	effort := c.SecretManager.GetSecret("LLM_REASONING_EFFORT")
	if effort == "" {
		return nil
	}
	return &utils.ReasoningConfig{Effort: effort}
}

// builderModel resolves the LLM model for a builder task given a provider.
// When the provider has a per-user ConfiguredModel (from PostHog), that wins.
// Otherwise falls back to the global WEBSITE_BUILDER_LLM_MODEL secret, then
// the provider's default.
func (c *Controller) builderModel(provider *utils.LLMProvider) string {
	if provider != nil && provider.ConfiguredModel != "" {
		return provider.ConfiguredModel
	}
	if m := c.SecretManager.GetSecret("WEBSITE_BUILDER_LLM_MODEL"); m != "" {
		return m
	}
	if provider != nil {
		return provider.DefaultModel()
	}
	return constants.GatewayPreferredChatModel
}
