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

// finaliseSuccess is a stub — the real implementation lands in Task C2.20.
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
	_ = ctx
	_ = deps
	_ = repo
	_ = run
	_ = emitter
	_ = fileMap
	_ = narration
	_ = toolCalls
	_ = tracker
	_ = runStart
	_ = totalToolCalls
	_ = turns
	_ = userMessage
	_ = user
	_ = project
	return nil
}

// finaliseFailureV2 is a stub — the real implementation lands in Task C2.20.
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
	_ = ctx
	_ = deps
	_ = repo
	_ = run
	_ = emitter
	_ = preCommit
	_ = tracker
	_ = runStart
	return New(code, phase, message, wrapped)
}
