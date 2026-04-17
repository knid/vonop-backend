package responses

import (
	"time"

	"github.com/google/uuid"
)

type BuildTaskInformationResponse struct {
	ID           uuid.UUID `json:"id"`
	ProjectID    uuid.UUID `json:"project_id"`
	BuildID      uuid.UUID `json:"build_id"`
	Finished     bool      `json:"finished"`
	Platform     string    `json:"platform"`
	Status       string    `json:"status"`
	AutoDeploy   bool      `json:"auto_deploy"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
