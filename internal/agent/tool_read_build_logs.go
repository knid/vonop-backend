package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// ReadBuildLogsTool exposes the latest failed mobile-build's status
// timeline + error context to the chat agent so it can diagnose Codemagic
// build failures from inside the conversation. Used by mobile_app_native
// projects to close the "fix-with-AI" loop kicked off from the BuildRow
// "Fix with AI" button (P1 #17 mobile pipeline audit).
//
// The tool is auth-scoped to the project owner: it walks v1 shadow row
// owned by rc.User, then v1-project-owned AppBuildTasks, so a forged
// task_id cannot leak another user's logs.
type ReadBuildLogsTool struct {
	Deps *RunDependencies
}

var _ ToolExecutor = (*ReadBuildLogsTool)(nil)

func (t *ReadBuildLogsTool) Name() string { return "read_build_logs" }

func (t *ReadBuildLogsTool) Description() string {
	return "Read context for the most recent FAILED mobile build of this project (Codemagic build report): the platform, status, full error message, the matched failure-annotation (human-readable diagnosis + suggested fix), and the stage timeline of build events (queued/preparing/building/uploading/built/error). Pass the optional task_id to target a specific build. Use this BEFORE proposing a code fix in response to a 'build failed' chat message — the human-readable annotation usually pinpoints exactly what to change."
}

func (t *ReadBuildLogsTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "Optional UUID of the AppBuildTask to inspect. When omitted, returns the most recent failed build for this project (across both platforms).",
			},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

// readBuildLogsOutput is the JSON shape returned to the agent.
type readBuildLogsOutput struct {
	Found    bool             `json:"found"`
	Task     *buildTaskInfo   `json:"task,omitempty"`
	Events   []buildEventInfo `json:"events,omitempty"`
	Note     string           `json:"note,omitempty"`
}

type buildTaskInfo struct {
	TaskID                  string `json:"task_id"`
	BuildID                 string `json:"build_id"`
	Platform                string `json:"platform"` // "Android" | "IOS"
	Status                  string `json:"status"`
	Finished                bool   `json:"finished"`
	ErrorMessage            string `json:"error_message,omitempty"`
	AnnotationName          string `json:"annotation_name,omitempty"`
	AnnotationHumanMessage  string `json:"annotation_human_message,omitempty"`
	AnnotationSuggestedFix  string `json:"annotation_suggested_fix,omitempty"`
	CreatedAt               string `json:"created_at,omitempty"`
	UpdatedAt               string `json:"updated_at,omitempty"`
}

type buildEventInfo struct {
	EventType  string `json:"event_type"`
	Message    string `json:"message,omitempty"`
	Phase      string `json:"phase,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

func (t *ReadBuildLogsTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Deps == nil || rc.Deps.DB == nil {
		return "", false, fmt.Errorf("read_build_logs: no database available")
	}
	if rc.Project == nil {
		return "read_build_logs: no project in context", true, nil
	}
	if rc.User == nil {
		return "read_build_logs: no authenticated user in context", true, nil
	}
	if rc.Project.ProjectType != models.ProjectTypeMobileAppNative {
		return "read_build_logs is only available for mobile_app_native projects", true, nil
	}

	var args struct {
		TaskID string `json:"task_id"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}

	// Resolve the v1 shadow project for this AppProject. AppBuildTask FKs
	// reference Project (v1), not AppProject — same shadow row that
	// BuildExpoMobile creates with URL="expo://"+slug.
	shadowURL := "expo://" + rc.Project.Slug
	v1Project, err := rc.Deps.DB.GetProjectByUserAndURL(rc.User.ID.String(), shadowURL)
	if err != nil || v1Project == nil || v1Project.ID == uuid.Nil {
		// No shadow row → no builds have ever run for this project.
		out := readBuildLogsOutput{Found: false, Note: "No mobile builds have been triggered for this project yet."}
		b, _ := json.Marshal(out)
		return string(b), false, nil
	}

	// Pull all tasks for the v1 project (user-ownership join keeps this
	// auth-safe — a forged task_id can't read someone else's build).
	tasks, err := rc.Deps.DB.GetAppBuildTasksByUserAndProject(rc.User, v1Project.ID.String())
	if err != nil {
		return fmt.Sprintf("read_build_logs: failed to load tasks: %v", err), true, nil
	}
	if len(tasks) == 0 {
		out := readBuildLogsOutput{Found: false, Note: "No mobile builds have been triggered for this project yet."}
		b, _ := json.Marshal(out)
		return string(b), false, nil
	}

	// Pick the target task: explicit task_id when supplied, else latest
	// failed task by created_at desc.
	var picked *models.AppBuildTask
	if args.TaskID != "" {
		wantID, perr := uuid.Parse(args.TaskID)
		if perr != nil {
			return fmt.Sprintf("invalid task_id: %v", perr), true, nil
		}
		for i := range tasks {
			if tasks[i].ID == wantID {
				picked = &tasks[i]
				break
			}
		}
		if picked == nil {
			return fmt.Sprintf("task_id %s not found among this project's builds (or not owned by the current user)", args.TaskID), true, nil
		}
	} else {
		// Sort tasks by CreatedAt desc, pick the first failed one (or the
		// most recent task if none failed — the agent might want context
		// even on a successful run).
		sort.SliceStable(tasks, func(i, j int) bool {
			return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
		})
		for i := range tasks {
			if isTaskFailed(&tasks[i]) {
				picked = &tasks[i]
				break
			}
		}
		if picked == nil {
			out := readBuildLogsOutput{
				Found: false,
				Note:  "No FAILED builds found. The most recent build may still be in progress or may have succeeded.",
			}
			b, _ := json.Marshal(out)
			return string(b), false, nil
		}
	}

	// Re-load the task with relations so the BuildAnnotation join is populated.
	relTask, rerr := rc.Deps.DB.GetAppBuildTaskWithRelations(picked.ID.String())
	if rerr == nil && relTask != nil {
		picked = relTask
	}

	taskInfo := &buildTaskInfo{
		TaskID:       picked.ID.String(),
		BuildID:      picked.BuildID.String(),
		Platform:     picked.Platform,
		Status:       picked.StatusText,
		Finished:     picked.Finished,
		ErrorMessage: picked.ErrorMessage,
		CreatedAt:    picked.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    picked.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if picked.Annotation != nil {
		taskInfo.AnnotationName = picked.Annotation.Name
		taskInfo.AnnotationHumanMessage = picked.Annotation.HumanMessage
		taskInfo.AnnotationSuggestedFix = picked.Annotation.SuggestedFix
	}

	// Truncate ErrorMessage to a sane size — full Codemagic logs can be
	// 100KB+, but the agent only needs the relevant tail. The annotation
	// already pinpoints the failure pattern; a 4KB tail is plenty for any
	// follow-up reasoning.
	const maxErrChars = 4000
	if len(taskInfo.ErrorMessage) > maxErrChars {
		taskInfo.ErrorMessage = "…(truncated)…\n" + taskInfo.ErrorMessage[len(taskInfo.ErrorMessage)-maxErrChars:]
	}

	// Pull the build event timeline. Best-effort — if it errors, we still
	// return the task info with an empty timeline.
	events, eerr := rc.Deps.DB.ListBuildEventsByTask(ctx, picked.ID)
	infos := make([]buildEventInfo, 0, len(events))
	if eerr == nil {
		for _, ev := range events {
			info := buildEventInfo{
				EventType:  ev.EventType,
				OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339),
			}
			// Decode payload to surface message + phase. Payloads are the
			// raw Codemagic webhook body (small JSON objects), so this is
			// safe to deserialize.
			if len(ev.Payload) > 0 {
				var pl struct {
					Message string `json:"message"`
					Phase   string `json:"phase"`
				}
				if perr := json.Unmarshal(ev.Payload, &pl); perr == nil {
					if len(pl.Message) > 800 {
						pl.Message = pl.Message[:800] + "…"
					}
					info.Message = pl.Message
					info.Phase = pl.Phase
				}
			}
			infos = append(infos, info)
		}
	}

	out := readBuildLogsOutput{
		Found:  true,
		Task:   taskInfo,
		Events: infos,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", false, fmt.Errorf("marshal output: %w", err)
	}
	return string(b), false, nil
}

// isTaskFailed reports whether an AppBuildTask is in a terminal failed
// state. Codemagic + our cron mark failed tasks with status_text in
// {Failed, error, failed} and Finished=true. The cron-fail path is the
// "webhook timeout" branch (status="error", finished=true) — also a
// failure for our purposes.
func isTaskFailed(task *models.AppBuildTask) bool {
	if task == nil {
		return false
	}
	if !task.Finished {
		return false
	}
	switch task.StatusText {
	case "Failed", "failed", "Error", "error", "Cancelled":
		return true
	}
	return false
}
