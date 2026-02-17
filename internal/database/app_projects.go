package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetAppProjectsByUserID(userID string) ([]models.AppProject, error) {
	var projects []models.AppProject
	err := db.DB.NewSelect().Model(&projects).Where("user_id = ?", userID).Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectsByUserIDAndTypes(userID string, types []string) ([]models.AppProject, error) {
	var projects []models.AppProject
	q := db.DB.NewSelect().Model(&projects).Where("user_id = ?", userID)
	if len(types) > 0 {
		q = q.Where("project_type IN (?)", bun.In(types))
	}
	err := q.Order("created_at DESC").Scan(context.Background())
	return projects, err
}

func (db *Database) GetAppProjectByID(id string) (*models.AppProject, error) {
	var project models.AppProject
	err := db.DB.NewSelect().Model(&project).Where("id = ?", id).Scan(context.Background())
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
	_, err := db.DB.NewUpdate().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return p, err
}

func (db *Database) DeleteAppProject(p *models.AppProject) error {
	_, err := db.DB.NewDelete().Model(p).Where("id = ?", p.ID).Exec(context.Background())
	return err
}

func (db *Database) CountAppProjectsByUserID(userID string) (int, error) {
	count, err := db.DB.NewSelect().Model((*models.AppProject)(nil)).Where("user_id = ?", userID).Count(context.Background())
	return count, err
}
