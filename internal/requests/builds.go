package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

type BuildCreateRequest struct {
	ProjectID           uuid.UUID `json:"-"`
	Title               string    `json:"title" validate:"required,min=2" errormgs:"app name is required and must be at least 2 characters"`
	Description         string    `json:"description" validate:"required,min=2" errormgs:"description is required and must be at least 2 characters"`
	URL                 string    `json:"url"`
	Icon                string    `json:"icon" validate:"required" errormgs:"app icon is required"`
	SplashScreenColor   string    `json:"splash_screen_color"`
	NavBarStyle         string    `json:"navbar_style"`
	RemoveWemobBranding bool      `json:"remove_wemob_branding"`
}

func (r *BuildCreateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[BuildCreateRequest](*r, validate)
}
