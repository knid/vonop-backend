package controllers

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/codemagic"
	"github.com/knid/wemob/internal/services/storeauth"
)

// SubmitExpoOptions describes which platform to submit + per-call overrides.
//
// Track / Phased / SubmitAsDraft / RolloutFraction can override the per-app
// AppProject defaults if the user wants to ship a one-off to a different
// destination without changing the persisted setting.
type SubmitExpoOptions struct {
	Platform string // "android" or "ios"

	// Optional per-call overrides — empty / zero means "use AppProject default".
	TrackOverride         string
	PhasedReleaseOverride *bool
	SubmitAsDraftOverride *bool
	RolloutFraction       float64 // 0 = no staged rollout
	// IOS-only: when true, push to App Store review (in addition to TestFlight).
	// Defaults to false (TestFlight-only submission).
	SubmitToAppStore bool
	// ReleaseNotes: per-platform release notes sent to the store.
	ReleaseNotes string
}

// SubmitResult is what SubmitExpoMobile returns to the view.
type SubmitResult struct {
	Platform         string
	BuildTaskID      uuid.UUID // the AppBuildTask the deploy was based on
	CodemagicBuildID string
}

// Sentinel errors so the view can map to specific HTTP statuses.
var (
	ErrSubmitWrongProjectType  = errors.New("submit: project is not mobile_app_native")
	ErrSubmitNoBuildToSubmit   = errors.New("submit: no successful build for that platform yet")
	ErrSubmitMissingCredential = errors.New("submit: project has no default connection for that platform")
	ErrSubmitInvalidPlatform   = errors.New("submit: platform must be android or ios")
	ErrSubmitInvalidTrack      = errors.New("submit: invalid track value")
	ErrVersionMustBeGreater    = errors.New("submit: build version must be greater than the live store version")
)

// SubmitExpoMobile submits the latest successful Expo build to the App Store
// (TestFlight) or Play Store (selected track). Per-platform — caller submits
// each platform independently.
//
// Resolution flow:
//  1. Project type guard (mobile_app_native only)
//  2. Find the v1 shadow project for slug (created by BuildExpoMobile)
//  3. Find the latest successful AppBuildTask for the requested platform
//  4. Resolve per-app default credential (Apple via DefaultAppleConnectionID;
//     Google via DefaultGoogleConnectionID)
//  5. Decrypt via CredStore (P0.3+P1.0); base64-encode for transport
//  6. Trigger the Codemagic deploy workflow with all submission flags
func (c *Controller) SubmitExpoMobile(ctx context.Context, project *models.AppProject, user *models.User, opts SubmitExpoOptions) (*SubmitResult, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil, ErrSubmitWrongProjectType
	}
	if c.CodemagicService == nil {
		return nil, fmt.Errorf("codemagic service is not configured")
	}

	platform := strings.ToLower(strings.TrimSpace(opts.Platform))
	switch platform {
	case "android", "ios":
	default:
		return nil, ErrSubmitInvalidPlatform
	}

	// Find the v1 shadow project keyed by URL = expo://<slug> (created by BuildExpoMobile).
	v1project, err := c.DB.GetProjectByUserAndURL(user.ID.String(), "expo://"+project.Slug)
	if err != nil || v1project == nil || v1project.ID == uuid.Nil {
		return nil, ErrSubmitNoBuildToSubmit
	}

	// Find the most recent successful AppBuildTask for the requested platform.
	platformLabel := "Android"
	if platform == "ios" {
		platformLabel = "IOS"
	}
	task, err := c.DB.GetLatestSuccessfulBuildTaskByProjectAndPlatform(ctx, v1project.ID, platformLabel)
	if err != nil || task == nil {
		return nil, ErrSubmitNoBuildToSubmit
	}
	build, err := c.DB.GetProjectBuildById(task.BuildID.String())
	if err != nil || build == nil {
		return nil, fmt.Errorf("submit: build row missing for task %s: %w", task.ID, err)
	}

	// Resolve track + flags (per-call overrides win over per-app defaults).
	playTrack := nonEmptyOr(opts.TrackOverride, project.PlayStoreTrack)
	if playTrack == "" {
		playTrack = "internal"
	}
	appStoreTrack := nonEmptyOr(opts.TrackOverride, project.AppStoreTrack)
	if appStoreTrack == "" {
		appStoreTrack = "internal"
	}
	phased := project.PhasedRelease
	if opts.PhasedReleaseOverride != nil {
		phased = *opts.PhasedReleaseOverride
	}
	draft := project.SubmitAsDraft
	if opts.SubmitAsDraftOverride != nil {
		draft = *opts.SubmitAsDraftOverride
	}

	// P1#15 — load the per-project store listing + every per-locale row so we
	// can pass the metadata through to Apple/Play. Best-effort: if the listing
	// hasn't been saved yet, we fall back to an empty set so the submission
	// still goes through (the publishing block then keeps whatever is already
	// in App Store Connect / Play Console).
	listing, locales, listingErr := c.GetListingForSubmit(ctx, project.ID.String(), "")
	if listingErr != nil {
		slog.Warn("SubmitExpoMobile: listing not found, submitting without per-locale metadata",
			"project_id", project.ID, "err", listingErr)
		listing = nil
		locales = nil
	}

	switch platform {
	case "android":
		return c.submitAndroid(ctx, project, build, task, playTrack, opts.RolloutFraction, draft, opts.ReleaseNotes, listing, locales)
	case "ios":
		return c.submitIOS(ctx, project, build, task, appStoreTrack, opts.SubmitToAppStore, phased, draft, opts.ReleaseNotes, listing, locales)
	}
	return nil, ErrSubmitInvalidPlatform
}

// listingLocalesForSubmit converts the per-locale rows into the codemagic
// ListingLocale slice. The parent listing's default Locale is added with
// the parent's Title/Description if no row for it exists yet — this means
// the wizard's single-locale path stays compatible with the new flow.
func listingLocalesForSubmit(listing *models.AppStoreListing, locales map[string]*models.AppStoreListingLocalization) []codemagic.ListingLocale {
	if listing == nil {
		return nil
	}
	out := make([]codemagic.ListingLocale, 0, len(locales)+1)
	seen := make(map[string]bool, len(locales))
	for loc, row := range locales {
		if row == nil {
			continue
		}
		out = append(out, codemagic.ListingLocale{
			Locale:           loc,
			Title:            row.Title,
			Subtitle:         row.Subtitle,
			Description:      row.Description,
			Keywords:         row.Keywords,
			PromotionalText:  row.PromotionalText,
			ReleaseNotes:     row.ReleaseNotes,
			ShortDescription: row.ShortDescription,
			MarketingURL:     row.MarketingURL,
			SupportURL:       row.SupportURL,
			Screenshots:      row.Screenshots,
		})
		seen[loc] = true
	}
	// Backfill the parent listing's default locale from the legacy
	// Title / Description / Locale fields when no per-locale row exists
	// for it yet — preserves single-locale-wizard behavior.
	defaultLoc := listing.Locale
	if defaultLoc == "" {
		defaultLoc = "en-US"
	}
	if !seen[defaultLoc] && (listing.Title != "" || listing.Description != "") {
		out = append(out, codemagic.ListingLocale{
			Locale:      defaultLoc,
			Title:       listing.Title,
			Description: listing.Description,
		})
	}
	return out
}

func (c *Controller) submitAndroid(ctx context.Context, project *models.AppProject, build *models.Build, task *models.AppBuildTask, track string, rolloutFraction float64, draft bool, releaseNotes string, listing *models.AppStoreListing, locales map[string]*models.AppStoreListingLocalization) (*SubmitResult, error) {
	switch track {
	case "internal", "alpha", "beta", "production":
	default:
		return nil, ErrSubmitInvalidTrack
	}
	if project.DefaultGoogleConnectionID == nil {
		return nil, ErrSubmitMissingCredential
	}
	conn, err := c.DB.GetGooglePlayConnectionByOrg(ctx, project.OrganizationID, *project.DefaultGoogleConnectionID)
	if err != nil || conn == nil {
		return nil, fmt.Errorf("submit: load Google Play connection: %w", err)
	}
	saBytes, err := c.ReadGoogleSA(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("submit: decrypt SA JSON: %w", err)
	}
	sa, err := storeauth.ParseServiceAccount(saBytes)
	if err != nil {
		return nil, fmt.Errorf("submit: parse SA JSON: %w", err)
	}
	bundleID := bundleIDForProject(project, "")
	liveVC, err := storeauth.GetPlayLiveVersionCode(ctx, sa, bundleID, track)
	if err != nil {
		return nil, fmt.Errorf("submit: check live play version: %w", err)
	}
	if liveVC > 0 && int64(build.BuildNumber) <= liveVC {
		return nil, fmt.Errorf("submit: build number %d is not greater than live version code %d on track %q: %w", build.BuildNumber, liveVC, track, ErrVersionMustBeGreater)
	}
	params := codemagic.ExpoAndroidDeployParams{
		ProjectID:             project.ID,
		BuildID:               build.ID,
		TaskID:                task.ID,
		PackageName:           bundleID,
		BuildNumber:           build.BuildNumber,
		ServiceAccountJSONB64: base64.StdEncoding.EncodeToString(saBytes),
		Track:                 track,
		RolloutFraction:       rolloutFraction,
		SubmitAsDraft:         draft,
		AABS3Key:              fmt.Sprintf("aabs/%s.aab", build.ID.String()),
		ReleaseNotes:          releaseNotes,
		Locales:               listingLocalesForSubmit(listing, locales),
	}
	if listing != nil {
		params.Copyright = listing.Copyright
		params.PrimaryCategory = listing.PrimaryCategory
		params.SecondaryCategory = listing.SecondaryCategory
		params.EULAType = listing.EULAType
		params.EULAURL = listing.EULAURL
	}
	cmBuildID, err := c.CodemagicService.TriggerExpoAndroidDeploy(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("submit: trigger codemagic android deploy: %w", err)
	}
	slog.Info("SubmitExpoMobile: android submit triggered",
		"project_id", project.ID, "build_id", build.ID, "task_id", task.ID,
		"track", track, "draft", draft, "codemagic_build_id", cmBuildID)
	return &SubmitResult{Platform: "android", BuildTaskID: task.ID, CodemagicBuildID: cmBuildID}, nil
}

func (c *Controller) submitIOS(ctx context.Context, project *models.AppProject, build *models.Build, task *models.AppBuildTask, track string, submitToAppStore bool, phased bool, draft bool, releaseNotes string, listing *models.AppStoreListing, locales map[string]*models.AppStoreListingLocalization) (*SubmitResult, error) {
	switch track {
	case "internal", "external", "production":
	default:
		return nil, ErrSubmitInvalidTrack
	}
	if project.DefaultAppleConnectionID == nil {
		return nil, ErrSubmitMissingCredential
	}
	conn, err := c.DB.GetAppleConnectionByOrg(ctx, project.OrganizationID, *project.DefaultAppleConnectionID)
	if err != nil || conn == nil {
		return nil, fmt.Errorf("submit: load Apple connection: %w", err)
	}
	p8Bytes, err := c.ReadApplePrimaryP8(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("submit: decrypt .p8: %w", err)
	}
	bundleID := bundleIDForProject(project, "")
	liveBuild, err := storeauth.FetchLatestBuildNumber(ctx, storeauth.AppleConnectionInput{
		KeyID:    conn.KeyID,
		IssuerID: conn.IssuerID,
		P8PEM:    p8Bytes,
	}, bundleID)
	if err != nil {
		return nil, fmt.Errorf("submit: check live apple build number: %w", err)
	}
	if liveBuild > 0 && build.BuildNumber <= liveBuild {
		return nil, fmt.Errorf("submit: build number %d is not greater than live build number %d: %w", build.BuildNumber, liveBuild, ErrVersionMustBeGreater)
	}
	// Always submit to TestFlight; SubmitToAppStore toggles the additional production push.
	params := codemagic.ExpoIOSDeployParams{
		ProjectID:          project.ID,
		BuildID:            build.ID,
		TaskID:             task.ID,
		BundleID:           bundleID,
		BuildNumber:        build.BuildNumber,
		ASCAPIKeyB64:       base64.StdEncoding.EncodeToString(p8Bytes),
		ASCKeyID:           conn.KeyID,
		ASCIssuerID:        conn.IssuerID,
		SubmitToTestFlight: true,
		SubmitToAppStore:   submitToAppStore || track == "production",
		PhasedRelease:      phased && (submitToAppStore || track == "production"),
		SubmitAsDraft:      draft,
		IPAS3Key:           fmt.Sprintf("ipas/%s.ipa", build.ID.String()),
		ReleaseNotes:       releaseNotes,
		Locales:            listingLocalesForSubmit(listing, locales),
	}
	if listing != nil {
		params.Copyright = listing.Copyright
		params.PrimaryCategory = listing.PrimaryCategory
		params.SecondaryCategory = listing.SecondaryCategory
		params.EULAType = listing.EULAType
		params.EULAURL = listing.EULAURL
	}
	cmBuildID, err := c.CodemagicService.TriggerExpoIOSDeploy(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("submit: trigger codemagic ios deploy: %w", err)
	}
	slog.Info("SubmitExpoMobile: ios submit triggered",
		"project_id", project.ID, "build_id", build.ID, "task_id", task.ID,
		"track", track, "submit_to_app_store", submitToAppStore, "phased", phased, "draft", draft,
		"codemagic_build_id", cmBuildID)
	return &SubmitResult{Platform: "ios", BuildTaskID: task.ID, CodemagicBuildID: cmBuildID}, nil
}
