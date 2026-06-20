package views

import "testing"

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
