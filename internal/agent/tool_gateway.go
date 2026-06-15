package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/observability"
	"github.com/knid/wemob/internal/utils"
)

type ToolGateway struct {
	RunID    uuid.UUID
	Turn     int
	Registry *Registry
	RC       *RunContext
	Emitter  EventEmitter
	Tracker  *utils.TokenTracker
	Memory   *AgentRunMemory
	Deps     *RunDependencies
	Parts    *[]models.ChatMessagePart
	PartsMu  *sync.Mutex
}

func (g *ToolGateway) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	if g == nil {
		return "", fmt.Errorf("tool gateway is nil")
	}
	if g.Registry == nil {
		return "", fmt.Errorf("tool gateway registry is nil")
	}
	if g.RC == nil {
		return "", fmt.Errorf("tool gateway runtime context is nil")
	}

	toolStart := time.Now()
	toolCallID := uuid.NewString()

	if g.Tracker != nil {
		g.Tracker.RecordToolUsed(name)
	}

	partIdx := -1
	if g.Parts != nil {
		if g.PartsMu != nil {
			g.PartsMu.Lock()
		}
		*g.Parts = append(*g.Parts, models.ChatMessagePart{
			Type:       "tool_call",
			ToolCallID: toolCallID,
			Name:       name,
			Input:      json.RawMessage(argsJSON),
		})
		partIdx = len(*g.Parts) - 1
		if g.PartsMu != nil {
			g.PartsMu.Unlock()
		}
	}

	if g.Emitter != nil {
		g.Emitter.Emit(EventToolCallStart, ToolCallStartEvent{
			RunID:        g.RunID,
			Turn:         g.Turn,
			ToolCallID:   toolCallID,
			ToolName:     name,
			RiskLevel:    ClassifyToolRisk(name),
			InputPreview: stripToolInputForPreview(name, argsJSON),
		})
	}

	result, isErr, execErr := g.Registry.Execute(ctx, g.RC, name, json.RawMessage(argsJSON))
	durationMs := int(time.Since(toolStart).Milliseconds())
	isFailure := isErr || execErr != nil

	if g.Tracker != nil {
		requiredSkillIDs, loadedSkillIDs := g.Tracker.AgentSkills()
		langfuseContext := g.Tracker.LangfuseContext()
		langfuseContext.RequiredSkills = requiredSkillIDs
		langfuseContext.LoadedSkills = loadedSkillIDs
		observability.RecordTool(ctx, name, argsJSON, isFailure, durationMs, toolStart,
			langfuseContext.Attributes("tool")...)
	}

	if g.Emitter != nil {
		status := "ok"
		if isFailure {
			status = "error"
		}
		g.Emitter.Emit(EventToolCallResult, ToolCallResultEvent{
			RunID:      g.RunID,
			ToolCallID: toolCallID,
			ToolName:   name,
			RiskLevel:  ClassifyToolRisk(name),
			Status:     status,
			DurationMs: durationMs,
			Summary:    firstLine(result, 140),
			IsError:    isFailure,
		})
	}

	if partIdx >= 0 {
		if g.PartsMu != nil {
			g.PartsMu.Lock()
		}
		if partIdx < len(*g.Parts) {
			(*g.Parts)[partIdx].Result = result
			(*g.Parts)[partIdx].IsError = isFailure
			(*g.Parts)[partIdx].DurationMs = durationMs
		}
		if g.PartsMu != nil {
			g.PartsMu.Unlock()
		}
	}

	if g.Memory != nil {
		g.Memory.RecordTool(name, argsJSON, result, isFailure)
		if g.Deps != nil && g.Deps.DB != nil {
			g.Memory.Persist(ctx, g.Deps.DB, g.RunID)
		}
	}

	if execErr != nil {
		return result, execErr
	}
	return result, nil
}
