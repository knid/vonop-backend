package controllers

import (
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
	crmServices "github.com/knid/wemob/internal/services/crm"
	emailServices "github.com/knid/wemob/internal/services/email"
	"github.com/knid/wemob/internal/services/storage"
)

type Controller struct {
	DB                     *database.Database
	SlackService           *services.SlackService
	EmailService           emailServices.EmailService
	CRMService             crmServices.CRMService
	TaskService            *asynq.Client
	AssetsStorage          *storage.S3Storage
	UserApiKeysStorage     *storage.S3Storage
	ConcurrentBuildLimiter chan struct{}
}

func NewController(db *database.Database, concurrentBuildLimiter chan struct{},
	slackService *services.SlackService, emailService emailServices.EmailService,
	crmService crmServices.CRMService, taskService *asynq.Client, assetsStorage *storage.S3Storage, userApiKeysStorage *storage.S3Storage) *Controller {

	return &Controller{
		DB:                     db,
		SlackService:           slackService,
		EmailService:           emailService,
		CRMService:             crmService,
		ConcurrentBuildLimiter: concurrentBuildLimiter,
		TaskService:            taskService,
		AssetsStorage:          assetsStorage,
		UserApiKeysStorage:     userApiKeysStorage,
	}
}
