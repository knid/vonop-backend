package fly

import "time"

type MachineSpec struct {
	Image              string
	MemoryMB           int
	CPUKind            string // "shared" | "performance"
	CPUs               int
	MaxMachines        int
	Concurrency        int
	AutoStopSeconds    int
	MinMachinesRunning int // 0 = scale-to-zero; >=1 keeps machines warm
	Region             string
	Healthcheck        HealthcheckSpec
}

type HealthcheckSpec struct {
	Type     string // "tcp" | "http"
	Port     int
	Interval string // "15s"
	Path     string // only for http
}

// MachineEvent represents a Fly webhook payload (subset we care about).
type MachineEvent struct {
	AppName    string    `json:"app_name"`
	MachineID  string    `json:"machine_id"`
	State      string    `json:"state"` // "started" | "stopped" | "destroyed" | "failed"
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}
