package requests

import "github.com/google/uuid"

type NotificationCreateRequest struct {
	AppID       uuid.UUID
	Title       string
	Description string
	Image       string
}
