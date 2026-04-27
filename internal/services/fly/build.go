package fly

import (
	"fmt"
	"os"
	"os/exec"
)

// BuildAndPush submits the source directory to Fly's managed builder, waits
// for the image to be built and pushed to registry.fly.io, and returns the
// resulting image reference.
//
// imageLabel is appended as the image tag — pass deployment.ID so the image
// can be tied back to the originating deploy.
//
// flyctl must be on PATH. The worker container ships it.
func (s *Service) BuildAndPush(appName, sourceDir, imageLabel string) (imageRef string, buildLog string, err error) {
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
