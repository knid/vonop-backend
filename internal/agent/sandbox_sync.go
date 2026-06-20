package agent

import (
	"context"
	"log/slog"

	"github.com/knid/wemob/internal/models"
)

func syncSandboxAfterCommit(ctx context.Context, deps *RunDependencies, project *models.AppProject, state *BuildState) {
	if deps == nil || deps.SyncSandboxFiles == nil || project == nil || state == nil {
		return
	}
	paths := state.ChangedPaths()
	if len(paths) == 0 {
		return
	}
	if err := deps.SyncSandboxFiles(ctx, project, paths); err != nil {
		slog.Warn("agent: sandbox sync after git commit failed",
			"error", err, "project_id", project.ID, "paths", paths)
	}
}
