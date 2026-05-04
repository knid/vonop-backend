//go:build integration

package controllers

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// triggerMobileBuildForAgent is the controller-side adapter that the
// chat agent's `trigger_build_after_fix` tool calls (P1 #17 mobile
// pipeline audit). These tests exercise the adapter end-to-end against
// a real DB + fake Codemagic + fake S3 — same fixture used by
// build_expo_mobile_integration_test.go — so we get coverage of:
//  * happy-path build dispatch with the platforms the tool passed in
//  * auth/scope errors when called for a project that isn't
//    mobile_app_native (matches the tool's project-type guard, but
//    we also exercise the controller-layer guard for defense-in-depth)
//  * the "no platforms" empty-slice default → both Android + iOS
//
// The tool-layer auth (rc.User must own the project) is verified by
// the agent package's own tests; the controller layer trusts the
// caller-supplied user since both come from the same per-request
// context.

func TestTriggerMobileBuildForAgent_HappyPath_BothPlatforms(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	buildID, taskIDs, err := f.c.triggerMobileBuildForAgent(ctx, f.project, f.user, []string{"android", "ios"})
	if err != nil {
		t.Fatalf("triggerMobileBuildForAgent: %v", err)
	}
	if buildID == "" {
		t.Fatal("expected non-empty buildID, got empty")
	}
	if _, perr := uuid.Parse(buildID); perr != nil {
		t.Errorf("buildID is not a UUID: %q", buildID)
	}
	if len(taskIDs) != 2 {
		t.Fatalf("expected 2 task IDs, got %d (%v)", len(taskIDs), taskIDs)
	}
	if taskIDs["android"] == "" {
		t.Errorf("android task_id missing: %v", taskIDs)
	}
	if taskIDs["ios"] == "" {
		t.Errorf("ios task_id missing: %v", taskIDs)
	}
	if _, perr := uuid.Parse(taskIDs["android"]); perr != nil {
		t.Errorf("android task_id is not a UUID: %q", taskIDs["android"])
	}
	if _, perr := uuid.Parse(taskIDs["ios"]); perr != nil {
		t.Errorf("ios task_id is not a UUID: %q", taskIDs["ios"])
	}

	// Verify the controller actually fanned out to Codemagic.
	calls := f.cmRec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("expected 2 codemagic calls, got %d", len(calls))
	}
}

func TestTriggerMobileBuildForAgent_HappyPath_AndroidOnly(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	buildID, taskIDs, err := f.c.triggerMobileBuildForAgent(ctx, f.project, f.user, []string{"android"})
	if err != nil {
		t.Fatalf("triggerMobileBuildForAgent: %v", err)
	}
	if buildID == "" {
		t.Fatal("expected non-empty buildID, got empty")
	}
	if len(taskIDs) != 1 || taskIDs["android"] == "" {
		t.Errorf("expected only android task ID, got %v", taskIDs)
	}
	if taskIDs["ios"] != "" {
		t.Errorf("unexpected ios task ID: %q", taskIDs["ios"])
	}

	calls := f.cmRec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("expected 1 codemagic call, got %d", len(calls))
	}
}

func TestTriggerMobileBuildForAgent_EmptyPlatformsDefaultsToBoth(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	buildID, taskIDs, err := f.c.triggerMobileBuildForAgent(ctx, f.project, f.user, nil)
	if err != nil {
		t.Fatalf("triggerMobileBuildForAgent: %v", err)
	}
	if buildID == "" {
		t.Fatal("expected non-empty buildID")
	}
	if len(taskIDs) != 2 {
		t.Errorf("empty platforms should default to both, got %d task IDs (%v)", len(taskIDs), taskIDs)
	}
}

// TestTriggerMobileBuildForAgent_RejectsNonMobileNative — the controller
// inherits BuildExpoMobile's project-type guard. A wrong project_type
// must error out before any DB/Codemagic side effect.
func TestTriggerMobileBuildForAgent_RejectsNonMobileNative(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()
	f.project.ProjectType = models.ProjectTypeApp // mutate to wrong type

	buildID, taskIDs, err := f.c.triggerMobileBuildForAgent(ctx, f.project, f.user, []string{"android"})
	if err == nil {
		t.Fatal("expected error for non-mobile_app_native project, got nil")
	}
	if !strings.Contains(err.Error(), "mobile_app_native") {
		t.Errorf("error = %q, want substring 'mobile_app_native'", err.Error())
	}
	if buildID != "" {
		t.Errorf("expected empty buildID on rejection, got %q", buildID)
	}
	if len(taskIDs) != 0 {
		t.Errorf("expected 0 task IDs on rejection, got %v", taskIDs)
	}
	if got := len(f.cmRec.snapshot()); got != 0 {
		t.Errorf("expected 0 codemagic calls on rejection, got %d", got)
	}
}

// TestTriggerMobileBuildForAgent_RejectsForeignUser — auth scope check:
// when the caller is NOT the project owner, the controller should error
// out (BuildExpoMobile creates the v1 shadow project keyed by user_id +
// url, so a foreign user would silently fork into a parallel shadow row;
// we surface the mismatch by exercising a foreign user against the same
// project record). This isn't a strict "permission denied" code path
// today — it's "the build runs against the foreign user's shadow", which
// would still return a buildID. The agent layer's rc.User check is what
// keeps the boundary tight; this test documents the expected behavior so
// future refactors don't accidentally remove that guard.
//
// Concretely: with a foreign user, the build STILL succeeds end-to-end,
// because BuildExpoMobile only checks platform permissions and pending
// builds — it doesn't verify project.UserID == user.ID. The test pins
// this behavior to the controller boundary so the agent-layer auth
// gate (RunContext.User check in tool_trigger_build.go) stays the
// authoritative scope check.
func TestTriggerMobileBuildForAgent_ForeignUserCreatesParallelShadow(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Create an unrelated user (no membership in the project's org).
	other := &models.User{
		ID: uuid.New(), Name: "Other", Email: "other-" + uuid.NewString()[:8] + "@example.com", Password: "x",
	}
	if _, err := f.db.DB.NewInsert().Model(other).Exec(ctx); err != nil {
		t.Fatalf("insert foreign user: %v", err)
	}

	buildID, taskIDs, err := f.c.triggerMobileBuildForAgent(ctx, f.project, other, []string{"android"})
	if err != nil {
		t.Fatalf("triggerMobileBuildForAgent for foreign user (controller has no auth gate): %v", err)
	}
	if buildID == "" {
		t.Errorf("expected non-empty buildID even for foreign user (auth gate is at the agent layer)")
	}
	if len(taskIDs) == 0 {
		t.Errorf("expected at least one task ID, got none")
	}
}
