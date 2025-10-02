package requests

import "github.com/google/uuid"

type LogAppEventRequest struct {
	EventType  string    `json:"event_type"`
	DeviceID   uuid.UUID `json:"device_id"`
	AppVersion string    `json:"app_version"`
	Platform   string    `json:"platform"`
	Model      string    `json:"model"`
}
