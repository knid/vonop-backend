package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/knid/wemob/internal/models"
)

const (
	runCheckTimeout = 90 * time.Second
	runCheckMaxOut  = 12 * 1024
)

type RunCheckTool struct{}

func (RunCheckTool) Name() string { return "run_check" }
func (RunCheckTool) Description() string {
	return "Run the project's local build/typecheck script against the current in-memory files. Does not install dependencies. Use after file changes to catch build errors before finishing."
}
func (RunCheckTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"enum":        []string{"auto", "web", "mobile", "backend"},
				"description": "Which project target to check. Default auto chooses mobile for native apps, otherwise web.",
			},
		},
	}
}

func (RunCheckTool) Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (string, bool, error) {
	var args struct {
		Target string `json:"target"`
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args)
	}
	target := strings.TrimSpace(args.Target)
	if target == "" || target == "auto" {
		if rc.Project != nil && rc.Project.ProjectType == models.ProjectTypeMobileAppNative {
			target = "mobile"
		} else {
			target = "web"
		}
	}

	tmp, err := os.MkdirTemp("", "wemob-run-check-*")
	if err != nil {
		return fmt.Sprintf("run_check failed to create temp dir: %v", err), true, nil
	}
	defer os.RemoveAll(tmp)

	for path, content := range rc.FileMap.Snapshot() {
		full := filepath.Join(tmp, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return fmt.Sprintf("run_check failed to create %s: %v", path, err), true, nil
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return fmt.Sprintf("run_check failed to write %s: %v", path, err), true, nil
		}
	}

	workDir, command, err := runCheckCommand(rc, tmp, target)
	if err != nil {
		return err.Error(), true, nil
	}
	linkNodeModules(rc, tmp, workDir)

	checkCtx, cancel := context.WithTimeout(ctx, runCheckTimeout)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, command[0], command[1:]...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if len(output) > runCheckMaxOut {
		output = output[:runCheckMaxOut] + "\n...(truncated)"
	}
	if checkCtx.Err() != nil {
		return fmt.Sprintf("run_check timed out after %s\n%s", runCheckTimeout, output), true, nil
	}
	cmdText := strings.Join(command, " ")
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return fmt.Sprintf("run_check failed: %s\n\n%s", cmdText, output), true, nil
	}
	if output == "" {
		output = "ok"
	}
	return fmt.Sprintf("run_check passed: %s\n\n%s", cmdText, output), false, nil
}

func runCheckCommand(rc *RunContext, tmp, target string) (string, []string, error) {
	switch target {
	case "web":
		return packageScriptCommand(filepath.Join(tmp), "build")
	case "mobile":
		return packageScriptCommand(filepath.Join(tmp, "mobile"), "typecheck", "build")
	case "backend":
		return packageScriptCommand(filepath.Join(tmp, "backend"), "build")
	default:
		return "", nil, fmt.Errorf("unknown run_check target %q", target)
	}
}

func packageScriptCommand(dir string, preferred ...string) (string, []string, error) {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", nil, fmt.Errorf("run_check cannot find %s", pkgPath)
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "", nil, fmt.Errorf("run_check package.json is invalid: %v", err)
	}
	for _, script := range preferred {
		if _, ok := pkg.Scripts[script]; ok {
			return dir, []string{"npm", "run", script}, nil
		}
	}
	return "", nil, fmt.Errorf("run_check cannot find any script: %s", strings.Join(preferred, ", "))
}

func linkNodeModules(rc *RunContext, tmp, workDir string) {
	if rc == nil || rc.Deps == nil || rc.Deps.GitRepo == nil || rc.Project == nil {
		return
	}
	repo, err := rc.Deps.GitRepo.Open(rc.Project.ID.String())
	if err != nil || repo == nil {
		return
	}
	rel, err := filepath.Rel(tmp, workDir)
	if err != nil {
		return
	}
	source := filepath.Join(repo.Path, rel, "node_modules")
	if st, err := os.Stat(source); err != nil || !st.IsDir() {
		return
	}
	target := filepath.Join(workDir, "node_modules")
	_ = os.Remove(target)
	_ = os.Symlink(source, target)
}
