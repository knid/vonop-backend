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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/constants"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/remoteconfig"
	"github.com/knid/wemob/internal/services/secret"
	"github.com/knid/wemob/internal/services/workspacelock"
	"github.com/knid/wemob/internal/utils"
)

// TurnAttachments carries per-turn attachment context into the agent.
// Current is the set the user explicitly attached to this user turn (used
// for vision content blocks on the current message). Pool is the project-
// wide capped list used to build the system manifest. Both come from the
// view layer after resolving AttachmentIDs against the live AppAsset table.
type TurnAttachments struct {
	Current []models.AppAsset
	Pool    []models.AppAsset
}

// Budget limits (package-level defaults; can be overridden later if needed).
const (
	defaultMaxTurns            = 50
	defaultMaxToolCallsPerTurn = 20
	defaultMaxTokensPerRun     = 500_000
	defaultMaxWallclockMs      = 45 * 60 * 1000
)

// runNative is the legacy native Go implementation of the agent flow.
//
// When project.Status == "draft" and the repo is empty, Run injects templates.
// Otherwise it loads the existing fileMap from the repo.
//
// On success: files are committed to git, AppAgentRun is finalised, credits
// are deducted, events are emitted, and nil is returned.
//
// On failure: if the agent reached the loop/validation phase, partial progress
// is committed as a checkpoint and surfaced via EventCheckpoint; setup/commit
// failures do not create checkpoints. AppAgentRun is marked failed, credits for
// tokens already consumed are billed, an error event is emitted, and a *Error
// is returned.
func runNative(
	ctx context.Context,
	deps *RunDependencies,
	project *models.AppProject,
	user *models.User,
	message string,
	turnAttachments TurnAttachments,
	emitter EventEmitter,
) (runResult *RuntimeResult, runErr error) {
	runStart := time.Now()

	// Wall-clock deadline: wrap ctx with a max-lifetime timer.
	ctx, cancelDeadline := context.WithTimeout(ctx, time.Duration(defaultMaxWallclockMs)*time.Millisecond)
	defer cancelDeadline()

	// --- Step 1: enforce single-run-per-project invariant ---
	// Advisory pre-check (fast path for the common case). The authoritative
	// check is the partial unique index on app_agent_runs(app_project_id)
	// WHERE status='running', caught below at InsertAgentRun.
	if existing, existingErr := deps.DB.GetActiveAgentRun(project.ID); existingErr == nil && existing != nil {
		return nil, New(CodeRunInProgress, "setup", "another run is already in progress for this project", nil)
	}

	// --- Step 2: open git repo (creates if missing) ---
	repo, err := deps.GitRepo.OpenOrInit(project.ID.String())
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to open project repo", err)
	}

	// --- Step 3: first-run template injection (draft projects only) ---
	if project.Status == "draft" {
		existingFiles, _ := repo.ListFiles()
		if len(existingFiles) == 0 {
			if n, injErr := InjectTemplates(repo, project); injErr != nil {
				return nil, New(CodeGitError, "setup", fmt.Sprintf("template injection failed after %d files", n), injErr)
			}
		}
	}

	// --- Step 4: load fileMap from disk ---
	fileMap, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to read project files", err)
	}

	// --- Step 5: pre-run commit baseline ---
	runID := uuid.New()
	preCommit, err := PreRunCommit(repo, runID.String())
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to create pre-run snapshot", err)
	}

	// --- Step 6: acquire distributed workspace lock ---
	leaseOwner := "agent:" + runID.String()
	if deps.WorkerID != "" {
		leaseOwner = "worker:" + deps.WorkerID + ":" + runID.String()
	}
	var workspaceLease *workspacelock.Lease
	if deps.WorkspaceLock != nil {
		var lockErr error
		workspaceLease, lockErr = deps.WorkspaceLock.Acquire(ctx, project.ID.String(), leaseOwner)
		if lockErr != nil {
			if _, busy := lockErr.(*workspacelock.LockBusyError); busy {
				return nil, New(CodeRunInProgress, "setup", "another process is modifying this workspace", lockErr)
			}
			return nil, New(CodeInternal, "setup", "failed to acquire workspace lock", lockErr)
		}
		defer func() {
			if err := workspaceLease.Release(); err != nil {
				slog.Warn("agent: failed to release workspace lock", "project_id", project.ID, "error", err, "run_id", runID)
			}
		}()
	}

	// --- Step 7: insert AppAgentRun row ---
	provider := deps.LLMProvider
	model := resolveModel(deps.SecretManager, project, provider)
	isFirstGen := project != nil && project.Status == "draft"
	todos := NewTodoList(message)
	memory := NewAgentRunMemory(message)
	run := &models.AppAgentRun{
		ID:           runID,
		AppProjectID: project.ID,
		UserID:       user.ID,
		Status:       models.AgentRunStatusRunning,
		PreCommit:    preCommit,
		Model:        model,
		Provider:     provider.Name,
		Todos:        todos.RawJSON(),
		Memory:       memory.RawJSON(),
	}
	if _, err := deps.DB.InsertAgentRun(run); err != nil {
		// Check for the partial unique index violation (Postgres SQLSTATE 23505).
		if isUniqueViolation(err) {
			return nil, New(CodeRunInProgress, "setup", "another run is already in progress for this project", err)
		}
		return nil, New(CodeInternal, "setup", "failed to record run", err)
	}

	// --- Step 7: load conversation history (BEFORE inserting the current turn) ---
	history, err := deps.DB.GetRecentChatMessages(project.ID, 10)
	if err != nil {
		slog.Warn("failed to load history, starting fresh", "error", err, "project_id", project.ID)
		history = nil
	}

	// --- Step 8: persist the user message ---
	attachmentSnapshot := make([]models.ChatMessageAttachment, 0, len(turnAttachments.Current))
	currentImageURLs := make([]string, 0, len(turnAttachments.Current))
	for _, a := range turnAttachments.Current {
		isImage := constants.ClassifyAttachment(a.ContentType) == constants.AttachmentKindImage
		attachmentSnapshot = append(attachmentSnapshot, models.ChatMessageAttachment{
			AssetID:     a.ID,
			FileName:    a.FileName,
			ContentType: a.ContentType,
			FileSize:    a.FileSize,
			URL:         a.PublicURL,
			IsImage:     isImage,
		})
		if isImage {
			currentImageURLs = append(currentImageURLs, a.PublicURL)
		}
	}
	attachmentsJSON := mustJSON(attachmentSnapshot)
	// imagesJSON is the legacy column — keep populating it from current image
	// URLs so old read paths (legacy mapApiMessages fallback on the dashboard)
	// keep rendering after this ships.
	imagesJSON := mustJSON(normaliseImages(currentImageURLs))
	userParts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: message},
	})
	userMsgRow := &models.AppChatMessage{
		AppProjectID: project.ID,
		RunID:        &runID,
		Role:         "user",
		Content:      message,
		Images:       imagesJSON,
		Attachments:  attachmentsJSON,
		ToolCalls:    []byte("[]"),
		Parts:        userParts,
	}
	if _, err := deps.DB.InsertChatMessage(userMsgRow); err != nil {
		return nil, New(CodeInternal, "setup", "failed to persist user message", err)
	}
	run.UserMessageID = &userMsgRow.ID
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to update run user_message_id", "error", err, "run_id", runID)
	}

	// --- Step 9: build system messages + registry + tracker ---
	buildState := NewBuildState(message, isFirstGen)
	contextPacket := CompileAgentContext(AgentContextInput{
		Project:         project,
		Deps:            deps,
		AttachmentsPool: turnAttachments.Pool,
		Message:         message,
		IsFirstGen:      isFirstGen,
	})
	profile := contextPacket.Profile
	activeSkills := contextPacket.Skills
	llmMessages := contextPacket.SystemMessages
	memory.RecordRuntime(profile.ID, contextPacket.RequiredSkills, contextPacket.SkillManifest)
	memory.Persist(ctx, deps.DB, runID)

	llmMessages = append(llmMessages, convertHistory(history)...)

	// Append the current turn explicitly — we don't rely on history reload
	// to round-trip images, and we want the turn to land even if history
	// load failed. Slash command expansion happens here so the LLM sees the
	// full prompt body while the persisted DB row (above) keeps the literal
	// text the user typed.
	expandedMessage, _, _ := ExpandSlashCommand(message)

	// If the user is trying to resume after a failed run, augment their
	// message with a resume context block so the LLM knows what was already
	// accomplished before the failure.
	if isContinueMessage(expandedMessage) {
		if resumeCtx := buildResumeContext(deps, repo, project.ID); resumeCtx != "" {
			expandedMessage = resumeCtx + expandedMessage
		}
	}

	// Keep the current filesystem plus durable build progress on the live user
	// turn. This is refreshed after every tool round so the model has a
	// compact source of truth even when older tool messages are trimmed.
	coreUserContent := expandedMessage
	userTurn := utils.Message{Role: "user", Content: coreUserContent}
	refreshRunPrefix := func(msg *utils.Message) {
		prefix := renderRunPrefix(fileMap, buildState, todos)
		if prefix == "" {
			return
		}
		c := msg.Content
		const requestMarker = "\n\nUSER REQUEST\n============\n\n"
		if strings.HasPrefix(c, "CURRENT RUN CONTEXT\n") {
			if idx := strings.Index(c, requestMarker); idx >= 0 {
				c = c[idx+len(requestMarker):]
			}
		}
		msg.Content = "CURRENT RUN CONTEXT\n===================\n\n" + prefix + requestMarker + c
	}
	refreshRunPrefix(&userTurn)
	if len(currentImageURLs) > 0 {
		userTurn.Images = currentImageURLs
	}
	llmMessages = append(llmMessages, userTurn)

	registry := BuildRegistryFor(project, deps, model)

	tracker := utils.NewTokenTracker()
	tracker.UserID = user.ID.String()
	tracker.ProjectID = project.ID.String()
	tracker.OrganizationID = project.OrganizationID.String()
	// Bind the PostHog trace id to AppAgentRun.ID so $ai_generation and
	// $ai_trace events share an id operators can pivot from — both to the
	// agent_runs row and to error events emitted from finaliseFailureV2,
	// which previously generated a fresh uuid per call and left errors
	// orphaned from their own run's generations.
	tracker.TraceID = runID.String()
	tracker.RunID = runID.String()
	tracker.SetAgentSkills(contextPacket.SkillIDs, nil)
	// Opt-in PostHog LLM Analytics: record per-call input/output content so
	// the $ai_generation events carry full message context, and seed the
	// trace input with the user's prompt for the grouping $ai_trace event.
	tracker.TrackContent = true
	tracker.TraceInput = message
	ctx = utils.WithTokenTracker(ctx, tracker)

	// emitCreditsUpdate fires a credits_update event with the running
	// cumulative billed credits so the frontend can render a live meter
	// after each turn end. Tokens is informational (input+output total).
	emitCreditsUpdate := func(turn int) {
		credits := creditsBilled(deps, tracker, project.ID.String())
		_, _, total, _ := tracker.Totals()
		emitter.Emit(EventCreditsUpdate, CreditsUpdateEvent{
			RunID:       runID,
			Turn:        turn,
			CreditsUsed: credits,
			Tokens:      total,
		})
	}

	// Bind the credit checker to the run's token tracker.
	var creditCheck func() bool
	if deps.CreditChecker != nil {
		creditCheck = deps.CreditChecker(project.ID.String(), tracker)
	}

	// Resolve the tool-round budget and nudge/strip thresholds from remote
	// config so they can be tuned without a redeploy. Draft (new project) uses
	// the "generation" flags; chat edits use the "builder" flags.
	maxTurns := defaultMaxTurns
	nudgeAfter := 3
	stripAfter := 5
	if deps.RemoteConfig != nil {
		if project.Status == "draft" {
			if rcTurns := deps.RemoteConfig.GetInt(remoteconfig.AIGenerationMaxRounds, remoteconfig.Global); rcTurns > 0 {
				maxTurns = rcTurns
			}
			if rcNudge := deps.RemoteConfig.GetInt(remoteconfig.AIGenerationNudgeAfter, remoteconfig.Global); rcNudge > 0 {
				nudgeAfter = rcNudge
			}
			if rcStrip := deps.RemoteConfig.GetInt(remoteconfig.AIGenerationStripAfter, remoteconfig.Global); rcStrip > 0 {
				stripAfter = rcStrip
			}
		} else {
			if rcTurns := deps.RemoteConfig.GetInt(remoteconfig.AIBuilderToolMaxRounds, remoteconfig.Global); rcTurns > 0 {
				maxTurns = rcTurns
			}
			if rcNudge := deps.RemoteConfig.GetInt(remoteconfig.AIBuilderToolNudgeAfter, remoteconfig.Global); rcNudge > 0 {
				nudgeAfter = rcNudge
			}
			if rcStrip := deps.RemoteConfig.GetInt(remoteconfig.AIBuilderToolStripAfter, remoteconfig.Global); rcStrip > 0 {
				stripAfter = rcStrip
			}
		}
	}

	// Resolve the per-run token budget from remote config. Falls back to the
	// package default when RC is unavailable or returns a non-positive value.
	maxTokensPerRun := defaultMaxTokensPerRun
	if deps.RemoteConfig != nil {
		if rcTokens := deps.RemoteConfig.GetInt(remoteconfig.AIMaxTokensPerRun, remoteconfig.Global); rcTokens > 0 {
			maxTokensPerRun = rcTokens
		}
	}
	compactAfterTokens := 250_000
	compactIntervalTokens := 150_000
	trimToolResultChars := 4_000
	trimToolArgChars := 1_200
	keepRecentToolRounds := 2
	if project.Status == "draft" {
		keepRecentToolRounds = 5
	}
	if deps.RemoteConfig != nil {
		compactAfterTokens = deps.RemoteConfig.GetInt(remoteconfig.AICompactAfterTokens, remoteconfig.Global)
		compactIntervalTokens = deps.RemoteConfig.GetInt(remoteconfig.AICompactIntervalTokens, remoteconfig.Global)
		if rcTrim := deps.RemoteConfig.GetInt(remoteconfig.AITrimToolResultChars, remoteconfig.Global); rcTrim > 0 {
			trimToolResultChars = rcTrim
		}
		if rcTrim := deps.RemoteConfig.GetInt(remoteconfig.AITrimToolArgChars, remoteconfig.Global); rcTrim > 0 {
			trimToolArgChars = rcTrim
		}
		if project.Status == "draft" {
			if rcKeep := deps.RemoteConfig.GetInt(remoteconfig.AIGenerationKeepRecentToolRounds, remoteconfig.Global); rcKeep > 0 {
				keepRecentToolRounds = rcKeep
			}
		} else {
			if rcKeep := deps.RemoteConfig.GetInt(remoteconfig.AIBuilderKeepRecentToolRounds, remoteconfig.Global); rcKeep > 0 {
				keepRecentToolRounds = rcKeep
			}
		}
	}
	if compactIntervalTokens <= 0 {
		compactIntervalTokens = compactAfterTokens
	}

	rc := &RunContext{
		RunID:          runID,
		Profile:        profile,
		Skills:         activeSkills,
		RequiredSkills: contextPacket.RequiredSkills,
		SkillManifest:  contextPacket.SkillManifest,
		ReadOnly:       contextPacket.ReadOnly,
		Project:        project,
		User:           user,
		FileMap:        fileMap,
		BuildState:     buildState,
		Todos:          todos,
		Emitter:        emitter,
		Tracker:        tracker,
		Deps:           deps,
		ImageCache:     make(map[string]string),
		ImageInflight:  make(map[string]*imageInflightCall),
	}

	// --- Step 10: emit run_start ---
	emitter.Emit(EventRunStart, RunStartEvent{
		RunID:           runID,
		PreCommit:       preCommit,
		Model:           model,
		Provider:        provider.Name,
		MaxTurns:        maxTurns,
		Tools:           registry.Names(),
		AgentID:         profile.ID,
		AgentName:       profile.Name,
		Skills:          skillIDs(activeSkills),
		RequiredSkills:  contextPacket.SkillIDs,
		SuggestedSkills: contextPacket.SuggestedSkillIDs,
		AvailableSkills: contextPacket.ManifestSkillIDs,
		LoadedSkills:    nil,
		Handoffs:        append([]string(nil), profile.HandoffTargets...),
	})
	emitTodoSnapshot(rc)

	// --- Step 11: panic guard ---
	defer func() {
		if r := recover(); r != nil {
			slog.Error("agent.Run panicked", "error", r, "project_id", project.ID, "stack", string(debug.Stack()))
			runErr = finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
				CodeInternal, fmt.Sprintf("panic: %v", r), nil, tracker, runStart,
				fileMap, message)
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
		partsMu        sync.Mutex
	)

	// Budget check before entering the loop.
	if creditCheck != nil && !creditCheck() {
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted before run started", nil,
			tracker, runStart,
			fileMap, message)
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
		tracker.RecordToolUsed(name)
		// Push a placeholder tool_call part — the result/duration/is_error
		// fields land in the same struct after registry.Execute returns.
		// Capturing the index up front lets us mutate the same slot rather
		// than searching by id at the end.
		partsMu.Lock()
		partIdx := len(parts)
		parts = append(parts, models.ChatMessagePart{
			Type:       "tool_call",
			ToolCallID: toolCallID,
			Name:       name,
			Input:      json.RawMessage(argsJSON),
		})
		partsMu.Unlock()
		emitter.Emit(EventToolCallStart, ToolCallStartEvent{
			RunID:        runID,
			Turn:         turn,
			ToolCallID:   toolCallID,
			ToolName:     name,
			RiskLevel:    ClassifyToolRisk(name),
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
			RiskLevel:  ClassifyToolRisk(name),
			Status:     status,
			DurationMs: durationMs,
			Summary:    firstLine(result, 140),
			IsError:    isErr || execErr != nil,
		})
		partsMu.Lock()
		parts[partIdx].Result = result
		parts[partIdx].IsError = isErr || execErr != nil
		parts[partIdx].DurationMs = durationMs
		totalToolCalls++
		partsMu.Unlock()
		memory.RecordTool(name, argsJSON, result, isErr || execErr != nil)
		memory.Persist(ctx, deps.DB, runID)
		if execErr != nil {
			return result, execErr
		}
		return result, nil
	}

	repetition := newRepetitionDetector()
	lastCompactedTokens := 0

	narrationSink := func(chunk string) {
		if chunk == "" {
			return
		}
		repetition.Feed(chunk)
		partsMu.Lock()
		defer partsMu.Unlock()
		// Append to the trailing text part if there is one; otherwise push a
		// new text part. This is the chronological mirror of what the model
		// emitted: text → tool → text → tool → text → ... lands as alternating
		// parts in the slice, which the dashboard renders top-to-bottom.
		//
		// A tool_call → text transition marks the boundary between model
		// rounds: the previous round emitted tool calls, we executed them,
		// and the model is now narrating its next round. Emit TurnEnd /
		// TurnStart across the boundary so the UI, analytics, and the
		// Turns column on app_agent_runs all reflect the true round count
		// (previously pinned to 1 regardless of how many StreamWithTools
		// rounds ran).
		if n := len(parts); n > 0 && parts[n-1].Type == "text" {
			parts[n-1].Text += chunk
		} else {
			if n := len(parts); n > 0 && parts[n-1].Type == "tool_call" {
				emitter.Emit(EventTurnEnd, TurnEndEvent{
					RunID:      runID,
					Turn:       turn,
					StopReason: "tool_use",
					Tokens:     trackerDelta(tracker),
				})
				emitCreditsUpdate(turn)
				turn++
				emitter.Emit(EventTurnStart, TurnStartEvent{RunID: runID, Turn: turn})
			}
			parts = append(parts, models.ChatMessagePart{
				Type: "text",
				Text: chunk,
			})
		}
		emitter.Emit(EventNarration, NarrationEvent{RunID: runID, Turn: turn, Delta: chunk})
	}

	reasoningSink := func(chunk string) {
		if chunk == "" {
			return
		}
		partsMu.Lock()
		defer partsMu.Unlock()
		if n := len(parts); n > 0 && parts[n-1].Type == "reasoning" {
			parts[n-1].Text += chunk
		} else {
			parts = append(parts, models.ChatMessagePart{
				Type: "reasoning",
				Text: chunk,
			})
		}
		emitter.Emit(EventReasoning, ReasoningEvent{RunID: runID, Turn: turn, Delta: chunk})
	}

	toolSchemas := registryToUtilsTools(registry)

	toolRoundCount := 0
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
		reasoningSink,
		utils.ToolCallConfig{
			MaxRounds: maxTurns,
			// Sonnet 4.6's non-extended output cap. Running at the provider
			// max keeps the continuation dance (finish_reason=length → send
			// "continue") from firing in all but pathological runs.
			MaxTokens:            64000,
			NudgeAfter:           nudgeAfter,
			StripAfter:           stripAfter,
			StripTools:           []string{"read_files", "list_files", "grep", "read_attachment", "web_search", "read_build_logs", "read_deploy_logs", "read_mobile_config", "list_skills", "search_skills", "load_skill"},
			MaxMessages:          25,
			KeepRecentToolRounds: keepRecentToolRounds,
			RefreshPrefix:        refreshRunPrefix,
			TrimResults: func(toolName, result string) string {
				return trimToolResultForLLM(toolName, result, trimToolResultChars)
			},
			TrimArgs: func(toolName, args string) string {
				return trimToolArgsForLLM(toolName, args, trimToolArgChars)
			},
			CompactHistory: func(messages []utils.Message) ([]utils.Message, bool) {
				if compactAfterTokens <= 0 {
					return messages, false
				}
				_, _, total, _ := tracker.Totals()
				if total < compactAfterTokens {
					return messages, false
				}
				if lastCompactedTokens > 0 && total-lastCompactedTokens < compactIntervalTokens {
					return messages, false
				}
				compacted := compactLLMHistory(messages, memory, todos, 12)
				lastCompactedTokens = total
				if llmMessagesEqual(compacted, messages) {
					return messages, false
				}
				memory.Persist(ctx, deps.DB, runID)
				slog.Info("agent compacted LLM history",
					"project_id", project.ID, "run_id", runID,
					"tokens", total, "compact_after", compactAfterTokens,
					"messages_before", len(messages), "messages_after", len(compacted))
				return compacted, true
			},
			// Moonshot K2.6 prompt_cache_key: stable per-project so the system
			// prompt is cached across runs. Only set for Moonshot — other
			// providers may reject unknown fields.
			PromptCacheKey: func() string {
				if provider.IsMoonshot() {
					return "wemob:" + project.ID.String()
				}
				return ""
			}(),
			// Provider-executed server tools do not pass through our local
			// executor, so we cannot emit a matching tool_call_result. Emitting
			// starts without terminal results leaves the chat UI with orphan
			// running cards, so we intentionally suppress these callbacks.
			OnToolCallStart: nil,
			AfterToolRound: func(allIntercepted bool) string {
				toolRoundCount++
				remaining := maxTurns - toolRoundCount
				if remaining <= 2 {
					buildState.SetPhase(BuildPhaseValidation)
					return fmt.Sprintf("URGENT: You have used %d tool rounds and have at most %d total rounds left. Finish the user request now; if TODO STATE has open items, close them with evidence. Do not research or recreate existing files.", toolRoundCount, remaining)
				}
				if toolRoundCount >= 3 {
					return fmt.Sprintf("You have used %d tool rounds. Total budget is ~%d rounds. Avoid re-reading or recreating completed files. Use TODO STATE only if a checklist exists; otherwise continue the direct user request.", toolRoundCount, maxTurns)
				}
				return ""
			},
			ShouldContinue: func() bool {
				if ctx.Err() != nil {
					return false
				}
				if creditCheck != nil && !creditCheck() {
					return false
				}
				if _, _, total, _ := tracker.Totals(); total > maxTokensPerRun {
					return false
				}
				if repetition.Tripped() {
					return false
				}
				return true
			},
		},
	)
	// Narration-loop wins over any other classification: a tripped detector
	// means we told StreamWithTools to bail via ShouldContinue, so any
	// surfacing error is a consequence, not the cause.
	if repetition.Tripped() {
		slog.Warn("agent aborted: narration repetition loop",
			"project_id", project.ID, "run_id", runID)
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeRepetitionLoop, "model got stuck repeating output", nil,
			tracker, runStart,
			fileMap, message)
	}
	if llmErr != nil {
		// A ctx-triggered stream abort surfaces here as llmErr — classify it
		// by the ctx.Err kind so ops and the UI can tell the two failure modes
		// apart: DeadlineExceeded = our 45-min wallclock fired, Canceled =
		// external cancel (user hit Stop, process shutdown).
		if ctxErr := ctx.Err(); ctxErr != nil {
			slog.Warn("agent LLM call aborted by context",
				"project_id", project.ID, "ctx_err", ctxErr, "llm_err", llmErr)
			code, msg := classifyCtxErr(ctxErr)
			return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
				code, msg, ctxErr,
				tracker, runStart,
				fileMap, message)
		}
		// Truncation isn't a provider fault — it means the model exhausted
		// its per-call token budget and the continuation loop failed too.
		// Surface it distinctly so the UI can prompt the user to narrow
		// the request rather than showing a generic "provider error".
		if errors.Is(llmErr, utils.ErrResponseTruncated) {
			return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
				CodeTruncated, "model response exceeded token budget", llmErr,
				tracker, runStart,
				fileMap, message)
		}
		var toolRoundsErr *utils.ToolRoundsExceededError
		if errors.As(llmErr, &toolRoundsErr) {
			return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
				CodeToolRoundsExceeded, "tool round limit reached — reply \"continue\" to keep going from saved progress", llmErr,
				tracker, runStart,
				fileMap, message)
		}
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeProviderError, "LLM call failed", llmErr,
			tracker, runStart,
			fileMap, message)
	}

	// Post-loop budget checks to classify why the loop ended.
	if ctxErr := ctx.Err(); ctxErr != nil {
		code, msg := classifyCtxErr(ctxErr)
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			code, msg, ctxErr,
			tracker, runStart,
			fileMap, message)
	}
	if creditCheck != nil && !creditCheck() {
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeCreditsExhausted, "AI credits exhausted mid-run", nil,
			tracker, runStart,
			fileMap, message)
	}
	if _, _, total, _ := tracker.Totals(); total > maxTokensPerRun {
		return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "agent_loop",
			CodeBudgetExceeded,
			fmt.Sprintf("token budget exceeded (%d > %d)", total, maxTokensPerRun), nil,
			tracker, runStart,
			fileMap, message)
	}

	if !contextPacket.ReadOnly {
		if validationErr := ValidateAgentOutput(profile, fileMap, buildState, message, isFirstGen, project, rc.Skills); validationErr != nil {
			return nil, finaliseFailureV2(ctx, deps, repo, run, project, emitter, "validation",
				CodeValidationFailed, validationErr.Error(), validationErr,
				tracker, runStart,
				fileMap, message)
		}
	}

	emitter.Emit(EventTurnEnd, TurnEndEvent{
		RunID:      runID,
		Turn:       turn,
		StopReason: "end_turn",
		Tokens:     trackerDelta(tracker),
	})
	emitCreditsUpdate(turn)

	// --- Step 13: success shutdown (continued in C2.20) ---
	partsMu.Lock()
	finalParts := append([]models.ChatMessagePart(nil), parts...)
	finalToolCalls := totalToolCalls
	partsMu.Unlock()
	if err := finaliseSuccess(ctx, deps, repo, run, emitter, fileMap,
		finalParts, tracker, runStart,
		finalToolCalls, turn, message, user, project); err != nil {
		return nil, err
	}
	return &RuntimeResult{
		Parts:      finalParts,
		ToolCalls:  finalToolCalls,
		Turns:      turn,
		StopReason: "completed",
	}, nil
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
	cr, cw := t.CacheTotals()
	return TokenUsage{Input: p, Output: c, CacheRead: cr, CacheWrite: cw}
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
	case "write_file", "write_files", "edit_file", "multi_edit", "patch_files", "delete_file":
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
// Falls back to provider.DefaultModel() so switching LLM_PROVIDER doesn't leave
// a hardcoded Anthropic model name in the request.
func resolveModel(sm secret.SecretManager, project *models.AppProject, provider *utils.LLMProvider) string {
	// Per-user configured model (from PostHog flag) takes priority over
	// global Infisical secrets.
	if provider != nil && provider.ConfiguredModel != "" {
		return provider.ConfiguredModel
	}
	if project != nil && project.Status == "draft" {
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
	if provider != nil {
		return provider.DefaultModel()
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
	// Basic history replay: keep full tool results. No separate file drawer.
	var out []utils.Message
	for _, m := range history {
		if m.ErrorKind != "" {
			// Instead of silently dropping failed assistant turns, inject a
			// synthetic user message that tells the LLM what happened. This
			// preserves resume context so that when the user types "continue"
			// the model knows a previous run failed and can pick up where it
			// left off rather than treating the request as brand-new.
			reason := strings.TrimSpace(m.Content)
			if reason == "" {
				reason = humanizeErrorKind(m.ErrorKind)
			}
			summary := fmt.Sprintf(
				"The previous assistant generation failed with: %s. Please continue from where it left off.",
				reason,
			)
			out = append(out, utils.Message{Role: "user", Content: summary})
			continue
		}
		content := m.Content
		// Historical user messages are stored literally (e.g. "/plan a blog").
		// Re-expand on every load so the LLM sees the same prompt body it saw
		// on the original turn. We deliberately do NOT replay the SystemAddendum
		// for historical turns — modal constraints like "do not write files"
		// only apply to the turn they were typed on.
		if m.Role == "user" {
			content, _, _ = ExpandSlashCommand(content)
			content = "<user_message>" + escapeUntrustedTag(content, "user_message") + "</user_message>"
			out = append(out, utils.Message{Role: m.Role, Content: content})
			continue
		}

		if m.Role == "assistant" {
			var parts []models.ChatMessagePart
			if len(m.Parts) > 0 && string(m.Parts) != "[]" {
				_ = json.Unmarshal(m.Parts, &parts)
			}

			// Fallback for legacy rows without Parts
			if len(parts) == 0 {
				out = append(out, utils.Message{Role: "assistant", Content: content})
				continue
			}

			var pendingText string
			var pendingReasoning string
			var pendingToolCalls []utils.ToolCall
			var pendingToolResults []utils.Message

			flush := func() {
				if pendingText == "" && pendingReasoning == "" && len(pendingToolCalls) == 0 {
					return
				}
				out = append(out, utils.Message{
					Role:             "assistant",
					Content:          pendingText,
					ReasoningContent: pendingReasoning,
					ToolCalls:        pendingToolCalls,
				})
				out = append(out, pendingToolResults...)
				pendingText = ""
				pendingReasoning = ""
				pendingToolCalls = nil
				pendingToolResults = nil
			}

			for _, p := range parts {
				if p.Type == "reasoning" {
					if len(pendingToolCalls) > 0 {
						flush()
					}
					pendingReasoning += p.Text
				} else if p.Type == "text" {
					if pendingText != "" || len(pendingToolCalls) > 0 {
						flush()
					}
					pendingText = p.Text
				} else if p.Type == "tool_call" {
					pendingToolCalls = append(pendingToolCalls, utils.ToolCall{
						ID:   p.ToolCallID,
						Type: "function",
						Function: &utils.FunctionCall{
							Name:      p.Name,
							Arguments: string(p.Input),
						},
					})
					pendingToolResults = append(pendingToolResults, utils.Message{
						Role:       "tool",
						ToolCallID: p.ToolCallID,
						Name:       p.Name,
						Content:    p.Result,
					})
				}
			}
			flush()
			continue
		}

		out = append(out, utils.Message{Role: m.Role, Content: content})
	}
	return out
}

// lastAssistantTurn returns the most recent non-error assistant message in
// history, or nil if there isn't one. (Kept for test compatibility during transition.)
func lastAssistantTurn(history []models.AppChatMessage) *models.AppChatMessage {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role == "assistant" && m.ErrorKind == "" {
			return &m
		}
	}
	return nil
}

// escapeUntrustedTag replaces literal closing-tag substrings inside untrusted
// content so the user can't break out of the wrapping delimiters. The LLM sees
// the escaped form (e.g. &lt;/user_message&gt;) as inert text.
func escapeUntrustedTag(content, tag string) string {
	closing := "</" + tag + ">"
	escaped := "&lt;/" + tag + "&gt;"
	return strings.ReplaceAll(content, closing, escaped)
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
			tracker, runStart,
			fileMap, userMessage)
	}

	// 1b. Auto-deploy the backend/ subtree if this turn touched it.
	// mobile_app_native projects often have a Next.js backend (auth, AI,
	// payments) that the user expects to "just work" after a chat turn.
	// Without this, the user has to remember to click Deploy after every
	// AI/auth feature ask — see the BackendAutoDeployer dependency type
	// for the full reasoning. Diff is run synchronously (cheap, single
	// git command) but the deploy itself is fired in a goroutine so the
	// user gets the chat response immediately and the deploy completes
	// over the next ~1 min in the background. Best-effort by design —
	// errors are swallowed so the chat turn never fails on a hiccup
	// in the Asynq queue or the credit checker.
	if project != nil &&
		project.ProjectType == models.ProjectTypeMobileAppNative &&
		deps.AutoDeployBackend != nil &&
		finalCommit != "" &&
		finalCommit != run.PreCommit {
		changed, diffErr := repo.WorkingTreeChangedPaths(run.PreCommit)
		if diffErr != nil {
			slog.Warn("agent: auto-deploy diff failed",
				"error", diffErr, "project_id", project.ID, "pre_commit", run.PreCommit)
		} else if anyBackendPath(changed) {
			projectCopy := *project
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := deps.AutoDeployBackend(bgCtx, &projectCopy); err != nil {
					slog.Warn("agent: auto-deploy backend failed",
						"error", err, "project_id", projectCopy.ID)
				}
			}()
		}
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
		Attachments:  []byte("[]"),
		ToolCalls:    toolCallsJSON,
		Parts:        partsJSON,
	}
	if _, insErr := deps.DB.InsertChatMessage(assistantMsg); insErr != nil {
		slog.Warn("failed to persist assistant chat message", "error", insErr, "run_id", run.ID)
	}

	// 3. Finalise the AppAgentRun row.
	promptTok, completionTok, totalTok, _ := tracker.Totals()
	cacheReadTok, cacheWriteTok := tracker.CacheTotals()
	durationMs := int(time.Since(runStart).Milliseconds())
	credits := creditsBilled(deps, tracker, project.ID.String())
	costUSD := estimateCost(run.Model, promptTok, completionTok)

	run.Status = models.AgentRunStatusCompleted
	run.FinalCommit = finalCommit
	run.Turns = turns
	run.ToolCallCount = totalToolCalls
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.CacheReadTokens = cacheReadTok
	run.CacheWriteTokens = cacheWriteTok
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
		Tokens:       TokenUsage{Input: promptTok, Output: completionTok, CacheRead: cacheReadTok, CacheWrite: cacheWriteTok},
		CreditsUsed:  credits,
		DurationMs:   durationMs,
	})

	// 6. Track PostHog.
	if deps.TrackUserEvent != nil {
		action := constants.EVENT_APP_CHAT_COMPLETED
		if isInitial {
			action = constants.EVENT_APP_GENERATION_COMPLETED
		}
		deps.TrackUserEvent(action, map[string]any{
			"app_id":            project.ID.String(),
			"app_title":         project.Name,
			"prompt_preview":    utils.TruncatePromptPreview(tracker.TraceInput, 50),
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

	// 6b. Fire lifecycle event — mirrors PostHog event names.
	if deps.SendResendEvent != nil {
		crmEvent := constants.EVENT_APP_CHAT_COMPLETED
		if isInitial {
			crmEvent = constants.EVENT_APP_GENERATION_COMPLETED
		}
		deps.SendResendEvent(crmEvent, map[string]any{
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
		case "write_file", "edit_file", "multi_edit", "delete_file":
			n++
		}
	}
	return n
}

// creditsBilled totals the credit cost across every recorded LLM call in the
// tracker, rounding each call up independently and weighting cache-read
// tokens at 1/cacheReadDivisor× input. Per-call rounding mirrors what users
// are actually charged — aggregating tokens first and rounding once
// systematically under-billed cache-heavy runs (e.g. 53 vs 71 credits on a
// 35-call agent loop). Falls back to the per-key defaults if PostHog is
// unreachable.
func creditsBilled(deps *RunDependencies, tracker *utils.TokenTracker, projectID ...string) int {
	if tracker == nil {
		return 0
	}
	tpc, om, crd := 0, 0, 0
	if deps != nil && deps.RemoteConfig != nil {
		planName := ""
		if len(projectID) > 0 && projectID[0] != "" && deps.PlanNameForProject != nil {
			planName = deps.PlanNameForProject(projectID[0])
		}
		if planName == "" {
			planName = deps.UserPlanName
		}
		if planName != "" {
			pn := strings.ToLower(planName)
			tpc = deps.RemoteConfig.GetInt(remoteconfig.PlanAICreditsTokensPerCredit(pn), remoteconfig.Global)
			om = deps.RemoteConfig.GetInt(remoteconfig.PlanAICreditsOutputMultiplier(pn), remoteconfig.Global)
			crd = deps.RemoteConfig.GetInt(remoteconfig.PlanAICreditsCacheReadDivisor(pn), remoteconfig.Global)
		}
		if tpc <= 0 {
			tpc = deps.RemoteConfig.GetInt(remoteconfig.AICreditsTokensPerCredit, remoteconfig.Global)
		}
		if om <= 0 {
			om = deps.RemoteConfig.GetInt(remoteconfig.AICreditsOutputMultiplier, remoteconfig.Global)
		}
		if crd <= 0 {
			crd = deps.RemoteConfig.GetInt(remoteconfig.AICreditsCacheReadDivisor, remoteconfig.Global)
		}
	}
	if tpc <= 0 {
		tpc, _ = remoteconfig.AICreditsTokensPerCredit.Fallback.(int)
	}
	if om <= 0 {
		om, _ = remoteconfig.AICreditsOutputMultiplier.Fallback.(int)
	}
	if crd <= 0 {
		crd, _ = remoteconfig.AICreditsCacheReadDivisor.Fallback.(int)
	}
	total := 0
	for _, e := range tracker.Entries() {
		total += models.TokensToCreditsWithConfig(e.PromptTokens, e.CompletionTokens, e.CacheReadInputTokens, tpc, om, crd, e.CacheReadInclusive)
	}
	return total
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
	tracker *utils.TokenTracker,
	runStart time.Time,
	fileMap *SafeFileMap,
	originalUserMessage string,
) error {
	// 1. Attempt to commit partial progress so the user isn't left with
	// nothing when a run fails mid-loop (credits exhausted, timeout, etc.).
	// We skip this when the failure itself happened during commit (phase
	// == "commit") to avoid retrying a broken commit.
	checkpointCommit := ""
	if phase != "commit" && phase != "setup" && repo != nil && fileMap != nil {
		sanitizeFileMapForBuild(fileMap)
		if partialCommit, err := FinaliseRun(repo, fileMap, originalUserMessage); err == nil {
			if partialCommit != run.PreCommit {
				run.FinalCommit = partialCommit
				checkpointCommit = partialCommit
				emitter.Emit(EventCheckpoint, CheckpointEvent{
					RunID:  run.ID,
					Commit: partialCommit,
					Reason: string(code),
				})
				slog.Info("agent: committed partial progress on failure",
					"run_id", run.ID, "phase", phase, "code", code,
					"pre_commit", run.PreCommit, "partial_commit", partialCommit)
			}
		} else {
			slog.Warn("agent: failed to commit partial progress on failure",
				"run_id", run.ID, "error", err)
		}
	}
	failureMessage := failureDisplayMessage(message, wrapped)
	emitter.Emit(EventError, ErrorEvent{
		RunID:   run.ID,
		Phase:   phase,
		Message: failureMessage,
		Code:    code,
	})

	// 2. Persist failure marker message so the chat UI isn't orphaned. The
	// frontend renders a rich error card off ErrorKind; Content is a still-
	// readable fallback so old clients during a rolling deploy don't show
	// empty bubbles.
	errorKind := classifyErrorKind(code, wrapped)

	fallbackText := failureMessage
	if checkpointCommit != "" {
		fallbackText = "Generation failed. Partial progress was saved as a checkpoint."
	}

	fallbackParts, _ := json.Marshal([]models.ChatMessagePart{
		{Type: "text", Text: fallbackText},
	})
	orphanMsg := &models.AppChatMessage{
		AppProjectID: run.AppProjectID,
		RunID:        &run.ID,
		Role:         "assistant",
		Content:      fallbackText,
		Images:       []byte("[]"),
		Attachments:  []byte("[]"),
		ToolCalls:    []byte("[]"),
		Parts:        fallbackParts,
		ErrorKind:    errorKind,
	}
	if _, err := deps.DB.InsertChatMessage(orphanMsg); err != nil {
		slog.Warn("failed to persist failure message", "error", err, "run_id", run.ID)
	}

	// 3. Finalise the run row. We bill for whatever tokens were actually
	// consumed before the failure — Anthropic charges us regardless of
	// whether the run completed, so a canceled or budget_exceeded run still
	// represents real spend. Without this, a free user could burn unlimited
	// cost by repeatedly starting runs that hit the token cap or clicking
	// Stop mid-turn.
	var promptTok, completionTok int
	var cacheReadTok, cacheWriteTok int
	if tracker != nil {
		promptTok, completionTok, _, _ = tracker.Totals()
		cacheReadTok, cacheWriteTok = tracker.CacheTotals()
	}
	credits := creditsBilled(deps, tracker, run.AppProjectID.String())
	costUSD := estimateCost(run.Model, promptTok, completionTok)
	run.Status = mapCodeToStatus(code)
	run.Error = fmt.Sprintf("%s: %s", code, failureMessage)
	run.PromptTokens = promptTok
	run.CompletionTokens = completionTok
	run.CacheReadTokens = cacheReadTok
	run.CacheWriteTokens = cacheWriteTok
	run.CostUSD = costUSD
	run.CreditsUsed = credits
	run.DurationMs = int(time.Since(runStart).Milliseconds())
	run.CompletedAt = time.Now()
	if err := deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("failed to finalise failed agent run row", "error", err, "run_id", run.ID)
	}

	// 3b. Deduct credits for consumed tokens. Uses the same action label as
	// the success path so ai_credit_transactions rows group consistently.
	if deps.DeductCredits != nil && credits > 0 {
		deductAction := runAction(project)
		if err := deps.DeductCredits(context.Background(), credits, deductAction, run.AppProjectID.String()); err != nil {
			slog.Warn("failed to deduct credits for failed run", "error", err, "credits", credits, "run_id", run.ID)
		}
	}

	// 4. Track PostHog. Mirror the success-path properties so analytics can
	// compare model/token spend and durations across success and failure on
	// the same axes — without this, error funnels can't surface "expensive
	// failures" or per-model failure rates. Split chat vs initial generation
	// onto separate events so the activation funnel can measure first-app
	// success rate independently from steady-state chat reliability — without
	// this, a spike in chat errors looks identical to a regression in initial
	// generation in dashboards.
	isInitial := project != nil && project.Status == "draft"
	failEvent := constants.EVENT_APP_CHAT_FAILED
	if isInitial {
		failEvent = constants.EVENT_APP_GENERATION_FAILED
	}
	totalTok := promptTok + completionTok
	durationSeconds := float64(run.DurationMs) / 1000.0
	if deps.TrackUserEvent != nil {
		failedProps := map[string]any{
			"app_id":            run.AppProjectID.String(),
			"code":              string(code),
			"phase":             phase,
			"duration_seconds":  durationSeconds,
			"credits_used":      credits,
			"model":             run.Model,
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"total_tokens":      totalTok,
		}
		if project != nil {
			failedProps["app_title"] = project.Name
		}
		if tracker != nil && tracker.TraceInput != "" {
			failedProps["prompt_preview"] = utils.TruncatePromptPreview(tracker.TraceInput, 50)
		}
		deps.TrackUserEvent(failEvent, failedProps)
	}

	// 6b. Fire lifecycle event.
	if deps.SendResendEvent != nil {
		deps.SendResendEvent(failEvent, map[string]any{
			"app_id":           run.AppProjectID.String(),
			"code":             string(code),
			"phase":            phase,
			"duration_seconds": durationSeconds,
			"credits_used":     credits,
			"model":            run.Model,
		})
	}

	// 5. PostHog LLM Analytics: surface the failure on the LLM dashboard.
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
	// Flag the tracker so the $ai_trace event carries $ai_is_error even when
	// no $ai_generation child captured the failure (e.g. deadline mid-stream
	// before the first round completed).
	errMsg := fmt.Sprintf("%s: %s", code, failureMessage)
	if wrapped != nil && !strings.Contains(failureMessage, wrapped.Error()) {
		errMsg = fmt.Sprintf("%s (%s)", errMsg, wrapped.Error())
	}
	if tracker != nil {
		tracker.TraceErrorCode = string(code)
		tracker.TraceErrorMessage = errMsg
	}
	// Always emit the grouping $ai_trace — even when tracker is empty — so
	// every agent run shows up on the LLM Analytics dashboard. The recorder
	// skips the per-call loop and the usage DB write when totals are zero.
	if deps.RecordLLMUsage != nil && tracker != nil {
		deps.RecordLLMUsage(tracker, action, run.Model, run.Provider,
			projectID, projectType)
	}
	if deps.RecordLLMError != nil {
		deps.RecordLLMError(tracker, action, run.Model, run.Provider,
			projectID, projectType,
			int64(run.DurationMs), string(code), errMsg)
	}

	_ = ctx
	return New(code, phase, failureMessage, wrapped)
}

func failureDisplayMessage(message string, wrapped error) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "Generation failed"
	}
	if wrapped == nil {
		return msg
	}
	detail := strings.TrimSpace(wrapped.Error())
	if detail == "" || strings.Contains(msg, detail) {
		return msg
	}
	return msg + ": " + detail
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
// anyBackendPath returns true when the slice contains a path under
// backend/. Used by the auto-deploy hook to decide whether the chat turn
// produced a deployable change. Cheap; runs once per turn.
func anyBackendPath(paths []string) bool {
	for _, p := range paths {
		if strings.HasPrefix(p, "backend/") {
			return true
		}
	}
	return false
}

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
	case CodeBudgetExceeded, CodeTruncated, CodeToolRoundsExceeded:
		// Truncation is a model-side budget hit — same bucket as wallclock
		// for UI purposes ("your request was too big, try a smaller one").
		return models.AgentRunStatusBudgetExceeded
	case CodeCanceled, CodeCreditsExhausted:
		return models.AgentRunStatusCanceled
	default:
		return models.AgentRunStatusFailed
	}
}

// classifyCtxErr maps a ctx.Err() to the right agent ErrorCode + user-facing
// message. context.DeadlineExceeded means our own wallclock cap fired — that's
// a BUDGET_EXCEEDED. context.Canceled means something external (Stop button,
// process shutdown) called cancel() — that's a CANCELED. These used to share
// a single "run canceled or deadline exceeded" message which left users unable
// to tell "I hit the server's time limit" from "someone stopped me".
func classifyCtxErr(err error) (ErrorCode, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return CodeBudgetExceeded, fmt.Sprintf("run exceeded the %d-minute time limit — try a smaller change", defaultMaxWallclockMs/60000)
	}
	return CodeCanceled, "run was canceled"
}

// classifyErrorKind maps an agent ErrorCode + the wrapped underlying error to
// the short string persisted on AppChatMessage.ErrorKind. The frontend uses
// this to render the same rich error card from history that it shows live.
func classifyErrorKind(code ErrorCode, wrapped error) string {
	switch code {
	case CodeCreditsExhausted:
		return "credit_out"
	case CodeRateLimited:
		return "rate_limit"
	case CodeProviderError:
		return "provider_error"
	case CodeBudgetExceeded:
		return "budget_exceeded"
	case CodeToolRoundsExceeded:
		return "tool_rounds_exceeded"
	case CodeCanceled:
		if wrapped != nil && errors.Is(wrapped, context.Canceled) {
			return "context_canceled"
		}
		return "context_canceled"
	case CodeInternal, CodeGitError, CodeToolFailed, CodeValidationFailed,
		CodeTruncated, CodeRepetitionLoop, CodeRunInProgress:
		return "internal"
	}
	return "unknown"
}

// humanizeErrorKind converts the ErrorKind string persisted on failed
// assistant messages into a concise, user-facing (and LLM-facing) description.
func humanizeErrorKind(kind string) string {
	switch kind {
	case "credit_out":
		return "AI credits exhausted"
	case "rate_limit":
		return "rate limit hit"
	case "provider_error":
		return "LLM provider error"
	case "timeout":
		return "run timed out"
	case "context_canceled":
		return "run was canceled"
	case "tool_rounds_exceeded":
		return "tool round limit reached; reply continue to keep going"
	case "internal":
		return "internal error"
	default:
		return "unknown error"
	}
}

// isContinueMessage returns true when the user's message is clearly an attempt
// to resume after an interrupted run. Covers English and Turkish (the primary
// languages used with the product) plus common variants.
func isContinueMessage(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	// Strip trailing punctuation so "continue!" and "devam et." still match.
	lower = strings.TrimRight(lower, "!.?")
	continues := []string{
		"continue", "go on", "resume", "keep going", "proceed",
		"carry on", "pick up where you left off", "finish it",
		"devam et", "devam",
	}
	for _, c := range continues {
		if lower == c {
			return true
		}
	}
	return false
}

// buildResumeContext queries the most recent failed run for this project and
// produces a concise block that tells the LLM what was accomplished before the
// failure. It is prepended to the current user turn when isContinueMessage
// detects a resume intent.
func buildResumeContext(
	deps *RunDependencies,
	repo *gitrepo.Repo,
	projectID uuid.UUID,
) string {
	if deps == nil || deps.DB == nil || repo == nil {
		return ""
	}
	lastRun, err := deps.DB.GetLastNonSuccessAgentRun(projectID)
	if err != nil || lastRun == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Resume Context]\n")
	sb.WriteString("The previous assistant run failed")
	if lastRun.ToolCallCount > 0 {
		fmt.Fprintf(&sb, " after %d tool call(s)", lastRun.ToolCallCount)
	}
	if lastRun.Turns > 0 {
		fmt.Fprintf(&sb, " across %d turn(s)", lastRun.Turns)
	}
	sb.WriteString(".\n")
	sb.WriteString("Reason: ")
	sb.WriteString(classifyErrorKindFromStatus(lastRun.Status, lastRun.Error))
	sb.WriteString(".\n")
	if mem := ParseAgentRunMemory(lastRun.Memory); mem != nil {
		if rendered := mem.Render(nil); rendered != "" {
			sb.WriteString("\n")
			sb.WriteString(rendered)
			sb.WriteString("\n")
		}
	}
	// If partial progress was committed (Fix 2), list the files that changed.
	if lastRun.FinalCommit != "" && lastRun.FinalCommit != lastRun.PreCommit {
		changed, diffErr := repo.ChangedPathsSince(lastRun.PreCommit)
		if diffErr == nil && len(changed) > 0 {
			sb.WriteString("Files already written/modified before the failure:\n")
			for _, p := range changed {
				fmt.Fprintf(&sb, "  - %s\n", p)
			}
		}
	}
	sb.WriteString("Please continue from where the previous attempt left off.\n")
	sb.WriteString("[/Resume Context]\n\n")
	return sb.String()
}

// classifyErrorKindFromStatus maps an AppAgentRun status + error string back
// to a human-readable failure reason for the resume context.
func classifyErrorKindFromStatus(status, errStr string) string {
	switch status {
	case models.AgentRunStatusCanceled:
		if strings.Contains(errStr, "credit") {
			return "AI credits exhausted"
		}
		return "run was canceled"
	case models.AgentRunStatusBudgetExceeded:
		return "run exceeded budget/time limit"
	case models.AgentRunStatusFailed:
		if strings.Contains(errStr, "provider") {
			return "LLM provider error"
		}
		if strings.Contains(errStr, "repetition") {
			return "model got stuck in a repetition loop"
		}
		return "internal error"
	default:
		return "unknown error"
	}
}
