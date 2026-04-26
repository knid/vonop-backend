package controllers

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/requests"
	"github.com/knid/wemob/internal/utils"
)

// Lifetimes for one-time action tokens. Short enough to limit the damage
// if an email is intercepted, long enough that a user who reads their mail
// a few hours later still succeeds.
const (
	passwordResetTokenTTL = 30 * time.Minute
	verifyEmailTokenTTL   = 24 * time.Hour
)

// RequestPasswordReset issues a password reset token for the email address
// and emails the user a link that includes the token.
//
// Security posture: always returns nil on "user not found" so an attacker
// can't probe which emails are registered.
func (c *Controller) RequestPasswordReset(req requests.UserForgotPasswordRequest) error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(req); err != nil {
		return err
	}

	user, err := c.DB.GetUserByEmail(strings.ToLower(strings.TrimSpace(req.Email)))
	if err != nil {
		// Swallow the error on purpose — caller treats this as success so
		// the response looks identical whether the email exists or not.
		slog.Info("password reset requested for unknown email", "email", req.Email)
		return nil
	}

	// OAuth accounts don't have a password we own; bounce these to the provider.
	if user.Provider != "" && user.Provider != "Wemob" {
		slog.Info("password reset blocked for OAuth user", "user_id", user.ID, "provider", user.Provider)
		return nil
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return fmt.Errorf("failed to generate reset token: %w", err)
	}

	// Invalidate any prior outstanding tokens so a single email link is
	// authoritative at any point in time.
	if err := c.DB.InvalidatePendingUserActionTokens(user.ID.String(), models.UserActionTokenKindPasswordReset); err != nil {
		slog.Warn("failed to invalidate prior password reset tokens", "error", err, "user_id", user.ID)
	}

	token := &models.UserActionToken{
		UserID:    user.ID,
		Kind:      models.UserActionTokenKindPasswordReset,
		Token:     tokenStr,
		ExpiredAt: time.Now().Add(passwordResetTokenTTL),
	}
	if _, err := c.DB.CreateUserActionToken(token); err != nil {
		return fmt.Errorf("failed to store reset token: %w", err)
	}

	frontendURL := strings.TrimRight(c.SecretManager.GetSecret("FRONTEND_URL"), "/")
	if frontendURL == "" {
		frontendURL = "https://dashboard.wemob.io"
	}
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", frontendURL, tokenStr)

	if _, err := c.EmailService.SendEmail(user, constants.BREVO_PASSWORD_RESET_EMAIL_TEMPLATE_ID, map[string]any{
		"name":      user.Name,
		"reset_url": resetURL,
		"expires_in_minutes": int(passwordResetTokenTTL / time.Minute),
	}); err != nil {
		slog.Error("failed to send password reset email", "error", err, "user_id", user.ID)
		// Still return nil — we don't want to leak delivery status either.
	}

	return nil
}

// ConsumePasswordReset accepts a reset token + new password, validates the
// token, and rotates the user's password. Returns a typed error for the
// three things that can go wrong so the view can map to a 400/410/422.
func (c *Controller) ConsumePasswordReset(req requests.UserResetPasswordRequest) error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(req); err != nil {
		return err
	}
	if req.Password != req.PasswordCheck {
		return errors.New("passwords don't match")
	}

	token, err := c.DB.GetUserActionToken(req.Token, models.UserActionTokenKindPasswordReset)
	if err != nil || token.User == nil {
		return ErrResetTokenInvalid
	}
	if token.IsConsumed() {
		return ErrResetTokenInvalid
	}
	if token.IsExpired() {
		return ErrResetTokenExpired
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user := token.User
	user.Password = hashedPassword
	user.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateUser(user); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	if err := c.DB.ConsumeUserActionToken(token); err != nil {
		slog.Warn("failed to mark reset token consumed", "error", err, "token_id", token.ID)
	}

	go c.CRMService.SendEvent(user, constants.BREVO_EVENT_PASSWORD_RESET, map[string]any{})

	return nil
}

// SendVerifyEmail issues a verification token and emails the user a link.
// Requires the caller to be authenticated — see the view handler.
func (c *Controller) SendVerifyEmail(user *models.User) error {
	if user.Verified {
		return errors.New("email already verified")
	}

	tokenStr, err := utils.RandomHex(32)
	if err != nil {
		return fmt.Errorf("failed to generate verify token: %w", err)
	}

	if err := c.DB.InvalidatePendingUserActionTokens(user.ID.String(), models.UserActionTokenKindVerifyEmail); err != nil {
		slog.Warn("failed to invalidate prior verify tokens", "error", err, "user_id", user.ID)
	}

	token := &models.UserActionToken{
		UserID:    user.ID,
		Kind:      models.UserActionTokenKindVerifyEmail,
		Token:     tokenStr,
		ExpiredAt: time.Now().Add(verifyEmailTokenTTL),
	}
	if _, err := c.DB.CreateUserActionToken(token); err != nil {
		return fmt.Errorf("failed to store verify token: %w", err)
	}

	frontendURL := strings.TrimRight(c.SecretManager.GetSecret("FRONTEND_URL"), "/")
	if frontendURL == "" {
		frontendURL = "https://dashboard.wemob.io"
	}
	verifyURL := fmt.Sprintf("%s/verify-email?token=%s", frontendURL, tokenStr)

	if _, err := c.EmailService.SendEmail(user, constants.BREVO_VERIFACTION_EMAIL_TEMPLATE_ID, map[string]any{
		"name":       user.Name,
		"verify_url": verifyURL,
	}); err != nil {
		return fmt.Errorf("failed to send verification email: %w", err)
	}

	return nil
}

// ConsumeVerifyEmail accepts a verification token and flips the user's
// Verified flag. Returns a typed error on invalid / expired / consumed.
func (c *Controller) ConsumeVerifyEmail(req requests.UserVerifyEmailRequest) (*models.User, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.Struct(req); err != nil {
		return nil, err
	}

	token, err := c.DB.GetUserActionToken(req.Token, models.UserActionTokenKindVerifyEmail)
	if err != nil || token.User == nil {
		return nil, ErrVerificationInvalid
	}
	if token.IsConsumed() {
		return nil, ErrVerificationInvalid
	}
	if token.IsExpired() {
		return nil, ErrVerificationInvalid
	}

	user := token.User
	if user.Verified {
		// Idempotent: if the link is valid and the user is already verified,
		// just consume the token and return success.
		_ = c.DB.ConsumeUserActionToken(token)
		return user, nil
	}

	user.Verified = true
	user.UpdatedAt = time.Now()
	if _, err := c.DB.UpdateUser(user); err != nil {
		return nil, fmt.Errorf("failed to mark user verified: %w", err)
	}

	if err := c.DB.ConsumeUserActionToken(token); err != nil {
		slog.Warn("failed to mark verify token consumed", "error", err, "token_id", token.ID)
	}

	if err := c.GrantInitialFreeCreditsIfNeeded(user); err != nil {
		slog.Error("failed to grant initial free credits on verify", "error", err, "user_id", user.ID)
	}

	go c.CRMService.SendEvent(user, constants.BREVO_EVENT_EMAIL_VERIFIED, map[string]any{})

	return user, nil
}
