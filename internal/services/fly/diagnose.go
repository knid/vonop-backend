package fly

import (
	"fmt"
)

// FailureKind classifies why a Fly app couldn't reach a healthy state.
// Used by the deploy worker to decide whether auto-remediation can help
// (HostInitBug → retry in alt region) or whether the failure is the user's
// problem (AppCrash → bubble up).
type FailureKind int

const (
	FailureUnknown FailureKind = iota

	// FailureHostInitBug — Fly's init binary fails BEFORE the app's container
	// starts, kernel-reboots the VM, repeats. Signature: many exit/restart
	// cycles, machine never reaches "started". Cause is a Fly platform-side
	// host or init-binary issue (e.g. netlink EEXIST during net-namespace
	// setup). Remediated by destroying and redeploying in a sibling region.
	FailureHostInitBug

	// FailureAppCrash — the app started successfully and then exited. This is
	// almost always a real bug in the user's code (panic on startup, missing
	// env var, port binding failure). NOT auto-remediable.
	FailureAppCrash
)

// Diagnosis is the result of inspecting machine state + event history for an
// unhealthy Fly app. Populated only when WaitForHealthy times out — we don't
// pay for this on the happy path.
type Diagnosis struct {
	Kind         FailureKind
	MachineID    string
	Region       string
	RestartCount int
	ExitCount    int
	StartedOnce  bool   // true if the machine ever reached the "started" status
	Detail       string // human-readable, suitable for slog/event metadata
}

// HostInitBugRestartThreshold is the number of restart→exit cycles required
// before we classify a stuck machine as the Fly host-init bug. Fly's flyd
// caps auto-restarts at 10, so anything over ~8 with no successful start is
// the unambiguous signature.
const HostInitBugRestartThreshold = 8

// DiagnoseDeploymentFailure inspects every machine on the app and returns the
// most-actionable diagnosis. If multiple machines are unhealthy, the first
// HostInitBug match wins (so the worker can act); otherwise the highest-
// confidence finding is returned.
//
// Heuristic: a machine that has ≥HostInitBugRestartThreshold exit events but
// has NEVER reached the "started" status didn't crash — it kernel-rebooted
// before the app's entrypoint ran. This distinguishes the Fly init bug
// (pre-app failure) from genuine app crashes (post-start exit loop).
func (s *Service) DiagnoseDeploymentFailure(appName string) (Diagnosis, error) {
	type event struct {
		Type   string `json:"type"`
		Status string `json:"status"`
		Source string `json:"source"`
	}
	type machine struct {
		ID     string  `json:"id"`
		State  string  `json:"state"`
		Region string  `json:"region"`
		Events []event `json:"events"`
	}
	var machines []machine
	if err := s.doRequest("GET", "/v1/apps/"+appName+"/machines", nil, &machines); err != nil {
		return Diagnosis{}, err
	}

	// Walk every machine; remember the strongest diagnosis seen.
	var best Diagnosis
	for _, m := range machines {
		var restarts, exits int
		startedOnce := false
		for _, ev := range m.Events {
			switch ev.Type {
			case "restart":
				restarts++
			case "exit":
				exits++
			}
			if ev.Status == "started" {
				startedOnce = true
			}
		}

		switch {
		case exits >= HostInitBugRestartThreshold && !startedOnce:
			d := Diagnosis{
				Kind: FailureHostInitBug, MachineID: m.ID, Region: m.Region,
				RestartCount: restarts, ExitCount: exits, StartedOnce: false,
				Detail: fmt.Sprintf("%d exits / %d restarts on machine %s without ever reaching started — Fly host init bug", exits, restarts, m.ID),
			}
			return d, nil
		case exits > 0 && startedOnce && best.Kind == FailureUnknown:
			best = Diagnosis{
				Kind: FailureAppCrash, MachineID: m.ID, Region: m.Region,
				RestartCount: restarts, ExitCount: exits, StartedOnce: true,
				Detail: fmt.Sprintf("machine %s started then exited %d times — app-level failure", m.ID, exits),
			}
		}
	}
	return best, nil
}

// FallbackRegion returns a sibling region in roughly the same geography. The
// auto-remediation budget is one fallback only — if both primary and fallback
// hit the same bug, it's a Fly-wide incident and we bubble up rather than
// cycling regions.
func FallbackRegion(primary string) string {
	switch primary {
	case "fra":
		return "ams"
	case "ams", "cdg", "lhr":
		return "fra"
	case "iad", "ewr", "bos":
		return "ord"
	case "ord", "atl":
		return "iad"
	case "sjc", "lax", "sea":
		return "den"
	case "den":
		return "sjc"
	case "syd":
		return "nrt"
	case "nrt", "hkg", "sin":
		return "syd"
	case "gru", "scl":
		return "iad"
	default:
		return "ams"
	}
}
