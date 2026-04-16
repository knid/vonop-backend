package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/utils"
)

// Budget limits (package-level defaults; can be overridden later if needed).
const (
	defaultMaxTurns            = 50
	defaultMaxToolCallsPerTurn = 20
	defaultMaxTokensPerRun     = 500_000
	defaultMaxWallclockMs      = 10 * 60 * 1000
)

// Run is the single entry point for both initial generation and chat edit.
//
// When project.Status == "draft" and the repo is empty, Run injects templates.
// Otherwise it loads the existing fileMap from the repo.
//
// On success: files are committed to git, AppAgentRun is finalised, credits
// are deducted, events are emitted, and nil is returned.
//
// On failure: the in-memory fileMap is discarded, git is defensively reset to
// pre-commit, AppAgentRun is marked failed, NO credits are deducted, error
// events are emitted, and a *Error is returned.
func Run(
	ctx context.Context,
	deps *RunDependencies,
	project *models.AppProject,
	user *models.User,
	message string,
	images []string,
	emitter EventEmitter,
) (runErr error) {
	runStart := time.Now()

	// Wall-clock deadline: wrap ctx with a max-lifetime timer.
	ctx, cancelDeadline := context.WithTimeout(ctx, time.Duration(defaultMaxWallclockMs)*time.Millisecond)
	defer cancelDeadline()

	// --- Step 1: enforce single-run-per-project invariant ---
	// Advisory pre-check (fast path for the common case). The authoritative
	// check is the partial unique index on app_agent_runs(app_project_id)
	// WHERE status='running', caught below at InsertAgentRun.
	if existing, existingErr := deps.DB.GetActiveAgentRun(project.ID); existingErr == nil && existing != nil {
		return New(CodeRunInProgress, "setup", "another run is already in progress for this project", nil)
	}

	// --- Step 2: open git repo (creates if missing) ---
	repo, err := deps.GitRepo.OpenOrInit(project.ID.String())
	if err != nil {
		return New(CodeGitError, "setup", "failed to open project repo", err)
	}

	// --- Step 3: first-run template injection (draft projects only) ---
	if project.Status == "draft" {
		existingFiles, _ := repo.ListFiles()
		if len(existingFiles) == 0 {
			if n, injErr := InjectTemplates(repo, project); injErr != nil {
				return New(CodeGitError, "setup", fmt.Sprintf("template injection failed after %d files", n), injErr)
			}
		}
	}

	// --- Step 4: load fileMap from disk ---
	rawFiles, err := repo.ReadAllFiles()
	if err != nil {
		return New(CodeGitError, "setup", "failed to read project files", err)
	}
	fileMap := NewSafeFileMap(rawFiles)

	// --- Step 5: pre-run commit baseline ---
	runID := uuid.New()
	preCommit, err := PreRunCommit(repo, runID.String())
	if err != nil {
		return New(CodeGitError, "setup", "failed to create pre-run snapshot", err)
	}

	// --- Step 6: insert AppAgentRun row ---
	provider := deps.LLMProvider
	model := resolveModel(deps.SecretManager, project)
	run := &models.AppAgentRun{
		ID:           runID,
		AppProjectID: project.ID,
		UserID:       user.ID,
		Status:       models.AgentRunStatusRunning,
		PreCommit:    preCommit,
		Model:        model,
		Provider:     provider.Name,
	}
	if _, err := deps.DB.InsertAgentRun(run); err != nil {
		// Check for the partial unique index violation (Postgres SQLSTATE 23505).
		if isUniqueViolation(err) {
			return New(CodeRunInProgress, "setup", "another run is already in progress for this project", err)
		}
		return New(CodeInternal, "setup", "failed to record run", err)
	}

	// --- Step 7: load conversation history (BEFORE inserting the current turn) ---
	history, err := deps.DB.GetRecentChatMessages(project.ID, 30)
	if err != nil {
		slog.Warn("failed to load history, starting fresh", "error", err, "project_id", project.ID)
		history = nil
	}

	// --- Step 8: persist the user message ---
	imagesJSON := mustJSON(normaliseImages(images))
	userParts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: message},
	})
	userMsgRow := &models.AppChatMessage{
		AppProjectID: project.ID,
		RunID:        &runID,
		Role:         "user",
		Content:      message,
		Images:       imagesJSON,
		ToolCalls:    []byte("[]"),
		Parts:        userParts,
	}
	if _, err := deps.DB.InsertChatMessage(userMsgRow); err != nil {
		return New(CodeInternal, "setup", "failed to persist user message", err)
	}
	run.UserMessageID = &userMsgRow.ID
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to update run user_message_id", "error", err, "run_id", runID)
	}

	// --- Step 9: build system messages + registry + tracker ---
	llmMessages := BuildSystemMessages(project, fileMap, deps)

	// Modal commands (/plan, /explain, /review) inject an extra system block
	// that forbids file mutations on the current turn only. We append it to
	// the leading contiguous system run — BEFORE convertHistory — so the
	// native Anthropic adapter's leading-system extraction picks it up as a
	// real system message instead of demoting it to a "[System] " user
	// prefix. History replay does NOT re-inject this: see convertHistory for
	// the rationale.
	if addendum := SlashCommandSystemAddendum(message); addendum != "" {
		llmMessages = append(llmMessages, utils.Message{
			Role:    "system",
			Content: addendum,
		})
	}

	llmMessages = append(llmMessages, convertHistory(history)...)

	// Append the current turn explicitly — we don't rely on history reload
	// to round-trip images, and we want the turn to land even if history
	// load failed. Slash command expansion happens here so the LLM sees the
	// full prompt body while the persisted DB row (above) keeps the literal
	// text the user typed.
	expandedMessage, _, _ := ExpandSlashCommand(message)
	userTurn := utils.Message{Role: "user", Content: expandedMessage}
	if len(images) > 0 {
		userTurn.Images = images
	}
	llmMessages = append(llmMessages, userTurn)

	registry := BuildRegistryFor(project, deps)

	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	// Opt-in PostHog LLM Analytics: record per-call input/output content so
	// the $ai_generation events carry full message context, and seed the
	// trace input with the user's prompt for the grouping $ai_trace event.
	tracker.TrackContent = true
	tracker.TraceInput = message
	ctx = utils.WithTokenTracker(ctx, tracker)

	// Bind the credit checker to the run's token tracker.
	var creditCheck func() bool
	if deps.CreditChecker != nil {
		creditCheck = deps.CreditChecker(tracker)
	}

	// Resolve the tool-round budget from remote config so it can be tuned
	// without a redeploy. Falls back to defaultMaxTurns when RC is unavailable
	// or the flag returns a non-positive value.
	maxTurns := defaultMaxTurns
	if deps.RemoteConfig != nil {
		if rcTurns := deps.RemoteConfig.GetInt(remoteconfig.AIBuilderToolMaxRounds, remoteconfig.Global); rcTurns > 0 {
			maxTurns = rcTurns
		}
	}

	rc := &RunContext{
		RunID:   runID,
		Project: project,
		User:    user,
		FileMap: fileMap,
		Emitter: emitter,
		Tracker: tracker,
		Deps:    deps,
	}

	// --- Step 10: emit run_start ---
	emitter.Emit(EventRunStart, RunStartEvent{
		RunID:     runID,
		PreCommit: preCommit,
		Model:     model,
		Provider:  provider.Name,
		MaxTurns:  maxTurns,
		Tools:     registry.Names(),
	})

	// --- Step 11: panic guard ---
	defer func() {
		if r := recover(); r != nil {
			slog.Error("agent.Run panicked", "error", r, "project_id", project.ID, "stack", string(debug.Stack()))
			runErr = finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
				CodeInternal, fmt.Sprintf("panic: %v", r), nil, preCommit, tracker, runStart)
		}
	}()

	// --- Step 12: agent loop ---
	//
	// `parts` is the chronological record of this assistant turn — text and
	// tool_call blocks in the order the model emitted them. narrationSink
	// appends to the trailing text part (or pushes a new one if the previous
	// part was a tool); executor pushes a tool_call part each time and
	// updates its result/duration after the tool runs. At the end of the run
	// we persist parts as the canonical record and derive the legacy
	// content/tool_calls fields from it for backwards-compatible readers.
	var (
		turn           = 1
		totalToolCalls = 0
		parts          []models.ChatMessagePart
	)

	// Budget check before entering the loop.
	if creditCheck != nil && !creditCheck() {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted before run started", nil,
			preCommit, tracker, runStart)
	}

	// The dedicated keepalive goroutine that used to emit a typed
	// `keepalive` event every 15s has been removed: the view layer's
	// `:keepalive\n\n` SSE comment ticker covers transport-level idleness,
	// and the dashboard's idle watchdog now resets on every wire byte
	// (Phase 1's onActivity wiring), not just typed events. Emitting a
	// typed keepalive added overhead and gave clients a no-op event to
	// special-case.

	emitter.Emit(EventTurnStart, TurnStartEvent{RunID: runID, Turn: turn})

	// Tool executor adapter bridging the utils.ToolExecutor shape
	// (name, args string) -> (result, error)
	// into our Registry.Execute which also reports isError separately.
	executor := func(name string, argsJSON string) (string, error) {
		toolStart := time.Now()
		toolCallID := uuid.NewString()
		// Push a placeholder tool_call part — the result/duration/is_error
		// fields land in the same struct after registry.Execute returns.
		// Capturing the index up front lets us mutate the same slot rather
		// than searching by id at the end.
		partIdx := len(parts)
		parts = append(parts, models.ChatMessagePart{
			Type:       "tool_call",
			ToolCallID: toolCallID,
			Name:       name,
			Input:      json.RawMessage(argsJSON),
		})
		emitter.Emit(EventToolCallStart, ToolCallStartEvent{
			RunID:        runID,
			Turn:         turn,
			ToolCallID:   toolCallID,
			ToolName:     name,
			InputPreview: stripToolInputForPreview(name, argsJSON),
		})
		result, isErr, execErr := registry.Execute(ctx, rc, name, json.RawMessage(argsJSON))
		durationMs := int(time.Since(toolStart).Milliseconds())
		status := "ok"
		if isErr || execErr != nil {
			status = "error"
		}
		emitter.Emit(EventToolCallResult, ToolCallResultEvent{
			RunID:      runID,
			ToolCallID: toolCallID,
			ToolName:   name,
			Status:     status,
			DurationMs: durationMs,
			Summary:    firstLine(result, 140),
			IsError:    isErr || execErr != nil,
		})
		parts[partIdx].Result = result
		parts[partIdx].IsError = isErr || execErr != nil
		parts[partIdx].DurationMs = durationMs
		totalToolCalls++
		if execErr != nil {
			return result, execErr
		}
		return result, nil
	}

	narrationSink := func(chunk string) {
		if chunk == "" {
			return
		}
		// Append to the trailing text part if there is one; otherwise push a
		// new text part. This is the chronological mirror of what the model
		// emitted: text → tool → text → tool → text → ... lands as alternating
		// parts in the slice, which the dashboard renders top-to-bottom.
		if n := len(parts); n > 0 && parts[n-1].Type == "text" {
			parts[n-1].Text += chunk
		} else {
			parts = append(parts, models.ChatMessagePart{
				Type: "text",
				Text: chunk,
			})
		}
		emitter.Emit(EventNarration, NarrationEvent{RunID: runID, Turn: turn, Delta: chunk})
	}

	toolSchemas := registryToUtilsTools(registry)

	_, llmErr := utils.StreamWithTools(
		ctx,
		model,
		llmMessages,
		toolSchemas,
		provider,
		nil, // responseFormat
		nil, // reasoning
		executor,
		nil, // deprecated onToolCall; start events emitted from executor for correlatable IDs
		narrationSink,
		nil, // onReasoning
		utils.ToolCallConfig{
			MaxRounds:  maxTurns,
			MaxTokens:  16384,
			NudgeAfter: 50,
			StripAfter: -1,
			ShouldContinue: func() bool {
				if ctx.Err() != nil {
					return false
				}
				if creditCheck != nil && !creditCheck() {
					return false
				}
				if _, _, total, _ := tracker.Totals(); total > defaultMaxTokensPerRun {
					return false
				}
				return true
			},
		},
	)
	if llmErr != nil {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeProviderError, "LLM call failed", llmErr,
			preCommit, tracker, runStart)
	}

	// Post-loop budget checks to classify why the loop ended.
	if ctx.Err() != nil {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeBudgetExceeded, "run canceled or deadline exceeded", ctx.Err(),
			preCommit, tracker, runStart)
	}
	if creditCheck != nil && !creditCheck() {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted mid-run", nil,
			preCommit, tracker, runStart)
	}
	if _, _, total, _ := tracker.Totals(); total > defaultMaxTokensPerRun {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeBudgetExceeded,
			fmt.Sprintf("token budget exceeded (%d > %d)", total, defaultMaxTokensPerRun), nil,
			preCommit, tracker, runStart)
	}

	emitter.Emit(EventTurnEnd, TurnEndEvent{
		RunID:      runID,
		Turn:       turn,
		StopReason: "end_turn",
		Tokens:     trackerDelta(tracker),
	})

	// --- Step 13: success shutdown (continued in C2.20) ---
	return finaliseSuccess(ctx, deps, repo, run, emitter, fileMap,
		parts, tracker, runStart,
		totalToolCalls, turn, message, user, project)
}

// partsToContent flattens an ordered parts slice into the legacy plain-text
// content string by concatenating every text part in order.
func partsToContent(parts []models.ChatMessagePart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// partsToToolCalls projects the tool_call parts into the legacy
// ChatMessageToolCall slice readers still expect (analytics, history rebuild).
func partsToToolCalls(parts []models.ChatMessagePart) []models.ChatMessageToolCall {
	out := make([]models.ChatMessageToolCall, 0, len(parts))
	for _, p := range parts {
		if p.Type != "tool_call" {
			continue
		}
		out = append(out, models.ChatMessageToolCall{
			ID:         p.ToolCallID,
			Name:       p.Name,
			Input:      p.Input,
			Result:     p.Result,
			IsError:    p.IsError,
			DurationMs: p.DurationMs,
		})
	}
	return out
}

// registryToUtilsTools maps the agent Registry to the []utils.Tool shape
// StreamWithTools expects. Executor tools become OpenAI-style function wrappers;
// server tools (Anthropic-native, e.g. web_search) are passed through as-is.
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

// trackerDelta returns the current token usage from the tracker as a TokenUsage.
func trackerDelta(t *utils.TokenTracker) TokenUsage {
	p, c, _, _ := t.Totals()
	return TokenUsage{Input: p, Output: c}
}

// ---- helpers ----

// stripToolInputForPreview drops bulky fields from the tool input JSON
// before stamping it into ToolCallStartEvent.InputPreview. Without this,
// every write_file/edit_file call shipped the full file body THREE times
// over SSE: once in input_preview, once in file_written.content, and once
// in the persisted milestone. The dashboard reads file_written.content for
// optimistic file-tree updates, so input_preview is redundant — and the
// largest single source of wasted SSE bandwidth in the chat protocol.
//
// For non-file tools the original args pass through unchanged.
func stripToolInputForPreview(name string, argsJSON string) json.RawMessage {
	switch name {
	case "write_file", "edit_file", "delete_file":
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
			return json.RawMessage("{}")
		}
		// Keep the path so the UI can show a meaningful "writing X" hint;
		// drop content/old_string/new_string which can be hundreds of KB.
		stripped := map[string]json.RawMessage{}
		if p, ok := m["path"]; ok {
			stripped["path"] = p
		}
		if d, ok := m["description"]; ok {
			stripped["description"] = d
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

// runAction returns the PostHog action label for this run — matches the
// labels already used in finaliseSuccess for credit deduction so the LLM
// Analytics dashboard groups generate-app and edit-app traces consistently.
func runAction(project *models.AppProject) string {
	if project != nil && project.Status == "draft" {
		return "generate_app"
	}
	return "app_chat"
}

// resolveModel picks the LLM model for this run based on project state and secrets.
func resolveModel(sm secret.SecretManager, project *models.AppProject) string {
	if project.Status == "draft" {
		if m := sm.GetSecret("WEBSITE_BUILDER_LLM_MODEL"); m != "" {
			return m
		}
	}
	if m := sm.GetSecret("WEBSITE_BUILDER_CHAT_MODEL"); m != "" {
		return m
	}
	if m := sm.GetSecret("WEBSITE_BUILDER_LLM_MODEL"); m != "" {
		return m
	}
	return "claude-sonnet-4"
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func normaliseImages(images []string) []map[string]string {
	var out []map[string]string
	for _, img := range images {
		if img == "" {
			continue
		}
		out = append(out, map[string]string{"url": img})
	}
	return out
}

func convertHistory(history []models.AppChatMessage) []utils.Message {
	var out []utils.Message
	for _, m := range history {
		content := m.Content
		// Historical user messages are stored literally (e.g. "/plan a blog").
		// Re-expand on every load so the LLM sees the same prompt body it saw
		// on the original turn. We deliberately do NOT replay the SystemAddendum
		// for historical turns — modal constraints like "do not write files"
		// only apply to the turn they were typed on.
		if m.Role == "user" {
			content, _, _ = ExpandSlashCommand(content)
		}
		out = append(out, utils.Message{Role: m.Role, Content: content})
	}
	return out
}

func finaliseSuccess(
	ctx context.Context,
	deps *RunDependencies,
	repo *gitrepo.Repo,
	run *models.AppAgentRun,
	emitter EventEmitter,
	fileMap *SafeFileMap,
	parts []models.ChatMessagePart,
	tracker *utils.TokenTracker,
	runStart time.Time,
	totalToolCalls int,
	turns int,
	userMessage string,
	user *models.User,
	project *models.AppProject,
) error {
	// Derive the legacy fields from the canonical parts slice. New code reads
	// Parts directly; everything else (LLM history rebuild, analytics, the
	// previous dashboard render path) still works off Content + ToolCalls.
	narration := partsToContent(parts)
	toolCalls := partsToToolCalls(parts)
	// Capture isInitial up-front because we mutate project.Status below for
	// successful first generations, and the action label / event tracking
	// further down still need to know whether this run was the initial one.
	isInitial := project != nil && project.Status == "draft"

	// 0. Mechanically repair known build-breaking patterns the LLM sometimes
	//    emits (e.g. @import url("https://fonts.googleapis.com/...") placed after
	//    @import "tailwindcss" in globals.css, which violates the CSS spec and
	//    breaks Turbopack). Safer to strip invalid CSS than ship a broken build.
	sanitizeFileMapForBuild(fileMap)

	// 1. Write fileMap to working tree and commit.
	finalCommit, err := FinaliseRun(repo, fileMap, userMessage)
	if err != nil {
		return finaliseFailureV2(ctx, deps, repo, run, project, emitter, "commit",
			CodeGitError, "failed to write and commit files", err,
			run.PreCommit, tracker, runStart)
	}

	// 1a. Flip the project status from "draft" to "ready" on the first
	// successful generation. The dashboard's preview boot logic, the studio
	// hasStreamedOnce gate, and the templates-vs-edit branch in agent.Run
	// itself all key off this — leaving it stuck on "draft" means a fresh
	// page load after a successful generate never starts the preview and
	// the studio thinks the app still needs initial templates injected.
	//
	// Use the targeted UpdateAppProjectStatus so we don't risk silently
	// failing on unrelated JSONB columns like Theme/DataModels — a full
	// UpdateAppProject re-serializes every field.
	if isInitial && project != nil {
		if updErr := deps.DB.UpdateAppProjectStatus(project.ID.String(), "ready"); updErr != nil {
			// Don't fail the run — files committed successfully. Just log.
			slog.Warn("failed to flip project status to ready",
				"error", updErr, "project_id", project.ID)
		} else {
			project.Status = "ready"
		}
	}

	// 2. Persist the assistant chat message.
	toolCallsJSON, _ := json.Marshal(toolCalls)
	partsJSON, _ := json.Marshal(parts)
	if len(partsJSON) == 0 {
		partsJSON = []byte("[]")
	}
	assistantMsg := &models.AppChatMessage{
		AppProjectID: project.ID,
		RunID:        &run.ID,
		Role:         "assistant",
		Content:      narration,
		Images:       []byte("[]"),
		ToolCalls:    toolCallsJSON,
		Parts:        partsJSON,
	}
	if _, insErr := deps.DB.InsertChatMessage(assistantMsg); insErr != nil {
		slog.Warn("failed to persist assistant chat message", "error", insErr, "run_id", run.ID)
	}

	// 3. Finalise the AppAgentRun row.
	promptTok, completionTok, totalTok, _ := tracker.Totals()
	durationMs := int(time.Since(runStart).Milliseconds())
	credits := tokensToCredits(deps, promptTok, completionTok)
	costUSD := estimateCost(run.Model, promptTok, completionTok)

	run.Status = models.AgentRunStatusCompleted
	run.FinalCommit = finalCommit
	run.Turns = turns
	run.ToolCallCount = totalToolCalls
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.CostUSD = costUSD
	run.CreditsUsed = credits
	run.DurationMs = durationMs
	run.CompletedAt = time.Now()
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to finalise agent run row", "error", err, "run_id", run.ID)
	}

	// 4. Deduct credits.
	if deps.DeductCredits != nil && credits > 0 {
		action := "app_chat"
		if isInitial {
			action = "generate_app"
		}
		if err := deps.DeductCredits(ctx, credits, action, project.ID.String()); err != nil {
			slog.Warn("failed to deduct credits", "error", err, "credits", credits)
		}
	}

	// 5. Emit `done`.
	emitter.Emit(EventDone, DoneEvent{
		RunID:        run.ID,
		FinalCommit:  finalCommit,
		Turns:        turns,
		ToolCalls:    totalToolCalls,
		FilesChanged: countFileOps(toolCalls),
		Tokens:       TokenUsage{Input: promptTok, Output: completionTok},
		CreditsUsed:  credits,
		DurationMs:   durationMs,
	})

	// 6. Track PostHog.
	if deps.TrackUserEvent != nil {
		action := "app_chat_completed"
		if isInitial {
			action = "generate_app_completed"
		}
		deps.TrackUserEvent(action, map[string]any{
			"app_id":            project.ID.String(),
			"duration_seconds":  float64(durationMs) / 1000.0,
			"total_tokens":      totalTok,
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"credits_used":      credits,
			"turns":             turns,
			"tool_calls":        totalToolCalls,
			"model":             run.Model,
		})
	}

	// 6b. Track CRM (Brevo) — mirrors PostHog event names.
	if deps.SendCRMEvent != nil {
		crmEvent := "app_chat_completed"
		if isInitial {
			crmEvent = "generate_app_completed"
		}
		deps.SendCRMEvent(crmEvent, map[string]any{
			"app_id":       project.ID.String(),
			"project_type": project.ProjectType,
			"credits_used": credits,
			"model":        run.Model,
		})
	}

	// 7. PostHog LLM Analytics: emit per-call $ai_generation events plus a
	// grouping $ai_trace event for the built-in dashboard. The tracker has
	// been collecting messages/usage throughout the run via TrackContent.
	// Note: runAction would now return "app_chat" because we already flipped
	// project.Status above — use isInitial directly so the trace name still
	// reflects that this run was the initial generation.
	if deps.RecordLLMUsage != nil {
		traceAction := "app_chat"
		if isInitial {
			traceAction = "generate_app"
		}
		tracker.TraceOutput = narration
		deps.RecordLLMUsage(tracker, traceAction, run.Model, run.Provider,
			project.ID.String(), project.ProjectType)
	}

	// Unreference user (not logged to keep the event payload minimal).
	_ = user

	return nil
}

// countFileOps counts successful file-mutating tool calls.
func countFileOps(calls []models.ChatMessageToolCall) int {
	n := 0
	for _, c := range calls {
		if c.IsError {
			continue
		}
		switch c.Name {
		case "write_file", "edit_file", "delete_file":
			n++
		}
	}
	return n
}

// tokensToCredits computes credit cost via models.TokensToCreditsWithConfig
// using the same PostHog remote-config flags
// (rc-ai-credits-tokens-per-credit, rc-ai-credits-output-multiplier) the
// controller's TokensToCredits reads. Falls back to the per-key defaults if
// PostHog is unreachable. Without this, the agent overcharged users vs. the
// budget checker (which already went through remote config).
func tokensToCredits(deps *RunDependencies, input, output int) int {
	tpc, om := 0, 0
	if deps != nil && deps.RemoteConfig != nil {
		tpc = deps.RemoteConfig.GetInt(remoteconfig.AICreditsTokensPerCredit, remoteconfig.Global)
		om = deps.RemoteConfig.GetInt(remoteconfig.AICreditsOutputMultiplier, remoteconfig.Global)
	}
	if tpc <= 0 {
		tpc, _ = remoteconfig.AICreditsTokensPerCredit.Fallback.(int)
	}
	if om <= 0 {
		om, _ = remoteconfig.AICreditsOutputMultiplier.Fallback.(int)
	}
	return models.TokensToCreditsWithConfig(input, output, tpc, om)
}

// estimateCost wraps utils.EstimateInputOutputCostUSD.
func estimateCost(model string, input, output int) float64 {
	in, out := utils.EstimateInputOutputCostUSD(model, input, output, 0, 0)
	return in + out
}

func finaliseFailureV2(
	ctx context.Context,
	deps *RunDependencies,
	repo *gitrepo.Repo,
	run *models.AppAgentRun,
	project *models.AppProject,
	emitter EventEmitter,
	phase string,
	code ErrorCode,
	message string,
	wrapped error,
	preCommit string,
	tracker *utils.TokenTracker,
	runStart time.Time,
) error {
	// 1. Defensive git reset (usually a no-op — tools only touched fileMap).
	filesReverted := 0
	if preCommit != "" && repo != nil {
		if err := RollbackTo(repo, preCommit); err != nil {
			slog.Warn("rollback reset failed", "error", err, "run_id", run.ID)
		} else {
			filesReverted = 1
		}
	}

	// 2. Emit rollback event.
	emitter.Emit(EventRollback, RollbackEvent{
		RunID:         run.ID,
		PreCommit:     preCommit,
		Reason:        string(code),
		FilesReverted: filesReverted,
	})

	// 3. Emit error event.
	emitter.Emit(EventError, ErrorEvent{
		RunID:   run.ID,
		Phase:   phase,
		Message: message,
		Code:    code,
	})

	// 4. Persist "sorry" assistant message so the chat UI isn't orphaned.
	orphanText := "Sorry, that turn failed — your files are unchanged. Please try again."
	orphanParts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: orphanText},
	})
	orphanMsg := &models.AppChatMessage{
		AppProjectID: run.AppProjectID,
		RunID:        &run.ID,
		Role:         "assistant",
		Content:      orphanText,
		Images:       []byte("[]"),
		ToolCalls:    []byte("[]"),
		Parts:        orphanParts,
	}
	if _, err := deps.DB.InsertChatMessage(orphanMsg); err != nil {
		slog.Warn("failed to persist failure message", "error", err, "run_id", run.ID)
	}

	// 5. Finalise the run row (no credit deduction).
	var promptTok, completionTok int
	if tracker != nil {
		promptTok, completionTok, _, _ = tracker.Totals()
	}
	run.Status = mapCodeToStatus(code)
	run.Error = fmt.Sprintf("%s: %s", code, message)
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.DurationMs = int(time.Since(runStart).Milliseconds())
	run.CompletedAt = time.Now()
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to finalise failed agent run row", "error", err, "run_id", run.ID)
	}

	// 6. Track PostHog.
	if deps.TrackUserEvent != nil {
		deps.TrackUserEvent("app_chat_failed", map[string]any{
			"app_id":   run.AppProjectID.String(),
			"code":     string(code),
			"phase":    phase,
			"duration": run.DurationMs,
		})
	}

	// 6b. Track CRM (Brevo).
	if deps.SendCRMEvent != nil {
		deps.SendCRMEvent("app_chat_failed", map[string]any{
			"app_id": run.AppProjectID.String(),
			"code":   string(code),
			"phase":  phase,
		})
	}

	// 7. PostHog LLM Analytics: surface the failure on the LLM dashboard.
	// We always send an $ai_generation error event so error-rate panels see
	// the failure; if any LLM calls actually succeeded before the failure
	// (partial work), we ALSO emit the per-call generations + trace via
	// RecordLLMUsage so token spend isn't lost.
	action := runAction(project)
	projectID := run.AppProjectID.String()
	projectType := ""
	if project != nil {
		projectType = project.ProjectType
	}
	if deps.RecordLLMUsage != nil && tracker != nil {
		if _, _, total, _ := tracker.Totals(); total > 0 {
			deps.RecordLLMUsage(tracker, action, run.Model, run.Provider,
				projectID, projectType)
		}
	}
	if deps.RecordLLMError != nil {
		errMsg := fmt.Sprintf("%s: %s", code, message)
		if wrapped != nil {
			errMsg = fmt.Sprintf("%s (%s)", errMsg, wrapped.Error())
		}
		deps.RecordLLMError(action, run.Model, run.Provider,
			projectID, projectType,
			int64(run.DurationMs), errMsg)
	}

	_ = ctx
	return New(code, phase, message, wrapped)
}

// isUniqueViolation returns true if err is a Postgres unique-constraint
// violation (SQLSTATE 23505), used to detect the partial unique index
// conflict on concurrent run inserts.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// lib/pq surfaces *pq.Error with a Code field.
	type pgError interface{ SQLState() string }
	var pe pgError
	if errors.As(err, &pe) {
		return pe.SQLState() == "23505"
	}
	// Fallback: string match for drivers that don't expose SQLSTATE.
	return strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key")
}

// googleFontsImportPattern matches any CSS @import url("https://fonts.googleapis.com/...")
// line the LLM sometimes adds to globals.css. In a Tailwind v4 project this
// placement violates the CSS spec (imports must precede non-@layer rules,
// and @import "tailwindcss" expands hundreds of rules above it), so
// Turbopack fails the build. Fonts belong in next/font/google on layout.tsx.
var googleFontsImportPattern = regexp.MustCompile(`(?m)^\s*@import\s+url\(['"]?https://fonts\.googleapis\.com/[^)]*\)\s*;?\s*$\n?`)

// sanitizeFileMapForBuild rewrites known LLM mistakes in the in-memory file
// map before it's committed to the repo. Currently strips invalid Google
// Fonts @import statements from any .css file. The app may render without
// the intended font after stripping, but a working app with fallback fonts
// beats a broken build.
func sanitizeFileMapForBuild(fileMap *SafeFileMap) {
	if fileMap == nil {
		return
	}
	for path, content := range fileMap.Snapshot() {
		if !strings.HasSuffix(path, ".css") {
			continue
		}
		if !strings.Contains(content, "fonts.googleapis.com") {
			continue
		}
		cleaned := googleFontsImportPattern.ReplaceAllString(content, "")
		if cleaned == content {
			continue
		}
		fileMap.Set(path, cleaned)
		slog.Warn("stripped invalid Google Fonts @import from CSS (use next/font/google instead)",
			"path", path, "bytes_removed", len(content)-len(cleaned))
	}
}

// mapCodeToStatus maps agent error codes to AppAgentRun status strings.
func mapCodeToStatus(code ErrorCode) string {
	switch code {
	case CodeBudgetExceeded:
		return models.AgentRunStatusBudgetExceeded
	case CodeCreditsExhausted:
		return models.AgentRunStatusCanceled
	default:
		return models.AgentRunStatusFailed
	}
}
