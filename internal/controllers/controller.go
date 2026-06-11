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
	"github.com/knid/wemob/internal/services/codemagic"
	e2bService "github.com/knid/wemob/internal/services/e2b"
	"github.com/knid/wemob/internal/services/events"
	"github.com/knid/wemob/internal/services/fly"
	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/meta"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/revenuecat"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/redis/go-redis/v9"
)

type Controller struct {
	DB                     *database.Database
	SecretManager          secret.SecretManager
	SlackService           *services.SlackService
	EventService           events.EventService
	TaskService            *asynq.Client
	AsynqInspector         *asynq.Inspector
	RedisPubSub            *redis.Client // raw client for pub/sub channels (job events, cancel signals from worker ↔ API)
	AssetsStorage          *storage.S3Storage
	UserApiKeysStorage     *storage.S3Storage
	AndroidKeysStorage     *storage.S3Storage
	CredStore              *storage.CredStore
	ConcurrentBuildLimiter chan struct{}
	CodemagicService       *codemagic.Service
	// RevenueCatService is nil when REVENUECAT_API_KEY is unset (graceful
	// degrade: IAP auto-provisioning skips, builds proceed with empty IAP
	// keys, the template's lib/iap.ts initIAP() no-ops + warns).
	RevenueCatService *revenuecat.Service
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
	BareGitRepoService    *gitrepo.BareService
	MCPService            atomic.Pointer[mcpclient.Service]
	PostHog               posthog.Client // nil = disabled
	RemoteConfig          *remoteconfig.Service
	E2BService            *e2bService.Service
	RunningJobs           sync.Map // map[string]context.CancelFunc for in-flight background jobs
	PendingClarifications sync.Map // map[string]chan ClarificationResult for jobs awaiting user input
	JobEventBuses         sync.Map // map[string]*EventBus for live SSE fan-out to multiple clients
	identifiedUsers       sync.Map // user.ID.String() → time.Time of last $identify (TTL-gated, see identifyCacheTTL)
	personalOrgIDs        sync.Map // user.ID.String() → cachedBilling (org_id + plan + subscription_status with TTL)
	EdgeIPs               []string // known Fly edge IPs for DNS verification
	CaddyAdmin            *caddyService.Admin
}

func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, eventService events.EventService,
	taskService *asynq.Client, asynqInspector *asynq.Inspector,
	redisClient *redis.Client,
	assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage,
	androidKeysStorage *storage.S3Storage,
	credStore *storage.CredStore,
	secretManager secret.SecretManager,
	flyService *fly.Service, giteaService *gitea.Service,
	neonService *neon.Service,
	neonRegistry *neon.Registry,
	posthogClient posthog.Client,
	gitRepoService *gitrepo.Service,
	bareGitRepoService *gitrepo.BareService,
	codemagicService *codemagic.Service,
	revenueCatService *revenuecat.Service,
	metaCAPI *meta.Service,
	remoteConfigService *remoteconfig.Service,
	e2b *e2bService.Service,
	edgeIPs []string,
	caddyAdmin *caddyService.Admin) *Controller {

	return &Controller{
		DB:                     db,
		SlackService:           slackService,
		EventService:           eventService,
		ConcurrentBuildLimiter: concurrentBuildLimiter,
		TaskService:            taskService,
		AsynqInspector:         asynqInspector,
		RedisPubSub:            redisClient,
		AssetsStorage:          assetsStorage,
		UserApiKeysStorage:     userApiKeysStorage,
		AndroidKeysStorage:     androidKeysStorage,
		CredStore:              credStore,
		SecretManager:          secretManager,
		CodemagicService:       codemagicService,
		RevenueCatService:      revenueCatService,
		MetaCAPI:               metaCAPI,
		FlyService:             flyService,
		FlyInspector:           flyService,
		GiteaService:           giteaService,
		NeonService:            neonService,
		NeonRegistry:           neonRegistry,
		GitRepoService:         gitRepoService,
		BareGitRepoService:     bareGitRepoService,
		PostHog:                posthogClient,
		RemoteConfig:           remoteConfigService,
		E2BService:             e2b,
		EdgeIPs:                edgeIPs,
		CaddyAdmin:             caddyAdmin,
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

	providerName, model := c.parseProviderModel(providerModel)
	fastModel := c.SecretManager.GetSecret("WEBSITE_BUILDER_FAST_MODEL")

	var p *utils.LLMProvider
	switch providerName {
	case "anthropic":
		p = &utils.LLMProvider{Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", APIKey: c.SecretManager.GetSecret("ANTHROPIC_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel, ConfiguredModel: model}
	case "moonshot":
		moonshotReasoning := c.reasoningConfig()
		if !c.RemoteConfig.GetBool(remoteconfig.AIMoonshotThinkingEnabled, remoteconfig.Global) {
			moonshotReasoning = nil
		}
		p = &utils.LLMProvider{Name: "moonshot", BaseURL: "https://api.moonshot.ai/v1", APIKey: c.SecretManager.GetSecret("MOONSHOT_API_KEY"), DefaultReasoning: moonshotReasoning, FastModelOverride: fastModel, ConfiguredModel: model}
	default:
		p = &utils.LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: c.SecretManager.GetSecret("OPENROUTER_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel, ConfiguredModel: model}
	}

	// Every non-OpenRouter primary gets an OpenRouter fallback.
	if !p.IsOpenRouter() {
		c.attachOpenRouterFallback(p, fastModel)
	}

	slog.Info("LLM provider resolved per user", "user_id", user.ID, "tier", tier, "provider", p.Name, "model", model)
	return p
}

// parseProviderModel splits a "provider/model" string into provider name and
// bare model ID. Known providers: anthropic, moonshot, openrouter.
// If the first segment is not a known provider, the whole string is treated as
// an OpenRouter model ID (e.g. "deepseek/deepseek-v4-flash", "google/gemini-2.5-flash-lite").
func (c *Controller) parseProviderModel(providerModel string) (provider, model string) {
	providerModel = strings.TrimSpace(strings.ToLower(providerModel))
	parts := strings.SplitN(providerModel, "/", 2)
	if len(parts) != 2 {
		return "openrouter", providerModel
	}

	switch parts[0] {
	case "anthropic", "moonshot", "openrouter":
		return parts[0], parts[1]
	default:
		// First segment is a vendor namespace on OpenRouter (e.g. "deepseek/...")
		return "openrouter", providerModel
	}
}

// attachOpenRouterFallback wires an OpenRouter fallback onto any primary
// provider when OPENROUTER_API_KEY is configured. The fallback model is read
// from LLM_FALLBACK_MODEL (default: anthropic/claude-sonnet-4.6).
func (c *Controller) attachOpenRouterFallback(primary *utils.LLMProvider, fastModel string) {
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
// Set it in Infisical to control thinking mode: "none", "low", "medium", "high".
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
