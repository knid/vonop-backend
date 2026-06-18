package database

import (
	"context"
	"sort"

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

// GetAgentHistoryMessages returns the union of recent transcript messages and
// recent tool-bearing assistant messages, oldest-first. The two windows are
// loaded independently so long text-only conversations do not hide the latest
// tool usage history from the agent prompt.
func (db *Database) GetAgentHistoryMessages(projectID uuid.UUID, transcriptLimit, toolMessageLimit int) ([]models.AppChatMessage, error) {
	byID := map[uuid.UUID]models.AppChatMessage{}
	if transcriptLimit > 0 {
		msgs, err := db.GetRecentChatMessages(projectID, transcriptLimit)
		if err != nil {
			return nil, err
		}
		for _, msg := range msgs {
			byID[msg.ID] = msg
		}
	}
	if toolMessageLimit > 0 {
		var toolMsgs []models.AppChatMessage
		err := db.DB.NewSelect().
			Model(&toolMsgs).
			Where("app_project_id = ?", projectID).
			Where("role = ?", "assistant").
			Where("jsonb_array_length(tool_calls) > 0").
			Order("created_at DESC").
			Limit(toolMessageLimit).
			Scan(context.Background())
		if err != nil {
			return nil, err
		}
		for _, msg := range toolMsgs {
			byID[msg.ID] = msg
		}
	}
	out := make([]models.AppChatMessage, 0, len(byID))
	for _, msg := range byID {
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID.String() < out[j].ID.String()
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
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
