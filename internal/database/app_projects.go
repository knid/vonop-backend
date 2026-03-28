package database

import (
	"context"
	"log/slog"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetAppProjectsByUserID(userID string) ([]models.AppProject, error) {
	var projects []models.AppProject
	err := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("user_id = ?", userID).Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectsByUserIDAndTypes(userID string, types []string) ([]models.AppProject, error) {
	var projects []models.AppProject
	q := db.DB.NewSelect().Model(&projects).
		ExcludeColumn("data_models", "prompt").
		Where("user_id = ?", userID)
	if len(types) > 0 {
		q = q.Where("project_type IN (?)", bun.In(types))
	}
	err := q.Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectByID(id string) (*models.AppProject, error) {
	if cached, ok := getProjectFromCache(id); ok {
		return cached, nil
	}
	var project models.AppProject
	err := db.DB.NewSelect().Model(&project).Where("id = ?", id).Scan(context.Background())
	if err == nil {
		setProjectInCache(id, &project)
	}
	return &project, err
}

func (db *Database) GetAppProjectBySlug(slug string) (*models.AppProject, error) {
	var project models.AppProject
	err := db.DB.NewSelect().Model(&project).Where("slug = ?", slug).Scan(context.Background())
	return &project, err
}

func (db *Database) CreateAppProject(p *models.AppProject) (*models.AppProject, error) {
	_, err := db.DB.NewInsert().Model(p).Exec(context.Background())
	return p, err
}

func (db *Database) UpdateAppProject(p *models.AppProject) (*models.AppProject, error) {
	// Guard against bloated JSONB columns
	if len(p.Theme) > 50*1024 {
		p.Theme = []byte("{}")
		slog.Warn("UpdateAppProject: theme too large, reset to empty", "project_id", p.ID, "size", len(p.Theme))
	}
	if len(p.DataModels) > 1<<20 {
		p.DataModels = []byte("[]")
		slog.Warn("UpdateAppProject: data_models too large, reset to empty", "project_id", p.ID, "size", len(p.DataModels))
	}
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(p.ID.String())
	}
	return p, err
}

func (db *Database) DeleteAppProject(p *models.AppProject) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(p.ID.String())
	}
	return err
}

func (db *Database) UpdateAppProjectSandboxID(projectID string, sandboxID string) error {
	_, err := db.DB.NewUpdate().Model((*models.AppProject)(nil)).
		Set("sandbox_id = ?", sandboxID).
		Where("id = ?", projectID).
		Exec(context.Background())
	if err == nil {
		db.InvalidateProjectCache(projectID)
	}
	return err
}

func (db *Database) CountAppProjectsByUserID(userID string) (int, error) {
	count, err := db.DB.NewSelect().Model((*models.AppProject)(nil)).Where("user_id = ?", userID).Count(context.Background())
	return count, err
}

func (db *Database) CountAppProjectsByUserIDSince(userID string, since time.Time) (int, error) {
	count, err := db.DB.NewSelect().Model((*models.AppProject)(nil)).
		Where("user_id = ?", userID).
		Where("created_at >= ?", since).
		Count(context.Background())
	return count, err
}
