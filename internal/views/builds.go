package views

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) GetProjectBuilds(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get builds for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	builds, err := v.Controller.GetBuilds(project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, builds, http.StatusOK)
}

func (v *HttpViews) CreateProjectBuild(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to create a new build for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{})
		return
	}

	limits, planName, err := v.Controller.GetUserLimitsWithPlan(user)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	if !limits.CanBuildMobile {
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "mobile builds require Professional plan", http.StatusForbidden, nil)
		return
	}

	// Subscription billing period anchors on the personal org (Plan 5).
	org, err := v.Controller.DB.GetPersonalOrgByUserID(user.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	subsStart := org.SubscriptionStartAt
	now := time.Now()
	renewStart := time.Date(now.Year(), now.Month(), subsStart.Day(), 0, 0, 0, 0, now.Location())
	if now.Before(renewStart) {
		renewStart = renewStart.AddDate(0, -1, 0)
	}

	count, err := v.Controller.DB.CountUserBuildsSince(user.ID.String(), renewStart)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	if count >= limits.MontlyBuildCount {
		_, renewEnd := utils.BillingPeriod(org.SubscriptionStartAt, time.Now())
		utils.JSONError(w, constants.ErrBuildLimitReached,
			"you have reached your monthly build limit",
			http.StatusForbidden, map[string]any{
				"used":         count,
				"limit":        limits.MontlyBuildCount,
				"renewal_date": renewEnd.Format(time.RFC3339),
				"plan_name":    planName,
			})
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_QUOTA_REACHED, map[string]any{
			"action": "create_build",
			"limit":  limits.MontlyBuildCount,
		})
		go v.Controller.TrackUserEvent(user, "quota_reached", posthog.NewProperties().Set("action", "create_build").Set("limit", limits.MontlyBuildCount))
		return
	}

	projectID := chi.URLParam(r, "projectId")
	if projectID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "project_id"})
		return
	}

	projectInformationRes, err := v.Controller.GetProject(user, projectID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}
	project, err := v.Controller.GetProjectModelById(projectInformationRes.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	var buildCreateReq requests.BuildCreateRequest
	if err := utils.ParseJSONBody(r, &buildCreateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	buildCreateReq.ProjectID = project.ID

	if buildCreateReq.RemoveWemobBranding && !limits.CanRemoveBrandLogo {
		utils.JSONError(w, constants.ErrFeatureNotAvailable, "Your current subscription plan does not allow removing Wemob branding.", http.StatusForbidden, nil)
		return
	}

	buildCreateRes, err := v.Controller.CreateBuild(user, buildCreateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, buildCreateRes, http.StatusCreated)
}

func (v *HttpViews) GetProjectBuild(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to get a specific build for a specific project

	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if requireActiveSubscription(w, v.Controller, user) {
		return
	}

	buildID := chi.URLParam(r, "buildId")
	if buildID == "" {
		utils.JSONErrorCode(w, constants.ErrValidationRequired, http.StatusBadRequest, map[string]any{"field": "build_id"})
		return
	}

	build, err := v.Controller.GetBuild(user, buildID)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, build, http.StatusOK)
}
