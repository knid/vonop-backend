package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetComponents(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")

	components, err := v.Controller.GetComponents(category)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, components, http.StatusOK)
}

func (v *HttpViews) GetComponent(w http.ResponseWriter, r *http.Request) {
	componentID := chi.URLParam(r, "cid")
	if componentID == "" {
		utils.JSONErrorResponse(w, "component id is required", http.StatusBadRequest)
		return
	}

	component, err := v.Controller.GetComponent(componentID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, component, http.StatusOK)
}

func (v *HttpViews) InsertComponent(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	websiteID := chi.URLParam(r, "id")
	if websiteID == "" {
		utils.JSONErrorResponse(w, "website id is required", http.StatusBadRequest)
		return
	}

	var req requests.InsertComponentRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.InsertComponent(user, websiteID, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, result, http.StatusOK)
}
