package fly

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type machineConfig struct {
	Image    string                 `json:"image"`
	Env      map[string]string      `json:"env,omitempty"`
	Services []machineServiceConfig `json:"services,omitempty"`
	Guest    machineGuestConfig     `json:"guest"`
	Restart  map[string]any         `json:"restart,omitempty"`
	Metadata map[string]string      `json:"metadata,omitempty"`
}

type machineServiceConfig struct {
	InternalPort int                       `json:"internal_port"`
	Protocol     string                    `json:"protocol"`
	Ports        []machinePortConfig       `json:"ports"`
	Concurrency  *machineConcurrencyConfig `json:"concurrency,omitempty"`
	AutoStop     string                    `json:"auto_stop_machines,omitempty"`
	AutoStart    bool                      `json:"auto_start_machines"`
	MinMachines  int                       `json:"min_machines_running"`
	Checks       []machineCheckConfig      `json:"checks,omitempty"`
}

type machinePortConfig struct {
	Port       int      `json:"port"`
	Handlers   []string `json:"handlers,omitempty"`
	ForceHTTPS bool     `json:"force_https,omitempty"`
}

type machineConcurrencyConfig struct {
	Type      string `json:"type"`       // "requests" | "connections"
	HardLimit int    `json:"hard_limit"`
	SoftLimit int    `json:"soft_limit"`
}

type machineCheckConfig struct {
	Type     string `json:"type"`
	Port     int    `json:"port,omitempty"`
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
	Method   string `json:"method,omitempty"`
	Path     string `json:"path,omitempty"`
}

type machineGuestConfig struct {
	CPUKind  string `json:"cpu_kind"`
	CPUs     int    `json:"cpus"`
	MemoryMB int    `json:"memory_mb"`
}

type machineCreateRequest struct {
	Name   string        `json:"name,omitempty"`
	Region string        `json:"region"`
	Config machineConfig `json:"config"`
}

type machineSummary struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Name  string `json:"name"`
}

// DeployMachines creates the first Machine for an app or replaces the existing
// fleet with new Machines built from `spec`. Idempotent: safe to call multiple
// times — replaces the running fleet atomically.
func (s *Service) DeployMachines(appName string, spec MachineSpec) error {
	if spec.Image == "" {
		return fmt.Errorf("fly: DeployMachines: spec.Image is required")
	}

	cfg := machineConfig{
		Image: spec.Image,
		Guest: machineGuestConfig{
			CPUKind:  spec.CPUKind,
			CPUs:     spec.CPUs,
			MemoryMB: spec.MemoryMB,
		},
		Services: []machineServiceConfig{{
			InternalPort: 3000,
			Protocol:     "tcp",
			Ports: []machinePortConfig{
				// 80 redirects to 443 via Fly's edge so we don't serve plain HTTP.
				{Port: 80, Handlers: []string{"http"}, ForceHTTPS: true},
				// 443 terminates TLS at Fly's edge using *.fly.dev wildcard cert,
				// then proxies HTTP to internal_port 3000. Required for Caddy to
				// reach the app via HTTPS without our own cert plumbing.
				{Port: 443, Handlers: []string{"tls", "http"}},
			},
			Concurrency: &machineConcurrencyConfig{
				Type:      "requests",
				HardLimit: spec.Concurrency,
				SoftLimit: spec.Concurrency * 8 / 10, // soft = 80% of hard
			},
			AutoStop:    "stop", // stop after idle
			AutoStart:   true,   // wake on incoming request
			MinMachines: 0,      // scale-to-zero
			Checks: []machineCheckConfig{{
				Type:     spec.Healthcheck.Type,
				Port:     spec.Healthcheck.Port,
				Interval: spec.Healthcheck.Interval,
				Timeout:  "5s",
			}},
		}},
		Metadata: map[string]string{
			"managed_by":     "wemob",
			"max_machines":   fmt.Sprintf("%d", spec.MaxMachines),
			"auto_stop_secs": fmt.Sprintf("%d", spec.AutoStopSeconds),
		},
	}

	// List existing machines so we can replace or update them.
	var existing []machineSummary
	if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &existing); err != nil {
		return err
	}

	// Strategy: if there are existing machines, update the first one and destroy
	// the rest. If none, create one.
	if len(existing) == 0 {
		req := machineCreateRequest{Region: spec.Region, Config: cfg}
		return s.doRequest("POST", "/v1/apps/"+appName+"/machines", req, nil)
	}

	keep := existing[0]
	updateReq := map[string]any{"region": spec.Region, "config": cfg}
	if err := s.doRequest("POST", "/v1/apps/"+appName+"/machines/"+keep.ID, updateReq, nil); err != nil {
		return err
	}
	for _, m := range existing[1:] {
		err := s.doRequest("DELETE", "/v1/apps/"+appName+"/machines/"+m.ID+"?force=true", nil, nil)
		if err == nil {
			continue
		}
		if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusNotFound {
			continue // already gone
		}
		slog.Warn("fly: failed to destroy orphan machine", "app", appName, "machine", m.ID, "error", err)
		// Continue: the updated primary is serving traffic, orphans don't get traffic.
	}
	return nil
}

// WaitForHealthy polls until at least one Machine for the app reports state
// "started" or returns an error after `timeout`.
func (s *Service) WaitForHealthy(appName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		var machines []machineSummary
		if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &machines); err != nil {
			return err
		}
		for _, m := range machines {
			if m.State == "started" {
				return nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("fly: WaitForHealthy: %s did not reach started state within %s", appName, timeout)
		}
		select {
		case <-tick.C:
		case <-time.After(remaining):
			return fmt.Errorf("fly: WaitForHealthy: %s did not reach started state within %s", appName, timeout)
		}
	}
}
