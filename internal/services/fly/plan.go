package fly

import (
	"math"
	"strconv"

	"github.com/knid/wemob/internal/models"
)

// PlanMachineSpec returns the Fly Machine spec corresponding to a user's plan
// tier. CPU comes from limits.DeployCPUs (source of truth — A1 fix); memory
// from limits.DeployMemoryMB. Concurrency, MaxMachines, and AutoStopSeconds
// derive from the memory tier. region defaults to FLY_PRIMARY_REGION but a
// caller can pass a per-app override (E6, Pro+ region selection).
func PlanMachineSpec(limits *models.StandardUserLimits, region string) MachineSpec {
	memoryMB := 256
	if limits != nil && limits.DeployMemoryMB > 0 {
		memoryMB = limits.DeployMemoryMB
	}

	// CPU from DeployCPUs (A1). fly.toml [[vm]] cpus is integer-only, so we
	// ceil fractional values: "0.5" → 1, "1" → 1, "2" → 2. Default 1 when the
	// field is missing or unparseable so we never accidentally request 0 CPUs
	// (Fly rejects that with a 422).
	cpus := 1
	if limits != nil && limits.DeployCPUs != "" {
		if v, err := strconv.ParseFloat(limits.DeployCPUs, 64); err == nil && v > 0 {
			cpus = int(math.Ceil(v))
		}
	}

	s := MachineSpec{
		CPUKind:  "shared",
		CPUs:     cpus,
		MemoryMB: memoryMB,
		Region:   region,
		Healthcheck: HealthcheckSpec{
			Type:     "tcp",
			Port:     3000,
			Interval: "15s",
		},
	}

	// Memory-tier defaults for concurrency / horizontal-scale ceiling /
	// autostop window. A11 raised free hard_limit from 25 to 100; A12 dropped
	// the unreachable >=2048 performance-1x branch.
	switch {
	case memoryMB <= 256:
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 1, 100, 60
	case memoryMB <= 512:
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 2, 100, 180
	default: // 1024+
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 3, 200, 300
	}

	// A10: warm-machine knob. 0 keeps scale-to-zero semantics (current
	// behavior across all plans). Caller-provided value is propagated to
	// fly.toml's min_machines_running.
	if limits != nil && limits.MinMachinesRunning > 0 {
		s.MinMachinesRunning = limits.MinMachinesRunning
	}

	return s
}
