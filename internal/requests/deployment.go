package requests

type CreateDeploymentRequest struct {
	ProjectID               string   `json:"project_id"`
	BuildID                 string   `json:"build_id"`
	Platforms               []string `json:"platforms"`
	IconID                  string   `json:"icon_id"`
	Title                   string   `json:"title"`
	Description             string   `json:"description"`
	Method                  string   `json:"method"`
	GooglePlayCredentialsID string   `json:"google_play_credentials_id"`
	AppStoreCredentialsID   string   `json:"app_store_credentials_id"`
	ScreenshotPaths         []string `json:"screenshot_paths"`
}

type SignIPARequest struct {
	ProjectID     string `json:"project_id"`
	BuildID       string `json:"build_id"`
	CredentialsID string `json:"credentials_id"`
}
