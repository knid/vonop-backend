package responses

import (
	"time"

	"github.com/google/uuid"
)

type PreviewCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type PreviewCodeRedeemResponse struct {
	PreviewURL   string    `json:"preview_url"`
	AppProjectID uuid.UUID `json:"app_project_id"`
	AppName      string    `json:"app_name"`
	AppIcon      string    `json:"app_icon,omitempty"`
	SandboxID    string    `json:"sandbox_id,omitempty"`
}
