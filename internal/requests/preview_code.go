package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type GeneratePreviewCodeRequest struct {
	AppProjectID string `json:"app_project_id" validate:"required,uuid" errormgs:"must be a valid app project UUID"`
}

func (r *GeneratePreviewCodeRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[GeneratePreviewCodeRequest](*r, validate)
}
