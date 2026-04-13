package controllers

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
	crmServices "github.com/knid/wemob/internal/services/crm"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/neon"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/coolify"
	"github.com/knid/wemob/internal/services/gitea"
	"github.com/knid/wemob/internal/services/remoteconfig"
	e2bService "github.com/knid/wemob/internal/services/e2b"
	"github.com/posthog/posthog-go"
)

type Controller struct {
	DB                     *database.Database
	SecretManager          secret.SecretManager
	SlackService           *services.SlackService
	EmailService           emailServices.EmailService
	CRMService             crmServices.CRMService
	TaskService            *asynq.Client
	AssetsStorage          *storage.S3Storage
	UserApiKeysStorage     *storage.S3Storage
	ConcurrentBuildLimiter chan struct{}
	CodemagicService       *codemagic.Service
	CoolifyService         *coolify.Service
	GiteaService           *gitea.Service
	NeonService            *neon.Service
	GitRepoService         *gitrepo.Service
	MCPService             atomic.Pointer[mcpclient.Service]
	PostHog                posthog.Client // nil = disabled
	RemoteConfig           *remoteconfig.Service
	E2BService             *e2bService.Service
	RunningJobs            sync.Map       // map[string]context.CancelFunc for in-flight background jobs
	PendingClarifications  sync.Map       // map[string]chan ClarificationResult for jobs awaiting user input
	JobEventBuses          sync.Map       // map[string]*EventBus for live SSE fan-out to multiple clients
	identifiedUsers        sync.Map       // tracks users already identified to PostHog this process
}

func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage, secretManager secret.SecretManager,
	coolifyService *coolify.Service, giteaService *gitea.Service,
	neonService *neon.Service,
	posthogClient posthog.Client,
	gitRepoService *gitrepo.Service,
	codemagicService *codemagic.Service,
	remoteConfigService *remoteconfig.Service,
	e2b *e2bService.Service) *Controller {

	return &Controller{
		DB:                     db,
		SlackService:           slackService,
		EmailService:           emailService,
		CRMService:             crmService,
		ConcurrentBuildLimiter: concurrentBuildLimiter,
		TaskService:            taskService,
		AssetsStorage:          assetsStorage,
		UserApiKeysStorage:     userApiKeysStorage,
		SecretManager:          secretManager,
		CodemagicService:       codemagicService,
		CoolifyService:         coolifyService,
		GiteaService:           giteaService,
		NeonService:            neonService,
		GitRepoService:         gitRepoService,
		PostHog:                posthogClient,
		RemoteConfig:           remoteConfigService,
		E2BService:             e2b,
	}
}

// llmProvider resolves the active LLM provider from the LLM_PROVIDER secret.
// Supported values: "openrouter" (default), "anthropic", "google", "moonshot", "zai".
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
		c.attachOverloadFallback(p, fastModel)
		return p
	case "google":
		return &utils.LLMProvider{Name: "google", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKey: c.SecretManager.GetSecret("GOOGLE_API_KEY"), FastModelOverride: fastModel}
	case "moonshot":
		return &utils.LLMProvider{Name: "moonshot", BaseURL: "https://api.moonshot.ai/v1", APIKey: c.SecretManager.GetSecret("MOONSHOT_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel}
	case "zai":
		return &utils.LLMProvider{Name: "zai", BaseURL: "https://api.z.ai/api/paas/v4", APIKey: c.SecretManager.GetSecret("ZAI_API_KEY"), DefaultReasoning: c.reasoningConfig(), FastModelOverride: fastModel}
	default:
		slog.Warn("Unknown or empty LLM_PROVIDER, falling back to openrouter", "raw", raw)
		return &utils.LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: c.SecretManager.GetSecret("OPENROUTER_API_KEY"), FastModelOverride: fastModel}
	}
}

// attachOverloadFallback wires an OpenRouter fallback onto an Anthropic-native
// provider when OPENROUTER_API_KEY is configured. The fallback model is read
// from LLM_FALLBACK_MODEL (default: anthropic/claude-sonnet-4.6).
func (c *Controller) attachOverloadFallback(primary *utils.LLMProvider, fastModel string) {
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
