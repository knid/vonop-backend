package agent

import (
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
)

const yachtSaaSPrompt = `Create a Yacht Logbook SaaS platform.
Admin/Yacht Owner can manage yachts. Captain can only see assigned yacht.
Include weekly status entry form, log history, smart alerts, and mobile-first dashboard.`

const testDesignBrief = `# Design Brief

## Register
Product register.

## Design Direction
Restrained marine operations console with precise hierarchy.

## Typography
One neutral sans family with tabular numerals for operational data.

## Color
Light-first neutral palette with deep green primary and rust alert accent.

## Layout
Dashboard-first app shell with role-specific work zones.

## Components
Use Wemob UI primitives for buttons, cards, forms, empty states, and metrics.
	`

func legacyWebProject() *models.AppProject {
	return &models.AppProject{
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeNextJSLegacy,
	}
}

func splitWebProject() *models.AppProject {
	return &models.AppProject{
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
	}
}

func TestValidateGeneratedAppRejectsMarketingBrochureForSaaSRequest(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "export default function Layout({children}) { return children }",
		"app/page.tsx":   "export default function Page(){ return <main>Hero Testimonials CTA Yacht showcase</main> }",
		"DESIGN.md":      testDesignBrief,
	})

	err := validateGeneratedApp(files, NewBuildState(yachtSaaSPrompt, true), yachtSaaSPrompt, true, legacyWebProject())
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	for _, want := range []string{"captain interface", "data entry form", "log history", "smart alerts"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in validation error, got: %s", want, msg)
		}
	}
}

func TestValidateGeneratedAppAllowsOperationalSaaSShape(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "export default function Layout({children}) { return children }",
		"app/page.tsx": `export default function Page(){
			return <main>
				<section>Admin fleet dashboard for yacht owners</section>
				<section>Captain assigned yacht interface and role access</section>
				<form><input /><textarea /><button>Submit weekly entry</button></form>
				<section>Log history with past chronological entries</section>
				<section>Smart alert threshold battery service warning</section>
			</main>
		}`,
		"DESIGN.md": testDesignBrief,
	})

	if err := validateGeneratedApp(files, NewBuildState(yachtSaaSPrompt, true), yachtSaaSPrompt, true, legacyWebProject()); err != nil {
		t.Fatalf("expected validation success, got: %v", err)
	}
}

func TestValidateGeneratedAppAllowsMissingDesignBrief(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "export default function Layout({children}) { return children }",
		"app/page.tsx":   "export default function Page(){ return <main>Product surface</main> }",
	})

	err := validateGeneratedApp(files, NewBuildState("build a web app", true), "build a web app", true, legacyWebProject())
	if err != nil {
		t.Fatalf("expected missing DESIGN.md to be recoverable, got: %v", err)
	}
}

func TestValidateGeneratedAppAllowsMobileNativeShape(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"mobile/package.json":           `{"scripts":{"typecheck":"tsc --noEmit"}}`,
		"mobile/app/_layout.tsx":        "export default function Layout() { return null }",
		"mobile/app/(app)/index.tsx":    "export default function Home() { return null }",
		"mobile/app/(auth)/sign-in.tsx": "export default function SignIn() { return null }",
	})

	if err := validateGeneratedApp(files, NewBuildState("mobile app", true), "mobile app", true, &models.AppProject{ProjectType: models.ProjectTypeMobileAppNative}); err != nil {
		t.Fatalf("expected mobile validation success, got: %v", err)
	}
}

func TestValidateGeneratedAppRejectsMobileNativeMissingHome(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"mobile/package.json":    `{"scripts":{"typecheck":"tsc --noEmit"}}`,
		"mobile/app/_layout.tsx": "export default function Layout() { return null }",
	})

	err := validateGeneratedApp(files, NewBuildState("mobile app", true), "mobile app", true, &models.AppProject{ProjectType: models.ProjectTypeMobileAppNative})
	if err == nil {
		t.Fatal("expected mobile validation error")
	}
	if !strings.Contains(err.Error(), "mobile/app/(app)/index.tsx") {
		t.Fatalf("expected missing mobile home path, got: %v", err)
	}
}

func TestValidateGeneratedAppRejectsImagePlaceholders(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/page.tsx": "const hero = '__IMG_HERO__'",
	})

	err := validateGeneratedApp(files, nil, "change image", false, legacyWebProject())
	if err == nil {
		t.Fatal("expected placeholder validation error")
	}
	if !strings.Contains(err.Error(), "app/page.tsx") {
		t.Fatalf("expected path in error, got: %v", err)
	}
}

func TestValidateGeneratedAppAllowsSplitWebShape(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"frontend/package.json": `{"scripts":{"build":"vite build"}}`,
		"frontend/src/App.tsx": `export default function App(){
			return <main>
				<section>Admin fleet dashboard for yacht owners</section>
				<section>Captain assigned yacht interface and role access</section>
				<form><input /><textarea /><button>Submit weekly entry</button></form>
				<section>Log history with past chronological entries</section>
				<section>Smart alert threshold battery service warning</section>
			</main>
		}`,
		"DESIGN.md": testDesignBrief,
	})

	if err := validateGeneratedApp(files, NewBuildState(yachtSaaSPrompt, true), yachtSaaSPrompt, true, splitWebProject()); err != nil {
		t.Fatalf("expected split web validation success, got: %v", err)
	}
}

func TestValidateGeneratedAppRejectsSplitWebMissingFrontend(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"DESIGN.md": testDesignBrief,
	})

	err := validateGeneratedApp(files, NewBuildState("build app", true), "build app", true, splitWebProject())
	if err == nil {
		t.Fatal("expected split web validation error")
	}
	if !strings.Contains(err.Error(), "frontend/package.json") {
		t.Fatalf("expected missing frontend/package.json, got: %v", err)
	}
}
