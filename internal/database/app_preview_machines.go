package database

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetPreviewMachineByProjectID(projectID string) (*models.AppPreviewMachine, error) {
	var m models.AppPreviewMachine
	err := db.DB.NewSelect().Model(&m).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &m, err
}

func (db *Database) CreatePreviewMachine(m *models.AppPreviewMachine) (*models.AppPreviewMachine, error) {
	_, err := db.DB.NewInsert().Model(m).Exec(context.Background())
	return m, err
}

func (db *Database) UpdatePreviewMachine(m *models.AppPreviewMachine) error {
	m.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().Model(m).Where("id = ?", m.ID).Exec(context.Background())
	return err
}

func (db *Database) DeletePreviewMachineByProjectID(projectID string) error {
	_, err := db.DB.NewDelete().Model((*models.AppPreviewMachine)(nil)).Where("app_project_id = ?", projectID).Exec(context.Background())
	return err
}

// CountActivePreviewMachines returns the number of machines that are not stopped/destroyed.
func (db *Database) CountActivePreviewMachines() (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.AppPreviewMachine)(nil)).
		Where("status NOT IN (?)", "stopped").
		Count(context.Background())
	return count, err
}

// ListIdlePreviewMachines returns machines whose last_access_at is older than the given duration.
func (db *Database) ListIdlePreviewMachines(olderThan time.Duration) ([]models.AppPreviewMachine, error) {
	var machines []models.AppPreviewMachine
	cutoff := time.Now().Add(-olderThan)
	err := db.DB.NewSelect().
		Model(&machines).
		Where("last_access_at < ?", cutoff).
		Where("status != ?", "stopped").
		Scan(context.Background())
	return machines, err
}
