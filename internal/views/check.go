package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CheckStatus(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if uuid.Validate(projectId) != nil {
		utils.JSONErrorResponse(w, "invalid project id", http.StatusBadRequest)
		return
	}

	project, err := v.Controller.GetProjectModelById(projectId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "project not found", http.StatusBadRequest)
			return
		}
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	user, err := v.Controller.GetUserModelById(project.UserID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	var status string
	if user.SubscriptionStatus == "active" {
		status = "active"
	} else {
		status = "unactive"
	}

	utils.JSONResponse(w, map[string]string{
		"status": status,
	}, http.StatusOK)
}
