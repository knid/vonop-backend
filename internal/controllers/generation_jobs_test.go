package controllers

import "testing"

func TestCreditsUpdateIsMilestoneEvent(t *testing.T) {
	if !IsMilestoneEvent("credits_update") {
		t.Fatal("credits_update must be persisted so reconnect can replay the latest billed credits")
	}
}

func TestSubagentStartIsMilestoneEvent(t *testing.T) {
	if !IsMilestoneEvent("subagent_start") {
		t.Fatal("subagent_start must be persisted so reconnect can show subagent lifecycle")
	}
}

func TestSubagentEndIsMilestoneEvent(t *testing.T) {
	if !IsMilestoneEvent("subagent_end") {
		t.Fatal("subagent_end must be persisted so reconnect can show subagent lifecycle")
	}
}
