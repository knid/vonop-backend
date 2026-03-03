package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetProjectBuilds(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get builds for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONCodedErrorResponse(w, "SUBSCRIPTION_REQUIRED", "active subscription required", http.StatusForbidden, nil)
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	builds, err := v.Controller.GetBuilds(project)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, builds, http.StatusOK)
}

func (v *HttpViews) CreateProjectBuild(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to create a new build for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{})
		return
	}

	var builds []responses.BuildInformationResponse
	projects, err := v.Controller.GetProjects(user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	for _, p := range projects {
		project, err := v.Controller.GetProject(user, p.ID.String())
		if err != nil {
			continue
		}

		p, err := v.Controller.GetProjectModelById(project.ID.String())
		if err != nil {
			continue
		}

		b, err := v.Controller.GetBuilds(p)
		if err != nil {
			continue
		}

		builds = append(builds, b...)
	}

	subsStart := user.SubscriptionStartAt
	now := time.Now()

	renewStart := time.Date(now.Year(), now.Month(), subsStart.Day(), 0, 0, 0, 0, now.Location())

	if now.Before(renewStart) {
		renewStart = renewStart.AddDate(0, -1, 0)
	}

	renewEnd := renewStart.AddDate(0, 1, 0)

	count := 0
	for _, b := range builds {
		if b.CreatedAt.After(renewStart) && b.CreatedAt.Before(renewEnd) {
			count++
		}
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

	if count >= limits.MontlyBuildCount {
		utils.JSONErrorResponse(w, "you have reached your monthly build limit", http.StatusForbidden)
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "create_build",
			"limit":  limits.MontlyBuildCount,
		})
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorResponse(w, "project_id is required", http.StatusBadRequest)
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.GetBuilds(project)

	var buildCreateReq requests.BuildCreateRequest
	if err := utils.ParseJSONBody(r, &buildCreateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	buildCreateReq.ProjectID = project.ID

	if buildCreateReq.RemoveWemobBranding && !limits.CanRemoveBrandLogo {
		utils.JSONErrorResponse(w, "your current subscription plan does not allow removing Wemob branding", http.StatusForbidden)
		return
	}

	buildCreateRes, err := v.Controller.CreateBuild(user, buildCreateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, buildCreateRes, http.StatusCreated)
}

func (v *HttpViews) GetProjectBuild(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get a specific build for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if !(user.SubscriptionStatus == "active" || user.SubscriptionStatus == "trial") {
		utils.JSONErrorResponse(w, "you do not have access to this resource", http.StatusForbidden)
		return
	}

	buildID := chi.URLParam(r, "buildId")
	if buildID == "" {
		utils.JSONErrorResponse(w, "build_id is required", http.StatusBadRequest)
		return
	}

	build, err := v.Controller.GetBuild(user, buildID)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, build, http.StatusOK)
}
