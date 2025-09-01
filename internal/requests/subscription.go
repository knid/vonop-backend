package requests

import "github.com/google/uuid"

type UserSubscriptionUpdateRequest struct {
	SubscriptionID uuid.UUID `json:"uuid" validate:"uuid4,required"`
}
