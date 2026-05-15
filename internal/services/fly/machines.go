package fly

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type machineSummary struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Name  string `json:"name"`
}

// MachineStatus is the studio-facing summary of one Fly machine.
// Mirrors the Fly Machines API response, trimmed to the fields the
// studio actually displays.
type MachineStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`       // "started" | "stopped" | "destroyed" | ...
	Region    string `json:"region"`
	Image     string `json:"image"`       // e.g. "registry.fly.io/wemob-acme:abc123"
	UpdatedAt string `json:"updated_at"` // RFC3339, as returned by Fly
}

// DeployMachines releases the image to a Fly Machine via `flyctl deploy --image`.
//
// We shell out to flyctl rather than POSTing to the Machines API directly because
// Apps V2 (Machines) has NO public API to create a "release". The legacy GraphQL
// `deployImage` mutation 422s for Machines apps ("requires the 'nomad' platform").
// Without a release, the app stays `deployed=false` forever, secrets stay Staged,
// and machines never bind to them no matter how many times you create them.
//
// flyctl's deploy flow handles all of:
//   - creating the release (which flips the app to `deployed=true`)
//   - destroying old machines and creating new ones in one atomic operation
//   - binding the new machines to the latest secret version (so process.env is set)
//
// We write a complete fly.toml in a tmp dir so flyctl uses OUR config
// (autostop=suspend, scale-to-zero, concurrency limits) instead of inventing
// defaults. `--ha=false` prevents flyctl's automatic standby machine creation
// (we'd just have to delete the standby afterwards).
//
// `--strategy immediate` replaces all machines at once. Brief downtime is fine
// for these scale-to-zero apps — they're often already suspended when redeployed.
func (s *Service) DeployMachines(appName string, spec MachineSpec) error {
	if spec.Image == "" {
		return fmt.Errorf("fly: DeployMachines: spec.Image is required")
	}

	tmpDir, err := os.MkdirTemp("", "fly-deploy-")
	if err != nil {
		return fmt.Errorf("fly: mkdir temp: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tomlPath := filepath.Join(tmpDir, "fly.toml")
	if err := os.WriteFile(tomlPath, []byte(buildFlyToml(appName, spec)), 0o644); err != nil {
		return fmt.Errorf("fly: write fly.toml: %w", err)
	}

	if err := retryFlyctlDeploy(appName, spec.Image, func() error {
		cmd := exec.Command("flyctl", "deploy",
			"--app", appName,
			"--image", spec.Image,
			"--strategy", "immediate",
			"--ha=false",
			"--yes",
		)
		cmd.Dir = tmpDir
		cmd.Env = []string{
			"HOME=" + os.Getenv("HOME"),
			"PATH=" + os.Getenv("PATH"),
			"FLY_API_TOKEN=" + s.APIToken,
			"FLY_NO_UPDATE_CHECK=1",
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return &flyctlDeployError{err: err, output: string(out)}
		}
		slog.Info("fly_release_succeeded", "app", appName, "image", spec.Image)
		return nil
	}); err != nil {
		return err
	}

	// flyctl deploy leaves machines in "stopped" state for autostart-on-traffic
	// apps (auto_start_machines=true, min_machines_running=0). There's no traffic
	// during deploy, so WaitForHealthy would otherwise wait 10 minutes for an
	// autostart that never comes. Kick them explicitly.
	return s.StartStoppedMachines(appName)
}

// StartStoppedMachines explicitly starts any machines on the app that are in
// the "stopped" state. Works around the flyctl deploy + scale-to-zero combo
// described in DeployMachines: machines come up stopped and only start on
// first request, which means deploys appear hung from WaitForHealthy's view.
//
// Idempotent for already-started machines (they're skipped). Errors propagate
// so a deploy fails fast instead of timing out on the healthcheck.
func (s *Service) StartStoppedMachines(appName string) error {
	var machines []machineSummary
	if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &machines); err != nil {
		return fmt.Errorf("fly: list machines for start: %w", err)
	}
	for _, m := range machines {
		if m.State != "stopped" {
			continue
		}
		if err := s.doRequest("POST", "/v1/apps/"+appName+"/machines/"+m.ID+"/start", nil, nil); err != nil {
			return fmt.Errorf("fly: start machine %s: %w", m.ID, err)
		}
		slog.Info("fly_machine_started", "app", appName, "machine", m.ID)
	}
	return nil
}

// flyctlDeployError wraps flyctl exec failures so the retry layer can
// pattern-match the output for transient errors (MANIFEST_UNKNOWN).
type flyctlDeployError struct {
	err    error
	output string
}

func (e *flyctlDeployError) Error() string {
	return fmt.Sprintf("flyctl: %s (output: %s)", e.err, truncate(e.output, 1000))
}

// retryFlyctlDeploy retries while flyctl's stderr reports MANIFEST_UNKNOWN —
// Fly's Machines API registry index lags Depot's push by 5–10s. Backoff
// 1s/2s/4s/8s capped, 30s deadline.
func retryFlyctlDeploy(appName, imageRef string, fn func() error) error {
	backoff := time.Second
	deadline := time.Now().Add(30 * time.Second)
	attempt := 0
	for {
		attempt++
		err := fn()
		if err == nil {
			if attempt > 1 {
				slog.Info("fly_deploy_succeeded_after_retry", "app", appName, "image", imageRef, "attempts", attempt)
			}
			return nil
		}
		var fe *flyctlDeployError
		retryable := errors.As(err, &fe) && strings.Contains(fe.output, "MANIFEST_UNKNOWN")
		if !retryable || time.Now().After(deadline) {
			return err
		}
		slog.Debug("fly_deploy_manifest_unknown_retrying", "app", appName, "image", imageRef, "attempt", attempt)
		time.Sleep(backoff)
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
}

// buildFlyToml renders a complete fly.toml that captures every machine config
// knob we care about — flyctl uses this to provision machines with the same
// shape we used to send via the Machines API directly. The fly.toml syntax
// (`auto_stop_machines = "suspend"`, etc.) is what flyctl translates internally
// to the API field names (`autostop`, etc.).
func buildFlyToml(appName string, spec MachineSpec) string {
	hard := spec.Concurrency
	soft := spec.Concurrency * 8 / 10 // soft = 80% of hard

	var extras strings.Builder
	if spec.MaxMachines > 0 {
		fmt.Fprintf(&extras, "  max_machines_running = %d\n", spec.MaxMachines)
	}
	if spec.AutoStopSeconds > 0 {
		fmt.Fprintf(&extras, "  auto_stop_machines_seconds = %d\n", spec.AutoStopSeconds)
	}

	// A10: min_machines_running is a per-plan knob now. 0 keeps the historical
	// scale-to-zero behavior; >=1 keeps machines warm so paid plans can opt
	// out of cold starts at the cost of steady-state Fly compute.
	minRunning := spec.MinMachinesRunning
	if minRunning < 0 {
		minRunning = 0
	}

	return fmt.Sprintf(`app = %q
primary_region = %q

[[vm]]
  cpu_kind = %q
  cpus = %d
  memory_mb = %d

[http_service]
  internal_port = 3000
  force_https = true
  auto_stop_machines = "suspend"
  auto_start_machines = true
  min_machines_running = %d
%s
  [http_service.concurrency]
    type = "requests"
    soft_limit = %d
    hard_limit = %d
`,
		appName, spec.Region,
		spec.CPUKind, spec.CPUs, spec.MemoryMB,
		minRunning,
		extras.String(),
		soft, hard,
	)
}

// GetMachineStatus returns the most representative machine for the given
// Fly app. If multiple machines exist (we don't run multi-machine apps
// today, but tolerate it), the first machine in the response wins. If
// there are zero machines, returns a non-nil error so callers can show
// a "not deployed yet" state.
func (s *Service) GetMachineStatus(appName string) (MachineStatus, error) {
	var raw []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		State    string `json:"state"`
		Region   string `json:"region"`
		ImageRef struct {
			Registry   string `json:"registry"`
			Repository string `json:"repository"`
			Tag        string `json:"tag"`
		} `json:"image_ref"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &raw); err != nil {
		return MachineStatus{}, err
	}
	if len(raw) == 0 {
		return MachineStatus{}, fmt.Errorf("fly: no machines for app %q", appName)
	}
	m := raw[0]
	var image string
	if m.ImageRef.Registry != "" && m.ImageRef.Repository != "" {
		image = m.ImageRef.Registry + "/" + m.ImageRef.Repository
		if m.ImageRef.Tag != "" {
			image += ":" + m.ImageRef.Tag
		}
	}
	return MachineStatus{
		ID:        m.ID,
		Name:      m.Name,
		State:     m.State,
		Region:    m.Region,
		Image:     image,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

// DestroyAllMachines stops + removes every Machine on the app. Used by the
// auto-remediation path: a redeploy in a fallback region needs the stuck-in-
// `fra` machines gone first, otherwise flyctl deploy keeps the dead ones
// around alongside the new replacements. Best-effort per-machine: a single
// failure is logged and skipped so one stubborn machine can't block the rest.
//
// Note: flyctl deploy already destroys+recreates machines as part of a normal
// release. Use this only when the existing machines are in an unrecoverable
// state (looping on init failure) AND you need to change deploy parameters
// (e.g. region) that flyctl deploy alone won't shift.
func (s *Service) DestroyAllMachines(appName string) error {
	var machines []machineSummary
	if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &machines); err != nil {
		return fmt.Errorf("fly: list machines for destroy: %w", err)
	}
	for _, m := range machines {
		// force=true is required when the machine is in stopped/failed state
		// because the API otherwise expects a stop call first.
		if err := s.doRequest("DELETE", "/v1/apps/"+appName+"/machines/"+m.ID+"?force=true", nil, nil); err != nil {
			slog.Warn("fly: destroy machine failed (continuing)", "app", appName, "machine", m.ID, "err", err)
			continue
		}
	}
	return nil
}

// WaitForHealthy polls until at least one Machine for the app reports state
// "started" or returns an error after `timeout`.
//
// Tick is 1s — flyctl deploy --strategy immediate often returns within a few
// seconds of the machine actually being ready, so a 3s poll added avoidable
// tail latency to first-deploy time-to-served.
func (s *Service) WaitForHealthy(appName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(time.Second)
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
