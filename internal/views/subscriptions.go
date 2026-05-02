package views

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetLimits(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	limits, err := v.Controller.GetUserLimits(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	limitsResponse := responses.LimitsResponse{
		ID:                      limits.ID,
		MaximumAccessIdCount:    limits.MaximumCredentialCount,
		MontlyBuildCount:        limits.MontlyBuildCount,
		MontlyNotificationCount: limits.MontlyNotificationCount,
		CanBuildToAndroid:       limits.CanBuildToAndroid,
		CanBuildToIOS:           limits.CanBuildToIOS,
		CanUseAnalytics:         limits.CanUseAnalytics,
		CanRemoveBrandLogo:      limits.CanRemoveBrandLogo,
		MaxDeployedWebApps:      limits.MaxDeployedWebApps,
		MaxDeployedMobileApps:   limits.MaxDeployedMobileApps,
		CanDeployMobile:         limits.CanDeployMobile,
		CreditRolloverMonths:    limits.CreditRolloverMonths,
		CanAccessCode:           limits.CanAccessCode,
		CanBuyDomain:            limits.CanBuyDomain,
		CanUseEmailService:      limits.CanUseEmailService,
		CanBuildMobile:          limits.CanBuildMobile,
		MonthlyAICredits:        limits.MonthlyAICredits,
		MaxWebApps:              limits.MaxWebApps,
		MaxMobileApps:           limits.MaxMobileApps,
		CanUseEcommerce:         limits.CanUseEcommerce,
		CanDeployWeb:            limits.CanDeployWeb,
		CanExportToGitHub:       limits.CanExportToGitHub,
		DeployMemoryMB:          limits.DeployMemoryMB,
		DeployCPUs:              limits.DeployCPUs,
		DeployDBMaxCU:           limits.DeployDBMaxCU,
		DeployDBStorageMB:       limits.DeployDBStorageMB,
	}

	utils.JSONResponse(w, limitsResponse, http.StatusOK)
}

func (v *HttpViews) GetSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	response, err := v.Controller.GetSubscription(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

func (v *HttpViews) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	utils.JSONError(w, constants.ErrForbidden, "subscription changes must go through Stripe checkout", http.StatusForbidden, nil)
}

func (v *HttpViews) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var req requests.UserSubscriptionCancelRequest
	if body, _ := io.ReadAll(r.Body); len(body) > 0 {
		if jerr := json.Unmarshal(body, &req); jerr != nil {
			utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
			return
		}
	}

	if err := v.Controller.CancelSubscription(user, req.OrganizationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorCode(w, constants.ErrNotFound, http.StatusNotFound, nil)
			return
		}
		msg := err.Error()
		if msg == "organization not found or access denied" || msg == "insufficient permissions to create project in this organization" {
			utils.JSONErrorCode(w, constants.ErrForbidden, http.StatusForbidden, nil)
			return
		}
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.EventService.SendEvent(user, constants.EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

	utils.JSONResponse(w, map[string]string{"message": "Subscription cancelled successfully"}, http.StatusOK)
}
