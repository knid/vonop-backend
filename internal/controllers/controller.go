package controllers

import (
	"github.com/knid/wemob/internal/database"
)

type Controller struct {
	DB *database.Database
}

func NewController(db *database.Database) *Controller {
	return &Controller{
		DB: db,
	}
}
