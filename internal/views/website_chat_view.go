package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) ChatWebsite(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	var req requests.ChatMessageRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.JSONErrorResponse(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	utils.SetSSEHeaders(w)
	flusher.Flush()

	events := make(chan responses.SSEEvent, 10)
	go v.Controller.ChatStream(r.Context(), user, websiteID, req, events)

	for event := range events {
		if err := utils.WriteSSEEvent(w, flusher, event.Event, event.Data); err != nil {
			return
		}
	}
}

func (v *HttpViews) GetWebsiteConversation(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	result, err := v.Controller.GetConversation(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
