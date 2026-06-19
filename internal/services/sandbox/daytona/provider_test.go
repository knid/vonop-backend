package daytona

import (
	"context"
	"strings"
	"testing"

	daytonasdk "github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	sdkoptions "github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	"github.com/knid/wemob/internal/services/sandbox"
)

type fakeClient struct{}

func (fakeClient) Create(context.Context, any, ...func(*sdkoptions.CreateSandbox)) (*daytonasdk.Sandbox, error) {
	return &daytonasdk.Sandbox{ID: "sandbox-123"}, nil
}

func (fakeClient) Get(context.Context, string) (*daytonasdk.Sandbox, error) {
	return &daytonasdk.Sandbox{ID: "sandbox-123"}, nil
}

func TestNewRequiresAPIKey(t *testing.T) {
	provider, err := New(" ", "", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
}

func TestNewWithClientConfiguresProvider(t *testing.T) {
	provider := NewWithClient(fakeClient{}, "preview.wemob.app")
	if err := provider.ready(); err != nil {
		t.Fatalf("expected provider to be ready: %v", err)
	}
	if provider.previewDomain != "preview.wemob.app" {
		t.Fatalf("preview domain = %q", provider.previewDomain)
	}
}

func TestMapState(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want sandbox.State
	}{
		{name: "started", in: "started", want: sandbox.StateReady},
		{name: "stopped", in: "stopped", want: sandbox.StateStopped},
		{name: "archived", in: "archived", want: sandbox.StateArchived},
		{name: "pending build", in: "pending_build", want: sandbox.StateCreating},
		{name: "starting", in: "starting", want: sandbox.StateStarting},
		{name: "build failed", in: "build_failed", want: sandbox.StateError},
		{name: "unknown", in: "unexpected", want: sandbox.StateDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := mapState(tt.in)
			if got != tt.want {
				t.Fatalf("mapState(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDestinationPathDefaultsToProjectRoot(t *testing.T) {
	got, err := destinationPath("src/App.tsx", sandbox.RuntimePlan{})
	if err != nil {
		t.Fatalf("destinationPath returned error: %v", err)
	}
	if got != "/workspace/project/src/App.tsx" {
		t.Fatalf("destinationPath = %q", got)
	}
}

func TestDestinationPathSupportsFutureRootPrefix(t *testing.T) {
	plan := sandbox.RuntimePlan{
		Roots: []sandbox.Root{
			{Name: sandbox.RootBackend, Path: "/workspace/backend"},
		},
	}
	got, err := destinationPath("backend/package.json", plan)
	if err != nil {
		t.Fatalf("destinationPath returned error: %v", err)
	}
	if got != "/workspace/backend/package.json" {
		t.Fatalf("destinationPath = %q", got)
	}
}

func TestDestinationPathRejectsUnsafePaths(t *testing.T) {
	for _, filePath := range []string{"", "/abs", "../escape", "a/../b", "a\x00b"} {
		t.Run(filePath, func(t *testing.T) {
			if _, err := destinationPath(filePath, sandbox.RuntimePlan{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestProcessCommandWrapsCWDAndLogs(t *testing.T) {
	got := processCommand(sandbox.ProcessSpec{
		Command: "npm run dev -- --port 3000",
		Cwd:     "/workspace/project",
		LogPath: "/tmp/wemob-frontend.log",
	})
	for _, want := range []string{
		"bash -lc ",
		"mkdir -p",
		"/tmp",
		"cd",
		"/workspace/project",
		"npm run dev -- --port 3000",
		"/tmp/wemob-frontend.log",
		"2>&1",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("processCommand missing %q in %q", want, got)
		}
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("don't")
	want := "'don'\"'\"'t'"
	if got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
}
