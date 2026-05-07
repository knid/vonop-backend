package database

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

// BackfillBuildDefaults scans each AppProject and writes a
// settings.build_defaults object derived from the project's
// most-recent Build row. Idempotent: skips projects whose
// settings already have a build_defaults key.
//
// Run once at startup. Logs progress and per-project errors at Warn
// level so a single bad row doesn't stall the rest.
func BackfillBuildDefaults(ctx context.Context, db *bun.DB) error {
	var projects []models.AppProject
	if err := db.NewSelect().Model(&projects).Scan(ctx); err != nil {
		return err
	}

	filled := 0
	skipped := 0
	for _, p := range projects {
		if p.Settings != nil {
			if _, hasDefaults := p.Settings["build_defaults"]; hasDefaults {
				skipped++
				continue
			}
		}
		// Find the latest build for this AppProject via the direct
		// app_project_id FK. The v1 shadow Project indirection was
		// removed 2026-05-07.
		var latest models.Build
		err := db.NewSelect().
			Model(&latest).
			Where("app_project_id = ?", p.ID.String()).
			Order("created_at DESC").
			Limit(1).
			Scan(ctx)
		if err != nil {
			// No builds for this project yet — nothing to backfill.
			skipped++
			continue
		}

		defaults := map[string]interface{}{
			"app_name":           latest.Title,
			"icon_s3_key":        latest.Icon,
			"splash_color":       latest.SplashScreenColor,
			"navbar_style":       latest.NavBarStyle,
			"remove_wemob_brand": latest.RemoveWemobBranding,
		}
		if p.Settings == nil {
			p.Settings = map[string]interface{}{}
		}
		p.Settings["build_defaults"] = defaults

		if _, err := db.NewUpdate().
			Model(&p).
			Column("settings").
			Where("id = ?", p.ID).
			Exec(ctx); err != nil {
			slog.Warn("backfill build_defaults: update failed", "project_id", p.ID, "err", err)
			continue
		}
		filled++
	}
	slog.Info("backfill build_defaults complete", "filled", filled, "skipped", skipped)
	return nil
}
