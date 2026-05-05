package fly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// machineFixture builds a Fly Machines API JSON response for one machine.
// events is a list of (type, status) pairs in chronological order.
func machineFixture(id, state, region string, events [][2]string) map[string]any {
	evs := make([]map[string]any, 0, len(events))
	for _, e := range events {
		evs = append(evs, map[string]any{
			"type":   e[0],
			"status": e[1],
			"source": "flyd",
		})
	}
	return map[string]any{
		"id":     id,
		"state":  state,
		"region": region,
		"events": evs,
	}
}

func newTestServer(t *testing.T, machines []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(machines)
	}))
}

func TestDiagnoseDeploymentFailure_HostInitBug(t *testing.T) {
	// Machine with 10 restart-exit cycles, never reached "started" — the
	// canonical Fly host-init-bug signature.
	events := [][2]string{}
	for i := 0; i < 10; i++ {
		events = append(events, [2]string{"restart", "starting"})
		events = append(events, [2]string{"exit", "stopped"})
	}
	server := newTestServer(t, []map[string]any{
		machineFixture("bad-1", "stopped", "fra", events),
	})
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	d, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Kind != FailureHostInitBug {
		t.Errorf("Kind = %v, want HostInitBug", d.Kind)
	}
	if d.MachineID != "bad-1" || d.Region != "fra" {
		t.Errorf("got machine=%q region=%q, want bad-1/fra", d.MachineID, d.Region)
	}
	if d.ExitCount != 10 {
		t.Errorf("ExitCount = %d, want 10", d.ExitCount)
	}
	if d.StartedOnce {
		t.Error("StartedOnce = true, want false for init bug")
	}
}

func TestDiagnoseDeploymentFailure_AppCrashNotRemediated(t *testing.T) {
	// Machine that started successfully then crash-looped. This is the user's
	// app bug, NOT the host-init bug — diagnosis must distinguish the two.
	events := [][2]string{
		{"launch", "created"},
		{"start", "started"},
	}
	for i := 0; i < 12; i++ {
		events = append(events, [2]string{"exit", "stopped"})
		events = append(events, [2]string{"restart", "starting"})
	}
	server := newTestServer(t, []map[string]any{
		machineFixture("crashy-1", "stopped", "fra", events),
	})
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	d, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Kind == FailureHostInitBug {
		t.Errorf("Kind = HostInitBug, want AppCrash — machine reached started before exits")
	}
	if d.Kind != FailureAppCrash {
		t.Errorf("Kind = %v, want AppCrash", d.Kind)
	}
	if !d.StartedOnce {
		t.Error("StartedOnce = false, want true")
	}
}

func TestDiagnoseDeploymentFailure_HealthyMachineReturnsUnknown(t *testing.T) {
	server := newTestServer(t, []map[string]any{
		machineFixture("ok-1", "started", "fra", [][2]string{
			{"launch", "created"}, {"start", "started"},
		}),
	})
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	d, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Kind != FailureUnknown {
		t.Errorf("Kind = %v, want Unknown for healthy machine", d.Kind)
	}
}

func TestDiagnoseDeploymentFailure_PrioritizesInitBugOverAppCrash(t *testing.T) {
	// Multi-machine app: one app-crashed, one host-init-bugged.
	// HostInitBug is the actionable one — we should return it.
	crashEvents := [][2]string{{"start", "started"}}
	for i := 0; i < 9; i++ {
		crashEvents = append(crashEvents, [2]string{"exit", "stopped"})
	}
	initBugEvents := [][2]string{}
	for i := 0; i < 10; i++ {
		initBugEvents = append(initBugEvents, [2]string{"exit", "stopped"})
	}
	server := newTestServer(t, []map[string]any{
		machineFixture("crashy", "stopped", "fra", crashEvents),
		machineFixture("init-bug", "stopped", "fra", initBugEvents),
	})
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	d, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Kind != FailureHostInitBug {
		t.Errorf("Kind = %v, want HostInitBug (must win over AppCrash)", d.Kind)
	}
	if d.MachineID != "init-bug" {
		t.Errorf("MachineID = %q, want init-bug", d.MachineID)
	}
}

func TestDiagnoseDeploymentFailure_BelowThresholdNotInitBug(t *testing.T) {
	// Few exits without a "started" — not enough signal to call it the
	// host-init bug. Could be transient. Should NOT trigger remediation.
	events := [][2]string{}
	for i := 0; i < HostInitBugRestartThreshold-1; i++ {
		events = append(events, [2]string{"exit", "stopped"})
	}
	server := newTestServer(t, []map[string]any{
		machineFixture("maybe", "stopped", "fra", events),
	})
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	d, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.Kind == FailureHostInitBug {
		t.Errorf("Kind = HostInitBug, want non-bug below threshold (got %d exits, threshold %d)",
			d.ExitCount, HostInitBugRestartThreshold)
	}
}

func TestDiagnoseDeploymentFailure_PropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	s := &Service{APIToken: "test", APIBase: server.URL, httpClient: server.Client()}
	_, err := s.DiagnoseDeploymentFailure("wemob-x")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected API error, got %v", err)
	}
}

func TestFallbackRegion(t *testing.T) {
	cases := []struct {
		primary, want string
	}{
		{"fra", "ams"},
		{"ams", "fra"},
		{"cdg", "fra"},
		{"iad", "ord"},
		{"ord", "iad"},
		{"sjc", "den"},
		{"syd", "nrt"},
		{"nrt", "syd"},
		{"unknown-region-xyz", "ams"}, // safe default
	}
	for _, tc := range cases {
		if got := FallbackRegion(tc.primary); got != tc.want {
			t.Errorf("FallbackRegion(%q) = %q, want %q", tc.primary, got, tc.want)
		}
	}
	// Pairs must not loop back to the same region — fallback budget is one.
	for _, primary := range []string{"fra", "ams", "iad", "ord", "sjc", "syd", "nrt"} {
		if FallbackRegion(primary) == primary {
			t.Errorf("FallbackRegion(%q) returned itself", primary)
		}
	}
}
