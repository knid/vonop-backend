package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetFlutterBuildsByProjectID(projectID string) ([]models.FlutterBuild, error) {
	var builds []models.FlutterBuild
	err := db.DB.NewSelect().Model(&builds).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return builds, err
}

func (db *Database) GetFlutterBuildByID(id string) (*models.FlutterBuild, error) {
	var build models.FlutterBuild
	err := db.DB.NewSelect().Model(&build).Where("id = ?", id).Scan(context.Background())
	return &build, err
}

func (db *Database) CreateFlutterBuild(b *models.FlutterBuild) (*models.FlutterBuild, error) {
	_, err := db.DB.NewInsert().Model(b).Exec(context.Background())
	return b, err
}

func (db *Database) UpdateFlutterBuild(b *models.FlutterBuild) (*models.FlutterBuild, error) {
	_, err := db.DB.NewUpdate().Model(b).Where("id = ?", b.ID).Exec(context.Background())
	return b, err
}
