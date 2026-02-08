package responses

import (
	"time"

	"github.com/google/uuid"
)

type APIKeyResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	KeyPrefix string    `json:"key_prefix"`
	Scopes    string    `json:"scopes"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// APIKeyCreatedResponse includes the full key (only shown once at creation).
type APIKeyCreatedResponse struct {
	APIKeyResponse
	Key string `json:"key"`
}
