package controllers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/agentruntime"
	"github.com/knid/wemob/internal/models"
)

type DeepAgentsRunLaunch struct {
	JobID        uuid.UUID                       `json:"job_id"`
	RuntimeRunID uuid.UUID                       `json:"agent_runtime_run_id"`
	Runtime      *models.AgentRuntimeRun         `json:"-"`
	Access       *agentruntime.RuntimeAccessInfo `json:"runtime"`
}

func (c *Controller) StartDeepAgentsRunForJob(
	ctx context.Context,
	user *models.User,
	project *models.AppProject,
	job *models.GenerationJob,
	message string,
	attachmentIDs []uuid.UUID,
) (*DeepAgentsRunLaunch, error) {
	if c.DeepAgentsClient == nil {
		return nil, fmt.Errorf("deep agents runtime client is not configured")
	}
	if c.BareGitRepoService == nil {
		return nil, fmt.Errorf("bare Git repo service is not configured")
	}
	if user == nil || project == nil || job == nil {
		return nil, fmt.Errorf("user, project, and job are required")
	}

	orgID := project.OrganizationID
	if orgID == uuid.Nil {
		resolved, err := c.ResolveOrgForProjectID(user, project.ID.String())
		if err != nil {
			return nil, fmt.Errorf("resolve project org: %w", err)
		}
		orgID = resolved
	}

	thread, err := c.DB.GetOrCreateAgentThread(ctx, &models.AgentThread{
		AppProjectID:       project.ID,
		OrganizationID:     orgID,
		UserID:             user.ID,
		DeepAgentsThreadID: uuid.NewString(),
		ConversationKey:    "default",
		Status:             models.AgentThreadStatusActive,
		Metadata:           map[string]any{},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent thread: %w", err)
	}

	mode := deepAgentsModeForJob(job.Type)
	baseCommit, err := c.ensureDeepAgentsProjectRepo(ctx, project, orgID)
	if err != nil {
		return nil, err
	}
	worktree, err := c.BareGitRepoService.CreateRunWorktree(ctx, project.ID.String(), job.ID.String(), baseCommit)
	if err != nil {
		return nil, fmt.Errorf("create run worktree: %w", err)
	}
	worktreeOwned := true
	defer func() {
		if worktreeOwned {
			_ = worktree.Remove(ctx)
		}
	}()

	runtimeRun := &models.AgentRuntimeRun{
		GenerationJobID:    job.ID,
		AgentThreadID:      thread.ID,
		AppProjectID:       project.ID,
		OrganizationID:     orgID,
		UserID:             user.ID,
		DeepAgentsThreadID: thread.DeepAgentsThreadID,
		RuntimeVersion:     "go-client",
		GraphID:            agentruntime.DefaultAssistantID,
		Mode:               mode,
		Status:             models.AgentRuntimeRunStatusQueued,
		BaseCommit:         baseCommit,
		WorktreePath:       worktree.Path,
		Model:              "",
		Provider:           "",
		Metadata: map[string]any{
			"job_type": job.Type,
		},
	}
	runtimeRun, err = c.DB.InsertAgentRuntimeRun(ctx, runtimeRun)
	if err != nil {
		return nil, fmt.Errorf("insert runtime run: %w", err)
	}

	started, err := c.DeepAgentsClient.StartRun(ctx, agentruntime.StartRunRequest{
		ThreadID:        thread.DeepAgentsThreadID,
		JobID:           job.ID.String(),
		ProjectID:       project.ID.String(),
		OrgID:           orgID.String(),
		UserID:          user.ID.String(),
		RuntimeRunID:    runtimeRun.ID.String(),
		Message:         message,
		Mode:            mode,
		ProjectType:     project.ProjectType,
		FrontendRuntime: project.FrontendRuntime,
		WorktreePath:    worktree.Path,
		BaseCommit:      baseCommit,
		AttachmentIDs:   uuidStrings(attachmentIDs),
		FeatureFlags:    map[string]any{},
		CreditBudget:    0,
	})
	if err != nil {
		runtimeRun.Status = models.AgentRuntimeRunStatusFailed
		runtimeRun.ErrorMessage = err.Error()
		_ = c.DB.UpdateAgentRuntimeRun(ctx, runtimeRun)
		return nil, fmt.Errorf("start deep agents run: %w", err)
	}
	worktreeOwned = false

	now := time.Now()
	runtimeRun.DeepAgentsRunID = started.RunID
	runtimeRun.Status = models.AgentRuntimeRunStatusRunning
	runtimeRun.StartedAt = now
	if err := c.DB.UpdateAgentRuntimeRun(ctx, runtimeRun); err != nil {
		return nil, fmt.Errorf("update runtime run: %w", err)
	}
	if err := c.DB.LinkGenerationJobToAgentRuntimeRun(ctx, job.ID, runtimeRun.ID, thread.DeepAgentsThreadID, started.RunID); err != nil {
		return nil, fmt.Errorf("link generation job runtime run: %w", err)
	}

	access, err := c.DeepAgentsClient.IssueRuntimeAccessToken(agentruntime.RuntimeAccessRequest{
		UserID:      user.ID.String(),
		OrgID:       orgID.String(),
		ProjectID:   project.ID.String(),
		ThreadID:    thread.DeepAgentsThreadID,
		AssistantID: started.AssistantID,
		ExpiresIn:   30 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("issue runtime access token: %w", err)
	}

	return &DeepAgentsRunLaunch{
		JobID:        job.ID,
		RuntimeRunID: runtimeRun.ID,
		Runtime:      runtimeRun,
		Access:       access,
	}, nil
}

func (c *Controller) ensureDeepAgentsProjectRepo(ctx context.Context, project *models.AppProject, orgID uuid.UUID) (string, error) {
	if project.CurrentCommit != "" {
		return project.CurrentCommit, nil
	}
	if repo, err := c.DB.GetProjectGitRepository(ctx, project.ID); err == nil && repo.CurrentCommit != "" {
		if err := c.DB.UpdateProjectCurrentCommit(ctx, project.ID, repo.CurrentCommit); err != nil {
			return "", fmt.Errorf("sync project current commit: %w", err)
		}
		project.CurrentCommit = repo.CurrentCommit
		return repo.CurrentCommit, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("load project git repository: %w", err)
	}

	if migration, err := c.BareGitRepoService.MigrateLegacyRepoToBare(ctx, project.ID.String()); err == nil && migration.CurrentCommit != "" {
		if _, err := c.DB.UpsertProjectGitRepository(ctx, &models.ProjectGitRepository{
			AppProjectID:   project.ID,
			OrganizationID: orgID,
			StorageMode:    "bare_worktree",
			BareRepoPath:   migration.BareRepoPath,
			LegacyRepoPath: migration.LegacyRepoPath,
			DefaultBranch:  "main",
			CurrentCommit:  migration.CurrentCommit,
			MainRef:        "refs/heads/main",
			Metadata: map[string]any{
				"created_from":         "legacy_repo_migration",
				"created_dirty_commit": migration.CreatedDirtyCommit,
			},
		}); err != nil {
			return "", fmt.Errorf("record migrated project repository: %w", err)
		}
		if err := c.DB.UpdateProjectCurrentCommit(ctx, project.ID, migration.CurrentCommit); err != nil {
			return "", fmt.Errorf("update migrated project current commit: %w", err)
		}
		project.CurrentCommit = migration.CurrentCommit
		return migration.CurrentCommit, nil
	}

	repo, err := c.BareGitRepoService.InitBareProject(ctx, project.ID.String())
	if err != nil {
		return "", fmt.Errorf("initialize project bare repository: %w", err)
	}
	if _, err := c.DB.UpsertProjectGitRepository(ctx, &models.ProjectGitRepository{
		AppProjectID:   project.ID,
		OrganizationID: orgID,
		StorageMode:    "bare_worktree",
		BareRepoPath:   repo.Path,
		DefaultBranch:  repo.DefaultBranch,
		CurrentCommit:  repo.CurrentCommit,
		MainRef:        "refs/heads/main",
		Metadata:       map[string]any{"created_from": "deepagents_launch"},
	}); err != nil {
		return "", fmt.Errorf("record initialized project repository: %w", err)
	}
	if err := c.DB.UpdateProjectCurrentCommit(ctx, project.ID, repo.CurrentCommit); err != nil {
		return "", fmt.Errorf("update initialized project current commit: %w", err)
	}
	project.CurrentCommit = repo.CurrentCommit
	return repo.CurrentCommit, nil
}

func deepAgentsModeForJob(jobType string) string {
	if jobType == "generate_app" {
		return "generate"
	}
	return "edit"
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
