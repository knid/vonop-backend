package responses

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type JobCreatedResponse struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}

type JobResponse struct {
	ID          uuid.UUID       `json:"id"`
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Events      json.RawMessage `json:"events"`
	StreamState json.RawMessage `json:"stream_state,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}
