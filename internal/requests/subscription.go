package requests

import "github.com/google/uuid"

type UserSubscriptionUpdateRequest struct {
	SubscriptionID uuid.UUID  `json:"uuid" validate:"uuid4,required"`
	OrganizationID *uuid.UUID `json:"org_id,omitempty"`
}

type UserSubscriptionCancelRequest struct {
	OrganizationID *uuid.UUID `json:"org_id,omitempty"`
}
