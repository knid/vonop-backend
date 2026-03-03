package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetNavBarItems(w http.ResponseWriter, r *http.Request) {
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

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "this projects is not active", http.StatusBadRequest)
		return
	}

	navBarItems, err := v.Controller.DB.GetNavBarItemsByProject(projectId)
	if err != nil {
		utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"data": navBarItems.Items,
	}, http.StatusOK)
}

func (v *HttpViews) GetNavBarItemsBackground(w http.ResponseWriter, r *http.Request) {
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

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "this projects is not active", http.StatusBadRequest)
		return
	}

	navBarItems, err := v.Controller.DB.GetNavBarItemsByProject(projectId)
	if err != nil {
		utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"data": navBarItems.BackgroundColor,
	}, http.StatusOK)
}

func (v *HttpViews) GetJSInjectionData(w http.ResponseWriter, r *http.Request) {
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

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "this projects is not active", http.StatusBadRequest)
		return
	}

	jsInjectionData, err := v.Controller.DB.GetJSInjectionDataByProject(projectId)
	if err != nil {
		utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
		return
	}

	utils.JSONResponse(w, map[string]string{
		"data": jsInjectionData.Base64Data,
	}, http.StatusOK)
}
