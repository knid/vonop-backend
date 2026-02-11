package responses

import "github.com/google/uuid"

type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type WebsiteCreatedEvent struct {
	WebsiteResponse
	TotalPages int      `json:"total_pages"`
	PageTitles []string `json:"page_titles"`
}

type PageGeneratedEvent struct {
	PageResponse
	PageIndex  int `json:"page_index"`
	TotalPages int `json:"total_pages"`
}

type GenerationCompleteEvent struct {
	WebsiteID uuid.UUID `json:"website_id"`
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
}

type GenerationErrorEvent struct {
	Error string `json:"error"`
	Phase string `json:"phase"`
}

type CodeChunkEvent struct {
	PageIndex  int    `json:"page_index"`
	PageTitle  string `json:"page_title"`
	Chunk      string `json:"chunk"`
	TotalPages int    `json:"total_pages"`
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

type ChatCompleteEvent struct {
	WebsiteID    uuid.UUID `json:"website_id"`
	Summary      string    `json:"summary"`
	FilesChanged int       `json:"files_changed"`
}

type FileGenerationStartEvent struct {
	WebsiteResponse
	TotalFiles int      `json:"total_files"`
	FilePaths  []string `json:"file_paths"`
}

type ChatThinkingEvent struct {
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
