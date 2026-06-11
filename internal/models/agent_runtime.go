package models

import (
	"time"

	"github.com/google/uuid"
)

const (
	AgentThreadStatusActive   = "active"
	AgentThreadStatusArchived = "archived"

	AgentRuntimeRunStatusQueued         = "queued"
	AgentRuntimeRunStatusRunning        = "running"
	AgentRuntimeRunStatusAwaitingInput  = "awaiting_input"
	AgentRuntimeRunStatusCompleted      = "completed"
	AgentRuntimeRunStatusFailed         = "failed"
	AgentRuntimeRunStatusCanceled       = "canceled"
	AgentRuntimeRunStatusBudgetExceeded = "budget_exceeded"

	AgentApprovalStatusPending  = "pending"
	AgentApprovalStatusApproved = "approved"
	AgentApprovalStatusRejected = "rejected"
	AgentApprovalStatusEdited   = "edited"
	AgentApprovalStatusExpired  = "expired"
)

// AgentThread maps a Wemob project conversation to the Deep Agents thread
// that owns checkpoints, message state, subagents, memory, and native stream
// state. Wemob stores only the pointer and product ownership metadata.
type AgentThread struct {
	ID                 uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID       uuid.UUID      `bun:",notnull,type:uuid,unique:agent_threads_project_conversation" json:"app_project_id"`
	AppProject         *AppProject    `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	OrganizationID     uuid.UUID      `bun:",notnull,type:uuid" json:"organization_id"`
	Organization       *Organization  `bun:"rel:belongs-to,join:organization_id=id" json:"-"`
	UserID             uuid.UUID      `bun:",notnull,type:uuid" json:"user_id"`
	User               *User          `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	DeepAgentsThreadID string         `bun:"deepagents_thread_id,notnull,unique" json:"deepagents_thread_id"`
	ConversationKey    string         `bun:",notnull,default:'default',unique:agent_threads_project_conversation" json:"conversation_key"`
	Status             string         `bun:",notnull,default:'active'" json:"status"`
	Metadata           map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"metadata"`
	CreatedAt          time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt          time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// AgentRuntimeRun is Wemob's product metadata for a Deep Agents run. The
// runtime state remains in Deep Agents/LangGraph; this row links that runtime
// identity to Wemob billing, project, Git finalization, and status records.
type AgentRuntimeRun struct {
	ID                     uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	GenerationJobID        uuid.UUID      `bun:",notnull,type:uuid" json:"generation_job_id"`
	GenerationJob          *GenerationJob `bun:"rel:belongs-to,join:generation_job_id=id" json:"-"`
	AgentThreadID          uuid.UUID      `bun:",notnull,type:uuid" json:"agent_thread_id"`
	AgentThread            *AgentThread   `bun:"rel:belongs-to,join:agent_thread_id=id" json:"-"`
	AppProjectID           uuid.UUID      `bun:",notnull,type:uuid" json:"app_project_id"`
	AppProject             *AppProject    `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	OrganizationID         uuid.UUID      `bun:",notnull,type:uuid" json:"organization_id"`
	Organization           *Organization  `bun:"rel:belongs-to,join:organization_id=id" json:"-"`
	UserID                 uuid.UUID      `bun:",notnull,type:uuid" json:"user_id"`
	User                   *User          `bun:"rel:belongs-to,join:user_id=id" json:"-"`
	DeepAgentsThreadID     string         `bun:"deepagents_thread_id,notnull" json:"deepagents_thread_id"`
	DeepAgentsRunID        string         `bun:"deepagents_run_id,notnull,default:''" json:"deepagents_run_id"`
	DeepAgentsCheckpointID string         `bun:"deepagents_checkpoint_id,notnull,default:''" json:"deepagents_checkpoint_id"`
	RuntimeVersion         string         `bun:",notnull" json:"runtime_version"`
	GraphID                string         `bun:",notnull,default:'wemob_agent'" json:"graph_id"`
	Mode                   string         `bun:",notnull" json:"mode"`
	Status                 string         `bun:",notnull,default:'queued'" json:"status"`
	StartedAt              time.Time      `bun:",nullzero" json:"started_at,omitempty"`
	CompletedAt            time.Time      `bun:",nullzero" json:"completed_at,omitempty"`
	BaseCommit             string         `bun:",notnull,default:''" json:"base_commit"`
	FinalCommit            string         `bun:",notnull,default:''" json:"final_commit"`
	CheckpointCommit       string         `bun:",notnull,default:''" json:"checkpoint_commit"`
	RunCommit              string         `bun:",notnull,default:''" json:"run_commit"`
	RunRef                 string         `bun:",notnull,default:''" json:"run_ref"`
	FinalizedToProject     bool           `bun:",notnull,default:false" json:"finalized_to_project"`
	WorktreePath           string         `bun:",notnull,default:''" json:"worktree_path"`
	SandboxID              string         `bun:",notnull,default:''" json:"sandbox_id"`
	Model                  string         `bun:",notnull,default:''" json:"model"`
	Provider               string         `bun:",notnull,default:''" json:"provider"`
	PromptTokens           int            `bun:",notnull,default:0" json:"prompt_tokens"`
	CompletionTokens       int            `bun:",notnull,default:0" json:"completion_tokens"`
	CacheReadTokens        int            `bun:",notnull,default:0" json:"cache_read_tokens"`
	CacheWriteTokens       int            `bun:",notnull,default:0" json:"cache_write_tokens"`
	CostUSD                float64        `bun:",notnull,default:0" json:"cost_usd"`
	CreditsUsed            int            `bun:",notnull,default:0" json:"credits_used"`
	ErrorCode              string         `bun:",notnull,default:''" json:"error_code"`
	ErrorMessage           string         `bun:",type:text,notnull,default:''" json:"error_message"`
	Metadata               map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"metadata"`
	CreatedAt              time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt              time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// ProjectGitRepository records the canonical bare repository backing an
// AppProject. Project source of truth is Git; checkpoints and Deep Agents
// memory store runtime state, not source files.
type ProjectGitRepository struct {
	ID                   uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	AppProjectID         uuid.UUID      `bun:",notnull,type:uuid,unique" json:"app_project_id"`
	AppProject           *AppProject    `bun:"rel:belongs-to,join:app_project_id=id" json:"-"`
	OrganizationID       uuid.UUID      `bun:",notnull,type:uuid" json:"organization_id"`
	Organization         *Organization  `bun:"rel:belongs-to,join:organization_id=id" json:"-"`
	StorageMode          string         `bun:",notnull,default:'bare_worktree'" json:"storage_mode"`
	BareRepoPath         string         `bun:",notnull" json:"bare_repo_path"`
	LegacyRepoPath       string         `bun:",notnull,default:''" json:"legacy_repo_path"`
	DefaultBranch        string         `bun:",notnull,default:'main'" json:"default_branch"`
	CurrentCommit        string         `bun:",notnull,default:''" json:"current_commit"`
	MainRef              string         `bun:",notnull,default:'refs/heads/main'" json:"main_ref"`
	LastSuccessfulRunRef string         `bun:",notnull,default:''" json:"last_successful_run_ref"`
	LastBackupCommit     string         `bun:",notnull,default:''" json:"last_backup_commit"`
	LastBackupAt         time.Time      `bun:",nullzero" json:"last_backup_at,omitempty"`
	Status               string         `bun:",notnull,default:'active'" json:"status"`
	Metadata             map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"metadata"`
	CreatedAt            time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt            time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

// AgentApproval mirrors Deep Agents human interrupts into Wemob product state
// for authorization, audit, and frontend approval UI.
type AgentApproval struct {
	ID                    uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	GenerationJobID       uuid.UUID      `bun:",notnull,type:uuid" json:"generation_job_id"`
	AgentRuntimeRunID     *uuid.UUID     `bun:",type:uuid,nullzero" json:"agent_runtime_run_id,omitempty"`
	AppProjectID          uuid.UUID      `bun:",notnull,type:uuid" json:"app_project_id"`
	OrganizationID        uuid.UUID      `bun:",notnull,type:uuid" json:"organization_id"`
	DeepAgentsInterruptID string         `bun:"deepagents_interrupt_id,notnull,default:''" json:"deepagents_interrupt_id"`
	ApprovalType          string         `bun:",notnull" json:"approval_type"`
	Status                string         `bun:",notnull,default:'pending'" json:"status"`
	Title                 string         `bun:",notnull" json:"title"`
	Description           string         `bun:",type:text,notnull,default:''" json:"description"`
	RiskLevel             string         `bun:",notnull,default:'medium'" json:"risk_level"`
	Payload               map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"payload"`
	ProposedAction        map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"proposed_action"`
	ReviewerResponse      map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"reviewer_response"`
	RequestedByAgent      string         `bun:",notnull,default:''" json:"requested_by_agent"`
	ReviewedByUserID      *uuid.UUID     `bun:",type:uuid,nullzero" json:"reviewed_by_user_id,omitempty"`
	ReviewedAt            time.Time      `bun:",nullzero" json:"reviewed_at,omitempty"`
	CreatedAt             time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	ExpiresAt             time.Time      `bun:",nullzero" json:"expires_at,omitempty"`
}

// AgentWorkspaceArtifact stores product-visible artifact metadata only. Large
// content lives in object storage or the runtime artifact backend.
type AgentWorkspaceArtifact struct {
	ID                uuid.UUID      `bun:",pk,type:uuid,default:uuid_generate_v4()" json:"id"`
	GenerationJobID   *uuid.UUID     `bun:",type:uuid,nullzero" json:"generation_job_id,omitempty"`
	AgentRuntimeRunID *uuid.UUID     `bun:",type:uuid,nullzero" json:"agent_runtime_run_id,omitempty"`
	AppProjectID      uuid.UUID      `bun:",notnull,type:uuid" json:"app_project_id"`
	OrganizationID    uuid.UUID      `bun:",notnull,type:uuid" json:"organization_id"`
	ArtifactType      string         `bun:",notnull" json:"artifact_type"`
	Name              string         `bun:",notnull" json:"name"`
	ContentType       string         `bun:",notnull,default:''" json:"content_type"`
	StorageKey        string         `bun:",notnull,default:''" json:"storage_key"`
	PublicURL         string         `bun:",notnull,default:''" json:"public_url"`
	Metadata          map[string]any `bun:",type:jsonb,notnull,default:'{}'" json:"metadata"`
	CreatedAt         time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
}
