package controllers

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// Sentinel errors so the view can map controller errors to HTTP statuses
// without inspecting message strings.
var (
	// ErrMinVersionProjectNotFound — the slug/ID doesn't resolve.
	ErrMinVersionProjectNotFound = errors.New("project not found")
	// ErrMinVersionNotMobileNative — only mobile_app_native projects have
	// a min_version mechanism. WebView/web projects can't enforce one.
	ErrMinVersionNotMobileNative = errors.New("min_version is only supported on mobile_app_native projects")
	// ErrMinVersionPermissionDenied — caller is not org owner/admin.
	ErrMinVersionPermissionDenied = errors.New("only organization owners and admins can update min_version")
	// ErrMinVersionUnknownPlatform — req.Platform isn't ios or android.
	ErrMinVersionUnknownPlatform = errors.New("platform must be ios or android")
)

// GetProjectForPublicMobile resolves a project by slug first and falls back
// to UUID when the slug lookup misses. Used by the public min-version
// endpoint so the mobile app can pass either form (the codemagic build sets
// app.json's slug = project.Slug, so slug is the typical case at runtime).
//
// Returns nil + ErrMinVersionProjectNotFound on a miss so the view can map
// to a clean 404 without exposing whether it tried slug or ID first.
func (c *Controller) GetProjectForPublicMobile(slugOrID string) (*models.AppProject, error) {
	if slugOrID == "" {
		return nil, ErrMinVersionProjectNotFound
	}
	if project, err := c.DB.GetAppProjectBySlug(slugOrID); err == nil && project != nil && project.ID != uuid.Nil {
		return project, nil
	}
	if project, err := c.DB.GetAppProjectByID(slugOrID); err == nil && project != nil && project.ID != uuid.Nil {
		return project, nil
	}
	return nil, ErrMinVersionProjectNotFound
}

// MinVersionForPlatform returns (min_build_number, store_url, message) for
// the given project + platform. Honors the per-customer custom message when
// set; otherwise falls back to a default English string.
//
// Missing MobileAppConfig → all zeros + default message (the gate is
// effectively disabled because min=0 means "no minimum").
func (c *Controller) MinVersionForPlatform(project *models.AppProject, platform string) (int64, string, string, error) {
	if project == nil {
		return 0, "", "", ErrMinVersionProjectNotFound
	}
	if platform != "ios" && platform != "android" {
		return 0, "", "", ErrMinVersionUnknownPlatform
	}

	var minBuild int64
	var storeURL string
	var message string

	cfg, err := c.DB.GetMobileAppConfig(project.ID.String())
	if err == nil && cfg != nil {
		switch platform {
		case "ios":
			minBuild = cfg.MinBuildNumberIOS
			// AppStoreURL was added in P3.1. When that branch isn't merged
			// yet, this field doesn't exist on the model and storeURL stays
			// empty — the mobile app's gate handles empty storeURL by
			// hiding the button. Once P3.1 merges, this lights up
			// automatically without a code change here.
			storeURL = c.appStoreURLFromConfig(cfg)
		case "android":
			minBuild = cfg.MinBuildNumberAndroid
			storeURL = c.playStoreURLFromConfig(cfg)
		}
		message = cfg.MinVersionMessage
	}

	if message == "" {
		message = "A new version is required. Please update to continue."
	}
	return minBuild, storeURL, message, nil
}

// appStoreURLFromConfig + playStoreURLFromConfig are tiny indirection points
// so this file compiles cleanly whether or not P3.1's AppStoreURL/
// PlayStoreURL columns exist on the model. When P3.1 merges, swap these to
// `cfg.AppStoreURL` / `cfg.PlayStoreURL` directly.
func (c *Controller) appStoreURLFromConfig(_ *models.MobileAppConfig) string {
	// P3.1 (not yet merged into main) adds AppStoreURL on MobileAppConfig.
	// Returning "" until then keeps this branch buildable from origin/main.
	// Once P3.1 lands, change to: return cfg.AppStoreURL
	return ""
}

func (c *Controller) playStoreURLFromConfig(_ *models.MobileAppConfig) string {
	// Same as above, for PlayStoreURL.
	return ""
}

// UpdateMinVersion applies a new floor + message to the project's
// MobileAppConfig. The caller MUST be an owner or admin of the project's
// organization — bumping the floor locks out users on older builds, so
// this is a sensitive lever.
//
// Creates a MobileAppConfig row when none exists (matches the upsert
// semantics in agent_deps.go).
func (c *Controller) UpdateMinVersion(user *models.User, projectID string, platform string, minBuildNumber int64, message string) (*models.MobileAppConfig, error) {
	if user == nil {
		return nil, ErrMinVersionPermissionDenied
	}
	if platform != "ios" && platform != "android" {
		return nil, ErrMinVersionUnknownPlatform
	}

	project, err := c.DB.GetAppProjectByID(projectID)
	if err != nil || project == nil || project.ID == uuid.Nil {
		return nil, ErrMinVersionProjectNotFound
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil, ErrMinVersionNotMobileNative
	}

	// Org-membership role check. Mirrors MoveAppProjectToOrg's pattern.
	member, err := c.DB.GetOrganizationMember(project.OrganizationID.String(), user.ID.String())
	if err != nil || member == nil || (member.Role != "owner" && member.Role != "admin") {
		return nil, ErrMinVersionPermissionDenied
	}

	// Load existing config or seed a new one. We don't pre-populate the
	// other fields because UpsertMobileAppConfig's Set clauses are
	// EXCLUDED-based, so any field NOT explicitly set on the input falls
	// back to its column default on insert and to its EXCLUDED-bound value
	// (which itself comes from the input) on update. Loading the existing
	// row first preserves the OTHER fields on the per-platform update.
	cfg, err := c.DB.GetMobileAppConfig(projectID)
	if err != nil || cfg == nil {
		cfg = &models.MobileAppConfig{
			ProjectID: project.ID,
		}
	}

	switch platform {
	case "ios":
		cfg.MinBuildNumberIOS = minBuildNumber
	case "android":
		cfg.MinBuildNumberAndroid = minBuildNumber
	}
	cfg.MinVersionMessage = message

	updated, err := c.DB.UpsertMobileAppConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("upsert mobile app config: %w", err)
	}
	return updated, nil
}
