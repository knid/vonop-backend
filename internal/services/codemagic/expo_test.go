package codemagic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestTriggerExpoAndroidBuild_PassesAllExpectedEnvVars(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "cm-build-123"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "test-token", "expo-app-id")

	params := ExpoAndroidBuildParams{
		ProjectID:           uuid.New(),
		BuildID:             uuid.New(),
		TaskID:              uuid.New(),
		AppTitle:            "Test App",
		AppSlug:             "testapp",
		BundleID:            "com.wemob.testapp",
		PackageName:         "com.wemob.testapp",
		BuildNumber:         42,
		MarketingVersion:    "1.2.0",
		SplashColor:         "#000000",
		FirstBuild:          true,
		IconKey:             "icon-uuid",
		EASProjectUUID:      "stable-uuid",
		ExpoPublicAPIURL:    "https://testapp.wemob.app",
		ExpoPublicProjectID: "11111111-1111-1111-1111-111111111111",
	}

	buildID, err := s.TriggerExpoAndroidBuild(context.Background(), params)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if buildID != "cm-build-123" {
		t.Errorf("got buildID %q want cm-build-123", buildID)
	}

	if captured["workflowId"] != WorkflowExpoAndroidBuild {
		t.Errorf("workflowId: got %v want %v", captured["workflowId"], WorkflowExpoAndroidBuild)
	}

	envBlock, ok := captured["environment"].(map[string]any)
	if !ok {
		t.Fatal("missing environment block")
	}
	vars, ok := envBlock["variables"].(map[string]any)
	if !ok {
		t.Fatal("missing environment.variables")
	}

	expectKeys := []string{
		"CM_PROJECT_ID", "CM_BUILD_ID", "CM_TASK_ID",
		"CM_APP_TITLE", "CM_APP_SLUG",
		"CM_BUNDLE_ID", "CM_PACKAGE_NAME",
		"CM_BUILD_NUMBER", "CM_MARKETING_VERSION",
		"CM_SPLASH_COLOR", "CM_FIRST_BUILD",
		"CM_ICON_KEY", "CM_EAS_PROJECT_UUID",
		"EXPO_PUBLIC_API_URL", "EXPO_PUBLIC_PROJECT_ID",
	}
	for _, k := range expectKeys {
		if _, present := vars[k]; !present {
			t.Errorf("expected env var %q not set", k)
		}
	}
	if vars["CM_FIRST_BUILD"].(string) != "true" {
		t.Errorf("CM_FIRST_BUILD: got %v want true", vars["CM_FIRST_BUILD"])
	}
	if vars["CM_BUILD_NUMBER"].(string) != "42" {
		t.Errorf("CM_BUILD_NUMBER: got %v want 42", vars["CM_BUILD_NUMBER"])
	}
	if vars["EXPO_PUBLIC_API_URL"].(string) != "https://testapp.wemob.app" {
		t.Errorf("EXPO_PUBLIC_API_URL: got %v want https://testapp.wemob.app", vars["EXPO_PUBLIC_API_URL"])
	}
	if vars["EXPO_PUBLIC_PROJECT_ID"].(string) != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("EXPO_PUBLIC_PROJECT_ID: got %v want UUID", vars["EXPO_PUBLIC_PROJECT_ID"])
	}
}

func TestTriggerExpoAndroidBuild_RetriesOn5xx(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "cm-build-after-retry"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "t", "a")
	_, err := s.TriggerExpoAndroidBuild(context.Background(), ExpoAndroidBuildParams{
		ProjectID: uuid.New(), BuildID: uuid.New(), TaskID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts (2 retries), got %d", attempts)
	}
}

// newServiceForTest constructs a Service pointing at a test HTTP server.
// Uses unexported fields (accessible since we're in the same package).
func newServiceForTest(t *testing.T, apiBase, token, appID string) *Service {
	t.Helper()
	return &Service{
		APIToken:   token,
		AppID:      appID,
		Branch:     "main",
		apiBaseURL: apiBase,
		client:     http.DefaultClient,
	}
}
