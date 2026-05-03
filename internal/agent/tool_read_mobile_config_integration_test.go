//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
)

// TestReadMobileConfigTool_ReturnsEnabledMethods exercises the tool against a
// real MobileAppConfig row and verifies the bridge methods, URL scheme, and
// biometric flag round-trip into the JSON output the agent receives.
func TestReadMobileConfigTool_ReturnsEnabledMethods(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	ctx := context.Background()

	project := &models.AppProject{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		OrganizationID: uuid.New(),
		Name:           "test",
		Slug:           "test-" + uuid.NewString(),
		Prompt:         "n/a",
		ProjectType:    models.ProjectTypeMobileAppNative,
		Status:         "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	cfg := &models.MobileAppConfig{
		ProjectID:             project.ID,
		BridgeMethods:         []string{"camera", "biometric"},
		URLScheme:             "wemobapp",
		UniversalLinkDomain:   "example.com",
		BiometricEnabled:      true,
		MinBuildNumberIOS:     42,
		MinBuildNumberAndroid: 17,
		MinVersionMessage:     "Critical fix — please update.",
	}
	if _, err := db.DB.NewInsert().Model(cfg).Exec(ctx); err != nil {
		t.Fatalf("insert mobile config: %v", err)
	}

	tool := &ReadMobileConfigTool{}
	rc := &RunContext{
		Deps:    &RunDependencies{DB: db},
		Project: project,
	}

	out, isErr, err := tool.Execute(ctx, rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool execute: %v", err)
	}
	if isErr {
		t.Fatalf("expected isError=false, got true; out=%s", out)
	}

	var got readMobileConfigOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v; out=%s", err, out)
	}
	if got.URLScheme != "wemobapp" {
		t.Errorf("URLScheme = %q, want wemobapp", got.URLScheme)
	}
	if got.UniversalLinkDomain != "example.com" {
		t.Errorf("UniversalLinkDomain = %q, want example.com", got.UniversalLinkDomain)
	}
	if !got.BiometricEnabled {
		t.Errorf("BiometricEnabled = false, want true")
	}
	if len(got.EnabledNativeFeatures) != 2 || got.EnabledNativeFeatures[0] != "camera" || got.EnabledNativeFeatures[1] != "biometric" {
		t.Errorf("EnabledNativeFeatures = %v, want [camera biometric]", got.EnabledNativeFeatures)
	}
	if got.MinBuildNumberIOS != 42 {
		t.Errorf("MinBuildNumberIOS = %d, want 42", got.MinBuildNumberIOS)
	}
	if got.MinBuildNumberAndroid != 17 {
		t.Errorf("MinBuildNumberAndroid = %d, want 17", got.MinBuildNumberAndroid)
	}
	if got.MinVersionMessage != "Critical fix — please update." {
		t.Errorf("MinVersionMessage = %q, want %q", got.MinVersionMessage, "Critical fix — please update.")
	}
}

// TestReadMobileConfigTool_HandlesMissingConfig verifies the tool gracefully
// returns an empty-features payload (rather than an error or isError=true)
// when no MobileAppConfig row exists for the project — this is the new-project
// case the agent must be able to recover from.
func TestReadMobileConfigTool_HandlesMissingConfig(t *testing.T) {
	db := database.NewTestDB(t,
		(*models.AppProject)(nil),
		(*models.MobileAppConfig)(nil),
	)
	ctx := context.Background()

	project := &models.AppProject{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		OrganizationID: uuid.New(),
		Name:           "test",
		Slug:           "test-" + uuid.NewString(),
		Prompt:         "n/a",
		ProjectType:    models.ProjectTypeMobileAppNative,
		Status:         "draft",
	}
	if _, err := db.DB.NewInsert().Model(project).Exec(ctx); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	tool := &ReadMobileConfigTool{}
	rc := &RunContext{
		Deps:    &RunDependencies{DB: db},
		Project: project,
	}

	out, isErr, err := tool.Execute(ctx, rc, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tool execute: %v", err)
	}
	if isErr {
		t.Errorf("expected isError=false for missing config, got true; out=%s", out)
	}

	var got readMobileConfigOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal output: %v; out=%s", err, out)
	}
	if len(got.EnabledNativeFeatures) != 0 {
		t.Errorf("expected empty EnabledNativeFeatures for missing config, got %v", got.EnabledNativeFeatures)
	}
}
