package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type CreateAppTemplateRequest struct {
	Name         string         `json:"name" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Slug         string         `json:"slug" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Description  string         `json:"description" validate:"omitempty,max=2000" errormgs:"must be at most 2000 characters"`
	Tags         []string       `json:"tags"`
	TemplateType string         `json:"template_type" validate:"required,oneof=web mobile both" errormgs:"must be 'web', 'mobile', or 'both'"`
	ProjectType  string         `json:"project_type" validate:"required,oneof=app ecommerce mobile_app" errormgs:"must be 'app', 'ecommerce', or 'mobile_app'"`
	Prompt       string         `json:"prompt"`
	Theme        map[string]any `json:"theme"`
	DataModels   []any          `json:"data_models"`
	Settings     map[string]any `json:"settings"`
}

func (r *CreateAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateAppTemplateRequest](*r, validate)
}

type UpdateAppTemplateRequest struct {
	Name            string         `json:"name" validate:"omitempty,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Slug            string         `json:"slug" validate:"omitempty,min=2,max=100" errormgs:"must be between 2 and 100 characters"`
	Description     string         `json:"description" validate:"omitempty,max=2000" errormgs:"must be at most 2000 characters"`
	Tags            []string       `json:"tags"`
	TemplateType    string         `json:"template_type" validate:"omitempty,oneof=web mobile both" errormgs:"must be 'web', 'mobile', or 'both'"`
	ProjectType     string         `json:"project_type" validate:"omitempty,oneof=app ecommerce mobile_app" errormgs:"must be 'app', 'ecommerce', or 'mobile_app'"`
	Prompt          string         `json:"prompt"`
	PreviewImageKey string         `json:"preview_image_key"`
	PreviewURL      string         `json:"preview_url"`
	Theme           map[string]any `json:"theme"`
	DataModels      []any          `json:"data_models"`
	Settings        map[string]any `json:"settings"`
}

func (r *UpdateAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateAppTemplateRequest](*r, validate)
}

type PublishAppTemplateRequest struct {
	Published bool `json:"published"`
}

type SortAppTemplateRequest struct {
	SortOrder int `json:"sort_order" validate:"min=0" errormgs:"must be 0 or greater"`
}

func (r *SortAppTemplateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[SortAppTemplateRequest](*r, validate)
}
