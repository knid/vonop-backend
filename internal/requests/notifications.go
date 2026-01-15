package requests

import "github.com/google/uuid"

type NotificationCreateRequest struct {
	ProjectID   uuid.UUID `json:"project_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Image       string    `json:"image"`
	URL         string    `json:"url"`
	SendAfter   string    `json:"send_after"`
}
