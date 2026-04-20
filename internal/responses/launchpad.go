package responses

// LaunchpadResponse is the single-call aggregate shape served by
// GET /v1/apps/:appId/launchpad. It powers the Ship tab's Launchpad
// surface, combining data from AppProject, MobileAppConfig, Deployment,
// Build, AppBuildTask, apple_connections, and google_play_connections
// so the frontend renders without additional round-trips.
//
// Phase 2a fills HeroStatus, WebPreview, and Stores. Listing, Builds,
// and Identity are declared here but returned zero/empty in 2a; they
// are filled in Phase 2b.
type LaunchpadResponse struct {
	App        LaunchpadApp        `json:"app"`
	HeroStatus LaunchpadHero       `json:"hero_status"`
	WebPreview LaunchpadWebPreview `json:"web_preview"`
	Stores     LaunchpadStores     `json:"stores"`

	// Phase 2b — declared so the frontend type is stable, but filled
	// with zero values in 2a. Populating these is Phase 2b's job.
	Listing  LaunchpadListing  `json:"listing"`
	Builds   LaunchpadBuilds   `json:"builds"`
	Identity LaunchpadIdentity `json:"identity"`

	// Phase 5a — most-recent in-flight or recently-concluded submission
	// (7-day window). Nil when nothing's in flight.
	RecentDeployment *LaunchpadRecentDeployment `json:"recent_deployment,omitempty"`
}

type LaunchpadRecentDeployment struct {
	ID          string `json:"id"`
	Store       string `json:"store"`
	Status      string `json:"status"`
	StatusKind  string `json:"status_kind"`
	SubmittedAt string `json:"submitted_at"`
	Method      string `json:"method"`
	// ETASeconds is the median elapsed time from submitted → store_live
	// across platform deployments in the last 30 days. Zero means we
	// don't have enough data yet (fewer than 3 paired events); frontend
	// falls back to "a few days" for managed submissions.
	ETASeconds int `json:"eta_seconds,omitempty"`
}

type LaunchpadApp struct {
	ID            string                 `json:"id"`            // AppProject ID (uuid string)
	Name          string                 `json:"name"`
	BundleID      string                 `json:"bundle_id"`     // e.g. "com.tasktracker.pro"
	IconS3Key     string                 `json:"icon_s3_key"`   // build_defaults.icon_s3_key
	VersionName   string                 `json:"version_name"`  // "1.0.3"
	BuildNumber   int                    `json:"build_number"`  // monotonic
	V1ProjectID   string                 `json:"v1_project_id,omitempty"`
	BuildDefaults LaunchpadBuildDefaults `json:"build_defaults"`
}

type LaunchpadBuildDefaults struct {
	AppName             string `json:"app_name,omitempty"`
	IconS3Key           string `json:"icon_s3_key,omitempty"`
	SplashColor         string `json:"splash_color,omitempty"`
	NavBarStyle         string `json:"navbar_style,omitempty"`
	RemoveWemobBranding bool   `json:"remove_wemob_branding,omitempty"`
}

// LaunchpadHero is the computed one-sentence summary + single primary
// CTA that answers "what do I do now?".
type LaunchpadHero struct {
	Summary     string `json:"summary"`      // "Live on Play Store — App Store needs 3 things"
	PrimaryCTA  string `json:"primary_cta"`  // human label, e.g. "Finish App Store setup"
	PrimaryHref string `json:"primary_href"` // deep link within /apps/:id/ship
}

type LaunchpadWebPreview struct {
	Status     string `json:"status"` // "deployed" | "building" | "failed" | "none"
	URL        string `json:"url,omitempty"`
	DeployedAt string `json:"deployed_at,omitempty"` // ISO 8601
}

type LaunchpadStores struct {
	PlayStore LaunchpadStoreTile `json:"play_store"`
	AppStore  LaunchpadStoreTile `json:"app_store"`
}

type LaunchpadStoreTile struct {
	// Status vocabulary: "not_configured" | "configuring" | "ready_to_ship"
	// | "building" | "needs_review" | "submitted" | "in_review" | "live"
	// | "update_available" | "rejected" | "failed"
	Status             string   `json:"status"`
	LiveVersion        string   `json:"live_version,omitempty"`    // "1.0.2"
	LiveBuildNum       int      `json:"live_build_num,omitempty"`  // 11
	InstallCount       int      `json:"install_count,omitempty"`   // only when live, Play Store
	Rating             float64  `json:"rating,omitempty"`          // Play Store
	MissingItems       []string `json:"missing_items,omitempty"`   // e.g. ["Screenshots (3 needed)", "Privacy URL"]
	PrimaryAction      string   `json:"primary_action,omitempty"`  // "Submit update" | "Start setup" | "View listing"
	ConnectionKind     string   `json:"connection_kind,omitempty"` // "managed" | "user" | ""
	LatestDeploymentID string   `json:"latest_deployment_id,omitempty"`
}

// Stubs for Phase 2b — declared for type stability.

// LaunchpadListing summarises the store-listing readiness. Each tile
// is one piece of metadata the stores need (icon, title, description,
// screenshots, locale, category, privacy URL, release notes).
type LaunchpadListing struct {
	ReadyItems int                    `json:"ready_items"` // tiles with status "ready"
	TotalItems int                    `json:"total_items"` // always 8 in 2b
	Tiles      []LaunchpadListingTile `json:"tiles"`
}

// LaunchpadListingTile is one of the 8 listing items. Status vocabulary:
// "ready" (green check), "warn" (yellow, needed but not blocking),
// "missing" (amber dashed, blocking), "empty" (grey, optional).
type LaunchpadListingTile struct {
	Key         string `json:"key"`          // "icon" | "title" | "description" | "screenshots"
	                                          // | "locale" | "category" | "privacy_url" | "release_notes"
	Label       string `json:"label"`        // "Icon", "Title", "Description", …
	Status      string `json:"status"`       // "ready" | "warn" | "missing" | "empty"
	Summary     string `json:"summary"`      // e.g. "TaskTracker Pro" | "14 chars" | "1 of 3 needed"
	Remediation string `json:"remediation"`  // optional — "Generate with AI", empty if not applicable
}

type LaunchpadBuilds struct {
	Active []LaunchpadBuildSummary `json:"active"`
	Recent []LaunchpadBuildSummary `json:"recent"`
}

type LaunchpadBuildSummary struct {
	ID             string `json:"id"`
	BuildID        string `json:"build_id"`
	Platform       string `json:"platform"`                   // "android" | "ios"
	Status         string `json:"status"`
	StatusText     string `json:"status_text,omitempty"`
	Finished       bool   `json:"finished"`
	Version        string `json:"version,omitempty"`
	BuildNumber    int    `json:"build_number,omitempty"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	AnnotationID   string `json:"annotation_id,omitempty"`
	AnnotationName string `json:"annotation_name,omitempty"`
	AnnotationMsg  string `json:"annotation_human_message,omitempty"`
	AnnotationFix  string `json:"annotation_suggested_fix,omitempty"`
}

type LaunchpadIdentity struct {
	BundleID    string   `json:"bundle_id"`
	Version     string   `json:"version"` // "1.0.3 (Build 12)"
	Permissions []string `json:"permissions"`
}
