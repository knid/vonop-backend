package fly

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildAndPush submits the source directory to Fly's managed builder, waits
// for the image to be built and pushed to registry.fly.io, and returns the
// resulting image reference (the actual tag flyctl chose; we don't try to
// dictate it because `--image-label` historically produces tag mismatches).
//
// envVars: every entry is forwarded to the builder as --build-arg, matching
// Coolify's prior behavior. Docker silently ignores --build-arg values whose
// keys aren't declared as `ARG` in the Dockerfile, so in practice only the
// NEXT_PUBLIC_* vars (which the template Dockerfile declares) end up affecting
// the build. Passing all of them anyway means future Dockerfile additions
// (e.g., an integration that wants DATABASE_URL at build time for SSG) work
// without a parallel code change here.
//
// Why NEXT_PUBLIC_* must be at build time, not runtime: Next.js inlines
// NEXT_PUBLIC_* references at `next build` via webpack's DefinePlugin across
// both client and server bundles. Fly secrets land in the process env at
// runtime, which is too late — the bundle has `undefined` baked in.
//
// imageLabel is included in the build log for traceability — it's no longer
// passed to flyctl.
//
// A minimal fly.toml is written to the source dir before invoking flyctl
// because `flyctl deploy --build-only` requires either a fly.toml or an
// existing machine to derive build config from. The toml is removed
// afterwards so it doesn't accumulate in user repos.
func (s *Service) BuildAndPush(appName, sourceDir, imageLabel string, envVars map[string]string) (imageRef string, buildLog string, err error) {
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

	args := []string{"deploy",
		"--app", appName,
		"--remote-only",
		"--build-only",
		"--push",
	}
	for k, v := range envVars {
		args = append(args, "--build-arg", k+"="+v)
	}
	cmd := exec.Command("flyctl", args...)
	cmd.Dir = sourceDir
	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
		"FLY_API_TOKEN=" + s.APIToken,
		"FLY_NO_UPDATE_CHECK=1",
	}
	out, err := cmd.CombinedOutput()
	buildLog = string(out)
	if err != nil {
		slog.Warn("fly_build_failed", "app", appName, "label", imageLabel, "log_tail", truncate(buildLog, 2000))
		return "", buildLog, fmt.Errorf("flyctl: %w (output: %s)", err, truncate(buildLog, 2000))
	}
	imageRef, err = parseImageRef(buildLog)
	if err != nil {
		slog.Error("fly_build_no_image_ref", "app", appName, "label", imageLabel, "log_tail", truncate(buildLog, 2000))
		return "", buildLog, err
	}
	slog.Info("fly_build_succeeded", "app", appName, "label", imageLabel, "image_ref", imageRef, "build_args", len(envVars))
	return imageRef, buildLog, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
