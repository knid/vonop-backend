package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompileTextPromptReplacesVariables(t *testing.T) {
	got, err := CompileTextPrompt("Hello {{name}}, see {{thing}}.", map[string]string{
		"name":  "Ada",
		"thing": "Langfuse",
	})
	if err != nil {
		t.Fatalf("CompileTextPrompt returned error: %v", err)
	}
	if got != "Hello Ada, see Langfuse." {
		t.Fatalf("compiled prompt = %q", got)
	}
}

func TestCompileTextPromptErrorsOnMissingVariable(t *testing.T) {
	if _, err := CompileTextPrompt("Hello {{name}}.", nil); err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestPromptClientGetTextPrompt(t *testing.T) {
	var sawPath, sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.String()
		user, pass, _ := r.BasicAuth()
		sawAuth = user + ":" + pass
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":    "wemob-test",
			"version": 7,
			"prompt":  "Hi {{name}}",
		})
	}))
	defer srv.Close()

	client := PromptClient{
		BaseURL:    srv.URL,
		PublicKey:  "pk",
		SecretKey:  "sk",
		HTTPClient: srv.Client(),
	}
	prompt, err := client.GetTextPrompt(context.Background(), "wemob-test", "production")
	if err != nil {
		t.Fatalf("GetTextPrompt returned error: %v", err)
	}
	if sawPath != "/api/public/v2/prompts/wemob-test?label=production" {
		t.Fatalf("path = %q", sawPath)
	}
	if sawAuth != "pk:sk" {
		t.Fatalf("auth = %q", sawAuth)
	}
	if prompt.Name != "wemob-test" || prompt.Version != 7 || prompt.Prompt != "Hi {{name}}" {
		t.Fatalf("prompt = %+v", prompt)
	}
}
