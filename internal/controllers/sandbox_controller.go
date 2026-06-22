package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/sandbox"
)

func (c *Controller) EnsurePreview(ctx context.Context, project *models.AppProject) (*sandbox.PreviewStatus, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return nil, err
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	addSandboxSyncRevision(files)
	status, err := c.SandboxManager.EnsurePreview(ctx, project, files, envVars)
	if err != nil {
		return nil, err
	}
	if c.DB != nil && project.SandboxID != "" {
		_ = c.DB.UpdateAppProjectSandboxID(project.ID.String(), project.SandboxID)
	}
	return status, nil
}

func (c *Controller) EnsureAgentSandbox(ctx context.Context, project *models.AppProject) (*sandbox.PreviewStatus, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	c.refreshSandboxID(ctx, project)
	status, err := c.PreviewStatus(ctx, project)
	if err != nil {
		return nil, err
	}
	if !agentPreviewShouldEnsure(status) {
		return status, nil
	}
	return c.EnsurePreview(ctx, project)
}

func agentPreviewShouldEnsure(status *sandbox.PreviewStatus) bool {
	if status == nil {
		return true
	}
	if status.SyncStale {
		return true
	}
	switch status.State {
	case sandbox.PreviewMissing, sandbox.PreviewStopped, sandbox.PreviewError:
		return true
	default:
		return false
	}
}

func (c *Controller) PreviewStatus(ctx context.Context, project *models.AppProject) (*sandbox.PreviewStatus, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	status, err := c.SandboxManager.PreviewStatus(ctx, project)
	if status != nil {
		if files, sourceErr := c.sandboxSourceFiles(project); sourceErr == nil {
			addSandboxEnvFiles(project, files, c.CollectAllEnvVars(project))
			status.SourceRevision = sandboxSourceRevision(files)
			status.SyncStale = status.SourceRevision != "" && status.SourceRevision != status.SandboxRevision
		}
	}
	return status, err
}

func (c *Controller) RestartPreview(ctx context.Context, project *models.AppProject) (*sandbox.PreviewStatus, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return nil, err
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	addSandboxSyncRevision(files)
	status, err := c.SandboxManager.RestartPreview(ctx, project, files, envVars)
	if err != nil {
		return nil, err
	}
	if project.SandboxID != "" {
		_ = c.DB.UpdateAppProjectSandboxID(project.ID.String(), project.SandboxID)
	}
	return status, nil
}

func (c *Controller) PreviewLogs(ctx context.Context, project *models.AppProject, target string, lines int) (string, error) {
	if c.SandboxManager == nil {
		return "", fmt.Errorf("sandbox manager is not configured")
	}
	return c.SandboxManager.PreviewLogs(ctx, project, target, lines)
}

func (c *Controller) SandboxStatus(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	return c.SandboxManager.Status(ctx, project)
}

func (c *Controller) StartSandbox(ctx context.Context, project *models.AppProject) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return nil, err
	}
	return c.startSandboxWithFiles(ctx, project, files)
}

func (c *Controller) startSandboxWithFiles(ctx context.Context, project *models.AppProject, files map[string]string) (*sandbox.Status, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	addSandboxSyncRevision(files)
	status, err := c.SandboxManager.Start(ctx, project, files, envVars)
	if err != nil {
		return nil, err
	}
	if project.SandboxID != "" {
		_ = c.DB.UpdateAppProjectSandboxID(project.ID.String(), project.SandboxID)
	}
	return status, nil
}

func (c *Controller) SyncSandbox(ctx context.Context, project *models.AppProject) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	c.refreshSandboxID(ctx, project)
	if project == nil || project.SandboxID == "" {
		return nil
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return err
	}
	return c.syncSandboxWithFiles(ctx, project, files)
}

func (c *Controller) SyncSandboxPaths(ctx context.Context, project *models.AppProject, paths []string) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	if len(paths) == 0 {
		return nil
	}
	c.refreshSandboxID(ctx, project)
	if project == nil || project.SandboxID == "" {
		return nil
	}
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return err
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	sourceRevision := sandboxSourceRevision(files)
	deltas := make([]sandbox.FileDelta, 0, len(paths))
	for _, p := range normalizeSandboxChangedPaths(project, paths) {
		if content, ok := files[p]; ok {
			deltas = append(deltas, sandbox.FileDelta{Path: p, Content: content})
			continue
		}
		deltas = append(deltas, sandbox.FileDelta{Path: p, Delete: true})
	}
	if len(deltas) == 0 {
		return nil
	}
	deltas = append(deltas, sandbox.FileDelta{Path: sandbox.SyncRevisionPath, Content: sourceRevision})
	return c.SandboxManager.SyncDeltas(ctx, project, deltas, envVars)
}

func (c *Controller) SyncSandboxDeltas(ctx context.Context, project *models.AppProject, deltas []sandbox.FileDelta) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	if len(deltas) == 0 {
		return nil
	}
	c.refreshSandboxID(ctx, project)
	files, err := c.sandboxSourceFiles(project)
	if err != nil {
		return err
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	sourceRevision := sandboxSourceRevision(files)
	normalized := make([]sandbox.FileDelta, 0, len(deltas)+1)
	for _, delta := range deltas {
		paths := normalizeSandboxChangedPaths(project, []string{delta.Path})
		if len(paths) == 0 {
			continue
		}
		delta.Path = paths[0]
		normalized = append(normalized, delta)
	}
	if len(normalized) == 0 {
		return nil
	}
	normalized = append(normalized, sandbox.FileDelta{Path: sandbox.SyncRevisionPath, Content: sourceRevision})
	return c.SandboxManager.SyncDeltas(ctx, project, normalized, envVars)
}

func (c *Controller) syncSandboxWithFiles(ctx context.Context, project *models.AppProject, files map[string]string) error {
	if c.SandboxManager == nil {
		return fmt.Errorf("sandbox manager is not configured")
	}
	envVars := c.CollectAllEnvVars(project)
	addSandboxEnvFiles(project, files, envVars)
	addSandboxSyncRevision(files)
	return c.SandboxManager.Sync(ctx, project, files, envVars)
}

func (c *Controller) refreshSandboxID(_ context.Context, project *models.AppProject) {
	if c == nil || c.DB == nil || project == nil || project.SandboxID != "" || project.ID == uuid.Nil {
		return
	}
	latest, err := c.DB.GetAppProjectByID(project.ID.String())
	if err != nil || latest == nil || latest.SandboxID == "" {
		return
	}
	adoptLatestSandboxID(project, latest)
}

func adoptLatestSandboxID(project, latest *models.AppProject) bool {
	if project == nil || latest == nil || project.SandboxID != "" || latest.SandboxID == "" {
		return false
	}
	project.SandboxID = latest.SandboxID
	return true
}

func (c *Controller) OpenSandboxRoute(ctx context.Context, project *models.AppProject, route, setBy string) (*sandbox.PreviewTarget, error) {
	if c.SandboxManager == nil {
		return nil, fmt.Errorf("sandbox manager is not configured")
	}
	target, err := c.SandboxManager.OpenRoute(ctx, project, route, setBy)
	if err != nil {
		return nil, err
	}
	if project.Settings == nil {
		project.Settings = map[string]any{}
	}
	stored := map[string]any{
		"url":        target.URL,
		"label":      target.Label,
		"kind":       string(target.Kind),
		"set_by":     target.SetBy,
		"updated_at": target.UpdatedAt.Format(time.RFC3339),
	}
	project.Settings["preview_target"] = stored
	return target, c.DB.UpdateAppProjectPreviewTarget(project.ID.String(), stored)
}

func (c *Controller) sandboxSourceFiles(project *models.AppProject) (map[string]string, error) {
	if project == nil {
		return nil, fmt.Errorf("project is required")
	}
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil {
		return nil, fmt.Errorf("no generated files found")
	}
	fileMap, err := repo.ReadAllFiles()
	if err != nil || len(fileMap) == 0 {
		return nil, fmt.Errorf("no generated files found")
	}
	if project.ProjectType == models.ProjectTypeMobileAppNative {
		mobileFiles := stripPreviewSubtree(fileMap, "mobile/")
		if len(mobileFiles) == 0 {
			return nil, fmt.Errorf("no mobile files found")
		}
		return mobileFiles, nil
	}
	return fileMap, nil
}

func addSandboxEnvFiles(project *models.AppProject, files map[string]string, envVars map[string]string) {
	if project == nil || files == nil {
		return
	}
	dotEnv := BuildDotEnvLocal(envVars)
	for _, path := range sandboxEnvFilePaths(project) {
		files[path] = dotEnv
	}
}

func addSandboxSyncRevision(files map[string]string) {
	if files == nil {
		return
	}
	files[sandbox.SyncRevisionPath] = sandboxSourceRevision(files)
}

func sandboxSourceRevision(files map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		if p == sandbox.SyncRevisionPath {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write([]byte(files[p]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sandboxEnvFilePaths(project *models.AppProject) []string {
	if project == nil {
		return nil
	}
	switch {
	case project.UsesSplitWebRuntime(), project.FrontendRuntime == models.FrontendRuntimeNone:
		if project.BackendEnabled || project.FrontendRuntime == models.FrontendRuntimeNone {
			return []string{"backend/.env.local"}
		}
	case project.ProjectType != models.ProjectTypeMobileAppNative:
		return []string{".env.local"}
	}
	return nil
}

func normalizeSandboxChangedPaths(project *models.AppProject, paths []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
		if p == "" {
			continue
		}
		if project != nil && project.ProjectType == models.ProjectTypeMobileAppNative {
			if !strings.HasPrefix(p, "mobile/") {
				continue
			}
			p = strings.TrimPrefix(p, "mobile/")
			if p == "" {
				continue
			}
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
