package views

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/backend/health
func (v *HttpViews) GetBackendHealth(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	out, err := v.Controller.GetBackendHealth(r.Context(), project)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, out, http.StatusOK)
}

// GET /v2/apps/{appId}/backend/routes
func (v *HttpViews) GetBackendRoutes(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	routes, err := v.Controller.GetBackendRoutes(r.Context(), project)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, responses.BackendRoutesResponse{Routes: routes}, http.StatusOK)
}

// GET /v2/apps/{appId}/backend/logs?since=15m&level=info
func (v *HttpViews) GetBackendLogs(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	sinceStr := strings.TrimSpace(r.URL.Query().Get("since"))
	var since time.Time
	if sinceStr != "" {
		if d, err := time.ParseDuration(sinceStr); err == nil && d > 0 {
			since = time.Now().Add(-d)
		}
	}
	level := strings.TrimSpace(r.URL.Query().Get("level"))

	lines, err := v.Controller.GetBackendLogs(r.Context(), project, since, level)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrInternal, http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, responses.BackendLogsResponse{Lines: lines}, http.StatusOK)
}
