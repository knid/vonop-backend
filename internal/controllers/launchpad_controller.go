package controllers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
)

// GetLaunchpad assembles the single aggregate response for the Ship
// tab's Launchpad surface. Callers supply the AppProject ID and the
// authenticated user; the controller enforces ownership before building
// the response.
//
// Returns (nil, error) when the app doesn't exist or the user doesn't
// own it; callers translate that to 404 or 403.
func (c *Controller) GetLaunchpad(ctx context.Context, user *models.User, appID uuid.UUID) (*responses.LaunchpadResponse, error) {
	if user == nil {
		return nil, errors.New("user required")
	}
	// Load the project (and verify ownership).
	var project models.AppProject
	if err := c.DB.DB.NewSelect().
		Model(&project).
		Where("id = ?", appID).
		Scan(ctx); err != nil {
		return nil, err
	}
	if project.UserID != user.ID {
		// Organization-shared apps land here too; Phase 2a doesn't
		// cover org membership checks — that's Phase 4/5.
		return nil, errors.New("not authorized")
	}

	resp := &responses.LaunchpadResponse{
		App: responses.LaunchpadApp{
			ID:   project.ID.String(),
			Name: project.Name,
		},
	}

	// Load build_defaults and fill App identity fields.
	if bd, ok := project.Settings["build_defaults"].(map[string]interface{}); ok {
		if s, _ := bd["app_name"].(string); s != "" {
			resp.App.BuildDefaults.AppName = s
		}
		if s, _ := bd["icon_s3_key"].(string); s != "" {
			resp.App.BuildDefaults.IconS3Key = s
			resp.App.IconS3Key = s
		}
		if s, _ := bd["splash_color"].(string); s != "" {
			resp.App.BuildDefaults.SplashColor = s
		}
		if s, _ := bd["navbar_style"].(string); s != "" {
			resp.App.BuildDefaults.NavBarStyle = s
		}
		if b, _ := bd["remove_wemob_brand"].(bool); b {
			resp.App.BuildDefaults.RemoveWemobBranding = b
		}
	}

	// Bundle ID is derived from project.Slug (existing CM_PACKAGE_NAME
	// convention). Phase 2b adds a proper override field.
	if project.Slug != "" {
		resp.App.BundleID = "com.wemob." + project.Slug
	}

	// V1 project id — used for all downstream lookups that go through
	// the legacy Build/Deployment tables. Absent on apps that haven't
	// been provisioned yet; in that case we bail on stores/version.
	var v1ProjectID string
	if v, ok := project.Settings["v1_project_id"].(string); ok {
		v1ProjectID = v
	}

	if v1ProjectID != "" {
		var latestBuild models.Build
		if err := c.DB.DB.NewSelect().
			Model(&latestBuild).
			Where("project_id = ?", v1ProjectID).
			Order("created_at DESC").
			Limit(1).
			Scan(ctx); err == nil {
			resp.App.VersionName = latestBuild.VersionString
			resp.App.BuildNumber = latestBuild.BuildNumber
		}
	}

	// WebPreview — reuse the existing GetLatestDeploymentStatus helper
	// which resolves the real Coolify status for non-terminal deployments.
	resp.WebPreview = c.buildWebPreview(ctx, project)

	// Stores — per-platform tile.
	resp.Stores.PlayStore = c.buildStoreTile(ctx, user.ID, v1ProjectID, "android")
	resp.Stores.AppStore = c.buildStoreTile(ctx, user.ID, v1ProjectID, "ios")

	// Listing — tile-by-tile store listing readiness.
	resp.Listing = c.buildListing(ctx, project.ID)

	// Builds — active (in-flight) + recent (last 5 finished) build tasks.
	resp.Builds = c.buildBuilds(ctx, v1ProjectID)

	// Identity — bundle ID, version string, and permissions.
	resp.Identity = c.buildIdentity(ctx, project.ID, resp.App)

	// Hero — pure function of the populated response.
	resp.HeroStatus = computeHero(resp)

	return resp, nil
}

// buildIdentity constructs the LaunchpadIdentity from the already-computed
// LaunchpadApp (for BundleID and version fields) and a MobileAppConfig DB
// lookup (for the permissions list).
func (c *Controller) buildIdentity(ctx context.Context, appProjectID uuid.UUID, app responses.LaunchpadApp) responses.LaunchpadIdentity {
	id := responses.LaunchpadIdentity{
		BundleID:    app.BundleID,
		Permissions: []string{},
	}
	if app.VersionName != "" {
		id.Version = fmt.Sprintf("%s (Build %d)", app.VersionName, app.BuildNumber)
	} else {
		id.Version = "not yet built"
	}

	var cfg models.MobileAppConfig
	if err := c.DB.DB.NewSelect().
		Model(&cfg).
		Where("project_id = ?", appProjectID).
		Scan(ctx); err == nil {
		if cfg.Permissions != nil {
			id.Permissions = cfg.Permissions
		}
	}

	return id
}

// buildWebPreview delegates to the existing GetLatestDeploymentStatus helper
// (which handles Coolify polling for non-terminal states) and normalises the
// result into the LaunchpadWebPreview shape.
func (c *Controller) buildWebPreview(ctx context.Context, project models.AppProject) responses.LaunchpadWebPreview {
	// GetLatestDeploymentStatus takes a string project ID and queries
	// AppDeployment (platform="web"). The ctx parameter isn't used by the
	// existing helper, but we keep the signature consistent.
	dep, err := c.GetLatestDeploymentStatus(project.ID.String())
	if err != nil {
		return responses.LaunchpadWebPreview{Status: "none"}
	}

	deployedAt := ""
	if !dep.UpdatedAt.IsZero() {
		deployedAt = dep.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	return responses.LaunchpadWebPreview{
		Status:     normalizeWebStatus(dep.Status),
		URL:        dep.DeployedURL,
		DeployedAt: deployedAt,
	}
}

// normalizeWebStatus maps the raw AppDeployment status strings to the
// LaunchpadWebPreview vocabulary.
// Real values observed in app_deployments: "pending", "building", "deployed",
// "failed", "undeployed".
func normalizeWebStatus(s string) string {
	switch strings.ToLower(s) {
	case "deployed":
		return "deployed"
	case "building", "pending":
		return "building"
	case "failed":
		return "failed"
	case "undeployed", "":
		return "none"
	}
	return "none"
}

// buildStoreTile queries the latest Deployment for the given platform and
// the user's connection presence, then returns a LaunchpadStoreTile.
func (c *Controller) buildStoreTile(ctx context.Context, userID uuid.UUID, v1ProjectID, platform string) responses.LaunchpadStoreTile {
	tile := responses.LaunchpadStoreTile{
		Status:       "not_configured",
		MissingItems: []string{},
	}
	if v1ProjectID == "" {
		return tile
	}

	// Is there a connection for this platform on the user?
	hasConnection := false
	switch platform {
	case "ios":
		var count int
		_ = c.DB.DB.NewSelect().
			Model((*models.AppleConnection)(nil)).
			Where("user_id = ?", userID).
			Where("deleted_at IS NULL").
			ColumnExpr("count(*)").
			Scan(ctx, &count)
		hasConnection = count > 0
	case "android":
		var count int
		_ = c.DB.DB.NewSelect().
			Model((*models.GooglePlayConnection)(nil)).
			Where("user_id = ?", userID).
			Where("deleted_at IS NULL").
			ColumnExpr("count(*)").
			Scan(ctx, &count)
		hasConnection = count > 0
	}

	// Latest deployment for this platform.
	var latestDep models.Deployment
	err := c.DB.DB.NewSelect().
		Model(&latestDep).
		Where("project_id = ?", v1ProjectID).
		Where("? = ANY(platforms)", platform).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)

	if err != nil {
		// No deployment yet — compute readiness from connection presence.
		if !hasConnection {
			switch platform {
			case "ios":
				tile.MissingItems = append(tile.MissingItems, "Apple Developer account")
			case "android":
				tile.MissingItems = append(tile.MissingItems, "Google Play account")
			}
			tile.Status = "not_configured"
			tile.PrimaryAction = "Start setup"
		} else {
			tile.Status = "ready_to_ship"
			tile.PrimaryAction = "Start setup"
			tile.MissingItems = []string{}
		}
		return tile
	}

	// Map deployment status to tile status.
	// Real values written by the reports webhook: "Queued", "Deploying",
	// "Deployed", "error". Case-insensitive matching handles any
	// capitalisation variance from Codemagic.
	switch strings.ToLower(latestDep.Status) {
	case "deployed":
		tile.Status = "live"
		tile.PrimaryAction = "Submit update"
	case "deploying":
		tile.Status = "submitted"
		tile.PrimaryAction = "View listing"
	case "reviewing", "submitted":
		tile.Status = "in_review"
		tile.PrimaryAction = "View listing"
	case "error", "rejected":
		tile.Status = "rejected"
		tile.PrimaryAction = "Fix"
	case "queued", "building":
		tile.Status = "submitted"
		tile.PrimaryAction = "View listing"
	default:
		tile.Status = "submitted"
		tile.PrimaryAction = "View listing"
	}

	// managed vs user track.
	if strings.EqualFold(latestDep.Method, "wemob") {
		tile.ConnectionKind = "managed"
	} else {
		tile.ConnectionKind = "user"
	}

	tile.MissingItems = []string{}
	return tile
}

// buildListing reads AppStoreListing and MobileAppConfig for the given
// AppProject and returns a fully-populated LaunchpadListing with 8 tiles.
func (c *Controller) buildListing(ctx context.Context, appProjectID uuid.UUID) responses.LaunchpadListing {
	listing := responses.LaunchpadListing{
		TotalItems: 8,
		Tiles:      []responses.LaunchpadListingTile{},
	}

	var sl models.AppStoreListing
	_ = c.DB.DB.NewSelect().
		Model(&sl).
		Where("app_project_id = ?", appProjectID).
		Scan(ctx)

	var cfg models.MobileAppConfig
	_ = c.DB.DB.NewSelect().
		Model(&cfg).
		Where("project_id = ?", appProjectID).
		Scan(ctx)

	sensitivePerms := map[string]struct{}{
		"camera": {}, "microphone": {}, "photos": {},
		"location": {}, "contacts": {}, "bluetooth": {},
	}
	hasSensitive := false
	for _, p := range cfg.Permissions {
		if _, ok := sensitivePerms[strings.ToLower(p)]; ok {
			hasSensitive = true
			break
		}
	}

	// Icon
	iconTile := responses.LaunchpadListingTile{Key: "icon", Label: "Icon"}
	if sl.IconS3Key != "" {
		iconTile.Status = "ready"
		iconTile.Summary = "1024×1024 PNG"
	} else {
		iconTile.Status = "missing"
		iconTile.Summary = "Required"
		iconTile.Remediation = "Use project icon"
	}
	listing.Tiles = append(listing.Tiles, iconTile)

	// Title
	titleTile := responses.LaunchpadListingTile{Key: "title", Label: "Title"}
	title := strings.TrimSpace(sl.Title)
	switch {
	case len(title) == 0:
		titleTile.Status = "missing"
		titleTile.Summary = "Required"
		titleTile.Remediation = "Use project name"
	case len(title) < 2:
		titleTile.Status = "warn"
		titleTile.Summary = "Too short"
	case len(title) > 30:
		titleTile.Status = "warn"
		titleTile.Summary = fmt.Sprintf("%d chars (max 30)", len(title))
	default:
		titleTile.Status = "ready"
		titleTile.Summary = fmt.Sprintf("%d chars", len(title))
	}
	listing.Tiles = append(listing.Tiles, titleTile)

	// Description
	descTile := responses.LaunchpadListingTile{Key: "description", Label: "Description"}
	desc := strings.TrimSpace(sl.Description)
	switch {
	case len(desc) == 0:
		descTile.Status = "missing"
		descTile.Summary = "Required"
		descTile.Remediation = "Draft with AI"
	case len(desc) < 80:
		descTile.Status = "warn"
		descTile.Summary = fmt.Sprintf("%d chars (min 80)", len(desc))
	case len(desc) < 170:
		descTile.Status = "warn"
		descTile.Summary = fmt.Sprintf("%d chars (170 recommended)", len(desc))
	default:
		descTile.Status = "ready"
		descTile.Summary = fmt.Sprintf("%d chars", len(desc))
	}
	listing.Tiles = append(listing.Tiles, descTile)

	// Screenshots
	ssTile := responses.LaunchpadListingTile{Key: "screenshots", Label: "Screenshots"}
	count := len(sl.Screenshots)
	switch {
	case count == 0:
		ssTile.Status = "missing"
		ssTile.Summary = "3 needed"
		ssTile.Remediation = "Generate with AI"
	case count < 3:
		ssTile.Status = "missing"
		ssTile.Summary = fmt.Sprintf("%d of 3 needed", count)
		ssTile.Remediation = "Generate with AI"
	case count < 5:
		ssTile.Status = "warn"
		ssTile.Summary = fmt.Sprintf("%d uploaded (5 recommended)", count)
	default:
		ssTile.Status = "ready"
		ssTile.Summary = fmt.Sprintf("%d uploaded", count)
	}
	listing.Tiles = append(listing.Tiles, ssTile)

	// Locale
	localeTile := responses.LaunchpadListingTile{
		Key:     "locale",
		Label:   "Locale",
		Status:  "ready",
		Summary: firstNonEmpty(sl.Locale, "en-US"),
	}
	listing.Tiles = append(listing.Tiles, localeTile)

	// Category
	catTile := responses.LaunchpadListingTile{
		Key:     "category",
		Label:   "Category",
		Status:  "empty",
		Summary: "Set in store console",
	}
	listing.Tiles = append(listing.Tiles, catTile)

	// Privacy URL
	privacyTile := responses.LaunchpadListingTile{Key: "privacy_url", Label: "Privacy URL"}
	privacy := strings.TrimSpace(sl.PrivacyPolicyURL)
	switch {
	case privacy == "" && hasSensitive:
		privacyTile.Status = "missing"
		privacyTile.Summary = "Required (sensitive permissions in use)"
		privacyTile.Remediation = "Generate for me"
	case privacy == "":
		privacyTile.Status = "warn"
		privacyTile.Summary = "Recommended"
	default:
		privacyTile.Status = "ready"
		privacyTile.Summary = privacy
	}
	listing.Tiles = append(listing.Tiles, privacyTile)

	// Release notes
	rnTile := responses.LaunchpadListingTile{
		Key:     "release_notes",
		Label:   "Release notes",
		Status:  "empty",
		Summary: "Drafted per build",
	}
	listing.Tiles = append(listing.Tiles, rnTile)

	for _, t := range listing.Tiles {
		if t.Status == "ready" {
			listing.ReadyItems++
		}
	}

	return listing
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// buildBuilds queries AppBuildTask for the given v1ProjectID and returns
// a LaunchpadBuilds with two slices:
//   - Active: all tasks where finished=false, ordered newest first.
//   - Recent: last 5 tasks where finished=true, ordered newest first.
//
// Both slices are always initialised (never nil) so the JSON serialises
// as [] rather than null.
func (c *Controller) buildBuilds(ctx context.Context, v1ProjectID string) responses.LaunchpadBuilds {
	builds := responses.LaunchpadBuilds{
		Active: []responses.LaunchpadBuildSummary{},
		Recent: []responses.LaunchpadBuildSummary{},
	}
	if v1ProjectID == "" {
		return builds
	}

	var active []models.AppBuildTask
	if err := c.DB.DB.NewSelect().
		Model(&active).
		Relation("Annotation").
		Relation("Build").
		Where("app_build_task.project_id = ?", v1ProjectID).
		Where("app_build_task.finished = ?", false).
		Order("app_build_task.created_at DESC").
		Scan(ctx); err == nil {
		for _, t := range active {
			builds.Active = append(builds.Active, buildTaskToSummary(t))
		}
	}

	var recent []models.AppBuildTask
	if err := c.DB.DB.NewSelect().
		Model(&recent).
		Relation("Annotation").
		Relation("Build").
		Where("app_build_task.project_id = ?", v1ProjectID).
		Where("app_build_task.finished = ?", true).
		Order("app_build_task.created_at DESC").
		Limit(5).
		Scan(ctx); err == nil {
		for _, t := range recent {
			builds.Recent = append(builds.Recent, buildTaskToSummary(t))
		}
	}

	return builds
}

// buildTaskToSummary converts an AppBuildTask (with its Annotation and Build
// relations eagerly loaded) into a flat LaunchpadBuildSummary. Nil-checks on
// the relations guard against tasks fetched without the relations pre-loaded.
func buildTaskToSummary(t models.AppBuildTask) responses.LaunchpadBuildSummary {
	s := responses.LaunchpadBuildSummary{
		ID:         t.ID.String(),
		BuildID:    t.BuildID.String(),
		Platform:   t.Platform,
		Status:     t.StatusText,
		StatusText: t.StatusText,
		Finished:   t.Finished,
		CreatedAt:  t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:  t.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	if t.Build != nil {
		s.Version = t.Build.VersionString
		s.BuildNumber = t.Build.BuildNumber
	}
	if t.ErrorMessage != "" {
		s.ErrorMessage = t.ErrorMessage
	}
	if t.AnnotationID != nil && t.Annotation != nil {
		s.AnnotationID = t.AnnotationID.String()
		s.AnnotationName = t.Annotation.Name
		s.AnnotationMsg = t.Annotation.HumanMessage
		s.AnnotationFix = t.Annotation.SuggestedFix
	}
	return s
}

// computeHero is a pure function: it takes the already-populated response
// and returns a one-sentence summary + primary CTA based on the priority
// ladder (biggest blocker wins).
func computeHero(r *responses.LaunchpadResponse) responses.LaunchpadHero {
	if r.Stores.AppStore.Status == "rejected" {
		return responses.LaunchpadHero{
			Summary:     "App Store rejected the last submission.",
			PrimaryCTA:  "Fix App Store rejection",
			PrimaryHref: "#app-store",
		}
	}
	if r.Stores.PlayStore.Status == "rejected" {
		return responses.LaunchpadHero{
			Summary:     "Play Store rejected the last submission.",
			PrimaryCTA:  "Fix Play Store rejection",
			PrimaryHref: "#play-store",
		}
	}
	if r.Stores.PlayStore.Status == "live" && r.Stores.AppStore.Status == "not_configured" {
		return responses.LaunchpadHero{
			Summary:     "Live on Play Store — App Store not yet configured.",
			PrimaryCTA:  "Start App Store setup",
			PrimaryHref: "#app-store",
		}
	}
	if r.Stores.AppStore.Status == "live" && r.Stores.PlayStore.Status == "not_configured" {
		return responses.LaunchpadHero{
			Summary:     "Live on App Store — Play Store not yet configured.",
			PrimaryCTA:  "Start Play Store setup",
			PrimaryHref: "#play-store",
		}
	}
	if r.Stores.PlayStore.Status == "live" && r.Stores.AppStore.Status == "live" {
		return responses.LaunchpadHero{
			Summary:     "Live on both stores.",
			PrimaryCTA:  "View listings",
			PrimaryHref: "#stores",
		}
	}
	if r.WebPreview.Status == "none" {
		return responses.LaunchpadHero{
			Summary:     "Deploy the web preview to unlock mobile builds.",
			PrimaryCTA:  "Deploy web preview",
			PrimaryHref: "#web",
		}
	}
	return responses.LaunchpadHero{
		Summary:     "Ready to ship. Pick a store below.",
		PrimaryCTA:  "",
		PrimaryHref: "",
	}
}
