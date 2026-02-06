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
