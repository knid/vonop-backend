package requests

import "github.com/google/uuid"

type LogAppEventRequest struct {
	SessionID uuid.UUID      `json:"session_id"`
	EventType string         `json:"event_type"`
	Data      map[string]any `json:"data"`
}

type CreateSessionRequest struct {
	DeviceID   uuid.UUID `json:"device_id"`
	AppVersion string    `json:"app_version"`
	Platform   string    `json:"platform"`
	Model      string    `json:"model"`
}
