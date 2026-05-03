package agent

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestEnforceProjectPathScope(t *testing.T) {
	cases := []struct {
		name        string
		projectType string
		path        string
		wantBlocked bool
	}{
		{"non-mobile allows root", models.ProjectTypeApp, "package.json", false},
		{"non-mobile allows nested", models.ProjectTypeApp, "src/foo.ts", false},
		{"ecommerce allows root", models.ProjectTypeEcommerce, "package.json", false},
		{"mobile allows mobile/", models.ProjectTypeMobileAppNative, "mobile/app/index.tsx", false},
		{"mobile allows backend/", models.ProjectTypeMobileAppNative, "backend/app/api/route.ts", false},
		{"mobile blocks root", models.ProjectTypeMobileAppNative, "package.json", true},
		{"mobile blocks other top-level", models.ProjectTypeMobileAppNative, "src/foo.ts", true},
		{"mobile blocks lookalike (no slash)", models.ProjectTypeMobileAppNative, "mobile.config.ts", true},
		{"mobile blocks lookalike (different prefix)", models.ProjectTypeMobileAppNative, "mobile-app/foo.ts", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rc := &RunContext{Project: &models.AppProject{ProjectType: tc.projectType}}
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
