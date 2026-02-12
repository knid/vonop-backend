package views

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// DELETE /v2/apps/{appId}/conversation
func (v *HttpViews) ClearAppConversation(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	_, err = v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.ClearAppConversation(appID); err != nil {
		utils.JSONErrorResponse(w, "failed to clear conversation", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

// POST /v2/apps/{appId}/chat
func (v *HttpViews) ChatApp(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	if requireActiveSubscription(w, user) {
		return
	}
	if v.deductAICredits(w, user, models.AICreditCostChat, "app_chat", "") {
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.AppChatRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "app_chat",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.StreamChatAppFileOps(ctx, project, user, req.Message, req.Images, events)
		},
	})
}
