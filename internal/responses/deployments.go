package responses

import (
	"time"

	"github.com/google/uuid"
)

type DeploymentResponse struct {
	ID                      uuid.UUID `json:"id"`
	UserID                  uuid.UUID `json:"user_id"`
	ProjectID               uuid.UUID `json:"project_id"`
	BuildID                 uuid.UUID `json:"build_id"`
	Platforms               []string  `json:"platforms"`
	IconID                  string    `json:"icon_id"`
	Title                   string    `json:"title"`
	Description             string    `json:"description"`
	Status                  string    `json:"status"`
	StatusMessage           string    `json:"status_message"`
	ScreenshotIDs           []string  `json:"screenshot_ids"`
	Method                  string    `json:"method"`
	Locale                  string    `json:"locale"`
	AppleConnectionID       uuid.UUID `json:"app_store_credentials_id"`
	GooglePlayConnectionID  uuid.UUID `json:"google_play_credentials_id"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}
