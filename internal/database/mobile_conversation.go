package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) CreateMobileConversation(conv *models.MobileConversation) (*models.MobileConversation, error) {
	_, err := db.DB.NewInsert().Model(conv).Returning("*").Exec(context.Background())
	return conv, err
}

func (db *Database) GetMobileConversationByProject(projectId string) (*models.MobileConversation, error) {
	var conv models.MobileConversation
	err := db.DB.NewSelect().Model(&conv).Where("project_id = ?", projectId).Scan(context.Background())
	return &conv, err
}

func (db *Database) GetOrCreateMobileConversation(conv *models.MobileConversation) (*models.MobileConversation, error) {
	existing, err := db.GetMobileConversationByProject(conv.ProjectID.String())
	if err == nil && existing.ID != uuid.Nil {
		return existing, nil
	}
	_, err = db.DB.NewInsert().Model(conv).Returning("*").Exec(context.Background())
	return conv, err
}

func (db *Database) UpdateMobileConversation(conv *models.MobileConversation) (*models.MobileConversation, error) {
	msg := string(conv.Messages)
	if msg == "" || msg == "null" || msg == `"null"` {
		msg = "[]"
	}
	if len(msg) == 0 || msg[0] != '[' {
		msg = "[]"
	}
	_, err := db.DB.NewUpdate().
		Model((*models.MobileConversation)(nil)).
		Set("messages = ?::jsonb", msg).
		Set("summary = ?", conv.Summary).
		Where("id = ?", conv.ID).
		Exec(context.Background())
	return conv, err
}

func (db *Database) DeleteMobileConversation(projectId string) error {
	_, err := db.DB.NewDelete().
		Model((*models.MobileConversation)(nil)).
		Where("project_id = ?", projectId).
		Exec(context.Background())
	return err
}
