package storepoll

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// ConnectionReader returns plaintext credential bytes for a connection.
// Callers (controller.ReadApplePrimaryP8 / controller.ReadGoogleSA) implement
// the dual-read CredStore-or-legacy fallback.
type ConnectionReader interface {
	ReadApplePrimaryP8(ctx context.Context, conn *models.AppleConnection) ([]byte, error)
	ReadGoogleSA(ctx context.Context, conn *models.GooglePlayConnection) ([]byte, error)
}

// Poller periodically queries App Store Connect + Google Play
// Publishing APIs to detect state transitions on in-flight
// deployments and writes corresponding deployment_events with
// actor_type = "store_api".
type Poller struct {
	DB     *database.Database
	Reader ConnectionReader
	// PollEvery controls the ticker cadence. Sensible range 5-15 minutes;
	// under 1 minute will rate-limit against Apple.
	PollEvery time.Duration
}

// Run blocks until ctx is cancelled, ticking every PollEvery.
// Non-fatal errors (bad credentials, store API hiccups) are logged
// and skipped; the loop carries on.
func (p *Poller) Run(ctx context.Context) {
	if p.PollEvery <= 0 {
		p.PollEvery = 10 * time.Minute
	}
	ticker := time.NewTicker(p.PollEvery)
	defer ticker.Stop()

	// Kick once immediately on startup so the timeline freshens on
	// the first load after a deploy of the backend.
	p.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	deployments, err := p.DB.ListInFlightDeployments(ctx)
	if err != nil {
		slog.Warn("storepoll: list in-flight deployments failed", "err", err)
		return
	}
	if len(deployments) == 0 {
		return
	}
	slog.Debug("storepoll: ticking", "deployments", len(deployments))

	for i := range deployments {
		d := deployments[i]
		// Pick the checker to run. Platforms are "ios" / "android";
		// a deployment targets exactly one store in practice (Pilot
		// and QuickShip both submit one at a time). If multi-platform
		// deployments appear in the future both branches below run.
		for _, platform := range d.Platforms {
			switch platform {
			case "ios":
				if d.AppleConnection == nil {
					continue
				}
				p8Bytes, err := p.Reader.ReadApplePrimaryP8(ctx, d.AppleConnection)
				if err != nil {
					slog.Warn("storepoll: apple read p8 failed", "deployment_id", d.ID, "err", err)
					continue
				}
				chk := AppleChecker{DB: p.DB}
				if err := chk.Check(ctx, d, p8Bytes); err != nil {
					slog.Warn("storepoll: apple check failed", "deployment_id", d.ID, "err", err)
				}
			case "android":
				if d.GooglePlayConnection == nil {
					continue
				}
				saBytes, err := p.Reader.ReadGoogleSA(ctx, d.GooglePlayConnection)
				if err != nil {
					slog.Warn("storepoll: play read service account failed", "deployment_id", d.ID, "err", err)
					continue
				}
				chk := PlayChecker{DB: p.DB}
				if err := chk.Check(ctx, d, saBytes); err != nil {
					slog.Warn("storepoll: play check failed", "deployment_id", d.ID, "err", err)
				}
			}
		}
	}
}
