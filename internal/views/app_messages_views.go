package views

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/agent"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// GET /v2/apps/{appId}/messages?limit=100
//
// Returns the most recent N chat messages for a project, oldest-first. Used
// by the dashboard to rehydrate the chat panel on page load.
func (v *HttpViews) GetAppMessages(w http.ResponseWriter, r *http.Request) {
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

	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}

	msgs, err := v.Controller.DB.GetRecentChatMessages(project.ID, limit)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	if msgs == nil {
		msgs = []models.AppChatMessage{}
	}
	msgs = agent.SanitizeChatMessagesForDisplay(msgs)
	utils.JSONResponse(w, map[string]any{"messages": msgs}, http.StatusOK)
}

// DELETE /v2/apps/{appId}/messages
//
// Deletes all chat messages for a project. Called from the "Clear conversation"
// button in the dashboard.
func (v *HttpViews) ClearAppMessages(w http.ResponseWriter, r *http.Request) {
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

	if err := v.Controller.DB.ClearChatMessages(project.ID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}
