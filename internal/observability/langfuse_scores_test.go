package observability

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestScoreClientCreateScoreRequestShape(t *testing.T) {
	var gotPath string
	var gotAuthUser string
	var gotAuthPass string
	var gotBody string

	client := ScoreClient{
		BaseURL:   "https://cloud.langfuse.com",
		PublicKey: "pk-lf-test",
		SecretKey: "sk-lf-test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotAuthUser, gotAuthPass, _ = req.BasicAuth()
			body, _ := io.ReadAll(req.Body)
			gotBody = string(body)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	err := client.CreateScore(context.Background(), ScoreRequest{
		TraceID: "trace-1",
		Name:    ScoreAgentRunSuccess,
		Value:   1,
		Comment: "agent run completed",
	})
	if err != nil {
		t.Fatalf("CreateScore returned error: %v", err)
	}
	if gotPath != "/api/public/scores" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuthUser != "pk-lf-test" || gotAuthPass != "sk-lf-test" {
		t.Fatalf("auth = %q/%q", gotAuthUser, gotAuthPass)
	}
	for _, want := range []string{`"traceId":"trace-1"`, `"name":"agent_run_success"`, `"value":1`, `"comment":"agent run completed"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body %q missing %s", gotBody, want)
		}
	}
}

func TestScoreClientCreateScoreNoopsWhenUnconfigured(t *testing.T) {
	err := (ScoreClient{}).CreateScore(context.Background(), ScoreRequest{
		TraceID: "trace-1",
		Name:    ScoreAgentRunSuccess,
		Value:   1,
	})
	if err != nil {
		t.Fatalf("CreateScore returned error: %v", err)
	}
}

func TestScoreClientFromEnvAndSecretsUsesOTELTracesEndpointHost(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")
	t.Setenv("LANGFUSE_BASE_URL", "")
	t.Setenv("LANGFUSE_OTEL_TRACES_ENDPOINT", "")

	client := ScoreClientFromEnvAndSecrets(fakeSecretProvider{
		secrets: map[string]string{
			"LANGFUSE_PUBLIC_KEY":           "pk-lf-secret",
			"LANGFUSE_SECRET_KEY":           "sk-lf-secret",
			"LANGFUSE_OTEL_TRACES_ENDPOINT": "https://self-hosted.example.com/custom/api/public/otel/v1/traces",
		},
	})

	if client.BaseURL != "https://self-hosted.example.com/custom" {
		t.Fatalf("BaseURL = %q", client.BaseURL)
	}
}

func TestBaseURLFromOTELTracesEndpoint(t *testing.T) {
	tests := map[string]string{
		"https://cloud.langfuse.com/api/public/otel/v1/traces":            "https://cloud.langfuse.com",
		"https://us.cloud.langfuse.com/api/public/otel":                   "https://us.cloud.langfuse.com",
		"https://langfuse.example.com/base/api/public/otel/v1/traces?x=1": "https://langfuse.example.com/base",
		"not a url": "",
		"":          "",
	}
	for endpoint, want := range tests {
		if got := baseURLFromOTELTracesEndpoint(endpoint); got != want {
			t.Fatalf("baseURLFromOTELTracesEndpoint(%q) = %q, want %q", endpoint, got, want)
		}
	}
}
