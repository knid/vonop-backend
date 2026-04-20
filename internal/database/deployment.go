package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) GetDeployments(user *models.User) ([]models.Deployment, error) {
	var deployments []models.Deployment
	err := db.DB.NewSelect().Model(&deployments).
		Relation("Build").
		Relation("AppleConnection").
		Relation("GooglePlayConnection").
		Where("deployment.user_id = ?", user.ID).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return deployments, nil
}

func (db *Database) GetDeploymentByID(deploymentID string) (*models.Deployment, error) {
	var deployment models.Deployment
	err := db.DB.NewSelect().Model(&deployment).Where("id = ?", deploymentID).Scan(context.Background(), &deployment)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (db *Database) GetDeploymentWithRelations(deploymentID string) (*models.Deployment, error) {
	var deployment models.Deployment
	err := db.DB.NewSelect().Model(&deployment).
		Relation("User").
		Where("deployment.id = ?", deploymentID).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (db *Database) CreateDeployment(deployment *models.Deployment) (*models.Deployment, error) {
	_, err := db.DB.NewInsert().Model(deployment).Exec(context.Background())
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

func (db *Database) UpdateDeployment(deployment *models.Deployment) (*models.Deployment, error) {
	_, err := db.DB.NewUpdate().Model(deployment).Where("id = ?", deployment.ID).Exec(context.Background())
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

// ListInFlightDeployments returns deployments the poller should check:
// anything submitted, reviewing, uploaded, or deploying, submitted within
// the past 30 days. Terminal states (deployed, rejected, failed) are
// skipped so we don't keep hitting the stores for finished work.
func (d *Database) ListInFlightDeployments(ctx context.Context) ([]models.Deployment, error) {
	var deployments []models.Deployment
	since := time.Now().Add(-30 * 24 * time.Hour)
	if err := d.DB.NewSelect().
		Model(&deployments).
		Relation("Project").
		Relation("AppleConnection").
		Relation("GooglePlayConnection").
		Where("deployment.created_at > ?", since).
		Where("LOWER(deployment.status) IN (?)", bun.In([]string{"submitted", "queued", "reviewing", "uploaded_to_store", "deploying"})).
		Scan(ctx); err != nil {
		return nil, err
	}
	return deployments, nil
}

// ListPendingConnectionDeployments returns all deployments awaiting a
// user-side connection. Includes User relation so the nudge runner
// can address a Brevo email to them.
func (d *Database) ListPendingConnectionDeployments(ctx context.Context) ([]models.Deployment, error) {
	var deployments []models.Deployment
	if err := d.DB.NewSelect().
		Model(&deployments).
		Relation("User").
		Where("deployment.status = ?", "pending_connection").
		Scan(ctx); err != nil {
		return nil, err
	}
	return deployments, nil
}

