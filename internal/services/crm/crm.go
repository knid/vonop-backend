package crm

import "github.com/knid/wemob/internal/models"

type CRMService interface {
	SendEvent(user *models.User, event string, attributes map[string]any) error
}
