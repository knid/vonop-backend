package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	MCPAuthTypeNone   = "none"
	MCPAuthTypeAPIKey = "api_key"
	MCPAuthTypeOAuth  = "oauth"

	MCPConnectionStatusPendingAuth = "pending_auth"
	MCPConnectionStatusConnected   = "connected"
	MCPConnectionStatusError       = "error"

	MCPGrantPolicyAuto    = "auto"
	MCPGrantPolicyAsk     = "ask"
	MCPGrantPolicyBlocked = "blocked"

	MCPApprovalStatusPending  = "pending"
	MCPApprovalStatusApproved = "approved"
	MCPApprovalStatusDenied   = "denied"
	MCPApprovalStatusExpired  = "expired"

	MCPToolCallStatusPending  = "pending"
	MCPToolCallStatusApproved = "approved"
	MCPToolCallStatusDenied   = "denied"
	MCPToolCallStatusExecuted = "executed"
	MCPToolCallStatusError    = "error"
)

type MCPConnection struct {
	ID                     uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID           uuid.UUID       `bun:",notnull,type:uuid,unique:mcp_connections_project_slug"`
	AppProject             *AppProject     `bun:"rel:belongs-to,join:app_project_id=id"`
	Name                   string          `bun:",notnull"`
	Slug                   string          `bun:",notnull,unique:mcp_connections_project_slug"`
	TemplateID             string          `bun:",notnull,default:''"`
	AuthType               string          `bun:",notnull,default:'api_key'"`
	ServerURL              string          `bun:",notnull"`
	Status                 string          `bun:",notnull,default:'pending_auth'"`
	Config                 map[string]any  `bun:",type:jsonb,default:'{}'"`
	CredentialData         json.RawMessage `bun:"credential_data,type:jsonb,default:'{}'"`
	CredentialsEncryptedV1 bool            `bun:",notnull,default:false"`
	LastDiscoveredAt       time.Time       `bun:",nullzero"`
	LastError              string          `bun:",notnull,default:''"`
	CreatedAt              time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt              time.Time       `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
	DeletedAt              time.Time       `bun:",soft_delete,nullzero"`
}

type MCPConnectionTool struct {
	ID           uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	ConnectionID uuid.UUID      `bun:",notnull,type:uuid,unique:mcp_connection_tools_connection_name"`
	Connection   *MCPConnection `bun:"rel:belongs-to,join:connection_id=id"`
	Name         string         `bun:",notnull,unique:mcp_connection_tools_connection_name"`
	PrefixedName string         `bun:",notnull"`
	DisplayName  string         `bun:",notnull,default:''"`
	Description  string         `bun:",notnull,default:''"`
	InputSchema  map[string]any `bun:",type:jsonb,default:'{}'"`
	Annotations  map[string]any `bun:",type:jsonb,default:'{}'"`
	RiskLevel    string         `bun:",notnull,default:'medium'"`
	Status       string         `bun:",notnull,default:'enabled'"`
	LastSeenAt   time.Time      `bun:",nullzero,notnull,default:current_timestamp"`
	CreatedAt    time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt    time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type AgentMCPGrant struct {
	ID              uuid.UUID         `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID    uuid.UUID         `bun:",notnull,type:uuid,unique:agent_mcp_grants_project_agent_connection"`
	AppProject      *AppProject       `bun:"rel:belongs-to,join:app_project_id=id"`
	MCPConnectionID uuid.UUID         `bun:",notnull,type:uuid,unique:agent_mcp_grants_project_agent_connection"`
	MCPConnection   *MCPConnection    `bun:"rel:belongs-to,join:mcp_connection_id=id"`
	AgentID         string            `bun:",notnull,unique:agent_mcp_grants_project_agent_connection"`
	Enabled         bool              `bun:",notnull,default:false"`
	DefaultPolicy   string            `bun:",notnull,default:'ask'"`
	ToolPolicies    map[string]string `bun:",type:jsonb,default:'{}'"`
	CreatedAt       time.Time         `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time         `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type MCPToolApproval struct {
	ID                uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	GenerationJobID   uuid.UUID       `bun:",notnull,type:uuid"`
	AppProjectID      uuid.UUID       `bun:",notnull,type:uuid"`
	ConnectionID      uuid.UUID       `bun:",notnull,type:uuid"`
	ToolID            uuid.UUID       `bun:",type:uuid,nullzero"`
	AgentID           string          `bun:",notnull"`
	ToolName          string          `bun:",notnull"`
	PrefixedName      string          `bun:",notnull"`
	RiskLevel         string          `bun:",notnull,default:'medium'"`
	ArgumentsPreview  json.RawMessage `bun:",type:jsonb,default:'{}'"`
	Status            string          `bun:",notnull,default:'pending'"`
	RequestedByUserID uuid.UUID       `bun:",notnull,type:uuid"`
	DecidedByUserID   *uuid.UUID      `bun:",type:uuid,nullzero"`
	DecisionAt        time.Time       `bun:",nullzero"`
	ExpiresAt         time.Time       `bun:",nullzero,notnull"`
	CreatedAt         time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt         time.Time       `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}

type AgentMCPToolCall struct {
	ID               uuid.UUID       `bun:",pk,type:uuid,default:uuid_generate_v4()"`
	AppProjectID     uuid.UUID       `bun:",notnull,type:uuid"`
	ConnectionID     uuid.UUID       `bun:",notnull,type:uuid"`
	ToolID           uuid.UUID       `bun:",type:uuid,nullzero"`
	GenerationJobID  uuid.UUID       `bun:",type:uuid,nullzero"`
	RunID            uuid.UUID       `bun:",type:uuid,nullzero"`
	ApprovalID       uuid.UUID       `bun:",type:uuid,nullzero"`
	AgentID          string          `bun:",notnull"`
	ToolName         string          `bun:",notnull"`
	PrefixedName     string          `bun:",notnull"`
	Policy           string          `bun:",notnull,default:'ask'"`
	RiskLevel        string          `bun:",notnull,default:'medium'"`
	ArgumentsPreview json.RawMessage `bun:",type:jsonb,default:'{}'"`
	Status           string          `bun:",notnull,default:'pending'"`
	ResultSummary    string          `bun:",notnull,default:''"`
	Error            string          `bun:",notnull,default:''"`
	DurationMs       int             `bun:",notnull,default:0"`
	ApprovedByUserID *uuid.UUID      `bun:",type:uuid,nullzero"`
	CreatedAt        time.Time       `bun:"created_at,nullzero,notnull,default:current_timestamp"`
	UpdatedAt        time.Time       `bun:"updated_at,nullzero,notnull,default:current_timestamp"`
}
