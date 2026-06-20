package agent

import (
	"context"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

type RunFinalizer struct {
	Deps *RunDependencies
}

func NewRunFinalizer(deps *RunDependencies) RunFinalizer {
	return RunFinalizer{Deps: deps}
}

type FinalizeSuccessInput struct {
	Ctx             context.Context
	Repo            *gitrepo.Repo
	Run             *models.AppAgentRun
	Emitter         EventEmitter
	FileMap         *SafeFileMap
	BuildState      *BuildState
	Parts           []models.ChatMessagePart
	Tracker         *utils.TokenTracker
	RunStart        time.Time
	TotalToolCalls  int
	Turns           int
	UserMessage     string
	User            *models.User
	Project         *models.AppProject
	LangfuseTraceID string
}

func (f RunFinalizer) Success(input FinalizeSuccessInput) error {
	return withRuntimePhase(input.Ctx, RuntimePhaseRecordUsageAndScores, func(ctx context.Context) error {
		return finaliseSuccess(
			ctx,
			f.Deps,
			input.Repo,
			input.Run,
			input.Emitter,
			input.FileMap,
			input.BuildState,
			input.Parts,
			input.Tracker,
			input.RunStart,
			input.TotalToolCalls,
			input.Turns,
			input.UserMessage,
			input.User,
			input.Project,
			input.LangfuseTraceID,
		)
	})
}
