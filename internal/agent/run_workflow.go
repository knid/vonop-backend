package agent

import "context"

type AgentRunWorkflow struct {
	AgentLoop AgentLoop
}

func NewAgentRunWorkflow(loop AgentLoop) *AgentRunWorkflow {
	if loop == nil {
		loop = DefaultAgentLoop{}
	}
	return &AgentRunWorkflow{AgentLoop: loop}
}

func (w *AgentRunWorkflow) ExecuteAgentLoop(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	phaseCtx, span := startRuntimePhase(ctx, RuntimePhaseExecuteAgentLoop)
	defer span.End()
	return w.AgentLoop.Execute(phaseCtx, input)
}
