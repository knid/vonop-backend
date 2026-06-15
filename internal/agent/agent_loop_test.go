package agent

import (
	"testing"

	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultAgentLoopRequiresProvider(t *testing.T) {
	loop := DefaultAgentLoop{}

	_, err := loop.Execute(t.Context(), AgentLoopInput{
		Model:    "claude-sonnet-4-6",
		Messages: []utils.Message{{Role: "user", Content: "hi"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing LLM provider configuration")
}

func TestAgentLoopInputCarriesCallbacks(t *testing.T) {
	var chunked string
	input := AgentLoopInput{
		OnChunk: func(chunk string) {
			chunked += chunk
		},
	}

	input.OnChunk("a")
	input.OnChunk("b")

	assert.Equal(t, "ab", chunked)
}
