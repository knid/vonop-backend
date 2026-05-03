package views

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/utils"
)

type requestKeystoreDownloadReq struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
}

// RequestKeystoreDownload starts the email-OTP gate for keystore download.
// POST /v1/projects/{projectId}/keystore/download/request
func (v *HttpViews) RequestKeystoreDownload(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var req requestKeystoreDownloadReq
	// Prefer path params; fall back to JSON body (for clients that POST a body).
	req.OrgID = chi.URLParam(r, "org_id")
	req.ProjectID = chi.URLParam(r, "project_id")
	if req.OrgID == "" || req.ProjectID == "" {
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			utils.JSONError(w, constants.ErrValidationRequired, "invalid request body", http.StatusBadRequest, nil)
			return
		}
	}

	if req.OrgID == "" || req.ProjectID == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "missing org_id or project_id", http.StatusBadRequest, nil)
		return
	}

	if err := v.Controller.RequestKeystoreDownload(r.Context(), user, req.OrgID, req.ProjectID); err != nil {
		utils.JSONError(w, constants.ErrForbidden, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, map[string]string{"status": "otp_sent"}, http.StatusOK)
}

type redeemKeystoreDownloadReq struct {
	OrgID     string `json:"org_id"`
	ProjectID string `json:"project_id"`
	OTP       string `json:"otp"`
}

type redeemKeystoreDownloadResp struct {
	URL string `json:"url"`
}

// RedeemKeystoreDownload validates the OTP and returns a presigned S3 download URL.
// POST /v1/projects/{projectId}/keystore/download/redeem
func (v *HttpViews) RedeemKeystoreDownload(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var req redeemKeystoreDownloadReq
	if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
		utils.JSONError(w, constants.ErrValidationRequired, "invalid request body", http.StatusBadRequest, nil)
		return
	}
	// Allow path params to fill in any missing fields from the JSON body.
	if req.OrgID == "" {
		req.OrgID = chi.URLParam(r, "org_id")
	}
	if req.ProjectID == "" {
		req.ProjectID = chi.URLParam(r, "project_id")
	}
	if req.OrgID == "" || req.ProjectID == "" || req.OTP == "" {
		utils.JSONError(w, constants.ErrValidationRequired, "missing org_id, project_id, or otp", http.StatusBadRequest, nil)
		return
	}

	url, err := v.Controller.RedeemKeystoreDownload(r.Context(), user, req.OrgID, req.ProjectID, req.OTP)
	if err != nil {
		if errors.Is(err, controllers.ErrKeystoreOTPInvalid) || errors.Is(err, controllers.ErrKeystoreOTPRequired) {
			utils.JSONError(w, constants.ErrAuthInvalidToken, err.Error(), http.StatusUnauthorized, nil)
			return
		}
		utils.JSONError(w, constants.ErrForbidden, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, redeemKeystoreDownloadResp{URL: url}, http.StatusOK)
}
