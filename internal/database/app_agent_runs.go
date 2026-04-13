package database

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// InsertAgentRun persists a new agent run in `running` status.
func (db *Database) InsertAgentRun(r *models.AppAgentRun) (*models.AppAgentRun, error) {
	_, err := db.DB.NewInsert().Model(r).Returning("*").Exec(context.Background())
	return r, err
}

// UpdateAgentRun writes terminal state to an existing run row.
func (db *Database) UpdateAgentRun(r *models.AppAgentRun) error {
	_, err := db.DB.NewUpdate().
		Model(r).
		Where("id = ?", r.ID).
		Exec(context.Background())
	return err
}

// GetActiveAgentRun returns the currently running run for a project, or
// (nil, nil) if none. Used to enforce one-run-per-project and to let the
// dashboard reconnect after a refresh.
func (db *Database) GetActiveAgentRun(projectID uuid.UUID) (*models.AppAgentRun, error) {
	var r models.AppAgentRun
	err := db.DB.NewSelect().
		Model(&r).
		Where("app_project_id = ?", projectID).
		Where("status = ?", models.AgentRunStatusRunning).
		Order("created_at DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetAgentRun returns a run by ID regardless of status.
func (db *Database) GetAgentRun(runID uuid.UUID) (*models.AppAgentRun, error) {
	var r models.AppAgentRun
	err := db.DB.NewSelect().Model(&r).Where("id = ?", runID).Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// MarkOrphanedRunsFailed is called on server startup: any `running` rows left
// over from a previous process are marked `failed`. We don't resume them.
func (db *Database) MarkOrphanedRunsFailed() (int, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.AppAgentRun)(nil)).
		Set("status = ?", models.AgentRunStatusFailed).
		Set("error = ?", "server restarted").
		Set("completed_at = ?", time.Now()).
		Where("status = ?", models.AgentRunStatusRunning).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
