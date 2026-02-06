package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// File-based generation endpoints

func (v *HttpViews) GenerateWebsiteFiles(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req requests.WebsiteGenerateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.GenerateWebsiteFiles(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) GenerateWebsiteFilesSSE(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req requests.WebsiteGenerateRequest
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
	go v.Controller.GenerateWebsiteFilesStream(r.Context(), user, req, events)

	for event := range events {
		if err := utils.WriteSSEEvent(w, flusher, event.Event, event.Data); err != nil {
			return
		}
	}
}

// File CRUD endpoints

func (v *HttpViews) GetWebsiteFiles(w http.ResponseWriter, r *http.Request) {
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

	files, err := v.Controller.GetWebsiteFiles(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, files, http.StatusOK)
}

func (v *HttpViews) GetWebsiteFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	fileID := chi.URLParam(r, "fid")
	if websiteID == "" || fileID == "" {
		utils.JSONErrorResponse(w, "website id and file id are required", http.StatusBadRequest)
		return
	}

	file, err := v.Controller.GetWebsiteFile(user, websiteID, fileID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, file, http.StatusOK)
}

func (v *HttpViews) CreateWebsiteFile(w http.ResponseWriter, r *http.Request) {
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

	var req requests.FileCreateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.CreateWebsiteFile(user, websiteID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) UpdateWebsiteFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	fileID := chi.URLParam(r, "fid")
	if websiteID == "" || fileID == "" {
		utils.JSONErrorResponse(w, "website id and file id are required", http.StatusBadRequest)
		return
	}

	var req requests.FileUpdateRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.UpdateWebsiteFile(user, websiteID, fileID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}

func (v *HttpViews) DeleteWebsiteFile(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	fileID := chi.URLParam(r, "fid")
	if websiteID == "" || fileID == "" {
		utils.JSONErrorResponse(w, "website id and file id are required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DeleteWebsiteFile(user, websiteID, fileID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

// Publish file-based website

func (v *HttpViews) PublishWebsiteFiles(w http.ResponseWriter, r *http.Request) {
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

	result, err := v.Controller.PublishWebsiteFiles(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
