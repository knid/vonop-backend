package fly

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestPlanMachineSpec(t *testing.T) {
	cases := []struct {
		name             string
		memoryMB         int
		wantCPUKind      string
		wantCPUs         int
		wantMaxMachines  int
		wantConcurrency  int
		wantAutoStopSecs int
	}{
		{"free 256", 256, "shared", 1, 1, 25, 60},
		{"starter 512", 512, "shared", 1, 2, 50, 180},
		{"pro 1024", 1024, "shared", 2, 3, 100, 300},
		{"business 2048", 2048, "performance", 1, 5, 200, 300},
		{"unset defaults to 256", 0, "shared", 1, 1, 25, 60},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limits := &models.StandardUserLimits{DeployMemoryMB: c.memoryMB}
			spec := PlanMachineSpec(limits, "iad")
			if c.name == "free 256" && spec.Region != "iad" {
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
		})
	}
}
