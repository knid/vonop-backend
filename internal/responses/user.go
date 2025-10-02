package responses

import (
	"time"

	"github.com/google/uuid"
)

type UserInformationResponse struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	Photo              string    `json:"profile_photo"`
	Verified           bool      `json:"verified"`
	Provider           string    `json:"provider"`
	SubscriptionID     string    `json:"subscription_id"`
	SubscriptionStatus string    `json:"subscription_status"`
}

type UserLoginResponse struct {
	Token     string    `json:"token"`
	ExpiredAt time.Time `json:"expired_at"`
}

type UserUpdateResponse struct {
	UserInformationResponse
}

type UserRegisterResponse struct {
	UserInformationResponse
}

type UserForgotPasswordResponse struct {
	SuccessResponse
}

type UserVerifyEmailResponse struct {
	SuccessResponse
}

type UserResendVerifactionEmailResponse struct {
	SuccessResponse
}

type UserDeleteResponse struct {
	SuccessResponse
}
