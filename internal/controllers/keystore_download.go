package controllers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
)

const (
	keystoreOTPTTL    = 10 * time.Minute
	keystoreURLTTL    = 5 * time.Minute
	keystoreOTPLength = 6
)

// ErrKeystoreOTPRequired is returned when the user calls Redeem before Request.
var ErrKeystoreOTPRequired = errors.New("keystore download: no pending OTP for this user/project")

// ErrKeystoreOTPInvalid is returned when the OTP doesn't match or is expired.
var ErrKeystoreOTPInvalid = errors.New("keystore download: invalid or expired OTP")

// RequestKeystoreDownload starts the download flow:
//  1. Verifies the requesting user is an org admin or owner (P0.4 — was
//     previously any member, which let agency contractors with view access
//     exfiltrate signing material).
//  2. Invalidates any existing keystore-download OTPs for this user.
//  3. Mints a new 6-digit OTP and stores it as a UserActionToken.
//  4. Emails the OTP to the user.
//
// The token's stored value is "{projectID}:{otp}" so we can validate both
// in Redeem without an extra column.
func (c *Controller) RequestKeystoreDownload(ctx context.Context, requestingUser *models.User, orgIDStr, projectIDStr string) error {
	if _, err := c.RequireOrgAdminOrOwner(requestingUser, orgIDStr); err != nil {
		return err
	}

	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return errors.New("invalid project id")
	}
	project, err := c.DB.GetAppProjectByID(projectUUID.String())
	if err != nil {
		return err
	}
	if project == nil || project.OrganizationID == uuid.Nil {
		return errors.New("project not found in org")
	}

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return errors.New("invalid organization id")
	}
	if project.OrganizationID != orgID {
		return errors.New("project not found in org")
	}

	// Invalidate any pending OTPs for this kind so only one is live at a time.
	if err := c.DB.InvalidatePendingUserActionTokens(requestingUser.ID.String(), models.UserActionTokenKindKeystoreDownload); err != nil {
		return fmt.Errorf("invalidate pending tokens: %w", err)
	}

	otp, err := generateNumericOTP(keystoreOTPLength)
	if err != nil {
		return fmt.Errorf("generate otp: %w", err)
	}

	// Bind the OTP to the user and project: token value = "{userID}:{projectID}:{otp}".
	// Including the user ID ensures two users who happen to generate the same 6-digit
	// OTP at the same time for the same project cannot redeem each other's tokens.
	tokenValue := requestingUser.ID.String() + ":" + projectIDStr + ":" + otp
	token := &models.UserActionToken{
		UserID:    requestingUser.ID,
		Kind:      models.UserActionTokenKindKeystoreDownload,
		Token:     tokenValue,
		ExpiredAt: time.Now().Add(keystoreOTPTTL),
	}
	if _, err := c.DB.CreateUserActionToken(token); err != nil {
		return fmt.Errorf("store otp token: %w", err)
	}

	// Email the OTP — mirrors the password-reset send pattern.
	go c.EventService.SendEvent(requestingUser, constants.EVENT_KEYSTORE_DOWNLOAD_OTP, map[string]any{
		"name":               requestingUser.Name,
		"project_name":       project.Name,
		"otp":                otp,
		"expires_in_minutes": int(keystoreOTPTTL / time.Minute),
	})

	return nil
}

// RedeemKeystoreDownload validates the OTP and returns a presigned S3 URL
// for the keystore. Returns ErrKeystoreOTPRequired if no token exists,
// ErrKeystoreOTPInvalid if the OTP doesn't match or is expired/consumed.
//
// Re-checks admin/owner role independently (P0.4 — defence in depth: even if
// an OTP somehow leaks to a non-admin, redeem must still verify the caller's
// role at consume time, since role demotions between Request and Redeem
// should revoke access immediately).
func (c *Controller) RedeemKeystoreDownload(ctx context.Context, requestingUser *models.User, orgIDStr, projectIDStr, otp string) (string, error) {
	if _, err := c.RequireOrgAdminOrOwner(requestingUser, orgIDStr); err != nil {
		return "", err
	}

	projectUUID, err := uuid.Parse(projectIDStr)
	if err != nil {
		return "", errors.New("invalid project id")
	}
	project, err := c.DB.GetAppProjectByID(projectUUID.String())
	if err != nil {
		return "", err
	}
	if project == nil || project.OrganizationID == uuid.Nil {
		return "", errors.New("project not found in org")
	}

	orgID, err := uuid.Parse(orgIDStr)
	if err != nil {
		return "", errors.New("invalid organization id")
	}
	if project.OrganizationID != orgID {
		return "", errors.New("project not found in org")
	}

	tokenValue := requestingUser.ID.String() + ":" + projectIDStr + ":" + strings.TrimSpace(otp)
	token, err := c.DB.GetUserActionToken(tokenValue, models.UserActionTokenKindKeystoreDownload)
	if err != nil || token == nil || token.ID == uuid.Nil {
		return "", ErrKeystoreOTPInvalid
	}
	if token.UserID != requestingUser.ID {
		// Same OTP guessed by a different user — treat as invalid (not "required").
		return "", ErrKeystoreOTPInvalid
	}
	if token.IsConsumed() {
		return "", ErrKeystoreOTPInvalid
	}
	if token.IsExpired() {
		return "", ErrKeystoreOTPInvalid
	}

	if err := c.DB.ConsumeUserActionToken(token); err != nil {
		return "", fmt.Errorf("consume otp: %w", err)
	}

	jksBytes, propsBytes, err := c.readAndroidKeystoreAndProperties(ctx, projectUUID, projectIDStr)
	if err != nil {
		return "", err
	}

	// Bundle both files into a single in-memory zip so the frontend only needs
	// one download and the password is never returned in plaintext over the API.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	if jw, err := zw.Create("upload-keystore.jks"); err != nil {
		return "", fmt.Errorf("create zip entry for jks: %w", err)
	} else if _, err = jw.Write(jksBytes); err != nil {
		return "", fmt.Errorf("write jks into zip: %w", err)
	}
	if pw, err := zw.Create("key.properties"); err != nil {
		return "", fmt.Errorf("create zip entry for key.properties: %w", err)
	} else if _, err = pw.Write(propsBytes); err != nil {
		return "", fmt.Errorf("write key.properties into zip: %w", err)
	}
	if err := zw.Close(); err != nil {
		return "", fmt.Errorf("close zip: %w", err)
	}

	// Upload the zip to a one-shot key in the assets bucket, then presign it.
	zipKey := fmt.Sprintf("keystore-bundles/%s/%s.zip", projectIDStr, uuid.New().String())
	if err := c.AssetsStorage.PutBytes(ctx, zipKey, buf.Bytes(), "application/zip"); err != nil {
		return "", fmt.Errorf("upload keystore bundle: %w", err)
	}
	presigned, err := c.AssetsStorage.GetObject(ctx, zipKey, int64(keystoreURLTTL/time.Second))
	if err != nil {
		return "", fmt.Errorf("presign keystore bundle: %w", err)
	}
	return presigned.URL, nil
}

// generateNumericOTP returns a zero-padded numeric string of the requested
// length. Uses crypto/rand for unpredictability.
func generateNumericOTP(length int) (string, error) {
	if length <= 0 || length > 12 {
		return "", errors.New("invalid otp length")
	}
	max := big.NewInt(1)
	for i := 0; i < length; i++ {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n), nil
}
