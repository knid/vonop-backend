package responses

import (
	"time"

	"github.com/google/uuid"
)

type AppTemplateResponse struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Description     string    `json:"description"`
	Tags            []string  `json:"tags"`
	TemplateType    string    `json:"template_type"`
	PreviewImageKey string    `json:"preview_image_key"`
	PreviewURL      string    `json:"preview_url"`
	ProjectType     string    `json:"project_type"`
	Published       bool      `json:"published,omitempty"`
	SortOrder       int       `json:"sort_order,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type AppTemplateDetailResponse struct {
	ID              uuid.UUID      `json:"id"`
	Name            string         `json:"name"`
	Slug            string         `json:"slug"`
	Description     string         `json:"description"`
	Tags            []string       `json:"tags"`
	TemplateType    string         `json:"template_type"`
	PreviewImageKey string         `json:"preview_image_key"`
	PreviewURL      string         `json:"preview_url"`
	Theme           map[string]any `json:"theme"`
	Settings        map[string]any `json:"settings"`
	ProjectType     string         `json:"project_type"`
	Prompt          string         `json:"prompt"`
	Published       bool           `json:"published,omitempty"`
	SortOrder       int            `json:"sort_order,omitempty"`
	SourceRepoID    string         `json:"source_repo_id,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AppTemplateListResponse struct {
	Templates []AppTemplateResponse `json:"templates"`
}
