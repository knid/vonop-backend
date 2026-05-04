package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// makeFakeBuildTrigger returns a stubbed MobileBuildTrigger plus a pointer
// to the captured platforms slice so tests can assert on what the tool
// asked the controller to build.
func makeFakeBuildTrigger(retBuildID string, retTaskIDs map[string]string, retErr error) (MobileBuildTrigger, *[]string) {
	captured := []string{}
	fn := func(_ context.Context, _ *models.AppProject, _ *models.User, platforms []string) (string, map[string]string, error) {
		captured = append([]string(nil), platforms...) // reset to last call
		return retBuildID, retTaskIDs, retErr
	}
	return fn, &captured
}

func newTriggerBuildTestContext(projectType string, trigger MobileBuildTrigger, withUser bool) *RunContext {
	user := (*models.User)(nil)
	if withUser {
		user = &models.User{ID: uuid.New()}
	}
	return &RunContext{
		RunID: uuid.New(),
		Project: &models.AppProject{
			ID:          uuid.New(),
			Slug:        "trigger-build-test",
			ProjectType: projectType,
		},
		User:    user,
		FileMap: NewSafeFileMap(map[string]string{}),
		Emitter: noopEmitter{},
		Deps: &RunDependencies{
			TriggerMobileBuild: trigger,
		},
	}
}

// TestTriggerBuildAfterFixTool_HappyPath_DefaultPlatform verifies the
// tool dispatches to the trigger callback with the "both" default and
// returns the Codemagic build_id + per-platform task IDs in the JSON.
func TestTriggerBuildAfterFixTool_HappyPath_DefaultPlatform(t *testing.T) {
	wantBuildID := uuid.NewString()
	wantTasks := map[string]string{
		"android": uuid.NewString(),
		"ios":     uuid.NewString(),
	}
	trigger, captured := makeFakeBuildTrigger(wantBuildID, wantTasks, nil)
	rc := newTriggerBuildTestContext(models.ProjectTypeMobileAppNative, trigger, true)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute infra error: %v", err)
	}
	if isErr {
		t.Fatalf("Execute returned tool error: %s", out)
	}

	if got := *captured; len(got) != 2 || got[0] != "android" || got[1] != "ios" {
		t.Errorf("default platforms = %v, want [android ios]", got)
	}

	var got triggerBuildOutput
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("unmarshal output: %v; out=%s", uerr, out)
	}
	if got.BuildID != wantBuildID {
		t.Errorf("BuildID = %q, want %q", got.BuildID, wantBuildID)
	}
	if got.AndroidTaskID != wantTasks["android"] {
		t.Errorf("AndroidTaskID = %q, want %q", got.AndroidTaskID, wantTasks["android"])
	}
	if got.IOSTaskID != wantTasks["ios"] {
		t.Errorf("IOSTaskID = %q, want %q", got.IOSTaskID, wantTasks["ios"])
	}
	if got.StatusURL == "" {
		t.Error("StatusURL is empty; expected a /v2/apps/{id}/builds URL")
	}
	if !strings.Contains(got.StatusURL, rc.Project.ID.String()) {
		t.Errorf("StatusURL %q should contain project ID %q", got.StatusURL, rc.Project.ID.String())
	}
}

// TestTriggerBuildAfterFixTool_PlatformIOSOnly verifies the platform arg
// is forwarded verbatim to the controller callback.
func TestTriggerBuildAfterFixTool_PlatformIOSOnly(t *testing.T) {
	trigger, captured := makeFakeBuildTrigger(uuid.NewString(), map[string]string{"ios": uuid.NewString()}, nil)
	rc := newTriggerBuildTestContext(models.ProjectTypeMobileAppNative, trigger, true)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{"platform":"ios"}`))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if isErr {
		t.Fatalf("tool error: %s", out)
	}
	if got := *captured; len(got) != 1 || got[0] != "ios" {
		t.Errorf("platforms = %v, want [ios]", got)
	}
	var got triggerBuildOutput
	_ = json.Unmarshal([]byte(out), &got)
	if got.AndroidTaskID != "" {
		t.Errorf("AndroidTaskID should be empty for ios-only build, got %q", got.AndroidTaskID)
	}
	if got.IOSTaskID == "" {
		t.Error("IOSTaskID should be populated")
	}
}

// TestTriggerBuildAfterFixTool_RejectsWrongProjectType — defense in
// depth, mirroring the registry-level gate.
func TestTriggerBuildAfterFixTool_RejectsWrongProjectType(t *testing.T) {
	trigger, captured := makeFakeBuildTrigger(uuid.NewString(), nil, nil)
	rc := newTriggerBuildTestContext(models.ProjectTypeApp, trigger, true)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if !isErr {
		t.Errorf("expected isErr=true for non-mobile_app_native, got false; out=%s", out)
	}
	if !strings.Contains(out, "mobile_app_native") {
		t.Errorf("error message should mention mobile_app_native, got %q", out)
	}
	if len(*captured) > 0 {
		t.Errorf("trigger callback should NOT have been invoked; got platforms %v", *captured)
	}
}

// TestTriggerBuildAfterFixTool_RejectsMissingUser — the agent's auth
// boundary depends on rc.User being set; an unauth'd run must short-
// circuit instead of dispatching a build.
func TestTriggerBuildAfterFixTool_RejectsMissingUser(t *testing.T) {
	trigger, captured := makeFakeBuildTrigger(uuid.NewString(), nil, nil)
	rc := newTriggerBuildTestContext(models.ProjectTypeMobileAppNative, trigger, false)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if !isErr {
		t.Errorf("expected isErr=true for missing user, got false; out=%s", out)
	}
	if !strings.Contains(out, "authenticated user") {
		t.Errorf("error should mention authentication, got %q", out)
	}
	if len(*captured) > 0 {
		t.Errorf("trigger callback should NOT have been invoked; got platforms %v", *captured)
	}
}

// TestTriggerBuildAfterFixTool_SurfacesControllerError — when the
// controller-layer trigger returns an error (limits, pending build,
// etc.), the tool surfaces it as a tool-usage error so the model can
// reason about it instead of crashing the run.
func TestTriggerBuildAfterFixTool_SurfacesControllerError(t *testing.T) {
	trigger, _ := makeFakeBuildTrigger("", nil, errors.New("a build is already in progress, please wait"))
	rc := newTriggerBuildTestContext(models.ProjectTypeMobileAppNative, trigger, true)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected nil infra error, got %v", err)
	}
	if !isErr {
		t.Errorf("expected isErr=true on controller failure, got false; out=%s", out)
	}
	if !strings.Contains(out, "already in progress") {
		t.Errorf("error message should propagate controller text, got %q", out)
	}
}

// TestTriggerBuildAfterFixTool_NoTriggerCallback — defense in depth
// when the controller layer didn't bind TriggerMobileBuild (e.g. dev
// environments where the build path is disabled).
func TestTriggerBuildAfterFixTool_NoTriggerCallback(t *testing.T) {
	rc := newTriggerBuildTestContext(models.ProjectTypeMobileAppNative, nil, true)
	tool := &TriggerBuildAfterFixTool{Deps: rc.Deps}

	out, isErr, err := tool.Execute(context.Background(), rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if !isErr {
		t.Errorf("expected isErr=true when callback unbound, got false; out=%s", out)
	}
	if !strings.Contains(out, "not wired") {
		t.Errorf("error message should mention 'not wired', got %q", out)
	}
}

// TestNormalizeBuildPlatforms — direct coverage of the schema-arg → []string
// adapter so future schema-arg additions don't break the default branch.
func TestNormalizeBuildPlatforms(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"android", "ios"}},
		{"both", []string{"android", "ios"}},
		{"BOTH", []string{"android", "ios"}},
		{"  ios  ", []string{"ios"}},
		{"ANDROID", []string{"android"}},
		{"unknown", []string{"android", "ios"}}, // safe fallback to both
	}
	for _, c := range cases {
		got := normalizeBuildPlatforms(c.in)
		if len(got) != len(c.want) {
			t.Errorf("normalizeBuildPlatforms(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("normalizeBuildPlatforms(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
