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
// Additionally renews per-project AppleSigningAssets (P1 #14) when cert /
// profile is < 30 days from expiry — calls EnsureAppleSigningAssets which
// re-mints both via ASC.
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
	if err := c.runAppleSigningRenewal(ctx); err != nil {
		slog.Warn("apple signing renewal failed", "err", err)
	}
	return nil
}

// runAppleSigningRenewal walks every AppleSigningAssets row that's within
// 30 days of expiry and calls EnsureAppleSigningAssets on it — which
// re-mints the cert + profile via ASC and upserts the row. Failures are
// logged per-project; one bad ASC connection doesn't abort the rest.
func (c *Controller) runAppleSigningRenewal(ctx context.Context) error {
	rows, err := c.DB.ListExpiringAppleSigningAssets(ctx, credExpiryWarnDays)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(credHealthConcurrency)

	for _, row := range rows {
		row := row // capture loop variable
		eg.Go(func() error {
			project, err := c.DB.GetAppProjectByID(row.AppProjectID.String())
			if err != nil || project == nil {
				slog.Warn("apple signing renewal: project lookup", "project_id", row.AppProjectID, "err", err)
				return nil
			}
			conn, err := c.DB.GetAppleConnectionById(row.AppleConnectionID.String())
			if err != nil || conn == nil {
				slog.Warn("apple signing renewal: connection lookup", "conn_id", row.AppleConnectionID, "err", err)
				return nil
			}
			if _, err := c.EnsureAppleSigningAssets(ctx, project, conn); err != nil {
				slog.Warn("apple signing renewal: ensure failed",
					"project_id", row.AppProjectID,
					"conn_id", row.AppleConnectionID,
					"err", err)
				return nil
			}
			slog.Info("apple signing renewed",
				"project_id", row.AppProjectID,
				"old_cert_expires", row.CertificateExpiresAt,
				"old_profile_expires", row.ProfileExpiresAt)
			return nil
		})
	}
	return eg.Wait()
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
			p8Bytes, err := c.ReadApplePrimaryP8(ctx, conn)
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
			saBytes, err := c.ReadGoogleSA(ctx, conn)
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
