package responses

import (
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

type UserSubscriptionInformationResponse struct {
	ID     uuid.UUID                 `json:"id"`
	Name   string                    `json:"name"`
	Price  int                       `json:"price"`
	Limits models.StandartUserLimits `json:"limits"`
}

type UserSubscriptionUpdateResponse struct {
	SuccessResponse
}

type UserSubscriptionCancelResponse struct {
	SuccessResponse
}
