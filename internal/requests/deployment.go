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
	// ReleaseNotes — optional "what's new in this version" copy captured
	// at submission time (populated by QuickShip in the frontend).
	ReleaseNotes string `json:"release_notes,omitempty"`
	// RolloutTrack — optional track selection. Values:
	//   Play Store: "internal" | "closed" | "open" | "production"
	//   App Store:  "internal" | "closed" | "production" (TestFlight variants)
	// Empty defaults to "production" behavior at submit time.
	RolloutTrack string `json:"rollout_track,omitempty"`
	// DeferConnection — when true with method = "user", the deployment is
	// created in status "pending_connection" instead of being submitted.
	// Codemagic is not triggered; the user is telling us they'll set up
	// their Apple / Google connection later. The deployment auto-resumes
	// when the matching connection is added (see ResumePendingDeployments).
	DeferConnection bool `json:"defer_connection,omitempty"`
}

type SignIPARequest struct {
	ProjectID     string `json:"project_id"`
	BuildID       string `json:"build_id"`
	CredentialsID string `json:"credentials_id"`
}
