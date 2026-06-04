package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/knid/wemob/internal/utils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerConfig describes a single MCP server to connect to.
type ServerConfig struct {
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers"`
	Enabled         bool              `json:"enabled"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	ToolPrefix      string            `json:"tool_prefix"`
	RestrictNetwork bool              `json:"restrict_network"`
}

// Config is the top-level configuration parsed from the MCP_SERVERS_CONFIG secret.
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

type DiscoveredTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type serverConnection struct {
	config  ServerConfig
	session *mcp.ClientSession
}

type toolEntry struct {
	serverIdx int
	mcpTool   mcp.Tool
	prefixed  string
}

// Service manages connections to MCP servers and exposes their tools.
type Service struct {
	connections []serverConnection
	tools       []toolEntry
}

func DiscoverTools(ctx context.Context, sc ServerConfig) ([]DiscoveredTool, error) {
	session, err := connectSession(ctx, sc)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	var result []DiscoveredTool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return result, err
		}
		inputSchema := make(map[string]any)
		if tool.InputSchema != nil {
			if data, err := json.Marshal(tool.InputSchema); err == nil {
				_ = json.Unmarshal(data, &inputSchema)
			}
		}
		annotations := make(map[string]any)
		if tool.Annotations != nil {
			if data, err := json.Marshal(tool.Annotations); err == nil {
				_ = json.Unmarshal(data, &annotations)
			}
		}
		result = append(result, DiscoveredTool{
			Name:        tool.Name,
			Title:       tool.Title,
			Description: tool.Description,
			InputSchema: inputSchema,
			Annotations: annotations,
		})
	}
	return result, nil
}

func CallTool(ctx context.Context, sc ServerConfig, toolName string, argumentsJSON string) (string, error) {
	var args map[string]any
	if argumentsJSON != "" {
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return fmt.Sprintf("Failed to parse arguments for %s: %v", toolName, err), err
		}
	}

	session, err := connectSession(ctx, sc)
	if err != nil {
		return "", err
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return fmt.Sprintf("Tool %s is currently unavailable: %v", toolName, err), err
	}

	return stringifyToolResult(toolName, result), nil
}

func connectSession(ctx context.Context, sc ServerConfig) (*mcp.ClientSession, error) {
	timeout := time.Duration(sc.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	connCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport := &mcp.StreamableClientTransport{Endpoint: sc.URL}
	if sc.RestrictNetwork {
		transport.HTTPClient = NewSafeHTTPClient(sc.Headers, timeout)
	} else if len(sc.Headers) > 0 {
		transport.HTTPClient = &http.Client{
			Transport: &headerTransport{
				base:    http.DefaultTransport,
				headers: sc.Headers,
			},
		}
	}

	client := mcp.NewClient(&mcp.Implementation{
		Name:    "wemob-backend",
		Version: "1.0.0",
	}, nil)
	session, err := client.Connect(connCtx, transport, nil)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// NewService parses configJSON, connects to each enabled server, discovers tools,
// and returns a ready-to-use Service. Failed servers are logged and skipped.
func NewService(ctx context.Context, configJSON string) (*Service, error) {
	var config Config
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("invalid MCP config JSON: %w", err)
	}

	svc := &Service{}
	var connectErrors []string

	for _, sc := range config.Servers {
		if !sc.Enabled {
			slog.Info("MCP server disabled, skipping", "name", sc.Name)
			continue
		}

		session, err := connectSession(ctx, sc)
		if err != nil {
			msg := fmt.Sprintf("failed to connect to MCP server %q: %v", sc.Name, err)
			slog.Warn(msg)
			connectErrors = append(connectErrors, msg)
			continue
		}

		connIdx := len(svc.connections)
		svc.connections = append(svc.connections, serverConnection{
			config:  sc,
			session: session,
		})

		// Discover tools from this server
		toolsBefore := len(svc.tools)
		for tool, err := range session.Tools(ctx, nil) {
			if err != nil {
				slog.Warn("error listing tools from MCP server", "name", sc.Name, "error", err)
				break
			}
			prefixed := tool.Name
			if sc.ToolPrefix != "" {
				prefixed = sc.ToolPrefix + "__" + tool.Name
			}
			svc.tools = append(svc.tools, toolEntry{
				serverIdx: connIdx,
				mcpTool:   *tool,
				prefixed:  prefixed,
			})
		}

		slog.Info("Connected to MCP server",
			"name", sc.Name,
			"tools_discovered", len(svc.tools)-toolsBefore,
		)
	}

	if len(connectErrors) > 0 && len(svc.connections) == 0 {
		return svc, fmt.Errorf("all MCP servers failed: %s", strings.Join(connectErrors, "; "))
	}
	if len(connectErrors) > 0 {
		return svc, fmt.Errorf("some MCP servers failed: %s", strings.Join(connectErrors, "; "))
	}

	return svc, nil
}

// Tools converts all discovered MCP tools into utils.Tool format for the LLM.
func (s *Service) Tools() []utils.Tool {
	if s == nil {
		return nil
	}
	result := make([]utils.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		params := make(map[string]any)
		if t.mcpTool.InputSchema != nil {
			data, err := json.Marshal(t.mcpTool.InputSchema)
			if err == nil {
				json.Unmarshal(data, &params)
			}
		}
		result = append(result, utils.Tool{
			Type: "function",
			Function: utils.ToolFunction{
				Name:        t.prefixed,
				Description: t.mcpTool.Description,
				Parameters:  params,
			},
		})
	}
	return result
}

// ToolExecutor returns a ToolExecutor that routes calls to the correct MCP server.
func (s *Service) ToolExecutor() utils.ToolExecutor {
	return func(name string, arguments string) (string, error) {
		return s.callTool(name, arguments)
	}
}

// HasTools returns true if any tools were discovered from MCP servers.
func (s *Service) HasTools() bool {
	return s != nil && len(s.tools) > 0
}

// Close shuts down all MCP server sessions.
func (s *Service) Close() {
	if s == nil {
		return
	}
	for _, conn := range s.connections {
		conn.session.Close()
	}
}

// IsMCPTool returns true if the tool name matches an MCP tool (has __ prefix separator).
func (s *Service) IsMCPTool(name string) bool {
	if s == nil {
		return false
	}
	for _, t := range s.tools {
		if t.prefixed == name {
			return true
		}
	}
	return false
}

func (s *Service) callTool(prefixedName string, argumentsJSON string) (string, error) {
	var entry *toolEntry
	for i := range s.tools {
		if s.tools[i].prefixed == prefixedName {
			entry = &s.tools[i]
			break
		}
	}
	if entry == nil {
		return fmt.Sprintf("Unknown MCP tool: %s", prefixedName), nil
	}

	conn := s.connections[entry.serverIdx]

	// Parse arguments
	var args map[string]any
	if argumentsJSON != "" {
		if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
			return fmt.Sprintf("Failed to parse arguments for %s: %v", prefixedName, err), nil
		}
	}

	timeout := time.Duration(conn.config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	result, err := conn.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      entry.mcpTool.Name,
		Arguments: args,
	})
	if err != nil {
		return fmt.Sprintf("Tool %s is currently unavailable: %v", prefixedName, err), nil
	}

	return stringifyToolResult(prefixedName, result), nil
}

func stringifyToolResult(toolName string, result *mcp.CallToolResult) string {
	if result == nil {
		return "Tool returned no result"
	}
	var texts []string
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	if result.IsError {
		if len(texts) > 0 {
			return fmt.Sprintf("Tool %s returned an error: %s", toolName, strings.Join(texts, "\n"))
		}
		return fmt.Sprintf("Tool %s returned an error", toolName)
	}
	if len(texts) == 0 {
		return "Tool returned no text content"
	}
	return strings.Join(texts, "\n")
}

// headerTransport wraps an http.RoundTripper to inject custom headers.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}
