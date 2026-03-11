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

type WebsitePreviewEvent struct {
	URL      string `json:"url"`
	ProxyURL string `json:"proxy_url"`
}

// ProgressEvent is a generic progress indicator for long-running operations.
// The frontend can display this as a progress bar or status message.
type ProgressEvent struct {
	Phase   string `json:"phase"`             // e.g. "generating_files", "provisioning_db", "generating_images", "auto_fix"
	Message string `json:"message"`           // human-readable status message
	Current int    `json:"current,omitempty"` // current step (0 if indeterminate)
	Total   int    `json:"total,omitempty"`   // total steps (0 if indeterminate)
}

// AutoFixEvent notifies the frontend that deterministic auto-fixes were applied.
type AutoFixEvent struct {
	FilesFixed int      `json:"files_fixed"`
	FixTypes   []string `json:"fix_types"` // e.g. ["ts_to_tsx", "use_client", "import_mismatch"]
	Paths      []string `json:"paths"`     // paths of fixed files
}

