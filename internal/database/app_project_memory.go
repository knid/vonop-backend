package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppProjectMemory(projectID string) (*models.AppProjectMemory, error) {
	var mem models.AppProjectMemory
	err := db.DB.NewSelect().Model(&mem).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &mem, err
}

func (db *Database) GetOrCreateAppProjectMemory(projectID uuid.UUID) (*models.AppProjectMemory, error) {
	existing, err := db.GetAppProjectMemory(projectID.String())
	if err == nil && existing.ID != uuid.Nil {
		return existing, nil
	}
	mem := &models.AppProjectMemory{
		AppProjectID:       projectID,
		DesignPreferences:  []byte("[]"),
		RejectedApproaches: []byte("[]"),
		TechnicalDecisions: []byte("[]"),
		UserPreferences:    []byte("[]"),
	}
	_, err = db.DB.NewInsert().Model(mem).Returning("*").Exec(context.Background())
	return mem, err
}

func (db *Database) UpdateAppProjectMemory(mem *models.AppProjectMemory) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AppProjectMemory)(nil)).
		Set("design_preferences = ?::jsonb", string(mem.DesignPreferences)).
		Set("rejected_approaches = ?::jsonb", string(mem.RejectedApproaches)).
		Set("technical_decisions = ?::jsonb", string(mem.TechnicalDecisions)).
		Set("user_preferences = ?::jsonb", string(mem.UserPreferences)).
		Set("updated_at = current_timestamp").
		Where("id = ?", mem.ID).
		Exec(context.Background())
	return err
}
