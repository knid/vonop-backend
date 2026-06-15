package observability

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

const (
	defaultLangfuseBaseURL = "https://cloud.langfuse.com"
	langfuseOTELPath       = "/api/public/otel/v1/traces"
)

type LangfuseConfig struct {
	Enabled     bool
	Endpoint    string
	Headers     map[string]string
	Environment string
}

type SecretProvider interface {
	GetSecret(key string) string
	GetEnv() string
}

type TraceAttributeInput struct {
	Name           string
	Action         string
	AppType        string
	Provider       string
	Model          string
	Reference      string
	UserID         string
	OrgID          string
	ProjectID      string
	TraceID        string
	RunID          string
	SessionID      string
	Input          string
	Output         string
	RequiredSkills []string
	LoadedSkills   []string
	Environment    string
	Release        string
	Version        string
	Tags           []string
}

type TraceContext struct {
	Name           string
	Action         string
	AppType        string
	Provider       string
	Model          string
	Reference      string
	UserID         string
	OrgID          string
	ProjectID      string
	TraceID        string
	RunID          string
	SessionID      string
	Input          string
	Output         string
	RequiredSkills []string
	LoadedSkills   []string
	Environment    string
	Release        string
	Version        string
	Tags           []string
}

func (tc TraceContext) Attributes(extraTags ...string) []attribute.KeyValue {
	tags := append([]string(nil), tc.Tags...)
	tags = append(tags, extraTags...)
	return TraceAttributes(TraceAttributeInput{
		Name:           tc.Name,
		Action:         tc.Action,
		AppType:        tc.AppType,
		Provider:       tc.Provider,
		Model:          tc.Model,
		Reference:      tc.Reference,
		UserID:         tc.UserID,
		OrgID:          tc.OrgID,
		ProjectID:      tc.ProjectID,
		TraceID:        tc.TraceID,
		RunID:          tc.RunID,
		SessionID:      tc.SessionID,
		Input:          tc.Input,
		Output:         tc.Output,
		RequiredSkills: tc.RequiredSkills,
		LoadedSkills:   tc.LoadedSkills,
		Environment:    tc.Environment,
		Release:        tc.Release,
		Version:        tc.Version,
		Tags:           tags,
	})
}

type MessageSnapshot struct {
	Role    string   `json:"role"`
	Content string   `json:"content,omitempty"`
	Images  []string `json:"images,omitempty"`
}

type GenerationAttributeInput struct {
	Model                    string
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	LatencyMs                int64
	IsStreaming              bool
	SpanName                 string
	CostUSD                  float64
	CostSource               string
	InputMessages            []MessageSnapshot
	OutputText               string
	ToolNames                []string
}

func LangfuseConfigFromEnv() LangfuseConfig {
	return LangfuseConfigFromEnvAndSecrets(nil)
}

func LangfuseConfigFromEnvAndSecrets(secrets SecretProvider) LangfuseConfig {
	publicKey := envOrSecret("LANGFUSE_PUBLIC_KEY", secrets)
	secretKey := envOrSecret("LANGFUSE_SECRET_KEY", secrets)
	if publicKey == "" || secretKey == "" {
		return LangfuseConfig{Enabled: false}
	}

	endpoint := envOrSecret("LANGFUSE_OTEL_TRACES_ENDPOINT", secrets)
	if endpoint == "" {
		baseURL := strings.TrimRight(envOrSecret("LANGFUSE_BASE_URL", secrets), "/")
		if baseURL == "" {
			baseURL = defaultLangfuseBaseURL
		}
		endpoint = baseURL + langfuseOTELPath
	}

	auth := base64.StdEncoding.EncodeToString([]byte(publicKey + ":" + secretKey))
	env := strings.TrimSpace(os.Getenv("ENVIRONMENT"))
	if env == "" {
		env = strings.TrimSpace(os.Getenv("APP_ENV"))
	}
	if env == "" && secrets != nil {
		env = strings.TrimSpace(secrets.GetEnv())
	}
	if env == "" {
		env = "default"
	}

	return LangfuseConfig{
		Enabled:  true,
		Endpoint: endpoint,
		Headers: map[string]string{
			"Authorization":                "Basic " + auth,
			"x-langfuse-ingestion-version": "4",
		},
		Environment: env,
	}
}

func envOrSecret(key string, secrets SecretProvider) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if secrets == nil {
		return ""
	}
	return strings.TrimSpace(secrets.GetSecret(key))
}

func TraceAttributes(in TraceAttributeInput) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 24)
	if in.Name != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.name", in.Name))
	}
	if in.Environment != "" {
		attrs = append(attrs, attribute.String("langfuse.environment", in.Environment))
		attrs = append(attrs, attribute.String("deployment.environment.name", in.Environment))
	}
	if len(in.Tags) > 0 {
		attrs = append(attrs, attribute.StringSlice("langfuse.trace.tags", dedupeOrdered(in.Tags)))
	}
	if in.UserID != "" {
		attrs = append(attrs, attribute.String("langfuse.user.id", in.UserID))
	}
	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = in.RunID
	}
	if sessionID == "" {
		sessionID = in.TraceID
	}
	if sessionID != "" {
		attrs = append(attrs, attribute.String("langfuse.session.id", sessionID))
	}
	if in.Release != "" {
		attrs = append(attrs, attribute.String("langfuse.release", in.Release))
	}
	if in.Version != "" {
		attrs = append(attrs, attribute.String("langfuse.version", in.Version))
	}
	if in.Input != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.input", in.Input))
	}
	if in.Output != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.output", in.Output))
	}
	if in.OrgID != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.org_id", in.OrgID))
	}
	if in.ProjectID != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.project_id", in.ProjectID))
	}
	if in.RunID != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.run_id", in.RunID))
	}
	if len(in.RequiredSkills) > 0 {
		attrs = append(attrs, attribute.StringSlice("wemob.agent.required_skills", in.RequiredSkills))
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.agent_required_skills", strings.Join(in.RequiredSkills, ",")))
	}
	if len(in.LoadedSkills) > 0 {
		attrs = append(attrs, attribute.StringSlice("wemob.agent.loaded_skills", in.LoadedSkills))
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.agent_loaded_skills", strings.Join(in.LoadedSkills, ",")))
	}
	if in.Action != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.action", in.Action))
	}
	if in.AppType != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.app_type", in.AppType))
	}
	if in.Provider != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.provider", in.Provider))
	}
	if in.Model != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.model", in.Model))
	}
	if in.Reference != "" {
		attrs = append(attrs, attribute.String("langfuse.trace.metadata.reference", in.Reference))
	}
	return attrs
}

func GenerationAttributes(entry GenerationAttributeInput, round int) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "generation"),
		attribute.String("langfuse.observation.model.name", entry.Model),
		attribute.String("gen_ai.request.model", entry.Model),
		attribute.Int("gen_ai.usage.input_tokens", entry.PromptTokens),
		attribute.Int("gen_ai.usage.output_tokens", entry.CompletionTokens),
		attribute.Int("gen_ai.usage.total_tokens", entry.TotalTokens),
		attribute.Int("langfuse.observation.metadata.round", round),
		attribute.Bool("langfuse.observation.metadata.streaming", entry.IsStreaming),
		attribute.Int64("langfuse.observation.metadata.latency_ms", entry.LatencyMs),
	}
	if entry.SpanName != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.metadata.span_name", entry.SpanName))
	}
	if entry.CostUSD > 0 {
		attrs = append(attrs, attribute.Float64("gen_ai.usage.cost", entry.CostUSD))
	}
	if entry.CostSource != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.metadata.cost_source", entry.CostSource))
	}
	if entry.CacheCreationInputTokens > 0 {
		attrs = append(attrs, attribute.Int("langfuse.observation.metadata.cache_creation_input_tokens", entry.CacheCreationInputTokens))
	}
	if entry.CacheReadInputTokens > 0 {
		attrs = append(attrs, attribute.Int("langfuse.observation.metadata.cache_read_input_tokens", entry.CacheReadInputTokens))
	}
	if len(entry.InputMessages) > 0 {
		if raw := messagesJSON(entry.InputMessages); raw != "" {
			attrs = append(attrs, attribute.String("langfuse.observation.input", raw))
		}
	}
	if entry.OutputText != "" {
		attrs = append(attrs, attribute.String("langfuse.observation.output", entry.OutputText))
	}
	if len(entry.ToolNames) > 0 {
		attrs = append(attrs, attribute.StringSlice("langfuse.observation.metadata.tools", dedupeOrdered(entry.ToolNames)))
	}
	return attrs
}

func ToolAttributes(toolName, input string, isError bool, durationMs int) []attribute.KeyValue {
	level := "DEFAULT"
	if isError {
		level = "ERROR"
	}
	return []attribute.KeyValue{
		attribute.String("langfuse.observation.type", "span"),
		attribute.String("langfuse.observation.level", level),
		attribute.String("langfuse.observation.metadata.tool_name", toolName),
		attribute.String("langfuse.observation.input", input),
		attribute.Bool("langfuse.observation.metadata.is_error", isError),
		attribute.Int("langfuse.observation.metadata.duration_ms", durationMs),
	}
}

func messagesJSON(messages []MessageSnapshot) string {
	raw, err := json.Marshal(messages)
	if err != nil {
		return ""
	}
	return string(raw)
}

func dedupeOrdered(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
