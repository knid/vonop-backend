package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/utils"
)

type GenerateAppRequest struct {
	Prompt      string `json:"prompt" validate:"required,min=10" errormgs:"must be at least 10 characters long"`
	ProjectType string `json:"project_type" validate:"omitempty,oneof=app ecommerce mobile_app" errormgs:"must be 'app', 'ecommerce', or 'mobile_app'"`
}

func (r *GenerateAppRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[GenerateAppRequest](*r, validate)
}

type UpdateAppProjectRequest struct {
	Name     string         `json:"name" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters long"`
	Slug     string         `json:"slug" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters long"`
	Settings map[string]any `json:"settings,omitempty"`
}

func (r *UpdateAppProjectRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateAppProjectRequest](*r, validate)
}

type AppChatRequest struct {
	Message         string   `json:"message" validate:"required,min=2" errormgs:"must be at least 2 characters long"`
	Images          []string `json:"images,omitempty"`           // base64 encoded images
	ReasoningEffort string   `json:"reasoning_effort,omitempty"` // "none", "medium", "high"
}

func (r *AppChatRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AppChatRequest](*r, validate)
}

type ConnectIntegrationRequest struct {
	Provider    string         `json:"provider" validate:"required" errormgs:"is required"`
	Credentials map[string]any `json:"credentials" validate:"required" errormgs:"is required"`
}

func (r *ConnectIntegrationRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ConnectIntegrationRequest](*r, validate)
}

type UpdateIntegrationRequest struct {
	Credentials map[string]any `json:"credentials" validate:"required" errormgs:"is required"`
	Config      map[string]any `json:"config"`
}

func (r *UpdateIntegrationRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateIntegrationRequest](*r, validate)
}

type DeployWebRequest struct {
	Target string `json:"target" validate:"omitempty,oneof=production preview" errormgs:"must be 'production' or 'preview'"`
}

type DeployFlutterRequest struct {
	PackageName string `json:"package_name" validate:"required,min=3" errormgs:"must be at least 3 characters"`
	AppName     string `json:"app_name" validate:"required,min=2" errormgs:"must be at least 2 characters"`
}

func (r *DeployFlutterRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[DeployFlutterRequest](*r, validate)
}

type CreateAppSnapshotRequest struct {
	Description string `json:"description" validate:"required,min=2,max=200" errormgs:"must be between 2 and 200 characters long"`
}

func (r *CreateAppSnapshotRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateAppSnapshotRequest](*r, validate)
}

type AddAppDomainRequest struct {
	Domain string `json:"domain" validate:"required,min=4,max=253" errormgs:"must be a valid domain"`
}

func (r *AddAppDomainRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AddAppDomainRequest](*r, validate)
}

type ScreenshotToAppRequest struct {
	ImageBase64 string `json:"image_base64" validate:"required" errormgs:"is required"`
}

func (r *ScreenshotToAppRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ScreenshotToAppRequest](*r, validate)
}

type AppFileCreateRequest struct {
	FilePath string `json:"file_path" validate:"required,min=2" errormgs:"must be at least 2 characters"`
	Content  string `json:"content" validate:"required" errormgs:"is required"`
	FileType string `json:"file_type" validate:"required,oneof=tsx ts css json" errormgs:"must be tsx, ts, css, or json"`
}

func (r *AppFileCreateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AppFileCreateRequest](*r, validate)
}

type AppFileUpdateRequest struct {
	Content string `json:"content" validate:"required" errormgs:"is required"`
}

func (r *AppFileUpdateRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AppFileUpdateRequest](*r, validate)
}

type SetEnvVarRequest struct {
	Key      string `json:"key" validate:"required,min=1,max=100" errormgs:"must be between 1 and 100 characters"`
	Value    string `json:"value" validate:"required" errormgs:"is required"`
	IsPublic bool   `json:"is_public"`
}

func (r *SetEnvVarRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[SetEnvVarRequest](*r, validate)
}

type UpdateEnvVarRequest struct {
	Value    *string `json:"value"`
	IsPublic *bool   `json:"is_public"`
}

type ReplyJobRequest struct {
	Message string `json:"message" validate:"required,min=1" errormgs:"message is required"`
}

func (r *ReplyJobRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ReplyJobRequest](*r, validate)
}

type ClarifyJobAnswer struct {
	ID     string `json:"id" validate:"required" errormgs:"is required"`
	Answer string `json:"answer" validate:"required" errormgs:"is required"`
}

type ClarifyJobRequest struct {
	Answers []ClarifyJobAnswer `json:"answers" validate:"omitempty,dive" errormgs:"must contain valid answers"`
	Skip    bool               `json:"skip"`
}

func (r *ClarifyJobRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ClarifyJobRequest](*r, validate)
}
