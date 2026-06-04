package mcpclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestValidateRemoteURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https domain", raw: "https://mcp.example.com/mcp"},
		{name: "public ipv4 allowed", raw: "https://8.8.8.8/mcp"},
		{name: "http rejected", raw: "http://mcp.example.com/mcp", wantErr: true},
		{name: "userinfo rejected", raw: "https://token@mcp.example.com/mcp", wantErr: true},
		{name: "localhost rejected", raw: "https://localhost/mcp", wantErr: true},
		{name: "localhost suffix rejected", raw: "https://api.localhost/mcp", wantErr: true},
		{name: "loopback rejected", raw: "https://127.0.0.1/mcp", wantErr: true},
		{name: "ipv6 loopback rejected", raw: "https://[::1]/mcp", wantErr: true},
		{name: "private ip rejected", raw: "https://10.0.0.10/mcp", wantErr: true},
		{name: "metadata ip rejected", raw: "https://169.254.169.254/latest", wantErr: true},
		{name: "metadata host rejected", raw: "https://metadata.google.internal/mcp", wantErr: true},
		{name: "short loopback rejected", raw: "https://127.1/mcp", wantErr: true},
		{name: "integer ip rejected", raw: "https://2130706433/mcp", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemoteURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRemoteURL(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestSafeHTTPClientRejectsCrossOriginRedirect(t *testing.T) {
	client := NewSafeHTTPClient(map[string]string{"Authorization": "Bearer secret"}, time.Second)
	from := &http.Request{URL: mustURL(t, "https://mcp.example.com/mcp")}

	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "https://other.example.com/mcp")}, []*http.Request{from}); err == nil {
		t.Fatal("expected cross-origin redirect to be rejected")
	}
	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "https://mcp.example.com/next")}, []*http.Request{from}); err != nil {
		t.Fatalf("same-origin redirect rejected: %v", err)
	}
	if err := client.CheckRedirect(&http.Request{URL: mustURL(t, "https://127.0.0.1/mcp")}, []*http.Request{from}); err == nil {
		t.Fatal("expected private-network redirect to be rejected")
	}
}

func TestSafeHTTPClientRejectsUnsafeInitialRequest(t *testing.T) {
	client := NewSafeHTTPClient(nil, time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://mcp.example.com/mcp", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	if _, err := client.Transport.RoundTrip(req); err == nil {
		t.Fatal("expected unsafe initial request to be rejected")
	}
}

func TestCallToolInvalidArgumentsReturnsError(t *testing.T) {
	output, err := CallTool(context.Background(), ServerConfig{
		URL:             "https://mcp.example.com/mcp",
		Enabled:         true,
		TimeoutSeconds:  1,
		RestrictNetwork: true,
	}, "lookup", "{")
	if err == nil {
		t.Fatal("expected invalid JSON arguments to return an error")
	}
	if !strings.Contains(output, "Failed to parse arguments for lookup") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}
