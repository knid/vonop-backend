package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/utils"
)

const agentMemoryMarker = "AGENT RUN MEMORY"

type AgentRunMemory struct {
	OriginalRequest   string    `json:"original_request,omitempty"`
	CurrentGoal       string    `json:"current_goal,omitempty"`
	AgentID           string    `json:"agent_id,omitempty"`
	SkillIDs          []string  `json:"skill_ids,omitempty"`
	RequiredSkillIDs  []string  `json:"required_skill_ids,omitempty"`
	LoadedSkillIDs    []string  `json:"loaded_skill_ids,omitempty"`
	AvailableSkillIDs []string  `json:"available_skill_ids,omitempty"`
	Completed         []string  `json:"completed,omitempty"`
	Remaining         []string  `json:"remaining,omitempty"`
	FilesChanged      []string  `json:"files_changed,omitempty"`
	FilesRead         []string  `json:"files_read,omitempty"`
	Decisions         []string  `json:"decisions,omitempty"`
	Errors            []string  `json:"errors,omitempty"`
	NextAction        string    `json:"next_action,omitempty"`
	Compactions       int       `json:"compactions,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

func NewAgentRunMemory(request string) *AgentRunMemory {
	request = strings.TrimSpace(request)
	return &AgentRunMemory{
		OriginalRequest: request,
		CurrentGoal:     firstLine(request, 240),
		NextAction:      "Continue the user request from the latest tool results and TODO state.",
		UpdatedAt:       time.Now(),
	}
}

func ParseAgentRunMemory(raw json.RawMessage) *AgentRunMemory {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	var mem AgentRunMemory
	if err := json.Unmarshal(raw, &mem); err != nil {
		return nil
	}
	return &mem
}

func (m *AgentRunMemory) RawJSON() json.RawMessage {
	if m == nil {
		return json.RawMessage("{}")
	}
	m.UpdatedAt = time.Now()
	raw, err := json.Marshal(m)
	if err != nil || len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

func (m *AgentRunMemory) Persist(ctx context.Context, db *database.Database, runID uuid.UUID) {
	if m == nil || db == nil || runID == uuid.Nil {
		return
	}
	_ = db.UpdateAgentRunMemory(ctx, runID, m.RawJSON())
}

func (m *AgentRunMemory) RecordTool(name, args, result string, isError bool) {
	if m == nil {
		return
	}
	m.UpdatedAt = time.Now()
	paths := extractToolPaths(args)
	if name == "create_design" {
		paths = []string{designFilePath}
	}
	if isError {
		m.Errors = appendLimited(m.Errors, fmt.Sprintf("%s: %s", name, firstLine(result, 220)), 30)
		m.NextAction = "Review the failed tool result, fix the cause, and continue the current user request."
		return
	}
	switch name {
	case "create_design", "write_files", "edit_file", "multi_edit", "patch_files", "delete_file":
		m.FilesChanged = appendUniqueLimited(m.FilesChanged, paths, 80)
		if len(paths) > 0 {
			m.Completed = appendLimited(m.Completed, fmt.Sprintf("%s: %s", name, strings.Join(paths, ", ")), 40)
		} else {
			m.Completed = appendLimited(m.Completed, fmt.Sprintf("%s completed", name), 40)
		}
	case "read_files", "read_attachment", "read_mobile_config", "read_build_logs", "read_deploy_logs", "list_files", "grep":
		m.FilesRead = appendUniqueLimited(m.FilesRead, paths, 80)
		if len(paths) > 0 {
			m.Decisions = appendLimited(m.Decisions, fmt.Sprintf("Inspected %s via %s", strings.Join(paths, ", "), name), 30)
		}
	case "list_skills", "search_skills":
		m.Decisions = appendLimited(m.Decisions, fmt.Sprintf("Reviewed skill catalog via %s.", name), 30)
	case "load_skill":
		if id := extractToolStringArg(args, "id"); id != "" {
			m.LoadedSkillIDs = appendUniqueLimited(m.LoadedSkillIDs, []string{id}, 40)
			m.SkillIDs = appendUniqueLimited(m.SkillIDs, []string{id}, 40)
			m.Decisions = appendLimited(m.Decisions, fmt.Sprintf("Loaded skill %s.", id), 30)
		}
	case "update_todos":
		m.Decisions = appendLimited(m.Decisions, "Updated structured TODO state.", 30)
	case "trigger_build_after_fix", "trigger_web_deploy":
		m.Completed = appendLimited(m.Completed, firstNonEmpty(firstLine(result, 180), name+" completed"), 40)
	}
	m.NextAction = "Use the latest file tree, TODO state, and recent tool results to finish the current user request."
}

func (m *AgentRunMemory) RecordRuntime(agentID string, requiredSkills, availableSkills []SkillFile) {
	if m == nil {
		return
	}
	m.AgentID = agentID
	m.RequiredSkillIDs = skillIDs(requiredSkills)
	m.AvailableSkillIDs = skillIDs(availableSkills)
	m.SkillIDs = appendUniqueLimited(append([]string(nil), m.RequiredSkillIDs...), m.LoadedSkillIDs, 80)
	m.UpdatedAt = time.Now()
}

func (m *AgentRunMemory) RecordLoadedSkill(skill SkillFile) {
	if m == nil || skill.ID == "" {
		return
	}
	m.LoadedSkillIDs = appendUniqueLimited(m.LoadedSkillIDs, []string{skill.ID}, 40)
	m.SkillIDs = appendUniqueLimited(m.SkillIDs, []string{skill.ID}, 80)
	m.Decisions = appendLimited(m.Decisions, fmt.Sprintf("Loaded skill %s.", skill.ID), 30)
	m.UpdatedAt = time.Now()
}

func (m *AgentRunMemory) MarkCompacted() {
	if m == nil {
		return
	}
	m.Compactions++
	m.UpdatedAt = time.Now()
}

func (m *AgentRunMemory) Render(todos *TodoList) string {
	if m == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(agentMemoryMarker)
	sb.WriteString("\n================\n\n")
	writeMemoryField(&sb, "Original request", m.OriginalRequest)
	writeMemoryField(&sb, "Current goal", m.CurrentGoal)
	writeMemoryField(&sb, "Agent", m.AgentID)
	writeMemoryList(&sb, "Active skills", m.SkillIDs)
	writeMemoryList(&sb, "Required skills", m.RequiredSkillIDs)
	writeMemoryList(&sb, "Loaded optional skills", m.LoadedSkillIDs)
	writeMemoryList(&sb, "Available skills", m.AvailableSkillIDs)
	writeMemoryList(&sb, "Completed work", m.Completed)
	writeMemoryList(&sb, "Changed files", m.FilesChanged)
	writeMemoryList(&sb, "Important files inspected", m.FilesRead)
	writeMemoryList(&sb, "Decisions / facts", m.Decisions)
	writeMemoryList(&sb, "Errors / blockers", m.Errors)
	if todos != nil {
		if rendered := todos.Render(); rendered != "" {
			sb.WriteString("\n")
			sb.WriteString(rendered)
		}
	}
	writeMemoryField(&sb, "Next action", m.NextAction)
	if m.Compactions > 0 {
		fmt.Fprintf(&sb, "\nCompactions so far: %d\n", m.Compactions)
	}
	sb.WriteString("\nTreat this block as the compact source of truth for older omitted tool history. Recent messages and direct tool results still override this summary.\n")
	return sb.String()
}

func writeMemoryField(sb *strings.Builder, label, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	fmt.Fprintf(sb, "%s: %s\n", label, value)
}

func writeMemoryList(sb *strings.Builder, label string, values []string) {
	values = compactStrings(values, 12)
	if len(values) == 0 {
		return
	}
	sb.WriteString("\n")
	sb.WriteString(label)
	sb.WriteString(":\n")
	for _, v := range values {
		fmt.Fprintf(sb, "- %s\n", v)
	}
}

func compactLLMHistory(messages []utils.Message, memory *AgentRunMemory, todos *TodoList, keepTail int) []utils.Message {
	if memory == nil || keepTail <= 0 || len(messages) <= keepTail+3 {
		return messages
	}

	filtered := make([]utils.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "user" && strings.HasPrefix(msg.Content, agentMemoryMarker) {
			continue
		}
		filtered = append(filtered, msg)
	}

	systemCount := 0
	for systemCount < len(filtered) && filtered[systemCount].Role == "system" {
		systemCount++
	}

	pinnedIdx := -1
	for i := systemCount; i < len(filtered); i++ {
		if filtered[i].Role == "user" && strings.HasPrefix(filtered[i].Content, "CURRENT RUN CONTEXT\n") {
			pinnedIdx = i
			break
		}
	}

	tailStart := len(filtered) - keepTail
	if tailStart < systemCount {
		tailStart = systemCount
	}
	for tailStart < len(filtered) && filtered[tailStart].Role == "tool" {
		tailStart++
	}

	out := make([]utils.Message, 0, systemCount+keepTail+2)
	out = append(out, filtered[:systemCount]...)
	out = append(out, utils.Message{Role: "user", Content: memory.Render(todos)})
	if pinnedIdx >= systemCount && pinnedIdx < tailStart {
		out = append(out, filtered[pinnedIdx])
	}
	out = append(out, filtered[tailStart:]...)
	if llmMessagesEqual(out, messages) {
		return messages
	}
	memory.MarkCompacted()
	return out
}

func llmMessagesEqual(a, b []utils.Message) bool {
	return reflect.DeepEqual(a, b)
}

func trimToolResultForLLM(toolName, result string, maxChars int) string {
	result = strings.TrimSpace(result)
	if maxChars <= 0 || len(result) <= maxChars {
		return result
	}
	head := maxChars / 2
	tail := maxChars - head
	if strings.Contains(toolName, "log") {
		head = maxChars / 4
		tail = maxChars - head
	}
	return result[:head] + fmt.Sprintf("\n\n[trimmed %d chars from older %s result]\n\n", len(result)-maxChars, toolName) + result[len(result)-tail:]
}

func trimToolArgsForLLM(toolName, args string, maxChars int) string {
	if maxChars <= 0 || len(args) <= maxChars {
		return args
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return args[:maxChars] + "...(trimmed)"
	}
	for _, key := range []string{"content", "old_string", "new_string", "replacement", "text"} {
		if _, ok := m[key]; ok {
			m[key] = json.RawMessage(`"[trimmed from older tool call args]"`)
		}
	}
	if files, ok := m["files"]; ok {
		m["files"] = stripBatchFilePreview(files)
	}
	b, err := json.Marshal(m)
	if err != nil || len(b) > maxChars {
		return compactToolArgsFallback(m)
	}
	return string(b)
}

func compactToolArgsFallback(args map[string]json.RawMessage) string {
	compact := map[string]json.RawMessage{
		"_trimmed": json.RawMessage(`"older tool call args omitted"`),
	}
	for _, key := range []string{"path", "target_path", "file", "filepath", "description"} {
		if v, ok := args[key]; ok {
			compact[key] = v
		}
	}
	if paths, ok := args["paths"]; ok {
		compact["paths"] = paths
	}
	if files, ok := args["files"]; ok {
		compact["files"] = stripBatchFilePreview(files)
	}
	b, err := json.Marshal(compact)
	if err != nil {
		return `{"_trimmed":"older tool call args omitted"}`
	}
	return string(b)
}

func extractToolPaths(args string) []string {
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for k, val := range t {
				lk := strings.ToLower(k)
				if lk == "path" || lk == "file" || lk == "filepath" || lk == "target_path" {
					if s, ok := val.(string); ok {
						addPath(&out, seen, s)
					}
				}
				if lk == "paths" {
					walk(val)
				}
				if lk == "files" {
					walk(val)
				}
			}
		case []any:
			for _, item := range t {
				if s, ok := item.(string); ok {
					addPath(&out, seen, s)
				} else {
					walk(item)
				}
			}
		}
	}
	walk(v)
	sort.Strings(out)
	return out
}

func extractToolStringArg(args, key string) string {
	var v map[string]any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return ""
	}
	s, _ := v[key].(string)
	return strings.TrimSpace(s)
}

func addPath(out *[]string, seen map[string]bool, path string) {
	path = strings.TrimSpace(path)
	if path == "" || seen[path] {
		return
	}
	seen[path] = true
	*out = append(*out, path)
}

func appendLimited(values []string, value string, max int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	values = append(values, value)
	if max > 0 && len(values) > max {
		values = values[len(values)-max:]
	}
	return values
}

func appendUniqueLimited(values []string, add []string, max int) []string {
	seen := make(map[string]bool, len(values)+len(add))
	out := make([]string, 0, len(values)+len(add))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range add {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if max > 0 && len(out) > max {
		out = out[len(out)-max:]
	}
	return out
}

func compactStrings(values []string, max int) []string {
	if len(values) == 0 {
		return nil
	}
	start := 0
	if max > 0 && len(values) > max {
		start = len(values) - max
	}
	out := make([]string, 0, len(values)-start)
	seen := map[string]bool{}
	for _, v := range values[start:] {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
