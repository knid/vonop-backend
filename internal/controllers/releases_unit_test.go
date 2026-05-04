package controllers

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

// TestMapAppleStateToReleaseStatus locks down the wire mapping between
// Apple's appStoreState strings and our internal Release.Status. Adding a
// new state should explicitly add a case here so the polling worker doesn't
// silently flap a release into the wrong terminal state.
func TestMapAppleStateToReleaseStatus(t *testing.T) {
	cases := []struct {
		state    string
		notif    string
		reason   string
		want     string
		wantMsg  string
	}{
		{"WAITING_FOR_REVIEW", "", "", models.ReleaseStatusUploaded, "Apple: WAITING_FOR_REVIEW"},
		{"PENDING_REVIEW", "", "", models.ReleaseStatusUploaded, "Apple: PENDING_REVIEW"},
		{"IN_REVIEW", "", "", models.ReleaseStatusInReview, "Apple: IN_REVIEW"},
		{"PENDING_DEVELOPER_RELEASE", "", "", models.ReleaseStatusApproved, "Apple: PENDING_DEVELOPER_RELEASE"},
		{"PROCESSING_FOR_APP_STORE", "", "", models.ReleaseStatusApproved, "Apple: PROCESSING_FOR_APP_STORE"},
		{"READY_FOR_SALE", "", "", models.ReleaseStatusLive, "Apple: READY_FOR_SALE"},
		{"REJECTED", "", "icons", models.ReleaseStatusRejected, "Apple: REJECTED — icons"},
		{"METADATA_REJECTED", "", "", models.ReleaseStatusRejected, "Apple: METADATA_REJECTED"},
		{"DEVELOPER_REJECTED", "", "", models.ReleaseStatusManuallyRejected, "Apple: DEVELOPER_REJECTED"},
		{"INVALID_BINARY", "", "", models.ReleaseStatusInvalid, "Apple: INVALID_BINARY"},
		{"PREPARE_FOR_SUBMISSION", "", "", "", ""}, // informational, not a transition
	}
	for _, tc := range cases {
		got, msg := mapAppleStateToReleaseStatus(tc.state, tc.notif, tc.reason)
		if got != tc.want {
			t.Errorf("state=%q: got status %q, want %q", tc.state, got, tc.want)
		}
		if msg != tc.wantMsg {
			t.Errorf("state=%q: got msg %q, want %q", tc.state, msg, tc.wantMsg)
		}
	}
}

// TestMapPlayTrackStatusToReleaseStatus locks down the Play track release
// status → Wemob release status mapping. The polling worker leans on this
// for Android.
func TestMapPlayTrackStatusToReleaseStatus(t *testing.T) {
	cases := []struct{ play, want string }{
		{"draft", models.ReleaseStatusDraft},
		{"inProgress", models.ReleaseStatusInReview},
		{"halted", models.ReleaseStatusPaused},
		{"completed", models.ReleaseStatusLive},
		{"unknown", ""},
	}
	for _, tc := range cases {
		got, _ := MapPlayTrackStatusToReleaseStatus(tc.play)
		if got != tc.want {
			t.Errorf("play %q: got %q, want %q", tc.play, got, tc.want)
		}
	}
}
