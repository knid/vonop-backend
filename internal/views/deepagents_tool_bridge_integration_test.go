//go:build integration

package views

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/knid/wemob/internal/controllers"
	"github.com/knid/wemob/internal/database"
	"github.com/knid/wemob/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertProject(t *testing.T, db *database.Database, project *models.AppProject) {
	_, err := db.DB.NewInsert().Model(project).Exec(context.Background())
	require.NoError(t, err)
}

func insertUser(t *testing.T, db *database.Database, user *models.User) {
	_, err := db.DB.NewInsert().Model(user).Exec(context.Background())
	require.NoError(t, err)
}

func insertAgentRun(t *testing.T, db *database.Database, projectID, userID uuid.UUID, status string) *models.AppAgentRun {
	run := &models.AppAgentRun{
		ID:           uuid.New(),
		AppProjectID: projectID,
		UserID:       userID,
		Status:       status,
		PreCommit:    "0000000000000000000000000000000000000000",
		Model:        "test-model",
		Provider:     "test-provider",
		RuntimeKind:  "deepagents",
		CreatedAt:    time.Now(),
	}
	_, err := db.DB.NewInsert().Model(run).Exec(context.Background())
	require.NoError(t, err)
	return run
}

func makeToolBody(projectID, userID uuid.UUID) []byte {
	b, _ := json.Marshal(map[string]any{
		"job_id":       uuid.New().String(),
		"project_id":   projectID.String(),
		"user_id":      userID.String(),
		"tool_call_id": uuid.New().String(),
		"arguments":    map[string]any{},
	})
	return b
}

func makeToolRequest(method, runID, toolName string, body []byte, token string) *http.Request {
	r := httptest.NewRequest(method, "/internal/v1/agent-runs/"+runID+"/tools/"+toolName+"/execute", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestDeepAgentsToolExecute_WrongTokenRejected(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	v := &HttpViews{Controller: c}
	body := makeToolBody(uuid.New(), uuid.New())
	r := makeToolRequest(http.MethodPost, uuid.New().String(), "sandbox_status", body, "wrong-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeepAgentsToolExecute_MissingTokenRejected(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	v := &HttpViews{Controller: c}
	body := makeToolBody(uuid.New(), uuid.New())
	r := makeToolRequest(http.MethodPost, uuid.New().String(), "sandbox_status", body, "")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeepAgentsToolExecute_CompletedRunBlocked(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusCompleted)

	v := &HttpViews{Controller: c}
	body := makeToolBody(project.ID, user.ID)
	r := makeToolRequest(http.MethodPost, run.ID.String(), "sandbox_status", body, "correct-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDeepAgentsToolExecute_FailedRunBlocked(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusFailed)

	v := &HttpViews{Controller: c}
	body := makeToolBody(project.ID, user.ID)
	r := makeToolRequest(http.MethodPost, run.ID.String(), "sandbox_status", body, "correct-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestDeepAgentsToolExecute_MismatchedProjectRejected(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusRunning)

	v := &HttpViews{Controller: c}
	body := makeToolBody(uuid.New(), user.ID)
	r := makeToolRequest(http.MethodPost, run.ID.String(), "sandbox_status", body, "correct-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeepAgentsToolExecute_MismatchedUserRejected(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusRunning)

	v := &HttpViews{Controller: c}
	body := makeToolBody(project.ID, uuid.New())
	r := makeToolRequest(http.MethodPost, run.ID.String(), "sandbox_status", body, "correct-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestDeepAgentsToolExecute_FileToolsRejected(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusRunning)

	v := &HttpViews{Controller: c}
	body := makeToolBody(project.ID, user.ID)

	for _, tool := range []string{"write_file", "edit_file", "read_files", "delete_file", "create_design", "update_todos", "load_skill"} {
		r := makeToolRequest(http.MethodPost, run.ID.String(), tool, body, "correct-token")
		w := httptest.NewRecorder()
		v.HandleDeepAgentsToolExecute(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, "tool %q should be rejected", tool)
	}
}

func TestDeepAgentsToolExecute_RunningRunAcceptsAllowedTool(t *testing.T) {
	db := database.NewTestDB(t, &models.User{}, &models.AppProject{}, &models.AppAgentRun{})
	c, _ := controllers.NewTestController(t, db, controllers.TestControllerOptions{
		Secrets: map[string]string{"DEEPAGENTS_INTERNAL_TOKEN": "correct-token"},
	})

	project := &models.AppProject{ID: uuid.New(), ProjectType: "app"}
	user := &models.User{ID: uuid.New()}
	insertProject(t, db, project)
	insertUser(t, db, user)
	run := insertAgentRun(t, db, project.ID, user.ID, models.AgentRunStatusRunning)

	v := &HttpViews{Controller: c}
	body := makeToolBody(project.ID, user.ID)
	r := makeToolRequest(http.MethodPost, run.ID.String(), "sandbox_read_logs", body, "correct-token")

	w := httptest.NewRecorder()
	v.HandleDeepAgentsToolExecute(w, r)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, w.Code,
		"expected 200 or 404 for allowed tool on running run, got %d: %s", w.Code, w.Body.String())
}
