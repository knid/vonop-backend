package responses

import (
	"time"

	"github.com/google/uuid"
)

// CommitHistoryEntry represents a git commit in the project's version history.
type CommitHistoryEntry struct {
	Hash        string    `json:"hash"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type AppProjectResponse struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Prompt      string         `json:"prompt"`
	ProjectType string         `json:"project_type"`
	Theme       map[string]any `json:"theme"`
	Settings    map[string]any `json:"settings"`
	Status      string         `json:"status"`
	HasStore      bool           `json:"has_store"`
	ScreenshotURL string         `json:"screenshot_url,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type AppProjectDetailResponse struct {
	AppProjectResponse
	DataModels   []map[string]any     `json:"data_models,omitempty"`
	Files        []AppFileResponse    `json:"files,omitempty"`
	Integrations []AppIntegrationResp `json:"integrations,omitempty"`
}

type AppFileResponse struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	FileType string `json:"file_type"`
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
	Hash        string    `json:"hash"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type AppConversationResponse struct {
	ID        uuid.UUID                    `json:"id"`
	Messages  []AppConversationMessageResp `json:"messages"`
	CreatedAt time.Time                    `json:"created_at"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

type AppConversationMessageResp struct {
	ID                 string                    `json:"id,omitempty"`
	Role               string                    `json:"role"`
	Content            string                    `json:"content"`
	Timestamp          int64                     `json:"timestamp"`
	FileOperations     []AppChatFileOpResp       `json:"file_operations,omitempty"`
	ImageURLs          []string                  `json:"image_urls,omitempty"`
	SnapshotID         string                    `json:"snapshot_id,omitempty"`
	Suggestions        []string                  `json:"suggestions,omitempty"`
	ThinkingText       string                    `json:"thinking_text,omitempty"`
	ThinkingDurationMs int                       `json:"thinking_duration_ms,omitempty"`
	ToolCalls          []AppChatToolCallResp     `json:"tool_calls,omitempty"`
}

type AppChatToolCallResp struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
}

type AppChatFileOpResp struct {
	Action   string `json:"action"`
	FilePath string `json:"file_path"`
	FileType string `json:"file_type,omitempty"`
}

type AppDomainResponse struct {
	ID        uuid.UUID `json:"id"`
	Domain    string    `json:"domain"`
	Status    string    `json:"status"`
	DNSRecord string    `json:"dns_record,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type EnvVarResponse struct {
	ID              uuid.UUID `json:"id"`
	Key             string    `json:"key"`
	Value           string    `json:"value"`
	IsPublic        bool      `json:"is_public"`
	EnvLocalContent string    `json:"env_local_content,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type MissingEnvVarResponse struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	IsPublic    bool   `json:"is_public"`
	Provider    string `json:"provider,omitempty"`
}

type AppInitResponse struct {
	Project        AppProjectDetailResponse  `json:"project"`
	Conversation   *AppConversationResponse  `json:"conversation"`
	Domains        []AppDomainResponse       `json:"domains"`
	EnvVars        []EnvVarResponse          `json:"env_vars"`
	MissingEnvVars []MissingEnvVarResponse   `json:"missing_env_vars"`
	Integrations   []AppIntegrationResp      `json:"integrations"`
	Deployment     *AppDeploymentResponse    `json:"deployment"`
	SandboxID      string                    `json:"sandbox_id"`
}

type IntegrationProviderInfo struct {
	Provider       string   `json:"provider"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Category       string   `json:"category"`
	RequiredFields []string `json:"required_fields"`
	OptionalFields []string `json:"optional_fields,omitempty"`
	Website        string   `json:"website"`
}
