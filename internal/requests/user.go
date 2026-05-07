package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type UserInfoRequest struct {
	Token string `json:"token" validate:"required"`
}

type UserUpdateRequest struct {
	Name     string `json:"name" validate:"min=3,max=30,required" errormgs:"must be between 3 and 30 characters long"`
	Email    string `json:"email" validate:"email,required" errormgs:"must be a valid email address"`
	Company  string `json:"company"`
	Type     string `json:"type"`
	Photo    string `json:"photo" validate:"required"`
	Language string `json:"language" validate:"omitempty,oneof=en tr" errormgs:"must be 'en' or 'tr'"`
}

type UserRegisterRequest struct {
	Name          string `json:"name" validate:"min=3,max=30,required" errormgs:"must be between 3 and 30 characters long"`
	Email         string `json:"email" validate:"email,required" errormgs:"must be a valid email address"`
	Company       string `json:"company"`
	Type          string `json:"type"`
	Photo         string `json:"photo"`
	Password      string `json:"password" validate:"required,min=8,max=64" errormgs:"must be between 8 and 64 characters long"`
	PasswordCheck string `json:"password_check" validate:"required,min=8,max=64" errormgs:"must be between 8 and 64 characters long"`
	ReferralCode  string `json:"referral_code"`
	// Meta CAPI ad attribution from the front-end Pixel. All optional.
	Fbp            string `json:"fbp"`
	Fbc            string `json:"fbc"`
	EventSourceURL string `json:"event_source_url"`
}

func (u *UserRegisterRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UserRegisterRequest](*u, validate)
}

type UserLoginRequest struct {
	Email    string `json:"email" validate:"email,required"`
	Password string `json:"password" validate:"required,min=8,max=64"`
}

type UserForgotPasswordRequest struct {
	Email string `json:"email" validate:"email,required"`
}

type UserResetPasswordRequest struct {
	Token         string `json:"token" validate:"required"`
	Password      string `json:"password" validate:"required,min=8,max=64" errormgs:"must be between 8 and 64 characters long"`
	PasswordCheck string `json:"password_check" validate:"required,min=8,max=64" errormgs:"must be between 8 and 64 characters long"`
}

type UserVerifyEmailRequest struct {
	Token string `json:"token" validate:"required"`
}

type UserResendVerifyEmailRequest struct {
	Email string `json:"email" validate:"email,required"`
}
