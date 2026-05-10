// Asynq middleware that emits PostHog `job_started` / `job_succeeded` /
// `job_failed` events around every task handler. Replaces the previous
// state where Asynq lifecycle was only visible via slog → BetterStack and
// could not be queried, alerted on, or correlated with user events.
//
// Cron tasks (type prefix `cron:`) are skipped — they fire on tight
// schedules with no user attribution and would drown out user-triggered
// job events.
//
// Distinct ID resolution — best-effort, in priority order:
//   1. user_id from payload (agent:run carries this directly)
//   2. org_id from payload (billing:org-grace-teardown)
//   3. deployment_id (deploy:web — no user in payload, but joinable in queries)
//   4. runtime_app_id (fly:delete-app)
//   5. job_id (agent:run secondary key, also unique)
//   6. "system" (fallback for tasks with empty payloads)
package middleware

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/controllers"
	"github.com/posthog/posthog-go"
)

// jobPayloadProbe carries every ID field we care about across the union of
// Asynq payload structs. Fields that aren't present in a given payload stay
// at their zero value and are filtered out before being attached to events.
type jobPayloadProbe struct {
	UserID       string `json:"user_id"`
	OrgID        string `json:"org_id"`
	DeploymentID string `json:"deployment_id"`
	RuntimeAppID string `json:"runtime_app_id"`
	FlyAppName   string `json:"fly_app_name"`
	JobID        string `json:"job_id"`
	ProjectID    string `json:"project_id"`
}

// AsynqPostHogLifecycle wraps every task handler with PostHog event capture.
// Returns a function compatible with asynq.MiddlewareFunc so it can be
// attached via mux.Use(...).
func AsynqPostHogLifecycle(c *controllers.Controller) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			// Skip PostHog tracking for cron heartbeats — they fire on tight
			// schedules (every 30s/1m/5m), have no user attribution, and
			// drown out user-triggered job events in the activity feed.
			if strings.HasPrefix(t.Type(), "cron:") {
				return next.ProcessTask(ctx, t)
			}

			start := time.Now()

			queue, _ := asynq.GetQueueName(ctx)
			taskID, _ := asynq.GetTaskID(ctx)
			attempt, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)

			distinctID, payloadProps := extractPayloadAttribution(t.Payload())

			baseProps := posthog.NewProperties().
				Set("task_type", t.Type()).
				Set("task_id", taskID).
				Set("queue", queue).
				Set("attempt", attempt).
				Set("max_retry", maxRetry).
				Set("payload_bytes", len(t.Payload()))
			for k, v := range payloadProps {
				baseProps.Set(k, v)
			}

			c.TrackSystemEvent(distinctID, constants.EVENT_JOB_STARTED, cloneProps(baseProps))

			err := next.ProcessTask(ctx, t)
			durationMs := time.Since(start).Milliseconds()
			done := cloneProps(baseProps).Set("duration_ms", durationMs)

			if err == nil {
				c.TrackSystemEvent(distinctID, constants.EVENT_JOB_SUCCEEDED, done)
				return nil
			}

			isFinal := attempt >= maxRetry
			done.
				Set("error", truncate(err.Error(), 500)).
				Set("is_final", isFinal)
			c.TrackSystemEvent(distinctID, constants.EVENT_JOB_FAILED, done)
			return err
		})
	}
}

// extractPayloadAttribution decodes the task payload into a probe struct and
// returns (distinctID, extra-properties). Empty distinctID falls back to
// "system" inside TrackSystemEvent. Decode failures are non-fatal — we still
// emit the lifecycle event keyed by the task type.
func extractPayloadAttribution(payload []byte) (string, map[string]string) {
	props := map[string]string{}
	if len(payload) == 0 {
		return "", props
	}
	var p jobPayloadProbe
	if err := json.Unmarshal(payload, &p); err != nil {
		return "", props
	}

	if p.UserID != "" {
		props["user_id"] = p.UserID
	}
	if p.OrgID != "" {
		props["org_id"] = p.OrgID
	}
	if p.DeploymentID != "" {
		props["deployment_id"] = p.DeploymentID
	}
	if p.RuntimeAppID != "" {
		props["runtime_app_id"] = p.RuntimeAppID
	}
	if p.FlyAppName != "" {
		props["fly_app_name"] = p.FlyAppName
	}
	if p.JobID != "" {
		props["job_id"] = p.JobID
	}
	if p.ProjectID != "" {
		props["project_id"] = p.ProjectID
	}

	switch {
	case p.UserID != "":
		return p.UserID, props
	case p.OrgID != "":
		return p.OrgID, props
	case p.DeploymentID != "":
		return p.DeploymentID, props
	case p.RuntimeAppID != "":
		return p.RuntimeAppID, props
	case p.JobID != "":
		return p.JobID, props
	default:
		return "", props
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// cloneProps avoids the started/succeeded/failed events sharing a mutable
// props map — posthog-go enqueues events asynchronously and a later Set
// would otherwise leak into an already-buffered event.
func cloneProps(p posthog.Properties) posthog.Properties {
	out := posthog.NewProperties()
	for k, v := range p {
		out.Set(k, v)
	}
	return out
}
