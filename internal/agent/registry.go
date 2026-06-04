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

// Execute dispatches to the named tool. Unknown names return isError=true
// rather than error, so one bad tool call doesn't kill the run.
func (r *Registry) Execute(ctx context.Context, rc *RunContext, name string, input json.RawMessage) (string, bool, error) {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("unknown tool: %s", name), true, nil
	}
	if decision := ApplyToolGuardrails(rc, name, input); !decision.Allowed {
		msg := decision.Message
		if msg == "" {
			msg = "tool blocked by agent policy"
		}
		return "<tool_output tool=\"" + name + "\">" + escapeUntrustedTag(msg, "tool_output") + "</tool_output>", true, nil
	}
	result, isErr, err := t.Execute(ctx, rc, input)
	// read_attachment wraps its body in <attachment> itself (with filename/mime
	// metadata); everything else gets the generic <tool_output> wrap. Both are
	// defense-in-depth boundaries the system prompt teaches the model to treat
	// as untrusted data.
	if name != "read_attachment" {
		result = "<tool_output tool=\"" + name + "\">" + escapeUntrustedTag(result, "tool_output") + "</tool_output>"
	}
	return result, isErr, err
}

// BuildRegistryFor returns a registry populated with the base tools plus any
// conditional tools appropriate for the project's state.
//
// Note: web_search is not a local executor — it's an Anthropic server-side
// tool exposed via deps.LLMProvider.WebSearchTool(). If the provider is
// not Anthropic (OpenRouter, Google, etc.) the server tool returns nil and
// the agent runs without web search for that project.
func BuildRegistryFor(project *models.AppProject, deps *RunDependencies, model string) *Registry {
	profile := ResolveAgentProfile(project)
	if profile != nil && profile.BuildRegistry != nil {
		return profile.BuildRegistry(project, deps, model)
	}
	return buildWebBuilderRegistry(project, deps, model)
}

func buildWebBuilderRegistry(project *models.AppProject, deps *RunDependencies, model string) *Registry {
	r := NewRegistry()
	registerBaseBuilderTools(r, deps)
	registerServerTools(r, deps, model)
	registerDataAndAITools(r, deps)

	if project == nil || project.Status != "draft" {
		r.Register(&ReadDeployLogsTool{Deps: deps})
		r.Register(&TriggerWebDeployTool{Deps: deps})
	}
	if project != nil && project.ProjectType == "ecommerce" {
		r.Register(SetupStoreTool{Deps: deps})
	}
	return r
}

func buildMobileBuilderRegistry(project *models.AppProject, deps *RunDependencies, model string) *Registry {
	r := NewRegistry()
	registerBaseBuilderTools(r, deps)
	registerServerTools(r, deps, model)
	registerDataAndAITools(r, deps)

	r.Register(&ReadMobileConfigTool{})
	r.Register(ProvisionLocalDatabaseTool{Deps: deps})
	if project == nil || project.Status != "draft" {
		r.Register(&ReadBuildLogsTool{Deps: deps})
		r.Register(&TriggerBuildAfterFixTool{Deps: deps})
	}

	if project == nil || project.Status != "draft" {
		r.Register(&ReadDeployLogsTool{Deps: deps})
		r.Register(&TriggerWebDeployTool{Deps: deps})
	}
	return r
}

func registerBaseBuilderTools(r *Registry, deps *RunDependencies) {
	r.Register(ListSkillsTool{})
	r.Register(SearchSkillsTool{})
	r.Register(LoadSkillTool{})
	r.Register(ReadFilesTool{})
	r.Register(ListFilesTool{})
	r.Register(GrepTool{})
	r.Register(UpdateTodosTool{})
	r.Register(WriteFileTool{})
	r.Register(WriteFilesTool{})
	r.Register(EditFileTool{})
	r.Register(MultiEditTool{})
	r.Register(PatchFilesTool{})
	r.Register(DeleteFileTool{})
	r.Register(GenerateImageTool{Deps: deps})
	r.Register(ReadAttachmentTool{Deps: deps})
}

func registerServerTools(r *Registry, deps *RunDependencies, model string) {
	if deps != nil && deps.LLMProvider != nil {
		if ws := deps.LLMProvider.WebSearchTool(model); ws != nil {
			r.AddServerTool(ServerToolRef{Tool: ws, DisplayName: "web_search"})
		}
	}
}

func registerDataAndAITools(r *Registry, deps *RunDependencies) {
	// Database tools are registered unconditionally so the model has a stable
	// tool list across the run. Each tool's internal state check decides
	// whether a particular call is valid at execution time.
	r.Register(ProvisionDatabaseTool{Deps: deps})
	r.Register(ExecuteSQLTool{Deps: deps})

	// AI gateway setup is registered unconditionally so the model can opt into
	// AI on demand. The addendum only injects after setup has persisted.
	r.Register(SetupAIGatewayTool{Deps: deps})
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

// projectHasLocalDatabase returns true if the project has an on-device
// SQLite database wired (signaled by Settings["has_local_database"]).
// Used by context.go to gate LocalDatabaseAddendum.
func projectHasLocalDatabase(project *models.AppProject) bool {
	if project == nil || project.Settings == nil {
		return false
	}
	if v, ok := project.Settings["has_local_database"].(bool); ok {
		return v
	}
	return false
}
