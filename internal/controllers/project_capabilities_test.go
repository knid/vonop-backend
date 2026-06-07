package controllers

import (
	"testing"

	"github.com/knid/wemob/internal/models"
)

func TestDefaultProjectCapabilities(t *testing.T) {
	cases := []struct {
		name            string
		projectType     string
		frontendRuntime string
		frontendHosting string
		backendEnabled  bool
		backendRuntime  string
	}{
		{
			name:            "new web app",
			projectType:     models.ProjectTypeApp,
			frontendRuntime: models.FrontendRuntimeViteReact,
			frontendHosting: models.FrontendHostingS3CDN,
			backendRuntime:  models.BackendRuntimeNone,
		},
		{
			name:            "ecommerce stays legacy",
			projectType:     models.ProjectTypeEcommerce,
			frontendRuntime: models.FrontendRuntimeNextJSLegacy,
			frontendHosting: models.FrontendHostingFly,
			backendRuntime:  models.BackendRuntimeNone,
		},
		{
			name:            "mobile stays expo",
			projectType:     models.ProjectTypeMobileAppNative,
			frontendRuntime: models.FrontendRuntimeExpoNative,
			frontendHosting: models.FrontendHostingCodemagic,
			backendRuntime:  models.BackendRuntimeNone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frontendRuntime, frontendHosting, backendEnabled, backendRuntime := defaultProjectCapabilities(tc.projectType)
			if frontendRuntime != tc.frontendRuntime {
				t.Errorf("frontendRuntime = %q, want %q", frontendRuntime, tc.frontendRuntime)
			}
			if frontendHosting != tc.frontendHosting {
				t.Errorf("frontendHosting = %q, want %q", frontendHosting, tc.frontendHosting)
			}
			if backendEnabled != tc.backendEnabled {
				t.Errorf("backendEnabled = %v, want %v", backendEnabled, tc.backendEnabled)
			}
			if backendRuntime != tc.backendRuntime {
				t.Errorf("backendRuntime = %q, want %q", backendRuntime, tc.backendRuntime)
			}
		})
	}
}

func TestFrontendRuntimeOverrideBackendOnlyAllowedWithoutEnv(t *testing.T) {
	t.Setenv("WEMOB_ALLOW_FRONTEND_RUNTIME_OVERRIDE", "")

	if !allowFrontendRuntimeOverride(models.ProjectTypeApp, models.FrontendRuntimeNone) {
		t.Fatal("expected frontend_runtime=none to be allowed for app projects without override env")
	}
	if allowFrontendRuntimeOverride(models.ProjectTypeApp, models.FrontendRuntimeViteReact) {
		t.Fatal("expected vite_react override to stay env-gated")
	}
	if allowFrontendRuntimeOverride(models.ProjectTypeEcommerce, models.FrontendRuntimeNone) {
		t.Fatal("expected frontend_runtime=none to be rejected for ecommerce")
	}
}

func TestFrontendCapabilitiesForRuntimeNone(t *testing.T) {
	frontendRuntime, frontendHosting := frontendCapabilitiesForRuntime(models.FrontendRuntimeNone)
	if frontendRuntime != models.FrontendRuntimeNone {
		t.Fatalf("frontendRuntime = %q, want %q", frontendRuntime, models.FrontendRuntimeNone)
	}
	if frontendHosting != models.FrontendHostingNone {
		t.Fatalf("frontendHosting = %q, want %q", frontendHosting, models.FrontendHostingNone)
	}
}

type fakeTemplateRepo struct {
	files map[string]bool
}

func (r fakeTemplateRepo) FileExists(path string) bool {
	return r.files[path]
}

func TestShouldPreserveLegacyCopiedTemplate(t *testing.T) {
	cases := []struct {
		name    string
		project *models.AppProject
		repo    fakeTemplateRepo
		want    bool
	}{
		{
			name: "root next app copy",
			project: &models.AppProject{
				ProjectType:     models.ProjectTypeApp,
				FrontendRuntime: models.FrontendRuntimeViteReact,
			},
			repo: fakeTemplateRepo{files: map[string]bool{
				"package.json": true,
			}},
			want: true,
		},
		{
			name: "vite frontend copy",
			project: &models.AppProject{
				ProjectType:     models.ProjectTypeApp,
				FrontendRuntime: models.FrontendRuntimeViteReact,
			},
			repo: fakeTemplateRepo{files: map[string]bool{
				"package.json":          true,
				"frontend/package.json": true,
			}},
		},
		{
			name: "ecommerce stays controlled by project type",
			project: &models.AppProject{
				ProjectType:     models.ProjectTypeEcommerce,
				FrontendRuntime: models.FrontendRuntimeNextJSLegacy,
			},
			repo: fakeTemplateRepo{files: map[string]bool{
				"package.json": true,
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldPreserveLegacyCopiedTemplate(tc.project, tc.repo)
			if got != tc.want {
				t.Fatalf("shouldPreserveLegacyCopiedTemplate() = %v, want %v", got, tc.want)
			}
		})
	}
}
