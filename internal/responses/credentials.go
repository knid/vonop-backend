package responses

import (
	"time"

	"github.com/google/uuid"
)

type AppStoreCredentialsResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	KeyID     string    `json:"key_id"`
	TeamID    string    `json:"team_id"`
	IssuerID  string    `json:"issuer_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type GooglePlayCredentialsResponse struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
