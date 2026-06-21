package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/middleware"
	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
)

// --- requireMethod ---

func TestRequireMethod_Allowed(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	ok := requireMethod(w, r, http.MethodGet)
	assert.True(t, ok)
	assert.Empty(t, w.Body.String()) // no error written
}

func TestRequireMethod_MultipleAllowed(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	ok := requireMethod(w, r, http.MethodGet, http.MethodPost)
	assert.True(t, ok)
}

func TestRequireMethod_Denied(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	ok := requireMethod(w, r, http.MethodGet)
	assert.False(t, ok)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Method not allowed", resp["error"])
}

// --- writeJSON ---

func TestWriteJSON_SetsContentTypeAndStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusOK, map[string]string{"hello": "world"})

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "world", resp["hello"])
}

func TestWriteJSON_CreatedStatus(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, http.StatusCreated, map[string]int{"id": 42})
	assert.Equal(t, http.StatusCreated, w.Code)
}

// --- decodeJSON ---

func TestDecodeJSON_ValidBody(t *testing.T) {
	body := strings.NewReader(`{"name":"test","value":123}`)
	r := httptest.NewRequest(http.MethodPost, "/test", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	var req struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	ok := decodeJSON(w, r, &req)
	assert.True(t, ok)
	assert.Equal(t, "test", req.Name)
	assert.Equal(t, 123, req.Value)
}

func TestDecodeJSON_InvalidBody(t *testing.T) {
	body := strings.NewReader(`{invalid json}`)
	r := httptest.NewRequest(http.MethodPost, "/test", body)
	w := httptest.NewRecorder()

	var req struct{}
	ok := decodeJSON(w, r, &req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- validateAndResolvePath ---

func TestValidateAndResolvePath_ValidPath(t *testing.T) {
	w := httptest.NewRecorder()
	basePath := t.TempDir()
	absPath, ok := validateAndResolvePath(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody), basePath, "test.txt")
	assert.True(t, ok)
	assert.True(t, strings.HasPrefix(absPath, basePath))
}

func TestValidateAndResolvePath_TraversalPath(t *testing.T) {
	w := httptest.NewRecorder()
	basePath := t.TempDir()
	absPath, ok := validateAndResolvePath(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody), basePath, "../../etc/passwd")
	assert.False(t, ok)
	assert.Empty(t, absPath)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- resolveAgentConfig ---

func TestResolveAgentConfig_DefaultAgent(t *testing.T) {
	origAgents := model.Agents
	origDefault := model.DefaultAgentID
	defer func() {
		model.Agents = origAgents
		model.DefaultAgentID = origDefault
	}()

	model.DefaultAgentID = "test-agent"
	model.Agents = map[string]*model.Agent{
		"test-agent": {ID: "test-agent", Backend: "claude", Models: []model.AgentModel{{ID: "sonnet", Name: "Sonnet", Default: true}}, SystemPrompt: "be helpful", Command: "claude"},
	}

	backend, agentModel, sysPrompt, cmd, ok := resolveAgentConfig("")
	assert.True(t, ok)
	assert.Equal(t, "claude", backend)
	assert.Equal(t, "sonnet", agentModel)
	assert.Equal(t, "be helpful", sysPrompt)
	assert.Equal(t, "claude", cmd)
}

func TestResolveAgentConfig_SpecificAgent(t *testing.T) {
	origAgents := model.Agents
	defer func() { model.Agents = origAgents }()

	model.Agents = map[string]*model.Agent{
		"coder": {ID: "coder", Backend: "codebuddy", Models: []model.AgentModel{{ID: "glm-5.1", Name: "GLM 5.1", Default: true}}, SystemPrompt: "code", Command: "cb"},
	}

	backend, agentModel, _, _, ok := resolveAgentConfig("coder")
	assert.True(t, ok)
	assert.Equal(t, "codebuddy", backend)
	assert.Equal(t, "glm-5.1", agentModel)
}

func TestResolveAgentConfig_NoDefaultNoAgents(t *testing.T) {
	origAgents := model.Agents
	origDefault := model.DefaultAgentID
	defer func() {
		model.Agents = origAgents
		model.DefaultAgentID = origDefault
	}()

	model.DefaultAgentID = ""
	model.Agents = map[string]*model.Agent{}

	_, _, _, _, ok := resolveAgentConfig("")
	assert.False(t, ok)
}

func TestResolveAgentConfig_UnknownAgent(t *testing.T) {
	origAgents := model.Agents
	defer func() { model.Agents = origAgents }()

	model.Agents = map[string]*model.Agent{}

	_, _, _, _, ok := resolveAgentConfig("nonexistent")
	assert.False(t, ok)
}

// --- requireSessionID ---

func TestRequireSessionID_FromQueryParam(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test?session_id=abc-123", http.NoBody)
	sessionID, ok := requireSessionID(w, r)
	assert.True(t, ok)
	assert.Equal(t, "abc-123", sessionID)
}

func TestRequireSessionID_FromCookie(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("chat_session_id"), Value: "cookie-session"})
	sessionID, ok := requireSessionID(w, r)
	assert.True(t, ok)
	assert.Equal(t, "cookie-session", sessionID)
}

func TestRequireSessionID_Missing(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	sessionID, ok := requireSessionID(w, r)
	assert.False(t, ok)
	assert.Empty(t, sessionID)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- requireProject (existing, verify unchanged) ---

func TestRequireProject_Valid(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: "/tmp"})
	projectPath, ok := requireProject(w, r)
	assert.True(t, ok)
	assert.Equal(t, "/tmp", projectPath)
}

func TestRequireProject_Missing(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	projectPath, ok := requireProject(w, r)
	assert.False(t, ok)
	assert.Empty(t, projectPath)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// --- Integration: helpers work together in a handler ---

func TestHelperIntegration_MethodGuardAndWriteJSON(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		projectPath, ok := requireProject(w, r)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"project": projectPath})
	}

	// Valid request
	r := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	r.AddCookie(&http.Cookie{Name: model.ScopedCookieName("clawbench_project"), Value: "/tmp/project"})
	w := httptest.NewRecorder()
	handler(w, r)
	assert.Equal(t, http.StatusOK, w.Code)

	// Wrong method
	r2 := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	w2 := httptest.NewRecorder()
	handler(w2, r2)
	assert.Equal(t, http.StatusMethodNotAllowed, w2.Code)
}

// Placeholder to suppress unused import warning
var _ = middleware.GetProjectFromCookie
