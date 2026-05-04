package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// GetSubscriptionsSummary returns the four-number panel used by the Studio
// "Subscriptions" widget for a mobile_app_native project:
//
//	{ active_count, mrr_cents, churn_30d, trials_count }
//
// Any project type other than mobile_app_native gets a 400 — the panel is
// only meaningful for native apps with RC-backed IAP.
//
// Path: GET /v2/apps/{appId}/subscriptions/summary
func (v *HttpViews) GetSubscriptionsSummary(w http.ResponseWriter, r *http.Request) {
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
	if project.ProjectType != models.ProjectTypeMobileAppNative {
		utils.JSONError(w, constants.ErrValidationBody,
			"subscriptions/summary only supports mobile_app_native projects",
			http.StatusBadRequest, nil)
		return
	}

	summary, err := v.Controller.DB.SummarizeMobileSubscriptions(r.Context(), project.ID.String())
	if err != nil {
		utils.JSONError(w, constants.ErrInternal, err.Error(), http.StatusInternalServerError, nil)
		return
	}
	utils.JSONResponse(w, summary, http.StatusOK)
}
