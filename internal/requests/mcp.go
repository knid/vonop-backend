package requests

type CreateMCPConnectionRequest struct {
	TemplateID  string         `json:"template_id"`
	Name        string         `json:"name"`
	ServerURL   string         `json:"server_url"`
	AuthType    string         `json:"auth_type"`
	Credentials map[string]any `json:"credentials"`
	Config      map[string]any `json:"config"`
}

type StartMCPOAuthRequest struct {
	ConnectionID string         `json:"connection_id"`
	TemplateID   string         `json:"template_id"`
	Name         string         `json:"name"`
	ServerURL    string         `json:"server_url"`
	Config       map[string]any `json:"config"`
	RedirectURL  string         `json:"redirect_url"`
}

type PutAgentMCPGrantRequest struct {
	Enabled       bool              `json:"enabled"`
	DefaultPolicy string            `json:"default_policy"`
	ToolPolicies  map[string]string `json:"tool_policies"`
}

type SubmitMCPApprovalRequest struct {
	Decision string `json:"decision"`
}
