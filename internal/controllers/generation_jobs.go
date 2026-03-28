package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/remoteconfig"
)

// milestoneEvents are event types worth persisting to the database.
// High-frequency events like chunk/thinking are skipped.
var milestoneEvents = map[string]bool{
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
	"rollback":               true,
	"deps_ready":             true,
}

// CreateJob creates a new generation job, enforcing a per-user concurrency limit.
// For generate_app jobs, only one may run at a time to prevent double-submission.
func (c *Controller) CreateJob(userID uuid.UUID, jobType string) (*models.GenerationJob, error) {
	running, err := c.DB.GetUserRunningJobs(userID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to check running jobs: %w", err)
	}
	maxJobs := c.RemoteConfig.GetInt(remoteconfig.MaxConcurrentJobsPerUser, userID.String())
	if running >= maxJobs {
		return nil, fmt.Errorf("maximum %d concurrent jobs allowed", maxJobs)
	}
	// Prevent duplicate initial generation: if the user already has a running
	// generate_app job, reject the new one to avoid wasting resources.
	if jobType == "generate_app" {
		activeJobs, _ := c.DB.GetUserActiveJobs(userID.String())
		for _, j := range activeJobs {
			if j.Type == "generate_app" {
				return nil, fmt.Errorf("a generation is already in progress")
			}
		}
	}

	job := &models.GenerationJob{
		UserID: userID,
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

// CancelJob cancels a running or awaiting_input job by calling its stored cancel function.
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

	// Clean up pending clarification channel if present
	if ch, ok := c.PendingClarifications.LoadAndDelete(jobID); ok {
		if typedCh, ok := ch.(chan ClarificationResult); ok {
			close(typedCh)
		}
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
// before persisting to the DB. The content is already saved in AppGeneratedFile.
func StripHeavyFields(eventName string, data any) any {
	if eventName != "file_complete" && eventName != "chat_complete" {
		return data
	}
	// Marshal to JSON, strip the "content" key, return as map
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
