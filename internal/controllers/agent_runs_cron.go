package controllers

import (
	"context"
	"log/slog"
	"time"
)

// stuckAgentRunThreshold needs to comfortably exceed any legitimate agent run.
// p99 turns finish well inside a few minutes; 30 min keeps clear of that while
// still freeing the per-project partial unique index quickly enough that a
// crashed Fly machine doesn't lock other replicas out of starting new runs.
const stuckAgentRunThreshold = 30 * time.Minute

func (c *Controller) ReapStuckAgentRunsTask(ctx context.Context) error {
	n, err := c.DB.MarkOrphanedRunsFailed(ctx, stuckAgentRunThreshold)
	if err != nil {
		return err
	}
	if n > 0 {
		slog.Warn("reaped_stuck_agent_runs", "count", n, "stale_after", stuckAgentRunThreshold)
	}
	return nil
}
