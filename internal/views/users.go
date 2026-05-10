package views

import (
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/middleware"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
)

func (v *HttpViews) GetUser(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	utils.JSONResponse(w, userInformationRes, http.StatusOK)
}

func (v *HttpViews) UpdateUser(w http.ResponseWriter, r *http.Request) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	userInformationRes, err := v.Controller.GetUser(tokenString)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var userUpdateReq requests.UserUpdateRequest
	if err := utils.ParseJSONBody(r, &userUpdateReq); err != nil {
		utils.ValidationError(w, err)
		return
	}

	user, err := v.Controller.DB.GetUserById(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	updatedUserRes, err := v.Controller.UpdateUser(user, userUpdateReq)
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	utils.JSONResponse(w, updatedUserRes, http.StatusOK)
}

// UpdateUserLanguage handles PUT /v1/users/me/language. Lightweight endpoint
// for syncing the i18next-detected (or switcher-selected) locale from the
// frontend without forcing a full UpdateUser round-trip with name/email/photo.
func (v *HttpViews) UpdateUserLanguage(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	var body struct {
		Language string `json:"language"`
	}
	if err := utils.ParseJSONBody(r, &body); err != nil {
		utils.ValidationError(w, err)
		return
	}

	if _, err := v.Controller.UpdateUserLanguage(user, body.Language); err != nil {
		utils.JSONError(w, "invalid_language", err.Error(), http.StatusBadRequest, nil)
		return
	}

	utils.JSONResponse(w, map[string]string{"language": body.Language}, http.StatusOK)
}

func (v *HttpViews) DeleteUser(w http.ResponseWriter, r *http.Request) {
	user, err := v.Controller.GetUserFromReq(r)
	if err != nil {
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	deletedUserRes, err := v.Controller.DeleteUser(user.ID.String())
	if err != nil {
		utils.JSONInternalError(w, err)
		return
	}

	go v.Controller.EventService.SendEvent(user, constants.EVENT_USER_DELETED, map[string]any{})

	v.Controller.TrackUserEvent(user, constants.EVENT_USER_DELETED, posthog.NewProperties().
		Set("subscription_status", v.Controller.GetUserBilling(user).SubscriptionStatus))

	utils.JSONResponse(w, deletedUserRes, http.StatusOK)
}

func (v *HttpViews) UserLogin(w http.ResponseWriter, r *http.Request) {
	var userLoginReq requests.UserLoginRequest
	if err := utils.ParseJSONBody(r, &userLoginReq); err != nil {
		utils.ValidationError(w, err)
		return
	}
	userLoginRes, err := v.Controller.LoginUser(userLoginReq)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrInvalidCredentials):
			utils.JSONErrorCode(w, constants.ErrAuthInvalidCredentials, http.StatusUnauthorized, nil)
		default:
			// Validation errors from validate.Struct still flow through here.
			utils.ValidationError(w, err)
		}
		return
	}

	go func() {
		if u, lookupErr := v.Controller.DB.GetUserByEmail(userLoginReq.Email); lookupErr == nil {
			v.Controller.EnsureIdentified(u)
			v.Controller.TrackUserEvent(u, constants.EVENT_NEW_LOGIN, posthog.NewProperties().
				Set("provider", u.Provider).
				Set("subscription_status", v.Controller.GetUserBilling(u).SubscriptionStatus))
		}
	}()

	utils.JSONResponse(w, userLoginRes, http.StatusOK)
}

func (v *HttpViews) UserRegister(w http.ResponseWriter, r *http.Request) {
	var userRegisterReq requests.UserRegisterRequest
	if err := utils.ParseJSONBody(r, &userRegisterReq); err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}
	locale := middleware.ResolveLocale(r.Header.Get("Accept-Language"))
	userInformationRes, err := v.Controller.RegisterUser(userRegisterReq, locale)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrEmailTaken):
			utils.JSONErrorCode(w, constants.ErrAuthEmailTaken, http.StatusConflict, nil)
		default:
			// Password mismatch + validate.Struct errors flow through here.
			utils.ValidationError(w, err)
		}
		return
	}

	attr := controllers.MetaAttributionFromRequest(r,
		userRegisterReq.Fbp, userRegisterReq.Fbc, userRegisterReq.EventSourceURL)

	go func() {
		u, lookupErr := v.Controller.DB.GetUserById(userInformationRes.ID.String())
		if lookupErr != nil {
			return
		}
		v.Controller.PersistMetaAttribution(u, attr)
		v.Controller.EnsureIdentified(u)
		v.Controller.TrackUserEvent(u, constants.EVENT_NEW_USER, posthog.NewProperties().
			Set("provider", u.Provider).
			Set("email", u.Email))
		v.Controller.FireMetaCompleteRegistration(u, attr)
	}()

	utils.JSONResponse(w, userInformationRes, http.StatusOK)
}

// UserForgotPassword starts a password reset flow. Always returns 200 with
// a neutral message so an attacker can't probe for registered emails.
func (v *HttpViews) UserForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req requests.UserForgotPasswordRequest
	if err := utils.ParseJSONBody(r, &req); err != nil {
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	if err := v.Controller.RequestPasswordReset(req); err != nil {
		// Validation errors are the only thing we surface — not "user exists"
		// vs "user missing". Everything else is swallowed inside the controller.
		utils.ValidationError(w, err)
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
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	if err := v.Controller.ConsumePasswordReset(req); err != nil {
		switch {
		case errors.Is(err, controllers.ErrResetTokenInvalid):
			utils.JSONErrorCode(w, constants.ErrAuthResetTokenInvalid, http.StatusBadRequest, nil)
		case errors.Is(err, controllers.ErrResetTokenExpired):
			utils.JSONErrorCode(w, constants.ErrAuthResetTokenExpired, http.StatusBadRequest, nil)
		default:
			// validate.Struct + password-mismatch errors stay on ValidationError.
			utils.ValidationError(w, err)
		}
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
		utils.JSONErrorCode(w, constants.ErrAuthInvalidToken, http.StatusUnauthorized, nil)
		return
	}

	if err := v.Controller.SendVerifyEmail(user); err != nil {
		utils.JSONInternalError(w, err)
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
		utils.JSONErrorCode(w, constants.ErrValidationBody, http.StatusBadRequest, nil)
		return
	}

	user, err := v.Controller.ConsumeVerifyEmail(req)
	if err != nil {
		switch {
		case errors.Is(err, controllers.ErrVerificationInvalid):
			utils.JSONErrorCode(w, constants.ErrAuthVerificationInvalid, http.StatusBadRequest, nil)
		default:
			utils.ValidationError(w, err)
		}
		return
	}

	utils.JSONResponse(w, map[string]any{
		"success":  true,
		"verified": user.Verified,
		"message":  "Email verified.",
	}, http.StatusOK)
}
