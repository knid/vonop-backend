package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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

// Sentinel errors for auth flows. Views switch on these via errors.Is to
// emit precise coded responses (AUTH_INVALID_CREDENTIALS, AUTH_EMAIL_TAKEN,
// AUTH_RESET_TOKEN_INVALID, AUTH_RESET_TOKEN_EXPIRED, AUTH_VERIFICATION_INVALID).
// Anything not matched falls through to JSONInternalError so we never leak
// raw err.Error() text in 5xx responses.
var (
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrEmailTaken          = errors.New("email already in use")
	ErrResetTokenInvalid   = errors.New("password reset token is invalid")
	ErrResetTokenExpired   = errors.New("password reset token has expired")
	ErrVerificationInvalid = errors.New("email verification token is invalid or expired")
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

	// Determine if user has ever used credits (for welcome experience)
	hasUsage, _ := c.DB.HasUsageTransactions(user.ID.String())

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
		IsNewUser:          !hasUsage,
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
	// Upload base64 photos to S3 instead of storing in DB
	photo := userUpdateReq.Photo
	if strings.HasPrefix(photo, "data:image/") {
		imgData, err := utils.DecodeBase64Image(photo)
		if err != nil {
			return nil, errors.New("invalid photo format")
		}
		if len(imgData) > 2*1024*1024 {
			return nil, errors.New("photo must be under 2MB")
		}
		s3Key := fmt.Sprintf("profile_photos/%s.png", user.ID.String())
		publicURL, err := c.AssetsStorage.UploadFileBytePublic(context.Background(), s3Key, imgData, "image/png")
		if err != nil {
			return nil, errors.New("failed to upload photo")
		}
		photo = publicURL
	}
	user.Photo = photo

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

	hasUsage, _ := c.DB.HasUsageTransactions(updatedUser.ID.String())

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
		IsNewUser:          !hasUsage,
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

	hashedPassword, err := utils.HashPassword(userRegisterReq.Password)
	if err != nil {
		return nil, errors.New("failed to hash password")
	}

	user := models.User{
		Name:     userRegisterReq.Name,
		Email:    userRegisterReq.Email,
		Provider: "Wemob",
		Type:     "user",
		Company:  userRegisterReq.Company,
		Password: hashedPassword,
	}

	createdUser, err := c.DB.CreateUser(&user)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, ErrEmailTaken
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
	if err != nil {
		return nil, err
	}
	createdUser.CustomerID = customer.ID
	if _, err := c.DB.UpdateUser(createdUser); err != nil {
		slog.Error("failed to link Stripe customer to user", "error", err, "user_id", createdUser.ID, "customer_id", customer.ID)
	}

	c.assignFreePlan(createdUser)

	// Apply referral code if provided during registration
	if userRegisterReq.ReferralCode != "" {
		if err := c.ApplyReferralCode(createdUser, userRegisterReq.ReferralCode); err != nil {
			slog.Warn("failed to apply referral code during registration", "error", err, "user_id", createdUser.ID, "code", userRegisterReq.ReferralCode)
		}
	}

	go c.SlackService.Client.PostMessage(
		constants.SLACK_CHANNEL_ID_NEW_USER,
		slack.MsgOptionText("New registered user (Wemob): "+user.Email, false),
	)

	go c.EventService.SendEvent(createdUser, constants.EVENT_NEW_USER, map[string]any{
		"provider": "wemob",
	})

	go func() {
		if err := c.SendVerifyEmail(createdUser); err != nil {
			slog.Error("failed to send signup verification email", "error", err, "user_id", createdUser.ID)
		}
	}()

	return &responses.UserInformationResponse{
		ID:                 createdUser.ID,
		Name:               createdUser.Name,
		Email:              createdUser.Email,
		Photo:              createdUser.Photo,
		Verified:           createdUser.Verified,
		Provider:           createdUser.Provider,
		Type:               createdUser.Type,
		Company:            createdUser.Company,
		CompanyLogo:        createdUser.CompanyLogo,
		SubscriptionID:     createdUser.SubscriptionID.String(),
		SubscriptionStatus: createdUser.SubscriptionStatus,
		SubscribedAt:       createdUser.SubscriptionStartAt,
		TrialStartedAt:     createdUser.TrialStartAt,
		SetupFinished:      createdUser.SetupFinished,
		TrialUsed:          createdUser.TrialUsed,
		CreatedAt:          createdUser.CreatedAt,
		IsNewUser:          true,
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
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !utils.CheckPassword(userLoginReq.Password, user.Password) {
		return nil, ErrInvalidCredentials
	}

	// Lazy-migrate SHA-256 passwords to bcrypt on successful login
	if !utils.IsBcryptHash(user.Password) {
		if bcryptHash, hashErr := utils.HashPassword(userLoginReq.Password); hashErr == nil {
			user.Password = bcryptHash
			c.DB.UpdateUser(user)
		}
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

	go c.EventService.SendEvent(user, constants.EVENT_NEW_LOGIN, map[string]any{
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

	// CRM + PostHog events fired by the view layer (views/users.go).
	// Admin deletion path (admin_users.go) fires its own event.
	return deletedUser, nil
}

func (c *Controller) GoogleLogin(oauthUser goth.User) (*responses.UserLoginResponse, error) {
	isNewUser := false
	user, err := c.DB.GetUserByEmail(oauthUser.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			isNewUser = true
			pass, _ := utils.RandomHex(32)
			hashedPass, _ := utils.HashPassword(pass)
			newUser := models.User{
				Name:     oauthUser.Name,
				Email:    oauthUser.Email,
				Photo:    oauthUser.AvatarURL,
				Password: hashedPass,
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
			if _, err := c.DB.UpdateUser(user); err != nil {
				slog.Error("failed to link Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", customer.ID)
			}

			c.assignFreePlan(user)

			if err := c.GrantInitialFreeCreditsIfNeeded(user); err != nil {
				slog.Error("failed to grant initial free credits on google signup", "error", err, "user_id", user.ID)
			}

			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_USER,
				slack.MsgOptionText("New registered user (Google): "+user.Email, false))

			go c.EventService.SendEvent(user, constants.EVENT_NEW_USER, map[string]any{
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

	go c.EventService.SendEvent(user, constants.EVENT_NEW_LOGIN, map[string]any{
		"provider": "google",
	})

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
		IsNewUser: isNewUser,
	}, nil
}

func (c *Controller) assignFreePlan(user *models.User) {
	freePlan, err := c.DB.GetSubscriptionByName("Free")
	if err != nil {
		slog.Warn("assignFreePlan: free plan not found", "error", err)
		return
	}
	user.SubscriptionID = freePlan.ID
	user.SubscriptionStatus = "free"
	if _, err := c.DB.UpdateUser(user); err != nil {
		slog.Warn("assignFreePlan: failed to update user", "error", err)
		return
	}
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

	c.EnsureIdentified(user)

	return user, nil
}
