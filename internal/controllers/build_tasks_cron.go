package controllers

import (
	"context"
	"log/slog"
	"time"
)

// stuckBuildTaskThreshold: max Codemagic workflow ~45 min + 15 min buffer for retries.
const stuckBuildTaskThreshold = 60 * time.Minute

func (c *Controller) ReapStuckBuildTasksTask(ctx context.Context) error {
	n, err := c.DB.MarkOrphanedBuildTasksFailed(ctx, stuckBuildTaskThreshold)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Warn("reaped_stuck_build_tasks", "count", n, "stale_after", stuckBuildTaskThreshold)
	}
	return nil
}
