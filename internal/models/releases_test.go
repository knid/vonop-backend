package models

import "testing"

// TestIsTerminalReleaseStatus codifies which states block forward
// transitions in MarkReleaseStatus. If you flip one of these to non-terminal,
// re-check that the silent no-op gate in database/releases.go still does
// what you want.
func TestIsTerminalReleaseStatus(t *testing.T) {
	terminal := []string{
		ReleaseStatusLive,
		ReleaseStatusRejected,
		ReleaseStatusManuallyRejected,
		ReleaseStatusInvalid,
		ReleaseStatusRolledBack,
	}
	nonTerminal := []string{
		ReleaseStatusDraft,
		ReleaseStatusUploaded,
		ReleaseStatusInReview,
		ReleaseStatusApproved,
		ReleaseStatusPaused,
	}
	for _, s := range terminal {
		if !IsTerminalReleaseStatus(s) {
			t.Errorf("expected %q terminal", s)
		}
	}
	for _, s := range nonTerminal {
		if IsTerminalReleaseStatus(s) {
			t.Errorf("expected %q non-terminal", s)
		}
	}
}
