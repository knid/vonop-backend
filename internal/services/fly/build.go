package fly

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildAndPush submits the source directory to Fly's managed builder, waits
// for the image to be built and pushed to registry.fly.io, and returns the
// resulting image reference.
//
// imageLabel is appended as the image tag — pass deployment.ID so the image
// can be tied back to the originating deploy.
//
// flyctl must be on PATH. The worker container ships it.
//
// A minimal fly.toml is written to the source dir before invoking flyctl
// because `flyctl deploy --build-only` requires either a fly.toml or an
// existing machine to derive build config from. The toml is removed
// afterwards so it doesn't accumulate in user repos.
func (s *Service) BuildAndPush(appName, sourceDir, imageLabel string) (imageRef string, buildLog string, err error) {
	tomlPath := filepath.Join(sourceDir, "fly.toml")
	tomlContent := fmt.Sprintf(`app = %q
primary_region = %q

[build]
  dockerfile = "Dockerfile"
`, appName, s.Region)
	if err := os.WriteFile(tomlPath, []byte(tomlContent), 0o644); err != nil {
		return "", "", fmt.Errorf("write fly.toml: %w", err)
	}
	defer os.Remove(tomlPath)

	cmd := exec.Command("flyctl", "deploy",
		"--app", appName,
		"--remote-only",
		"--build-only",
		"--push",
		"--image-label", imageLabel,
	)
	cmd.Dir = sourceDir
	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),        // flyctl reads ~/.fly config
		"PATH=" + os.Getenv("PATH"),        // flyctl needs to find docker / git
		"FLY_API_TOKEN=" + s.APIToken,
		"FLY_NO_UPDATE_CHECK=1",
	}
	out, err := cmd.CombinedOutput()
	buildLog = string(out)
	if err != nil {
		return "", buildLog, fmt.Errorf("flyctl: %w (output: %s)", err, truncate(buildLog, 2000))
	}
	imageRef, err = parseImageRef(buildLog)
	if err != nil {
		return "", buildLog, err
	}
	return imageRef, buildLog, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
