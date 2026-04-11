package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/uptrace/bun"
)

func (db *Database) CreateGenerationJob(job *models.GenerationJob) (*models.GenerationJob, error) {
	_, err := db.DB.NewInsert().Model(job).Exec(context.Background())
	return job, err
}

func (db *Database) GetGenerationJob(jobID string) (*models.GenerationJob, error) {
	var job models.GenerationJob
	err := db.DB.NewSelect().
		Model(&job).
		Where("id = ?", jobID).
		Scan(context.Background())
	return &job, err
}

func (db *Database) AppendGenerationJobEvent(jobID string, event models.JobEvent) error {
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("events = events || ('[' || ?::text || ']')::jsonb", string(eventJSON)).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(context.Background())
	return err
}

// SetGenerationJobEvents replaces the entire events array with the provided events.
// Used as a final consistency write to ensure all milestone events are persisted,
// even if individual AppendGenerationJobEvent calls failed under DB pressure.
func (db *Database) SetGenerationJobEvents(jobID string, events []models.JobEvent) error {
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return err
	}
	_, err = db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("events = ?", string(eventsJSON)).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(context.Background())
	return err
}

// UpdateGenerationJobStreamState overwrites the partial stream_state buffer on a job.
// Used for throttled flushes of streaming text/thinking/tool-calls during generation so
// reconnecting clients can rehydrate the in-flight UI.
func (db *Database) UpdateGenerationJobStreamState(jobID string, state json.RawMessage) error {
	_, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("stream_state = ?", string(state)).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(context.Background())
	return err
}

func (db *Database) SetGenerationJobResult(jobID string, result json.RawMessage) error {
	_, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("result = ?", result).
		Set("status = ?", "completed").
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(context.Background())
	return err
}

func (db *Database) UpdateGenerationJobStatus(jobID string, status string, errorMsg string) error {
	_, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("status = ?", status).
		Set("error = ?", errorMsg).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(context.Background())
	return err
}

func (db *Database) GetUserRunningJobs(userID string) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.GenerationJob)(nil)).
		Where("user_id = ?", userID).
		Where("status IN (?)", bun.In([]string{"running", "awaiting_input"})).
		Where("created_at > ?", time.Now().Add(-15*time.Minute)).
		Count(context.Background())
	return count, err
}

func (db *Database) GetUserActiveJobs(userID string) ([]models.GenerationJob, error) {
	var jobs []models.GenerationJob
	err := db.DB.NewSelect().
		Model(&jobs).
		Where("user_id = ?", userID).
		Where("status IN (?)", bun.In([]string{"running", "awaiting_input"})).
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return jobs, err
}

// FailStaleAwaitingInputJobs transitions awaiting_input jobs older than the given duration to failed.
func (db *Database) FailStaleAwaitingInputJobs(maxAge time.Duration) (int, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("status = ?", "failed").
		Set("error = ?", "timed out while awaiting user input").
		Set("updated_at = ?", time.Now()).
		Where("status = ?", "awaiting_input").
		Where("updated_at < ?", time.Now().Add(-maxAge)).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	rows, _ := res.RowsAffected()
	return int(rows), nil
}
