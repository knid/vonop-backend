package views

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetLimits(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	limits, err := v.Controller.DB.GetStandartUserLimitsByUser(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
			return
		}
		utils.JSONErrorResponse(w, "failed to retrieve limits", http.StatusInternalServerError)
		return
	}

	limitsResponse := responses.LimitsResponse{
		ID:                      limits.ID,
		MaximumProjectCount:     limits.MaximumProjectCount,
		MaximumAccessIdCount:    limits.MaximumCredentialCount,
		MontlyBuildCount:        limits.MontlyBuildCount,
		MontlyNotificationCount: limits.MontlyNotificationCount,
		CanBuildToAndroid:       limits.CanBuildToAndroid,
		CanBuildToIOS:           limits.CanBuildToIOS,
		CanUseAnalytics:         limits.CanUseAnalytics,
		CanRemoveBrandLogo:      limits.CanRemoveBrandLogo,
		CanSeeActiveUsers:       limits.CanSeeActiveUsers,
		MaxDeployedWebApps:      limits.MaxDeployedWebApps,
		CreditRolloverMonths:    limits.CreditRolloverMonths,
		CanAccessCode:           limits.CanAccessCode,
		CanBuyDomain:            limits.CanBuyDomain,
		CanUseEmailService:      limits.CanUseEmailService,
		CanBuildMobile:          limits.CanBuildMobile,
		MonthlyAICredits:        limits.MonthlyAICredits,
		MonthlyAppCount:         limits.MonthlyAppCount,
		MonthlyDeployCount:      limits.MonthlyDeployCount,
		CanUseEcommerce:         limits.CanUseEcommerce,
	}

	utils.JSONResponse(w, limitsResponse, http.StatusOK)
}

func (v *HttpViews) GetSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	response, err := v.Controller.GetSubscription(user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "not found", http.StatusNotFound)
			return
		}
		utils.JSONErrorResponse(w, "failed to retrieve subscription", http.StatusInternalServerError)
		return
	}

	utils.JSONResponse(w, response, http.StatusOK)
}

func (v *HttpViews) UpdateSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	subscriptionId := r.URL.Query().Get("subscription_id")
	if subscriptionId == "" {
		utils.JSONErrorResponse(w, "subscription_id is required", http.StatusBadRequest)
		return
	}

	err = v.Controller.UpdateSubscription(user, subscriptionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.JSONErrorResponse(w, "subscription not found", http.StatusNotFound)
			return
		}
		utils.JSONErrorResponse(w, "failed to update subscription", http.StatusInternalServerError)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_UPDATED, map[string]any{
		"subscription_id": subscriptionId,
	})

	utils.JSONResponse(w, map[string]string{"message": "Subscription updated successfully"}, http.StatusOK)
}

func (v *HttpViews) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	err = v.Controller.CancelSubscription(user)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to cancel subscription", http.StatusInternalServerError)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

	utils.JSONResponse(w, map[string]string{"message": "Subscription cancelled successfully"}, http.StatusOK)
}

