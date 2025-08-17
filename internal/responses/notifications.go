package responses

import (
	"time"

	"github.com/google/uuid"
)

type NotificationInformationResponse struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Image       string    `json:"image"`
	CreatedAt   time.Time `json:"created_at"`
	SentAt      time.Time `json:"sent_at"`
}

type NotificationListResponse struct {
	NotificationList []*NotificationInformationResponse `json:"notification_list"`
}

type NotificationSendResponse struct {
	SuccessResponse
}
