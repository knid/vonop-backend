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

// Snapshot
type CreateSnapshotRequest struct {
	Description string `json:"description" validate:"required,min=2,max=200" errormgs:"must be between 2 and 200 characters long"`
}

func (r *CreateSnapshotRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateSnapshotRequest](*r, validate)
}

// Screenshot-to-Code
type ScreenshotToCodeRequest struct {
	ImageBase64 string     `json:"image_base64" validate:"required" errormgs:"is required"`
	TemplateID  *uuid.UUID `json:"template_id"`
}

func (r *ScreenshotToCodeRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ScreenshotToCodeRequest](*r, validate)
}

// Visual Element Edit
type EditElementRequest struct {
	FileID          string `json:"file_id" validate:"required" errormgs:"is required"`
	ElementSelector string `json:"element_selector" validate:"required" errormgs:"is required"`
	Instruction     string `json:"instruction" validate:"required,min=5" errormgs:"must be at least 5 characters long"`
}

func (r *EditElementRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[EditElementRequest](*r, validate)
}

// Component Insert
type InsertComponentRequest struct {
	ComponentID  string `json:"component_id" validate:"required" errormgs:"is required"`
	TargetFileID string `json:"target_file_id" validate:"required" errormgs:"is required"`
	Position     string `json:"position" validate:"required,oneof=top bottom after-selector" errormgs:"must be top, bottom, or after-selector"`
}

func (r *InsertComponentRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[InsertComponentRequest](*r, validate)
}

// Dark Mode / Theme
type UpdateThemeRequest struct {
	ColorScheme     map[string]any `json:"color_scheme" validate:"required" errormgs:"is required"`
	DarkColorScheme map[string]any `json:"dark_color_scheme"`
}

func (r *UpdateThemeRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[UpdateThemeRequest](*r, validate)
}

// Custom Domain
type AddDomainRequest struct {
	Domain string `json:"domain" validate:"required,min=4,max=253" errormgs:"must be a valid domain"`
}

func (r *AddDomainRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[AddDomainRequest](*r, validate)
}

// SEO
type UpdateSEORequest struct {
	MetaTitle         string   `json:"meta_title"`
	MetaDescription   string   `json:"meta_description"`
	OGImage           string   `json:"og_image"`
	OGTitle           string   `json:"og_title"`
	OGDescription     string   `json:"og_description"`
	FaviconURL        string   `json:"favicon_url"`
	GoogleAnalyticsID string   `json:"google_analytics_id"`
	Keywords          []string `json:"keywords"`
}

// Form
type CreateFormRequest struct {
	FormName          string              `json:"form_name" validate:"required,min=2,max=100" errormgs:"must be between 2 and 100 characters long"`
	Fields            []FormFieldRequest  `json:"fields" validate:"required,min=1" errormgs:"must have at least one field"`
	RedirectURL       string              `json:"redirect_url"`
	EmailNotification bool                `json:"email_notification"`
	NotifyEmail       string              `json:"notify_email"`
}

type FormFieldRequest struct {
	Name     string   `json:"name" validate:"required" errormgs:"is required"`
	Type     string   `json:"type" validate:"required" errormgs:"is required"`
	Label    string   `json:"label" validate:"required" errormgs:"is required"`
	Required bool     `json:"required"`
	Options  []string `json:"options"`
}

func (r *CreateFormRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[CreateFormRequest](*r, validate)
}

// Form Submission (public)
type SubmitFormRequest struct {
	Data map[string]any `json:"data" validate:"required" errormgs:"is required"`
}

func (r *SubmitFormRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[SubmitFormRequest](*r, validate)
}

type ConvertToMobileRequest struct {
	AppName         string `json:"app_name" validate:"required,min=3,max=30" errormgs:"must be between 3 and 30 characters long"`
	BuildForAndroid bool   `json:"build_for_android"`
	BuildForIOS     bool   `json:"build_for_ios"`
}

func (r *ConvertToMobileRequest) Validate(validate *validator.Validate) error {
	return utils.ValidateFunc[ConvertToMobileRequest](*r, validate)
}
