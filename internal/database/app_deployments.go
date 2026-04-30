package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetAppDeploymentsByProjectID(projectID string) ([]models.AppDeployment, error) {
	var deployments []models.AppDeployment
	err := db.DB.NewSelect().Model(&deployments).Where("app_project_id = ?", projectID).Order("created_at DESC").Scan(context.Background())
	return deployments, err
}

func (db *Database) GetAppDeploymentByID(id string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).Where("id = ?", id).Scan(context.Background())
	return &deployment, err
}

func (db *Database) GetLatestAppDeployment(projectID string, platform string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).Where("app_project_id = ? AND platform = ?", projectID, platform).Order("created_at DESC").Limit(1).Scan(context.Background())
	return &deployment, err
}

func (db *Database) CreateAppDeployment(d *models.AppDeployment) (*models.AppDeployment, error) {
	_, err := db.DB.NewInsert().Model(d).Exec(context.Background())
	return d, err
}

func (db *Database) GetBuildingAppDeployment(projectID string) (*models.AppDeployment, error) {
	var deployment models.AppDeployment
	err := db.DB.NewSelect().Model(&deployment).
		Where("app_project_id = ?", projectID).
		Where("platform = ?", "web").
		Where("status IN (?)", bun.In([]string{"queued", "building", "deploying"})).
		Order("created_at DESC").Limit(1).Scan(context.Background())
	return &deployment, err
}

func (db *Database) UpdateAppDeployment(d *models.AppDeployment) (*models.AppDeployment, error) {
	_, err := db.DB.NewUpdate().Model(d).Where("id = ?", d.ID).Exec(context.Background())
	return d, err
}

// CountAppDeploymentsByStatusSince counts deployments matching `status` whose
// updated_at is more recent than `since`. Used by the health endpoint to
// report deploy success/failure rates over a recent window.
func (db *Database) CountAppDeploymentsByStatusSince(ctx context.Context, status string, since time.Time) (int, error) {
	return db.DB.NewSelect().Model((*models.AppDeployment)(nil)).
		Where("status = ?", status).
		Where("updated_at > ?", since).
		Count(ctx)
}

// IsAppAlreadyDeployed returns true if the given app project has any deployment with status "deployed".
func (db *Database) IsAppAlreadyDeployed(appProjectID string) (bool, error) {
	count, err := db.DB.NewSelect().
		TableExpr("app_deployments").
		Where("app_project_id = ?", appProjectID).
		Where("platform = 'web'").
		Where("status = 'deployed'").
		Count(context.Background())
	return count > 0, err
}

// ReapStuckAppDeployments flips any deployment row that's been sitting in an
// in-progress state ('queued', 'building', 'deploying') without a status
// update for longer than `staleAfter` to 'failed'. Returns the number of rows
// affected. Last-line defense for cases where the worker died mid-task or the
// asynq retry budget was exhausted without the handler getting a chance to
// write the failed state itself.
func (db *Database) ReapStuckAppDeployments(ctx context.Context, staleAfter time.Duration, reason string) (int, error) {
	res, err := db.DB.NewUpdate().Model((*models.AppDeployment)(nil)).
		Set("status = ?", "failed").
		Set("error_message = ?", reason).
		Set("updated_at = ?", time.Now()).
		Where("status IN (?)", bun.In([]string{"queued", "building", "deploying"})).
		Where("updated_at < ?", time.Now().Add(-staleAfter)).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
