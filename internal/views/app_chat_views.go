package views

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// legacyProxyURLPattern matches our own asset proxy URL shape so the deprecated
// Images []string field on AppChatRequest can be resolved to AttachmentIDs
// during the rollout window.
var legacyProxyURLPattern = regexp.MustCompile(`/v2/apps/[^/]+/assets/([0-9a-fA-F\-]{36})/file`)

func extractAssetIDsFromLegacyURLs(urls []string) []uuid.UUID {
	var ids []uuid.UUID
	for _, u := range urls {
		m := legacyProxyURLPattern.FindStringSubmatch(u)
		if len(m) != 2 {
			slog.Warn("chat: legacy images URL did not match proxy pattern, dropping", "url", u)
			continue
		}
		id, err := uuid.Parse(m[1])
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// POST /v2/apps/{appId}/chat
func (v *HttpViews) ChatApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user == nil {
		utils.JSONErrorCode(w, constants.ErrAuthRequired, http.StatusUnauthorized, nil)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.checkMinimumCredits(w, user) {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if project == nil {
		utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.AppChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	// Deprecation shim: map legacy `images` URLs to attachment_ids.
	if len(req.AttachmentIDs) == 0 && len(req.Images) > 0 {
		req.AttachmentIDs = extractAssetIDsFromLegacyURLs(req.Images)
	}

	// Per-turn attachment count cap.
	if len(req.AttachmentIDs) > constants.MaxAttachmentsPerTurn {
		utils.JSONError(w, constants.ErrValidationOutOfRange,
			fmt.Sprintf("Too many attachments (max %d per message).", constants.MaxAttachmentsPerTurn),
			http.StatusBadRequest, map[string]any{
				"field": "attachment_ids",
				"limit": constants.MaxAttachmentsPerTurn,
			})
		return
	}

	// Resolve attachment IDs and the project-wide pool in one go.
	turnAttachments, attachErr := loadTurnAttachments(r.Context(), v.Controller.DB, project.ID, req.AttachmentIDs)
	if attachErr != nil {
		utils.ValidationError(w, attachErr)
		return
	}

	if r.Context().Err() != nil {
		return
	}

	// Draft projects have never been generated — the first chat message is the
	// initial generation. This lets the studio handle both the first prompt and
	// subsequent edits through the same endpoint, so the UI can stay consistent
	// all the way through instead of running a separate bootstrap flow.
	isInitialGeneration := project.Status == "draft"

	jobType := "app_chat"
	eventName := "app_chat_sent"
	if isInitialGeneration {
		jobType = "generate_app"
		eventName = "app_generation_started"
	}

	v.Controller.TrackUserEvent(user, eventName, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("project_type", project.ProjectType).
		Set("has_images", len(req.Images) > 0))

	chatEvent := constants.EVENT_APP_CHAT_SENT
	if isInitialGeneration {
		chatEvent = constants.EVENT_APP_GENERATION_STARTED
	}
	go v.Controller.EventService.SendEvent(user, chatEvent, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": project.ProjectType,
	})

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: jobType,
		AppID:   project.ID,
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent, jobID string) {
			defer close(events)
			deps := v.Controller.AgentDeps(user)
			emitter := newAgentSSEAdapter(events)
			if err := agent.Run(ctx, deps, project, user, req.Message, turnAttachments, emitter); err != nil {
				slog.Error("agent.Run failed", "error", err, "project_id", project.ID)
			}
			emitter.Close()
		},
	})
}

// GET /v2/apps/{appId}/active-job
// Returns the currently running or awaiting_input generation job for this app,
// or 200 with {"job_id": null} if none. The frontend calls this on mount to
// reconnect the SSE stream after a refresh or browser restart — replacing the
// old localStorage-based job-id recovery, which got stuck on stale jobIds and
// didn't survive Safari's storage eviction on mobile.
func (v *HttpViews) GetAppActiveJob(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	if user == nil {
		utils.JSONErrorCode(w, constants.ErrAuthRequired, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil || project == nil {
		utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusNotFound, nil)
		return
	}

	job, err := v.Controller.GetAppActiveJob(project.ID)
	if err != nil || job == nil {
		utils.JSONResponse(w, map[string]any{"job_id": nil}, http.StatusOK)
		return
	}
	utils.JSONResponse(w, map[string]any{
		"job_id": job.ID,
		"status": job.Status,
		"type":   job.Type,
	}, http.StatusOK)
}

// loadTurnAttachments resolves the user's attached IDs against the project's
// AppAsset table, enforces ownership and the per-turn image cap, and loads
// the project-wide pool used to build the LLM's manifest.
//
// Returns a HTTP-message-grade error suitable for utils.ValidationError.
func loadTurnAttachments(
	ctx context.Context,
	db *database.Database,
	projectID uuid.UUID,
	attachmentIDs []uuid.UUID,
) (agent.TurnAttachments, error) {
	var current []models.AppAsset
	if len(attachmentIDs) > 0 {
		ids := make([]string, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			ids = append(ids, id.String())
		}
		fetched, err := db.GetAppAssetsByIDs(ids)
		if err != nil {
			return agent.TurnAttachments{}, fmt.Errorf("failed to load attachments: %w", err)
		}
		// Reject if any ID is missing or belongs to another project.
		// 404-style behaviour to avoid leaking existence — collapse to a
		// single message that tells the client "one or more attachments are
		// invalid" without naming the specific ID.
		byID := make(map[string]models.AppAsset, len(fetched))
		for _, a := range fetched {
			if a.AppProjectID != projectID {
				return agent.TurnAttachments{}, fmt.Errorf("one or more attachments are invalid")
			}
			byID[a.ID.String()] = a
		}
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				return agent.TurnAttachments{}, fmt.Errorf("one or more attachments are invalid")
			}
		}
		// Preserve user-specified order so the order in the manifest matches
		// the user's mental model.
		current = make([]models.AppAsset, 0, len(ids))
		for _, id := range ids {
			current = append(current, byID[id])
		}
	}

	// Per-turn image-count cap (applies after classification).
	imgCount := 0
	for _, a := range current {
		if constants.ClassifyAttachment(a.ContentType) == constants.AttachmentKindImage {
			imgCount++
		}
	}
	if imgCount > constants.MaxImageAttachmentsPerTurn {
		return agent.TurnAttachments{}, fmt.Errorf("too many image attachments (max %d per message)", constants.MaxImageAttachmentsPerTurn)
	}

	pool, err := db.GetRecentAppAssets(projectID, constants.ManifestMaxAssets)
	if err != nil {
		return agent.TurnAttachments{}, fmt.Errorf("failed to load asset pool: %w", err)
	}

	return agent.TurnAttachments{Current: current, Pool: pool}, nil
}
