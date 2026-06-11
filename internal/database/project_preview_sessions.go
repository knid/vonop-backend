package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateProjectPreviewSession(
	ctx context.Context,
	session *models.ProjectPreviewSession,
) (*models.ProjectPreviewSession, error) {
	_, err := db.DB.NewInsert().Model(session).Returning("*").Exec(ctx)
	return session, err
}

func (db *Database) GetProjectPreviewSessionBySlug(
	ctx context.Context,
	previewSlug string,
) (*models.ProjectPreviewSession, error) {
	var session models.ProjectPreviewSession
	err := db.DB.NewSelect().
		Model(&session).
		Where("preview_slug = ?", previewSlug).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func (db *Database) ListActiveProjectPreviewSessions(
	ctx context.Context,
	projectID uuid.UUID,
) ([]models.ProjectPreviewSession, error) {
	var sessions []models.ProjectPreviewSession
	err := db.DB.NewSelect().
		Model(&sessions).
		Where("app_project_id = ?", projectID).
		Where("status IN (?)", bun.In([]string{
			models.PreviewSessionStatusStarting,
			models.PreviewSessionStatusReady,
		})).
		Order("started_at DESC").
		Scan(ctx)
	return sessions, err
}

func (db *Database) MarkProjectPreviewSessionReady(
	ctx context.Context,
	id uuid.UUID,
	publicPreviewURL string,
	daytonaPreviewURL string,
	daytonaPreviewToken string,
) error {
	now := time.Now()
	_, err := db.DB.NewUpdate().
		Model((*models.ProjectPreviewSession)(nil)).
		Set("status = ?", models.PreviewSessionStatusReady).
		Set("sandbox_status = ?", "running").
		Set("public_preview_url = ?", publicPreviewURL).
		Set("daytona_preview_url = ?", daytonaPreviewURL).
		Set("daytona_preview_token = ?", daytonaPreviewToken).
		Set("ready_at = ?", now).
		Set("last_healthcheck_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

func (db *Database) MarkProjectPreviewSessionFailed(
	ctx context.Context,
	id uuid.UUID,
	reason string,
) error {
	_, err := db.DB.NewUpdate().
		Model((*models.ProjectPreviewSession)(nil)).
		Set("status = ?", models.PreviewSessionStatusFailed).
		Set("sandbox_status = ?", "failed").
		Set("metadata = jsonb_set(coalesce(metadata, '{}'::jsonb), '{failure_reason}', to_jsonb(?::text), true)", reason).
		Where("id = ?", id).
		Exec(ctx)
	return err
}

func (db *Database) TouchProjectPreviewSessionAccess(
	ctx context.Context,
	previewSlug string,
) error {
	_, err := db.DB.NewUpdate().
		Model((*models.ProjectPreviewSession)(nil)).
		Set("last_accessed_at = ?", time.Now()).
		Where("preview_slug = ?", previewSlug).
		Exec(ctx)
	return err
}

func (db *Database) ExpireProjectPreviewSessions(
	ctx context.Context,
	now time.Time,
) (int64, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.ProjectPreviewSession)(nil)).
		Set("status = ?", models.PreviewSessionStatusExpired).
		Set("sandbox_status = ?", "expired").
		Where("expires_at <= ?", now).
		Where("status IN (?)", bun.In([]string{
			models.PreviewSessionStatusStarting,
			models.PreviewSessionStatusReady,
		})).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
