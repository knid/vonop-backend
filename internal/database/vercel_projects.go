package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetVercelProjectByAppProjectID(appProjectID string) (*models.VercelProject, error) {
	var vp models.VercelProject
	err := db.DB.NewSelect().Model(&vp).Where("app_project_id = ?", appProjectID).Scan(context.Background())
	return &vp, err
}

func (db *Database) CreateVercelProject(vp *models.VercelProject) (*models.VercelProject, error) {
	_, err := db.DB.NewInsert().Model(vp).Exec(context.Background())
	return vp, err
}

func (db *Database) UpdateVercelProject(vp *models.VercelProject) (*models.VercelProject, error) {
	_, err := db.DB.NewUpdate().Model(vp).Where("id = ?", vp.ID).Exec(context.Background())
	return vp, err
}
