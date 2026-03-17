package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

type LogAppEventRequest struct {
	SessionID string         `json:"session_id" validate:"required,min=1,max=64" errormgs:"is required"`
	EventType string         `json:"event_type" validate:"required,oneof=app_opened session_started session_ended url_visited ping push_notification_received push_notification_clicked" errormgs:"must be a valid event type"`
	Data      map[string]any `json:"data"`
}

func (r *LogAppEventRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[LogAppEventRequest](*r, validate)
}

type CreateSessionRequest struct {
	DeviceID   uuid.UUID `json:"device_id" validate:"required" errormgs:"is required"`
	AppVersion string    `json:"app_version" validate:"required,max=50" errormgs:"is required and must be at most 50 characters"`
	Platform   string    `json:"platform" validate:"required,max=20" errormgs:"is required and must be at most 20 characters"`
	Model      string    `json:"model" validate:"required,max=100" errormgs:"is required and must be at most 100 characters"`
}

func (r *CreateSessionRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateSessionRequest](*r, validate)
}
