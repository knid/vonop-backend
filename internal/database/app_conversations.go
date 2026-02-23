package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAppConversationByProjectID(projectID string) (*models.AppConversation, error) {
	var conv models.AppConversation
	err := db.DB.NewSelect().Model(&conv).Where("app_project_id = ?", projectID).Scan(context.Background())
	return &conv, err
}

func (db *Database) CreateAppConversation(c *models.AppConversation) (*models.AppConversation, error) {
	_, err := db.DB.NewInsert().Model(c).Returning("*").Exec(context.Background())
	return c, err
}

// GetOrCreateAppConversation returns the existing conversation for the project,
// or creates a new one if none exists.
func (db *Database) GetOrCreateAppConversation(c *models.AppConversation) (*models.AppConversation, error) {
	existing, err := db.GetAppConversationByProjectID(c.AppProjectID.String())
	if err == nil && existing.ID != uuid.Nil {
		return existing, nil
	}
	// No existing conversation — create a new one
	_, err = db.DB.NewInsert().Model(c).Returning("*").Exec(context.Background())
	return c, err
}

func (db *Database) UpdateAppConversation(c *models.AppConversation) (*models.AppConversation, error) {
	_, err := db.DB.NewUpdate().
		Model((*models.AppConversation)(nil)).
		Set("messages = ?::jsonb", string(c.Messages)).
		Set("updated_at = current_timestamp").
		Where("id = ?", c.ID).
		Exec(context.Background())
	return c, err
}

func (db *Database) DeleteAppConversation(projectID string) error {
	_, err := db.DB.NewDelete().
		Model((*models.AppConversation)(nil)).
		Where("app_project_id = ?", projectID).
		Exec(context.Background())
	return err
}
