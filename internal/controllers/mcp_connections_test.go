package controllers

import (
	"testing"

	"github.com/knid/wemob/internal/agent"
)

func TestSanitizeMCPReturnURL(t *testing.T) {
	c := &Controller{SecretManager: &fakeStaticSecretManager{values: map[string]string{
		"FRONTEND_URL":       "https://app.wemob.io/",
		"WEMOB_FRONTEND_URL": "https://studio.wemob.io",
	}}}

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty allowed"},
		{name: "frontend origin allowed", raw: "https://app.wemob.io/studio/projects/1?tab=mcp", want: "https://app.wemob.io/studio/projects/1?tab=mcp"},
		{name: "secondary origin allowed", raw: "https://studio.wemob.io/apps/1", want: "https://studio.wemob.io/apps/1"},
		{name: "relative rejected", raw: "/studio/projects/1", wantErr: true},
		{name: "other origin rejected", raw: "https://evil.example.com/callback", wantErr: true},
		{name: "userinfo rejected", raw: "https://user@app.wemob.io/callback", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.sanitizeMCPReturnURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeMCPReturnURL(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("sanitizeMCPReturnURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestClassifyMCPRiskDestructiveFalseIsNotHigh(t *testing.T) {
	got := classifyMCPRisk("sync_messages", map[string]any{"destructiveHint": false})
	if got != agent.ToolRiskMedium {
		t.Fatalf("risk = %q, want %q", got, agent.ToolRiskMedium)
	}
}

func TestClassifyMCPRiskUsesAnnotations(t *testing.T) {
	if got := classifyMCPRisk("read_inbox", map[string]any{"readOnlyHint": true}); got != agent.ToolRiskLow {
		t.Fatalf("readOnlyHint risk = %q, want %q", got, agent.ToolRiskLow)
	}
	if got := classifyMCPRisk("archive_email", map[string]any{"destructiveHint": true}); got != agent.ToolRiskDestructive {
		t.Fatalf("destructiveHint risk = %q, want %q", got, agent.ToolRiskDestructive)
	}
}
