package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetDeploymentEvents(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}
	idStr := chi.URLParam(r, "deploymentId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationInvalidFormat, http.StatusBadRequest, map[string]any{"field": "deployment_id"})
		return
	}
	resp, err := v.Controller.GetDeploymentEvents(r.Context(), user, id)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
