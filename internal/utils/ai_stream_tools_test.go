package utils

import (
	"strings"
	"testing"
)

func TestShouldCacheToolResult_DoesNotCacheFailedExactStringEdits(t *testing.T) {
	result := `{"changed":false,"error_code":"old_string_not_found","next_step":"read_file_range"}`

	if shouldCacheToolResult("multi_edit", result) {
		t.Fatal("failed multi_edit exact-string result must not be cached")
	}
	if shouldCacheToolResult("edit_file", result) {
		t.Fatal("failed edit_file exact-string result must not be cached")
	}
}

func TestShouldCacheToolResult_CachesSuccessfulAndReadResults(t *testing.T) {
	if !shouldCacheToolResult("read_files", "content") {
		t.Fatal("read results should still be cacheable")
	}
	if !shouldCacheToolResult("multi_edit", `{"changed":true,"path":"app/page.tsx"}`) {
		t.Fatal("successful edit result should remain cacheable")
	}
}

func TestExactStringEditRecoveryInstruction_TellsModelToResync(t *testing.T) {
	result := `{"changed":false,"error_code":"old_string_not_found","next_step":"read_file_range"}`

	instruction := exactStringEditRecoveryInstruction("multi_edit", result)
	if instruction == "" {
		t.Fatal("expected recovery instruction for failed exact-string edit")
	}
	if !containsAll(instruction, "Do not retry", "read_file_range") {
		t.Fatalf("instruction should force resync before retry, got: %q", instruction)
	}
}

func containsAll(s string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(s, needle) {
			return false
		}
	}
	return true
}
