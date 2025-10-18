package database

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetProjectBuilds(project *models.Project) ([]models.Build, error) {
	var builds []models.Build
	err := db.DB.NewSelect().Model(&builds).Where("project_id = ?", project.ID.String()).Scan(context.Background())

	return builds, err
}

func (db *Database) GetBuildsByUserId(userId string) ([]models.Build, error) {
	var builds []models.Build
	user, err := db.GetUserById(userId)
	if err != nil {
		return nil, err
	}
	projects, err := db.GetProjects(user)
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		projectBuilds, err := db.GetProjectBuilds(&project)
		if err != nil {
			return nil, err
		}
		builds = append(builds, projectBuilds...)
	}

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

func (db *Database) SetReleasedBuild(build *models.Build) (*models.Build, error) {
	var activeBuild models.Build

	err := db.DB.NewSelect().Model(&activeBuild).Where("active = true").Scan(context.Background())
	if err != nil {
		return nil, err
	}

	tx, err := db.DB.BeginTx(context.Background(), &sql.TxOptions{})
	if err != nil {
		return nil, err
	}

	activeBuild.Active = false
	_, err = tx.NewUpdate().Model(&activeBuild).Column("active").WherePK().Exec(context.Background())
	if err != nil {
		activeBuild.Active = true
		tx.Rollback()
		return nil, err
	}

	build.Active = true
	_, err = tx.NewUpdate().Model(build).WherePK().Column("active").Exec(context.Background())
	if err != nil {
		build.Active = false
		tx.Rollback()
		return nil, err
	}

	tx.Commit()

	return build, nil
}
