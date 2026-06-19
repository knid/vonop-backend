package sandbox

import "context"

type ProviderClient interface {
	Create(ctx context.Context, plan RuntimePlan) (string, error)
	GetStatus(ctx context.Context, sandboxID string, plan RuntimePlan) (*Status, error)
	Start(ctx context.Context, sandboxID string) error
	Stop(ctx context.Context, sandboxID string) error
	Archive(ctx context.Context, sandboxID string) error
	Delete(ctx context.Context, sandboxID string) error

	WriteFiles(ctx context.Context, sandboxID string, files []FileDelta, plan RuntimePlan) error
	Execute(ctx context.Context, sandboxID, command, cwd string, timeoutMs int) (stdout, stderr string, exitCode int, err error)

	StartProcess(ctx context.Context, sandboxID string, spec ProcessSpec) error
	StopProcess(ctx context.Context, sandboxID string, name ProcessName) error
	ProcessLogs(ctx context.Context, sandboxID string, spec ProcessSpec, lines int) (*LogsResult, error)
	ProcessStates(ctx context.Context, sandboxID string, plan RuntimePlan) (map[ProcessName]ProcessState, error)

	PreviewURL(ctx context.Context, sandboxID string, port int) (string, error)

	CreateTerminal(ctx context.Context, sandboxID string, cols, rows int) (*Terminal, error)
	SendTerminalInput(ctx context.Context, sandboxID, terminalID, input string) error
	CloseTerminal(ctx context.Context, sandboxID, terminalID string) error

	CreateSnapshot(ctx context.Context, sandboxID string) (*Snapshot, error)
	RestoreSnapshot(ctx context.Context, sandboxID, snapshotID string) error
	RunDiagnostics(ctx context.Context, sandboxID string, plan RuntimePlan) (*DiagnosticsResult, error)
}
