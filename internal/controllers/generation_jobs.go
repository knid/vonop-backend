package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/jobstream"
	"github.com/knid/wemob/internal/services/remoteconfig"
)

// Sentinel errors for CreateJob so the view can distinguish "in progress"
// (429 GENERATION_IN_PROGRESS) from infrastructure failures (500).
var (
	ErrConcurrentJobLimit          = errors.New("concurrent job limit reached")
	ErrGenerationAlreadyInProgress = errors.New("a generation is already in progress")
)

// --- Conversation / clarification types ---

// ConversationReply is a natural language reply from the user during the pre-build conversation.
type ConversationReply struct {
	Message string `json:"message"`
}

// ClarificationAnswer is a single user answer to a clarification question.
type ClarificationAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

// ClarificationResult is sent through the pending channel from the clarify endpoint.
type ClarificationResult struct {
	Answers []ClarificationAnswer
	Skip    bool
}

// milestoneEvents are event types worth persisting to the database.
// High-frequency events like chunk/thinking/narration/tool_call_delta are
// skipped; those are folded into the stream_state buffer instead so reconnect
// can rehydrate them without blowing up the persisted event log.
var milestoneEvents = map[string]bool{
	// Legacy events still emitted by ecommerce/dashboard stream routes.
	"file_complete":          true,
	"chat_complete":          true,
	"file_generation_start":  true,
	"project_created":        true,
	"error":                  true,
	"done":                   true,
	"store_chat_complete":    true,
	"app_generation_started": true,
	"clarification_needed":   true,
	"clarification_received": true,
	"conversation_done":      true,
	"awaiting_reply":         true,
	"deps_ready":             true,
	// Agent runtime events (2026-04 rewrite). On reconnect these get
	// replayed so the client's timeline rebuilds past tool calls + file
	// writes. narration/tool_call_delta/keepalive stay out.
	"run_start":              true,
	"turn_start":             true,
	"tool_call_start":        true,
	"tool_call_result":       true,
	"tool_approval_required": true,
	"tool_approval_resolved": true,
	"file_written":           true,
	"file_deleted":           true,
	"turn_end":               true,
	"credits_update":         true,
}

// CreateJob creates a new generation job, enforcing both a per-user and a
// per-org concurrency limit. The per-user limit prevents one member from
// saturating the org's slots; the per-org limit prevents an N-member org from
// holding N× the concurrency it pays for. For generate_app jobs, only one may
// run at a time to prevent double-submission. appID is optional — pass
// uuid.Nil for jobs that aren't scoped to an app.
func (c *Controller) CreateJob(userID uuid.UUID, jobType string, appID uuid.UUID) (*models.GenerationJob, error) {
	running, err := c.DB.GetUserRunningJobs(userID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to check running jobs: %w", err)
	}
	maxJobs := c.RemoteConfig.GetInt(remoteconfig.MaxConcurrentJobsPerUser, userID.String())
	if running >= maxJobs {
		return nil, fmt.Errorf("%w: maximum %d concurrent jobs allowed", ErrConcurrentJobLimit, maxJobs)
	}

	user, err := c.DB.GetUserById(userID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load user for org cap check: %w", err)
	}
	orgID, err := c.RequireOrgIDForUser(user, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve org for job: %w", err)
	}
	orgRunning, err := c.DB.GetOrgRunningJobs(context.Background(), orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to check org running jobs: %w", err)
	}
	maxOrgJobs := c.RemoteConfig.GetInt(remoteconfig.MaxConcurrentJobsPerOrg, orgID.String())
	if orgRunning >= maxOrgJobs {
		return nil, fmt.Errorf("%w: your organization has reached its concurrent generation limit (%d). Please wait for an existing job to finish", ErrConcurrentJobLimit, maxOrgJobs)
	}
	// Prevent duplicate initial generation on the same app: if this app already
	// has a running generate_app job, reject the new one to avoid wasting
	// resources on a double-submission (e.g. double-clicked Create button).
	// Different apps are allowed to generate in parallel.
	if jobType == "generate_app" && appID != uuid.Nil {
		activeJobs, _ := c.DB.GetUserActiveJobs(userID.String())
		for _, j := range activeJobs {
			if j.Type == "generate_app" && j.AppID == appID {
				return nil, ErrGenerationAlreadyInProgress
			}
		}
	}

	job := &models.GenerationJob{
		UserID: userID,
		AppID:  appID,
		Type:   jobType,
		Status: "running",
		Events: json.RawMessage("[]"),
	}

	job, err = c.DB.CreateGenerationJob(job)
	if err != nil {
		return nil, fmt.Errorf("failed to create job: %w", err)
	}

	return job, nil
}

// GetAppActiveJob returns the most recent running or awaiting_input job for the
// given app, or nil if none exists. Used by the chat UI to reconnect the SSE
// stream after a page refresh or browser restart.
func (c *Controller) GetAppActiveJob(appID uuid.UUID) (*models.GenerationJob, error) {
	return c.DB.GetAppActiveJob(appID.String())
}

// CancelJob cancels a running or awaiting_input job. For agent-run jobs
// (executed in the worker) this publishes to jobstream.CancelChannel(jobID);
// the worker subscribes for the duration of agent.Run and unwinds via its
// existing ctx-aware paths. For legacy in-API-process jobs (dashboards) it
// invokes the in-process cancelFn registered in RunningJobs.
//
// In both cases the DB row is flipped to "cancelled" so the UI sees the
// terminal state regardless of whether the cancel signal reached its
// target — a worker that's already past its emit-loop will still finish
// the run, but its FinalizeJob won't reopen a cancelled row.
func (c *Controller) CancelJob(jobID string, userID uuid.UUID) error {
	job, err := c.DB.GetGenerationJob(jobID)
	if err != nil {
		return fmt.Errorf("job not found")
	}
	if job.UserID != userID {
		return fmt.Errorf("job not found")
	}
	if job.Status != "running" && job.Status != "awaiting_input" {
		return fmt.Errorf("job is not running")
	}

	if c.DeepAgentsClient != nil && (job.DeepAgentsRunID != "" || job.AgentRuntimeRunID != nil) {
		runID := job.DeepAgentsRunID
		if runID == "" {
			if runtimeRun, err := c.DB.GetAgentRuntimeRunByGenerationJob(context.Background(), job.ID); err == nil {
				runID = runtimeRun.DeepAgentsRunID
			}
		}
		if runID != "" {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := c.DeepAgentsClient.CancelRun(cancelCtx, runID); err != nil {
				slog.Warn("CancelJob: Deep Agents cancel failed",
					"job_id", jobID, "deepagents_run_id", runID, "error", err)
			}
		}
	}

	// Drop pending clarification channel if present (legacy path; no production
	// agent path emits this today, but the helper still services older clients).
	if ch, ok := c.PendingClarifications.LoadAndDelete(jobID); ok {
		if typedCh, ok := ch.(chan ClarificationResult); ok {
			close(typedCh)
		}
	}

	// Try the worker-side cancel first. If the job was created by the new
	// agent-run path the worker has a subscription on this channel; if it
	// was created by a legacy in-API-process StreamFunc (dashboards) the
	// publish has 0 receivers and we fall through to the in-process cancel.
	//
	// Bound the publish by a short timeout — go-redis's default dial timeout
	// is 5s, and without an explicit ctx every CancelJob would block that
	// long when Redis is unreachable. 2s is plenty for a healthy publish on
	// a managed Valkey; the in-process cancel below still fires regardless.
	publishCtx, publishCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer publishCancel()
	if err := jobstream.PublishCancel(publishCtx, c.RedisPubSub, jobID); err != nil {
		slog.Warn("CancelJob: publish cancel failed (will still try in-process cancel)",
			"job_id", jobID, "error", err)
	}

	if cancelFn, ok := c.RunningJobs.LoadAndDelete(jobID); ok {
		if fn, ok := cancelFn.(context.CancelFunc); ok {
			fn()
		}
	}

	return c.DB.UpdateGenerationJobStatus(jobID, "cancelled", "cancelled by user")
}

// GetJob fetches a job with ownership check.
func (c *Controller) GetJob(jobID string, userID uuid.UUID) (*models.GenerationJob, error) {
	job, err := c.DB.GetGenerationJob(jobID)
	if err != nil {
		return nil, fmt.Errorf("job not found")
	}
	if job.UserID != userID {
		return nil, fmt.Errorf("job not found")
	}
	return job, nil
}

// GetActiveJobs returns running jobs for a user.
func (c *Controller) GetActiveJobs(userID uuid.UUID) ([]models.GenerationJob, error) {
	return c.DB.GetUserActiveJobs(userID.String())
}

// SubmitClarification sends user answers to a job awaiting clarification.
func (c *Controller) SubmitClarification(jobID string, result ClarificationResult) error {
	val, ok := c.PendingClarifications.Load(jobID)
	if !ok {
		return fmt.Errorf("no pending clarification for this job")
	}
	ch, ok := val.(chan ClarificationResult)
	if !ok {
		return fmt.Errorf("invalid clarification channel type")
	}
	// Non-blocking send — channel is buffered with size 1
	select {
	case ch <- result:
		return nil
	default:
		return fmt.Errorf("clarification already submitted")
	}
}

// SubmitReply sends a natural language reply to a job awaiting user input.
// Reuses the same PendingClarifications channel infrastructure.
func (c *Controller) SubmitReply(jobID string, reply ConversationReply) error {
	val, ok := c.PendingClarifications.Load(jobID)
	if !ok {
		return fmt.Errorf("no pending reply expected for this job")
	}
	ch, ok := val.(chan ClarificationResult)
	if !ok {
		return fmt.Errorf("invalid reply channel type")
	}
	// Package the reply as a ClarificationResult so the blocking side can read it
	result := ClarificationResult{
		Answers: []ClarificationAnswer{{ID: "reply", Answer: reply.Message}},
	}
	select {
	case ch <- result:
		return nil
	default:
		return fmt.Errorf("reply already submitted")
	}
}

// IsMilestoneEvent returns true if the event type should be persisted.
func IsMilestoneEvent(eventName string) bool {
	return milestoneEvents[eventName]
}

// StripHeavyFields removes bulky fields (like file content) from event data
// before persisting to the DB. The content is already saved in the project
// repo (file_written) or AppGeneratedFile (legacy file_complete).
func StripHeavyFields(eventName string, data any) any {
	switch eventName {
	case "file_complete", "chat_complete", "file_written":
		// fall through
	default:
		return data
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return data
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return data
	}
	delete(m, "content")
	return m
}

// FinalizeJob updates job status based on the last event and context state.
func (c *Controller) FinalizeJob(jobID string, ctx context.Context, lastEvent *responses.SSEEvent) {
	c.RunningJobs.Delete(jobID)
	c.PendingClarifications.Delete(jobID)

	if ctx.Err() != nil {
		c.DB.UpdateGenerationJobStatus(jobID, "cancelled", "job was cancelled or timed out")
		return
	}

	if lastEvent != nil && lastEvent.Event == "error" {
		errorData, _ := json.Marshal(lastEvent.Data)
		c.DB.UpdateGenerationJobStatus(jobID, "failed", string(errorData))
		return
	}

	if lastEvent != nil {
		resultJSON, _ := json.Marshal(lastEvent.Data)
		c.DB.SetGenerationJobResult(jobID, resultJSON)
	} else {
		c.DB.UpdateGenerationJobStatus(jobID, "completed", "")
	}
}
