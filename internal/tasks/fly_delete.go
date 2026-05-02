package tasks

import "github.com/google/uuid"

const (
	TypeDeleteFlyApp = "fly:delete-app"
)

type FlyDeleteAppPayload struct {
	FlyAppName   string    `json:"fly_app_name"`
	RuntimeAppID uuid.UUID `json:"runtime_app_id"`
}
