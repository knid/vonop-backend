package controllers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils/external"
)

// SendComposePush executes a dashboard-driven push:
//  1. Validates the user belongs to the project's org (any role; admin-only
//     tightening is a follow-up — see plan §Open questions §9).
//  2. Resolves the v1 shadow row's OneSignal app ID + REST API key.
//  3. POSTs to OneSignal via SendNotificationViaSDK.
//  4. Persists a ComposePushHistory audit-log row (T8.4 fix).
//  5. Returns the OneSignal notification ID + recipients count.
func (c *Controller) SendComposePush(ctx context.Context, project *models.AppProject, user *models.User, req requests.ComposePushRequest) (*responses.ComposePushResponse, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	if user == nil {
		return nil, errors.New("user is required")
	}
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		return nil, errors.New("compose-push: project type must be mobile_app_native")
	}
	if _, err := c.RequireOrgMembership(user, project.OrganizationID.String()); err != nil {
		return nil, err
	}

	v := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(v); err != nil {
		return nil, err
	}

	// Resolve v1 shadow row to get the per-tenant OneSignal app ID + REST key.
	// Lazy-provision when the row is missing OR still has the placeholder ID
	// (covers projects whose CreateAppProject goroutine missed, or test fixtures
	// that don't run the eager bootstrap).
	shadowURL := "expo://" + project.Slug
	v1, err := c.DB.GetProjectByUserAndURL(project.UserID.String(), shadowURL)
	needsProvision := err != nil || v1 == nil || v1.ID == uuid.Nil ||
		v1.OneSignalAppID == "" || strings.HasPrefix(v1.OneSignalAppID, onesignalShadowPrefix)
	if needsProvision {
		if perr := c.provisionOneSignalApp(ctx, project); perr != nil {
			return nil, fmt.Errorf("compose-push: OneSignal provisioning failed: %w", perr)
		}
		v1, err = c.DB.GetProjectByUserAndURL(project.UserID.String(), shadowURL)
		if err != nil || v1 == nil || v1.ID == uuid.Nil ||
			v1.OneSignalAppID == "" || strings.HasPrefix(v1.OneSignalAppID, onesignalShadowPrefix) {
			return nil, errors.New("compose-push: OneSignal app not yet provisioned for this project; verify ONESIGNAL_ORG_ID + ONESIGNAL_ORG_API_KEY are set, then retry")
		}
	}

	// Build the audience.
	params := external.SendNotificationParams{
		Headings: req.Title,
		Contents: req.Body,
	}
	var audienceIDs []string
	switch req.AudienceType {
	case "segments":
		segments := req.AudienceSegments
		if len(segments) == 0 {
			segments = []string{"Subscribed Users"}
		}
		params.IncludedSegments = segments
		audienceIDs = segments
	case "external_ids":
		if len(req.AudienceExternalUserIDs) == 0 {
			return nil, errors.New("compose-push: external_ids audience requires at least one user id")
		}
		params.IncludeAliases = map[string][]string{"external_id": req.AudienceExternalUserIDs}
		audienceIDs = req.AudienceExternalUserIDs
	}

	// <P2#25> rich notification + scheduling + deep-link forwarding.
	// All fields are optional — empty strings stay empty and OneSignal
	// silently drops the keys.
	params.SendAfter = req.SendAfter
	params.BigPicture = req.BigPicture
	if req.IOSAttachmentURL != "" {
		params.IOSAttachments = map[string]string{"id": req.IOSAttachmentURL}
	}
	params.WebURL = req.WebURL
	params.AppURL = req.AppURL

	osResult, err := external.SendNotificationViaSDK(ctx, v1.OneSignalAppID, v1.OneSignalAppToken, params)
	if err != nil {
		return nil, fmt.Errorf("compose-push: send: %w", err)
	}

	// Persist audit-log row. Pick the FIRST title/body locale alphabetically
	// for the human-readable history columns (the agent's user app reads the
	// localised payload directly from OneSignal, so we don't need every locale
	// in the log row).
	histTitle, histBody := firstLocaleString(req.Title), firstLocaleString(req.Body)
	hist := &models.ComposePushHistory{
		AppProjectID:            project.ID,
		OrganizationID:          project.OrganizationID,
		SenderUserID:            user.ID,
		Title:                   histTitle,
		Body:                    histBody,
		AudienceType:            req.AudienceType,
		AudienceIDs:             audienceIDs,
		OneSignalNotificationID: osResult.NotificationID,
		RecipientsCount:         osResult.RecipientsCount,
	}
	if perr := c.DB.InsertComposePushHistory(hist); perr != nil {
		// Don't fail the response — the push went out, that's what matters.
		// Log so we can detect this in BetterStack if it becomes a pattern.
		slog.Warn("compose-push: history insert failed (push delivered, audit row missed)",
			"error", perr,
			"project_id", project.ID,
			"sender_user_id", user.ID,
			"onesignal_notification_id", osResult.NotificationID,
		)
	}

	return &responses.ComposePushResponse{
		NotificationID:  osResult.NotificationID,
		RecipientsCount: osResult.RecipientsCount,
	}, nil
}

// ListComposePushHistory returns paginated history for a project. Caller-supplied
// limit + offset; the database helper clamps to (0, 100] / [0, ∞).
func (c *Controller) ListComposePushHistory(ctx context.Context, project *models.AppProject, user *models.User, limit, offset int) (*responses.ComposePushHistoryResponse, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	if user == nil {
		return nil, errors.New("user is required")
	}
	if _, err := c.RequireOrgMembership(user, project.OrganizationID.String()); err != nil {
		return nil, err
	}

	rows, total, err := c.DB.ListComposePushHistory(project.ID.String(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list history: %w", err)
	}

	items := make([]responses.ComposePushHistoryItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, responses.ComposePushHistoryItem{
			ID:                      r.ID,
			Title:                   r.Title,
			Body:                    r.Body,
			AudienceType:            r.AudienceType,
			AudienceIDs:             r.AudienceIDs,
			OneSignalNotificationID: r.OneSignalNotificationID,
			RecipientsCount:         r.RecipientsCount,
			SenderUserID:            r.SenderUserID,
			CreatedAt:               r.CreatedAt,
		})
	}
	return &responses.ComposePushHistoryResponse{Items: items, Total: total}, nil
}

// firstLocaleString returns the value of the alphabetically first key in the map
// (or empty string when the map is nil/empty). Used for the audit-log
// human-readable title/body fields where one canonical locale per row suffices.
func firstLocaleString(m map[string]string) string {
	var first string
	var firstKey string
	for k, v := range m {
		if firstKey == "" || k < firstKey {
			firstKey = k
			first = v
		}
	}
	return first
}
