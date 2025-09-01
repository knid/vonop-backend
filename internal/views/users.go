package views

import (
	"errors"
	"net/http"

	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
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

	deletedUserRes, err := v.Controller.DeleteUser(userInformationRes.ID.String())
	if err != nil {
		utils.JSONErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}

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

	utils.JSONResponse(w, userInformationRes, http.StatusOK)
}

func (v *HttpViews) UserForgotPassword(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to send forgot password email
}

func (v *HttpViews) UserResetPassword(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to reset user password after receiving the reset token
}

func (v *HttpViews) UserVerifyEmail(w http.ResponseWriter, r *http.Request) {
	// Implementation of the view to verify user email using a token
}

func (v *HttpViews) UserResendVerifactionEmail(w http.ResponseWriter, r *http.Request) {
	//  Implementation of the view to resend verification email to the user
}
