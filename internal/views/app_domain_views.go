package views

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

// writeAddAppDomainError translates user-facing rejections from
// AddAppDomain into structured 4xx responses. Returns true when handled.
func writeAddAppDomainError(w http.ResponseWriter, err error) bool {
	var limitErr *controllers.DomainLimitError
	if errors.As(err, &limitErr) {
		utils.JSONError(w, constants.ErrDomainLimitReached, limitErr.Error(), http.StatusForbidden, map[string]any{
			"limit": limitErr.Limit,
			"used":  limitErr.Used,
		})
		return true
	}
	switch {
	case errors.Is(err, controllers.ErrDomainInvalidFormat):
		utils.JSONError(w, constants.ErrDomainInvalid, err.Error(), http.StatusBadRequest, nil)
	case errors.Is(err, controllers.ErrDomainReserved):
		utils.JSONError(w, constants.ErrDomainInvalid, err.Error(), http.StatusBadRequest, nil)
	case errors.Is(err, controllers.ErrDomainAttachedToOther),
		errors.Is(err, controllers.ErrDomainAlreadyInUse):
		utils.JSONError(w, constants.ErrDomainTaken, err.Error(), http.StatusConflict, nil)
	case errors.Is(err, controllers.ErrAppNotDeployed):
		utils.JSONError(w, constants.ErrAppNotDeployed, err.Error(), http.StatusConflict, nil)
	default:
		return false
	}
	return true
}

// GET /v2/apps/{appId}/domains
func (v *HttpViews) GetAppDomains(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	domains, err := v.Controller.GetAppDomains(project.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, domains, http.StatusOK)
}

// POST /v2/apps/{appId}/domains
func (v *HttpViews) AddAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	// WEM-263: Check CanBuyDomain subscription limit
	limits, limitsErr := v.Controller.GetUserLimits(user)
	if limitsErr != nil || !limits.CanBuyDomain {
		go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_NEED, map[string]any{"action": "buy_domain"})
		utils.JSONError(w, constants.ErrSubscriptionRequired, "custom domains require a paid plan", http.StatusForbidden, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	var req requests.AddAppDomainRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.ValidationError(w, err)
		return
	}

	result, err := v.Controller.AddAppDomain(project, user, req)
	if err != nil {
		if writeAddAppDomainError(w, err) {
			return
		}
		// Validator errors (e.g. ValidateFunc on the request struct) also
		// surface here; route them to the field-level catalog.
		var verr validator.ValidationErrors
		if errors.As(err, &verr) {
			utils.ValidationError(w, err)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_DOMAIN_ADDED, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("domain", req.Domain))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_DOMAIN_ADDED, map[string]any{
		"app_id": project.ID.String(),
		"domain": req.Domain,
	})

	utils.JSONResponse(w, result, http.StatusCreated)
}

// POST /v2/apps/{appId}/domains/{did}/verify
func (v *HttpViews) VerifyAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	domainID := chi.URLParam(r, "did")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	result, err := v.Controller.VerifyAppDomain(domainID, project)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	// WEM-271: Track domain verification events
	v.Controller.TrackUserEvent(user, constants.EVENT_DOMAIN_VERIFIED, posthog.NewProperties().
		Set("app_id", project.ID.String()).
		Set("domain", result.Domain).
		Set("status", result.Status))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_DOMAIN_VERIFIED, map[string]any{
		"app_id": project.ID.String(),
		"domain": result.Domain,
		"status": result.Status,
	})

	utils.JSONResponse(w, result, http.StatusOK)
}

// DELETE /v2/apps/{appId}/domains/{did}
func (v *HttpViews) RemoveAppDomain(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	appID := chi.URLParam(r, "appId")
	domainID := chi.URLParam(r, "did")
	project, err := v.Controller.GetAppProject(appID, user)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
		return
	}

	if err := v.Controller.RemoveAppDomain(domainID, project); err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	v.Controller.TrackUserEvent(user, constants.EVENT_DOMAIN_REMOVED, posthog.NewProperties().
		Set("app_id", project.ID.String()))

	go v.Controller.EventService.SendEvent(user, constants.EVENT_DOMAIN_REMOVED, map[string]any{
		"app_id": project.ID.String(),
	})

	utils.JSONResponse(w, responses.SuccessResponse{Success: true}, http.StatusOK)
}
