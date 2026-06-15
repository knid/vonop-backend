package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gatewayTestTool struct {
	result  string
	isError bool
	err     error
}

func (t gatewayTestTool) Name() string         { return "test_tool" }
func (gatewayTestTool) Description() string    { return "test tool" }
func (gatewayTestTool) Schema() map[string]any { return map[string]any{"type": "object"} }
func (t gatewayTestTool) Execute(_ context.Context, _ *RunContext, input json.RawMessage) (string, bool, error) {
	return t.result, t.isError, t.err
}

type gatewayTestEmitter struct {
	events []Event
}

func (e *gatewayTestEmitter) Emit(eventType string, data any) bool {
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}
func (e *gatewayTestEmitter) Buffer() []Event { return e.events }
func (e *gatewayTestEmitter) Close()          {}

func TestToolGatewayExecuteEmitsStartAndResult(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gatewayTestTool{result: "<tool_output tool=\"test_tool\">ok</tool_output>"})
	emitter := &gatewayTestEmitter{}
	tracker := utils.NewTokenTracker()

	gateway := ToolGateway{
		RunID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Turn:     2,
		Registry: registry,
		RC:       &RunContext{},
		Emitter:  emitter,
		Tracker:  tracker,
	}

	result, err := gateway.Execute(context.Background(), "test_tool", `{"x":1}`)

	require.NoError(t, err)
	assert.Contains(t, result, "<tool_output")
	assert.Len(t, emitter.events, 2)
	assert.Equal(t, EventToolCallStart, emitter.events[0].Type)
	assert.Equal(t, EventToolCallResult, emitter.events[1].Type)
	assert.Equal(t, []string{"test_tool"}, tracker.ToolsUsed())
}

func TestToolGatewayUpdatesAssistantParts(t *testing.T) {
	registry := NewRegistry()
	registry.Register(gatewayTestTool{result: "ok"})
	parts := make([]models.ChatMessagePart, 0)

	gateway := ToolGateway{
		RunID:    uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Turn:     1,
		Registry: registry,
		RC:       &RunContext{},
		Parts:    &parts,
	}

	result, err := gateway.Execute(t.Context(), "test_tool", `{"x":1}`)

	require.NoError(t, err)
	assert.Contains(t, result, "ok")
	require.Len(t, parts, 1)
	assert.Equal(t, "tool_call", parts[0].Type)
	assert.Equal(t, "test_tool", parts[0].Name)
	assert.JSONEq(t, `{"x":1}`, string(parts[0].Input))
	assert.Contains(t, parts[0].Result, "ok")
	assert.False(t, parts[0].IsError)
}
