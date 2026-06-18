//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/knid/wemob/internal/services/gitrepo"
	"github.com/knid/wemob/internal/utils"
	"github.com/stretchr/testify/require"
)

type runIntegrationSecretManager struct{}

func (runIntegrationSecretManager) GetSecret(string) string { return "" }
func (runIntegrationSecretManager) GetEnv() string          { return "test" }

type runIntegrationEmitter struct {
	mu     sync.Mutex
	events []Event
}

func (e *runIntegrationEmitter) Emit(eventType string, data any) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, Event{Type: eventType, Data: data})
	return true
}

func (e *runIntegrationEmitter) Buffer() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Event, len(e.events))
	copy(out, e.events)
	return out
}

func (e *runIntegrationEmitter) Close() {}

func (e *runIntegrationEmitter) has(eventType string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, got := range e.events {
		if got.Type == eventType {
			return true
		}
	}
	return false
}

func (e *runIntegrationEmitter) types() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, 0, len(e.events))
	for _, event := range e.events {
		out = append(out, event.Type)
	}
	return out
}

func TestRunIntegrationReadOnlyPlanPersistsRunAndAssistant(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Plan: keep it simple.\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":5,\"total_tokens\":17}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(llm.Close)

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-plan")
	emitter := &runIntegrationEmitter{}
	deps := newRunIntegrationDeps(db, gitSvc, llm.URL)

	err := Run(context.Background(), deps, project, user, "/plan add a settings page", TurnAttachments{}, emitter)
	require.NoError(t, err)
	require.True(t, emitter.has(EventRunStart))
	require.True(t, emitter.has(EventDone))

	runs := make([]models.AppAgentRun, 0)
	err = db.DB.NewSelect().Model(&runs).Where("app_project_id = ?", project.ID).Order("created_at ASC").Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, models.AgentRunStatusCompleted, runs[0].Status)
	require.Equal(t, 1, runs[0].Turns)
	require.Equal(t, 12, runs[0].PromptTokens)
	require.Equal(t, 5, runs[0].CompletionTokens)
	require.NotEmpty(t, runs[0].PreCommit)
	require.NotEmpty(t, runs[0].FinalCommit)

	messages, err := db.GetRecentChatMessages(project.ID, 10)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "user", messages[0].Role)
	require.Equal(t, "/plan add a settings page", messages[0].Content)
	require.Equal(t, "assistant", messages[1].Role)
	require.Equal(t, "Plan: keep it simple.", messages[1].Content)
	require.Contains(t, string(messages[1].Parts), "Plan: keep it simple.")
}

func TestRunIntegrationWriteFilesExecutesToolAndCommits(t *testing.T) {
	var requests int
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "/chat/completions", r.URL.Path)
		w.Header().Set("Content-Type", "text/event-stream")
		if requests == 1 {
			writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index": 0,
					"id":    "call_write_1",
					"type":  "function",
					"function": map[string]any{
						"name": "write_files",
						"arguments": mustJSONString(t, map[string]any{
							"files": []any{map[string]any{
								"path":        "frontend/src/plan.ts",
								"content":     "export const plan = 'Keep it simple.';\n",
								"description": "write plan notes",
							}},
						}),
					},
				}},
			}}}})
			writeSSE(t, w, map[string]any{
				"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
				"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 8, "total_tokens": 28},
			})
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": "Created frontend/src/plan.ts."}}}})
		writeSSE(t, w, map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 30, "completion_tokens": 4, "total_tokens": 34},
		})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(llm.Close)

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-write")
	emitter := &runIntegrationEmitter{}
	deps := newRunIntegrationDeps(db, gitSvc, llm.URL)

	err := Run(context.Background(), deps, project, user, "create plan notes", TurnAttachments{}, emitter)
	require.NoError(t, err)
	require.Equal(t, 2, requests)
	require.True(t, emitter.has(EventToolCallStart), "events: %v", emitter.types())
	require.True(t, emitter.has(EventToolCallResult), "events: %v", emitter.types())
	require.True(t, emitter.has(EventFileWritten), "events: %v", emitter.types())
	require.True(t, emitter.has(EventDone), "events: %v", emitter.types())

	repo, err := gitSvc.Open(project.ID.String())
	require.NoError(t, err)
	content, err := repo.ReadFile("frontend/src/plan.ts")
	require.NoError(t, err)
	require.Equal(t, "export const plan = 'Keep it simple.';\n", string(content))

	var run models.AppAgentRun
	err = db.DB.NewSelect().Model(&run).Where("app_project_id = ?", project.ID).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, models.AgentRunStatusCompleted, run.Status)
	require.Equal(t, 1, run.ToolCallCount)
	require.Equal(t, 50, run.PromptTokens)
	require.Equal(t, 12, run.CompletionTokens)
	require.NotEqual(t, run.PreCommit, run.FinalCommit)

	messages, err := db.GetRecentChatMessages(project.ID, 10)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "assistant", messages[1].Role)
	require.Equal(t, "Created frontend/src/plan.ts.", messages[1].Content)
	require.Contains(t, string(messages[1].ToolCalls), "write_files")
	require.Contains(t, string(messages[1].Parts), "tool_call")
}

func TestRunIntegrationSaaSLandingPageMultiTurnJourney(t *testing.T) {
	const initialPage = `export default function App() {
  return (
    <main className="bg-slate-950 text-white">
      <section className="hero">
        <p className="eyebrow">AcmeOps SaaS</p>
        <h1>Operate your SaaS from one calm dashboard.</h1>
        <p className="accent">Launch, observe, and support customers without tool sprawl.</p>
      </section>
    </main>
  )
}
`
	const colorPage = `export default function App() {
  return (
    <main className="bg-emerald-950 text-white">
      <section className="hero">
        <p className="eyebrow">AcmeOps SaaS</p>
        <h1>Operate your SaaS from one calm dashboard.</h1>
        <p className="accent">Launch, observe, and support customers without tool sprawl.</p>
      </section>
    </main>
  )
}
`
	const featurePage = `export default function App() {
  return (
    <main className="bg-emerald-950 text-white">
      <section className="hero">
        <p className="eyebrow">AcmeOps SaaS</p>
        <h1>Operate your SaaS from one calm dashboard.</h1>
        <p className="accent">Launch, observe, and support customers without tool sprawl.</p>
      </section>
      <section className="feature-grid">
        <article>Usage-based billing alerts</article>
        <article>Customer health scoring</article>
        <article>Incident-ready audit trail</article>
      </section>
    </main>
  )
}
`

	var requestBodies []string
	var requestCount int
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBodies = append(requestBodies, string(body))
		requestCount++
		w.Header().Set("Content-Type", "text/event-stream")

		switch requestCount {
		case 1:
			streamToolCall(t, w, "call_design_1", "create_design", map[string]any{
				"register":           "brand",
				"design_direction":   "A focused SaaS landing page with a quiet operational feel.",
				"color_system":       "Deep slate base, white text, restrained emerald accent color.",
				"typography":         "Confident headline scale, compact supporting copy, readable body text.",
				"layout":             "Single hero first, constrained width, visible next section on scroll.",
				"components":         "Hero, feature grid, concise CTA states.",
				"imagery":            "Product UI screenshots only, avoid abstract decoration.",
				"accessibility":      "High contrast, clear focus states, no text overlap.",
				"forbidden_patterns": "No purple gradients, no decorative blobs, no nested cards.",
			})
		case 2:
			streamToolCall(t, w, "call_write_1", "write_files", map[string]any{
				"files": []any{map[string]any{
					"path":      "frontend/src/App.tsx",
					"content":   initialPage,
					"overwrite": true,
					"reason":    "replace starter app with initial SaaS landing page",
				}},
			})
		case 3:
			streamText(t, w, "Created the SaaS landing page.")
		case 4:
			streamToolCall(t, w, "call_read_color", "read_files", map[string]any{
				"paths": []string{"frontend/src/App.tsx"},
			})
		case 5:
			streamToolCall(t, w, "call_edit_color", "edit_file", map[string]any{
				"path":        "frontend/src/App.tsx",
				"old_string":  `bg-slate-950`,
				"new_string":  `bg-emerald-950`,
				"description": "switch landing page color theme to emerald",
			})
		case 6:
			streamText(t, w, "Updated the landing page color.")
		case 7:
			streamToolCall(t, w, "call_read_feature", "read_files", map[string]any{
				"paths": []string{"frontend/src/App.tsx"},
			})
		case 8:
			streamToolCall(t, w, "call_edit_feature", "edit_file", map[string]any{
				"path": "frontend/src/App.tsx",
				"old_string": `      </section>
    </main>`,
				"new_string": `      </section>
      <section className="feature-grid">
        <article>Usage-based billing alerts</article>
        <article>Customer health scoring</article>
        <article>Incident-ready audit trail</article>
      </section>
    </main>`,
				"description": "add SaaS feature grid",
			})
		case 9:
			streamText(t, w, "Added the feature grid.")
		default:
			t.Fatalf("unexpected LLM request %d", requestCount)
		}
	}))
	t.Cleanup(llm.Close)

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-saas-journey")
	project.Status = "draft"
	_, err := db.UpdateAppProject(project)
	require.NoError(t, err)
	deps := newRunIntegrationDeps(db, gitSvc, llm.URL)

	err = Run(context.Background(), deps, project, user, "create a landing page for my saas", TurnAttachments{}, &runIntegrationEmitter{})
	require.NoError(t, err)
	require.Equal(t, "ready", project.Status)

	repo, err := gitSvc.Open(project.ID.String())
	require.NoError(t, err)
	headAfterInitial, err := repo.Head()
	require.NoError(t, err)
	assertRepoFile(t, repo, "frontend/src/App.tsx", []string{"AcmeOps SaaS", "bg-slate-950"})
	assertRepoFile(t, repo, "DESIGN.md", []string{"Design Direction", "Color System", "Typography"})

	err = Run(context.Background(), deps, project, user, "update color to emerald", TurnAttachments{}, &runIntegrationEmitter{})
	require.NoError(t, err)
	headAfterColor, err := repo.Head()
	require.NoError(t, err)
	require.NotEqual(t, headAfterInitial, headAfterColor)
	assertRepoFile(t, repo, "frontend/src/App.tsx", []string{"bg-emerald-950"})

	err = Run(context.Background(), deps, project, user, "add feature grid for billing alerts and customer health", TurnAttachments{}, &runIntegrationEmitter{})
	require.NoError(t, err)
	headAfterFeature, err := repo.Head()
	require.NoError(t, err)
	require.NotEqual(t, headAfterColor, headAfterFeature)
	assertRepoFile(t, repo, "frontend/src/App.tsx", []string{"Usage-based billing alerts", "Customer health scoring", "Incident-ready audit trail"})

	messages, err := db.GetRecentChatMessages(project.ID, 10)
	require.NoError(t, err)
	require.Len(t, messages, 6)
	require.Equal(t, []string{"user", "assistant", "user", "assistant", "user", "assistant"}, chatRoles(messages))

	var runs []models.AppAgentRun
	err = db.DB.NewSelect().Model(&runs).Where("app_project_id = ?", project.ID).Order("created_at ASC").Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, runs, 3)
	require.Equal(t, []int{2, 2, 2}, []int{runs[0].ToolCallCount, runs[1].ToolCallCount, runs[2].ToolCallCount})

	require.GreaterOrEqual(t, len(requestBodies), 9)
	require.Contains(t, requestBodies[3], "Created the SaaS landing page.")
	require.Contains(t, requestBodies[3], "frontend/src/App.tsx")
	require.Contains(t, requestBodies[6], "Updated the landing page color.")
	require.Contains(t, requestBodies[6], "bg-emerald-950")
}

func TestRunIntegrationToolMatrixReadSearchPatchDelete(t *testing.T) {
	var requestCount int
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "text/event-stream")

		switch requestCount {
		case 1:
			streamToolCall(t, w, "call_list", "list_files", map[string]any{})
		case 2:
			streamToolCall(t, w, "call_grep", "grep", map[string]any{
				"pattern": "Alpha",
				"glob":    "frontend/src/*.tsx",
			})
		case 3:
			streamToolCall(t, w, "call_read", "read_files", map[string]any{
				"paths": []string{"frontend/src/App.tsx", "frontend/src/helper.ts"},
			})
		case 4:
			streamToolCall(t, w, "call_multi", "multi_edit", map[string]any{
				"path": "frontend/src/App.tsx",
				"edits": []any{
					map[string]any{"old_string": "Alpha", "new_string": "Beta"},
					map[string]any{"old_string": "old-tag", "new_string": "new-tag"},
				},
				"description": "update app copy and tag",
			})
		case 5:
			streamToolCall(t, w, "call_multi_files", "multi_edit", map[string]any{
				"files": []any{
					map[string]any{
						"path": "frontend/src/App.tsx",
						"edits": []any{
							map[string]any{"old_string": "PatchMe", "new_string": "Patched App"},
						},
					},
					map[string]any{
						"path": "frontend/src/helper.ts",
						"edits": []any{
							map[string]any{"old_string": "HelperPatch", "new_string": "Patched Helper"},
						},
					},
				},
			})
		case 6:
			streamToolCall(t, w, "call_delete", "delete_file", map[string]any{
				"path":   "frontend/src/remove-me.ts",
				"reason": "cleanup obsolete helper",
			})
		case 7:
			streamText(t, w, "Updated searched files and removed obsolete helper.")
		default:
			t.Fatalf("unexpected LLM request %d", requestCount)
		}
	}))
	t.Cleanup(llm.Close)

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-tool-matrix")
	repo, err := gitSvc.Open(project.ID.String())
	require.NoError(t, err)
	require.NoError(t, repo.WriteFile("frontend/src/App.tsx", []byte("export const title = 'Alpha'\nexport const tag = 'old-tag'\nexport const patch = 'PatchMe'\n")))
	require.NoError(t, repo.WriteFile("frontend/src/helper.ts", []byte("export const helper = 'HelperPatch'\n")))
	require.NoError(t, repo.WriteFile("frontend/src/remove-me.ts", []byte("export const removeMe = true\n")))

	emitter := &runIntegrationEmitter{}
	deps := newRunIntegrationDeps(db, gitSvc, llm.URL)
	err = Run(context.Background(), deps, project, user, "update searched files and remove obsolete helper", TurnAttachments{}, emitter)
	require.NoError(t, err)
	require.Equal(t, 7, requestCount)
	require.True(t, emitter.has(EventFileWritten), "events: %v", emitter.types())
	require.True(t, emitter.has(EventFileDeleted), "events: %v", emitter.types())

	assertRepoFile(t, repo, "frontend/src/App.tsx", []string{"Beta", "new-tag", "Patched App"})
	assertRepoFile(t, repo, "frontend/src/helper.ts", []string{"Patched Helper"})
	_, err = repo.ReadFile("frontend/src/remove-me.ts")
	require.Error(t, err)

	var run models.AppAgentRun
	err = db.DB.NewSelect().Model(&run).Where("app_project_id = ?", project.ID).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, models.AgentRunStatusCompleted, run.Status)
	require.Equal(t, 6, run.ToolCallCount)

	messages, err := db.GetRecentChatMessages(project.ID, 10)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Contains(t, string(messages[1].ToolCalls), "list_files")
	require.Contains(t, string(messages[1].ToolCalls), "grep")
	require.Contains(t, string(messages[1].ToolCalls), "read_files")
	require.Contains(t, string(messages[1].ToolCalls), "multi_edit")
	require.NotContains(t, string(messages[1].ToolCalls), "patch_files")
	require.Contains(t, string(messages[1].ToolCalls), "delete_file")
}

func TestRunIntegrationProviderFailurePersistsFailedRunAndErrorMessage(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}))
	t.Cleanup(llm.Close)

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-provider-failure")
	emitter := &runIntegrationEmitter{}
	deps := newRunIntegrationDeps(db, gitSvc, llm.URL)

	err := Run(context.Background(), deps, project, user, "tell me what changed", TurnAttachments{}, emitter)
	require.Error(t, err)
	require.True(t, emitter.has(EventRunStart))
	require.True(t, emitter.has(EventError))

	var run models.AppAgentRun
	err = db.DB.NewSelect().Model(&run).Where("app_project_id = ?", project.ID).Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, models.AgentRunStatusFailed, run.Status)
	require.Contains(t, run.Error, "LLM call failed")
	require.NotEmpty(t, run.PreCommit)

	messages, err := db.GetRecentChatMessages(project.ID, 10)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "assistant", messages[1].Role)
	require.NotEmpty(t, messages[1].ErrorKind)
	require.Contains(t, messages[1].Content, "LLM call failed")
}

func streamToolCall(t *testing.T, w http.ResponseWriter, id, name string, args any) {
	t.Helper()
	writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{
			"index": 0,
			"id":    id,
			"type":  "function",
			"function": map[string]any{
				"name":      name,
				"arguments": mustJSONString(t, args),
			},
		}},
	}}}})
	writeSSE(t, w, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "tool_calls"}},
		"usage":   map[string]any{"prompt_tokens": 20, "completion_tokens": 8, "total_tokens": 28},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func streamText(t *testing.T, w http.ResponseWriter, text string) {
	t.Helper()
	writeSSE(t, w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
	writeSSE(t, w, map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 30, "completion_tokens": 4, "total_tokens": 34},
	})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func assertRepoFile(t *testing.T, repo *gitrepo.Repo, path string, needles []string) {
	t.Helper()
	b, err := repo.ReadFile(path)
	require.NoError(t, err)
	content := string(b)
	for _, needle := range needles {
		require.Contains(t, content, needle)
	}
}

func chatRoles(messages []models.AppChatMessage) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}

func TestRunIntegrationOpenRouterSmoke(t *testing.T) {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" || os.Getenv("WEMOB_REAL_LLM_SMOKE") != "1" {
		t.Skip("set WEMOB_REAL_LLM_SMOKE=1 and OPENROUTER_API_KEY to run real OpenRouter smoke")
	}

	db, gitSvc, user, project := newRunIntegrationFixture(t, "agent-run-openrouter-smoke")
	emitter := &runIntegrationEmitter{}
	deps := newRunIntegrationDeps(db, gitSvc, "https://openrouter.ai/api/v1")
	deps.LLMProvider.APIKey = apiKey
	deps.LLMProvider.ConfiguredModel = os.Getenv("OPENROUTER_MODEL")
	if deps.LLMProvider.ConfiguredModel == "" {
		deps.LLMProvider.ConfiguredModel = "minimax/minimax-m3"
	}

	err := Run(context.Background(), deps, project, user, "/plan add a simple settings page", TurnAttachments{}, emitter)
	require.NoError(t, err)
	require.True(t, emitter.has(EventDone))
}

func newRunIntegrationFixture(t *testing.T, slug string) (*database.Database, *gitrepo.Service, *models.User, *models.AppProject) {
	t.Helper()
	db := database.NewTestDB(t,
		(*models.User)(nil),
		(*models.AppProject)(nil),
		(*models.AppAgentRun)(nil),
		(*models.AppChatMessage)(nil),
	)

	user := &models.User{
		ID:       uuid.New(),
		Name:     "Integration User",
		Email:    slug + "@example.com",
		Password: "x",
	}
	_, err := db.DB.NewInsert().Model(user).Exec(context.Background())
	require.NoError(t, err)

	project := &models.AppProject{
		ID:              uuid.New(),
		UserID:          user.ID,
		OrganizationID:  uuid.New(),
		Name:            "Integration Project",
		Slug:            slug,
		Prompt:          "Existing app",
		ProjectType:     models.ProjectTypeApp,
		FrontendRuntime: models.FrontendRuntimeViteReact,
		FrontendHosting: models.FrontendHostingNone,
		BackendRuntime:  models.BackendRuntimeNone,
		Status:          "ready",
		Theme:           []byte("{}"),
		DataModels:      []byte("[]"),
		Settings:        map[string]any{},
	}
	_, err = db.DB.NewInsert().Model(project).Exec(context.Background())
	require.NoError(t, err)

	gitSvc := gitrepo.NewService(t.TempDir())
	repo, err := gitSvc.OpenOrInit(project.ID.String())
	require.NoError(t, err)
	require.NoError(t, gitConfig(repo.Path, "user.email", "agent-test@example.com"))
	require.NoError(t, gitConfig(repo.Path, "user.name", "Agent Test"))
	return db, gitSvc, user, project
}

func newRunIntegrationDeps(db *database.Database, gitSvc *gitrepo.Service, llmBaseURL string) *RunDependencies {
	return &RunDependencies{
		DB:            db,
		GitRepo:       gitSvc,
		SecretManager: runIntegrationSecretManager{},
		LLMProvider: &utils.LLMProvider{
			Name:            "openrouter",
			BaseURL:         llmBaseURL,
			APIKey:          "test-key",
			ConfiguredModel: "test-model",
		},
		UserPlanName: "pro",
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func gitConfig(repoPath, key, value string) error {
	cmd := exec.Command("git", "config", key, value)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}
