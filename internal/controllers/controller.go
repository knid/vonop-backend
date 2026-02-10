package controllers

import (
	"sync"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
	crmServices "github.com/knid/wemob/internal/services/crm"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services/preview"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/storage"
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
	RunningJobs            sync.Map // map[string]context.CancelFunc for in-flight background jobs
}

func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage,
	userApiKeysStorage *storage.S3Storage, secretManager secret.SecretManager,
	vercelService *vercel.Service, previewService *preview.Service) *Controller {

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
	}
}
