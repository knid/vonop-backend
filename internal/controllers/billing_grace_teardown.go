package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/tasks"
)

// OrgGraceTeardownDelay is the window between subscription cancellation and
// automatic teardown of every Fly app + Neon DB owned by the org. 7 days
// matches Stripe's default dunning window and gives users time to re-subscribe
// without losing data.
const OrgGraceTeardownDelay = 7 * 24 * time.Hour

func orgGraceTeardownTaskID(orgID uuid.UUID) string {
	return "org-grace-teardown-" + orgID.String()
}

// EnqueueOrgGraceTeardown schedules a one-shot teardown to run
// OrgGraceTeardownDelay from now. If a prior schedule for the same org is
// still pending, this is a no-op (asynq.ErrTaskIDConflict) — the existing
// timer continues.
func (c *Controller) EnqueueOrgGraceTeardown(orgID uuid.UUID) error {
	payload, err := json.Marshal(tasks.OrgGraceTeardownPayload{OrgID: orgID})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	task := asynq.NewTask(tasks.TypeOrgGraceTeardown, payload)
	_, err = c.TaskService.Enqueue(task,
		asynq.Queue("cron"),
		asynq.MaxRetry(3),
		asynq.Timeout(20*time.Minute),
		asynq.ProcessIn(OrgGraceTeardownDelay),
		asynq.TaskID(orgGraceTeardownTaskID(orgID)),
	)
	if err != nil {
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			return nil
		}
		return fmt.Errorf("enqueue: %w", err)
	}
	slog.Info("org_grace_teardown_scheduled", "org_id", orgID, "fires_at", time.Now().Add(OrgGraceTeardownDelay))
	return nil
}

// CancelOrgGraceTeardown removes a pending teardown if the org re-subscribed
// within the grace window. Best-effort: a missing task is fine (means it
// already fired, was never scheduled, or got drained from the scheduled queue).
// AsynqInspector's DeleteTask only works on scheduled/pending/retry queues —
// once running, a teardown is allowed to complete; the handler also re-checks
// SubscriptionStatus before doing destructive work as a second line of defense.
func (c *Controller) CancelOrgGraceTeardown(orgID uuid.UUID) error {
	if c.AsynqInspector == nil {
		return nil
	}
	taskID := orgGraceTeardownTaskID(orgID)
	// Try each queue state; ignore not-found errors.
	for _, q := range []string{"cron"} {
		_ = c.AsynqInspector.DeleteTask(q, taskID)
	}
	return nil
}

// HandleOrgGraceTeardownTask is the worker entrypoint. Re-checks the org's
// subscription status before doing anything destructive — a re-subscribed
// org slips through the cancel-task race because asynq.DeleteTask is only
// best-effort and we'd rather skip a rare valid teardown than nuke an
// active customer's data.
func (c *Controller) HandleOrgGraceTeardownTask(ctx context.Context, t *asynq.Task) error {
	var p tasks.OrgGraceTeardownPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		slog.Error("org_grace_teardown_unmarshal_failed", "error", err)
		return asynq.SkipRetry
	}
	org, err := c.DB.GetOrganizationByID(p.OrgID.String())
	if err != nil {
		slog.Warn("org_grace_teardown_org_lookup_failed", "org_id", p.OrgID, "error", err)
		return asynq.SkipRetry
	}
	if org.SubscriptionStatus != "canceled" {
		slog.Info("org_grace_teardown_aborted_resubscribed",
			"org_id", p.OrgID, "subscription_status", org.SubscriptionStatus)
		return nil
	}

	projects, err := c.DB.GetAppProjectsByOrgID(p.OrgID.String())
	if err != nil {
		return fmt.Errorf("list org projects: %w", err)
	}
	slog.Info("org_grace_teardown_starting", "org_id", p.OrgID, "project_count", len(projects))
	for i := range projects {
		project := &projects[i]
		c.tearDownProjectInfra(ctx, project)
		if err := c.DB.DeleteAppProject(project); err != nil {
			slog.Warn("org_grace_teardown_db_delete_failed",
				"project_id", project.ID, "error", err)
		}
	}
	slog.Info("org_grace_teardown_completed", "org_id", p.OrgID, "project_count", len(projects))
	return nil
}
