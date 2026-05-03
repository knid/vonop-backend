package views

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// ComposePush sends a push notification to a mobile_app_native project's audience.
// POST /v2/apps/{appId}/push/compose
func (v *HttpViews) ComposePush(w http.ResponseWriter, r *http.Request) {
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
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"compose-push only supports mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	var req requests.ComposePushRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := req.Validate(validate); err != nil {
		utils.ValidationError(w, err)
		return
	}

	resp, err := v.Controller.SendComposePush(r.Context(), project, user, req)
	if err != nil {
		utils.JSONError(w, constants.ErrInternal, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}

// GetComposePushHistory returns paginated send history.
// GET /v2/apps/{appId}/push/history?limit=20&offset=0
func (v *HttpViews) GetComposePushHistory(w http.ResponseWriter, r *http.Request) {
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
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"compose-push history only supports mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	resp, err := v.Controller.ListComposePushHistory(r.Context(), project, user, limit, offset)
	if err != nil {
		utils.JSONError(w, constants.ErrInternal, err.Error(), http.StatusBadRequest, nil)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
