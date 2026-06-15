package observability

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

type fakeSecretProvider struct {
	env     string
	secrets map[string]string
}

func (f fakeSecretProvider) GetSecret(key string) string { return f.secrets[key] }
func (f fakeSecretProvider) GetEnv() string              { return f.env }

func TestLangfuseConfigFromEnvBuildsOTLPEndpointAndHeaders(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-lf-test")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-lf-test")
	t.Setenv("LANGFUSE_BASE_URL", "https://us.cloud.langfuse.com")
	t.Setenv("ENVIRONMENT", "test")

	cfg := LangfuseConfigFromEnv()

	if !cfg.Enabled {
		t.Fatal("expected Langfuse config to be enabled")
	}
	if cfg.Endpoint != "https://us.cloud.langfuse.com/api/public/otel/v1/traces" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Headers["Authorization"] != "Basic cGstbGYtdGVzdDpzay1sZi10ZXN0" {
		t.Fatalf("authorization header = %q", cfg.Headers["Authorization"])
	}
	if cfg.Headers["x-langfuse-ingestion-version"] != "4" {
		t.Fatalf("ingestion header = %q", cfg.Headers["x-langfuse-ingestion-version"])
	}
	if cfg.Environment != "test" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
}

func TestLangfuseConfigFromSecretsWhenEnvKeysAreMissing(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")
	t.Setenv("LANGFUSE_BASE_URL", "")
	t.Setenv("LANGFUSE_OTEL_TRACES_ENDPOINT", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("APP_ENV", "")

	cfg := LangfuseConfigFromEnvAndSecrets(fakeSecretProvider{
		env: "prod",
		secrets: map[string]string{
			"LANGFUSE_PUBLIC_KEY": "pk-lf-secret",
			"LANGFUSE_SECRET_KEY": "sk-lf-secret",
			"LANGFUSE_BASE_URL":   "https://us.cloud.langfuse.com",
		},
	})

	if !cfg.Enabled {
		t.Fatal("expected Langfuse config to be enabled from secrets")
	}
	if cfg.Endpoint != "https://us.cloud.langfuse.com/api/public/otel/v1/traces" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Headers["Authorization"] != "Basic cGstbGYtc2VjcmV0OnNrLWxmLXNlY3JldA==" {
		t.Fatalf("authorization header = %q", cfg.Headers["Authorization"])
	}
	if cfg.Environment != "prod" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
}

func TestLangfuseConfigEnvOverridesSecrets(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "pk-lf-env")
	t.Setenv("LANGFUSE_SECRET_KEY", "sk-lf-env")
	t.Setenv("LANGFUSE_BASE_URL", "https://cloud.langfuse.com")
	t.Setenv("ENVIRONMENT", "env-prod")

	cfg := LangfuseConfigFromEnvAndSecrets(fakeSecretProvider{
		env: "secret-prod",
		secrets: map[string]string{
			"LANGFUSE_PUBLIC_KEY": "pk-lf-secret",
			"LANGFUSE_SECRET_KEY": "sk-lf-secret",
			"LANGFUSE_BASE_URL":   "https://us.cloud.langfuse.com",
		},
	})

	if cfg.Endpoint != "https://cloud.langfuse.com/api/public/otel/v1/traces" {
		t.Fatalf("endpoint = %q", cfg.Endpoint)
	}
	if cfg.Headers["Authorization"] != "Basic cGstbGYtZW52OnNrLWxmLWVudg==" {
		t.Fatalf("authorization header = %q", cfg.Headers["Authorization"])
	}
	if cfg.Environment != "env-prod" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
}

func TestLangfuseConfigDisabledWithoutKeys(t *testing.T) {
	t.Setenv("LANGFUSE_PUBLIC_KEY", "")
	t.Setenv("LANGFUSE_SECRET_KEY", "")

	cfg := LangfuseConfigFromEnv()

	if cfg.Enabled {
		t.Fatal("expected Langfuse config to be disabled without credentials")
	}
}

func TestTraceAttributesUseFilterableLangfuseKeys(t *testing.T) {
	attrs := attrsByKey(TraceAttributes(TraceAttributeInput{
		Name:           "generate_app",
		Action:         "generate_app",
		AppType:        "mobile_app_native",
		Provider:       "anthropic",
		Model:          "claude-sonnet-4-6",
		Reference:      "project-1",
		UserID:         "user-1",
		OrgID:          "org-1",
		ProjectID:      "project-1",
		TraceID:        "run-1",
		RunID:          "run-1",
		Input:          "build a shop",
		RequiredSkills: []string{"web/frontend-patterns"},
		LoadedSkills:   []string{"shared/api-design"},
		Environment:    "production",
		Tags:           []string{"wemob", "agent", "generate_app"},
	}))

	assertStringAttr(t, attrs, "langfuse.trace.name", "generate_app")
	assertStringAttr(t, attrs, "langfuse.user.id", "user-1")
	assertStringAttr(t, attrs, "langfuse.session.id", "run-1")
	assertStringAttr(t, attrs, "langfuse.trace.input", "build a shop")
	assertStringAttr(t, attrs, "langfuse.environment", "production")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.org_id", "org-1")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.project_id", "project-1")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.action", "generate_app")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.app_type", "mobile_app_native")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.provider", "anthropic")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.model", "claude-sonnet-4-6")
	assertStringAttr(t, attrs, "langfuse.trace.metadata.reference", "project-1")
	assertStringSliceAttr(t, attrs, "langfuse.trace.tags", []string{"wemob", "agent", "generate_app"})
	assertStringSliceAttr(t, attrs, "wemob.agent.required_skills", []string{"web/frontend-patterns"})
	assertStringSliceAttr(t, attrs, "wemob.agent.loaded_skills", []string{"shared/api-design"})
}

func TestGenerationAttributesMapUsageCostAndContent(t *testing.T) {
	entry := GenerationAttributeInput{
		Model:                    "claude-sonnet-4-6",
		PromptTokens:             100,
		CompletionTokens:         40,
		TotalTokens:              140,
		CacheCreationInputTokens: 20,
		CacheReadInputTokens:     10,
		LatencyMs:                1234,
		IsStreaming:              true,
		SpanName:                 "stream_with_tools",
		CostUSD:                  0.0123,
		CostSource:               "catalog_estimate",
		InputMessages: []MessageSnapshot{
			{Role: "user", Content: "hello"},
		},
		OutputText: "world",
	}

	attrs := attrsByKey(GenerationAttributes(entry, 2))

	assertStringAttr(t, attrs, "langfuse.observation.type", "generation")
	assertStringAttr(t, attrs, "langfuse.observation.model.name", "claude-sonnet-4-6")
	assertStringAttr(t, attrs, "langfuse.observation.metadata.span_name", "stream_with_tools")
	assertStringAttr(t, attrs, "langfuse.observation.metadata.cost_source", "catalog_estimate")
	assertStringAttr(t, attrs, "langfuse.observation.input", `[{"role":"user","content":"hello"}]`)
	assertStringAttr(t, attrs, "langfuse.observation.output", "world")
	assertIntAttr(t, attrs, "gen_ai.usage.input_tokens", 100)
	assertIntAttr(t, attrs, "gen_ai.usage.output_tokens", 40)
	assertIntAttr(t, attrs, "gen_ai.usage.total_tokens", 140)
	assertIntAttr(t, attrs, "langfuse.observation.metadata.cache_creation_input_tokens", 20)
	assertIntAttr(t, attrs, "langfuse.observation.metadata.cache_read_input_tokens", 10)
	assertIntAttr(t, attrs, "langfuse.observation.metadata.round", 2)
	assertBoolAttr(t, attrs, "langfuse.observation.metadata.streaming", true)
	assertFloatAttr(t, attrs, "gen_ai.usage.cost", 0.0123)
	assertIntAttr(t, attrs, "langfuse.observation.metadata.latency_ms", 1234)
}

func TestToolAttributesMapErrorsAsLangfuseObservationLevel(t *testing.T) {
	attrs := attrsByKey(ToolAttributes("write_files", `{"path":"app/page.tsx"}`, true, 250))

	assertStringAttr(t, attrs, "langfuse.observation.type", "span")
	assertStringAttr(t, attrs, "langfuse.observation.level", "ERROR")
	assertStringAttr(t, attrs, "langfuse.observation.metadata.tool_name", "write_files")
	assertStringAttr(t, attrs, "langfuse.observation.input", `{"path":"app/page.tsx"}`)
	assertBoolAttr(t, attrs, "langfuse.observation.metadata.is_error", true)
	assertIntAttr(t, attrs, "langfuse.observation.metadata.duration_ms", 250)
}

func attrsByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	out := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		out[string(attr.Key)] = attr.Value
	}
	return out
}

func assertStringAttr(t *testing.T, attrs map[string]attribute.Value, key, want string) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if got.AsString() != want {
		t.Fatalf("%s = %q, want %q", key, got.AsString(), want)
	}
}

func assertStringSliceAttr(t *testing.T, attrs map[string]attribute.Value, key string, want []string) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	values := got.AsStringSlice()
	if len(values) != len(want) {
		t.Fatalf("%s len = %d, want %d", key, len(values), len(want))
	}
	for i := range want {
		if values[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q", key, i, values[i], want[i])
		}
	}
}

func assertIntAttr(t *testing.T, attrs map[string]attribute.Value, key string, want int64) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if got.AsInt64() != want {
		t.Fatalf("%s = %d, want %d", key, got.AsInt64(), want)
	}
}

func assertBoolAttr(t *testing.T, attrs map[string]attribute.Value, key string, want bool) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if got.AsBool() != want {
		t.Fatalf("%s = %v, want %v", key, got.AsBool(), want)
	}
}

func assertFloatAttr(t *testing.T, attrs map[string]attribute.Value, key string, want float64) {
	t.Helper()
	got, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %s", key)
	}
	if got.AsFloat64() != want {
		t.Fatalf("%s = %f, want %f", key, got.AsFloat64(), want)
	}
}
