package requests

import "github.com/google/uuid"

type UserSubscriptionUpdateRequest struct {
	SubscriptionID uuid.UUID `json:"uuid" validator:"uuid4,required"`
}
