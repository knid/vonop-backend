package utils

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// normalizeToolCacheKey produces a canonical cache key for tool dedup.
// It normalizes JSON arguments so semantically identical calls match.
func normalizeToolCacheKey(name, args string) string {
	var obj any
	if json.Unmarshal([]byte(args), &obj) == nil {
		if canonical, err := json.Marshal(obj); err == nil {
			return name + ":" + string(canonical)
		}
	}
	return name + ":" + args
}

// sanitizeToolError strips internal details from tool execution errors before
// the message is fed back into the LLM context.
func sanitizeToolError(toolName string, err error) string {
	msg := err.Error()
	for _, sensitive := range []string{"sql:", "pq:", "s3:", "dial tcp", "connect:", "tls:", "x509:"} {
		if strings.Contains(strings.ToLower(msg), sensitive) {
			slog.Error("Tool execution error (sanitized)", "tool", toolName, "error", msg)
			return fmt.Sprintf("Tool execution failed for %s: internal error, please try again", toolName)
		}
	}
	return fmt.Sprintf("Tool execution failed for %s: %v", toolName, err)
}

// isParallelSafeTool returns true for tools that do not mutate shared project
// state and can run concurrently without changing agent semantics.
func isParallelSafeTool(name string) bool {
	switch name {
	case "read_files", "list_files", "grep", "read_attachment", "generate_image",
		"read_build_logs", "read_deploy_logs", "read_mobile_config":
		return true
	default:
		return false
	}
}

type toolRoundTrimState struct {
	toolCallIDs    []string
	argsTrimmed    bool
	resultsTrimmed bool
}

func trimToolRoundsOlderThan(
	currentMessages []Message,
	toolRoundHistory []toolRoundTrimState,
	keepRecentToolRounds int,
	trimArgs func(toolName, args string) string,
	trimResults func(toolName, result string) string,
) []toolRoundTrimState {
	if keepRecentToolRounds < 0 {
		keepRecentToolRounds = 0
	}
	cutoff := len(toolRoundHistory) - keepRecentToolRounds
	if cutoff <= 0 {
		return toolRoundHistory
	}
	for i := 0; i < cutoff; i++ {
		rec := &toolRoundHistory[i]
		ids := make(map[string]bool, len(rec.toolCallIDs))
		for _, id := range rec.toolCallIDs {
			if id != "" {
				ids[id] = true
			}
		}
		if len(ids) == 0 {
			rec.argsTrimmed = true
			rec.resultsTrimmed = true
			continue
		}
		if trimArgs != nil && !rec.argsTrimmed {
			for msgIdx := range currentMessages {
				msg := &currentMessages[msgIdx]
				matched := false
				for tcIdx := range msg.ToolCalls {
					tc := &msg.ToolCalls[tcIdx]
					if !ids[tc.ID] {
						continue
					}
					matched = true
					if tc.Function != nil {
						tc.Function.Arguments = trimArgs(tc.Function.Name, tc.Function.Arguments)
					}
				}
				if matched {
					msg.ReasoningContent = ""
				}
			}
			rec.argsTrimmed = true
		}
		if trimResults != nil && !rec.resultsTrimmed {
			for msgIdx := range currentMessages {
				msg := &currentMessages[msgIdx]
				if msg.Role == "tool" && ids[msg.ToolCallID] {
					msg.Content = trimResults(msg.Name, msg.Content)
				}
			}
			rec.resultsTrimmed = true
		}
	}
	return toolRoundHistory[cutoff:]
}

func incompleteToolCallRecoveryMessage(finishReason string, toolCallsMap map[int]*ToolCall, attempt, maxAttempts int) string {
	details := incompleteToolCallDetails(toolCallsMap)

	var sb strings.Builder
	sb.WriteString("The previous response started a tool call but ended before the tool call contained complete valid JSON")
	if finishReason != "" {
		fmt.Fprintf(&sb, " (finish_reason=%q)", finishReason)
	}
	sb.WriteString(".\n")
	fmt.Fprintf(&sb, "Recovery attempt %d of %d.\n\n", attempt, maxAttempts)
	if attempt > 1 {
		sb.WriteString("The previous recovery attempt also produced incomplete tool-call JSON. Focus on emitting one complete, syntactically valid JSON argument object now. Prefer patch_files or multi_edit when they express the change cleanly, because their argument shape is easier to complete reliably.\n\n")
	}
	sb.WriteString("Incomplete tool call(s):\n")
	for _, detail := range details {
		name := detail.Name
		if name == "" {
			name = "(unknown)"
		}
		fmt.Fprintf(&sb, "- %s arguments so far: %s\n", name, truncateBody(detail.ArgumentsSoFar, 700))
	}
	if incompleteToolCallsInclude(toolCallsMap, "write_files") {
		sb.WriteString("\nThe incomplete call was write_files. This means the streamed tool-call JSON ended incomplete due to provider payload size limits; it does not mean write_files cannot create the requested file. For this recovery attempt, DO NOT attempt to write the entire file at once. Break the file into smaller logical chunks (e.g., separate components) and call write_files multiple times, or use patch_files/multi_edit for targeted replacements. Do not restart the task, do not repeat successful work, and do not respond with plain text unless the task is already complete.")
	} else {
		sb.WriteString("\nRe-issue only the missing or incomplete tool call with complete valid JSON. Do not restart the task, do not repeat successful work, and do not respond with plain text unless the task is already complete.")
	}
	return sb.String()
}

func incompleteToolCallDetails(toolCallsMap map[int]*ToolCall) []IncompleteToolCallDetail {
	indices := make([]int, 0, len(toolCallsMap))
	for idx := range toolCallsMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	details := make([]IncompleteToolCallDetail, 0, len(indices))
	for _, idx := range indices {
		tc := toolCallsMap[idx]
		detail := IncompleteToolCallDetail{Index: idx}
		if tc != nil {
			detail.ID = tc.ID
			if tc.Function != nil {
				detail.Name = tc.Function.Name
				detail.ArgumentsSoFar = tc.Function.Arguments
				detail.ArgumentBytes = len(tc.Function.Arguments)
			}
		}
		details = append(details, detail)
	}
	return details
}

func incompleteToolCallsInclude(toolCallsMap map[int]*ToolCall, toolName string) bool {
	for _, tc := range toolCallsMap {
		if tc != nil && tc.Function != nil && tc.Function.Name == toolName {
			return true
		}
	}
	return false
}

func filterToolsByDeniedNames(tools []Tool, denied map[string]bool) []Tool {
	if len(denied) == 0 {
		return tools
	}
	filtered := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if denied[t.Function.Name] {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered
}
