package responses

import (
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
