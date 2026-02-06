package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

type WebsiteGenerateRequest struct {
	Prompt     string     `json:"prompt" validate:"required,min=10" errormgs:"must be at least 10 characters long"`
	TemplateID *uuid.UUID `json:"template_id"`
}

func (r *WebsiteGenerateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[WebsiteGenerateRequest](*r, validate)
}

type WebsiteUpdateRequest struct {
	Name string `json:"name" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters long"`
	Slug string `json:"slug" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters long"`
}

func (r *WebsiteUpdateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[WebsiteUpdateRequest](*r, validate)
}

type PageCreateRequest struct {
	Path        string `json:"path" validate:"required" errormgs:"is required"`
	Title       string `json:"title" validate:"required,min=1,max=100" errormgs:"must be between 1 and 100 characters long"`
	HTMLContent string `json:"html_content" validate:"required" errormgs:"is required"`
}

func (r *PageCreateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[PageCreateRequest](*r, validate)
}

type PageUpdateRequest struct {
	Title       string `json:"title" validate:"required,min=1,max=100" errormgs:"must be between 1 and 100 characters long"`
	HTMLContent string `json:"html_content" validate:"required" errormgs:"is required"`
}

func (r *PageUpdateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[PageUpdateRequest](*r, validate)
}

type AIEditPageRequest struct {
	Prompt string `json:"prompt" validate:"required,min=5" errormgs:"must be at least 5 characters long"`
}

func (r *AIEditPageRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AIEditPageRequest](*r, validate)
}

type AIAddPageRequest struct {
	Prompt string `json:"prompt" validate:"required,min=5" errormgs:"must be at least 5 characters long"`
}

func (r *AIAddPageRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AIAddPageRequest](*r, validate)
}

type ConvertToMobileRequest struct {
	AppName         string `json:"app_name" validate:"required,min=3,max=30" errormgs:"must be between 3 and 30 characters long"`
	BuildForAndroid bool   `json:"build_for_android"`
	BuildForIOS     bool   `json:"build_for_ios"`
}

func (r *ConvertToMobileRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ConvertToMobileRequest](*r, validate)
}
