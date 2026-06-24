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

func shouldCacheToolResult(toolName string, result string) bool {
	switch toolName {
	case "edit_file", "multi_edit":
		var payload struct {
			Changed   *bool  `json:"changed"`
			ErrorCode string `json:"error_code"`
		}
		if json.Unmarshal([]byte(unwrapToolOutputResult(result)), &payload) == nil && payload.Changed != nil && !*payload.Changed {
			switch payload.ErrorCode {
			case "old_string_not_found", "old_string_ambiguous", "stale_read", "old_string_empty":
				return false
			}
		}
	}
	return true
}

func unwrapToolOutputResult(result string) string {
	const closeTag = "</tool_output>"
	if !strings.HasPrefix(result, "<tool_output ") || !strings.HasSuffix(result, closeTag) {
		return result
	}
	start := strings.Index(result, ">")
	if start < 0 {
		return result
	}
	return result[start+1 : len(result)-len(closeTag)]
}

func exactStringEditRecoveryInstruction(toolName string, result string) string {
	if shouldCacheToolResult(toolName, result) {
		return ""
	}
	return "The exact-string edit failed because the file state or match was invalid. Do not retry the same edit. Call read_file_range on the target file or grep for the current target text first, then issue one corrected edit."
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
		"read_deploy_logs":
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

type toolCallWindowMode int

const (
	toolCallWindowDrop toolCallWindowMode = iota
	toolCallWindowSummarize
	toolCallWindowTruncate
	toolCallWindowFull
)

func trimToolCallsForWindow(
	currentMessages []Message,
	keepFull int,
	keepTruncated int,
	keepSummarized int,
	trimArgs func(toolName, args string) string,
	trimResults func(toolName, result string) string,
	summarizeTool func(toolName, args, result string, isError bool) (string, string),
) []Message {
	if keepFull < 0 {
		keepFull = 0
	}
	if keepTruncated < 0 {
		keepTruncated = 0
	}
	if keepSummarized < 0 {
		keepSummarized = 0
	}
	type ref struct {
		id   string
		name string
		args string
	}
	var refs []ref
	for _, msg := range currentMessages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				name := ""
				args := ""
				if tc.Function != nil {
					name = tc.Function.Name
					args = tc.Function.Arguments
				}
				refs = append(refs, ref{id: tc.ID, name: name, args: args})
			}
		}
	}
	if len(refs) == 0 {
		return currentMessages
	}
	fullStart := len(refs) - keepFull
	if fullStart < 0 {
		fullStart = 0
	}
	truncatedStart := fullStart - keepTruncated
	if truncatedStart < 0 {
		truncatedStart = 0
	}
	summarizedStart := truncatedStart - keepSummarized
	if summarizedStart < 0 {
		summarizedStart = 0
	}
	type toolCallWindowRef struct {
		mode toolCallWindowMode
		name string
		args string
	}
	modes := make(map[string]toolCallWindowRef, len(refs))
	for i, r := range refs {
		switch {
		case i >= fullStart:
			modes[r.id] = toolCallWindowRef{mode: toolCallWindowFull, name: r.name, args: r.args}
		case i >= truncatedStart:
			modes[r.id] = toolCallWindowRef{mode: toolCallWindowTruncate, name: r.name, args: r.args}
		case i >= summarizedStart:
			modes[r.id] = toolCallWindowRef{mode: toolCallWindowSummarize, name: r.name, args: r.args}
		default:
			modes[r.id] = toolCallWindowRef{mode: toolCallWindowDrop, name: r.name, args: r.args}
		}
	}
	out := make([]Message, 0, len(currentMessages))
	for _, msg := range currentMessages {
		switch msg.Role {
		case "assistant":
			if len(msg.ToolCalls) == 0 {
				out = append(out, msg)
				continue
			}
			kept := make([]ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				ref, ok := modes[tc.ID]
				if !ok || ref.mode == toolCallWindowFull {
					kept = append(kept, tc)
					continue
				}
				if ref.mode == toolCallWindowDrop {
					continue
				}
				if ref.mode == toolCallWindowSummarize {
					if summarizeTool == nil {
						continue
					}
					if tc.Function != nil {
						args, _ := summarizeTool(tc.Function.Name, tc.Function.Arguments, "", false)
						tc.Function.Arguments = args
					}
					kept = append(kept, tc)
					continue
				}
				if trimArgs != nil && tc.Function != nil {
					tc.Function.Arguments = trimArgs(tc.Function.Name, tc.Function.Arguments)
				}
				kept = append(kept, tc)
			}
			msg.ToolCalls = kept
			if len(msg.ToolCalls) == 0 && strings.TrimSpace(msg.Content) == "" {
				continue
			}
			out = append(out, msg)
		case "tool":
			ref, ok := modes[msg.ToolCallID]
			if ok && ref.mode == toolCallWindowDrop {
				continue
			}
			if ok && ref.mode == toolCallWindowSummarize && summarizeTool == nil {
				continue
			}
			if ok && ref.mode == toolCallWindowSummarize && summarizeTool != nil {
				name := msg.Name
				if name == "" {
					name = ref.name
				}
				_, result := summarizeTool(name, ref.args, msg.Content, msg.IsError)
				msg.Content = result
			}
			if ok && ref.mode == toolCallWindowTruncate && trimResults != nil {
				msg.Content = trimResults(msg.Name, msg.Content)
			}
			out = append(out, msg)
		default:
			out = append(out, msg)
		}
	}
	return out
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
		sb.WriteString("The previous recovery attempt also produced incomplete tool-call JSON. Focus on emitting one complete, syntactically valid JSON argument object now. Prefer multi_edit when it expresses the change cleanly, because its argument shape is easier to complete reliably.\n\n")
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
		sb.WriteString("\nThe incomplete call was write_files. This means the streamed tool-call JSON ended incomplete due to provider payload size limits; it does not mean write_files cannot create the requested file. For this recovery attempt, DO NOT attempt to write the entire file at once. Break the file into smaller logical chunks (e.g., separate components) and call write_files multiple times, or use multi_edit for targeted replacements. Do not restart the task, do not repeat successful work, and do not respond with plain text unless the task is already complete.")
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
