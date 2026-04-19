package responses

type ShipItem struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "build" | "deployment"
	AppID      string `json:"app_id"`
	AppName    string `json:"app_name"`
	Store      string `json:"store,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Version    string `json:"version,omitempty"`
	Status     string `json:"status"`
	StatusKind string `json:"status_kind"`
	ETASeconds int    `json:"eta_seconds,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

type ShipResponse struct {
	Items []ShipItem `json:"items"`
}
