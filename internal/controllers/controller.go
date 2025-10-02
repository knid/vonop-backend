package controllers

import (
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/services"
)

type Controller struct {
	DB           *database.Database
	SlackService *services.SlackService
}

func NewController(db *database.Database, slackService *services.SlackService) *Controller {
	return &Controller{
		DB:           db,
		SlackService: slackService,
	}
}
