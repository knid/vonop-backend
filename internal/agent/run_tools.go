package agent

import (
	"encoding/json"

	"github.com/knid/wemob/internal/utils"
)

// registryToUtilsTools maps the agent Registry to the []utils.Tool shape
// StreamWithTools expects.
func registryToUtilsTools(r *Registry) []utils.Tool {
	out := make([]utils.Tool, 0, len(r.tools)+len(r.serverTools))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			out = append(out, utils.Tool{
				Type: "function",
				Function: utils.ToolFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  t.Schema(),
				},
			})
		}
	}
	for _, st := range r.serverTools {
		if st.Tool != nil {
			out = append(out, *st.Tool)
		}
	}
	return out
}

func trackerDelta(t *utils.TokenTracker) TokenUsage {
	p, c, _, _ := t.Totals()
	cr, cw := t.CacheTotals()
	return TokenUsage{Input: p, Output: c, CacheRead: cr, CacheWrite: cw}
}

// stripToolInputForPreview drops bulky fields from file-mutating tool input
// before stamping it into ToolCallStartEvent.InputPreview.
func stripToolInputForPreview(name string, argsJSON string) json.RawMessage {
	switch name {
	case "write_file", "write_files", "edit_file", "multi_edit", "delete_file":
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
			return json.RawMessage("{}")
		}
		stripped := map[string]json.RawMessage{}
		if p, ok := m["path"]; ok {
			stripped["path"] = p
		}
		if d, ok := m["description"]; ok {
			stripped["description"] = d
		}
		if files, ok := m["files"]; ok {
			stripped["files"] = stripBatchFilePreview(files)
		}
		out, err := json.Marshal(stripped)
		if err != nil {
			return json.RawMessage("{}")
		}
		return out
	default:
		return json.RawMessage(argsJSON)
	}
}

func stripBatchFilePreview(files json.RawMessage) json.RawMessage {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(files, &arr); err != nil {
		return json.RawMessage("[]")
	}
	out := make([]map[string]json.RawMessage, 0, len(arr))
	for _, f := range arr {
		item := map[string]json.RawMessage{}
		if p, ok := f["path"]; ok {
			item["path"] = p
		}
		if d, ok := f["description"]; ok {
			item["description"] = d
		}
		out = append(out, item)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}
