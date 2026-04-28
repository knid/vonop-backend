package controllers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

func (c *Controller) GoogleMobileLogin(req requests.GoogleMobileLoginRequest) (*responses.UserLoginResponse, error) {
	resp, err := http.Get("https://oauth2.googleapis.com/tokeninfo?id_token=" + req.IDToken)
	if err != nil || resp.StatusCode != 200 {
		return nil, fmt.Errorf("invalid Google token")
	}
	defer resp.Body.Close()

	var tokenInfo struct {
		Email         string `json:"email"`
		EmailVerified string `json:"email_verified"`
		Sub           string `json:"sub"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return nil, fmt.Errorf("failed to verify Google token")
	}

	if tokenInfo.Email == "" || tokenInfo.EmailVerified != "true" {
		return nil, fmt.Errorf("invalid Google token: email not verified")
	}

	email := tokenInfo.Email

	isNewUser := false
	user, err := c.DB.GetUserByEmail(email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			isNewUser = true
			pass, _ := utils.RandomHex(32)
			hashedPass, _ := utils.HashPassword(pass)
			newUser := models.User{
				Name:     req.Name,
				Email:    email,
				Photo:    req.Photo,
				Password: hashedPass,
				Provider: "Google",
				Verified: true,
			}

			user, err = c.DB.CreateUser(&newUser)
			if err != nil {
				return nil, fmt.Errorf("failed to create user")
			}

			params := &stripe.CustomerParams{
				Email: stripe.String(user.Email),
				Metadata: map[string]string{
					"user_id": user.ID.String(),
				},
			}
			stripeCustomer, err := customer.New(params)
			if err != nil {
				return nil, err
			}
			user.CustomerID = stripeCustomer.ID
			c.DB.UpdateUser(user)

			c.assignFreePlan(user)

			if err := c.GrantInitialFreeCreditsIfNeeded(user); err != nil {
				slog.Error("failed to grant initial free credits on google mobile signup", "error", err, "user_id", user.ID)
			}

			go c.CRMService.SendEvent(user, constants.EVENT_NEW_USER, map[string]any{
				"provider": "google_mobile",
			})
		} else {
			return nil, fmt.Errorf("failed to look up user")
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

	go c.CRMService.SendEvent(user, constants.EVENT_NEW_LOGIN, map[string]any{
		"provider": "google_mobile",
	})

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
		IsNewUser: isNewUser,
	}, nil
}
