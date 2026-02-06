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
