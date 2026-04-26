package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CheckStatus(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if uuid.Validate(projectId) != nil {
		utils.JSONErrorCode(w, constants.ErrValidationInvalidFormat, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	project, err := v.Controller.GetProjectModelById(projectId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrProjectNotFound, http.StatusBadRequest, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	user, err := v.Controller.GetUserModelById(project.UserID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	var status string
	switch user.SubscriptionStatus {
	case "active", "free", "trial":
		status = "active"
	default:
		status = "unactive"
	}

	utils.JSONResponse(w, map[string]string{
		"status": status,
	}, http.StatusOK)
}
