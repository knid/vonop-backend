package controllers

import (
	"errors"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) GetUser(tokenStr string) (*models.User, error) {
	token, err := c.DB.GetTokenByToken(tokenStr)
	if err != nil {
		return nil, err
	}

	return &token.User, nil
}

func (c *Controller) UpdateUser(user *models.User, userUpdateReq requests.UserUpdateRequest) (*responses.UserInformationResponse, error) {
	if userUpdateReq.Password != userUpdateReq.PasswordCheck {
		return nil, errors.New("Passwords doesn't match")
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(userUpdateReq); err != nil {
		return nil, err
	}

	user.Name = userUpdateReq.Name
	user.Email = userUpdateReq.Email
	user.Photo = userUpdateReq.Photo
	user.Password = utils.GetSHA256Hash(userUpdateReq.Password)

	updatedUser, err := c.DB.UpdateUser(user)
	if err != nil {
		return nil, err
	}

	return &responses.UserInformationResponse{
		ID:       updatedUser.ID,
		Name:     updatedUser.Name,
		Email:    updatedUser.Email,
		Photo:    updatedUser.Photo,
		Verified: updatedUser.Verified,
		Provider: updatedUser.Provider,
	}, nil
}

func (c *Controller) RegisterUser(userRegisterReq requests.UserRegisterRequest) (*responses.UserInformationResponse, error) {
	if userRegisterReq.Password != userRegisterReq.PasswordCheck {
		return nil, errors.New("Passwords doesn't match")
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(userRegisterReq); err != nil {
		return nil, err
	}

	user := models.User{
		Name:     userRegisterReq.Name,
		Email:    userRegisterReq.Email,
		Password: utils.GetSHA256Hash(userRegisterReq.Password),
	}

	createdUser, err := c.DB.CreateUser(&user)
	if err != nil {
		return nil, err
	}

	return &responses.UserInformationResponse{
		ID:       createdUser.ID,
		Name:     createdUser.Name,
		Email:    createdUser.Email,
		Photo:    createdUser.Photo,
		Verified: createdUser.Verified,
		Provider: createdUser.Provider,
	}, nil
}

func (c *Controller) LoginUser(userLoginReq requests.UserLoginRequest) (*responses.UserLoginResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(userLoginReq); err != nil {
		return nil, err
	}

	user, err := c.DB.GetUserByEmail(userLoginReq.Email)
	if err != nil {
		return nil, err
	}

	if user.Password != utils.GetSHA256Hash(user.Password) {
		return nil, errors.New("Password is not correct")
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	token := models.Token{
		User:      *user,
		String:    tokenStr,
		ExpiredAt: time.Now().AddDate(0, 0, 14),
	}

	createdToken, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, err
	}

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
	}, nil

}

func (c *Controller) DeleteUser(tokenStr string) (*models.User, error) {
	user, err := c.GetUser(tokenStr)
	if err != nil {
		return nil, err
	}

	deletedUser, err := c.DB.DeleteUser(user)
	if err != nil {
		return nil, err
	}

	return deletedUser, nil
}
