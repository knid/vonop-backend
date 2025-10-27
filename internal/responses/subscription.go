package responses

import (
	"github.com/google/uuid"
)

type UserSubscriptionInformationResponse struct {
	ID     uuid.UUID      `json:"id"`
	Name   string         `json:"name"`
	Price  int            `json:"price"`
	Limits LimitsResponse `json:"limits"`
}

type UserSubscriptionUpdateResponse struct {
	SuccessResponse
}

type UserSubscriptionCancelResponse struct {
	SuccessResponse
}
