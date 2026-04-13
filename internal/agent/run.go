package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/utils"
)

// Budget limits (package-level defaults; can be overridden later if needed).
const (
	defaultMaxTurns            = 25
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
) error {
	runStart := time.Now()

	// Wall-clock deadline: wrap ctx with a max-lifetime timer.
	ctx, cancelDeadline := context.WithTimeout(ctx, time.Duration(defaultMaxWallclockMs)*time.Millisecond)
	defer cancelDeadline()

	// --- Step 1: enforce single-run-per-project invariant ---
	existing, _ := deps.DB.GetActiveAgentRun(project.ID)
	if existing != nil {
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
		return New(CodeInternal, "setup", "failed to record run", err)
	}

	// --- Step 7: persist the user message ---
	imagesJSON := mustJSON(normaliseImages(images))
	userMsgRow := &models.AppChatMessage{
		AppProjectID: project.ID,
		RunID:        &runID,
		Role:         "user",
		Content:      message,
		Images:       imagesJSON,
		ToolCalls:    []byte("[]"),
	}
	if _, err := deps.DB.InsertChatMessage(userMsgRow); err != nil {
		return New(CodeInternal, "setup", "failed to persist user message", err)
	}
	run.UserMessageID = &userMsgRow.ID
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to update run user_message_id", "error", err, "run_id", runID)
	}

	// --- Step 8: load conversation history ---
	history, err := deps.DB.GetRecentChatMessages(project.ID, 30)
	if err != nil {
		slog.Warn("failed to load history, starting fresh", "error", err, "project_id", project.ID)
		history = nil
	}

	// --- Step 9: build system messages + registry + tracker ---
	llmMessages := BuildSystemMessages(project, fileMap)
	llmMessages = append(llmMessages, convertHistory(history)...)

	registry := BuildRegistryFor(project, deps)

	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	ctx = utils.WithTokenTracker(ctx, tracker)

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
		MaxTurns:  defaultMaxTurns,
		Tools:     registry.Names(),
	})

	// --- Step 11: panic guard ---
	defer func() {
		if r := recover(); r != nil {
			slog.Error("agent.Run panicked", "error", r, "project_id", project.ID, "stack", string(debug.Stack()))
			_ = finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
				CodeInternal, fmt.Sprintf("panic: %v", r), nil, preCommit, tracker, runStart)
		}
	}()

	// --- Step 12: agent loop ---
	var (
		turn           = 1
		totalToolCalls = 0
		narrationBuf   strings.Builder
		toolCallLog    []models.ChatMessageToolCall
	)

	// Budget check before entering the loop.
	if deps.CreditChecker != nil && !deps.CreditChecker() {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted before run started", nil,
			preCommit, tracker, runStart)
	}

	// Keepalive goroutine — emits every 15s until the run finishes.
	keepaliveDone := make(chan struct{})
	go func() {
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-keepaliveDone:
				return
			case <-tick.C:
				emitter.Emit(EventKeepalive, KeepaliveEvent{RunID: runID, TS: time.Now().Unix()})
			}
		}
	}()
	defer close(keepaliveDone)

	emitter.Emit(EventTurnStart, TurnStartEvent{RunID: runID, Turn: turn})

	// Tool executor adapter bridging the utils.ToolExecutor shape
	// (name, args string) -> (result, error)
	// into our Registry.Execute which also reports isError separately.
	executor := func(name string, argsJSON string) (string, error) {
		toolStart := time.Now()
		toolCallID := uuid.NewString()
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
		toolCallLog = append(toolCallLog, models.ChatMessageToolCall{
			ID:         toolCallID,
			Name:       name,
			Input:      json.RawMessage(argsJSON),
			Result:     result,
			IsError:    isErr || execErr != nil,
			DurationMs: durationMs,
		})
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
		narrationBuf.WriteString(chunk)
		emitter.Emit(EventNarration, NarrationEvent{RunID: runID, Turn: turn, Delta: chunk})
	}

	toolSchemas := registryToUtilsTools(registry)

	narrationText, llmErr := utils.StreamWithTools(
		ctx,
		model,
		llmMessages,
		toolSchemas,
		provider,
		nil, // responseFormat
		nil, // reasoning
		executor,
		nil, // deprecated onToolCall, we use OnToolCallStart in opts
		narrationSink,
		nil, // onReasoning
		utils.ToolCallConfig{
			MaxRounds: defaultMaxTurns,
			MaxTokens: 16384,
			OnToolCallStart: func(toolName string) {
				emitter.Emit(EventToolCallStart, ToolCallStartEvent{
					RunID:    runID,
					Turn:     turn,
					ToolName: toolName,
				})
			},
			ShouldContinue: func() bool {
				if ctx.Err() != nil {
					return false
				}
				if deps.CreditChecker != nil && !deps.CreditChecker() {
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
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
			CodeProviderError, "LLM call failed", llmErr,
			preCommit, tracker, runStart)
	}

	// Post-loop budget checks to classify why the loop ended.
	if ctx.Err() != nil {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
			CodeBudgetExceeded, "run canceled or deadline exceeded", ctx.Err(),
			preCommit, tracker, runStart)
	}
	if deps.CreditChecker != nil && !deps.CreditChecker() {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted mid-run", nil,
			preCommit, tracker, runStart)
	}
	if _, _, total, _ := tracker.Totals(); total > defaultMaxTokensPerRun {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "agent_loop",
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

	// If StreamWithTools returned visible narration text, merge it into the
	// buffer (it's what we emit as the final assistant content).
	if narrationText != "" {
		narrationBuf.WriteString(narrationText)
	}

	// --- Step 13: success shutdown (continued in C2.20) ---
	return finaliseSuccess(ctx, deps, repo, run, emitter, fileMap,
		narrationBuf.String(), toolCallLog, tracker, runStart,
		totalToolCalls, turn, message, user, project)
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
		out = append(out, utils.Message{Role: m.Role, Content: m.Content})
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
	narration string,
	toolCalls []models.ChatMessageToolCall,
	tracker *utils.TokenTracker,
	runStart time.Time,
	totalToolCalls int,
	turns int,
	userMessage string,
	user *models.User,
	project *models.AppProject,
) error {
	// 1. Write fileMap to working tree and commit.
	finalCommit, err := FinaliseRun(repo, fileMap, userMessage)
	if err != nil {
		return finaliseFailureV2(ctx, deps, repo, run, emitter, "commit",
			CodeGitError, "failed to write and commit files", err,
			run.PreCommit, tracker, runStart)
	}

	// 2. Persist the assistant chat message.
	toolCallsJSON, _ := json.Marshal(toolCalls)
	assistantMsg := &models.AppChatMessage{
		AppProjectID: project.ID,
		RunID:        &run.ID,
		Role:         "assistant",
		Content:      narration,
		Images:       []byte("[]"),
		ToolCalls:    toolCallsJSON,
	}
	if _, insErr := deps.DB.InsertChatMessage(assistantMsg); insErr != nil {
		slog.Warn("failed to persist assistant chat message", "error", insErr, "run_id", run.ID)
	}

	// 3. Finalise the AppAgentRun row.
	promptTok, completionTok, totalTok, _ := tracker.Totals()
	durationMs := int(time.Since(runStart).Milliseconds())
	credits := tokensToCredits(promptTok, completionTok)
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
		if project.Status == "draft" {
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
		if project.Status == "draft" {
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

// tokensToCredits mirrors the existing Controller.TokensToCredits formula.
// 1 credit ~ 1000 input tokens OR 250 output tokens (Sonnet approximate).
// This duplicates the formula rather than calling into controllers because
// the agent package is below controllers in the dependency graph.
func tokensToCredits(input, output int) int {
	cr := input/1000 + output/250
	if cr < 1 {
		return 1
	}
	return cr
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
	orphanMsg := &models.AppChatMessage{
		AppProjectID: run.AppProjectID,
		RunID:        &run.ID,
		Role:         "assistant",
		Content:      "Sorry, that turn failed — your files are unchanged. Please try again.",
		Images:       []byte("[]"),
		ToolCalls:    []byte("[]"),
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

	_ = ctx
	return New(code, phase, message, wrapped)
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
