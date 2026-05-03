package codemagic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestTriggerExpoAndroidDeploy_PassesAllEnvVars(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "cm-deploy-android-1"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "test-token", "expo-app-id")
	params := ExpoAndroidDeployParams{
		ProjectID:             uuid.New(),
		BuildID:               uuid.New(),
		TaskID:                uuid.New(),
		PackageName:           "com.wemob.testapp",
		BuildNumber:           7,
		ServiceAccountJSONB64: "BASE64SAJSON==",
		Track:                 "alpha",
		RolloutFraction:       0.0,
		SubmitAsDraft:         true,
		AABS3Key:              "aabs/some-build.aab",
	}

	buildID, err := s.TriggerExpoAndroidDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if buildID != "cm-deploy-android-1" {
		t.Errorf("buildID: got %q want cm-deploy-android-1", buildID)
	}
	if captured["workflowId"] != WorkflowExpoAndroidDeploy {
		t.Errorf("workflowId: got %v want %v", captured["workflowId"], WorkflowExpoAndroidDeploy)
	}
	envBlock := captured["environment"].(map[string]any)
	vars := envBlock["variables"].(map[string]any)
	expect := map[string]string{
		"CM_PACKAGE_NAME":            "com.wemob.testapp",
		"CM_BUILD_NUMBER":            "7",
		"CM_GOOGLE_PLAY_SA_JSON_B64": "BASE64SAJSON==",
		"CM_PLAY_TRACK":              "alpha",
		"CM_SUBMIT_AS_DRAFT":         "true",
		"CM_AAB_S3_KEY":              "aabs/some-build.aab",
	}
	for k, want := range expect {
		got, ok := vars[k].(string)
		if !ok {
			t.Errorf("var %q missing or not string", k)
			continue
		}
		if got != want {
			t.Errorf("var %q: got %q want %q", k, got, want)
		}
	}
}

func TestTriggerExpoAndroidDeploy_DefaultTrackIsInternal(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "x"})
	}))
	defer server.Close()
	s := newServiceForTest(t, server.URL, "t", "a")
	_, err := s.TriggerExpoAndroidDeploy(context.Background(), ExpoAndroidDeployParams{
		ProjectID: uuid.New(), BuildID: uuid.New(), TaskID: uuid.New(),
		PackageName: "com.x.y", BuildNumber: 1, AABS3Key: "k",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	vars := captured["environment"].(map[string]any)["variables"].(map[string]any)
	if vars["CM_PLAY_TRACK"].(string) != "internal" {
		t.Errorf("default track: got %v want internal", vars["CM_PLAY_TRACK"])
	}
}

func TestTriggerExpoAndroidDeploy_RolloutFractionFormatted(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "x"})
	}))
	defer server.Close()
	s := newServiceForTest(t, server.URL, "t", "a")
	_, err := s.TriggerExpoAndroidDeploy(context.Background(), ExpoAndroidDeployParams{
		ProjectID: uuid.New(), BuildID: uuid.New(), TaskID: uuid.New(),
		Track: "production", RolloutFraction: 0.05, AABS3Key: "k",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	vars := captured["environment"].(map[string]any)["variables"].(map[string]any)
	if vars["CM_PLAY_ROLLOUT_FRACTION"].(string) != "0.0500" {
		t.Errorf("rollout fraction: got %v want 0.0500", vars["CM_PLAY_ROLLOUT_FRACTION"])
	}
}

func TestTriggerExpoIOSDeploy_PassesAllEnvVars(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"build_id": "cm-deploy-ios-1"})
	}))
	defer server.Close()

	s := newServiceForTest(t, server.URL, "t", "a")
	params := ExpoIOSDeployParams{
		ProjectID:          uuid.New(),
		BuildID:            uuid.New(),
		TaskID:             uuid.New(),
		BundleID:           "com.wemob.testapp",
		BuildNumber:        9,
		ASCAPIKeyB64:       "BASE64P8==",
		ASCKeyID:           "ABCD123",
		ASCIssuerID:        "11111111-2222-3333-4444-555555555555",
		SubmitToTestFlight: true,
		SubmitToAppStore:   false,
		PhasedRelease:      true,
		SubmitAsDraft:      false,
		IPAS3Key:           "ipas/some-build.ipa",
	}
	_, err := s.TriggerExpoIOSDeploy(context.Background(), params)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if captured["workflowId"] != WorkflowExpoIOSDeploy {
		t.Errorf("workflowId: got %v want %v", captured["workflowId"], WorkflowExpoIOSDeploy)
	}
	vars := captured["environment"].(map[string]any)["variables"].(map[string]any)
	expect := map[string]string{
		"CM_BUNDLE_ID":            "com.wemob.testapp",
		"CM_BUILD_NUMBER":         "9",
		"CM_ASC_API_KEY_B64":      "BASE64P8==",
		"CM_ASC_KEY_ID":           "ABCD123",
		"CM_ASC_ISSUER_ID":        "11111111-2222-3333-4444-555555555555",
		"CM_SUBMIT_TO_TESTFLIGHT": "true",
		"CM_SUBMIT_TO_APP_STORE":  "false",
		"CM_PHASED_RELEASE":       "true",
		"CM_SUBMIT_AS_DRAFT":      "false",
		"CM_IPA_S3_KEY":           "ipas/some-build.ipa",
	}
	for k, want := range expect {
		got, _ := vars[k].(string)
		if got != want {
			t.Errorf("var %q: got %q want %q", k, got, want)
		}
	}
}
