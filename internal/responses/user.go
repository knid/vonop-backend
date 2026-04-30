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
	Type               string    `json:"type"`
	Company            string    `json:"company"`
	CompanyLogo        string    `json:"company_logo"`
	SubscriptionID     string    `json:"subscription_id"`
	SubscriptionStatus string    `json:"subscription_status"`
	SubscribedAt       time.Time `json:"subscribed_at"`
	TrialStartedAt     time.Time `json:"trial_started_at"`
	TrialUsed          bool      `json:"trial_used"`
	SetupFinished      bool      `json:"setup_finished"`
	Language           string    `json:"language"`
	CreatedAt          time.Time `json:"created_at"`
	IsNewUser          bool      `json:"is_new_user"`
}

type UserLoginResponse struct {
	Token     string    `json:"token"`
	ExpiredAt time.Time `json:"expired_at"`
	IsNewUser bool      `json:"is_new_user,omitempty"`
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
