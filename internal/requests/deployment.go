package requests

import "github.com/google/uuid"

type CreateDeploymentRequest struct {
	ProjectID              string    `json:"project_id"`
	BuildID                string    `json:"build_id"`
	Platforms              []string  `json:"platforms"`
	IconID                 string    `json:"icon_id"`
	Title                  string    `json:"title"`
	Description            string    `json:"description"`
	Method                 string    `json:"method"`
	GooglePlayConnectionID string    `json:"google_play_credentials_id"`
	AppleConnectionID      string    `json:"app_store_credentials_id"`
	ScreenshotPaths        []string  `json:"screenshot_paths"`
	Locale                 string    `json:"locale"`
	OrganizationID         uuid.UUID `json:"organization_id" validate:"required" errormgs:"organization_id is required"`
	// ReleaseNotes — optional "what's new in this version" copy captured
	// at submission time (populated by QuickShip in the frontend).
	ReleaseNotes string `json:"release_notes,omitempty"`
	// RolloutTrack — optional track selection. Values:
	//   Play Store: "internal" | "closed" | "open" | "production"
	//   App Store:  "internal" | "closed" | "production" (TestFlight variants)
	// Empty defaults to "production" behavior at submit time.
	RolloutTrack string `json:"rollout_track,omitempty"`
}

// UpdateDeploymentRequest mirrors CreateDeploymentRequest minus the
// organization_id requirement — updates do not move deployments between orgs.
type UpdateDeploymentRequest struct {
	ProjectID              string   `json:"project_id"`
	BuildID                string   `json:"build_id"`
	Platforms              []string `json:"platforms"`
	IconID                 string   `json:"icon_id"`
	Title                  string   `json:"title"`
	Description            string   `json:"description"`
	Method                 string   `json:"method"`
	GooglePlayConnectionID string   `json:"google_play_credentials_id"`
	AppleConnectionID      string   `json:"app_store_credentials_id"`
	ScreenshotPaths        []string `json:"screenshot_paths"`
	Locale                 string   `json:"locale"`
	ReleaseNotes           string   `json:"release_notes,omitempty"`
	RolloutTrack           string   `json:"rollout_track,omitempty"`
}
