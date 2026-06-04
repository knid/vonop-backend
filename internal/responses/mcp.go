package responses

import (
	"time"

	"github.com/google/uuid"
)

type MCPTemplateResponse struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	Category               string   `json:"category"`
	AuthType               string   `json:"auth_type"`
	AuthTypes              []string `json:"auth_types,omitempty"`
	ServerURL              string   `json:"server_url,omitempty"`
	RequiresServerURL      bool     `json:"requires_server_url"`
	Scopes                 []string `json:"scopes,omitempty"`
	DocsURL                string   `json:"docs_url,omitempty"`
	Icon                   string   `json:"icon,omitempty"`
	Recommended            bool     `json:"recommended"`
	SupportsOAuthDiscovery bool     `json:"supports_oauth_discovery"`
	DefaultPolicy          string   `json:"default_policy,omitempty"`
	APIKeyHeaderName       string   `json:"api_key_header_name,omitempty"`
	APIKeyHeaderPrefix     string   `json:"api_key_header_prefix,omitempty"`
}

type MCPConnectionResponse struct {
	ID               uuid.UUID         `json:"id"`
	AppProjectID     uuid.UUID         `json:"app_project_id"`
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	TemplateID       string            `json:"template_id,omitempty"`
	AuthType         string            `json:"auth_type"`
	ServerURL        string            `json:"server_url"`
	Status           string            `json:"status"`
	Config           map[string]any    `json:"config"`
	LastDiscoveredAt *time.Time        `json:"last_discovered_at,omitempty"`
	LastError        string            `json:"last_error,omitempty"`
	Tools            []MCPToolResponse `json:"tools"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type MCPToolResponse struct {
	ID           uuid.UUID      `json:"id"`
	ConnectionID uuid.UUID      `json:"connection_id"`
	Name         string         `json:"name"`
	PrefixedName string         `json:"prefixed_name"`
	DisplayName  string         `json:"display_name"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	Annotations  map[string]any `json:"annotations"`
	RiskLevel    string         `json:"risk_level"`
	Status       string         `json:"status"`
}

type AgentMCPGrantResponse struct {
	ID            uuid.UUID         `json:"id"`
	AppProjectID  uuid.UUID         `json:"app_project_id"`
	ConnectionID  uuid.UUID         `json:"connection_id"`
	AgentID       string            `json:"agent_id"`
	Enabled       bool              `json:"enabled"`
	DefaultPolicy string            `json:"default_policy"`
	ToolPolicies  map[string]string `json:"tool_policies"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

type MCPOAuthStartResponse struct {
	Connection MCPConnectionResponse `json:"connection"`
	AuthURL    string                `json:"auth_url"`
}

type MCPApprovalResponse struct {
	ID       uuid.UUID `json:"id"`
	Status   string    `json:"status"`
	Decision string    `json:"decision"`
}
