package views

import (
	"testing"

	"github.com/knid/wemob/internal/services/sandbox"
)

func TestShouldEnsurePreviewRestartsNonReadyTerminalStates(t *testing.T) {
	for _, state := range []sandbox.PreviewState{
		sandbox.PreviewMissing,
		sandbox.PreviewStopped,
		sandbox.PreviewError,
	} {
		t.Run(string(state), func(t *testing.T) {
			if !shouldEnsurePreview(state) {
				t.Fatalf("shouldEnsurePreview(%q) = false, want true", state)
			}
		})
	}
}

func TestShouldEnsurePreviewDoesNotRestartActiveStates(t *testing.T) {
	for _, state := range []sandbox.PreviewState{
		sandbox.PreviewPreparing,
		sandbox.PreviewInstalling,
		sandbox.PreviewStarting,
		sandbox.PreviewReady,
	} {
		t.Run(string(state), func(t *testing.T) {
			if shouldEnsurePreview(state) {
				t.Fatalf("shouldEnsurePreview(%q) = true, want false", state)
			}
		})
	}
}

func TestUnseenPreviewLogLinesSuppressesRepeatedTailSnapshots(t *testing.T) {
	emitted := map[string]bool{}

	first := unseenPreviewLogLines("--- web.log ---\nready\n", emitted)
	if first != "--- web.log ---\nready\n" {
		t.Fatalf("first chunk = %q", first)
	}

	second := unseenPreviewLogLines("--- web.log ---\nready\nnew line\n", emitted)
	if second != "new line\n" {
		t.Fatalf("second chunk = %q", second)
	}

	third := unseenPreviewLogLines("--- web.log ---\nready\nnew line\n", emitted)
	if third != "" {
		t.Fatalf("third chunk = %q, want empty", third)
	}
}
