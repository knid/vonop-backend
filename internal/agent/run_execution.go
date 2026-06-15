package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
)

type RunExecution struct {
	Ctx        context.Context
	Deps       *RunDependencies
	Project    *models.AppProject
	User       *models.User
	Run        *models.AppAgentRun
	RunID      uuid.UUID
	Repo       *gitrepo.Repo
	FileMap    *SafeFileMap
	Tracker    *utils.TokenTracker
	Emitter    EventEmitter
	Registry   *Registry
	Todos      *TodoList
	Memory     *AgentRunMemory
	BuildState *BuildState
}

func (e *RunExecution) RunIDString() string {
	if e == nil || e.RunID == uuid.Nil {
		return ""
	}
	return e.RunID.String()
}
