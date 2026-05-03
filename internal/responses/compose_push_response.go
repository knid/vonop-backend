package responses

import (
	"time"

	"github.com/google/uuid"
)

// ComposePushResponse is returned from POST /v2/apps/{appId}/push/compose.
type ComposePushResponse struct {
	NotificationID  string `json:"notification_id"`
	RecipientsCount int    `json:"recipients_count"`
}

// ComposePushHistoryItem is one row of GET /v2/apps/{appId}/push/history.
type ComposePushHistoryItem struct {
	ID                      uuid.UUID `json:"id"`
	Title                   string    `json:"title"`
	Body                    string    `json:"body"`
	AudienceType            string    `json:"audience_type"`
	AudienceIDs             []string  `json:"audience_ids"`
	OneSignalNotificationID string    `json:"onesignal_notification_id"`
	RecipientsCount         int       `json:"recipients_count"`
	SenderUserID            uuid.UUID `json:"sender_user_id"`
	CreatedAt               time.Time `json:"created_at"`
}

// ComposePushHistoryResponse wraps the page + total count.
type ComposePushHistoryResponse struct {
	Items []ComposePushHistoryItem `json:"items"`
	Total int                      `json:"total"`
}
