package agent

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestEnforceProjectPathScope(t *testing.T) {
	cases := []struct {
		name            string
		projectType     string
		frontendRuntime string
		path            string
		wantBlocked     bool
	}{
		{"legacy web allows root", models.ProjectTypeApp, "", "package.json", false},
		{"legacy web allows nested", models.ProjectTypeApp, "", "src/foo.ts", false},
		{"ecommerce allows root", models.ProjectTypeEcommerce, "", "package.json", false},
		{"split web allows frontend/", models.ProjectTypeApp, models.FrontendRuntimeViteReact, "frontend/src/App.tsx", false},
		{"split web allows backend/", models.ProjectTypeApp, models.FrontendRuntimeViteReact, "backend/app/api/route.ts", false},
		{"split web allows design metadata", models.ProjectTypeApp, models.FrontendRuntimeViteReact, "DESIGN.md", false},
		{"split web blocks root", models.ProjectTypeApp, models.FrontendRuntimeViteReact, "package.json", true},
		{"split web blocks src root", models.ProjectTypeApp, models.FrontendRuntimeViteReact, "src/foo.ts", true},
		{"backend-only allows backend", models.ProjectTypeApp, models.FrontendRuntimeNone, "backend/app/api/route.ts", false},
		{"backend-only blocks frontend", models.ProjectTypeApp, models.FrontendRuntimeNone, "frontend/src/App.tsx", true},
		{"mobile allows mobile/", models.ProjectTypeMobileAppNative, "", "mobile/app/index.tsx", false},
		{"mobile allows backend/", models.ProjectTypeMobileAppNative, "", "backend/app/api/route.ts", false},
		{"mobile blocks root", models.ProjectTypeMobileAppNative, "", "package.json", true},
		{"mobile blocks other top-level", models.ProjectTypeMobileAppNative, "", "src/foo.ts", true},
		{"mobile blocks lookalike (no slash)", models.ProjectTypeMobileAppNative, "", "mobile.config.ts", true},
		{"mobile blocks lookalike (different prefix)", models.ProjectTypeMobileAppNative, "", "mobile-app/foo.ts", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := &RunContext{Project: &models.AppProject{ProjectType: tc.projectType, FrontendRuntime: tc.frontendRuntime}}
			msg := enforceProjectPathScope(rc, tc.path)
			blocked := msg != ""
			if blocked != tc.wantBlocked {
				t.Errorf("path=%q type=%q: blocked=%v want=%v (msg=%q)",
					tc.path, tc.projectType, blocked, tc.wantBlocked, msg)
			}
		})
	}
}

func TestEnforceProjectPathScope_NilSafe(t *testing.T) {
	if msg := enforceProjectPathScope(nil, "anything"); msg != "" {
		t.Errorf("nil rc should not block, got: %q", msg)
	}
	rc := &RunContext{Project: nil}
	if msg := enforceProjectPathScope(rc, "anything"); msg != "" {
		t.Errorf("nil project should not block, got: %q", msg)
	}
}
