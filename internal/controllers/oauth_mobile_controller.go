package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/utils"
	"github.com/posthog/posthog-go"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
	"google.golang.org/api/idtoken"
)

// GoogleMobileLogin signs in (or signs up) a user via the mobile id_token
// flow. `locale` is the base language code resolved from Accept-Language,
// used to seed new users' Language so emails and first UI render match the
// device locale.
func (c *Controller) GoogleMobileLogin(ctx context.Context, req requests.GoogleMobileLoginRequest, locale string) (*responses.UserLoginResponse, error) {
	expectedAudience := c.SecretManager.GetSecret("GOOGLE_MOBILE_CLIENT_ID")
	if expectedAudience == "" {
		// Fail loud rather than silently accept tokens with unchecked aud.
		panic("GOOGLE_MOBILE_CLIENT_ID secret is not set; mobile Google login cannot validate id_token audience")
	}

	// idtoken.Validate verifies aud + iss + signature + exp in one call;
	// the previous tokeninfo endpoint did none of these.
	payload, err := idtoken.Validate(ctx, req.IDToken, expectedAudience)
	if err != nil {
		return nil, fmt.Errorf("invalid Google token")
	}

	emailVal, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	if emailVal == "" || !emailVerified {
		return nil, fmt.Errorf("invalid Google token: email not verified")
	}

	email := emailVal
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)

	isNewUser := false
	user, err := c.DB.GetUserByEmail(email)
	if err == nil && user != nil && user.Provider == "Wemob" {
		// Refuse rather than merge: prevents takeover via pre-registered email collision.
		slog.Warn("google login refused: email has credentials account",
			"flow", "mobile",
			"email_hash", hashEmailForLog(email))
		return nil, ErrEmailHasCredentialsAccount
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			isNewUser = true
			pass, _ := utils.RandomHex(32)
			hashedPass, _ := utils.HashPassword(pass)
			newUser := models.User{
				Name:     name,
				Email:    email,
				Photo:    picture,
				Password: hashedPass,
				Provider: "Google",
				Verified: true,
			}
			if locale != "" {
				newUser.Language = locale
			}

			user, err = c.DB.CreateUser(&newUser)
			if err != nil {
				return nil, fmt.Errorf("failed to create user")
			}

			personalOrg, orgErr := c.EnsurePersonalOrg(user)
			if orgErr != nil {
				slog.Error("failed to ensure personal org on google mobile signup", "error", orgErr, "user_id", user.ID)
				return nil, fmt.Errorf("failed to create personal org")
			}

			params := &stripe.CustomerParams{
				Email: stripe.String(user.Email),
				Metadata: map[string]string{
					"organization_id": personalOrg.ID.String(),
					"user_id":         user.ID.String(),
				},
			}
			stripeCustomer, err := customer.New(params)
			if err != nil {
				return nil, err
			}
			personalOrg.StripeCustomerID = stripeCustomer.ID
			if _, err := c.DB.UpdateOrganization(personalOrg); err != nil {
				slog.Error("failed to persist Stripe customer to org", "error", err, "org_id", personalOrg.ID, "customer_id", stripeCustomer.ID)
			}
			user.CustomerID = stripeCustomer.ID
			if _, err := c.DB.UpdateUser(user); err != nil {
				slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", stripeCustomer.ID)
			}

			if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
				slog.Error("failed to grant initial free credits on google mobile signup", "error", err, "user_id", user.ID)
			}

			go c.EventService.SendEvent(user, constants.EVENT_NEW_USER, map[string]any{
				"provider": "google_mobile",
			})

			// Google id_token already carries email_verified=true (validated
			// at line 36 above). Mirror it to PostHog so OAuth signups don't
			// register as a drop-off at the funnel's verify step.
			c.TrackUserEvent(user, constants.EVENT_EMAIL_VERIFIED, posthog.NewProperties().
				Set("verified_via", "google_oauth_mobile"))
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

	go c.EventService.SendEvent(user, constants.EVENT_NEW_LOGIN, map[string]any{
		"provider": "google_mobile",
	})

	return &responses.UserLoginResponse{
		Token:     createdToken.String,
		ExpiredAt: createdToken.ExpiredAt,
		IsNewUser: isNewUser,
	}, nil
}
