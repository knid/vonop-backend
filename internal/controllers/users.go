package controllers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/markbates/goth"
	"github.com/slack-go/slack"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
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
		ID:                 user.ID,
		Name:               user.Name,
		Email:              user.Email,
		Photo:              user.Photo,
		Verified:           user.Verified,
		Provider:           user.Provider,
		Type:               user.Type,
		Company:            user.Company,
		CompanyLogo:        user.CompanyLogo,
		SubscriptionID:     user.SubscriptionID.String(),
		SubscriptionStatus: user.SubscriptionStatus,
		SubscribedAt:       user.SubscriptionStartAt,
		TrialStartedAt:     user.TrialStartAt,
		SetupFinished:      user.SetupFinished,
		TrialUsed:          user.TrialUsed,
		CreatedAt:          user.CreatedAt,
	}, nil
}

func (c *Controller) UpdateUser(user *models.User, userUpdateReq requests.UserUpdateRequest) (*responses.UserInformationResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := utils.ValidateFunc[requests.UserUpdateRequest](userUpdateReq, validate); err != nil {
		return nil, err
	}

	user.Name = userUpdateReq.Name
	if user.Provider == "Wemob" {
		user.Email = userUpdateReq.Email
	}
	user.Photo = userUpdateReq.Photo

	if user.Email != userUpdateReq.Email {
		user.Verified = false
	}

	updatedUser, err := c.DB.UpdateUser(user)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, errors.New("user with this email already exists")
		}
		return nil, err
	}

	return &responses.UserInformationResponse{
		ID:                 updatedUser.ID,
		Name:               updatedUser.Name,
		Email:              updatedUser.Email,
		Photo:              updatedUser.Photo,
		Verified:           updatedUser.Verified,
		Provider:           updatedUser.Provider,
		Type:               updatedUser.Type,
		Company:            updatedUser.Company,
		CompanyLogo:        updatedUser.CompanyLogo,
		SubscriptionID:     updatedUser.SubscriptionID.String(),
		SubscriptionStatus: updatedUser.SubscriptionStatus,
		SubscribedAt:       updatedUser.SubscriptionStartAt,
		TrialStartedAt:     updatedUser.TrialStartAt,
		SetupFinished:      updatedUser.SetupFinished,
		TrialUsed:          updatedUser.TrialUsed,
		CreatedAt:          updatedUser.CreatedAt,
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

	user := models.User{
		Name:     userRegisterReq.Name,
		Email:    userRegisterReq.Email,
		Provider: "Wemob",
		Type:     userRegisterReq.Type,
		Company:  userRegisterReq.Company,
		Password: utils.GetSHA256Hash(userRegisterReq.Password),
	}

	createdUser, err := c.DB.CreateUser(&user)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, errors.New("user with this email already exists")
		}
		return nil, err
	}

	params := stripe.CustomerParams{
		Email: &createdUser.Email,
		Metadata: map[string]string{
			"user_id": user.ID.String(),
		},
	}

	customer, err := customer.New(&params)
	createdUser.CustomerID = customer.ID
	c.DB.UpdateUser(createdUser)
	if err != nil {
		return nil, err
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_USER,
		slack.MsgOptionText("New registered user (Wemob): "+user.Email, false),
	)

	go c.CRMService.SendEvent(createdUser, constants.BREVO_EVENT_NEW_USER, map[string]any{
		"provider": "wemob",
	})

	return &responses.UserInformationResponse{
		ID:                 user.ID,
		Name:               user.Name,
		Email:              user.Email,
		Photo:              user.Photo,
		Verified:           user.Verified,
		Provider:           user.Provider,
		Type:               user.Type,
		Company:            user.Company,
		CompanyLogo:        user.CompanyLogo,
		SubscriptionID:     user.SubscriptionID.String(),
		SubscriptionStatus: user.SubscriptionStatus,
		SubscribedAt:       user.SubscriptionStartAt,
		TrialStartedAt:     user.TrialStartAt,
		SetupFinished:      user.SetupFinished,
		TrialUsed:          user.TrialUsed,
		CreatedAt:          user.CreatedAt,
	}, nil
}

func (c *Controller) LoginUser(userLoginReq requests.UserLoginRequest) (*responses.UserLoginResponse, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(userLoginReq); err != nil {
		return nil, err
	}

	user, err := c.DB.GetUserByEmail(userLoginReq.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user with this email does not exist")
		}
		return nil, err
	}

	if user.Password != utils.GetSHA256Hash(userLoginReq.Password) {
		return nil, errors.New("password is not correct")
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	token := models.Token{
		User:      user,
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().AddDate(0, 0, 14),
	}

	createdToken, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, err
	}

	go c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_LOGIN, map[string]any{
		"provider": "wemob",
	})

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

	userModel, err := c.DB.GetUserById(user.ID.String())
	if err != nil {
		return nil, err
	}

	deletedUser, err := c.DB.DeleteUser(userModel)
	if err != nil {
		return nil, err
	}

	go c.CRMService.SendEvent(deletedUser, constants.BREVO_EVENT_USER_DELETED, map[string]any{})
	return deletedUser, nil
}

func (c *Controller) GoogleLogin(oauthUser goth.User) (*responses.UserLoginResponse, error) {
	user, err := c.DB.GetUserByEmail(oauthUser.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			pass, _ := utils.RandomHex(32)
			/* 			userRegisterReq := requests.UserRegisterRequest{
			   				Name:          oauthUser.Name,
			   				Email:         oauthUser.Email,
			   				Password:      pass,
			   				PasswordCheck: pass,
			   			}
			   			c.RegisterUser(userRegisterReq) */
			newUser := models.User{
				Name:     oauthUser.Name,
				Email:    oauthUser.Email,
				Photo:    oauthUser.AvatarURL,
				Password: utils.GetSHA256Hash(pass),
				Provider: "Google",
				Verified: true,
			}

			user, err = c.DB.CreateUser(&newUser)
			if err != nil {
				return nil, err
			}

			params := stripe.CustomerParams{
				Email: stripe.String(user.Email),
				Metadata: map[string]string{
					"user_id": user.ID.String(),
				},
			}

			customer, err := customer.New(&params)
			if err != nil {
				return nil, err
			}
			user.CustomerID = customer.ID
			c.DB.UpdateUser(user)

			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_USER,
				slack.MsgOptionText("New registered user (Google): "+user.Email, false))

			go c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_USER, map[string]any{
				"provider": "google",
			})
		}
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return nil, err
	}

	token := models.Token{
		UserID:    user.ID,
		String:    tokenStr,
		ExpiredAt: time.Now().AddDate(0, 0, 14),
	}

	createdToken, err := c.DB.CreateToken(&token)
	if err != nil {
		return nil, err
	}

	go c.CRMService.SendEvent(user, constants.BREVO_EVENT_NEW_LOGIN, map[string]any{
		"provider": "google",
	})

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
	}, nil
}

func (c *Controller) GetUserFromReq(r *http.Request) (*models.User, error) {
	tokenString, err := utils.ExtractTokenFromHeader(r)
	if err != nil {
		return nil, err
	}

	token, err := c.DB.GetTokenByToken(tokenString)
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

	return user, nil
}
