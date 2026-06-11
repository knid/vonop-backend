package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

func (db *Database) GetAgentThreadByProject(
	ctx context.Context,
	projectID uuid.UUID,
	conversationKey string,
) (*models.AgentThread, error) {
	if conversationKey == "" {
		conversationKey = "default"
	}
	var thread models.AgentThread
	err := db.DB.NewSelect().
		Model(&thread).
		Where("app_project_id = ?", projectID).
		Where("conversation_key = ?", conversationKey).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &thread, nil
}

func (db *Database) GetAgentThreadByDeepAgentsThreadID(
	ctx context.Context,
	deepAgentsThreadID string,
) (*models.AgentThread, error) {
	var thread models.AgentThread
	err := db.DB.NewSelect().
		Model(&thread).
		Where("deepagents_thread_id = ?", deepAgentsThreadID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &thread, nil
}

func (db *Database) GetOrCreateAgentThread(
	ctx context.Context,
	thread *models.AgentThread,
) (*models.AgentThread, error) {
	if thread.ConversationKey == "" {
		thread.ConversationKey = "default"
	}
	existing, err := db.GetAgentThreadByProject(ctx, thread.AppProjectID, thread.ConversationKey)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	_, err = db.DB.NewInsert().
		Model(thread).
		On("CONFLICT (app_project_id, conversation_key) DO UPDATE").
		Set("updated_at = current_timestamp").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return thread, nil
}

func (db *Database) InsertAgentRuntimeRun(
	ctx context.Context,
	run *models.AgentRuntimeRun,
) (*models.AgentRuntimeRun, error) {
	_, err := db.DB.NewInsert().Model(run).Returning("*").Exec(ctx)
	return run, err
}

func (db *Database) GetAgentRuntimeRun(
	ctx context.Context,
	runID uuid.UUID,
) (*models.AgentRuntimeRun, error) {
	var run models.AgentRuntimeRun
	err := db.DB.NewSelect().Model(&run).Where("id = ?", runID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (db *Database) GetAgentRuntimeRunByGenerationJob(
	ctx context.Context,
	generationJobID uuid.UUID,
) (*models.AgentRuntimeRun, error) {
	var run models.AgentRuntimeRun
	err := db.DB.NewSelect().
		Model(&run).
		Where("generation_job_id = ?", generationJobID).
		Order("created_at DESC").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func (db *Database) UpdateAgentRuntimeRun(
	ctx context.Context,
	run *models.AgentRuntimeRun,
) error {
	run.UpdatedAt = time.Now()
	_, err := db.DB.NewUpdate().
		Model(run).
		Where("id = ?", run.ID).
		Exec(ctx)
	return err
}

func (db *Database) UpdateProjectCurrentCommit(
	ctx context.Context,
	projectID uuid.UUID,
	currentCommit string,
) error {
	_, err := db.DB.NewUpdate().
		Model((*models.AppProject)(nil)).
		Set("current_commit = ?", currentCommit).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", projectID).
		Exec(ctx)
	if err == nil {
		db.InvalidateProjectCache(projectID.String())
	}
	return err
}

func (db *Database) UpdateProjectGitRepositoryCurrentCommit(
	ctx context.Context,
	projectID uuid.UUID,
	currentCommit string,
	lastSuccessfulRunRef string,
) error {
	_, err := db.DB.NewUpdate().
		Model((*models.ProjectGitRepository)(nil)).
		Set("current_commit = ?", currentCommit).
		Set("last_successful_run_ref = ?", lastSuccessfulRunRef).
		Set("updated_at = ?", time.Now()).
		Where("app_project_id = ?", projectID).
		Exec(ctx)
	return err
}

func (db *Database) LinkGenerationJobToAgentRuntimeRun(
	ctx context.Context,
	jobID uuid.UUID,
	runID uuid.UUID,
	deepAgentsThreadID string,
	deepAgentsRunID string,
) error {
	_, err := db.DB.NewUpdate().
		Model((*models.GenerationJob)(nil)).
		Set("agent_runtime_run_id = ?", runID).
		Set("deepagents_thread_id = ?", deepAgentsThreadID).
		Set("deepagents_run_id = ?", deepAgentsRunID).
		Set("updated_at = ?", time.Now()).
		Where("id = ?", jobID).
		Exec(ctx)
	return err
}

func (db *Database) UpsertProjectGitRepository(
	ctx context.Context,
	repo *models.ProjectGitRepository,
) (*models.ProjectGitRepository, error) {
	_, err := db.DB.NewInsert().
		Model(repo).
		On("CONFLICT (app_project_id) DO UPDATE").
		Set("storage_mode = EXCLUDED.storage_mode").
		Set("bare_repo_path = EXCLUDED.bare_repo_path").
		Set("legacy_repo_path = EXCLUDED.legacy_repo_path").
		Set("default_branch = EXCLUDED.default_branch").
		Set("current_commit = EXCLUDED.current_commit").
		Set("main_ref = EXCLUDED.main_ref").
		Set("last_successful_run_ref = EXCLUDED.last_successful_run_ref").
		Set("metadata = EXCLUDED.metadata").
		Set("updated_at = current_timestamp").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return repo, nil
}

func (db *Database) GetProjectGitRepository(
	ctx context.Context,
	projectID uuid.UUID,
) (*models.ProjectGitRepository, error) {
	var repo models.ProjectGitRepository
	err := db.DB.NewSelect().
		Model(&repo).
		Where("app_project_id = ?", projectID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &repo, nil
}
