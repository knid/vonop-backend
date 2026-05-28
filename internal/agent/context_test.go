package agent

import (
	"strings"
	"testing"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

func TestBuildSystemMessages_MobileAppNativeUsesExpoPrompt(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "Expo Router") {
		t.Error("expected 'Expo Router' to appear in mobile_app_native system messages")
	}
	if !strings.Contains(joined, "NativeWind") {
		t.Error("expected 'NativeWind' to appear in mobile_app_native system messages")
	}
	if !strings.Contains(joined, "react-native-reusables") {
		t.Error("expected 'react-native-reusables' to appear in mobile_app_native system messages")
	}
}

func TestBuildSystemMessages_AppTypeUsesWebPrompt(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "Expo Router") {
		t.Error("expected web 'app' system messages NOT to include Expo content")
	}
}

func TestBuildSystemMessages_MobileNativeIncludesConfigAddendum(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	cfg := &models.MobileAppConfig{
		BridgeMethods:       []string{"camera.takePhoto", "biometric"},
		URLScheme:           "myapp",
		UniversalLinkDomain: "example.com",
		BiometricEnabled:    true,
		PushEnabled:         true,
		DarkModeEnabled:     true,
		OfflineEnabled:      false,
		Permissions:         []string{"camera", "location"},
	}
	deps := newDepsWithMobileConfig(t, project, cfg)
	msgs := BuildSystemMessages(project, deps, nil, false)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "camera.takePhoto") {
		t.Error("expected enabled bridge methods listed in addendum")
	}
	if !strings.Contains(joined, "myapp") {
		t.Error("expected URL scheme listed in addendum")
	}
	if !strings.Contains(joined, "example.com") {
		t.Error("expected universal link domain listed in addendum")
	}
	if !strings.Contains(joined, "Biometric enabled: true") {
		t.Error("expected biometric flag listed")
	}
	if !strings.Contains(joined, "Permissions:") || !strings.Contains(joined, "camera, location") {
		t.Error("expected permissions list")
	}
}

func TestBuildSystemMessages_MobileNativeNoConfigSkipsAddendum(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	deps := newDepsWithMobileConfig(t, project, nil) // explicitly no config
	msgs := BuildSystemMessages(project, deps, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "Current MobileAppConfig") {
		t.Error("expected no MobileAppConfig addendum when config is nil")
	}
	// The base prompt should still be present
	if !strings.Contains(joined, "Expo Router") {
		t.Error("expected base prompt to still be present")
	}
}

// fakeMobileConfigGetter is a test double for mobileConfigGetter.
type fakeMobileConfigGetter struct {
	cfg *models.MobileAppConfig
}

func (f *fakeMobileConfigGetter) GetMobileAppConfig(projectID string) (*models.MobileAppConfig, error) {
	return f.cfg, nil
}

// newDepsWithMobileConfig constructs RunDependencies with a fake mobileConfigGetter
// so T20 tests run without a real database connection.
func newDepsWithMobileConfig(t *testing.T, project *models.AppProject, cfg *models.MobileAppConfig) *RunDependencies {
	t.Helper()
	return &RunDependencies{
		mobileConfigFetcher: &fakeMobileConfigGetter{cfg: cfg},
	}
}

func TestBuildSystemMessages_NativeWithDatabaseUsesNativeAddendum(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{"has_database": true},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "DATABASE ADDENDUM (native + backend)") {
		t.Error("expected DatabaseAddendumNative on native project with has_database=true")
	}
	if strings.Contains(joined, "export const dynamic") {
		t.Error("native project must NOT receive the web-shaped DatabaseAddendum")
	}
}

func TestBuildSystemMessages_WebWithDatabaseUsesGenericAddendum(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Settings:    map[string]any{"has_database": true},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "DATABASE ADDENDUM (native + backend)") {
		t.Error("web project must NOT receive DatabaseAddendumNative")
	}
}

func TestBuildSystemMessages_LocalDatabaseAddendumOnFlag(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{"has_local_database": true},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "LOCAL DATABASE ADDENDUM") {
		t.Error("expected LocalDatabaseAddendum on native project with has_local_database=true")
	}
}

func TestBuildSystemMessages_LocalDatabaseAddendumNotInjectedWithoutFlag(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "LOCAL DATABASE ADDENDUM") {
		t.Error("LocalDatabaseAddendum must NOT appear without the flag")
	}
}

func TestBuildSystemMessages_BothAddendaForSyncedNative(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{"has_database": true, "has_local_database": true},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "DATABASE ADDENDUM (native + backend)") {
		t.Error("expected DatabaseAddendumNative")
	}
	if !strings.Contains(joined, "LOCAL DATABASE ADDENDUM") {
		t.Error("expected LocalDatabaseAddendum")
	}
}

// TestBuildSystemMessages_FileTreeNotInSystemBlocks locks in the prompt-cache
// fix: the rendered FILE TREE block must NOT appear in system messages. If it
// did, every write_file would mutate the system prefix and force a fresh
// prompt-cache write across the whole downstream conversation. The tree now
// rides with the user turn via BuildFileTreeUserPrefix instead.
//
// We assert by checking that the unique sentinel content of buildFileTreeBlock
// — the "=========" underline that follows the FILE TREE header — is absent.
// The string "FILE TREE" alone now appears in prompt copy, so it isn't a
// useful marker.
func TestBuildSystemMessages_FileTreeNotInSystemBlocks(t *testing.T) {
	const fileTreeSentinel = "FILE TREE\n========="
	for _, projectType := range []string{models.ProjectTypeApp, models.ProjectTypeMobileAppNative, "ecommerce"} {
		project := &models.AppProject{
			ProjectType: projectType,
			Settings:    map[string]any{},
		}

		msgs := BuildSystemMessages(project, nil, nil, false)
		joined := concatMessages(msgs)

		if strings.Contains(joined, fileTreeSentinel) {
			t.Errorf("project_type=%s: rendered FILE TREE block must not appear in system messages (cache thrash regression)", projectType)
		}
	}
}

// TestBuildFileTreeUserPrefix_RendersTree verifies the file tree helper
// returns a non-empty block when files exist, so the caller (run.go) can
// prepend it to the user turn.
func TestBuildFileTreeUserPrefix_RendersTree(t *testing.T) {
	fileMap := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "layout",
		"app/page.tsx":   "page",
	})

	prefix := BuildFileTreeUserPrefix(fileMap)
	if !strings.Contains(prefix, "FILE TREE") {
		t.Error("expected FILE TREE header in user-prefix output")
	}
	if !strings.Contains(prefix, "app/layout.tsx") || !strings.Contains(prefix, "app/page.tsx") {
		t.Error("expected file paths in user-prefix output")
	}
}

func TestBuildSystemMessages_FirstGenWebIncludesWorkOrders(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, true)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "Layout & Home First") {
		t.Error("expected first-gen web to include Layout & Home First work order")
	}
	if !strings.Contains(joined, "UI First, Wiring Second") {
		t.Error("expected first-gen web to include UI First work order")
	}
	if strings.Contains(joined, "EDITING DISCIPLINE") {
		t.Error("first-gen web must NOT include chat editing discipline")
	}
}

func TestBuildSystemMessages_ChatWebIncludesEditingDiscipline(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeApp,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "Layout & Home First") {
		t.Error("chat web must NOT include Layout & Home First work order")
	}
	if !strings.Contains(joined, "EDITING DISCIPLINE") {
		t.Error("expected chat web to include editing discipline")
	}
}

func TestBuildSystemMessages_FirstGenMobileIncludesWorkOrders(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, true)
	joined := concatMessages(msgs)

	if !strings.Contains(joined, "home screen first") {
		t.Error("expected first-gen mobile to include home screen first work order")
	}
	if strings.Contains(joined, "EDITING DISCIPLINE") {
		t.Error("first-gen mobile must NOT include chat editing discipline")
	}
}

func TestBuildSystemMessages_ChatMobileIncludesEditingDiscipline(t *testing.T) {
	project := &models.AppProject{
		ProjectType: models.ProjectTypeMobileAppNative,
		Settings:    map[string]any{},
	}
	msgs := BuildSystemMessages(project, nil, nil, false)
	joined := concatMessages(msgs)

	if strings.Contains(joined, "home screen first") {
		t.Error("chat mobile must NOT include home screen first work order")
	}
	if !strings.Contains(joined, "EDITING DISCIPLINE") {
		t.Error("expected chat mobile to include editing discipline")
	}
}

func concatMessages(msgs []utils.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
