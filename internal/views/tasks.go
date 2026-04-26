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
		utils.ValidationError(w, err)
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

	tasks, err := v.Controller.GetAllAppBuildTasks(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
			return
		}
		utils.ValidationError(w, err)
		return
	}

	utils.JSONResponse(w, tasks, 200)
}
