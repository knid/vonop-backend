package requests

import (
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

type GenerateAppRequest struct {
	Prompt          string     `json:"prompt" validate:"required" errormgs:"is required"`
	ProjectType     string     `json:"project_type" validate:"omitempty,oneof=app ecommerce mobile_app_native" errormgs:"must be 'app', 'ecommerce', or 'mobile_app_native'"`
	FrontendRuntime string     `json:"frontend_runtime,omitempty" validate:"omitempty,oneof=nextjs_legacy vite_react expo_native none" errormgs:"must be a supported frontend runtime"`
	Images          []string   `json:"images,omitempty"`
	OrganizationID  *uuid.UUID `json:"organization_id,omitempty"`
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
	Message string `json:"message" validate:"required,min=2" errormgs:"must be at least 2 characters long"`

	// AttachmentIDs are AppAsset UUIDs the user attached to this turn. The
	// view resolves them, enforces per-turn caps, and passes the resulting
	// AppAsset rows down to agent.Run.
	AttachmentIDs []uuid.UUID `json:"attachment_ids,omitempty"`

	// Images is the legacy field — was a list of proxy URLs (the comment
	// "base64 encoded" was always wrong). Decoded into AttachmentIDs by
	// extracting the asset UUID from the proxy URL pattern. Drops on the
	// next release once frontend traffic stops using it.
	//
	// Deprecated: use AttachmentIDs.
	Images []string `json:"images,omitempty"`

	ReasoningEffort string `json:"reasoning_effort,omitempty"` // "none", "medium", "high"
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

// ExpoBuildRequest is the body for POST /v2/apps/{appId}/build/expo.
type ExpoBuildRequest struct {
	Platforms        []string `json:"platforms" validate:"required,min=1,dive,oneof=android ios" errormgs:"platforms must include android and/or ios"`
	AppName          string   `json:"app_name" validate:"required,min=1,max=100" errormgs:"must be between 1 and 100 characters"`
	Icon             string   `json:"icon"`
	SplashColor      string   `json:"splash_color"`
	MarketingVersion string   `json:"marketing_version"`
	BundleIDOverride string   `json:"bundle_id_override"`
}

func (r *ExpoBuildRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ExpoBuildRequest](*r, validate)
}

type CreateAppSnapshotRequest struct {
	Description string `json:"description" validate:"required,min=2,max=200" errormgs:"must be between 2 and 200 characters long"`
}

func (r *CreateAppSnapshotRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateAppSnapshotRequest](*r, validate)
}

type AddAppDomainRequest struct {
	Domain string `json:"domain" validate:"required,min=4,max=253,fqdn" errormgs:"must be a valid domain name"`
}

func (r *AddAppDomainRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AddAppDomainRequest](*r, validate)
}

type ScreenshotToAppRequest struct {
	ImageBase64 string `json:"image_base64" validate:"required" errormgs:"is required"`
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
	Key      string `json:"key" validate:"required,min=1,max=100,env_var_key" errormgs:"must be a valid env var name (uppercase letters, digits, underscores)"`
	Value    string `json:"value" validate:"required,max=10000" errormgs:"is required and must be at most 10000 characters"`
	IsPublic bool   `json:"is_public"`
}

func (r *SetEnvVarRequest) Validate(validate *validator.Validate) error {
	validate.RegisterValidation("env_var_key", validateEnvVarKey)
	return utils.ValidateFunc[SetEnvVarRequest](*r, validate)
}

// validateEnvVarKey checks that the key matches [A-Z_][A-Z0-9_]* pattern.
func validateEnvVarKey(fl validator.FieldLevel) bool {
	key := fl.Field().String()
	if len(key) == 0 {
		return false
	}
	for i, c := range key {
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || c == '_') {
				return false
			}
		} else {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
				return false
			}
		}
	}
	return true
}

type UpdateEnvVarRequest struct {
	Value    *string `json:"value" validate:"omitempty,max=10000" errormgs:"must be at most 10000 characters"`
	IsPublic *bool   `json:"is_public"`
}

func (r *UpdateEnvVarRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateEnvVarRequest](*r, validate)
}

type ReplyJobRequest struct {
	Message string `json:"message" validate:"required,min=1" errormgs:"message is required"`
}

type ClarifyJobAnswer struct {
	ID     string `json:"id" validate:"required" errormgs:"is required"`
	Answer string `json:"answer" validate:"required" errormgs:"is required"`
}

type ClarifyJobRequest struct {
	Answers []ClarifyJobAnswer `json:"answers" validate:"omitempty,dive" errormgs:"must contain valid answers"`
	Skip    bool               `json:"skip"`
}

type TransferProjectRequest struct {
	OrganizationID uuid.UUID `json:"organization_id" validate:"required"`
}

func (r *TransferProjectRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[TransferProjectRequest](*r, validate)
}
