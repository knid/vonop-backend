package agentruntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartRunSignsRequest(t *testing.T) {
	now := time.Unix(1710000000, 0)
	var gotAuth, gotTimestamp, gotSignature, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/agent-runs" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotTimestamp = r.Header.Get("X-Wemob-Timestamp")
		gotSignature = r.Header.Get("X-Wemob-Signature")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(RuntimeRun{
			RunID:    "run-1",
			ThreadID: "thread-1",
			Status:   "running",
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "internal-secret", "access-secret")
	client.Now = func() time.Time { return now }
	run, err := client.StartRun(context.Background(), StartRunRequest{
		ThreadID:        "thread-1",
		JobID:           "job-1",
		ProjectID:       "project-1",
		OrgID:           "org-1",
		UserID:          "user-1",
		RuntimeRunID:    "runtime-run-1",
		Message:         "build",
		Mode:            "edit",
		ProjectType:     "app",
		FrontendRuntime: "vite_react",
		WorktreePath:    "/data/worktrees/project-1/job-1",
		BaseCommit:      "abc123",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.AssistantID != DefaultAssistantID {
		t.Fatalf("assistant id = %q", run.AssistantID)
	}
	if gotAuth != "Bearer internal-secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotTimestamp != "1710000000" {
		t.Fatalf("timestamp = %q", gotTimestamp)
	}
	wantSignature := testSignature("internal-secret", gotTimestamp, gotBody)
	if gotSignature != wantSignature {
		t.Fatalf("signature = %q want %q", gotSignature, wantSignature)
	}
	if !strings.Contains(gotBody, `"thread_id":"thread-1"`) {
		t.Fatalf("body missing thread id: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"worktree_path":"/data/worktrees/project-1/job-1"`) {
		t.Fatalf("body missing worktree path: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"base_commit":"abc123"`) {
		t.Fatalf("body missing base commit: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"agent_runtime_run_id":"runtime-run-1"`) {
		t.Fatalf("body missing runtime run id: %s", gotBody)
	}
}

func TestIssueRuntimeAccessToken(t *testing.T) {
	client := NewClient("https://runtime.internal", "internal-secret", "access-secret")
	client.Now = func() time.Time { return time.Unix(1710000000, 0) }

	info, err := client.IssueRuntimeAccessToken(RuntimeAccessRequest{
		UserID:    "user-1",
		OrgID:     "org-1",
		ProjectID: "project-1",
		ThreadID:  "thread-1",
		ExpiresIn: time.Minute,
	})
	if err != nil {
		t.Fatalf("IssueRuntimeAccessToken: %v", err)
	}
	if info.RuntimeAPIURL != "https://runtime.internal" {
		t.Fatalf("RuntimeAPIURL = %q", info.RuntimeAPIURL)
	}
	if info.AssistantID != DefaultAssistantID {
		t.Fatalf("AssistantID = %q", info.AssistantID)
	}
	parts := strings.Split(info.RuntimeAccessToken, ".")
	if len(parts) != 2 {
		t.Fatalf("token parts = %d", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	mac := hmac.New(sha256.New, []byte("access-secret"))
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		t.Fatal("token signature mismatch")
	}
	var claims map[string]any
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims["thread_id"] != "thread-1" || claims["assistant_id"] != DefaultAssistantID {
		t.Fatalf("claims = %+v", claims)
	}
	if int64(claims["exp"].(float64)) != 1710000060 {
		t.Fatalf("exp = %+v", claims["exp"])
	}
}

func TestCancelAndResumeUseRuntimeEndpoints(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/resume") {
			_ = json.NewEncoder(w).Encode(RuntimeRun{RunID: "run-1", ThreadID: "thread-1", Status: "running"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "internal-secret", "access-secret")
	if err := client.CancelRun(context.Background(), "run-1"); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if _, err := client.ResumeRun(context.Background(), "run-1", ResumeRunRequest{Decision: "approved"}); err != nil {
		t.Fatalf("ResumeRun: %v", err)
	}
	want := []string{
		"POST /internal/agent-runs/run-1/cancel",
		"POST /internal/agent-runs/run-1/resume",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %v want %v", paths, want)
	}
}

func testSignature(secret string, timestamp string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
