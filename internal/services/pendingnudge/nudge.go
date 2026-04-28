package pendingnudge

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	crmServices "github.com/knid/wemob/internal/services/crm"
)

// Runner periodically scans pending_connection deployments and fires
// email nudges via the CRM. Up to 3 nudges per deployment, spaced 2
// days apart.
type Runner struct {
	DB    *database.Database
	CRM   crmServices.CRMService
	Every time.Duration // Default 6h when zero.
	// SpacingDays sets the gap between consecutive nudges. Default 2.
	SpacingDays int
	// MaxNudges caps the total per deployment. Default 3.
	MaxNudges int
}

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	if r.Every <= 0 {
		r.Every = 6 * time.Hour
	}
	if r.SpacingDays <= 0 {
		r.SpacingDays = 2
	}
	if r.MaxNudges <= 0 {
		r.MaxNudges = 3
	}
	ticker := time.NewTicker(r.Every)
	defer ticker.Stop()

	r.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	deployments, err := r.DB.ListPendingConnectionDeployments(ctx)
	if err != nil {
		slog.Warn("pendingnudge: list deployments failed", "err", err)
		return
	}
	if len(deployments) == 0 {
		return
	}
	slog.Debug("pendingnudge: ticking", "deployments", len(deployments))

	now := time.Now()
	for i := range deployments {
		d := deployments[i]
		if d.User == nil {
			slog.Warn("pendingnudge: deployment missing user relation", "deployment_id", d.ID)
			continue
		}

		nudgeCount, err := database.CountEventsOfType(ctx, r.DB.DB, d.ID, models.DeploymentEventNudgeSent)
		if err != nil {
			slog.Warn("pendingnudge: count events failed", "deployment_id", d.ID, "err", err)
			continue
		}
		if nudgeCount >= r.MaxNudges {
			continue
		}

		// Nudge N is due once (N * SpacingDays) have passed since the deployment
		// was created. This spaces nudges by SpacingDays regardless of when the
		// prior tick observed them.
		dueAt := d.CreatedAt.Add(time.Duration(nudgeCount+1) * time.Duration(r.SpacingDays) * 24 * time.Hour)
		if now.Before(dueAt) {
			continue
		}

		platform := "ios"
		for _, p := range d.Platforms {
			if p == "android" {
				platform = "android"
				break
			}
		}
		attrs := map[string]any{
			"deployment_id": d.ID.String(),
			"project_id":    d.ProjectID.String(),
			"platform":      platform,
			"attempt":       nudgeCount + 1,
			"max_attempts":  r.MaxNudges,
			"submitted_at":  d.CreatedAt.UTC().Format(time.RFC3339),
			"switch_url":    "/ship/" + d.ProjectID.String() + "#app-store",
		}
		if err := r.CRM.SendEvent(d.User, constants.EVENT_PENDING_CONNECTION_NUDGE, attrs); err != nil {
			slog.Warn("pendingnudge: brevo send failed", "deployment_id", d.ID, "err", err)
			continue
		}

		if err := database.AppendDeploymentEvent(ctx, r.DB.DB, d.ID,
			models.DeploymentEventNudgeSent,
			models.DeploymentEventActorSystem, nil,
			map[string]interface{}{"attempt": nudgeCount + 1, "platform": platform}); err != nil {
			slog.Warn("pendingnudge: append event failed", "deployment_id", d.ID, "err", err)
			continue
		}
		slog.Info("pendingnudge: sent",
			"deployment_id", d.ID,
			"attempt", nudgeCount+1,
			"platform", platform,
			"user_id", d.UserID)
	}
}
