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
}

type LaunchpadApp struct {
	ID          string `json:"id"`           // AppProject ID (uuid string)
	Name        string `json:"name"`
	BundleID    string `json:"bundle_id"`    // e.g. "com.tasktracker.pro"
	IconS3Key   string `json:"icon_s3_key"`  // build_defaults.icon_s3_key
	VersionName string `json:"version_name"` // "1.0.3"
	BuildNumber int    `json:"build_number"` // monotonic
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
	Status         string   `json:"status"`
	LiveVersion    string   `json:"live_version,omitempty"`    // "1.0.2"
	LiveBuildNum   int      `json:"live_build_num,omitempty"`  // 11
	InstallCount   int      `json:"install_count,omitempty"`   // only when live, Play Store
	Rating         float64  `json:"rating,omitempty"`          // Play Store
	MissingItems   []string `json:"missing_items,omitempty"`   // e.g. ["Screenshots (3 needed)", "Privacy URL"]
	PrimaryAction  string   `json:"primary_action,omitempty"`  // "Submit update" | "Start setup" | "View listing"
	ConnectionKind string   `json:"connection_kind,omitempty"` // "managed" | "user" | ""
}

// Stubs for Phase 2b — declared for type stability.

type LaunchpadListing struct {
	ReadyItems int `json:"ready_items"` // out of 8 tiles
	TotalItems int `json:"total_items"`
}

type LaunchpadBuilds struct {
	Active []LaunchpadBuildSummary `json:"active"`
	Recent []LaunchpadBuildSummary `json:"recent"`
}

type LaunchpadBuildSummary struct {
	ID        string `json:"id"`
	Platform  string `json:"platform"` // "android" | "ios"
	Status    string `json:"status"`
	Version   string `json:"version,omitempty"`
	CreatedAt string `json:"created_at"`
}

type LaunchpadIdentity struct {
	BundleID    string   `json:"bundle_id"`
	Version     string   `json:"version"` // "1.0.3 (Build 12)"
	Permissions []string `json:"permissions"`
}
