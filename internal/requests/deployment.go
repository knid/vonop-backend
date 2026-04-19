package requests

type CreateDeploymentRequest struct {
	ProjectID               string   `json:"project_id"`
	BuildID                 string   `json:"build_id"`
	Platforms               []string `json:"platforms"`
	IconID                  string   `json:"icon_id"`
	Title                   string   `json:"title"`
	Description             string   `json:"description"`
	Method                  string   `json:"method"`
	GooglePlayConnectionID  string   `json:"google_play_credentials_id"`
	AppleConnectionID       string   `json:"app_store_credentials_id"`
	ScreenshotPaths         []string `json:"screenshot_paths"`
	Locale                  string   `json:"locale"`
}

type SignIPARequest struct {
	ProjectID     string `json:"project_id"`
	BuildID       string `json:"build_id"`
	CredentialsID string `json:"credentials_id"`
}
