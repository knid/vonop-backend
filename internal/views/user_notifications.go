package views

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/knid/wemob/internal/utils"
)

func (v *HttpViews) GetUserNotifications(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userNotifications, err := v.Controller.GetUserNotifications(user)
	utils.JSONResponse(w, userNotifications, http.StatusOK)
}

func (v *HttpViews) GetUserNotification(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	userNotificationId := chi.URLParam(r, "userNotificationId")
	userNotificationRes, err := v.Controller.GetUserNotification(user, userNotificationId)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusForbidden)
		return
	}

	utils.JSONResponse(w, userNotificationRes, http.StatusOK)
}
