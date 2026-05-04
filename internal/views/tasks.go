package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CancelBuildTask(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "task_id"})
		return
	}

	if err := v.Controller.CancelBuildTask(user, taskID); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

func (v *HttpViews) GetAllTasks(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	// Optional ?project_id=<v1-project-uuid> scopes the result to a single
	// project the user owns. Without it the response is the full task
	// history across every project the user has ever built (kept for
	// backwards compatibility with any caller that hasn't been updated).
	projectID := r.URL.Query().Get("project_id")

	tasks, err := v.Controller.GetAllAppBuildTasks(user, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, tasks, 200)
}
