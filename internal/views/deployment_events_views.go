package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetDeploymentEvents(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	idStr := chi.URLParam(r, "deploymentId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		utils.JSONErrorResponse(w, "invalid deployment_id", http.StatusBadRequest)
		return
	}
	resp, err := v.Controller.GetDeploymentEvents(r.Context(), user, id)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}
	utils.JSONResponse(w, resp, http.StatusOK)
}
