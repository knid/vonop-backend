package agentruntime

import "time"

const DefaultAssistantID = "wemob_agent"

type StartRunRequest struct {
	ThreadID        string         `json:"thread_id"`
	JobID           string         `json:"job_id"`
	ProjectID       string         `json:"project_id"`
	OrgID           string         `json:"org_id"`
	UserID          string         `json:"user_id"`
	RuntimeRunID    string         `json:"agent_runtime_run_id"`
	Message         string         `json:"message"`
	Mode            string         `json:"mode"`
	ProjectType     string         `json:"project_type"`
	FrontendRuntime string         `json:"frontend_runtime"`
	WorktreePath    string         `json:"worktree_path"`
	BaseCommit      string         `json:"base_commit,omitempty"`
	AttachmentIDs   []string       `json:"attachment_ids,omitempty"`
	FeatureFlags    map[string]any `json:"feature_flags,omitempty"`
	CreditBudget    int            `json:"credit_budget,omitempty"`
}

type RuntimeRun struct {
	RunID         string    `json:"run_id"`
	ThreadID      string    `json:"thread_id"`
	AssistantID   string    `json:"assistant_id"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	RuntimeAPIURL string    `json:"runtime_api_url,omitempty"`
	SandboxID     string    `json:"sandbox_id,omitempty"`
}

type ResumeRunRequest struct {
	Command  any            `json:"command,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Decision string         `json:"decision,omitempty"`
}

type RuntimeAccessRequest struct {
	UserID      string
	OrgID       string
	ProjectID   string
	ThreadID    string
	AssistantID string
	ExpiresIn   time.Duration
}

type RuntimeAccessInfo struct {
	RuntimeAPIURL      string `json:"runtime_api_url"`
	AssistantID        string `json:"assistant_id"`
	ThreadID           string `json:"thread_id"`
	RuntimeAccessToken string `json:"runtime_access_token"`
	ExpiresAtUnix      int64  `json:"expires_at"`
}
