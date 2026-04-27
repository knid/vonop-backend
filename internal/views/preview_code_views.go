package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

// POST /v1/preview-codes
func (v *HttpViews) GeneratePreviewCode(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var req requests.GeneratePreviewCodeRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	pc, err := v.Controller.GeneratePreviewCode(user, req.AppProjectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, responses.PreviewCodeResponse{
		Code:      pc.Code,
		ExpiresAt: pc.ExpiresAt,
	}, http.StatusCreated)
}

// POST /v1/preview-codes/{code}/redeem
func (v *HttpViews) RedeemPreviewCode(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	code := chi.URLParam(r, "code")
	if code == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "code"})
		return
	}

	pc, project, err := v.Controller.RedeemPreviewCode(user, code)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// Get app icon presigned URL if available
	var iconURL string
	if project.ScreenshotS3Key != "" {
		if presigned, err := v.Controller.AssetsStorage.GetObject(r.Context(), project.ScreenshotS3Key, 3600); err == nil {
			iconURL = presigned.URL
		}
	}

	utils.JSONResponse(w, responses.PreviewCodeRedeemResponse{
		PreviewURL:   pc.PreviewURL,
		AppProjectID: pc.AppProjectID,
		AppName:      project.Name,
		AppIcon:      iconURL,
		SandboxID:    pc.SandboxID,
	}, http.StatusOK)
}
