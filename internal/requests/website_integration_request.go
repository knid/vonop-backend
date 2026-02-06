package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type IntegrationConnectRequest struct {
	Provider    string         `json:"provider" validate:"required,oneof=supabase firebase stripe iyzico resend cloudinary google_sheets" errormgs:"must be a supported provider"`
	Credentials map[string]any `json:"credentials" validate:"required" errormgs:"is required"`
	Config      map[string]any `json:"config"`
}

func (r *IntegrationConnectRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[IntegrationConnectRequest](*r, validate)
}

type IntegrationUpdateRequest struct {
	Credentials map[string]any `json:"credentials"`
	Config      map[string]any `json:"config"`
}

func (r *IntegrationUpdateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[IntegrationUpdateRequest](*r, validate)
}
