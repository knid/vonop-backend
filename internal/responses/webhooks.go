package responses

import (
	"time"

	"github.com/google/uuid"
)

type WebhookEndpointResponse struct {
	ID        uuid.UUID `json:"id"`
	URL       string    `json:"url"`
	Events    string    `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// WebhookEndpointCreatedResponse includes the signing secret (only shown once).
type WebhookEndpointCreatedResponse struct {
	WebhookEndpointResponse
	Secret string `json:"secret"`
}
