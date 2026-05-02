//go:build integration

package eval

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestExpoFixtures runs each fixture against the live agent and verifies
// structural rules on the generated source.
//
// REQUIRES env: ANTHROPIC_API_KEY. Skip if absent.
//
// This test is gated behind -tags=integration so the standard
// `go test ./...` doesn't try to run it.
func TestExpoFixtures(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping integration eval")
	}
	if testing.Short() {
		t.Skip("integration test: skipping in short mode")
	}

	runner, err := newAgentRunner(t)
	if err != nil {
		t.Skipf("agent runner not yet implemented: %v (TODO P1.1)", err)
	}

	for _, fx := range ExpoFixtures {
		t.Run(fx.Name, func(t *testing.T) {
			generated, err := runner.GenerateOnce(context.Background(), fx.Prompt)
			if err != nil {
				t.Fatalf("agent run failed: %v", err)
			}
			blob := combineFiles(generated)
			for _, s := range fx.MustContain {
				if !strings.Contains(blob, s) {
					t.Errorf("fixture %q: expected %q in output, not found", fx.Name, s)
				}
			}
			for _, s := range fx.MustNotContain {
				if strings.Contains(blob, s) {
					t.Errorf("fixture %q: expected %q NOT in output, but found", fx.Name, s)
				}
			}
		})
	}
}

// agentRunner abstracts the agent invocation so the test can stay independent
// of the real production wiring (controllers, DB, etc.). The constructor
// returns an error if the runner can't be wired up — the test then t.Skips.
type agentRunner interface {
	// GenerateOnce runs the agent on `prompt` for a fresh mobile_app_native
	// project and returns the map of generated file paths to contents.
	GenerateOnce(ctx context.Context, prompt string) (map[string]string, error)
}

// newAgentRunner constructs the runner. Returns an error if integration
// infrastructure (DB, controllers) isn't available — the caller should t.Skip.
//
// TODO(P1.1): implement against the real agent API. For now this returns an
// error so the integration test skips cleanly.
func newAgentRunner(t *testing.T) (agentRunner, error) {
	t.Helper()
	return nil, errAgentRunnerUnimplemented
}

var errAgentRunnerUnimplemented = &runnerNotImpl{}

type runnerNotImpl struct{}

func (runnerNotImpl) Error() string {
	return "agent runner harness not yet implemented (TODO P1.1)"
}

func combineFiles(files map[string]string) string {
	var sb strings.Builder
	for path, content := range files {
		sb.WriteString("// FILE: ")
		sb.WriteString(path)
		sb.WriteString("\n")
		sb.WriteString(content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}
