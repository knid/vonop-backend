package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentRunMemoryRecordToolTracksChangedAndReadFiles(t *testing.T) {
	mem := NewAgentRunMemory("make the app nicer")

	mem.RecordTool("read_files", `{"paths":["app/page.tsx","app/globals.css"]}`, "contents", false)
	mem.RecordTool("edit_file", `{"path":"app/page.tsx","old_string":"x","new_string":"y"}`, "updated", false)

	assert.ElementsMatch(t, []string{"app/page.tsx", "app/globals.css"}, mem.FilesRead)
	assert.Equal(t, []string{"app/page.tsx"}, mem.FilesChanged)
	assert.Contains(t, strings.Join(mem.Completed, "\n"), "edit_file: app/page.tsx")
}

func TestCompactLLMHistoryKeepsSystemMemoryPinnedAndTail(t *testing.T) {
	mem := NewAgentRunMemory("build dashboard")
	mem.RecordTool("edit_file", `{"path":"app/page.tsx"}`, "updated", false)
	messages := []utils.Message{
		{Role: "system", Content: "base"},
		{Role: "user", Content: "old user"},
		{Role: "assistant", Content: "old assistant"},
		{Role: "user", Content: "CURRENT RUN CONTEXT\n===================\n\nfiles\n\nUSER REQUEST\n============\n\nbuild dashboard"},
		{Role: "assistant", Content: "recent"},
		{Role: "user", Content: "[System] nudge"},
	}

	out := compactLLMHistory(messages, mem, nil, 2)
	require.GreaterOrEqual(t, len(out), 4)
	assert.Equal(t, "system", out[0].Role)
	assert.Equal(t, "base", out[0].Content)
	assert.Equal(t, "user", out[1].Role)
	assert.Contains(t, out[1].Content, agentMemoryMarker)
	assert.Contains(t, out[1].Content, "app/page.tsx")
	assert.Contains(t, out[2].Content, "CURRENT RUN CONTEXT")
	assert.Equal(t, "assistant", out[len(out)-2].Role)
	assert.Equal(t, "user", out[len(out)-1].Role)
	assert.Equal(t, 1, mem.Compactions)
}

func TestTrimToolArgsForLLMDropsBulkyFields(t *testing.T) {
	args := `{"path":"app/page.tsx","old_string":"` + strings.Repeat("a", 2000) + `","new_string":"` + strings.Repeat("b", 2000) + `"}`
	got := trimToolArgsForLLM("edit_file", args, 500)

	assert.Contains(t, got, "app/page.tsx")
	assert.Contains(t, got, "trimmed from older tool call args")
	assert.Less(t, len(got), len(args))
}

func TestTrimToolArgsForLLMFallbackReturnsValidJSON(t *testing.T) {
	args := `{"path":"app/page.tsx","description":"` + strings.Repeat("d", 2000) + `","old_string":"` + strings.Repeat("a", 2000) + `","new_string":"` + strings.Repeat("b", 2000) + `"}`
	got := trimToolArgsForLLM("edit_file", args, 100)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &parsed))
	assert.Equal(t, "older tool call args omitted", parsed["_trimmed"])
	assert.Equal(t, "app/page.tsx", parsed["path"])
}

func TestAgentRunMemoryDoesNotMarkFailedWriteCompleted(t *testing.T) {
	mem := NewAgentRunMemory("change page")

	mem.RecordTool("edit_file", `{"path":"app/page.tsx"}`, "old string not found", true)

	assert.Empty(t, mem.FilesChanged)
	assert.Empty(t, mem.Completed)
	assert.Contains(t, strings.Join(mem.Errors, "\n"), "old string not found")
}

func TestTrimToolResultForLLMKeepsHeadAndTail(t *testing.T) {
	got := trimToolResultForLLM("read_files", strings.Repeat("a", 100)+"MIDDLE"+strings.Repeat("z", 100), 80)

	assert.Contains(t, got, "trimmed")
	assert.True(t, strings.HasPrefix(got, strings.Repeat("a", 40)))
	assert.True(t, strings.HasSuffix(got, strings.Repeat("z", 40)))
}
