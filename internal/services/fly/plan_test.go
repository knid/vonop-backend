package fly

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestPlanMachineSpec(t *testing.T) {
	cases := []struct {
		name             string
		memoryMB         int
		deployCPUs       string
		minRunning       int
		wantCPUKind      string
		wantCPUs         int
		wantMaxMachines  int
		wantConcurrency  int
		wantAutoStopSecs int
		wantMinRunning   int
	}{
		// A11: free hard_limit raised 25 → 100. A1: DeployCPUs is the source
		// of truth; "0.5" ceils to 1 CPU.
		{"free 256 + 0.5 CPU", 256, "0.5", 0, "shared", 1, 1, 100, 60, 0},
		{"starter 512 + 1 CPU", 512, "1", 0, "shared", 1, 2, 100, 180, 0},
		// E1/E2: Pro is now 512MB / 1 CPU (not 1024 / 2 CPU).
		{"pro 512 + 1 CPU", 512, "1", 0, "shared", 1, 2, 100, 180, 0},
		// A10: warm machines knob.
		{"pro warm", 512, "1", 1, "shared", 1, 2, 100, 180, 1},
		// Defaults / edge cases.
		{"unset defaults to 256/1", 0, "", 0, "shared", 1, 1, 100, 60, 0},
		// 1024+ memory (kept for power users / future Business tier).
		{"1024 + 2 CPU", 1024, "2", 0, "shared", 2, 3, 200, 300, 0},
		// Fractional CPU ceils up so we never request 0 CPUs.
		{"0.25 CPU ceils to 1", 256, "0.25", 0, "shared", 1, 1, 100, 60, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limits := &models.StandardUserLimits{
				DeployMemoryMB:     c.memoryMB,
				DeployCPUs:         c.deployCPUs,
				MinMachinesRunning: c.minRunning,
			}
			spec := PlanMachineSpec(limits, "iad")
			if spec.Region != "iad" {
				t.Errorf("Region: got %q, want %q", spec.Region, "iad")
			}
			if spec.CPUKind != c.wantCPUKind {
				t.Errorf("CPUKind: got %q, want %q", spec.CPUKind, c.wantCPUKind)
			}
			if spec.CPUs != c.wantCPUs {
				t.Errorf("CPUs: got %d, want %d", spec.CPUs, c.wantCPUs)
			}
			if spec.MaxMachines != c.wantMaxMachines {
				t.Errorf("MaxMachines: got %d, want %d", spec.MaxMachines, c.wantMaxMachines)
			}
			if spec.Concurrency != c.wantConcurrency {
				t.Errorf("Concurrency: got %d, want %d", spec.Concurrency, c.wantConcurrency)
			}
			if spec.AutoStopSeconds != c.wantAutoStopSecs {
				t.Errorf("AutoStopSeconds: got %d, want %d", spec.AutoStopSeconds, c.wantAutoStopSecs)
			}
			if spec.MinMachinesRunning != c.wantMinRunning {
				t.Errorf("MinMachinesRunning: got %d, want %d", spec.MinMachinesRunning, c.wantMinRunning)
			}
		})
	}
}
