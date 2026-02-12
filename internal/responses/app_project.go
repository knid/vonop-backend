package responses

import (
	"time"

	"github.com/google/uuid"
)

type AppProjectResponse struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Prompt      string         `json:"prompt"`
	ProjectType string         `json:"project_type"`
	Theme       map[string]any `json:"theme"`
	Settings    map[string]any `json:"settings"`
	Status      string         `json:"status"`
	HasStore    bool           `json:"has_store"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type AppProjectDetailResponse struct {
	AppProjectResponse
	AST          []map[string]any     `json:"ast,omitempty"`
	DataModels   []map[string]any     `json:"data_models,omitempty"`
	Files        []AppFileResponse    `json:"files,omitempty"`
	Integrations []AppIntegrationResp `json:"integrations,omitempty"`
}

type AppFileResponse struct {
	ID        uuid.UUID `json:"id"`
	Platform  string    `json:"platform"`
	FilePath  string    `json:"file_path"`
	Content   string    `json:"content"`
	FileType  string    `json:"file_type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AppIntegrationResp struct {
	ID        uuid.UUID      `json:"id"`
	Provider  string         `json:"provider"`
	Config    map[string]any `json:"config"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

type AppDeploymentResponse struct {
	ID           uuid.UUID `json:"id"`
	AppProjectID uuid.UUID `json:"app_project_id"`
	Platform     string    `json:"platform"`
	Provider     string    `json:"provider"`
	Status       string    `json:"status"`
	DeployedURL  string    `json:"deployed_url"`
	Version      int       `json:"version"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type AppSnapshotResponse struct {
	ID          uuid.UUID `json:"id"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"created_at"`
}

type AppConversationResponse struct {
	ID        uuid.UUID                    `json:"id"`
	Messages  []AppConversationMessageResp `json:"messages"`
	CreatedAt time.Time                    `json:"created_at"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

type AppConversationMessageResp struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type AppDomainResponse struct {
	ID        uuid.UUID `json:"id"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	DNSRecord string    `json:"dns_record,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type EnvVarResponse struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	IsPublic  bool      `json:"is_public"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IntegrationProviderInfo struct {
	Provider       string   `json:"provider"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	RequiredFields []string `json:"required_fields"`
	Website        string   `json:"website"`
}
