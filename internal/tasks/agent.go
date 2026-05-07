package tasks

const (
	// TypeAgentRun is the Asynq task that drives one invocation of agent.Run.
	// Replaces the in-process `go cfg.StreamFunc(...)` previously launched by
	// handleStreamOrAsync for generate_app and app_chat jobs.
	TypeAgentRun = "agent:run"
)

// AgentRunPayload carries only IDs — the worker re-reads project, user,
// attachments, and conversation history from the DB at execution time. This
// keeps a task picked up seconds after enqueue safe against in-flight DB
// updates between enqueue and dequeue.
type AgentRunPayload struct {
	JobID         string   `json:"job_id"`         // GenerationJob.ID — primary correlation key for events + cancel.
	UserID        string   `json:"user_id"`
	ProjectID     string   `json:"project_id"`
	Message       string   `json:"message"`        // User's prompt this turn. Persisted to AppChatMessage by agent.Run.
	AttachmentIDs []string `json:"attachment_ids"` // Resolved against AppAsset in worker via loadTurnAttachments.
}
