package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
	"github.com/posthog/posthog-go"
)

// Sentinel errors for release operations. Used by views / cron to map to
// HTTP statuses or differentiate "feature not yet implemented" from real
// failures.
var (
	ErrReleaseNotFound          = errors.New("release: not found")
	ErrReleasePlatformInvalid   = errors.New("release: platform must be ios or android")
	ErrReleaseNotImplemented    = errors.New("release: operation not implemented for this platform yet")
	ErrReleaseConnectionMissing = errors.New("release: project has no connection for this platform")
)

// CreateReleaseFromSubmitInput is the bag of parameters
// SubmitExpoMobile passes after Codemagic accepts a deploy. Splitting it
// out keeps the controller method signature stable as we add fields.
type CreateReleaseFromSubmitInput struct {
	AppProjectID     uuid.UUID
	OrganizationID   uuid.UUID
	Platform         string // "ios" | "android"
	Track            string
	VersionCode      int
	MarketingVersion string
	CodemagicTaskID  *uuid.UUID
	BuildID          *uuid.UUID
	RolloutFraction  float64
	// PhasedRequested toggles "ACTIVE" as the initial phased state for iOS.
	PhasedRequested bool
}

// CreateReleaseFromSubmit registers a Release row in status=uploaded, after
// Codemagic accepts the submit. Idempotent on
// (app_project_id, platform, track, version_code) via UpsertRelease.
func (c *Controller) CreateReleaseFromSubmit(ctx context.Context, in CreateReleaseFromSubmitInput) (*models.Release, error) {
	if in.AppProjectID == uuid.Nil {
		return nil, fmt.Errorf("CreateReleaseFromSubmit: app_project_id required")
	}
	switch in.Platform {
	case models.ReleasePlatformIOS, models.ReleasePlatformAndroid:
	default:
		return nil, ErrReleasePlatformInvalid
	}
	now := time.Now().UTC()
	r := &models.Release{
		ID:               uuid.New(),
		AppProjectID:     in.AppProjectID,
		OrganizationID:   in.OrganizationID,
		Platform:         in.Platform,
		Track:            in.Track,
		VersionCode:      in.VersionCode,
		VersionNumber:    in.VersionCode,
		MarketingVersion: in.MarketingVersion,
		Status:           models.ReleaseStatusUploaded,
		StatusMessage:    "Submitted to store via Codemagic",
		CodemagicTaskID:  in.CodemagicTaskID,
		BuildID:          in.BuildID,
		RolloutFraction:  in.RolloutFraction,
		SubmittedAt:      &now,
	}
	if in.PhasedRequested && in.Platform == models.ReleasePlatformIOS {
		r.PhasedReleaseState = "ACTIVE"
	}
	saved, err := c.DB.UpsertRelease(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("upsert release: %w", err)
	}
	c.emitReleaseEvent(saved, "uploaded", nil)
	return saved, nil
}

// MarkReleaseInReview transitions a release to in_review. Idempotent —
// re-calls when the row is already in_review, approved, or live become a
// no-op.
func (c *Controller) MarkReleaseInReview(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusInReview, message, occurredAt)
}

// MarkReleaseApproved transitions a release to approved.
func (c *Controller) MarkReleaseApproved(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusApproved, message, occurredAt)
}

// MarkReleaseLive transitions a release to live. This is the canonical
// "the app is actually on the store" signal — replaces today's lie that
// "Codemagic finished uploading" means live.
func (c *Controller) MarkReleaseLive(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusLive, message, occurredAt)
}

// MarkReleaseRejected transitions a release to rejected (Apple's
// REJECTED / METADATA_REJECTED states, or Play's "rejected" track release).
func (c *Controller) MarkReleaseRejected(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusRejected, message, occurredAt)
}

// MarkReleaseManuallyRejected — developer pulled the binary back themselves.
func (c *Controller) MarkReleaseManuallyRejected(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusManuallyRejected, message, occurredAt)
}

// MarkReleaseInvalid — Apple rejected the binary as INVALID_BINARY.
func (c *Controller) MarkReleaseInvalid(ctx context.Context, id uuid.UUID, message string, occurredAt time.Time) (*models.Release, error) {
	return c.transitionRelease(ctx, id, models.ReleaseStatusInvalid, message, occurredAt)
}

func (c *Controller) transitionRelease(ctx context.Context, id uuid.UUID, target, message string, occurredAt time.Time) (*models.Release, error) {
	if id == uuid.Nil {
		return nil, ErrReleaseNotFound
	}
	before, err := c.DB.GetRelease(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get release before transition: %w", err)
	}
	r, err := c.DB.MarkReleaseStatus(ctx, id, target, message, occurredAt)
	if err != nil {
		return nil, fmt.Errorf("mark release status: %w", err)
	}
	if before.Status != r.Status {
		c.emitReleaseEvent(r, "status_changed", map[string]any{
			"from":   before.Status,
			"to":     r.Status,
			"reason": message,
		})
	}
	return r, nil
}

// GetReleasesForApp returns all releases for an AppProject.
func (c *Controller) GetReleasesForApp(ctx context.Context, appProjectID uuid.UUID) ([]models.Release, error) {
	return c.DB.ListReleasesByProject(ctx, appProjectID)
}

// GetCurrentLiveRelease returns the latest live release for (project, platform).
// Returns (nil, nil) when nothing is live yet.
func (c *Controller) GetCurrentLiveRelease(ctx context.Context, appProjectID uuid.UUID, platform string) (*models.Release, error) {
	return c.DB.GetCurrentLiveRelease(ctx, appProjectID, platform)
}

// RollbackRelease marks `releaseID` as rolled_back and stamps `targetID`
// with rolled_back_from_release_id = releaseID. Both rows are updated
// atomically. This DOES NOT call Apple/Google to actually rollback — that's
// a follow-up. It only updates Wemob's view so the dashboard can show the
// state correctly while operations roll the public-facing release back.
func (c *Controller) RollbackRelease(ctx context.Context, releaseID, targetID uuid.UUID) (*models.Release, error) {
	if releaseID == uuid.Nil || targetID == uuid.Nil {
		return nil, fmt.Errorf("RollbackRelease: both release_id and target_id required")
	}
	if releaseID == targetID {
		return nil, fmt.Errorf("RollbackRelease: target must differ from source")
	}
	if err := c.DB.SetReleaseRollbackTarget(ctx, releaseID, targetID); err != nil {
		return nil, fmt.Errorf("set rollback target: %w", err)
	}
	r, err := c.DB.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, fmt.Errorf("reload release after rollback: %w", err)
	}
	c.emitReleaseEvent(r, "rolled_back", map[string]any{"target_release_id": targetID.String()})
	return r, nil
}

// HaltPhasedRollout pauses a phased rollout on the upstream store. iOS is
// implemented (ASC PATCH /v1/appStoreVersionPhasedReleases/{id}). Android
// returns ErrReleaseNotImplemented today — Play's halt-rollout endpoint
// requires opening an edit + writing a halted release; tracked as follow-up.
//
// On either path, the local Release.Status is set to "paused" so the user
// sees the desired state even when the upstream API call fails. Callers can
// retry by invoking again.
func (c *Controller) HaltPhasedRollout(ctx context.Context, releaseID uuid.UUID) (*models.Release, error) {
	if releaseID == uuid.Nil {
		return nil, ErrReleaseNotFound
	}
	r, err := c.DB.GetRelease(ctx, releaseID)
	if err != nil {
		return nil, fmt.Errorf("get release: %w", err)
	}

	// Always update the local view first so the dashboard shows the user's
	// desired state. Upstream call follows; if it fails, the user can retry.
	if _, err := c.DB.SetReleasePhasedState(ctx, r.ID, "PAUSED",
		models.ReleaseStatusPaused, "Phased rollout halted"); err != nil {
		return nil, fmt.Errorf("set local phased state: %w", err)
	}
	c.emitReleaseEvent(r, "phased_paused", nil)

	switch r.Platform {
	case models.ReleasePlatformIOS:
		if err := c.haltApplePhasedRollout(ctx, r); err != nil {
			slog.Warn("HaltPhasedRollout: apple call failed, local state set to paused — user can retry",
				"release_id", r.ID, "err", err)
			return r, fmt.Errorf("halt apple phased rollout: %w", err)
		}
		return c.DB.GetRelease(ctx, r.ID)
	case models.ReleasePlatformAndroid:
		// TODO: Play's "halt" requires opening an edit and writing a release
		// with status=halted. Not implemented yet — the frontend should hide
		// or disable the "Pause Rollout" button for Android until this is wired.
		// Returning ErrReleaseNotImplemented so callers can surface a clear message.
		return r, ErrReleaseNotImplemented
	default:
		return nil, ErrReleasePlatformInvalid
	}
}

// haltApplePhasedRollout calls ASC PATCH /v1/appStoreVersionPhasedReleases/{id}
// with phasedReleaseState = "PAUSED". Requires a valid AppleConnection
// resolved from the AppProject's default — caller must have ensured that.
func (c *Controller) haltApplePhasedRollout(ctx context.Context, r *models.Release) error {
	if r.StoreReleaseID == "" {
		return fmt.Errorf("release has no apple appStoreVersionPhasedRelease id")
	}
	// Resolve the AppProject so we can find the default Apple connection.
	project, err := c.DB.GetAppProjectByID(r.AppProjectID.String())
	if err != nil || project == nil {
		return fmt.Errorf("load app_project: %w", err)
	}
	if project.DefaultAppleConnectionID == nil {
		return ErrReleaseConnectionMissing
	}
	conn, err := c.DB.GetAppleConnectionByOrg(ctx, project.OrganizationID, *project.DefaultAppleConnectionID)
	if err != nil || conn == nil {
		return fmt.Errorf("load apple connection: %w", err)
	}
	p8Bytes, err := c.ReadApplePrimaryP8(ctx, conn)
	if err != nil {
		return fmt.Errorf("decrypt p8: %w", err)
	}
	in := storeauth.AppleConnectionInput{
		KeyID:    conn.KeyID,
		IssuerID: conn.IssuerID,
		P8PEM:    p8Bytes,
	}
	if err := storeauth.PatchAppStoreVersionPhasedReleaseState(ctx, in, r.StoreReleaseID, "PAUSED"); err != nil {
		return err
	}
	return nil
}

// emitReleaseEvent records a deployment-style event for Release lifecycle
// transitions. Today this is logged + sent to PostHog as a fire-and-forget
// signal; later this is where audit-log writes will land.
func (c *Controller) emitReleaseEvent(r *models.Release, kind string, extra map[string]any) {
	if r == nil {
		return
	}
	slog.Info("release event",
		"release_id", r.ID,
		"app_project_id", r.AppProjectID,
		"platform", r.Platform,
		"track", r.Track,
		"version_code", r.VersionCode,
		"status", r.Status,
		"kind", kind)
	if c.PostHog == nil {
		return
	}
	props := posthog.NewProperties().
		Set("release_id", r.ID.String()).
		Set("app_project_id", r.AppProjectID.String()).
		Set("organization_id", r.OrganizationID.String()).
		Set("platform", r.Platform).
		Set("track", r.Track).
		Set("version_code", r.VersionCode).
		Set("status", r.Status).
		Set("kind", kind)
	for k, v := range extra {
		props.Set(k, v)
	}
	c.PostHog.Enqueue(posthog.Capture{
		DistinctId: r.AppProjectID.String(),
		Event:      "release_lifecycle",
		Properties: props,
	})
}

// ascNotificationPayload is the minimum shape we extract from an
// App Store Connect Notifications webhook. The full schema has more fields
// — only the ones we map below are used here.
type ascNotificationPayload struct {
	NotificationType string `json:"notificationType"`
	Data             struct {
		AppStoreVersionID string `json:"appStoreVersionId"`
		AppStoreState     string `json:"appStoreState"`
		ReviewStatus      string `json:"reviewStatus"`
		Reason            string `json:"reason"`
	} `json:"data"`
}

// HandleASCNotification accepts a JSON payload from the ASC notifications
// webhook (or our own polling worker if it forwards through this method)
// and routes it to the correct release status. Idempotent — relies on
// MarkReleaseStatus to dedupe repeats.
func (c *Controller) HandleASCNotification(ctx context.Context, body []byte) error {
	var p ascNotificationPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("decode asc notification: %w", err)
	}
	if p.Data.AppStoreVersionID == "" {
		// Notifications without a version id (e.g. account-level) are not
		// actionable for releases — accept and ignore.
		return nil
	}
	r, err := c.DB.GetReleaseByStoreID(ctx, models.ReleasePlatformIOS, p.Data.AppStoreVersionID)
	if err != nil {
		// Unknown release id — log and ack so Apple stops retrying.
		slog.Warn("asc notification: release not found", "appStoreVersionId", p.Data.AppStoreVersionID, "err", err)
		return nil
	}
	target, msg := mapAppleStateToReleaseStatus(p.Data.AppStoreState, p.NotificationType, p.Data.Reason)
	if target == "" {
		return nil
	}
	if _, err := c.transitionRelease(ctx, r.ID, target, msg, time.Now().UTC()); err != nil {
		return fmt.Errorf("transition release from asc notification: %w", err)
	}
	return nil
}

// mapAppleStateToReleaseStatus maps Apple's appStoreState (and notificationType)
// to one of our release statuses. Returns "" when the event is informational
// and should not transition the release.
func mapAppleStateToReleaseStatus(appStoreState, notificationType, reason string) (string, string) {
	if appStoreState == "" && notificationType != "" {
		appStoreState = notificationType
	}
	switch appStoreState {
	case "WAITING_FOR_REVIEW", "PENDING_REVIEW":
		return models.ReleaseStatusUploaded, "Apple: " + appStoreState
	case "IN_REVIEW":
		return models.ReleaseStatusInReview, "Apple: IN_REVIEW"
	case "PENDING_DEVELOPER_RELEASE", "PROCESSING_FOR_APP_STORE":
		return models.ReleaseStatusApproved, "Apple: " + appStoreState
	case "READY_FOR_SALE":
		return models.ReleaseStatusLive, "Apple: READY_FOR_SALE"
	case "REJECTED", "METADATA_REJECTED":
		msg := "Apple: " + appStoreState
		if reason != "" {
			msg = msg + " — " + reason
		}
		return models.ReleaseStatusRejected, msg
	case "DEVELOPER_REJECTED":
		return models.ReleaseStatusManuallyRejected, "Apple: DEVELOPER_REJECTED"
	case "INVALID_BINARY":
		return models.ReleaseStatusInvalid, "Apple: INVALID_BINARY"
	}
	return "", ""
}

// MapPlayTrackStatusToReleaseStatus maps Google Play track release status
// strings to our release statuses. "completed" → live, "halted" → paused,
// etc. Exported for the polling worker.
func MapPlayTrackStatusToReleaseStatus(playStatus string) (string, string) {
	switch playStatus {
	case "draft":
		return models.ReleaseStatusDraft, "Play: draft"
	case "inProgress":
		return models.ReleaseStatusInReview, "Play: inProgress"
	case "halted":
		return models.ReleaseStatusPaused, "Play: halted"
	case "completed":
		return models.ReleaseStatusLive, "Play: completed"
	}
	return "", ""
}

// playRTDNCleartext is the JSON shape we expect after base64-decoding the
// Pub/Sub message data field. Real-Time Developer Notifications fan out
// across subscription, oneTimeProduct, and voidedPurchase notifications —
// none of which directly cover "publishing went live", which is why the
// polling worker remains canonical for release-status detection.
type playRTDNCleartext struct {
	Version                  string `json:"version"`
	PackageName              string `json:"packageName"`
	EventTimeMillis          string `json:"eventTimeMillis"`
	SubscriptionNotification struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		SubscriptionID   string `json:"subscriptionId"`
	} `json:"subscriptionNotification,omitempty"`
	OneTimeProductNotification struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		Sku              string `json:"sku"`
	} `json:"oneTimeProductNotification,omitempty"`
	VoidedPurchaseNotification struct {
		PurchaseToken string `json:"purchaseToken"`
		OrderID       string `json:"orderId"`
	} `json:"voidedPurchaseNotification,omitempty"`
}

// HandlePlayRTDN parses the inner Pub/Sub notification body. RTDN does not
// surface release-publishing state changes, so this method exists today
// mainly as scaffolding for the future IAP / subscription wiring. We log
// and acknowledge.
func (c *Controller) HandlePlayRTDN(ctx context.Context, cleartext []byte) error {
	var n playRTDNCleartext
	if err := json.Unmarshal(cleartext, &n); err != nil {
		return fmt.Errorf("decode play rtdn: %w", err)
	}
	switch {
	case n.SubscriptionNotification.NotificationType != 0:
		slog.Info("play rtdn: subscription notification (not yet wired)",
			"package", n.PackageName, "type", n.SubscriptionNotification.NotificationType)
	case n.OneTimeProductNotification.NotificationType != 0:
		slog.Info("play rtdn: one-time-product notification (not yet wired)",
			"package", n.PackageName, "type", n.OneTimeProductNotification.NotificationType)
	case n.VoidedPurchaseNotification.PurchaseToken != "":
		slog.Info("play rtdn: voided-purchase notification (not yet wired)",
			"package", n.PackageName)
	default:
		slog.Info("play rtdn: ack and ignore (no actionable section)", "package", n.PackageName)
	}
	return nil
}

// httpDoLimited is a tiny helper used by ASC release calls. It mirrors the
// pattern in storepoll/apple.go — bounded body read, 4xx surfaced.
func httpDoLimited(req *http.Request, client *http.Client, max int64) ([]byte, int, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, max))
	return body, resp.StatusCode, nil
}

// jsonBuf serializes any to a *bytes.Buffer for HTTP POST/PATCH bodies.
func jsonBuf(v any) (*bytes.Buffer, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewBuffer(b), nil
}
