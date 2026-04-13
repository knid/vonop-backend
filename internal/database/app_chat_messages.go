package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// InsertChatMessage persists a new chat message (user or assistant).
func (db *Database) InsertChatMessage(m *models.AppChatMessage) (*models.AppChatMessage, error) {
	_, err := db.DB.NewInsert().Model(m).Returning("*").Exec(context.Background())
	return m, err
}

// UpdateChatMessage updates mutable fields on an existing chat message — used
// when the assistant turn is finalised at the end of a run (RunID, content,
// tool calls all become known at shutdown).
func (db *Database) UpdateChatMessage(m *models.AppChatMessage) error {
	_, err := db.DB.NewUpdate().
		Model(m).
		Set("run_id = ?", m.RunID).
		Set("content = ?", m.Content).
		Set("tool_calls = ?", m.ToolCalls).
		Where("id = ?", m.ID).
		Exec(context.Background())
	return err
}

// GetRecentChatMessages returns the N most recent messages for a project,
// oldest-first. Used to hydrate the LLM message window.
func (db *Database) GetRecentChatMessages(projectID uuid.UUID, limit int) ([]models.AppChatMessage, error) {
	var msgs []models.AppChatMessage
	err := db.DB.NewSelect().
		Model(&msgs).
		Where("app_project_id = ?", projectID).
		Order("created_at DESC").
		Limit(limit).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	// Reverse in place so the slice is oldest-first.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

// ClearChatMessages deletes all messages for a project. Called by the
// "clear conversation" endpoint.
func (db *Database) ClearChatMessages(projectID uuid.UUID) error {
	_, err := db.DB.NewDelete().
		Model((*models.AppChatMessage)(nil)).
		Where("app_project_id = ?", projectID).
		Exec(context.Background())
	return err
}
