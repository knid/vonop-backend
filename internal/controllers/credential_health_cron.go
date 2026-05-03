package controllers

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/storeauth"
)

const (
	credExpiryWarnDays  = 30
	credExpiryAlertDays = 7
)

// RunCredentialHealthCheck is the daily cron entry point.
//
// For each Apple + Google connection:
//  1. Re-validate (mint a token, hit the live API) and persist the result.
//  2. Fetch certificates / profiles (Apple) and check expiry. Notify the org
//     if any expire within 30/7-day windows.
//
// Errors per-connection are logged but don't abort the run — one bad
// credential shouldn't stop the others.
func (c *Controller) RunCredentialHealthCheck(ctx context.Context) error {
	if err := c.runAppleCredentialHealth(ctx); err != nil {
		slog.Warn("apple credential health failed", "err", err)
	}
	if err := c.runGoogleCredentialHealth(ctx); err != nil {
		slog.Warn("google credential health failed", "err", err)
	}
	return nil
}

const credHealthConcurrency = 10

func (c *Controller) runAppleCredentialHealth(ctx context.Context) error {
	conns, err := c.DB.ListAllAppleConnections(ctx)
	if err != nil {
		return err
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(credHealthConcurrency)

	for _, conn := range conns {
		conn := conn // capture loop variable
		eg.Go(func() error {
			// Load the p8 key from S3.
			p8Bytes, err := c.UserApiKeysStorage.ReadObjectBytes(ctx, conn.P8KeyPath)
			if err != nil {
				slog.Warn("credential_health: read p8 failed", "conn", conn.ID, "err", err)
				persistConnectionValidation(ctx, c.DB, "apple", conn.ID, "invalid", "p8 unreadable: "+err.Error(), 0)
				return nil // don't abort the errgroup; log and continue
			}

			in := storeauth.AppleConnectionInput{
				KeyID:    conn.KeyID,
				IssuerID: conn.IssuerID,
				P8PEM:    p8Bytes,
			}

			// Re-validate by fetching certs (proves JWT + ASC connectivity).
			certs, err := storeauth.FetchCertificates(ctx, in)
			if err != nil {
				slog.Warn("credential_health: apple fetch certs failed", "conn", conn.ID, "err", err)
				persistConnectionValidation(ctx, c.DB, "apple", conn.ID, "invalid", "asc unreachable: "+err.Error(), 0)
				return nil
			}
			persistConnectionValidation(ctx, c.DB, "apple", conn.ID, "valid", "ok", len(certs))

			// Check cert expiry windows.
			for _, cert := range certs {
				days := time.Until(cert.ExpiresAt).Hours() / 24
				if days > credExpiryWarnDays {
					continue
				}
				level := "warn"
				if days <= credExpiryAlertDays {
					level = "alert"
				}
				c.notifyAppleCertExpiring(ctx, conn, cert, level)
			}
			return nil
		})
	}
	return eg.Wait()
}

func (c *Controller) runGoogleCredentialHealth(ctx context.Context) error {
	conns, err := c.DB.ListAllGooglePlayConnections(ctx)
	if err != nil {
		return err
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(credHealthConcurrency)

	for _, conn := range conns {
		conn := conn // capture loop variable
		eg.Go(func() error {
			saBytes, err := c.UserApiKeysStorage.ReadObjectBytes(ctx, conn.ServiceAccountPath)
			if err != nil {
				slog.Warn("credential_health: read sa json failed", "conn", conn.ID, "err", err)
				persistConnectionValidation(ctx, c.DB, "google_play", conn.ID, "invalid", "sa json unreadable", 0)
				return nil
			}
			sa, err := storeauth.ParseServiceAccount(saBytes)
			if err != nil {
				slog.Warn("credential_health: parse sa failed", "conn", conn.ID, "err", err)
				persistConnectionValidation(ctx, c.DB, "google_play", conn.ID, "invalid", "sa malformed", 0)
				return nil
			}
			// Probe connectivity: mint a token. This exercises the key + token endpoint.
			if _, err := storeauth.GoogleToken(ctx, sa); err != nil {
				slog.Warn("credential_health: google token mint failed", "conn", conn.ID, "err", err)
				persistConnectionValidation(ctx, c.DB, "google_play", conn.ID, "invalid", "token: "+err.Error(), 0)
				return nil
			}
			persistConnectionValidation(ctx, c.DB, "google_play", conn.ID, "valid", "ok", 0)
			// Note: service-account JSON key expiry can be checked via the IAM
			// serviceAccountKeys.list API, but that requires the IAM API scope.
			// Deferred to P1.1.
			return nil
		})
	}
	return eg.Wait()
}

// notifyAppleCertExpiring emits a structured log warning when an Apple
// distribution or development certificate is expiring soon.
//
// Two levels:
//   - "warn"  — expires within credExpiryWarnDays  (30 days)
//   - "alert" — expires within credExpiryAlertDays (7 days)
//
// TODO(P1.1): hook into a NotificationsService.Send(...) when org-targeted
// in-app + email notifications are wired up. Logging is the signal for now.
func (c *Controller) notifyAppleCertExpiring(ctx context.Context, conn *models.AppleConnection, cert storeauth.AppleCertificate, level string) {
	slog.Warn("apple cert expiring soon",
		"level", level,
		"org", conn.OrganizationID,
		"conn", conn.ID,
		"cert_id", cert.ID,
		"cert_type", cert.CertificateType,
		"display_name", cert.DisplayName,
		"expires_at", cert.ExpiresAt,
		"days_remaining", time.Until(cert.ExpiresAt).Hours()/24,
	)
	_ = ctx
}
