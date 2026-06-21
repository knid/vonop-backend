package agent

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/models"
)

type changedPathsSinceRepo interface {
	ChangedPathsSince(fromHash string) ([]string, error)
}

func syncSandboxAfterCommit(ctx context.Context, deps *RunDependencies, repo changedPathsSinceRepo, project *models.AppProject, state *BuildState, preCommit string) {
	if deps == nil || deps.SyncSandboxFiles == nil || project == nil {
		return
	}
	paths, source := sandboxSyncPaths(repo, state, preCommit)
	if len(paths) == 0 {
		return
	}
	if err := deps.SyncSandboxFiles(ctx, project, paths); err != nil {
		slog.Warn("agent: sandbox sync after git commit failed",
			"error", err, "project_id", project.ID, "paths", paths, "source", source)
	}
}

func sandboxSyncPaths(repo changedPathsSinceRepo, state *BuildState, preCommit string) ([]string, string) {
	if repo != nil && preCommit != "" {
		paths, err := repo.ChangedPathsSince(preCommit)
		if err == nil {
			return paths, "git_diff"
		}
		slog.Warn("agent: git diff for sandbox sync failed, falling back to build state",
			"error", err, "pre_commit", preCommit)
	}
	if state == nil {
		return nil, "none"
	}
	return state.ChangedPaths(), "build_state"
}
