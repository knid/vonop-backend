package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/knid/wemob/internal/models"
	apptemplates "github.com/knid/wemob/internal/templates"
)

type EnableFrontendTool struct {
	Deps *RunDependencies
}

func (EnableFrontendTool) Name() string {
	return "enable_frontend"
}

func (EnableFrontendTool) Description() string {
	return "Enable a Vite React frontend for a backend-only API project and scaffold frontend/."
}

func (EnableFrontendTool) Schema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t EnableFrontendTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	if rc == nil || rc.Project == nil {
		return "project context is missing", true, nil
	}
	if rc.Project.ProjectType != models.ProjectTypeApp {
		return "enable_frontend is only available for app projects", true, nil
	}
	if rc.Project.FrontendRuntime != models.FrontendRuntimeNone {
		return "frontend is already enabled for this project", false, nil
	}
	if t.Deps == nil || t.Deps.DB == nil {
		return "", false, fmt.Errorf("enable_frontend: database unavailable")
	}

	rc.Project.FrontendRuntime = models.FrontendRuntimeViteReact
	rc.Project.FrontendHosting = models.FrontendHostingS3CDN
	rc.Project.BackendEnabled = true
	rc.Project.BackendRuntime = models.BackendRuntimeNextJSAPI
	rc.Project.UpdatedAt = time.Now()
	if _, err := t.Deps.DB.UpdateAppProject(rc.Project); err != nil {
		return "", false, fmt.Errorf("enable_frontend: update project: %w", err)
	}

	written := 0
	for _, path := range apptemplates.ViteFilePaths {
		dest := "frontend/" + path
		if _, exists := rc.FileMap.Get(dest); exists {
			continue
		}
		content := apptemplates.GetViteTemplate(path)
		if content == "" {
			continue
		}
		rc.FileMap.Set(dest, content)
		written++
		rc.Emitter.Emit(EventFileWritten, FileWrittenEvent{
			RunID:     rc.RunID,
			Path:      dest,
			Operation: "create",
			Bytes:     len(content),
			Content:   content,
		})
	}

	return fmt.Sprintf("Frontend enabled. Scaffolded %d Vite React files under frontend/. Build the UI in frontend/ and call existing backend routes with relative /api/* URLs.", written), false, nil
}
