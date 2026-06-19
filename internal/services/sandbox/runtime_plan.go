package sandbox

import "github.com/knid/wemob/internal/models"

const (
	ProjectRoot   = "/workspace/project"
	BackendRoot   = "/workspace/backend"
	ArtifactsRoot = "/workspace/artifacts"
	TmpRoot       = "/tmp"
)

func BuildRuntimePlan(project *models.AppProject) RuntimePlan {
	plan := RuntimePlan{
		AppID:           project.ID.String(),
		ProjectType:     project.ProjectType,
		FrontendRuntime: project.FrontendRuntime,
		BackendEnabled:  project.BackendEnabled,
		BackendRuntime:  project.BackendRuntime,
		Roots: []Root{
			{Name: RootProject, Path: ProjectRoot},
			{Name: RootBackend, Path: BackendRoot},
			{Name: RootArtifacts, Path: ArtifactsRoot},
			{Name: RootTmp, Path: TmpRoot},
		},
		Previews: map[PreviewKind]int{},
	}

	switch {
	case project.ProjectType == models.ProjectTypeMobileAppNative:
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 8081
		plan.Previews[PreviewExpo] = 19000
		plan.Processes = append(plan.Processes,
			ProcessSpec{
				Name:        ProcessExpoWeb,
				Command:     "export NODE_OPTIONS='--max-old-space-size=3072' && CI=1 npx expo start --web --clear --port 8081",
				Cwd:         ProjectRoot,
				Port:        8081,
				Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:8081"},
				LogPath:     "/tmp/wemob-expo-web.log",
				AutoRestart: true,
			},
			ProcessSpec{
				Name:        ProcessExpoGo,
				Command:     "export EXPO_GO_MODE=1 && export NODE_OPTIONS='--max-old-space-size=3072' && CI=1 npx expo start --clear --port 19000",
				Cwd:         ProjectRoot,
				Port:        19000,
				Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:19000"},
				LogPath:     "/tmp/wemob-expo-go.log",
				AutoRestart: true,
			},
		)
	case project.FrontendRuntime == models.FrontendRuntimeNone && project.BackendEnabled:
		plan.DefaultPreview = PreviewBackend
		plan.Previews[PreviewBackend] = 3001
		plan.Processes = append(plan.Processes, backendProcess())
	case project.UsesSplitWebRuntime():
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 3000
		if project.BackendEnabled && project.BackendRuntime == models.BackendRuntimeNextJSAPI {
			plan.Previews[PreviewBackend] = 3001
			plan.Processes = append(plan.Processes, backendProcess())
		}
		plan.Processes = append(plan.Processes, ProcessSpec{
			Name:        ProcessFrontend,
			Command:     "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- --config vite.config.preview.ts --host 0.0.0.0 --port 3000",
			Cwd:         ProjectRoot,
			Port:        3000,
			Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:3000"},
			LogPath:     "/tmp/wemob-frontend.log",
			AutoRestart: true,
		})
	default:
		plan.DefaultPreview = PreviewApp
		plan.Previews[PreviewApp] = 3000
		plan.Processes = append(plan.Processes, ProcessSpec{
			Name:        ProcessFrontend,
			Command:     "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- --hostname 0.0.0.0 --port 3000",
			Cwd:         ProjectRoot,
			Port:        3000,
			Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:3000"},
			LogPath:     "/tmp/wemob-frontend.log",
			AutoRestart: true,
		})
	}

	plan.Previews[PreviewArtifacts] = 7000
	plan.Processes = append(plan.Processes, ProcessSpec{
		Name:        ProcessArtifacts,
		Command:     "python3 -m http.server 7000",
		Cwd:         ArtifactsRoot,
		Port:        7000,
		Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:7000"},
		LogPath:     "/tmp/wemob-artifacts.log",
		AutoRestart: false,
	})
	return plan
}

func backendProcess() ProcessSpec {
	return ProcessSpec{
		Name:        ProcessBackend,
		Command:     "export NODE_OPTIONS='--max-old-space-size=3072' && npm run dev -- -p 3001",
		Cwd:         BackendRoot,
		Port:        3001,
		Probe:       ReadinessProbe{Type: "http", URL: "http://localhost:3001"},
		LogPath:     "/tmp/wemob-backend.log",
		AutoRestart: true,
	}
}
