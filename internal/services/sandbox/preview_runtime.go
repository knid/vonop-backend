package sandbox

import (
	"fmt"
	"sort"
	"strings"

	"github.com/knid/wemob/internal/models"
)

const previewStatePath = "/workspace/.wemob/status.json"

type PreviewProcess struct {
	Name    string
	Cwd     string
	Port    int
	Command string
}

type PreviewRuntime struct {
	Root        string
	InstallDirs []string
	Processes   []PreviewProcess
	Main        string
}

func (r PreviewRuntime) Port(name string) int {
	for _, process := range r.Processes {
		if process.Name == name {
			return process.Port
		}
	}
	return 0
}

func BuildPreviewRuntime(project *models.AppProject) PreviewRuntime {
	if project == nil {
		return PreviewRuntime{}
	}
	switch {
	case project.ProjectType == models.ProjectTypeMobileAppNative:
		return PreviewRuntime{
			Root:        ProjectRoot,
			InstallDirs: []string{ProjectRoot},
			Main:        "web",
			Processes: []PreviewProcess{
				{Name: "web", Cwd: ProjectRoot, Port: 8081, Command: nodeHeapFlag + "CI=1 npx expo start --web --clear --port 8081 --host 0.0.0.0"},
				{Name: "expo", Cwd: ProjectRoot, Port: 19000, Command: "export EXPO_GO_MODE=1 && " + nodeHeapFlag + "CI=1 npx expo start --clear --port 19000 --host 0.0.0.0"},
			},
		}
	case project.FrontendRuntime == models.FrontendRuntimeNone &&
		project.BackendEnabled &&
		project.BackendRuntime == models.BackendRuntimeNextJSAPI:
		return PreviewRuntime{
			Root:        BackendRoot,
			InstallDirs: []string{BackendRoot},
			Main:        "backend",
			Processes: []PreviewProcess{
				{Name: "backend", Cwd: BackendRoot, Port: 3001, Command: nodeHeapFlag + "npm run dev -- -p 3001"},
			},
		}
	case project.UsesSplitWebRuntime():
		return PreviewRuntime{
			Root:        ProjectRoot,
			InstallDirs: []string{BackendRoot, ProjectRoot + "/frontend"},
			Main:        "web",
			Processes: []PreviewProcess{
				{Name: "backend", Cwd: BackendRoot, Port: 3001, Command: nodeHeapFlag + "npm run dev -- -p 3001"},
				{Name: "web", Cwd: ProjectRoot + "/frontend", Port: 3000, Command: nodeHeapFlag + "npm run dev -- --config vite.config.preview.ts --host 0.0.0.0 --port 3000"},
			},
		}
	default:
		return PreviewRuntime{
			Root:        ProjectRoot,
			InstallDirs: []string{ProjectRoot},
			Main:        "web",
			Processes: []PreviewProcess{
				{Name: "web", Cwd: ProjectRoot, Port: 3000, Command: nodeHeapFlag + "npm run dev -- --hostname 0.0.0.0 --port 3000"},
			},
		}
	}
}

func BuildPreviewSupervisorScript(runtime PreviewRuntime) string {
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("set -Eeuo pipefail\n")
	b.WriteString("mkdir -p /workspace/.wemob/logs\n")
	b.WriteString("write_state() {\n")
	b.WriteString("  printf '{\"state\":\"%s\",\"updated_at\":\"%s\"}\\n' \"$1\" \"$(date -u +%FT%TZ)\" > " + previewStatePath + "\n")
	b.WriteString("}\n")
	b.WriteString("cleanup() {\n")
	b.WriteString("  jobs -pr | xargs -r kill 2>/dev/null || true\n")
	b.WriteString("}\n")
	b.WriteString("trap cleanup EXIT\n")
	b.WriteString("write_state installing\n")
	for _, dir := range uniqueInstallDirs(runtime.InstallDirs) {
		b.WriteString(fmt.Sprintf("(cd %s && test ! -f package.json || npm install)\n", shellQuotePath(dir)))
	}
	b.WriteString("write_state starting\n")
	b.WriteString("started_processes=0\n")
	for _, process := range runtime.Processes {
		cwd := process.Cwd
		if cwd == "" {
			cwd = runtime.Root
		}
		logPath := fmt.Sprintf("/workspace/.wemob/logs/%s.log", process.Name)
		packagePath := strings.TrimRight(cwd, "/") + "/package.json"
		b.WriteString(fmt.Sprintf("if test -f %s; then\n", shellQuotePath(packagePath)))
		b.WriteString(fmt.Sprintf("  (cd %s && %s) > %s 2>&1 &\n", shellQuotePath(cwd), process.Command, shellQuotePath(logPath)))
		b.WriteString("  started_processes=$((started_processes + 1))\n")
		b.WriteString("else\n")
		b.WriteString(fmt.Sprintf("  printf 'skipped: %s not found\\n' > %s\n", packagePath, shellQuotePath(logPath)))
		b.WriteString("fi\n")
	}
	b.WriteString("if test \"$started_processes\" -eq 0; then\n")
	b.WriteString("  write_state error\n")
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	mainPort := runtime.Port(runtime.Main)
	if mainPort > 0 {
		b.WriteString("for i in $(seq 1 90); do\n")
		b.WriteString(fmt.Sprintf("  if curl -fsS http://127.0.0.1:%d >/dev/null; then\n", mainPort))
		b.WriteString("    write_state ready\n")
		b.WriteString("    break\n")
		b.WriteString("  fi\n")
		b.WriteString("  sleep 1\n")
		b.WriteString("done\n")
	}
	b.WriteString("wait -n\n")
	return b.String()
}

func uniqueInstallDirs(dirs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

func shellQuotePath(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
