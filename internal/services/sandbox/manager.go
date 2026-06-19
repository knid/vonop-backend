package sandbox

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"time"

	"github.com/knid/wemob/internal/models"
)

type Manager struct {
	provider ProviderClient
}

func NewManager(provider ProviderClient) *Manager {
	return &Manager{provider: provider}
}

func (m *Manager) Start(ctx context.Context, project *models.AppProject, files map[string]string, env map[string]string) (*Status, error) {
	plan := BuildRuntimePlan(project)
	sandboxID := project.SandboxID
	var err error
	if sandboxID == "" {
		sandboxID, err = m.provider.Create(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("create sandbox: %w", err)
		}
		project.SandboxID = sandboxID
	} else if err := m.provider.Start(ctx, sandboxID); err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}
	if err := m.Sync(ctx, project, files); err != nil {
		return nil, err
	}
	for _, spec := range plan.Processes {
		if spec.Name == ProcessArtifacts {
			continue
		}
		if err := m.provider.StartProcess(ctx, sandboxID, spec); err != nil {
			return nil, fmt.Errorf("start process %s: %w", spec.Name, err)
		}
	}
	return m.Status(ctx, project)
}

func (m *Manager) Status(ctx context.Context, project *models.AppProject) (*Status, error) {
	if project.SandboxID == "" {
		return &Status{Provider: ProviderDaytona, State: StateMissing}, nil
	}
	plan := BuildRuntimePlan(project)
	st, err := m.provider.GetStatus(ctx, project.SandboxID, plan)
	if err != nil {
		return nil, err
	}
	st.PreviewURLs = map[PreviewKind]string{}
	kinds := make([]PreviewKind, 0, len(plan.Previews))
	for kind := range plan.Previews {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, kind := range kinds {
		u, err := m.provider.PreviewURL(ctx, project.SandboxID, plan.Previews[kind])
		if err == nil {
			st.PreviewURLs[kind] = u
		}
	}
	processes, err := m.provider.ProcessStates(ctx, project.SandboxID, plan)
	if err == nil {
		st.Processes = processes
	}
	return st, nil
}

func (m *Manager) Sync(ctx context.Context, project *models.AppProject, files map[string]string) error {
	if project.SandboxID == "" {
		return fmt.Errorf("sandbox is not started")
	}
	deltas := make([]FileDelta, 0, len(files))
	for p, content := range files {
		deltas = append(deltas, FileDelta{Path: p, Content: content})
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Path < deltas[j].Path })
	if err := m.provider.WriteFiles(ctx, project.SandboxID, deltas, BuildRuntimePlan(project)); err != nil {
		return fmt.Errorf("sync sandbox files: %w", err)
	}
	return nil
}

func (m *Manager) OpenRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*PreviewTarget, error) {
	if project.SandboxID == "" {
		return nil, fmt.Errorf("sandbox is not started")
	}
	if route == "" {
		route = "/"
	}
	if route[0] != '/' {
		route = "/" + route
	}
	plan := BuildRuntimePlan(project)
	port, ok := plan.Previews[plan.DefaultPreview]
	if !ok {
		return nil, fmt.Errorf("default preview is unavailable")
	}
	base, err := m.provider.PreviewURL(ctx, project.SandboxID, port)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Path = path.Clean(route)
	if route == "/" {
		u.Path = "/"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return &PreviewTarget{
		URL:       u.String(),
		Label:     "App preview",
		Kind:      PreviewRoute,
		SetBy:     setBy,
		UpdatedAt: time.Now().UTC(),
	}, nil
}
