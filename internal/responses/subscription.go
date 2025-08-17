package responses

import "github.com/google/uuid"

type UserSubscriptionInformationResponse struct {
	ID uuid.UUID `json:"id"`
}

type UserSubscriptionUpdateResponse struct {
	SuccessResponse
}

type UserSubscriptionCancelResponse struct {
	SuccessResponse
}
