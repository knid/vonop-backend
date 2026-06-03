package models

import "time"

const (
	AgentTodoStatusPending    = "pending"
	AgentTodoStatusInProgress = "in_progress"
	AgentTodoStatusDone       = "done"
	AgentTodoStatusBlocked    = "blocked"
)

type AgentTodoEvidence struct {
	Files      []string `json:"files,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	Validation []string `json:"validation,omitempty"`
}

type AgentTodo struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Status    string            `json:"status"`
	Evidence  AgentTodoEvidence `json:"evidence,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}
