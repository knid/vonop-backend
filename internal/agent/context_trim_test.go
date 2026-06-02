package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrimToolArgsForHistory_WriteFile(t *testing.T) {
	args := `{"path":"app/page.tsx","content":"` + strings.Repeat("x", 5000) + `","description":"home","overwrite":true,"reason":"replace generated page"}`

	got := trimToolArgsForHistory("write_file", args)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &out))
	assert.Equal(t, "app/page.tsx", out["path"])
	assert.Equal(t, "home", out["description"])
	assert.Equal(t, true, out["overwrite"])
	assert.Equal(t, "replace generated page", out["reason"])
	assert.Equal(t, float64(5000), out["content_bytes"])
	assert.NotContains(t, got, strings.Repeat("x", 100))
	assert.Contains(t, out["content"], "[trimmed]")
}

func TestTrimToolArgsForHistory_WriteFiles(t *testing.T) {
	args := `{"files":[{"path":"app/page.tsx","content":"` + strings.Repeat("a", 3000) + `","description":"home"},{"path":"components/x.tsx","content":"` + strings.Repeat("b", 4000) + `","overwrite":true,"reason":"refresh"}]}`

	got := trimToolArgsForHistory("write_files", args)

	var out struct {
		Files []map[string]any `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(got), &out))
	require.Len(t, out.Files, 2)
	assert.Equal(t, "app/page.tsx", out.Files[0]["path"])
	assert.Equal(t, float64(3000), out.Files[0]["content_bytes"])
	assert.Equal(t, "components/x.tsx", out.Files[1]["path"])
	assert.Equal(t, float64(4000), out.Files[1]["content_bytes"])
	assert.NotContains(t, got, strings.Repeat("a", 100))
	assert.NotContains(t, got, strings.Repeat("b", 100))
}

func TestTrimToolArgsForHistory_EditTools(t *testing.T) {
	longOld := strings.Repeat("old", 1000)
	longNew := strings.Repeat("new", 1000)

	edit := trimToolArgsForHistory("edit_file", `{"path":"app/page.tsx","old_string":"`+longOld+`","new_string":"`+longNew+`"}`)
	var editOut map[string]any
	require.NoError(t, json.Unmarshal([]byte(edit), &editOut))
	assert.Equal(t, float64(len(longOld)), editOut["old_string_bytes"])
	assert.Equal(t, float64(len(longNew)), editOut["new_string_bytes"])
	assert.NotContains(t, edit, strings.Repeat("old", 20))
	assert.NotContains(t, edit, strings.Repeat("new", 20))

	multi := trimToolArgsForHistory("multi_edit", `{"path":"app/page.tsx","edits":[{"old_string":"`+longOld+`","new_string":"`+longNew+`"},{"old_string":"short","new_string":"next"}]}`)
	var multiOut map[string]any
	require.NoError(t, json.Unmarshal([]byte(multi), &multiOut))
	assert.Equal(t, float64(2), multiOut["edit_count"])
	assert.NotContains(t, multi, strings.Repeat("old", 20))
}

func TestTrimToolArgsForHistory_InvalidHandledArgsRemainValidJSON(t *testing.T) {
	got := trimToolArgsForHistory("write_file", `{"path":`)

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(got), &out))
	assert.Equal(t, true, out["trimmed"])
	assert.Equal(t, true, out["invalid_json"])
	assert.Equal(t, "write_file", out["tool"])
}

func TestTrimToolResultForHistory_ReadFiles(t *testing.T) {
	full := `<tool_output tool="read_files">===== app/page.tsx =====
` + strings.Repeat("page content\n", 500) + `

===== components/card.tsx =====
ERROR: file not found
</tool_output>`

	got := trimToolResultForHistory("read_files", full)

	assert.Contains(t, got, `<tool_output tool="read_files">`)
	assert.Contains(t, got, "app/page.tsx")
	assert.Contains(t, got, "components/card.tsx")
	assert.Contains(t, got, "ERROR: file not found")
	assert.Contains(t, got, "call read_files again")
	assert.NotContains(t, got, strings.Repeat("page content\n", 20))
	assert.NotEmpty(t, got)
}

func TestTrimToolResultForHistory_NonReadFilesUnchanged(t *testing.T) {
	result := `<tool_output tool="edit_file">edited app/page.tsx</tool_output>`
	assert.Equal(t, result, trimToolResultForHistory("edit_file", result))
}
