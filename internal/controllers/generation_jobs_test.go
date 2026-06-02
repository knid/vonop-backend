package controllers

import "testing"

func TestCreditsUpdateIsMilestoneEvent(t *testing.T) {
	if !IsMilestoneEvent("credits_update") {
		t.Fatal("credits_update must be persisted so reconnect can replay the latest billed credits")
	}
}
