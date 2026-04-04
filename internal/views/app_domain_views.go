package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// GET /v2/apps/{appId}/domains
func (v *HttpViews) GetAppDomains(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	domains, err := v.Controller.GetAppDomains(project.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, domains, http.StatusOK)
}

// POST /v2/apps/{appId}/domains
func (v *HttpViews) AddAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// WEM-263: Check CanBuyDomain subscription limit
	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr != nil || !limits.CanBuyDomain {
		go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "buy_domain"})
		utils.JSONCodedErrorResponse(w, "SUBSCRIPTION_REQUIRED", "custom domains require a paid plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	var req requests.AddAppDomainRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := v.Controller.AddAppDomain(project, user, req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "domain_added", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("domain", req.Domain))

	utils.JSONResponse(w, result, http.StatusCreated)
}

// POST /v2/apps/{appId}/domains/{did}/verify
func (v *HttpViews) VerifyAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	domainID := chi.URLParam(r, "did")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	result, err := v.Controller.VerifyAppDomain(domainID, project)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	// WEM-271: Track domain verification events
	v.Controller.TrackUserEvent(user, "domain_verified", posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("domain", result.Domain).
		Set("status", result.Status))

	utils.JSONResponse(w, result, http.StatusOK)
}

// DELETE /v2/apps/{appId}/domains/{did}
func (v *HttpViews) RemoveAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	appID := chi.URLParam(r, "appId")
	domainID := chi.URLParam(r, "did")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
		return
	}

	if err := v.Controller.RemoveAppDomain(domainID, project); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	v.Controller.TrackUserEvent(user, "domain_removed", posthog.NewProperties().
		Set("app_id", project.ID.String()))

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
