package views

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/invoice"
	"github.com/stripe/stripe-go/v82/subscription"
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusNotFound)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
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
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_SUBSCRIPTION_CANCELLED, map[string]any{})

	utils.JSONResponse(w, map[string]string{"message": "Subscription cancelled successfully"}, http.StatusOK)
}

func (v *HttpViews) EndTrialNowAndChargeForUser(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if user.CustomerID == "" {
		utils.JSONErrorResponse(w, "user has no stripe customer id", http.StatusBadRequest)
		return
	}

	if user.SubscriptionStatus != "trial" {
		utils.JSONErrorResponse(w, "user is not on trial", http.StatusConflict)
		return
	}

	lp := &stripe.SubscriptionListParams{
		Customer: stripe.String(user.CustomerID),
	}

	iter := subscription.List(lp)

	var trialSub *stripe.Subscription
	for iter.Next() {
		s := iter.Subscription()

		if s.Status == stripe.SubscriptionStatusTrialing && s.TrialEnd > 0 {
			trialSub = s
			break
		}
	}
	if err := iter.Err(); err != nil {
		utils.JSONErrorResponse(w, "failed to list subscriptions: "+err.Error(), http.StatusBadRequest)
		return
	}
	if trialSub == nil {
		utils.JSONErrorResponse(w, "no trial subscription found for this user", http.StatusConflict)
		return
	}

	up := &stripe.SubscriptionParams{
		TrialEndNow: stripe.Bool(true),
	}
	up.AddExpand("latest_invoice")
	up.AddExpand("latest_invoice.payment_intent")

	up.Params.IdempotencyKey = stripe.String("endtrial_" + user.ID.String() + "_" + trialSub.ID)

	updated, err := subscription.Update(trialSub.ID, up)
	if err != nil {
		utils.JSONErrorResponse(w, "failed to end trial now: "+err.Error(), http.StatusBadRequest)
		return
	}

	if updated.LatestInvoice == nil || updated.LatestInvoice.ID == "" {
		utils.JSONResponse(w, map[string]any{
			"status":          "trial_ended_no_invoice_yet",
			"subscription_id": updated.ID,
		}, http.StatusAccepted)
		return
	}

	time.Sleep(time.Second * 2)

	pay := &stripe.InvoicePayParams{}
	pay.AddExpand("payment_intent")
	pay.Params.IdempotencyKey = stripe.String("payinv_" + user.ID.String() + "_" + updated.LatestInvoice.ID)

	inv, err := invoice.Pay(updated.LatestInvoice.ID, pay)

	resp := map[string]any{
		"user_id": user.ID,
		// "customer_id":        user.CustomerID,
		"subscription_id":    updated.ID,
		"invoice_id":         inv.ID,
		"status":             inv.Status,
		"hosted_invoice_url": inv.HostedInvoiceURL,
	}

	if err != nil {
		resp["error"] = err.Error()
		resp["status"] = "payment_failed"
		resp["message"] = "invoice created but payment failed"
		utils.JSONResponse(w, resp, http.StatusPaymentRequired)
		return
	}

	v.Controller.TrackUserEvent(user, "trial_ended_manually", posthog.NewProperties().
		Set("subscription_id", updated.ID).
		Set("invoice_id", inv.ID).
		Set("invoice_status", string(inv.Status)))

	if resp["status"] != "paid" {
		utils.JSONResponse(w, resp, http.StatusPaymentRequired)
	}

	utils.JSONResponse(w, resp, http.StatusOK)
}
