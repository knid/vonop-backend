package views

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
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

	if requireActiveSubscription(w, v.Controller, user, project.OrganizationID) {
		return
	}
	if v.checkMinimumCredits(w, user, project.OrganizationID) {
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

	// Pre-flight attachment validation. The worker reloads attachments by ID at
	// execution time, but we still validate here so invalid IDs surface as a
	// 400 with field details rather than a worker-emitted error frame after
	// enqueue.
	if _, attachErr := loadTurnAttachments(r.Context(), v.Controller.DB, project.ID, req.AttachmentIDs); attachErr != nil {
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

	// Phase B Studio gating: chat / generate actions. Allow if user holds either
	// ai.chat or ai.generate (covers mixed custom perms and role defaults).
	// Non-org (personal legacy) projects bypass.
	if project.OrganizationID != uuid.Nil {
		hasAI := v.Controller.UserHasPermission(user, project.OrganizationID.String(), constants.PermAIChat) ||
			v.Controller.UserHasPermission(user, project.OrganizationID.String(), constants.PermAIGenerate)
		if !hasAI {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
	}

	jobType := "app_chat"
	chatEvent := constants.EVENT_APP_CHAT_SENT
	if isInitialGeneration {
		jobType = "generate_app"
		chatEvent = constants.EVENT_APP_GENERATION_STARTED
	}

	v.Controller.TrackUserEvent(user, chatEvent,
		v.Controller.AppEventProps(project.ID.String()).
			Set("project_type", project.ProjectType).
			Set("has_images", len(req.Images) > 0).
			Set("prompt_preview", utils.TruncatePromptPreview(req.Message, 50)))

	go v.Controller.EventService.SendEvent(user, chatEvent, map[string]any{
		"app_id":       project.ID.String(),
		"project_type": project.ProjectType,
	})

	v.enqueueAgentRunAndStream(w, r, user, AgentRunRequest{
		JobType:       jobType,
		AppID:         project.ID,
		UserID:        user.ID,
		Message:       req.Message,
		AttachmentIDs: req.AttachmentIDs,
	})
}

// GET /v2/apps/{appId}/active-job
// Returns the currently running/awaiting_input generation job for this app, or
// the latest Deep Agents-backed job when there is no active run. The frontend
// uses the job id to mint runtime access and hydrate the native LangGraph
// thread after refresh, including after the launch job has completed.
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
		job, err = v.Controller.GetAppLatestDeepAgentsJob(project.ID)
		if err != nil || job == nil {
			utils.JSONResponse(w, map[string]any{"job_id": nil}, http.StatusOK)
			return
		}
	}
	utils.JSONResponse(w, map[string]any{
		"job_id": job.ID,
		"status": job.Status,
		"type":   job.Type,
	}, http.StatusOK)
}

func loadTurnAttachments(
	ctx context.Context,
	db *database.Database,
	projectID uuid.UUID,
	attachmentIDs []uuid.UUID,
) (struct{}, error) {
	_ = ctx
	var current []models.AppAsset
	if len(attachmentIDs) > 0 {
		ids := make([]string, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			ids = append(ids, id.String())
		}
		fetched, err := db.GetAppAssetsByIDs(ids)
		if err != nil {
			return struct{}{}, fmt.Errorf("failed to load attachments: %w", err)
		}
		byID := make(map[string]models.AppAsset, len(fetched))
		for _, a := range fetched {
			if a.AppProjectID != projectID {
				return struct{}{}, fmt.Errorf("one or more attachments are invalid")
			}
			byID[a.ID.String()] = a
		}
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				return struct{}{}, fmt.Errorf("one or more attachments are invalid")
			}
		}
		current = make([]models.AppAsset, 0, len(ids))
		for _, id := range ids {
			current = append(current, byID[id])
		}
	}

	imgCount := 0
	for _, a := range current {
		if constants.ClassifyAttachment(a.ContentType) == constants.AttachmentKindImage {
			imgCount++
		}
	}
	if imgCount > constants.MaxImageAttachmentsPerTurn {
		return struct{}{}, fmt.Errorf("too many image attachments (max %d per message)", constants.MaxImageAttachmentsPerTurn)
	}
	return struct{}{}, nil
}
