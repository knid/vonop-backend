package responses

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	Slug            string         `json:"slug"`
	Prompt          string         `json:"prompt"`
	ColorScheme     map[string]any `json:"color_scheme"`
	DarkColorScheme map[string]any `json:"dark_color_scheme,omitempty"`
	Status          string         `json:"status"`
	PublishedURL    string         `json:"published_url"`
	ProjectID       uuid.UUID      `json:"project_id"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type PageResponse struct {
	ID          uuid.UUID `json:"id"`
	WebsiteID   uuid.UUID `json:"website_id"`
	Path        string    `json:"path"`
	Title       string    `json:"title"`
	HTMLContent string    `json:"html_content"`
	CSSContent  string    `json:"css_content"`
	JSContent   string    `json:"js_content"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type WebsiteDetailResponse struct {
	WebsiteResponse
	Pages []PageResponse `json:"pages"`
	Files []FileResponse `json:"files"`
}

type FileResponse struct {
	ID        uuid.UUID `json:"id"`
	WebsiteID uuid.UUID `json:"website_id"`
	FilePath  string    `json:"file_path"`
	Content   string    `json:"content"`
	FileType  string    `json:"file_type"`
	Layer     string    `json:"layer"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ConversationResponse struct {
	ID        uuid.UUID                `json:"id"`
	WebsiteID uuid.UUID                `json:"website_id"`
	Messages  []ConversationMessageDTO `json:"messages"`
	CreatedAt time.Time                `json:"created_at"`
	UpdatedAt time.Time                `json:"updated_at"`
}

type ConversationMessageDTO struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type IntegrationResponse struct {
	ID        uuid.UUID      `json:"id"`
	WebsiteID uuid.UUID      `json:"website_id"`
	Provider  string         `json:"provider"`
	Config    map[string]any `json:"config"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type IntegrationProviderInfo struct {
	Provider       string   `json:"provider"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	RequiredFields []string `json:"required_fields"`
	Website        string   `json:"website"`
}

type WebsiteListResponse struct {
	Websites []WebsiteResponse `json:"websites"`
}

type PublishResponse struct {
	SuccessResponse
	PublishedURL string `json:"published_url"`
}

type ConvertToMobileResponse struct {
	SuccessResponse
	ProjectID uuid.UUID `json:"project_id"`
}

type TemplateResponse struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Category   string    `json:"category"`
	PreviewURL string    `json:"preview_url"`
}

// Snapshot responses

type SnapshotResponse struct {
	ID          uuid.UUID `json:"id"`
	WebsiteID   uuid.UUID `json:"website_id"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	CreatedBy   uuid.UUID `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// Component responses

type ComponentResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Category     string    `json:"category"`
	HTMLTemplate string    `json:"html_template"`
	PreviewImage string    `json:"preview_image"`
	Tags         []string  `json:"tags"`
}

type ComponentListResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Category     string    `json:"category"`
	PreviewImage string    `json:"preview_image"`
	Tags         []string  `json:"tags"`
}

// Domain responses

type DomainResponse struct {
	ID                 uuid.UUID `json:"id"`
	WebsiteID          uuid.UUID `json:"website_id"`
	Domain             string    `json:"domain"`
	VerificationStatus string    `json:"verification_status"`
	VerificationToken  string    `json:"verification_token"`
	SSLStatus          string    `json:"ssl_status"`
	CreatedAt          time.Time `json:"created_at"`
	VerifiedAt         time.Time `json:"verified_at,omitempty"`
}

// SEO responses

type SEOResponse struct {
	ID                uuid.UUID `json:"id"`
	WebsiteID         uuid.UUID `json:"website_id"`
	MetaTitle         string    `json:"meta_title"`
	MetaDescription   string    `json:"meta_description"`
	OGImage           string    `json:"og_image"`
	OGTitle           string    `json:"og_title"`
	OGDescription     string    `json:"og_description"`
	FaviconURL        string    `json:"favicon_url"`
	GoogleAnalyticsID string    `json:"google_analytics_id"`
	Keywords          []string  `json:"keywords"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Form responses

type FormResponse struct {
	ID                uuid.UUID           `json:"id"`
	WebsiteID         uuid.UUID           `json:"website_id"`
	FormName          string              `json:"form_name"`
	Fields            []FormFieldResponse `json:"fields"`
	RedirectURL       string              `json:"redirect_url"`
	EmailNotification bool                `json:"email_notification"`
	NotifyEmail       string              `json:"notify_email"`
	CreatedAt         time.Time           `json:"created_at"`
	UpdatedAt         time.Time           `json:"updated_at"`
}

type FormFieldResponse struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

type FormSubmissionResponse struct {
	ID        uuid.UUID      `json:"id"`
	FormID    uuid.UUID      `json:"form_id"`
	Data      map[string]any `json:"data"`
	IPAddress string         `json:"ip_address"`
	CreatedAt time.Time      `json:"created_at"`
}
