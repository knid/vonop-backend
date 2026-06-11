package responses

// SSEEvent is the in-process envelope passed from legacy view StreamFuncs
// to handleStreamOrAsync's main goroutine. The main goroutine is the single
// owner of `Seq` — it stamps a monotonic per-job sequence number on every
// event before persisting and publishing. The seq flows out to clients as
// the SSE `id: <seq>` line so reconnect can resume idempotently via
// `Last-Event-ID`.
type SSEEvent struct {
	Seq   int64  `json:"seq,omitempty"`
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
	Action   string `json:"action"`
	FilePath string `json:"file_path"`
	FileType string `json:"file_type"`
	Content  string `json:"content,omitempty"` // cleaned final content (replaces accumulated chunks)
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

// ToolResultEvent is emitted after a tool executes so the frontend can render
// the tool output inline. Fields are intentionally loose: every tool yields
// a string result from its executor; the frontend uses `tool` to pick a
// renderer and falls back to a monospace preview if no match exists.
type ToolResultEvent struct {
	Tool    string `json:"tool"`
	Status  string `json:"status"`             // "ok" or "error"
	Preview string `json:"preview,omitempty"`  // Truncated human-readable result (≤ 600 chars)
	Summary string `json:"summary,omitempty"`  // One-line TL;DR suitable for a collapsed card
	IsError bool   `json:"is_error,omitempty"` // Legacy convenience flag — duplicates Status == "error"
}

// CreditsUpdateEvent is emitted periodically during a chat turn so the client
// can render a live cost meter. CreditsUsed is a snapshot of the running
// deduction and will match the final value in chat_complete.credits_used when
// the turn finishes cleanly.
type CreditsUpdateEvent struct {
	CreditsUsed int `json:"credits_used"`
	Tokens      int `json:"tokens,omitempty"`
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
