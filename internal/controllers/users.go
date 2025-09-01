package controllers

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
)

func (c *Controller) GetUserModelById(id string) (*models.User, error) {
	return c.DB.GetUserById(id)
}

func (c *Controller) GetUser(tokenStr string) (*responses.UserInformationResponse, error) {
	token, err := c.DB.GetTokenByToken(tokenStr)
	if err != nil {
		return nil, err
	}

	if token.ExpiredAt.Before(time.Now()) {
		return nil, errors.New("token is expired")
	}

	userId := token.UserID
	user, err := c.DB.GetUserById(userId.String())
	if err != nil {
		return nil, err
	}

	return &responses.UserInformationResponse{
		ID:       user.ID,
		Name:     user.Name,
		Email:    user.Email,
		Photo:    user.Photo,
		Verified: user.Verified,
		Provider: user.Provider,
	}, nil
}

func (c *Controller) UpdateUser(user *models.User, userUpdateReq requests.UserUpdateRequest) (*responses.UserInformationResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.UserUpdateRequest](userUpdateReq, validate); err != nil {
		return nil, err
	}

	user.Name = userUpdateReq.Name
	user.Email = userUpdateReq.Email
	user.Photo = userUpdateReq.Photo

	if user.Email != userUpdateReq.Email {
		user.Verified = false
	}

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
		return nil, errors.New("passwords doesn't match")
	}

	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.UserRegisterRequest](userRegisterReq, validate); err != nil {
		return nil, err
	}

	fmt.Println(validate.Struct(userRegisterReq))

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

	if user.Password != utils.GetSHA256Hash(userLoginReq.Password) {
		return nil, errors.New("password is not correct")
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	fmt.Println(user.ID)
	token := models.Token{
		User:      user,
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().AddDate(0, 0, 14),
	}
	fmt.Println(token)

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

	fmt.Println(user)

	userModel, err := c.DB.GetUserById(user.ID.String())
	if err != nil {
		return nil, err
	}

	fmt.Println(userModel)

	deletedUser, err := c.DB.DeleteUser(userModel)
	if err != nil {
		return nil, err
	}

	return deletedUser, nil
}
