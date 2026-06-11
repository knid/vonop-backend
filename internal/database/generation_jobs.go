package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
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

// MarkOrphanedGenerationJobs is called on server startup: any `running` or
// `awaiting_input` generation jobs left over from a previous process are
// marked `failed`. Without this, a stuck job blocks new generate_app and
// chat-edit flows because CreateJob/GetActiveAgentRun see them as in-flight
// and reject the new request with "a generation is already in progress".
func (db *Database) MarkOrphanedGenerationJobs() (int, error) {
	res, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("status = ?", "failed").
		Set("error = ?", "server restarted").
		Set("updated_at = ?", time.Now()).
		Where("status IN (?)", bun.In([]string{"running", "awaiting_input"})).
		Exec(context.Background())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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

// GetOrgRunningJobs counts running/awaiting_input generation jobs across every
// member of the org. Mirrors GetUserRunningJobs (same 15-min recency window).
// GenerationJob has no organization_id column yet, so we join through
// organization_members.
func (db *Database) GetOrgRunningJobs(ctx context.Context, orgID uuid.UUID) (int, error) {
	count, err := db.DB.NewSelect().
		Model((*models.GenerationJob)(nil)).
		Join("JOIN organization_members AS om ON om.user_id = generation_job.user_id").
		Where("om.organization_id = ?", orgID).
		Where("om.deleted_at IS NULL").
		Where("generation_job.status IN (?)", bun.In([]string{"running", "awaiting_input"})).
		Where("generation_job.created_at > ?", time.Now().Add(-15*time.Minute)).
		Count(ctx)
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

// GetAppActiveJob returns the most recent running or awaiting_input job scoped
// to a given app, or nil if none exists. Used by GET /v2/apps/{appId}/active-job
// so clients can reconnect to an in-flight SSE stream after a refresh without
// holding the job id in localStorage.
func (db *Database) GetAppActiveJob(appID string) (*models.GenerationJob, error) {
	var job models.GenerationJob
	err := db.DB.NewSelect().
		Model(&job).
		Where("app_id = ?", appID).
		Where("status IN (?)", bun.In([]string{"running", "awaiting_input"})).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// GetAppLatestDeepAgentsJob returns the most recent job that has been linked
// to a Deep Agents runtime. This lets clients rehydrate the native LangGraph
// thread after the launch job has already completed.
func (db *Database) GetAppLatestDeepAgentsJob(appID string) (*models.GenerationJob, error) {
	var job models.GenerationJob
	err := db.DB.NewSelect().
		Model(&job).
		Where("app_id = ?", appID).
		WhereGroup(" AND ", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.
				Where("agent_runtime_run_id IS NOT NULL").
				WhereOr("deepagents_thread_id <> ''")
		}).
		OrderExpr("created_at DESC").
		Limit(1).
		Scan(context.Background())
	if err != nil {
		return nil, err
	}
	return &job, nil
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
