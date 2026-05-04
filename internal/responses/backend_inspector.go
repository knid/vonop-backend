package responses

// BackendHealthResponse is what GET /v2/apps/{id}/backend/health returns.
type BackendHealthResponse struct {
	HasBackend      bool   `json:"has_backend"`
	URL             string `json:"url,omitempty"`
	MachineState    string `json:"machine_state,omitempty"` // "started" | "stopped" | "destroyed" | ...
	Region          string `json:"region,omitempty"`
	Image           string `json:"image,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
	LastDeployAt    string `json:"last_deploy_at,omitempty"`
	LastDeployOK    bool   `json:"last_deploy_ok"`
	LastDeployURL   string `json:"last_deploy_url,omitempty"`
	DerivedColor    string `json:"derived_color"` // "green" | "yellow" | "red" | "gray"
	Notes           string `json:"notes,omitempty"`
	IsDeploying     bool   `json:"is_deploying"`
	DeployStatus    string `json:"deploy_status,omitempty"`
	DeployStartedAt string `json:"deploy_started_at,omitempty"`
}

// BackendRoutesResponse is what GET /v2/apps/{id}/backend/routes returns.
type BackendRoutesResponse struct {
	Routes []BackendRouteEntry `json:"routes"`
}

type BackendRouteEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	File   string `json:"file"`
}

// BackendLogsResponse is what GET /v2/apps/{id}/backend/logs returns.
type BackendLogsResponse struct {
	Lines []BackendLogLine `json:"lines"`
}

type BackendLogLine struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}
