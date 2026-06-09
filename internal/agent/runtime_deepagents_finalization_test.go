package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
)

func TestDeepAgentsFinalization_ReloadsFileMapFromDiskAfterPythonWrites(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := gitRepo(t, repoPath)

	if err := writeContent(filepath.Join(repoPath, "src/main.tsx"), []byte("before")); err != nil {
		t.Fatalf("write before: %v", err)
	}
	runCmd(t, repoPath, "add", "src/main.tsx")
	runCmd(t, repoPath, "commit", "-m", "baseline")

	fileMap, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	if _, ok := fileMap.Get("src/main.tsx"); !ok {
		t.Fatalf("expected src/main.tsx in initial fileMap")
	}

	if err := writeContent(filepath.Join(repoPath, "src/app.tsx"), []byte("python wrote this")); err != nil {
		t.Fatalf("simulate python write: %v", err)
	}
	if err := writeContent(filepath.Join(repoPath, "src/main.tsx"), []byte("python updated this")); err != nil {
		t.Fatalf("simulate python update: %v", err)
	}

	reloaded, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		t.Fatalf("reload after python writes: %v", err)
	}

	if content, ok := reloaded.Get("src/app.tsx"); !ok {
		t.Fatalf("expected new file src/app.tsx after python write")
	} else if content != "python wrote this" {
		t.Fatalf("expected 'python wrote this', got %q", content)
	}

	if content, ok := reloaded.Get("src/main.tsx"); !ok {
		t.Fatalf("expected updated src/main.tsx")
	} else if content != "python updated this" {
		t.Fatalf("expected 'python updated this', got %q", content)
	}
}

func TestDeepAgentsFinalization_DeletionInFileMapAfterDiskRemoval(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := gitRepo(t, repoPath)

	if err := writeContent(filepath.Join(repoPath, "to_delete.txt"), []byte("temp")); err != nil {
		t.Fatalf("write: %v", err)
	}
	runCmd(t, repoPath, "add", "to_delete.txt")
	runCmd(t, repoPath, "commit", "-m", "baseline")

	first, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	if _, ok := first.Get("to_delete.txt"); !ok {
		t.Fatalf("expected to_delete.txt in initial fileMap")
	}

	// Python deletes via its filesystem tool. On the filesystem, this
	// is just an os.Remove of the working-tree copy. The file should
	// disappear from ReloadFileMapFromRepo because Repo.ReadAllFiles
	// only reports files that exist on disk.
	if err := os.Remove(filepath.Join(repoPath, "to_delete.txt")); err != nil {
		t.Fatalf("simulate python delete: %v", err)
	}

	reloaded, err := ReloadFileMapFromRepo(repo)
	if err != nil {
		t.Fatalf("reload after delete: %v", err)
	}

	if _, ok := reloaded.Get("to_delete.txt"); ok {
		t.Fatalf("expected to_delete.txt to be absent after disk removal")
	}
}

func TestDeepAgentsFinalization_ValidationSucceedsForValidOutput(t *testing.T) {
	t.Parallel()

	fileMap := NewSafeFileMap(map[string]string{
		"app/layout.tsx": `export default function Layout({ children }: { children: React.ReactNode }) { return <html><body>{children}</body></html>; }`,
		"app/page.tsx":   `export default function Home() { return <div>hello</div>; }`,
		"DESIGN.md":      "# Design\n\n## Register\n\nPremium SaaS landing page.\n\n## Design Direction\n\nModern and clean.\n\n## Typography\n\nInter for body, Geist for headings.\n\n## Color\n\nPrimary: #000, Accent: #fff.\n\n## Layout\n\nSingle column hero with feature grid.\n\n## Components\n\nHero, Features, CTA.",
	})

	err := ValidateAgentOutput(nil, fileMap, nil, "create a landing page", true, &models.AppProject{
		ProjectType: "app",
	}, nil)
	if err != nil {
		t.Fatalf("validation should pass for valid output, got: %v", err)
	}
}

func TestDeepAgentsFinalization_ValidationFailsForMissingRequiredFiles(t *testing.T) {
	t.Parallel()

	fileMap := NewSafeFileMap(map[string]string{})

	err := ValidateAgentOutput(nil, fileMap, nil, "create a mobile app", true, &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
	}, nil)
	if err == nil {
		t.Fatalf("expected validation error for missing mobile/package.json")
	}
}

func TestDeepAgentsFinalization_FirstGenChecksForRequiredFiles(t *testing.T) {
	t.Parallel()

	t.Run("split-web-frontend", func(t *testing.T) {
		t.Parallel()
		fileMap := NewSafeFileMap(map[string]string{
			"frontend/package.json": `{"name":"app"}`,
			"frontend/src/App.tsx":  "export default function App() {}",
			"DESIGN.md":             "# Design\n\n## Register\n\nPremium SaaS.\n\n## Design Direction\n\nMinimal.\n\n## Typography\n\nInter.\n\n## Color\n\nSlate + blue.\n\n## Layout\n\nHero + grid.\n\n## Components\n\nHeader, CTA, Footer.",
		})
		err := ValidateAgentOutput(nil, fileMap, nil, "build app", true, &models.AppProject{
			ProjectType:     "app",
			FrontendRuntime: models.FrontendRuntimeViteReact,
		}, nil)
		if err != nil {
			t.Fatalf("validation should pass for valid split-web output, got: %v", err)
		}
	})

	t.Run("split-web-missing-app-tsx", func(t *testing.T) {
		t.Parallel()
		fileMap := NewSafeFileMap(map[string]string{
			"frontend/package.json": `{"name":"app"}`,
		})
		err := ValidateAgentOutput(nil, fileMap, nil, "build app", true, &models.AppProject{
			ProjectType:     "app",
			FrontendRuntime: models.FrontendRuntimeViteReact,
		}, nil)
		if err == nil {
			t.Fatalf("expected validation error for missing frontend/src/App.tsx")
		}
	})
}

// TestDeepAgentsFinalization_FirstGenMobileAppValidatesRequiredFiles ensures
// first-generation mobile apps pass validation when all required files exist.
func TestDeepAgentsFinalization_FirstGenMobileAppValidatesRequiredFiles(t *testing.T) {
	t.Parallel()

	fileMap := NewSafeFileMap(map[string]string{
		"mobile/package.json":         `{"name":"mobile"}`,
		"mobile/app/_layout.tsx":      `export default function Root() { return null; }`,
		"mobile/app/(app)/index.tsx":  `export default function Home() { return null; }`,
	})

	err := ValidateAgentOutput(nil, fileMap, nil, "create a mobile app", true, &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
	}, nil)
	if err != nil {
		t.Fatalf("validation should pass for valid mobile output, got: %v", err)
	}
}

func TestDeepAgentsFinalization_ValidationSkipsForChatEdits(t *testing.T) {
	t.Parallel()

	fileMap := NewSafeFileMap(map[string]string{
		"src/hero.tsx": "export function Hero() { return <div>updated</div>; }",
	})

	err := ValidateAgentOutput(nil, fileMap, nil, "update hero text", false, &models.AppProject{
		ProjectType: "app",
	}, nil)
	if err != nil {
		t.Fatalf("chat edit validation should pass, got: %v", err)
	}
}

func gitRepo(t *testing.T, dir string) *gitrepo.Repo {
	t.Helper()

	runCmd(t, dir, "init", "-q")
	runCmd(t, dir, "config", "user.email", "test@localhost")
	runCmd(t, dir, "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.pyc\n__pycache__\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	runCmd(t, dir, "add", ".gitignore")
	runCmd(t, dir, "commit", "-m", "init")

	_ = uuid.New()
	return &gitrepo.Repo{Path: dir}
}
