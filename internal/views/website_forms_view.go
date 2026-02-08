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

func (v *HttpViews) GetWebsiteForms(w http.ResponseWriter, r *http.Request) {
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

	forms, err := v.Controller.GetForms(user, websiteID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, forms, http.StatusOK)
}

func (v *HttpViews) CreateWebsiteForm(w http.ResponseWriter, r *http.Request) {
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

	var req requests.CreateFormRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.CreateForm(user, websiteID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

func (v *HttpViews) DeleteWebsiteForm(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	formID := chi.URLParam(r, "fmid")
	if websiteID == "" || formID == "" {
		utils.JSONErrorResponse(w, "website id and form id are required", http.StatusBadRequest)
		return
	}

	err = v.Controller.DeleteForm(user, websiteID, formID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, nil, http.StatusNoContent)
}

func (v *HttpViews) GetFormSubmissions(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	formID := chi.URLParam(r, "fmid")
	if websiteID == "" || formID == "" {
		utils.JSONErrorResponse(w, "website id and form id are required", http.StatusBadRequest)
		return
	}

	submissions, err := v.Controller.GetFormSubmissions(user, websiteID, formID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, submissions, http.StatusOK)
}

// Public form submission endpoint (no auth)
func (v *HttpViews) SubmitWebsiteForm(w http.ResponseWriter, r *http.Request) {
	formID := chi.URLParam(r, "fmid")
	if formID == "" {
		utils.JSONErrorResponse(w, "form id is required", http.StatusBadRequest)
		return
	}

	var req requests.SubmitFormRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	ipAddress := r.RemoteAddr
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ipAddress = xff
	}
	userAgent := r.UserAgent()

	result, err := v.Controller.SubmitForm(formID, req, ipAddress, userAgent)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

// Screenshot-to-code view
func (v *HttpViews) GenerateWebsiteFromScreenshot(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	if v.deductAICredits(w, user, models.AICreditCostScreenshotCode, "screenshot_to_code", "") {
		return
	}

	var req requests.ScreenshotToCodeRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.GenerateWebsiteFromScreenshot(user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusCreated)
}

// Screenshot-to-code streaming view
func (v *HttpViews) GenerateWebsiteFromScreenshotSSE(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	if v.deductAICredits(w, user, models.AICreditCostScreenshotCode, "screenshot_to_code_stream", "") {
		return
	}

	var req requests.ScreenshotToCodeRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "screenshot_to_code",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.GenerateWebsiteFromScreenshotStream(ctx, user, req, events)
		},
	})
}

// Edit element view (SSE)
func (v *HttpViews) AIEditElement(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if requireActiveSubscription(w, user) {
		return
	}

	if v.deductAICredits(w, user, models.AICreditCostElementEdit, "edit_element", "") {
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	var req requests.EditElementRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.handleStreamOrAsync(w, r, user, StreamConfig{
		JobType: "edit_element",
		StreamFunc: func(ctx context.Context, events chan<- responses.SSEEvent) {
			v.Controller.AIEditElementStream(ctx, user, websiteID, req, events)
		},
	})
}

// Theme update view
func (v *HttpViews) UpdateWebsiteTheme(w http.ResponseWriter, r *http.Request) {
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

	var req requests.UpdateThemeRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.UpdateWebsiteTheme(user, websiteID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
