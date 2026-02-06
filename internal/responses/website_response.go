package responses

import (
	"time"

	"github.com/google/uuid"
)

type WebsiteResponse struct {
	ID           uuid.UUID      `json:"id"`
	Name         string         `json:"name"`
	Slug         string         `json:"slug"`
	Prompt       string         `json:"prompt"`
	ColorScheme  map[string]any `json:"color_scheme"`
	Status       string         `json:"status"`
	PublishedURL string         `json:"published_url"`
	ProjectID    uuid.UUID      `json:"project_id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
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
