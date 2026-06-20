package sandbox

import "time"

type Provider string

const ProviderDaytona Provider = "daytona"

type State string

const (
	StateMissing    State = "missing"
	StateCreating   State = "creating"
	StateSyncing    State = "syncing"
	StateInstalling State = "installing"
	StateStarting   State = "starting"
	StateReady      State = "ready"
	StateDegraded   State = "degraded"
	StateStopped    State = "stopped"
	StateArchived   State = "archived"
	StateError      State = "error"
)

type ProcessName string

const (
	ProcessFrontend  ProcessName = "frontend"
	ProcessBackend   ProcessName = "backend"
	ProcessExpoWeb   ProcessName = "expo_web"
	ProcessExpoGo    ProcessName = "expo_go"
	ProcessArtifacts ProcessName = "artifacts"
	ProcessPreview   ProcessName = "preview"
)

type ProcessStatus string

const (
	ProcessStopped  ProcessStatus = "stopped"
	ProcessStarting ProcessStatus = "starting"
	ProcessReady    ProcessStatus = "ready"
	ProcessError    ProcessStatus = "error"
)

type PreviewKind string

const (
	PreviewApp       PreviewKind = "app"
	PreviewBackend   PreviewKind = "backend"
	PreviewExpo      PreviewKind = "expo"
	PreviewArtifacts PreviewKind = "artifacts"
	PreviewRoute     PreviewKind = "route"
)

type RootName string

const (
	RootProject   RootName = "project"
	RootBackend   RootName = "backend"
	RootArtifacts RootName = "artifacts"
	RootTmp       RootName = "tmp"
)

type Root struct {
	Name RootName `json:"name"`
	Path string   `json:"path"`
}

type ReadinessProbe struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type ProcessSpec struct {
	Name        ProcessName       `json:"name"`
	Command     string            `json:"command"`
	Cwd         string            `json:"cwd"`
	Env         map[string]string `json:"env,omitempty"`
	Port        int               `json:"port"`
	Probe       ReadinessProbe    `json:"probe"`
	LogPath     string            `json:"log_path"`
	AutoRestart bool              `json:"auto_restart"`
}

type RuntimePlan struct {
	AppID           string              `json:"app_id"`
	ProjectType     string              `json:"project_type"`
	FrontendRuntime string              `json:"frontend_runtime"`
	BackendEnabled  bool                `json:"backend_enabled"`
	BackendRuntime  string              `json:"backend_runtime"`
	Roots           []Root              `json:"roots"`
	Processes       []ProcessSpec       `json:"processes"`
	DefaultPreview  PreviewKind         `json:"default_preview"`
	Previews        map[PreviewKind]int `json:"previews"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
}

type ProcessState struct {
	Name         ProcessName   `json:"name"`
	Status       ProcessStatus `json:"status"`
	Port         int           `json:"port"`
	URL          string        `json:"url,omitempty"`
	LastExitCode *int          `json:"last_exit_code,omitempty"`
	LastError    string        `json:"last_error,omitempty"`
	StartedAt    *time.Time    `json:"started_at,omitempty"`
}

type PreviewTarget struct {
	URL       string      `json:"url"`
	Label     string      `json:"label"`
	Kind      PreviewKind `json:"kind"`
	SetBy     string      `json:"set_by"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type Status struct {
	Provider          Provider                     `json:"provider"`
	SandboxID         string                       `json:"sandbox_id"`
	State             State                        `json:"state"`
	LastSyncCommit    string                       `json:"last_sync_commit,omitempty"`
	LastError         string                       `json:"last_error,omitempty"`
	ActivePreview     *PreviewTarget               `json:"active_preview,omitempty"`
	PreviewURLs       map[PreviewKind]string       `json:"preview_urls,omitempty"`
	Processes         map[ProcessName]ProcessState `json:"processes,omitempty"`
	RecommendedAction string                       `json:"recommended_action,omitempty"`
}

type PreviewState string

const (
	PreviewMissing    PreviewState = "missing"
	PreviewPreparing  PreviewState = "preparing"
	PreviewInstalling PreviewState = "installing"
	PreviewStarting   PreviewState = "starting"
	PreviewReady      PreviewState = "ready"
	PreviewStopped    PreviewState = "stopped"
	PreviewError      PreviewState = "error"
)

type PreviewStatus struct {
	AppID     string         `json:"app_id"`
	SandboxID string         `json:"sandbox_id,omitempty"`
	State     PreviewState   `json:"state"`
	URL       string         `json:"url,omitempty"`
	Ports     map[string]int `json:"ports,omitempty"`
	LogsTail  string         `json:"logs_tail,omitempty"`
	Error     string         `json:"error,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type PreviewEvent struct {
	Type      string         `json:"type"`
	State     PreviewState   `json:"state,omitempty"`
	Chunk     string         `json:"chunk,omitempty"`
	Status    *PreviewStatus `json:"status,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type FileDelta struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Delete  bool   `json:"delete,omitempty"`
}

type LogsResult struct {
	Process ProcessName `json:"process"`
	Content string      `json:"content"`
}

type Terminal struct {
	ID string `json:"id"`
}

type Snapshot struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type DiagnosticsResult struct {
	Summary string   `json:"summary"`
	Items   []string `json:"items"`
}
