package views

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetProjects(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.DB.GetUserById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, projects, http.StatusOK)

}

func (v *HttpViews) CreateProject(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.DB.GetUserById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	log.Println("before sub check")
	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		log.Println("on sub check")
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	projects, err := v.Controller.DB.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	subsInfo, err := v.Controller.GetSubscription(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	limits, err := v.Controller.DB.GetStandartUserLimitsById(subsInfo.Limits.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(projects) >= limits.MaximumProjectCount {
		utils.JSONErrorResponse(w, "you have reached your project limit", http.StatusForbidden)
		return
	}

	var projectCreateReq requests.ProjectCreateRequest
	if err := utils.ParseJSONBody(r, &projectCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	projectCreateRes, err := v.Controller.CreateProject(user, projectCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, projectCreateRes, http.StatusCreated)
}

func (v *HttpViews) GetProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.DB.GetUserById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, projectInformationRes, http.StatusOK)
}

func (v *HttpViews) DeleteProject(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to list all projects for a user
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}
	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := v.Controller.GetUserModelById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	deletedProjectRes, err := v.Controller.DeleteProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, deletedProjectRes, http.StatusNoContent)
}
