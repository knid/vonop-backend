package responses

import "github.com/google/uuid"

type DashboardResponse struct {
	ID          uuid.UUID                `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Provider    string                   `json:"provider"`
	Status      string                   `json:"status"`
	Settings    map[string]any           `json:"settings"`
	Schema      any                      `json:"schema,omitempty"`
	Panels      []DashboardPanelResponse `json:"panels,omitempty"`
	CreatedAt   int64                    `json:"created_at"`
	UpdatedAt   int64                    `json:"updated_at"`
}

type DashboardPanelResponse struct {
	ID        uuid.UUID      `json:"id"`
	Title     string         `json:"title"`
	PanelType string         `json:"panel_type"`
	Query     any            `json:"query"`
	Config    map[string]any `json:"config"`
	SortOrder int            `json:"sort_order"`
	GridWidth int            `json:"grid_width"`
}

type DashboardDataResponse struct {
	Panels []PanelDataResponse `json:"panels"`
}

type PanelDataResponse struct {
	PanelID uuid.UUID `json:"panel_id"`
	Title   string    `json:"title"`
	Type    string    `json:"type"`
	Data    any       `json:"data"`
	Error   string    `json:"error,omitempty"`
}

type CellUpdateResponse struct {
	Success bool   `json:"success"`
	Table   string `json:"table"`
	Column  string `json:"column"`
	Value   any    `json:"value"`
}
