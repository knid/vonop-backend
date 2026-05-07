package jobstream

import (
	"testing"
)

func TestEventChannelName(t *testing.T) {
	got := EventsChannel("abc-123")
	want := "job:events:abc-123"
	if got != want {
		t.Errorf("EventsChannel: got %q want %q", got, want)
	}
}

func TestCancelChannelName(t *testing.T) {
	got := CancelChannel("abc-123")
	want := "job:cancel:abc-123"
	if got != want {
		t.Errorf("CancelChannel: got %q want %q", got, want)
	}
}
