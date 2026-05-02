package fly

import (
	"strconv"

	"github.com/knid/wemob/internal/models"
)

// PlanMachineSpec returns the Fly Machine spec corresponding to a user's plan
// tier. Driven by DeployMemoryMB so existing plan rows don't need new fields.
// region should be the value of FLY_PRIMARY_REGION (e.g. "iad").
func PlanMachineSpec(limits *models.StandardUserLimits, region string) MachineSpec {
	memoryMB := 256
	if limits != nil && limits.DeployMemoryMB > 0 {
		memoryMB = limits.DeployMemoryMB
	}

	s := MachineSpec{
		CPUKind:  "shared",
		CPUs:     1,
		MemoryMB: memoryMB,
		Region:   region,
		Healthcheck: HealthcheckSpec{
			Type:     "tcp",
			Port:     3000,
			Interval: "15s",
		},
	}
	switch {
	case memoryMB <= 256:
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 1, 25, 60
	case memoryMB <= 512:
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 2, 50, 180
	case memoryMB <= 1024:
		s.CPUs, s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 2, 3, 100, 300
	default: // >= 2048
		s.CPUKind, s.CPUs = "performance", 1
		s.MaxMachines, s.Concurrency, s.AutoStopSeconds = 5, 200, 300
	}

	// Honor a custom DeployCPUs from limits when it would yield more CPU than
	// the memory-tier default. Fly's [[vm]] cpus field is integer-only, so
	// fractional values like "0.25" floor to 0 and never beat the default.
	if limits != nil && limits.DeployCPUs != "" {
		if v, err := strconv.ParseFloat(limits.DeployCPUs, 64); err == nil && int(v) > s.CPUs {
			s.CPUs = int(v)
		}
	}
	return s
}
