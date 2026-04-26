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

func (v *HttpViews) GetNavBarItems(w http.ResponseWriter, r *http.Request) {
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

	if requireActiveSubscription(w, user) {
		return
	}

	navBarItems, err := v.Controller.DB.GetNavBarItemsByProject(projectId)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"data": navBarItems.Items,
	}, http.StatusOK)
}

func (v *HttpViews) GetNavBarItemsBackground(w http.ResponseWriter, r *http.Request) {
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

	if requireActiveSubscription(w, user) {
		return
	}

	navBarItems, err := v.Controller.DB.GetNavBarItemsByProject(projectId)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"data": navBarItems.BackgroundColor,
	}, http.StatusOK)
}

func (v *HttpViews) GetJSInjectionData(w http.ResponseWriter, r *http.Request) {
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

	if requireActiveSubscription(w, user) {
		return
	}

	jsInjectionData, err := v.Controller.DB.GetJSInjectionDataByProject(projectId)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"data": jsInjectionData.Base64Data,
	}, http.StatusOK)
}
