package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/knid/wemob/internal/models"
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
		Where("status = ?", "running").
		Count(context.Background())
	return count, err
}

func (db *Database) GetUserActiveJobs(userID string) ([]models.GenerationJob, error) {
	var jobs []models.GenerationJob
	err := db.DB.NewSelect().
		Model(&jobs).
		Where("user_id = ?", userID).
		Where("status = ?", "running").
		OrderExpr("created_at DESC").
		Scan(context.Background())
	return jobs, err
}
