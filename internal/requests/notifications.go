package requests

import "github.com/google/uuid"

type NotificationCreateRequest struct {
	ProjectID   uuid.UUID
	Title       string
	Description string
	Image       string
}
