package database

import (
	"context"
	"fmt"
	"time"

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
	if _, err := uuid.Parse(id); err != nil {
		return nil, fmt.Errorf("invalid build ID: %w", err)
	}
	var build models.Build
	err := db.DB.NewSelect().Model(&build).Where("id = ?", id).Scan(context.Background())

	return &build, err
}

func (db *Database) CreateProjectBuild(build *models.Build) (*models.Build, error) {
	_, err := db.DB.NewInsert().Model(build).Exec(context.Background())

	return build, err
}

func (db *Database) CountUserBuildsSince(userID string, since time.Time) (int, error) {
	count, err := db.DB.NewSelect().
		TableExpr("builds AS b").
		Join("JOIN projects AS p ON p.id = b.project_id").
		Where("p.user_id = ?", userID).
		Where("b.created_at >= ?", since).
		Where("b.deleted_at IS NULL").
		Where("p.deleted_at IS NULL").
		Count(context.Background())
	return count, err
}

// GetLastBuildByAppProject returns the highest-numbered Build for an
// AppProject. Used by BuildExpoMobile to compute the next build number
// without going through the v1 shadow Project.
func (db *Database) GetLastBuildByAppProject(appProjectID string) (*models.Build, error) {
	var build models.Build
	err := db.DB.NewSelect().
		Model(&build).
		Where("app_project_id = ?", appProjectID).
		Order("build_number DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &build, nil
}

// GetBuildsByAppProject returns all builds for an AppProject ordered newest
// first. Used by the agent's read_build_logs tool.
func (db *Database) GetBuildsByAppProject(appProjectID string) ([]models.Build, error) {
	var builds []models.Build
	err := db.DB.NewSelect().
		Model(&builds).
		Where("app_project_id = ?", appProjectID).
		Order("build_number DESC").
		Scan(context.Background())
	return builds, err
}

// GetLastSuccessfulBuildByAppProject returns the highest-numbered Build for
// an AppProject that has at least one AppBuildTask with status_text = "Built"
// and finished = true. Used by SubmitExpoMobile to find what to upload.
func (db *Database) GetLastSuccessfulBuildByAppProject(appProjectID string) (*models.Build, error) {
	var build models.Build
	err := db.DB.NewSelect().
		Model(&build).
		Where("build.app_project_id = ?", appProjectID).
		Where("EXISTS (SELECT 1 FROM app_build_tasks t WHERE t.build_id = build.id AND t.status_text = ? AND t.finished = ? AND t.deleted_at IS NULL)", "Built", true).
		Order("build.build_number DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &build, nil
}

