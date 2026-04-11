package views

import (
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) GetUser(w http.ResponseWriter, r *http.Request) {
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

	utils.JSONResponse(w, userInformationRes, http.StatusOK)
}

func (v *HttpViews) UpdateUser(w http.ResponseWriter, r *http.Request) {
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

	var userUpdateReq requests.UserUpdateRequest
	if err := utils.ParseJSONBody(r, &userUpdateReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := v.Controller.DB.GetUserById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	updatedUserRes, err := v.Controller.UpdateUser(user, userUpdateReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, updatedUserRes, http.StatusOK)
}

func (v *HttpViews) DeleteUser(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	deletedUserRes, err := v.Controller.DeleteUser(user.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go v.Controller.CRMService.SendEvent(user, constants.BREVO_EVENT_USER_DELETED, map[string]any{})

	v.Controller.TrackUserEvent(user, "user_deleted", posthog.NewProperties().
		Set("subscription_status", user.SubscriptionStatus))

	utils.JSONResponse(w, deletedUserRes, http.StatusOK)
}

func (v *HttpViews) UserLogin(w http.ResponseWriter, r *http.Request) {
	var userLoginReq requests.UserLoginRequest
	if err := utils.ParseJSONBody(r, &userLoginReq); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	userLoginRes, err := v.Controller.LoginUser(userLoginReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		if u, lookupErr := v.Controller.DB.GetUserByEmail(userLoginReq.Email); lookupErr == nil {
			v.Controller.EnsureIdentified(u)
			v.Controller.TrackUserEvent(u, "user_logged_in", posthog.NewProperties().
				Set("provider", u.Provider).
				Set("subscription_status", u.SubscriptionStatus))
		}
	}()

	utils.JSONResponse(w, userLoginRes, http.StatusOK)
}

func (v *HttpViews) UserRegister(w http.ResponseWriter, r *http.Request) {
	var userRegisterReq requests.UserRegisterRequest
	if err := utils.ParseJSONBody(r, &userRegisterReq); err != nil {
		utils.JSONErrorResponse(w, errors.New("could not parse body").Error(), http.StatusBadRequest)
		return
	}
	userInformationRes, err := v.Controller.RegisterUser(userRegisterReq)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		if u, lookupErr := v.Controller.DB.GetUserById(userInformationRes.ID.String()); lookupErr == nil {
			v.Controller.EnsureIdentified(u)
			v.Controller.TrackUserEvent(u, "user_registered", posthog.NewProperties().
				Set("provider", u.Provider).
				Set("email", u.Email))
		}
	}()

	utils.JSONResponse(w, userInformationRes, http.StatusOK)
}

// UserForgotPassword starts a password reset flow. Always returns 200 with
// a neutral message so an attacker can't probe for registered emails.
func (v *HttpViews) UserForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req requests.UserForgotPasswordRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "could not parse body", http.StatusBadRequest)
		return
	}

	if err := v.Controller.RequestPasswordReset(req); err != nil {
		// Validation errors are the only thing we surface — not "user exists"
		// vs "user missing". Everything else is swallowed inside the controller.
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"success": true,
		"message": "If an account exists for that email, a reset link has been sent.",
	}, http.StatusOK)
}

// UserResetPassword consumes a reset token and sets a new password.
func (v *HttpViews) UserResetPassword(w http.ResponseWriter, r *http.Request) {
	var req requests.UserResetPasswordRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "could not parse body", http.StatusBadRequest)
		return
	}

	if err := v.Controller.ConsumePasswordReset(req); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"success": true,
		"message": "Your password has been updated. You can sign in now.",
	}, http.StatusOK)
}

// UserSendVerifyEmail (authenticated) issues a fresh verification email to
// the current user. No-op for users that are already verified.
func (v *HttpViews) UserSendVerifyEmail(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusUnauthorized)
		return
	}

	if err := v.Controller.SendVerifyEmail(user); err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"success": true,
		"message": "Verification email sent. Check your inbox.",
	}, http.StatusOK)
}

// UserConfirmVerifyEmail consumes a verify-email token and flips Verified.
// Public route — the token is the authentication.
func (v *HttpViews) UserConfirmVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req requests.UserVerifyEmailRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorResponse(w, "could not parse body", http.StatusBadRequest)
		return
	}

	user, err := v.Controller.ConsumeVerifyEmail(req)
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

	utils.JSONResponse(w, map[string]any{
		"success":  true,
		"verified": user.Verified,
		"message":  "Email verified.",
	}, http.StatusOK)
}

