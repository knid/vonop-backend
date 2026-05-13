package controllers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	"github.com/posthog/posthog-go"
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
	ErrInvalidCredentials          = errors.New("invalid email or password")
	ErrEmailTaken                  = errors.New("email already in use")
	ErrResetTokenInvalid           = errors.New("password reset token is invalid")
	ErrResetTokenExpired           = errors.New("password reset token has expired")
	ErrVerificationInvalid         = errors.New("email verification token is invalid or expired")
	ErrEmailHasCredentialsAccount  = errors.New("an account with this email already exists; sign in with your password, then link Google from Settings")
)

// hashEmailForLog returns the first 8 hex chars of sha256(lowercased email).
// Used in slog to monitor abuse patterns without writing raw emails to logs.
func hashEmailForLog(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])[:8]
}

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

	billing := c.GetUserBilling(user)

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
		SubscriptionID:     billing.SubscriptionID.String(),
		SubscriptionStatus: billing.SubscriptionStatus,
		SubscribedAt:       billing.SubscriptionStartAt,
		TrialStartedAt:     billing.TrialStartAt,
		SetupFinished:      user.SetupFinished,
		TrialUsed:          billing.TrialUsed,
		Language:           user.Language,
		CreatedAt:          user.CreatedAt,
		IsNewUser:          !hasUsage,
	}, nil
}

// UpdateUserLanguage persists the user's preferred email locale. Used by the
// frontend whenever the i18next-detected (or switcher-selected) locale changes,
// so Resend automations can branch on contact.language to pick the EN or TR
// template variant. Validation is intentionally narrow — only "en" or "tr".
func (c *Controller) UpdateUserLanguage(user *models.User, language string) (*models.User, error) {
	if language != "en" && language != "tr" {
		return nil, errors.New("language must be 'en' or 'tr'")
	}
	if user.Language == language {
		return user, nil
	}
	user.Language = language
	user.UpdatedAt = time.Now()
	return c.DB.UpdateUser(user)
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
	if userUpdateReq.Language != "" {
		user.Language = userUpdateReq.Language
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
		// Revoke active sessions; stolen tokens shouldn't survive an email change.
		if err := c.DB.DeleteTokensByUser(context.Background(), user.ID); err != nil {
			slog.Warn("failed to revoke sessions after email change", "error", err, "user_id", user.ID)
		}
	}

	updatedUser, err := c.DB.UpdateUser(user)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, errors.New("user with this email already exists")
		}
		return nil, err
	}

	hasUsage, _ := c.DB.HasUsageTransactions(updatedUser.ID.String())

	billing := c.GetUserBilling(updatedUser)

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
		SubscriptionID:     billing.SubscriptionID.String(),
		SubscriptionStatus: billing.SubscriptionStatus,
		SubscribedAt:       billing.SubscriptionStartAt,
		TrialStartedAt:     billing.TrialStartAt,
		SetupFinished:      updatedUser.SetupFinished,
		TrialUsed:          billing.TrialUsed,
		Language:           updatedUser.Language,
		CreatedAt:          updatedUser.CreatedAt,
		IsNewUser:          !hasUsage,
	}, nil
}

// RegisterUser creates a new email/password account. `locale` is the base
// language code resolved from Accept-Language (one of middleware.SupportedLocales),
// used to seed the user's Language so the first welcome email and any pre-login
// reads of user.language hit the right bundle without waiting on the frontend
// to push an update. Pass "" to fall back to the DB default.
func (c *Controller) RegisterUser(userRegisterReq requests.UserRegisterRequest, locale string) (*responses.UserInformationResponse, error) {
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
	if locale != "" {
		user.Language = locale
	}

	createdUser, err := c.DB.CreateUser(&user)
	if err != nil {
		if strings.Contains(err.Error(), "23505") {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	// Personal org must exist before we create the Stripe customer so the
	// customer's metadata.organization_id is set up-front. Webhook routing
	// keys off this metadata, so any race where Stripe fires before backfill
	// runs would silently no-op without it.
	personalOrg, err := c.EnsurePersonalOrg(createdUser)
	if err != nil {
		slog.Error("failed to ensure personal org on registration", "error", err, "user_id", createdUser.ID)
		return nil, err
	}

	params := stripe.CustomerParams{
		Email: &createdUser.Email,
		Metadata: map[string]string{
			"organization_id": personalOrg.ID.String(),
			"user_id":         createdUser.ID.String(),
		},
	}

	customer, err := customer.New(&params)
	if err != nil {
		return nil, err
	}

	// Dual-write the Stripe customer ID to both the org (new home) and the
	// user (legacy column, kept until cleanup) so any reads hitting the old
	// path during the transition still resolve.
	personalOrg.StripeCustomerID = customer.ID
	if _, err := c.DB.UpdateOrganization(personalOrg); err != nil {
		slog.Error("failed to persist Stripe customer to org", "error", err, "org_id", personalOrg.ID, "customer_id", customer.ID)
	}
	createdUser.CustomerID = customer.ID
	if _, err := c.DB.UpdateUser(createdUser); err != nil {
		slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", createdUser.ID, "customer_id", customer.ID)
	}

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
		SubscriptionID:     personalOrg.SubscriptionID.String(),
		SubscriptionStatus: personalOrg.SubscriptionStatus,
		SubscribedAt:       personalOrg.SubscriptionStartAt,
		TrialStartedAt:     personalOrg.TrialStartAt,
		SetupFinished:      createdUser.SetupFinished,
		TrialUsed:          personalOrg.TrialUsed,
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

// GoogleLogin signs in (or signs up) a user via the goth web OAuth callback.
// `locale` is the base language code resolved from the browser's
// Accept-Language header — used to seed new users' Language so transactional
// emails and the first UI render match the browser, instead of falling back
// to the DB default "en". Pass "" to skip and let the default apply.
func (c *Controller) GoogleLogin(oauthUser goth.User, attr MetaAttribution, locale string) (*responses.UserLoginResponse, error) {
	isNewUser := false
	user, err := c.DB.GetUserByEmail(oauthUser.Email)
	if err == nil && user != nil && user.Provider == "Wemob" {
		// Refuse rather than merge: prevents takeover via pre-registered email collision.
		slog.Warn("google login refused: email has credentials account",
			"flow", "web",
			"email_hash", hashEmailForLog(oauthUser.Email))
		return nil, ErrEmailHasCredentialsAccount
	}
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
			if locale != "" {
				newUser.Language = locale
			}

			user, err = c.DB.CreateUser(&newUser)
			if err != nil {
				return nil, err
			}

			personalOrg, orgErr := c.EnsurePersonalOrg(user)
			if orgErr != nil {
				slog.Error("failed to ensure personal org on google signup", "error", orgErr, "user_id", user.ID)
				return nil, orgErr
			}

			params := stripe.CustomerParams{
				Email: stripe.String(user.Email),
				Metadata: map[string]string{
					"organization_id": personalOrg.ID.String(),
					"user_id":         user.ID.String(),
				},
			}

			customer, err := customer.New(&params)
			if err != nil {
				return nil, err
			}
			personalOrg.StripeCustomerID = customer.ID
			if _, err := c.DB.UpdateOrganization(personalOrg); err != nil {
				slog.Error("failed to persist Stripe customer to org", "error", err, "org_id", personalOrg.ID, "customer_id", customer.ID)
			}
			user.CustomerID = customer.ID
			if _, err := c.DB.UpdateUser(user); err != nil {
				slog.Error("failed to persist Stripe customer to user", "error", err, "user_id", user.ID, "customer_id", customer.ID)
			}

			if err := c.GrantInitialFreeCreditsIfNeeded(user, personalOrg.ID); err != nil {
				slog.Error("failed to grant initial free credits on google signup", "error", err, "user_id", user.ID)
			}

			go c.SlackService.Client.PostMessage(
				constants.SLACK_CHANNEL_ID_NEW_USER,
				slack.MsgOptionText("New registered user (Google): "+user.Email, false))

			go c.EventService.SendEvent(user, constants.EVENT_NEW_USER, map[string]any{
				"provider": "google",
			})

			// Google asserts email_verified in its OAuth payload so we set
			// Verified: true above without a confirmation roundtrip. Mirror
			// that as a PostHog event so funnels treat OAuth signups the
			// same as email-confirmed signups instead of dropping them at
			// the verification step.
			c.TrackUserEvent(user, constants.EVENT_EMAIL_VERIFIED, posthog.NewProperties().
				Set("verified_via", "google_oauth_web"))

			// Meta CAPI: fire CompleteRegistration only on first signup. The
			// OAuth bridge cookie (set in OAuthBegin) carries fbp/fbc across
			// the Google redirect so match quality matches the password flow.
			c.PersistMetaAttribution(user, attr)
			go c.FireMetaCompleteRegistration(user, attr)
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
