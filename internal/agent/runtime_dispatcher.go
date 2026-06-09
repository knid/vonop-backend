package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
)

// Run is the runtime-dispatch entry point for every agent run request.
// The default path remains the existing native Go implementation.
func Run(
	ctx context.Context,
	deps *RunDependencies,
	project *models.AppProject,
	user *models.User,
	message string,
	turnAttachments TurnAttachments,
	emitter EventEmitter,
) error {
	runtime, err := NewRuntimeFromEnv()
	if err != nil {
		return New(CodeInternal, "setup", "failed to initialize agent runtime", err)
	}
	req := RuntimeRequest{
		RunID:       uuid.NewString(),
		JobID:       deps.JobID,
		Project:     project,
		User:        user,
		Message:     message,
		Deps:        deps,
		Attachments: turnAttachments,
		Provider:    deps.LLMProvider,
		Limits:      defaultRuntimeLimits(),
	}
	_, err = runtime.Run(ctx, req, emitter)
	return err
}
