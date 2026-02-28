package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetProjectBuilds(project *models.Project) ([]models.Build, error) {
	var builds []models.Build
	err := db.DB.NewSelect().Model(&builds).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return builds, err
}

func (db *Database) GetLastProjectBuild(project *models.Project) (*models.Build, error) {
	var activeBuild models.Build
	err := db.DB.NewSelect().Model(&activeBuild).Where("project_id = ?", project.ID.String()).Order("created_at DESC").Limit(1).Scan(context.Background())

	return &activeBuild, err
}

func (db *Database) GetProjectBuildById(id string) (*models.Build, error) {
	var build models.Build
	build.ID = uuid.MustParse(id)
	err := db.DB.NewSelect().Model(&build).Where("id = ?", id).Scan(context.Background())

	return &build, err
}

func (db *Database) CreateProjectBuild(build *models.Build) (*models.Build, error) {
	_, err := db.DB.NewInsert().Model(build).Exec(context.Background())

	return build, err
}

