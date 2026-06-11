package controllers

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/knid/wemob/internal/agentruntime"
	"github.com/knid/wemob/internal/models"
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
	if got != agentruntime.ToolRiskMedium {
		t.Fatalf("risk = %q, want %q", got, agentruntime.ToolRiskMedium)
	}
}

func TestClassifyMCPRiskUsesAnnotations(t *testing.T) {
	if got := classifyMCPRisk("read_inbox", map[string]any{"readOnlyHint": true}); got != agentruntime.ToolRiskLow {
		t.Fatalf("readOnlyHint risk = %q, want %q", got, agentruntime.ToolRiskLow)
	}
	if got := classifyMCPRisk("archive_email", map[string]any{"destructiveHint": true}); got != agentruntime.ToolRiskDestructive {
		t.Fatalf("destructiveHint risk = %q, want %q", got, agentruntime.ToolRiskDestructive)
	}
}

func TestMCPOAuthMetadataURLs(t *testing.T) {
	resource, err := mcpCanonicalResourceURI("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("mcpCanonicalResourceURI: %v", err)
	}
	if resource != "https://mcp.example.com/mcp" {
		t.Fatalf("resource = %q", resource)
	}
	metadataURL, err := mcpProtectedResourceMetadataURL(resource)
	if err != nil {
		t.Fatalf("mcpProtectedResourceMetadataURL: %v", err)
	}
	if metadataURL != "https://mcp.example.com/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("metadataURL = %q", metadataURL)
	}

	rootResource, err := mcpCanonicalResourceURI("https://mcp.example.com/")
	if err != nil {
		t.Fatalf("root resource: %v", err)
	}
	rootMetadataURL, err := mcpProtectedResourceMetadataURL(rootResource)
	if err != nil {
		t.Fatalf("root metadata URL: %v", err)
	}
	if rootMetadataURL != "https://mcp.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("root metadataURL = %q", rootMetadataURL)
	}
}

func TestParseWWWAuthenticateResourceMetadata(t *testing.T) {
	header := `Bearer error="invalid_token", resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource/mcp"`
	got := parseWWWAuthenticateParam(header, "resource_metadata")
	want := "https://mcp.example.com/.well-known/oauth-protected-resource/mcp"
	if got != want {
		t.Fatalf("resource_metadata = %q, want %q", got, want)
	}
}

func TestDecodeMCPCredentialDataAcceptsStoredJSONShapes(t *testing.T) {
	rawObject := []byte(`{"client_secret":"enc:v1:secret","expiry":123}`)
	quotedObject, _ := json.Marshal(string(rawObject))
	base64Object, _ := json.Marshal(base64.StdEncoding.EncodeToString(rawObject))

	for _, raw := range [][]byte{rawObject, quotedObject, base64Object} {
		got, err := decodeMCPCredentialData(raw)
		if err != nil {
			t.Fatalf("decodeMCPCredentialData(%s): %v", raw, err)
		}
		if got["client_secret"] != "enc:v1:secret" {
			t.Fatalf("client_secret = %v", got["client_secret"])
		}
		if got["expiry"] != float64(123) {
			t.Fatalf("expiry = %v", got["expiry"])
		}
	}
}

func TestMCPCatalogIncludesReadyProviders(t *testing.T) {
	c := &Controller{}
	templates := c.ListMCPTemplates()
	byID := map[string]bool{}
	for _, template := range templates {
		byID[template.ID] = true
		if template.ID == "posthog" {
			if template.ServerURL != "https://mcp.posthog.com/mcp" || !template.SupportsOAuthDiscovery {
				t.Fatalf("bad posthog template: %+v", template)
			}
			if template.AuthType != models.MCPAuthTypeOAuth {
				t.Fatalf("posthog auth type = %q", template.AuthType)
			}
		}
	}
	for _, id := range []string{"posthog", "betterstack", "sentry", "linear", "notion", "github", "vercel", "figma", "neon", "supabase", "stripe", "cloudflare"} {
		if !byID[id] {
			t.Fatalf("missing catalog template %q", id)
		}
	}
}
