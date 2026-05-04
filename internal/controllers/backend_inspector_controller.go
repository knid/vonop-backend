package controllers

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/responses"
	"github.com/knid/wemob/internal/services/fly"
)

// RouteEntry is one method-handler exported from a Next.js route.ts file.
type RouteEntry struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	File   string `json:"file"`
}

var routeFilePattern = regexp.MustCompile(`^backend/app/api/(?:.*/)?route\.ts$`)
var handlerExportPattern = regexp.MustCompile(`export\s+(?:async\s+)?function\s+(GET|POST|PUT|DELETE|PATCH|OPTIONS|HEAD)\b`)

// ParseRoutes scans a map of repo-relative file paths to file contents and
// returns the list of API route handlers exported from
// backend/app/api/**/route.ts. Pure; does no IO.
//
// Limitation: scans raw text, so a single-line-commented handler signature
// (`// export async function GET() {}`) is incorrectly counted. Acceptable
// for the studio's "what handlers exist?" view; not suitable as a lint.
func ParseRoutes(files map[string]string) []RouteEntry {
	var out []RouteEntry
	for path, content := range files {
		if !routeFilePattern.MatchString(path) {
			continue
		}
		urlPath := nextRouteURL(path)
		for _, m := range handlerExportPattern.FindAllStringSubmatch(content, -1) {
			out = append(out, RouteEntry{
				Method: m[1],
				Path:   urlPath,
				File:   path,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// nextRouteURL converts a Next.js route file path to its URL path.
// Examples:
//
//	backend/app/api/todos/route.ts                  → /api/todos
//	backend/app/api/users/[id]/route.ts             → /api/users/:id
//	backend/app/api/files/[...slug]/route.ts        → /api/files/*slug
//	backend/app/api/users/[id]/posts/[pid]/route.ts → /api/users/:id/posts/:pid
func nextRouteURL(filePath string) string {
	trimmed := strings.TrimPrefix(filePath, "backend/app")
	trimmed = strings.TrimSuffix(trimmed, "/route.ts")
	if trimmed == "" {
		return "/"
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, "/"), "/")
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, "[...") && strings.HasSuffix(part, "]"):
			parts[i] = "*" + part[4:len(part)-1]
		case strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]"):
			parts[i] = ":" + part[1:len(part)-1]
		}
	}
	return "/" + strings.Join(parts, "/")
}

// FlyInspector is the slice of fly.Service the studio inspector consumes.
// Defined as an interface so tests can substitute a fake without spinning
// an httptest.Server. *fly.Service satisfies it.
type FlyInspector interface {
	GetMachineStatus(appName string) (fly.MachineStatus, error)
	GetLogs(appName string, since time.Time, level string) ([]fly.LogLine, error)
}

// Compile-time guarantee that *fly.Service implements FlyInspector.
var _ FlyInspector = (*fly.Service)(nil)

// GetBackendHealth aggregates Fly machine status + last-deploy info for the
// given mobile-native project. Returns a fully populated response even when
// the backend doesn't exist yet (HasBackend=false, color=gray) — never errs
// for "no backend".
func (c *Controller) GetBackendHealth(ctx context.Context, project *models.AppProject) (responses.BackendHealthResponse, error) {
	out := responses.BackendHealthResponse{
		DerivedColor: "gray",
	}

	if project == nil {
		return out, errors.New("nil project")
	}

	runtimeApp, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	if err != nil || runtimeApp == nil || runtimeApp.FlyAppName == "" {
		return out, nil // gray, no backend
	}

	out.HasBackend = true
	out.URL = c.BackendDeployURL(project)

	if c.FlyInspector != nil {
		if status, ferr := c.FlyInspector.GetMachineStatus(runtimeApp.FlyAppName); ferr == nil {
			out.MachineState = status.State
			out.Region = status.Region
			out.Image = status.Image
			out.UpdatedAt = status.UpdatedAt
		}
	}

	if dep, derr := c.DB.GetLatestAppDeployment(project.ID.String(), "web"); derr == nil && dep != nil && !dep.CreatedAt.IsZero() {
		out.LastDeployAt = dep.CreatedAt.Format(time.RFC3339)
		out.LastDeployOK = dep.Status == "deployed"
		out.LastDeployURL = dep.DeployedURL
		if dep.Status == "queued" || dep.Status == "building" || dep.Status == "deploying" {
			out.IsDeploying = true
			out.DeployStatus = dep.Status
			out.DeployStartedAt = dep.CreatedAt.Format(time.RFC3339)
		}
	}

	out.DerivedColor = deriveBackendColor(out)
	return out, nil
}

// GetBackendRoutes lists API route handlers detected in backend/app/api/**.
// Reads files via the existing gitrepo store. Returns an empty slice when
// the project has no repo or no backend/app/api/ subtree — never errors
// for "no routes".
func (c *Controller) GetBackendRoutes(ctx context.Context, project *models.AppProject) ([]responses.BackendRouteEntry, error) {
	if project == nil {
		return nil, errors.New("nil project")
	}
	if c.GitRepoService == nil {
		return []responses.BackendRouteEntry{}, nil
	}
	repo, err := c.GitRepoService.Open(project.ID.String())
	if err != nil || repo == nil {
		return []responses.BackendRouteEntry{}, nil
	}
	files, err := repo.ReadAllFiles()
	if err != nil {
		return nil, err
	}
	parsed := ParseRoutes(files)
	out := make([]responses.BackendRouteEntry, 0, len(parsed))
	for _, r := range parsed {
		out = append(out, responses.BackendRouteEntry{
			Method: r.Method,
			Path:   r.Path,
			File:   r.File,
		})
	}
	return out, nil
}

// GetBackendLogs returns recent log lines for the deployed backend, filtered
// by since/level. Returns empty slice for projects without a backend or
// without a wired FlyInspector — never errors for "no backend".
func (c *Controller) GetBackendLogs(ctx context.Context, project *models.AppProject, since time.Time, level string) ([]responses.BackendLogLine, error) {
	if project == nil {
		return nil, errors.New("nil project")
	}
	runtimeApp, err := c.DB.GetRuntimeAppByAppProjectID(project.ID.String())
	if err != nil || runtimeApp == nil || runtimeApp.FlyAppName == "" {
		return []responses.BackendLogLine{}, nil
	}
	if c.FlyInspector == nil {
		return []responses.BackendLogLine{}, nil
	}
	raw, err := c.FlyInspector.GetLogs(runtimeApp.FlyAppName, since, level)
	if err != nil {
		return nil, err
	}
	out := make([]responses.BackendLogLine, 0, len(raw))
	for _, ln := range raw {
		out = append(out, responses.BackendLogLine{
			Timestamp: ln.Timestamp,
			Level:     ln.Level,
			Message:   ln.Message,
		})
	}
	return out, nil
}

// deriveBackendColor encodes the spec's color rules:
//
//	green  — machine started + last deploy succeeded
//	yellow — deploying / never deployed / auto-stopped (idle, not broken)
//	red    — last deploy failed or machine in an error state
//	gray   — no backend yet (caller handles this; here we expect HasBackend=true)
func deriveBackendColor(h responses.BackendHealthResponse) string {
	if !h.HasBackend {
		return "gray"
	}
	if h.IsDeploying {
		return "yellow"
	}
	switch h.MachineState {
	case "destroyed", "destroying", "failed":
		return "red"
	}
	if h.LastDeployAt != "" && !h.LastDeployOK {
		return "red"
	}
	if h.LastDeployAt == "" {
		return "yellow"
	}
	if h.MachineState == "stopped" || h.MachineState == "" {
		return "yellow"
	}
	if h.MachineState == "started" && h.LastDeployOK {
		return "green"
	}
	return "yellow"
}
