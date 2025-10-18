package services

import "github.com/knid/wemob/internal/models"

type EmailService interface {
	SendEmail(user *models.User, templateID int, params map[string]any) (string, error)
}
