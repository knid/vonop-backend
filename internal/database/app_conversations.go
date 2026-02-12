package database

import (
	"context"

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
