//go:build integration

package controllers

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/models"
)

// TestCancelBuildTask_CallsCodemagicCancelWhenIDPresent pins the core
// behavior: a cancellation flows through to Codemagic via POST
// /builds/{id}/cancel, and the local row still ends up Cancelled.
//
// This is the bug-fix scenario — before this change, CancelBuildTask only
// updated the local row and Codemagic kept burning ~25 min of build minutes.
func TestCancelBuildTask_CallsCodemagicCancelWhenIDPresent(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Pin the trigger response so we can assert the cancel call uses
	// exactly the same id we persisted on the row.
	f.cmRec.mu.Lock()
	f.cmRec.triggerBuildID = "cm-build-pin-cancel-test"
	f.cmRec.mu.Unlock()

	build, tasks, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms: []string{"android"},
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	taskID := tasks[0].ID

	// Sanity: the row should now carry the codemagic_build_id we pinned.
	stored, err := f.c.DB.GetAppBuildTaskById(taskID.String())
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if stored.CodemagicBuildID != "cm-build-pin-cancel-test" {
		t.Fatalf("CodemagicBuildID = %q, want cm-build-pin-cancel-test", stored.CodemagicBuildID)
	}

	// Cancel.
	if err := f.c.CancelBuildTask(f.user, taskID.String()); err != nil {
		t.Fatalf("CancelBuildTask: %v", err)
	}

	// Codemagic mock should have received exactly one cancel for our id.
	cancels := f.cmRec.cancelSnapshot()
	if len(cancels) != 1 {
		t.Fatalf("expected 1 codemagic cancel, got %d (%v)", len(cancels), cancels)
	}
	if cancels[0] != "cm-build-pin-cancel-test" {
		t.Errorf("cancelled build_id = %q, want cm-build-pin-cancel-test", cancels[0])
	}

	// Local row must end up Cancelled + Finished.
	after, err := f.c.DB.GetAppBuildTaskById(taskID.String())
	if err != nil {
		t.Fatalf("reload task after cancel: %v", err)
	}
	if !after.Finished {
		t.Errorf("task.Finished = false, want true")
	}
	if after.StatusText != "Cancelled" {
		t.Errorf("task.StatusText = %q, want Cancelled", after.StatusText)
	}
	_ = build
}

// TestCancelBuildTask_NoCallWhenCodemagicBuildIDMissing covers legacy rows
// (built before this change shipped) and rows whose trigger response we
// never persisted: cancel should not hit Codemagic at all, but the local
// row still gets cancelled.
func TestCancelBuildTask_NoCallWhenCodemagicBuildIDMissing(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	// Insert a Build + AppBuildTask manually, with no CodemagicBuildID.
	// Using the DB layer directly avoids the trigger path so the cm
	// recorder stays empty before the cancel.
	build := &models.Build{
		AppProjectID: f.project.ID,
		Title:        "legacy",
		BuildNumber:  1,
	}
	insertedBuild, err := f.db.CreateProjectBuild(build)
	if err != nil {
		t.Fatalf("create build: %v", err)
	}
	task, err := f.db.CreateAppBuildTask(*insertedBuild, f.project.ID, "Android", "Queued", false)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Pre-cancel sanity.
	if task.CodemagicBuildID != "" {
		t.Fatalf("expected empty CodemagicBuildID, got %q", task.CodemagicBuildID)
	}

	if err := f.c.CancelBuildTask(f.user, task.ID.String()); err != nil {
		t.Fatalf("CancelBuildTask: %v", err)
	}

	// Codemagic mock should NOT have been hit.
	if cancels := f.cmRec.cancelSnapshot(); len(cancels) != 0 {
		t.Errorf("expected 0 codemagic cancels, got %d (%v)", len(cancels), cancels)
	}

	// Local row must still be Cancelled.
	after, err := f.c.DB.GetAppBuildTaskById(task.ID.String())
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if !after.Finished || after.StatusText != "Cancelled" {
		t.Errorf("task = {Finished:%v StatusText:%q}, want {true Cancelled}", after.Finished, after.StatusText)
	}
	_ = ctx
	_ = uuid.Nil
}

// TestCancelBuildTask_LocalCancelStillHappensOnCodemagicFailure pins the
// best-effort contract: a Codemagic 5xx must not block the local cancel.
// The user's intent (stop showing me a building row) is honored even if
// Codemagic is down.
func TestCancelBuildTask_LocalCancelStillHappensOnCodemagicFailure(t *testing.T) {
	f := setupExpoBuildTest(t)
	ctx := context.Background()

	f.cmRec.mu.Lock()
	f.cmRec.triggerBuildID = "cm-build-cancel-fail-test"
	f.cmRec.cancelStatus = 500 // CancelBuild treats >= 500 as error
	f.cmRec.mu.Unlock()

	_, tasks, err := f.c.BuildExpoMobile(ctx, f.project, f.user, ExpoBuildOptions{
		Platforms: []string{"android"},
	})
	if err != nil {
		t.Fatalf("BuildExpoMobile: %v", err)
	}
	taskID := tasks[0].ID

	// Cancel — Codemagic returns 500, we expect this NOT to surface as an
	// error to the caller; the local row should still flip to Cancelled.
	if err := f.c.CancelBuildTask(f.user, taskID.String()); err != nil {
		t.Fatalf("CancelBuildTask returned error despite best-effort contract: %v", err)
	}

	if cancels := f.cmRec.cancelSnapshot(); len(cancels) != 1 {
		t.Errorf("expected 1 codemagic cancel attempt, got %d", len(cancels))
	}

	after, err := f.c.DB.GetAppBuildTaskById(taskID.String())
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if !after.Finished || after.StatusText != "Cancelled" {
		t.Errorf("task = {Finished:%v StatusText:%q}, want {true Cancelled}", after.Finished, after.StatusText)
	}
}
