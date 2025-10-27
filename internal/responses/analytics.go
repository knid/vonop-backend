package responses

import (
	"time"

	"github.com/google/uuid"
)

type CreateSessionResponse struct {
	SessionID uuid.UUID `json:"session_id"`
}

type LogAppEventResponse struct {
	SuccessResponse
}

type GetDeviceResponse struct {
	ID        uuid.UUID `json:"id"`
	Platform  string    `json:"os"`
	Model     string    `json:"device_type"`
	CreatedAt string    `json:"created_at"`
}

type GetDevicesResponse struct {
	Devices []GetDeviceResponse `json:"devices"`
}

type GetSessionResponse struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"project_id"`
	DeviceID    uuid.UUID `json:"device_id"`
	AppVersion  string    `json:"app_version"`
	IP          string    `json:"ip"`
	Geolocation string    `json:"geolocation"`
	Duration    int64     `json:"duration"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type GetProjectEventResponse struct {
	ID        uuid.UUID      `json:"id"`
	EventType string         `json:"event_type"`
	SessionID uuid.UUID      `json:"session_id"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
}
