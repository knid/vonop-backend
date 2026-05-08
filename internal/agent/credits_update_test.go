package agent

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/utils"
)

// recordingEmitter captures every Emit call into an ordered slice so tests
// can make assertions about event ordering. It satisfies EventEmitter the
// same way noopEmitter (in tools_base_path_scope_tools_test.go) does, but
// with a buffer for inspection.
type recordingEmitter struct {
	events []Event
}

func (r *recordingEmitter) Emit(eventType string, data any) bool {
	r.events = append(r.events, Event{Type: eventType, Data: data})
	return true
}

func (r *recordingEmitter) Buffer() []Event { return r.events }
func (r *recordingEmitter) Close()           {}

// TestCreditsUpdateEventEmittedAfterTurnEnd asserts that for every
// TurnEndEvent emitted by the agent loop, a CreditsUpdateEvent is emitted
// immediately after, and that CreditsUsed is monotonically non-decreasing
// across consecutive credits_update events.
//
// Regression check against the gap that motivated the feature: before this
// work, credits were only published at EventDone, so the frontend could not
// show a live meter mid-run.
//
// Test style: the package's existing "drive Run()" tests would require
// multi-hour scaffolding (a real LLM stub, git repo, project, user, etc.),
// so this test follows the noopEmitter pattern from
// tools_base_path_scope_tools_test.go and reconstructs the relevant slice
// of Run() — the emitCreditsUpdate closure plus the two TurnEnd emit
// sites — directly. The closure body is copied verbatim from run.go so a
// behavioural drift here would be caught by code review against the source.
// A complementary source-level check (TestCreditsUpdateEmitFollowsEveryTurnEnd_SourceContract)
// asserts that every emitter.Emit(EventTurnEnd, ...) call in run.go is
// followed immediately by an emitCreditsUpdate(turn) call.
func TestCreditsUpdateEventEmittedAfterTurnEnd(t *testing.T) {
	// --- arrange ---
	emitter := &recordingEmitter{}
	tracker := utils.NewTokenTracker()
	runID := uuid.New()
	// deps may be nil — creditsBilled handles that and falls back to the
	// per-key remote-config defaults (5000 tokens/credit etc.).
	var deps *RunDependencies

	// emitCreditsUpdate is the exact closure body from run.go. Keeping
	// these in lockstep is the test's load-bearing assumption.
	emitCreditsUpdate := func(turn int) {
		credits := creditsBilled(deps, tracker)
		_, _, total, _ := tracker.Totals()
		emitter.Emit(EventCreditsUpdate, CreditsUpdateEvent{
			RunID:       runID,
			Turn:        turn,
			CreditsUsed: credits,
			Tokens:      total,
		})
	}

	// Simulate two turns. Each turn records non-zero token usage so the
	// monotonic-non-decreasing invariant is observable (turn 2 must bill
	// at least as many credits as turn 1).
	tracker.Record("claude-sonnet-4-6", utils.UsageInfo{
		PromptTokens:     8000,
		CompletionTokens: 1500,
		TotalTokens:      9500,
	}, 0, true)

	turn := 1
	emitter.Emit(EventTurnEnd, TurnEndEvent{
		RunID:      runID,
		Turn:       turn,
		StopReason: "tool_use",
		Tokens:     trackerDelta(tracker),
	})
	emitCreditsUpdate(turn)
	turn++

	tracker.Record("claude-sonnet-4-6", utils.UsageInfo{
		PromptTokens:     6000,
		CompletionTokens: 2000,
		TotalTokens:      8000,
	}, 0, true)

	emitter.Emit(EventTurnEnd, TurnEndEvent{
		RunID:      runID,
		Turn:       turn,
		StopReason: "end_turn",
		Tokens:     trackerDelta(tracker),
	})
	emitCreditsUpdate(turn)

	// --- assert ---
	events := emitter.Buffer()
	var lastCredits int
	sawAfterTurnEnd := 0
	for i, ev := range events {
		if ev.Type != EventTurnEnd {
			continue
		}
		if i+1 >= len(events) || events[i+1].Type != EventCreditsUpdate {
			t.Fatalf("expected EventCreditsUpdate immediately after EventTurnEnd at index %d (got next=%q)",
				i, nextType(events, i))
		}
		cu, ok := events[i+1].Data.(CreditsUpdateEvent)
		if !ok {
			t.Fatalf("event after EventTurnEnd at index %d was not a CreditsUpdateEvent payload (got %T)",
				i, events[i+1].Data)
		}
		if cu.RunID != runID {
			t.Errorf("CreditsUpdateEvent.RunID = %s, want %s", cu.RunID, runID)
		}
		if cu.Turn == 0 {
			t.Errorf("CreditsUpdateEvent.Turn was zero — should match the prior TurnEnd's turn")
		}
		if cu.CreditsUsed < lastCredits {
			t.Fatalf("CreditsUsed regressed: %d -> %d at index %d", lastCredits, cu.CreditsUsed, i+1)
		}
		lastCredits = cu.CreditsUsed
		sawAfterTurnEnd++
	}
	if sawAfterTurnEnd < 2 {
		t.Fatalf("expected >= 2 credits_update events across 2 turns, got %d (events=%+v)",
			sawAfterTurnEnd, events)
	}
	// Sanity: there must have been credit accrual between turn 1 and turn 2.
	if lastCredits == 0 {
		t.Errorf("final CreditsUsed = 0; tracker.Record entries should bill at least 1 credit")
	}
}

// nextType is a small helper for error messages — returns the type of the
// event after index i, or "<end-of-stream>" if i is the last event.
func nextType(events []Event, i int) string {
	if i+1 >= len(events) {
		return "<end-of-stream>"
	}
	return events[i+1].Type
}

// TestCreditsUpdateEmitFollowsEveryTurnEnd_SourceContract is a static
// check against run.go's source: every `emitter.Emit(EventTurnEnd, ...)`
// must be followed (after an optional balanced block) by a call to
// `emitCreditsUpdate(turn)`. The behavioural test above can only observe
// the contract via a hand-rolled simulator; this one catches the case
// where someone deletes the emitCreditsUpdate(turn) call from a real
// emit site and forgets to update the simulator.
func TestCreditsUpdateEmitFollowsEveryTurnEnd_SourceContract(t *testing.T) {
	src, err := os.ReadFile("run.go")
	if err != nil {
		t.Fatalf("read run.go: %v", err)
	}
	// Find every call site. The two known sites (lines ~389 and ~523 at
	// time of writing) both span multiple lines because TurnEndEvent is a
	// struct literal, so we match through the closing `})` and check the
	// next non-blank line.
	re := regexp.MustCompile(`(?ms)emitter\.Emit\(EventTurnEnd,\s*TurnEndEvent\{.*?\}\)`)
	matches := re.FindAllIndex(src, -1)
	if len(matches) < 2 {
		t.Fatalf("expected >= 2 emitter.Emit(EventTurnEnd, ...) call sites in run.go, found %d",
			len(matches))
	}
	for _, m := range matches {
		end := m[1]
		// Look at the text immediately following the closing `})`.
		tail := string(src[end:])
		// Skip whitespace and newlines until we hit a non-blank line.
		var nextLine string
		for _, line := range strings.Split(tail, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			nextLine = trimmed
			break
		}
		if !strings.HasPrefix(nextLine, "emitCreditsUpdate(") {
			t.Errorf("emitter.Emit(EventTurnEnd, ...) at byte offset %d is not followed by emitCreditsUpdate(...); next non-blank line was: %q",
				m[0], nextLine)
		}
	}
}
