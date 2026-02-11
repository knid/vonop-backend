package controllers

import (
	"sync"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
	crmServices "github.com/knid/wemob/internal/services/crm"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services/mcpclient"
	"github.com/knid/wemob/internal/services/preview"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
	"github.com/knid/wemob/internal/utils"
	"github.com/knid/wemob/internal/services/vercel"
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
	VercelService          *vercel.Service
	PreviewService         *preview.Service
	MCPService             *mcpclient.Service
	RunningJobs            sync.Map // map[string]context.CancelFunc for in-flight background jobs
}

func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage, secretManager secret.SecretManager,
	vercelService *vercel.Service, previewService *preview.Service,
	mcpService *mcpclient.Service) *Controller {

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
		VercelService:          vercelService,
		PreviewService:         previewService,
		MCPService:             mcpService,
	}
}

// llmProvider resolves the active LLM provider from the LLM_PROVIDER secret.
// Supported values: "openrouter" (default), "anthropic", "google", "moonshot", "zai".
func (c *Controller) llmProvider() *utils.LLMProvider {
	switch c.SecretManager.GetSecret("LLM_PROVIDER") {
	case "anthropic":
		return &utils.LLMProvider{Name: "anthropic", BaseURL: "https://api.anthropic.com/v1", APIKey: c.SecretManager.GetSecret("ANTHROPIC_API_KEY")}
	case "google":
		return &utils.LLMProvider{Name: "google", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKey: c.SecretManager.GetSecret("GOOGLE_API_KEY")}
	case "moonshot":
		return &utils.LLMProvider{Name: "moonshot", BaseURL: "https://api.moonshot.ai/v1", APIKey: c.SecretManager.GetSecret("MOONSHOT_API_KEY")}
	case "zai":
		return &utils.LLMProvider{Name: "zai", BaseURL: "https://api.z.ai/api/paas/v4", APIKey: c.SecretManager.GetSecret("ZAI_API_KEY")}
	default:
		return &utils.LLMProvider{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: c.SecretManager.GetSecret("OPENROUTER_API_KEY")}
	}
}

// LLMProvider is the exported version of llmProvider for use in views.
func (c *Controller) LLMProvider() *utils.LLMProvider {
	return c.llmProvider()
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
