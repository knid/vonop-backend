package agent

import (
	"context"

	"github.com/knid/wemob/internal/utils"
)

type AgentLoop interface {
	Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error)
}

type AgentLoopInput struct {
	Model          string
	Messages       []utils.Message
	Tools          []utils.Tool
	Provider       *utils.LLMProvider
	ResponseFormat *utils.ResponseFormat
	Reasoning      *utils.ReasoningConfig
	Executor       utils.ToolExecutor
	OnToolCall     func(toolName string, args string)
	OnChunk        func(chunk string)
	OnReasoning    func(chunk string)
	Config         utils.ToolCallConfig
}

type AgentLoopResult struct {
	Text string
}

type DefaultAgentLoop struct{}

func (DefaultAgentLoop) Execute(ctx context.Context, input AgentLoopInput) (AgentLoopResult, error) {
	text, err := utils.StreamWithTools(
		ctx,
		input.Model,
		input.Messages,
		input.Tools,
		input.Provider,
		input.ResponseFormat,
		input.Reasoning,
		input.Executor,
		input.OnToolCall,
		input.OnChunk,
		input.OnReasoning,
		input.Config,
	)
	return AgentLoopResult{Text: text}, err
}
