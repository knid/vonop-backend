package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
)

type DeepAgentsFinalizeRequest struct {
	AgentRuntimeRunID uuid.UUID
	Successful        bool
	Summary           string
}

func (c *Controller) FinalizeDeepAgentsRun(
	ctx context.Context,
	req DeepAgentsFinalizeRequest,
) (*gitrepo.FinalizeRunResult, error) {
	if c.BareGitRepoService == nil {
		return nil, fmt.Errorf("bare Git repo service is not configured")
	}
	run, err := c.DB.GetAgentRuntimeRun(ctx, req.AgentRuntimeRunID)
	if err != nil {
		return nil, fmt.Errorf("load runtime run: %w", err)
	}
	if run.WorktreePath == "" {
		return nil, fmt.Errorf("runtime run has no worktree path")
	}
	project, err := c.DB.GetAppProjectByID(run.AppProjectID.String())
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}

	wt := &gitrepo.RunWorktree{
		ProjectID:     run.AppProjectID.String(),
		JobID:         run.GenerationJobID.String(),
		Path:          run.WorktreePath,
		BaseCommit:    run.BaseCommit,
		BareRepoPath:  filepath.Join(c.BareGitRepoService.BareRoot, run.AppProjectID.String()+".git"),
		DefaultBranch: "main",
	}
	result, err := c.BareGitRepoService.FinalizeRunWorktree(ctx, wt, gitrepo.FinalizeRunOptions{
		Successful:            req.Successful,
		Summary:               req.Summary,
		ExpectedCurrentCommit: project.CurrentCommit,
	})
	if err != nil && !errors.Is(err, gitrepo.ErrFinalizationConflict) {
		run.Status = models.AgentRuntimeRunStatusFailed
		run.ErrorMessage = err.Error()
		_ = c.DB.UpdateAgentRuntimeRun(ctx, run)
		return result, err
	}

	now := time.Now()
	if result != nil {
		run.RunCommit = result.RunCommit
		run.RunRef = result.RunRef
		run.CheckpointCommit = result.CheckpointCommit
		run.FinalCommit = result.FinalCommit
		run.FinalizedToProject = result.FinalizedToProject
	}
	if errors.Is(err, gitrepo.ErrFinalizationConflict) {
		run.Status = models.AgentRuntimeRunStatusFailed
		run.ErrorCode = "workspace_conflict"
		run.ErrorMessage = err.Error()
	} else if req.Successful {
		run.Status = models.AgentRuntimeRunStatusCompleted
		run.CompletedAt = now
	} else {
		run.Status = models.AgentRuntimeRunStatusFailed
		run.CompletedAt = now
	}
	if updateErr := c.DB.UpdateAgentRuntimeRun(ctx, run); updateErr != nil {
		return result, updateErr
	}

	if result != nil && result.FinalizedToProject && result.FinalCommit != "" {
		if err := c.DB.UpdateProjectCurrentCommit(ctx, run.AppProjectID, result.FinalCommit); err != nil {
			return result, err
		}
		if err := c.DB.UpdateProjectGitRepositoryCurrentCommit(ctx, run.AppProjectID, result.FinalCommit, result.RunRef); err != nil {
			return result, err
		}
	}
	return result, err
}
