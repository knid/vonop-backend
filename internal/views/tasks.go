package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) CancelBuildTask(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	taskID := chi.URLParam(r, "taskId")
	if taskID == "" {
		utils.JSONErrorResponse(w, "task_id is required", http.StatusBadRequest)
		return
	}

	if err := v.Controller.CancelBuildTask(user, taskID); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{"success": true}, http.StatusOK)
}

func (v *HttpViews) GetAllTasks(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	tasks, err := v.Controller.GetAllAppBuildTasks(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
			return
		}
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, tasks, 200)
}
