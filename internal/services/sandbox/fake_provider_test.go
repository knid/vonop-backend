package sandbox

import (
	"context"
	"fmt"
)

type fakeProvider struct {
	sandboxID        string
	createErr        error
	statusState      State
	statusLastError  string
	files            []FileDelta
	startCalls       int
	startErr         error
	writeErr         error
	started          []ProcessName
	stopped          []ProcessName
	startProcessErr  error
	previews         map[int]string
	previewErr       error
	processStatesErr error
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{sandboxID: "daytona-test", previews: map[int]string{}}
}

func (f *fakeProvider) Create(ctx context.Context, plan RuntimePlan) (string, error) {
	return f.sandboxID, f.createErr
}

func (f *fakeProvider) GetStatus(ctx context.Context, sandboxID string, plan RuntimePlan) (*Status, error) {
	state := f.statusState
	if state == "" {
		state = StateReady
	}
	return &Status{Provider: ProviderDaytona, SandboxID: sandboxID, State: state, LastError: f.statusLastError, PreviewURLs: map[PreviewKind]string{}}, nil
}

func (f *fakeProvider) Start(ctx context.Context, sandboxID string) error {
	f.startCalls++
	return f.startErr
}

func (f *fakeProvider) Stop(ctx context.Context, sandboxID string) error { return nil }

func (f *fakeProvider) Archive(ctx context.Context, sandboxID string) error { return nil }

func (f *fakeProvider) Delete(ctx context.Context, sandboxID string) error { return nil }

func (f *fakeProvider) WriteFiles(ctx context.Context, sandboxID string, files []FileDelta, plan RuntimePlan) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.files = append(f.files, files...)
	return nil
}

func (f *fakeProvider) Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (string, string, int, error) {
	return "", "", 0, nil
}

func (f *fakeProvider) StartProcess(ctx context.Context, sandboxID string, spec ProcessSpec) error {
	if f.startProcessErr != nil {
		return f.startProcessErr
	}
	f.started = append(f.started, spec.Name)
	return nil
}

func (f *fakeProvider) StopProcess(ctx context.Context, sandboxID string, name ProcessName) error {
	f.stopped = append(f.stopped, name)
	return nil
}

func (f *fakeProvider) ProcessLogs(ctx context.Context, sandboxID string, spec ProcessSpec, lines int) (*LogsResult, error) {
	return &LogsResult{Process: spec.Name, Content: "ok"}, nil
}

func (f *fakeProvider) ProcessStates(ctx context.Context, sandboxID string, plan RuntimePlan) (map[ProcessName]ProcessState, error) {
	if f.processStatesErr != nil {
		return nil, f.processStatesErr
	}
	out := map[ProcessName]ProcessState{}
	for _, spec := range plan.Processes {
		out[spec.Name] = ProcessState{Name: spec.Name, Status: ProcessReady, Port: spec.Port}
	}
	return out, nil
}

func (f *fakeProvider) PreviewURL(ctx context.Context, sandboxID string, port int) (string, error) {
	if f.previewErr != nil {
		return "", f.previewErr
	}
	if f.previews[port] == "" {
		f.previews[port] = fmt.Sprintf("https://provider-%d.example.test", port)
	}
	return f.previews[port], nil
}

func (f *fakeProvider) CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*Terminal, error) {
	return &Terminal{ID: "term-1"}, nil
}

func (f *fakeProvider) SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error {
	return nil
}

func (f *fakeProvider) CloseTerminal(ctx context.Context, sandboxID, terminalID string) error {
	return nil
}

func (f *fakeProvider) CreateSnapshot(ctx context.Context, sandboxID string) (*Snapshot, error) {
	return &Snapshot{ID: "snap-1"}, nil
}

func (f *fakeProvider) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error {
	return nil
}

func (f *fakeProvider) RunDiagnostics(ctx context.Context, sandboxID string, plan RuntimePlan) (*DiagnosticsResult, error) {
	return &DiagnosticsResult{Summary: "ok"}, nil
}
