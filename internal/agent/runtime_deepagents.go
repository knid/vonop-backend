package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/deepagents"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/services/workspacelock"
	"github.com/knid/wemob/internal/utils"
)

const deepagentsSchemaVersion = "2026-06-09"

type DeepAgentsRuntime struct {
	client *deepagents.Client
}

func (r *DeepAgentsRuntime) Kind() RuntimeKind {
	return RuntimeDeepAgents
}

type deepAgentsEventDataError struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	Phase       string `json:"phase"`
	Recoverable bool   `json:"recoverable"`
}

type deepAgentsEventDataMessage struct {
	Delta    string `json:"delta"`
	Turn     int    `json:"turn"`
	Agent    string `json:"agent"`
	Subagent string `json:"subagent"`
}

type deepAgentsEventDataTool struct {
	ToolCallID    string          `json:"tool_call_id"`
	ToolName      string          `json:"tool_name"`
	InputPreview  json.RawMessage `json:"input_preview"`
	RiskLevel     string          `json:"risk_level"`
	Status        string          `json:"status"`
	DurationMs    int             `json:"duration_ms"`
	DurationMsRaw float64         `json:"duration_ms_float"`
	Summary       string          `json:"summary"`
	IsError       bool            `json:"is_error"`
	Result        string          `json:"result"`
	Input         json.RawMessage `json:"input"`
}

type deepAgentsEventDataSubagentStart struct {
	SubagentID  string `json:"subagent_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parent      string `json:"parent"`
}

type deepAgentsEventDataSubagentEnd struct {
	SubagentID string `json:"subagent_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Summary    string `json:"summary"`
}

type deepAgentsEventDataSubagentProgress struct {
	SubagentID string `json:"subagent_id"`
	Name       string `json:"name"`
	Message    string `json:"message"`
}

type deepAgentsEventDataDone struct {
	Summary    string `json:"summary"`
	Turns      int    `json:"turns"`
	ToolCalls  int    `json:"tool_calls"`
	ModelCalls int    `json:"model_calls"`
	StopReason string `json:"stop_reason"`
}

// deepAgentsEventDataLLMUsage mirrors the payload emitted by the Python
// LangChain callback handler. Field names match the Python side verbatim
// so the JSON unmarshal is a 1:1 mapping. See utils.PythonUsageEntry for
// the canonical Go struct that lives in the TokenTracker.
type deepAgentsEventDataLLMUsage struct {
	SpanID           string  `json:"span_id"`
	Provider         string  `json:"provider"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CacheReadTokens  int     `json:"cache_read_tokens"`
	CacheWriteTokens int     `json:"cache_write_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMs        int     `json:"latency_ms"`
	Subagent         string  `json:"subagent"`
	RuntimeKind      string  `json:"runtime_kind"`
}

func (r *DeepAgentsRuntime) Run(ctx context.Context, req RuntimeRequest, emitter EventEmitter) (*RuntimeResult, error) {
	if r == nil {
		return nil, New(CodeInternal, "setup", "runtime is not initialized", nil)
	}
	if req.Deps == nil || req.Project == nil || req.User == nil {
		return nil, New(CodeInternal, "setup", "missing required run context", nil)
	}
	if req.Deps.DB == nil || req.Deps.GitRepo == nil {
		return nil, New(CodeInternal, "setup", "missing run dependencies", nil)
	}
	if req.Message == "" {
		return nil, New(CodeInternal, "setup", "message is required", nil)
	}

	runStart := time.Now()
	limits := req.Limits
	if limits.MaxWallClockMs <= 0 {
		limits = defaultRuntimeLimits()
	}
	runIDStr := strings.TrimSpace(req.RunID)
	if runIDStr == "" {
		runIDStr = uuid.NewString()
	}
	runID, err := uuid.Parse(runIDStr)
	if err != nil {
		return nil, New(CodeInternal, "setup", "invalid run ID", err)
	}

	ctx, cancelDeadline := context.WithTimeout(ctx, time.Duration(limits.MaxWallClockMs)*time.Millisecond)
	defer cancelDeadline()

	streamDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			callCtx, callCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer callCancel()
			if cancelErr := r.client.CancelRun(callCtx, runID.String()); cancelErr != nil {
				slog.Warn("agent: failed to cancel deepagents runtime", "run_id", req.RunID, "error", cancelErr)
			}
		case <-streamDone:
		}
	}()
	defer close(streamDone)

	lastEventAt := time.Now()
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		defer close(heartbeatDone)
		for {
			select {
			case <-streamDone:
				return
			case <-ticker.C:
				hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				if hbErr := r.client.Heartbeat(hbCtx, runID.String()); hbErr != nil {
					slog.Warn("agent: deepagents heartbeat failed", "run_id", runID, "error", hbErr)
				}
				hbCancel()
				stallDuration := time.Since(lastEventAt)
				if stallDuration > 60*time.Second {
					slog.Warn("agent: deepagents stream stall detected",
						"run_id", runID,
						"stall_seconds", int(stallDuration.Seconds()))
				}
			}
		}
	}()
	defer func() {
		<-heartbeatDone
	}()

	if existing, existingErr := req.Deps.DB.GetActiveAgentRun(req.Project.ID); existingErr == nil && existing != nil {
		return nil, New(CodeRunInProgress, "setup", "another run is already in progress for this project", nil)
	}

	repo, err := req.Deps.GitRepo.OpenOrInit(req.Project.ID.String())
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to open project repo", err)
	}

	if req.Project.Status == "draft" {
		existingFiles, _ := repo.ListFiles()
		if len(existingFiles) == 0 {
			if n, injErr := InjectTemplates(repo, req.Project); injErr != nil {
				return nil, New(CodeGitError, "setup", fmt.Sprintf("template injection failed after %d files", n), injErr)
			}
		}
	}

	fileMap, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to read project files", err)
	}

	preCommit, err := PreRunCommit(repo, runID.String())
	if err != nil {
		return nil, New(CodeGitError, "setup", "failed to create pre-run snapshot", err)
	}

	leaseOwner := "agent:" + runID.String()
	if req.Deps.WorkerID != "" {
		leaseOwner = "worker:" + req.Deps.WorkerID + ":" + runID.String()
	}
	var workspaceLease *workspacelock.Lease
	if req.Deps.WorkspaceLock != nil {
		workspaceLease, err = req.Deps.WorkspaceLock.Acquire(ctx, req.Project.ID.String(), leaseOwner)
		if err != nil {
			if _, busy := err.(*workspacelock.LockBusyError); busy {
				return nil, New(CodeRunInProgress, "setup", "another process is modifying this workspace", err)
			}
			return nil, New(CodeInternal, "setup", "failed to acquire workspace lock", err)
		}
		defer func() {
			if err := workspaceLease.Release(); err != nil {
				slog.Warn("agent: failed to release workspace lock", "project_id", req.Project.ID, "run_id", runID, "error", err)
			}
		}()
	}

	provider := req.Provider
	if provider == nil {
		provider = req.Deps.LLMProvider
	}
	providerName := "openrouter"
	if provider != nil && provider.Name != "" {
		providerName = provider.Name
	}
	model := resolveModel(req.Deps.SecretManager, req.Project, provider)
	isFirstGen := req.Project != nil && req.Project.Status == "draft"

	run := &models.AppAgentRun{
		ID:           runID,
		AppProjectID: req.Project.ID,
		UserID:       req.User.ID,
		Status:       models.AgentRunStatusRunning,
		PreCommit:    preCommit,
		Model:        model,
		Provider:     providerName,
		Todos:        []byte("[]"),
		Memory:       []byte("{}"),
		RuntimeKind:  string(RuntimeDeepAgents),
	}
	if _, err := req.Deps.DB.InsertAgentRun(run); err != nil {
		if isUniqueViolation(err) {
			return nil, New(CodeRunInProgress, "setup", "another run is already in progress for this project", err)
		}
		return nil, New(CodeInternal, "setup", "failed to record run", err)
	}

	attachmentSnapshot := make([]models.ChatMessageAttachment, 0, len(req.Attachments.Current))
	for _, a := range req.Attachments.Current {
		attachmentSnapshot = append(attachmentSnapshot, models.ChatMessageAttachment{
			AssetID:     a.ID,
			FileName:    a.FileName,
			ContentType: a.ContentType,
			FileSize:    a.FileSize,
			URL:         a.PublicURL,
			IsImage:     strings.HasPrefix(a.ContentType, "image/"),
		})
	}
	attachmentsJSON := mustJSON(attachmentSnapshot)
	userImages := make([]string, 0, len(attachmentSnapshot))
	for _, a := range attachmentSnapshot {
		if a.IsImage {
			userImages = append(userImages, a.URL)
		}
	}
	userParts, _ := json.Marshal([]models.ChatMessagePart{{Type: "text", Text: req.Message}})
	userMsg := &models.AppChatMessage{
		AppProjectID: req.Project.ID,
		RunID:        &runID,
		Role:         "user",
		Content:      req.Message,
		Images:       mustJSON(normaliseImages(userImages)),
		Attachments:  attachmentsJSON,
		ToolCalls:    []byte("[]"),
		Parts:        userParts,
	}
	if _, err := req.Deps.DB.InsertChatMessage(userMsg); err != nil {
		return nil, New(CodeInternal, "setup", "failed to persist user message", err)
	}
	run.UserMessageID = &userMsg.ID
	if err := req.Deps.DB.UpdateAgentRun(run); err != nil {
		slog.Warn("agent: deepagents failed to persist user_message_id", "run_id", runID, "error", err)
	}

	tracker := utils.NewTokenTracker()
	tracker.UserID = req.User.ID.String()
	tracker.ProjectID = req.Project.ID.String()
	tracker.OrganizationID = req.Project.OrganizationID.String()
	tracker.TraceID = runID.String()
	tracker.RunID = runID.String()
	tracker.TraceInput = req.Message
	ctx = utils.WithTokenTracker(ctx, tracker)

	var creditCheck func() bool
	if req.Deps.CreditChecker != nil {
		creditCheck = req.Deps.CreditChecker(req.Project.ID.String(), tracker)
	}
	if creditCheck != nil && !creditCheck() {
		return nil, New(CodeCreditsExhausted, "setup", "insufficient credits to start run", nil)
	}

	registry := BuildRegistryFor(req.Project, req.Deps, model)
	filteredRegistry := FilterRegistryForDeepAgents(registry)
	tools := []string{}
	toolSpecs := []deepagents.ToolSpec{}
	if registry != nil {
		tools = registry.Names()
	}
	if filteredRegistry != nil {
		toolSpecs = buildDeepAgentsToolSpecs(filteredRegistry)
		tools = filteredRegistry.Names()
	}
	emitter.Emit(EventRunStart, RunStartEvent{
		RunID:           runID,
		PreCommit:       preCommit,
		Model:           model,
		Provider:        providerName,
		MaxTurns:        limits.MaxModelCalls,
		Tools:           tools,
		AgentID:         "deepagents",
		AgentName:       "DeepAgents Runtime",
		Skills:          []string{},
		RequiredSkills:  []string{},
		SuggestedSkills: []string{},
		AvailableSkills: []string{},
		LoadedSkills:    []string{},
		Handoffs:        []string{},
		RuntimeKind:     string(RuntimeDeepAgents),
		RuntimeVersion:  runtimeVersion,
		SubagentNames:   deepagentsSubagentNames,
	})
	baseFileMap := fileMap
	var poller *WorkspaceDiffPoller
	if preCommit != "" {
		poller = NewWorkspaceDiffPoller(repo, preCommit, func(paths []string) {
			emitWorkspaceDiffEvents(repo, baseFileMap, runID, emitter, paths)
		})
		poller.Start()
		poller.WithContext(ctx)
	}

	parts := make([]models.ChatMessagePart, 0, 8)
	toolIndex := map[string]int{}
	partsMu := sync.Mutex{}
	turns := 1
	toolCalls := 0
	var doneTurns int
	var doneToolCalls int
	var stopReason string
	var firstErr *Error

	expandedMessage, _, _ := ExpandSlashCommand(req.Message)

	err = r.client.StreamRun(ctx, deepagents.DeepAgentsRunRequest{
		SchemaVersion:     deepagentsSchemaVersion,
		JobID:             req.JobID,
		RunID:             runID.String(),
		TraceID:           runID.String(),
		UserID:            req.User.ID.String(),
		OrganizationID:    req.Project.OrganizationID.String(),
		ProjectID:         req.Project.ID.String(),
		ProjectType:       req.Project.ProjectType,
		FrontendRuntime:   req.Project.FrontendRuntime,
		IsFirstGeneration: isFirstGen,
		ReadOnly:          false,
		Message:           expandedMessage,
		Attachments:       toDeepAgentsAttachments(req.Attachments.Current),
		Repo: deepagents.RepoSpec{
			MountBase:   strings.TrimSpace(req.Deps.GitRepo.BasePath),
			ProjectID:   req.Project.ID.String(),
			VirtualRoot: "/workspace",
		},
		Model: deepagents.ModelSpec{
			Provider:       providerName,
			Model:          model,
			LangchainModel: providerName + ":" + model,
		},
		Limits: deepagents.Limits{
			MaxWallclockMs:         limits.MaxWallClockMs,
			MaxModelCalls:          limits.MaxModelCalls,
			MaxToolCalls:           limits.MaxToolCalls,
			MaxOutputTokensPerCall: limits.MaxOutputTokensPerCall,
		},
		Tools:  toolSpecs,
		Skills: deepagents.SkillsSpec{},
		Metadata: map[string]any{
			"app_title":  req.Project.Name,
			"pre_commit": preCommit,
		},
	}, func(evt deepagents.DeepAgentsEvent) error {
		lastEventAt = time.Now()
		switch evt.Type {
		case "message_delta":
			var payload deepAgentsEventDataMessage
			_ = json.Unmarshal(evt.Data, &payload)
			nextTurn := payload.Turn
			if nextTurn <= 0 {
				nextTurn = turns
			}
			if nextTurn > turns {
				turns = nextTurn
			}
			if payload.Delta == "" {
				return nil
			}
			partsMu.Lock()
			if n := len(parts); n > 0 && parts[n-1].Type == "text" {
				parts[n-1].Text += payload.Delta
			} else {
				parts = append(parts, models.ChatMessagePart{Type: "text", Text: payload.Delta})
			}
			partsMu.Unlock()
			emitter.Emit(EventNarration, NarrationEvent{RunID: runID, Turn: nextTurn, Delta: payload.Delta})
			return nil

		case "reasoning_delta":
			var payload deepAgentsEventDataMessage
			_ = json.Unmarshal(evt.Data, &payload)
			if payload.Delta == "" {
				return nil
			}
			partsMu.Lock()
			if n := len(parts); n > 0 && parts[n-1].Type == "reasoning" {
				parts[n-1].Text += payload.Delta
			} else {
				parts = append(parts, models.ChatMessagePart{Type: "reasoning", Text: payload.Delta})
			}
			partsMu.Unlock()
			emitter.Emit(EventReasoning, ReasoningEvent{RunID: runID, Turn: turns, Delta: payload.Delta})
			return nil

		case "tool_call_start":
			var payload deepAgentsEventDataTool
			_ = json.Unmarshal(evt.Data, &payload)
			if payload.ToolCallID == "" {
				payload.ToolCallID = uuid.NewString()
			}
			toolCalls++
			input := payload.Input
			if input == nil {
				input = json.RawMessage("{}")
			}
			partsMu.Lock()
			toolIndex[payload.ToolCallID] = len(parts)
			parts = append(parts, models.ChatMessagePart{
				Type:       "tool_call",
				ToolCallID: payload.ToolCallID,
				Name:       payload.ToolName,
				Input:      input,
				DurationMs: 0,
			})
			partsMu.Unlock()
			emitter.Emit(EventToolCallStart, ToolCallStartEvent{
				RunID:        runID,
				Turn:         turns,
				ToolCallID:   payload.ToolCallID,
				ToolName:     payload.ToolName,
				RiskLevel:    payload.RiskLevel,
				InputPreview: payload.InputPreview,
			})
			return nil

		case "tool_call_result":
			var payload deepAgentsEventDataTool
			_ = json.Unmarshal(evt.Data, &payload)
			partsMu.Lock()
			if idx, ok := toolIndex[payload.ToolCallID]; ok {
				parts[idx].Result = payload.Result
				parts[idx].IsError = payload.IsError
				parts[idx].DurationMs = payload.DurationMs
				if payload.DurationMs == 0 && payload.DurationMsRaw > 0 {
					parts[idx].DurationMs = int(payload.DurationMsRaw)
				}
				if payload.Input != nil {
					parts[idx].Input = payload.Input
				}
			}
			partsMu.Unlock()
			emitter.Emit(EventToolCallResult, ToolCallResultEvent{
				RunID:      runID,
				ToolCallID: payload.ToolCallID,
				ToolName:   payload.ToolName,
				RiskLevel:  payload.RiskLevel,
				Status:     payload.Status,
				DurationMs: payload.DurationMs,
				Summary:    payload.Summary,
				IsError:    payload.IsError,
			})
			return nil

		case "done":
			var payload deepAgentsEventDataDone
			_ = json.Unmarshal(evt.Data, &payload)
			doneTurns = payload.Turns
			doneToolCalls = payload.ToolCalls
			stopReason = payload.StopReason
			if stopReason == "" {
				stopReason = "completed"
			}
			if payload.Turns > 0 {
				turns = payload.Turns
			}
			return nil

		case "error":
			var payload deepAgentsEventDataError
			_ = json.Unmarshal(evt.Data, &payload)
			code := mapPythonErrorCode(payload.Code)
			msg := payload.Message
			if msg == "" {
				msg = "deepagents runtime failed"
			}
			phase := payload.Phase
			if strings.TrimSpace(phase) == "" {
				phase = "agent_loop"
			}
			firstErr = New(code, phase, msg, nil)
			return nil

		case "subagent_start":
			var payload deepAgentsEventDataSubagentStart
			_ = json.Unmarshal(evt.Data, &payload)
			emitter.Emit(EventSubagentStart, SubagentStartEvent{
				RunID:       runID,
				SubagentID:  payload.SubagentID,
				Name:        payload.Name,
				Description: payload.Description,
				Parent:      payload.Parent,
			})
			return nil

		case "subagent_end":
			var payload deepAgentsEventDataSubagentEnd
			_ = json.Unmarshal(evt.Data, &payload)
			emitter.Emit(EventSubagentEnd, SubagentEndEvent{
				RunID:      runID,
				SubagentID: payload.SubagentID,
				Name:       payload.Name,
				Status:     payload.Status,
				Summary:    payload.Summary,
			})
			return nil

		case "subagent_progress":
			var payload deepAgentsEventDataSubagentProgress
			_ = json.Unmarshal(evt.Data, &payload)
			emitter.Emit(EventSubagentProgress, SubagentProgressEvent{
				RunID:      runID,
				SubagentID: payload.SubagentID,
				Name:       payload.Name,
				Message:    payload.Message,
			})
			return nil

		case "llm_usage":
			// PR 9: forward per-model-call usage from the Python LangChain
			// callback into the same TokenTracker the native runtime uses.
			// finaliseSuccess / finaliseFailureV2 then bill from the unified
			// set of entries — no native-vs-deepagents split in PostHog or
			// in the LLMTokenUsage row.
			var payload deepAgentsEventDataLLMUsage
			_ = json.Unmarshal(evt.Data, &payload)
			if tracker == nil {
				return nil
			}
			tracker.RecordFromPythonUsage(utils.PythonUsageEntry{
				SpanID:           payload.SpanID,
				Provider:         payload.Provider,
				Model:            payload.Model,
				PromptTokens:     payload.PromptTokens,
				CompletionTokens: payload.CompletionTokens,
				CacheReadTokens:  payload.CacheReadTokens,
				CacheWriteTokens: payload.CacheWriteTokens,
				CostUSD:          payload.CostUSD,
				LatencyMs:        payload.LatencyMs,
				Subagent:         payload.Subagent,
				RuntimeKind:      payload.RuntimeKind,
			})
			if creditCheck != nil && !creditCheck() {
				firstErr = New(CodeCreditsExhausted, "agent_loop", "credits exhausted during run", nil)
				return fmt.Errorf("credits exhausted")
			}
			return nil
		}

		return nil
	})
	if poller != nil {
		if paths, pollErr := poller.Poll(); pollErr != nil {
			slog.Warn("agent: failed final workspace poll", "run_id", runID, "error", pollErr)
		} else if len(paths) > 0 {
			emitWorkspaceDiffEvents(repo, baseFileMap, runID, emitter, paths)
		}
		poller.Stop()
	}

	reloadedFileMap, reloadErr := ReloadFileMapFromRepo(repo)
	if reloadErr == nil {
		fileMap = reloadedFileMap
	} else {
		slog.Warn("agent: failed to reload fileMap after deepagents run",
			"run_id", runID, "error", reloadErr)
	}

	if err != nil {
		if firstErr != nil {
			_ = finaliseFailureV2(ctx, req.Deps, repo, run, req.Project, emitter, firstErr.Phase, firstErr.Code, firstErr.Message, firstErr.Wrapped, tracker, runStart, fileMap, req.Message)
			return nil, firstErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			code, msg := classifyCtxErr(ctx.Err())
			_ = finaliseFailureV2(ctx, req.Deps, repo, run, req.Project, emitter, "agent_loop", code, msg, ctx.Err(), tracker, runStart, fileMap, req.Message)
			return nil, New(code, "agent_loop", msg, ctx.Err())
		}
		code := CodeRuntimeError
		msg := "runtime communication failed"
		_ = finaliseFailureV2(ctx, req.Deps, repo, run, req.Project, emitter, "agent_loop", code, msg, err, tracker, runStart, fileMap, req.Message)
		return nil, New(code, "agent_loop", msg, err)
	}

	if firstErr != nil {
		_ = finaliseFailureV2(ctx, req.Deps, repo, run, req.Project, emitter, firstErr.Phase, firstErr.Code, firstErr.Message, firstErr.Wrapped, tracker, runStart, fileMap, req.Message)
		return nil, firstErr
	}

	if validationErr := ValidateAgentOutput(ResolveAgentProfile(req.Project), fileMap, nil, req.Message, isFirstGen, req.Project, nil); validationErr != nil {
		_ = finaliseFailureV2(ctx, req.Deps, repo, run, req.Project, emitter, "validation",
			CodeValidationFailed, validationErr.Error(), validationErr,
			tracker, runStart, fileMap, req.Message)
		return nil, New(CodeValidationFailed, "validation", validationErr.Error(), validationErr)
	}

	if doneTurns == 0 {
		doneTurns = turns
	}
	if doneToolCalls == 0 {
		doneToolCalls = toolCalls
	}
	run.RuntimeVersion = runtimeVersion
	if err := finaliseSuccess(ctx, req.Deps, repo, run, emitter, fileMap, parts, tracker, runStart, doneToolCalls, doneTurns, req.Message, req.User, req.Project); err != nil {
		return nil, err
	}

	if stopReason == "" {
		stopReason = "completed"
	}
	return &RuntimeResult{
		Parts:      parts,
		ToolCalls:  doneToolCalls,
		Turns:      doneTurns,
		StopReason: stopReason,
	}, nil
}

func emitWorkspaceDiffEvents(
	repo *gitrepo.Repo,
	baseFileMap *SafeFileMap,
	runID uuid.UUID,
	emitter EventEmitter,
	paths []string,
) {
	if repo == nil || emitter == nil || len(paths) == 0 {
		return
	}

	for _, path := range paths {
		if isIgnoredWorkspaceDiffPath(path) {
			continue
		}
		if !repo.FileExists(path) {
			emitter.Emit(EventFileDeleted, FileDeletedEvent{
				RunID: runID,
				Path:  path,
			})
			continue
		}

		content, err := repo.ReadFile(path)
		if err != nil {
			slog.Warn("agent: failed to read changed workspace file", "run_id", runID, "path", path, "error", err)
			continue
		}

		operation := "update"
		if baseFileMap == nil {
			operation = "create"
		} else if _, exists := baseFileMap.Get(path); !exists {
			operation = "create"
		}

		if len(content) > maxWriteBytes {
			emitter.Emit(EventToolProgress, ToolProgressEvent{
				RunID:      runID,
				ToolName:   "workspace_poll",
				Path:       path,
				Operation:  operation,
				Bytes:      len(content),
				Message:    "file too large for immediate sync, content omitted",
				BatchTotal: 1,
			})
			continue
		}

		emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:      runID,
			Path:       path,
			Operation:  operation,
			Bytes:      len(content),
			Content:    string(content),
			BatchTotal: 1,
		})
	}
}

func mapPythonErrorCode(raw string) ErrorCode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "cancelled", "canceled", "cancellation":
		return CodeCanceled
	case "rate_limited", "rate_limit", "rate-limited":
		return CodeRateLimited
	case "truncated", "token_limit", "output_length":
		return CodeTruncated
	case "repetition_loop", "repetition":
		return CodeRepetitionLoop
	case "tool_rounds_exceeded":
		return CodeToolRoundsExceeded
	case "validation_failed":
		return CodeValidationFailed
	case "budget_exceeded", "timeout", "deadline_exceeded":
		return CodeBudgetExceeded
	case "runtime_error", "runtime_fault", "python_crash":
		return CodeRuntimeError
	default:
		return CodeProviderError
	}
}

func toDeepAgentsAttachments(in []models.AppAsset) []deepagents.Attachment {
	out := make([]deepagents.Attachment, 0, len(in))
	for _, a := range in {
		out = append(out, deepagents.Attachment{
			AssetID:     a.ID.String(),
			FileName:    a.FileName,
			ContentType: a.ContentType,
			URL:         a.PublicURL,
			IsImage:     strings.HasPrefix(a.ContentType, "image/"),
		})
	}
	return out
}

// buildDeepAgentsToolSpecs converts a Registry's tool executors into the
// ToolSpec list Python consumes to construct one first-class tool per spec.
// One Python function per spec — no generic wemob_tool wrapper.
func buildDeepAgentsToolSpecs(reg *Registry) []deepagents.ToolSpec {
	if reg == nil {
		return nil
	}
	out := make([]deepagents.ToolSpec, 0, len(reg.order))
	for _, name := range reg.order {
		exec, ok := reg.tools[name]
		if !ok {
			// Server tools (e.g. web_search) are not exposed to DeepAgents;
			// the native agent loop uses them via the LLM provider.
			continue
		}
		out = append(out, deepagents.ToolSpec{
			Name:        exec.Name(),
			Description: exec.Description(),
			Schema:      exec.Schema(),
			RiskLevel:   ClassifyToolRisk(exec.Name()),
		})
	}
	return out
}

// deepagentsSubagentNames mirrors the static subagent set the Python
// runtime instantiates. Kept in sync with build_subagents() in
// deepagents-runtime/app/runtime.py. The list is small and stable; when
// the plan evolves, this is the single place to update.
var deepagentsSubagentNames = []string{
	"design_direction_agent",
	"frontend_builder_agent",
	"data_backend_agent",
	"qa_debug_agent",
}
