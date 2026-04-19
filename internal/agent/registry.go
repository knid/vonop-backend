package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/utils"
)

// ToolExecutor implements one agent-callable tool.
//
// Schema() must return a JSON-schema object acceptable to the Anthropic tools
// API. Execute() receives the raw input bytes from the model, validates/parses
// them, performs the operation, and returns a human-readable result string
// plus an is_error flag. Returning err != nil is reserved for infrastructure
// errors (DB down, etc.) — tool-usage errors (invalid path, not found) should
// return isError=true with a descriptive result instead.
type ToolExecutor interface {
	Name() string
	Description() string
	Schema() map[string]any
	Execute(ctx context.Context, rc *RunContext, input json.RawMessage) (result string, isError bool, err error)
}

// ServerToolRef is an Anthropic server-side tool (e.g. web_search) that the
// provider executes internally. No local executor runs.
type ServerToolRef struct {
	Tool        *utils.Tool // the utils.Tool with ServerTool set
	DisplayName string      // surfaced in RunStartEvent.Tools
}

// Registry holds the active tool set for one run.
type Registry struct {
	order       []string
	tools       map[string]ToolExecutor
	serverTools []ServerToolRef
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]ToolExecutor)}
}

// Register adds an executor tool. Duplicate names are ignored.
func (r *Registry) Register(t ToolExecutor) {
	if _, exists := r.tools[t.Name()]; exists {
		return
	}
	r.order = append(r.order, t.Name())
	r.tools[t.Name()] = t
}

// AddServerTool registers an Anthropic server-side tool.
func (r *Registry) AddServerTool(ref ServerToolRef) {
	// Dedupe by display name against both executor and server tools.
	for _, existing := range r.order {
		if existing == ref.DisplayName {
			return
		}
	}
	r.serverTools = append(r.serverTools, ref)
	r.order = append(r.order, ref.DisplayName)
}

// ServerTools returns the registered server-side tools.
func (r *Registry) ServerTools() []ServerToolRef {
	out := make([]ServerToolRef, len(r.serverTools))
	copy(out, r.serverTools)
	return out
}

// Names returns tool names in registration order (executor + server tools
// mixed together). Used by RunStartEvent.Tools.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// AnthropicSchemas returns schemas for executor tools only (server tools are
// opaque to us — the provider handles them). Shape matches the native
// Anthropic tools API: { name, description, input_schema }.
func (r *Registry) AnthropicSchemas() []map[string]any {
	out := make([]map[string]any, 0, len(r.tools))
	for _, name := range r.order {
		t, ok := r.tools[name]
		if !ok {
			continue // server tool — skip
		}
		out = append(out, map[string]any{
			"name":         t.Name(),
			"description":  t.Description(),
			"input_schema": t.Schema(),
		})
	}
	return out
}

// Execute dispatches to the named tool. Unknown names return isError=true
// rather than error, so one bad tool call doesn't kill the run.
func (r *Registry) Execute(ctx context.Context, rc *RunContext, name string, input json.RawMessage) (string, bool, error) {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("unknown tool: %s", name), true, nil
	}
	return t.Execute(ctx, rc, input)
}

// BuildRegistryFor returns a registry populated with the base tools plus any
// conditional tools appropriate for the project's state.
//
// Note: web_search is not a local executor — it's an Anthropic server-side
// tool exposed via deps.LLMProvider.WebSearchTool(). If the provider is
// not Anthropic (OpenRouter, Google, etc.) the server tool returns nil and
// the agent runs without web search for that project.
func BuildRegistryFor(project *models.AppProject, deps *RunDependencies) *Registry {
	r := NewRegistry()
	r.Register(ReadFileTool{})
	r.Register(ListFilesTool{})
	r.Register(GrepTool{})
	r.Register(WriteFileTool{})
	r.Register(EditFileTool{})
	r.Register(MultiEditTool{})
	r.Register(DeleteFileTool{})
	r.Register(GenerateImageTool{Deps: deps})
	r.Register(ReadAttachmentTool{Deps: deps})

	if deps != nil && deps.LLMProvider != nil {
		if ws := deps.LLMProvider.WebSearchTool(); ws != nil {
			r.AddServerTool(ServerToolRef{Tool: ws, DisplayName: "web_search"})
		}
	}

	// Database tools are registered unconditionally so the model has a
	// stable tool list across the run. provision_database is the entry
	// point when no DB exists yet; execute_sql is the ad-hoc query path
	// once one does. Each tool's internal state check (NeonConnString
	// lookup) decides whether a particular call is valid at execution
	// time. projectHasDatabase still gates the DatabaseAddendum in
	// context.go so the addendum only injects after a DB exists.
	r.Register(ProvisionDatabaseTool{Deps: deps})
	r.Register(ExecuteSQLTool{Deps: deps})

	// AI gateway setup is registered unconditionally so the model can opt
	// into AI on demand. The tool's idempotency check (Settings["has_ai"])
	// rejects re-runs once it's wired. projectHasAI still gates the
	// AIAddendum in context.go so the addendum only injects after setup.
	r.Register(SetupAIGatewayTool{Deps: deps})

	if project != nil && project.ProjectType == "mobile_app" {
		r.Register(ConfigureMobileBridgeTool{Deps: deps})
	}
	if project != nil && project.ProjectType == "ecommerce" {
		r.Register(ListProductsTool{})
		r.Register(CreateProductTool{})
		r.Register(UpdateProductTool{})
		r.Register(DeleteProductTool{})
		r.Register(BulkCreateProductsTool{})
		r.Register(ListCategoriesTool{})
		r.Register(CreateCategoryTool{})
		r.Register(UpdateCategoryTool{})
		r.Register(DeleteCategoryTool{})
		r.Register(ListDiscountsTool{})
		r.Register(CreateDiscountTool{})
		r.Register(UpdateDiscountTool{})
		r.Register(DeleteDiscountTool{})
		r.Register(GetStoreTool{})
		r.Register(UpdateStoreSettingsTool{})
	}
	return r
}

// projectHasDatabase returns true if the project has a connected database
// (signaled by a boolean in project.Settings). The actual connection lookup
// still runs inside the execute_sql tool at call time — this predicate is
// a cheap flag for deciding whether to register the tool at all.
func projectHasDatabase(project *models.AppProject) bool {
	if project == nil || project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["has_database"].(bool); ok {
		return v
	}
	return false
}
