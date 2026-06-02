package agent

import (
	"strings"
	"testing"
)

func TestBuildStateRenderTracksFilesDeterministically(t *testing.T) {
	state := NewBuildState("build admin dashboard with history and alerts", true)
	state.RecordFileWrite("components/z.tsx", "create", "export function Zed() { return <div>Complete component with enough implementation detail to be treated as done.</div> }\n")
	state.RecordFileWrite("app/page.tsx", "create", "export default function Page() { return <main>Complete page with dashboard, alerts, and chronological history content.</main> }\n")
	state.RecordFileWrite("app/layout.tsx", "update", "export default function Layout({ children }) { return <html><body>{children}</body></html> }\n")
	state.RecordFileDelete("components/old.tsx")

	rendered := state.Render(NewSafeFileMap(map[string]string{
		"app/layout.tsx": "layout",
		"app/page.tsx":   "page",
	}))

	if !strings.Contains(rendered, "BUILD STATE") {
		t.Fatalf("expected BUILD STATE header, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Phase: main_ui") {
		t.Fatalf("expected foundation to advance when app shell exists, got:\n%s", rendered)
	}
	assertBefore(t, rendered, "- app/page.tsx", "- components/z.tsx")
	if !strings.Contains(rendered, "admin/owner dashboard") ||
		!strings.Contains(rendered, "chronological log/history view") ||
		!strings.Contains(rendered, "smart alert/visual indicator behavior") {
		t.Fatalf("expected requirement hints in render, got:\n%s", rendered)
	}
}

func TestBuildStateDoesNotListCreatedFileAsUpdated(t *testing.T) {
	state := NewBuildState("build app", true)
	state.RecordFileWrite("app/page.tsx", "create", "export default function Page() { return <main>Complete page implementation with visible app content.</main> }\n")
	state.RecordFileWrite("app/page.tsx", "update", "export default function Page() { return <main>Updated complete page implementation with visible app content.</main> }\n")

	rendered := state.Render(nil)
	if !strings.Contains(rendered, "Created this run:\n- app/page.tsx") {
		t.Fatalf("expected created path, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "Updated this run:\n- app/page.tsx") {
		t.Fatalf("created path should not also be listed as updated:\n%s", rendered)
	}
}

func TestRenderRunPrefixIncludesFileTreeAndBuildState(t *testing.T) {
	fileMap := NewSafeFileMap(map[string]string{"app/page.tsx": "page"})
	state := NewBuildState("build dashboard", true)
	state.RecordFileWrite("app/page.tsx", "create", strings.Repeat("export const dashboardWidget = true;\n", 4))

	rendered := renderRunPrefix(fileMap, state)
	if !strings.Contains(rendered, "FILE TREE") {
		t.Fatalf("expected file tree in prefix, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "BUILD STATE") {
		t.Fatalf("expected build state in prefix, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Do not recreate") {
		t.Fatalf("expected do-not-recreate guidance, got:\n%s", rendered)
	}
}

func TestBuildStateFlagsTinySourceFilesForRewrite(t *testing.T) {
	state := NewBuildState("build dashboard", true)
	reason := state.RecordFileWrite("components/admin-dashboard.tsx", "create", "export default function AdminDashboard() { return null }\n")

	if reason == "" {
		t.Fatal("expected tiny placeholder source file to be flagged")
	}

	rendered := state.Render(nil)
	if !strings.Contains(rendered, "Needs rewrite") ||
		!strings.Contains(rendered, "components/admin-dashboard.tsx") {
		t.Fatalf("expected needs-rewrite section for tiny source file, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "Do not recreate; edit only if more changes are needed:\n- components/admin-dashboard.tsx") {
		t.Fatalf("tiny source file should not be treated as completed durable work:\n%s", rendered)
	}
}

func TestBuildStateClearsRewriteFlagAfterCompleteUpdate(t *testing.T) {
	state := NewBuildState("build dashboard", true)
	state.RecordFileWrite("components/admin-dashboard.tsx", "create", "export default function AdminDashboard() { return null }\n")
	state.RecordFileWrite("components/admin-dashboard.tsx", "update", strings.Repeat("export const x = 1;\n", 10))

	rendered := state.Render(nil)
	if strings.Contains(rendered, "Needs rewrite; too small or placeholder-like, not complete:") {
		t.Fatalf("expected rewrite flag to clear after substantial update, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Do not recreate; edit only if more changes are needed:\n- components/admin-dashboard.tsx") {
		t.Fatalf("expected completed source file in do-not-recreate list, got:\n%s", rendered)
	}
}

func assertBefore(t *testing.T, s, first, second string) {
	t.Helper()
	firstIdx := strings.Index(s, first)
	secondIdx := strings.Index(s, second)
	if firstIdx < 0 || secondIdx < 0 {
		t.Fatalf("missing %q or %q in:\n%s", first, second, s)
	}
	if firstIdx > secondIdx {
		t.Fatalf("expected %q before %q in:\n%s", first, second, s)
	}
}
