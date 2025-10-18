package responses

import (
	"time"

	"github.com/google/uuid"
)

type UserNotificationResponse struct {
	ID        uuid.UUID `json:"id"`
	Content   string    `json:"content"`
	Readed    bool      `json:"readed"`
	CreatedAt time.Time `json:"created_at"`
}
