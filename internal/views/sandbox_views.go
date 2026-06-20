package views

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/sandbox"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) EnsureAppPreview(w http.ResponseWriter, r *http.Request) {
	project, ok := v.previewProjectFromRequest(w, r)
	if !ok {
		return
	}
	status, err := v.Controller.PreviewStatus(r.Context(), project)
	if err == nil && status.State == sandbox.PreviewMissing {
		status, err = v.Controller.EnsurePreview(r.Context(), project)
	}
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

func (v *HttpViews) GetAppPreviewStatus(w http.ResponseWriter, r *http.Request) {
	project, ok := v.previewProjectFromRequest(w, r)
	if !ok {
		return
	}
	status, err := v.Controller.PreviewStatus(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

func (v *HttpViews) RestartAppPreview(w http.ResponseWriter, r *http.Request) {
	project, ok := v.previewProjectFromRequest(w, r)
	if !ok {
		return
	}
	status, err := v.Controller.RestartPreview(r.Context(), project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, status, http.StatusOK)
}

func (v *HttpViews) StreamAppPreviewEvents(w http.ResponseWriter, r *http.Request) {
	project, ok := v.previewProjectFromRequest(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.JSONErrorCode(w, constants.ErrStreamingUnsupported, http.StatusInternalServerError, nil)
		return
	}
	utils.SetSSEHeaders(w)

	status, err := v.Controller.PreviewStatus(r.Context(), project)
	if err == nil && status.State == sandbox.PreviewMissing {
		status, err = v.Controller.EnsurePreview(r.Context(), project)
	}
	if err != nil {
		_ = utils.WriteSSEEvent(w, flusher, 0, "error", sandbox.PreviewEvent{
			Type:      "error",
			State:     sandbox.PreviewError,
			Chunk:     err.Error(),
			UpdatedAt: time.Now().UTC(),
		})
		return
	}
	logsTail := status.LogsTail
	status.LogsTail = ""
	_ = utils.WriteSSEEvent(w, flusher, 0, "status", sandbox.PreviewEvent{
		Type:      "status",
		State:     status.State,
		Status:    status,
		UpdatedAt: time.Now().UTC(),
	})
	if logsTail != "" {
		_ = utils.WriteSSEEvent(w, flusher, 0, "stdout", sandbox.PreviewEvent{
			Type:      "stdout",
			State:     status.State,
			Chunk:     logsTail,
			UpdatedAt: time.Now().UTC(),
		})
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			status, err := v.Controller.PreviewStatus(r.Context(), project)
			if err != nil {
				_ = utils.WriteSSEEvent(w, flusher, 0, "error", sandbox.PreviewEvent{
					Type:      "error",
					State:     sandbox.PreviewError,
					Chunk:     err.Error(),
					UpdatedAt: time.Now().UTC(),
				})
				return
			}
			nextLogsTail := status.LogsTail
			status.LogsTail = ""
			_ = utils.WriteSSEEvent(w, flusher, 0, "status", sandbox.PreviewEvent{
				Type:      "status",
				State:     status.State,
				Status:    status,
				UpdatedAt: time.Now().UTC(),
			})
			if nextLogsTail != "" && nextLogsTail != logsTail {
				chunk := nextLogsTail
				if strings.HasPrefix(nextLogsTail, logsTail) {
					chunk = strings.TrimPrefix(nextLogsTail, logsTail)
				}
				logsTail = nextLogsTail
				if chunk != "" {
					_ = utils.WriteSSEEvent(w, flusher, 0, "stdout", sandbox.PreviewEvent{
						Type:      "stdout",
						State:     status.State,
						Chunk:     chunk,
						UpdatedAt: time.Now().UTC(),
					})
				}
			}
		}
	}
}

func (v *HttpViews) previewProjectFromRequest(w http.ResponseWriter, r *http.Request) (*models.AppProject, bool) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return nil, false
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return nil, false
	}
	return project, true
}

func (v *HttpViews) SyncSandbox(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	if err := v.Controller.SyncSandbox(r.Context(), project); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, map[string]bool{"ok": true}, http.StatusOK)
}

func (v *HttpViews) OpenSandboxRoute(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	project, err := v.Controller.GetAppProject(chi.URLParam(r, "appId"), user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	var body struct {
		Route string `json:"route"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.ValidationError(w, err)
		return
	}
	target, err := v.Controller.OpenSandboxRoute(r.Context(), project, body.Route, "user")
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, target, http.StatusOK)
}
