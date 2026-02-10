package database

import (
	"context"

	"github.com/knid/wemob/internal/models"
)

// WebsiteConversation

func (db *Database) GetConversationByWebsiteId(websiteId string) (*models.WebsiteConversation, error) {
	var conversation models.WebsiteConversation
	err := db.DB.NewSelect().Model(&conversation).Where("website_id = ?", websiteId).Scan(context.Background())
	return &conversation, err
}

func (db *Database) CreateConversation(c *models.WebsiteConversation) (*models.WebsiteConversation, error) {
	_, err := db.DB.NewInsert().Model(c).Returning("*").Exec(context.Background())
	return c, err
}

func (db *Database) UpdateConversation(c *models.WebsiteConversation) (*models.WebsiteConversation, error) {
	_, err := db.DB.NewUpdate().
		Model((*models.WebsiteConversation)(nil)).
		Set("messages = ?", c.Messages).
		Set("updated_at = current_timestamp").
		Where("id = ?", c.ID).
		Exec(context.Background())
	return c, err
}
