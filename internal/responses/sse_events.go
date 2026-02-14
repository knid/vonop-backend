package responses

import "github.com/google/uuid"

type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type GenerationErrorEvent struct {
	Error string `json:"error"`
	Phase string `json:"phase"`
}

// File-based SSE events (chat + generation)

type FileOperationEvent struct {
	Action   string `json:"action"` // create, modify, delete
	FilePath string `json:"file_path"`
	FileType string `json:"file_type"`
	Chunk    string `json:"chunk"`
}

type FileCompleteEvent struct {
	Action   string    `json:"action"`
	FilePath string    `json:"file_path"`
	FileID   uuid.UUID `json:"file_id"`
	FileType string    `json:"file_type"`
	Content  string    `json:"content,omitempty"` // cleaned final content (replaces accumulated chunks)
}

type ChatThinkingEvent struct {
	Chunk string `json:"chunk"`
}

type ChatTextEvent struct {
	Chunk string `json:"chunk"`
}

type PlanningChunkEvent struct {
	Chunk string `json:"chunk"`
	Phase string `json:"phase"` // "structure", "vision"
}

type ValidationEvent struct {
	Iteration int      `json:"iteration"`
	Errors    []string `json:"errors"`
	Fixing    bool     `json:"fixing"`
}

type ToolCallEvent struct {
	Tool      string `json:"tool"`
	Arguments string `json:"arguments"`
}

type SeedProgressEvent struct {
	Phase   string `json:"phase"`
	Action  string `json:"action,omitempty"`
	Current int    `json:"current,omitempty"`
	Total   int    `json:"total,omitempty"`
	Summary string `json:"summary,omitempty"`
}
