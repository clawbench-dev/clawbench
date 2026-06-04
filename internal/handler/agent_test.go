package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAgentTestEnv creates a temp agents directory with DB records and in-memory agents.
// Returns the temp dir and a teardown function.
func setupAgentTestEnv(t *testing.T) (string, func()) {
	t.Helper()

	// Create temp dir for model cache etc.
	tmpDir := t.TempDir()

	// Save original globals
	origAgents := model.Agents
	origAgentList := model.AgentList
	origDB := service.DB
	origDBRead := service.DBRead

	// Init in-memory SQLite
	db, err := service.InitInMemoryDB()
	require.NoError(t, err)
	service.DB = db
	service.DBRead = db

	// Set up test agents directly in DB
	codebuddyAgent := &model.Agent{
		ID:        "codebuddy",
		Name:      "Test",
		Icon:      "🤖",
		Specialty: "testing",
		Backend:   "codebuddy",
		Models: []model.AgentModel{
			{ID: "glm-5.1", Name: "GLM 5.1", Default: true},
			{ID: "glm-4-flash", Name: "GLM 4 Flash"},
		},
		ThinkingEffortLevels: []string{"low", "medium", "high"},
		Source:               "auto",
	}
	claudeAgent := &model.Agent{
		ID:        "claude",
		Name:      "Claude",
		Icon:      "🧠",
		Specialty: "reasoning",
		Backend:   "claude",
		Models: []model.AgentModel{
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet", Default: true},
		},
		ThinkingEffortLevels: []string{"low", "medium", "high", "xhigh"},
		Source:               "auto",
	}

	require.NoError(t, service.SaveAgent(db, codebuddyAgent))
	require.NoError(t, service.SaveAgent(db, claudeAgent))

	// Load agents into memory
	model.Agents = map[string]*model.Agent{
		"codebuddy": codebuddyAgent,
		"claude":    claudeAgent,
	}
	model.AgentList = []*model.Agent{codebuddyAgent, claudeAgent}

	teardown := func() {
		model.Agents = origAgents
		model.AgentList = origAgentList
		service.DB = origDB
		service.DBRead = origDBRead
		_ = db.Close()
	}

	return tmpDir, teardown
}

func TestAgentGet(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "agents")
	assert.Contains(t, resp, "defaultAgent")
}

func TestAgentPatch_PreferredModel(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":              "codebuddy",
		"preferred_model": "glm-4-flash",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory agent updated
	assert.Equal(t, "glm-4-flash", model.Agents["codebuddy"].PreferredModel)

	// Verify DB updated
	var preferredModel string
	err := service.DB.QueryRow("SELECT preferred_model FROM agents WHERE id = ?", "codebuddy").Scan(&preferredModel)
	require.NoError(t, err)
	assert.Equal(t, "glm-4-flash", preferredModel)
}

func TestAgentPatch_InvalidPreferredModel(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":              "codebuddy",
		"preferred_model": "nonexistent-model",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentPatch_PreferredThinkingEffort(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":                        "codebuddy",
		"preferred_thinking_effort": "high",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory agent updated
	assert.Equal(t, "high", model.Agents["codebuddy"].PreferredThinkingEffort)

	// Verify DB updated
	var preferredThinking string
	err := service.DB.QueryRow("SELECT preferred_thinking_effort FROM agents WHERE id = ?", "codebuddy").Scan(&preferredThinking)
	require.NoError(t, err)
	assert.Equal(t, "high", preferredThinking)
}

func TestAgentPatch_InvalidPreferredThinkingEffort(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":                        "codebuddy",
		"preferred_thinking_effort": "ultra",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentPatch_NonexistentAgent(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":              "nonexistent",
		"preferred_model": "some-model",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAgentPatch_BothFields(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"id":                        "claude",
		"preferred_model":           "claude-sonnet-4-6",
		"preferred_thinking_effort": "xhigh",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory agent updated
	assert.Equal(t, "claude-sonnet-4-6", model.Agents["claude"].PreferredModel)
	assert.Equal(t, "xhigh", model.Agents["claude"].PreferredThinkingEffort)

	// Verify DB updated
	var preferredModel, preferredThinking string
	err := service.DB.QueryRow("SELECT preferred_model, preferred_thinking_effort FROM agents WHERE id = ?", "claude").Scan(&preferredModel, &preferredThinking)
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-6", preferredModel)
	assert.Equal(t, "xhigh", preferredThinking)
}

func TestAgentPatch_ClearPreferredModel(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// First set a preferred model
	model.Agents["codebuddy"].PreferredModel = "glm-4-flash"

	// Now clear it by sending empty string
	body := map[string]any{
		"id":              "codebuddy",
		"preferred_model": "",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", model.Agents["codebuddy"].PreferredModel)
}

func TestAgentPatch_DefaultModelIDRespectsPreferred(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Default without preferred_model should return the default model
	assert.Equal(t, "glm-5.1", model.Agents["codebuddy"].DefaultModelID())

	// Set preferred model
	model.Agents["codebuddy"].PreferredModel = "glm-4-flash"
	assert.Equal(t, "glm-4-flash", model.Agents["codebuddy"].DefaultModelID())

	// BaseModelID always returns the original default, ignoring preference
	assert.Equal(t, "glm-5.1", model.Agents["codebuddy"].BaseModelID())
}

func TestAgentPatch_EffectiveThinkingEffortRespectsPreferred(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Without preferred thinking, returns agent default (empty in test)
	assert.Equal(t, "", model.Agents["codebuddy"].EffectiveThinkingEffort())

	// Set preferred thinking effort
	model.Agents["codebuddy"].PreferredThinkingEffort = "high"
	assert.Equal(t, "high", model.Agents["codebuddy"].EffectiveThinkingEffort())

	// ThinkingEffort (original default) is not modified
	assert.Equal(t, "", model.Agents["codebuddy"].ThinkingEffort)
}

func TestAgentPatch_NoID(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	body := map[string]any{
		"preferred_model": "glm-4-flash",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentPatch_MethodNotAllowed(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestAgentRefreshModels_Success(t *testing.T) {
	tmpDir, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels for testing
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		if spec.Backend == "codebuddy" {
			return []model.AgentModel{
				{ID: "glm-6", Name: "GLM 6", Default: true},
				{ID: "glm-5.1", Name: "GLM 5.1"},
			}
		}
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	// Create model cache dir and set global
	cacheDir := filepath.Join(tmpDir, "model-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	origCacheDir := model.ModelCacheDir
	model.ModelCacheDir = cacheDir
	defer func() { model.ModelCacheDir = origCacheDir }()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	models, ok := resp["models"].([]any)
	require.True(t, ok, "response should contain models array")
	assert.Len(t, models, 2)

	// Verify in-memory agent models were updated
	assert.Equal(t, "glm-6", model.Agents["codebuddy"].Models[0].ID)
	assert.Equal(t, "glm-5.1", model.Agents["codebuddy"].Models[1].ID)

	// Verify cache file was written
	cached := model.ReadModelCache(cacheDir, "codebuddy")
	require.NotNil(t, cached)
	assert.Len(t, cached, 2)
	assert.Equal(t, "glm-6", cached[0].ID)
}

func TestAgentRefreshModels_AgentNotFound(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/agents/nonexistent/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAgentRefreshModels_DiscoveryNotSupported(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Use a fictional backend that has no discovery capability
	model.Agents["unknown"] = &model.Agent{ID: "unknown", Backend: "unknown"}
	model.AgentList = append(model.AgentList, model.Agents["unknown"])

	req := newRequest(t, http.MethodPost, "/api/agents/unknown/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentRefreshModels_DiscoveryFails(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels to return nil (simulating discovery failure)
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	// When discovery returns no models:
	// - If CLI is on PATH but returns empty: 500 (ModelDiscoveryFailed)
	// - If CLI is NOT on PATH: 404 (CLINotFound)
	// CI may not have codebuddy installed, so accept either
	assert.True(t, w.Code == http.StatusInternalServerError || w.Code == http.StatusNotFound,
		"expected 500 or 404, got %d", w.Code)
}

func TestServeAgentSubRoutes_RefreshModels(t *testing.T) {
	tmpDir, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels for testing
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		if spec.Backend == "codebuddy" {
			return []model.AgentModel{{ID: "glm-6", Name: "GLM 6", Default: true}}
		}
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	// Create model cache dir and set global
	cacheDir := filepath.Join(tmpDir, "model-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	origCacheDir := model.ModelCacheDir
	model.ModelCacheDir = cacheDir
	defer func() { model.ModelCacheDir = origCacheDir }()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSubRoutes, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestServeAgentSubRoutes_NotFound(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/agents/codebuddy/something-else", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentSubRoutes, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeAgentRefreshModels_MethodNotAllowed(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServeAgentRefreshModels_EmptyAgentID(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodPost, "/api/agents//refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeAgentRefreshModels_InvalidAgentID(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Path with extra slashes: /api/agents/foo/bar/refresh-models
	req := newRequest(t, http.MethodPost, "/api/agents/foo/bar/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeAgentRefreshModels_CLINotFound(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels to return nil, simulating CLI not available
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	// Use claude agent (which has DiscoverModelsFunc) — CLI likely not on CI
	req := newRequest(t, http.MethodPost, "/api/agents/claude/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	// Should be either 404 (CLINotFound) or 500 (ModelDiscoveryFailed)
	assert.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusInternalServerError,
		"expected 404 or 500, got %d", w.Code)
}

func TestAgentPatch_InvalidJSON(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Send malformed JSON to trigger decodeJSON failure (line 54-56)
	req := httptest.NewRequest(http.MethodPatch, "/api/agents", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAgentPatch_ClearPreferredThinkingEffort(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// First set a preferred thinking effort
	model.Agents["codebuddy"].PreferredThinkingEffort = "high"

	// Now clear it by sending empty string (empty string with no ThinkingEffortLevels should work)
	body := map[string]any{
		"id":                        "codebuddy",
		"preferred_thinking_effort": "",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", model.Agents["codebuddy"].PreferredThinkingEffort)
}

func TestAgentPatch_PreferredModelEmptyString(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Setting preferred_model to empty string should clear it without validation
	body := map[string]any{
		"id":              "codebuddy",
		"preferred_model": "",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", model.Agents["codebuddy"].PreferredModel)
}

func TestServeAgentRefreshModels_SaveAgentDBError(t *testing.T) {
	tmpDir, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels for testing
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		if spec.Backend == "codebuddy" {
			return []model.AgentModel{{ID: "glm-6", Name: "GLM 6", Default: true}}
		}
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	// Create model cache dir and set global
	cacheDir := filepath.Join(tmpDir, "model-cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	origCacheDir := model.ModelCacheDir
	model.ModelCacheDir = cacheDir
	defer func() { model.ModelCacheDir = origCacheDir }()

	// Delete agents table to cause SaveAgent to fail
	_, _ = service.DB.Exec("DROP TABLE agents")

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	// Should still return 200 (DB save failure is logged but not fatal)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory agent models were still updated
	assert.Equal(t, "glm-6", model.Agents["codebuddy"].Models[0].ID)
}

func TestServeAgentRefreshModels_WriteModelCacheError(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Override DiscoverModels for testing
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		if spec.Backend == "codebuddy" {
			return []model.AgentModel{{ID: "glm-6", Name: "GLM 6", Default: true}}
		}
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	// Set cache dir to invalid path to cause WriteModelCache to fail (lines 178-180)
	origCacheDir := model.ModelCacheDir
	model.ModelCacheDir = "/nonexistent/path/that/cannot/be/created"
	defer func() { model.ModelCacheDir = origCacheDir }()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	// Should still return 200 (cache write failure is logged but not fatal)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify in-memory agent models were still updated
	assert.Equal(t, "glm-6", model.Agents["codebuddy"].Models[0].ID)
}

func TestServeAgentRefreshModels_CLINotFoundSpecificError(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Create a custom agent whose CLI command doesn't exist on PATH
	model.Agents["fake-cli"] = &model.Agent{
		ID:      "fake-cli",
		Name:    "Fake CLI",
		Backend: "deepseek", // uses DefaultCmd "deepseek" which is unlikely on test PATH
		Models:  []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.AgentList = append(model.AgentList, model.Agents["fake-cli"])
	require.NoError(t, service.SaveAgent(service.DB, model.Agents["fake-cli"]))

	// Override DiscoverModels to return nil — will hit "no models" path
	origDiscover := model.DiscoverModels
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		return nil
	}
	defer func() { model.DiscoverModels = origDiscover }()

	req := newRequest(t, http.MethodPost, "/api/agents/fake-cli/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgentRefreshModels, req)

	// Should be 404 (CLINotFound) or 500 (ModelDiscoveryFailed) depending on whether CLI exists
	// The key behavior is that it returns an error, not 200
	assert.NotEqual(t, http.StatusOK, w.Code, "should return error when models discovery returns empty")
}

func TestAgentPatch_NoThinkingEffortLevels(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Create an agent with no ThinkingEffortLevels
	model.Agents["nolevels"] = &model.Agent{
		ID:      "nolevels",
		Name:    "No Levels",
		Backend: "test",
		Models:  []model.AgentModel{{ID: "m1", Name: "Model 1", Default: true}},
	}
	model.AgentList = append(model.AgentList, model.Agents["nolevels"])
	require.NoError(t, service.SaveAgent(service.DB, model.Agents["nolevels"]))

	// Setting preferred_thinking_effort on agent with no levels should accept any value
	body := map[string]any{
		"id":                        "nolevels",
		"preferred_thinking_effort": "anything",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "anything", model.Agents["nolevels"].PreferredThinkingEffort)
}

func TestAgentPatch_PatchAgentDBError(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Create a closed DB that will return errors on Exec
	closedDB, err := service.InitInMemoryDB()
	require.NoError(t, err)
	_ = closedDB.Close()

	// Replace service.DB with the closed DB
	origDB := service.DB
	service.DB = closedDB
	defer func() { service.DB = origDB }()

	body := map[string]any{
		"id":              "codebuddy",
		"preferred_model": "glm-4-flash",
	}
	req := newRequest(t, http.MethodPatch, "/api/agents", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ---------- ServeAgents method not allowed ----------

func TestServeAgents_MethodNotAllowed(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodDelete, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- ServeAgentRefreshModels with provider filter ----------

func TestServeAgentRefreshModels_WithProviderFilter(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Save original DiscoverModels and restore later
	origDiscover := model.DiscoverModels
	defer func() { model.DiscoverModels = origDiscover }()

	// Mock DiscoverModels to return models with provider prefix
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		return []model.AgentModel{
			{ID: "openai/gpt-4o", Name: "openai/GPT-4o"},
			{ID: "anthropic/claude-sonnet-4-20250514", Name: "anthropic/Claude Sonnet 4"},
			{ID: "deepseek/deepseek-chat", Name: "deepseek/DeepSeek Chat"},
		}
	}

	// Add agent_api_keys entry using SaveAgentAPIKey (handles encryption + key_nonce)
	require.NoError(t, service.SaveAgentAPIKey(service.DB, "codebuddy", "openai", "", "test-api-key"))

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	req.URL.Path = "/api/agents/codebuddy/refresh-models"
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	models := resp["models"].([]any)
	// Should only return openai/ prefixed models (stripped of prefix)
	assert.NotEmpty(t, models, "should have models after provider filtering")
}

func TestServeAgentRefreshModels_ProviderFilterNoMatch(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	origDiscover := model.DiscoverModels
	defer func() { model.DiscoverModels = origDiscover }()

	// Mock DiscoverModels to return models that DON'T match the provider prefix
	model.DiscoverModels = func(spec model.BackendSpec) []model.AgentModel {
		return []model.AgentModel{
			{ID: "openai/gpt-4o", Name: "openai/GPT-4o"},
			{ID: "anthropic/claude-sonnet-4-20250514", Name: "anthropic/Claude Sonnet 4"},
		}
	}

	// Set up provider that won't match any model prefix
	require.NoError(t, service.SaveAgentAPIKey(service.DB, "codebuddy", "deepseek", "", "test-api-key"))

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	req.URL.Path = "/api/agents/codebuddy/refresh-models"
	w := callHandler(ServeAgentRefreshModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// When no models match the prefix, all discovered models are returned as fallback
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	models := resp["models"].([]any)
	assert.Len(t, models, 2, "should return all models when no prefix matches")
}

func TestServeAgentRefreshModels_KnownModelsFallback(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Set up agent_api_keys entry for a provider with KnownModels (e.g., anthropic)
	require.NoError(t, service.SaveAgentAPIKey(service.DB, "codebuddy", "anthropic", "", "test-api-key"))

	// Make the agent's backend have NO discovery support by temporarily changing it
	origBackend := model.Agents["codebuddy"].Backend
	origModels := model.Agents["codebuddy"].Models
	model.Agents["codebuddy"].Backend = "nondiscoverable"
	model.Agents["codebuddy"].Models = nil
	defer func() {
		model.Agents["codebuddy"].Backend = origBackend
		model.Agents["codebuddy"].Models = origModels
	}()

	req := newRequest(t, http.MethodPost, "/api/agents/codebuddy/refresh-models", nil)
	withAuthCookie(req, model.SessionToken)
	req.URL.Path = "/api/agents/codebuddy/refresh-models"
	w := callHandler(ServeAgentRefreshModels, req)

	// Should fall back to KnownModels from anthropic provider
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	models := resp["models"].([]any)
	assert.NotEmpty(t, models, "should have KnownModels from anthropic provider")
}

// ---------- serveAgentsGet ACP state tests ----------

func TestServeAgentsGet_ACPStateFromPoolCache(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent
	acpAgent := &model.Agent{
		ID:        "acp-agent",
		Name:      "ACP Agent",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-agent"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	// Inject a pool entry with cached state
	pool := ai.GetACPConnectionPool()
	entry := &ai.ACPConnEntry{}
	entry.SetCachedModeState(&ai.ModeState{
		CurrentModeID:  "code",
		AvailableModes: []ai.ModeDef{{ID: "code", Name: "Code"}, {ID: "ask", Name: "Ask"}},
	})
	entry.SetCachedThinkingEffortState(&ai.ThinkingEffortState{
		CurrentID:       "high",
		AvailableLevels: []ai.ThinkingEffortDef{{ID: "low"}, {ID: "high"}},
	})
	entry.SetCachedModelListState(&ai.ModelListState{
		CurrentModelID: "m1",
		Models:         []model.AgentModel{{ID: "acp-m1", Name: "ACP Model 1", Default: true}},
	})
	// Set a client with commands
	client := ai.NewClawBenchACPClient()
	entry.SetClientForTest(client)
	pool.SetEntryForTest("acp-agent", entry)
	defer pool.CloseConnection("acp-agent")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	state, ok := acpStates["acp-agent"].(map[string]any)
	require.True(t, ok, "acpStates should contain acp-agent")

	// Verify mode state from pool cache
	modeState, ok := state["modeState"].(map[string]any)
	require.True(t, ok, "state should contain modeState")
	assert.Equal(t, "code", modeState["currentModeId"])

	// Verify thinking effort state from pool cache
	effortState, ok := state["thinkingEffortState"].(map[string]any)
	require.True(t, ok, "state should contain thinkingEffortState")
	assert.Equal(t, "high", effortState["currentId"])

	// Verify model list state from pool cache
	mlState, ok := state["modelListState"].(map[string]any)
	require.True(t, ok, "state should contain modelListState")
	assert.Equal(t, "m1", mlState["currentModelId"])

	// Verify models were overridden by ACP model list
	agents, ok := resp["agents"].([]any)
	require.True(t, ok)
	for _, a := range agents {
		agent := a.(map[string]any)
		if agent["id"] == "acp-agent" {
			models := agent["models"].([]any)
			m := models[0].(map[string]any)
			assert.Equal(t, "acp-m1", m["id"], "models should be overridden by ACP model list")
		}
	}
}

func TestServeAgentsGet_ACPStateFromDBFallback(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with DB-persisted state
	acpAgent := &model.Agent{
		ID:                "acp-db-agent",
		Name:              "ACP DB Agent",
		Backend:           "acp-test",
		Transport:         "acp-stdio",
		Models:            []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
		AcpModeState:      `{"currentModeId":"ask","availableModes":[{"id":"ask","name":"Ask"},{"id":"code","name":"Code"}]}`,
		AcpThinkingState:  `{"currentId":"low","availableLevels":[{"id":"low"},{"id":"medium"}]}`,
		AcpCommands:       `[{"name":"/compact","description":"Compact history"}]`,
		AcpModelListState: `{"currentModelId":"db-m1","models":[{"id":"db-m1","name":"DB Model 1","default":true}]}`,
	}
	model.Agents["acp-db-agent"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	// Ensure no pool entry exists
	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-db-agent")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	state, ok := acpStates["acp-db-agent"].(map[string]any)
	require.True(t, ok, "acpStates should contain acp-db-agent from DB fallback")

	// Verify mode state from DB
	modeState, ok := state["modeState"].(map[string]any)
	require.True(t, ok, "state should contain modeState from DB")
	assert.Equal(t, "ask", modeState["currentModeId"])

	// Verify thinking effort state from DB
	effortState, ok := state["thinkingEffortState"].(map[string]any)
	require.True(t, ok, "state should contain thinkingEffortState from DB")
	assert.Equal(t, "low", effortState["currentId"])

	// Verify commands from DB
	commands, ok := state["commands"].([]any)
	require.True(t, ok, "state should contain commands from DB")
	assert.Len(t, commands, 1)

	// Verify model list state from DB overrides models
	mlState, ok := state["modelListState"].(map[string]any)
	require.True(t, ok, "state should contain modelListState from DB")
	assert.Equal(t, "db-m1", mlState["currentModelId"])
}

func TestServeAgentsGet_ACPStateDBFallbackInvalidJSON(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with invalid DB-persisted state
	acpAgent := &model.Agent{
		ID:                "acp-bad-json",
		Name:              "ACP Bad JSON",
		Backend:           "acp-test",
		Transport:         "acp-stdio",
		Models:            []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
		AcpModeState:      `{invalid json`,
		AcpThinkingState:  `not json`,
		AcpCommands:       `also not json`,
		AcpModelListState: `bad`,
	}
	model.Agents["acp-bad-json"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	// Ensure no pool entry exists
	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-bad-json")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	// Invalid JSON should not produce any state
	_, exists := acpStates["acp-bad-json"]
	assert.False(t, exists, "invalid DB state should not produce acpState entry")
}

func TestServeAgentsGet_ACPStateEmptyDBFields(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with empty DB state fields
	acpAgent := &model.Agent{
		ID:        "acp-empty-db",
		Name:      "ACP Empty DB",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
	}
	model.Agents["acp-empty-db"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	// Ensure no pool entry exists
	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-empty-db")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	_, exists := acpStates["acp-empty-db"]
	assert.False(t, exists, "empty DB state should not produce acpState entry")
}

func TestServeAgentsGet_ACPStateDBFallbackEmptyAvailableModes(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with DB state that has empty availableModes
	acpAgent := &model.Agent{
		ID:               "acp-empty-modes",
		Name:             "ACP Empty Modes",
		Backend:          "acp-test",
		Transport:        "acp-stdio",
		Models:           []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
		AcpModeState:     `{"currentModeId":"code","availableModes":[]}`,
		AcpThinkingState: `{"currentId":"low","availableLevels":[]}`,
	}
	model.Agents["acp-empty-modes"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-empty-modes")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	// Empty availableModes/availableLevels should not produce state
	_, exists := acpStates["acp-empty-modes"]
	assert.False(t, exists, "empty available arrays should not produce acpState entry")
}

func TestServeAgentsGet_NonACPAgentNoACPState(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	// codebuddy and claude are CLI agents — no ACP state
	_, hasCodebuddy := acpStates["codebuddy"]
	_, hasClaude := acpStates["claude"]
	assert.False(t, hasCodebuddy, "CLI agent should not have ACP state")
	assert.False(t, hasClaude, "CLI agent should not have ACP state")
}

func TestServeAgentsGet_ACPCommandsEmptyArray(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with AcpCommands = "[]" (should be treated as no commands)
	acpAgent := &model.Agent{
		ID:          "acp-empty-cmds",
		Name:        "ACP Empty Cmds",
		Backend:     "acp-test",
		Transport:   "acp-stdio",
		Models:      []model.AgentModel{{ID: "m1", Name: "M1", Default: true}},
		AcpCommands: "[]",
	}
	model.Agents["acp-empty-cmds"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	pool := ai.GetACPConnectionPool()
	pool.CloseConnection("acp-empty-cmds")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	acpStates, ok := resp["acpStates"].(map[string]any)
	require.True(t, ok, "response should contain acpStates")

	// "[]" should not produce state (line 90: a.AcpCommands != "" && a.AcpCommands != "[]")
	_, exists := acpStates["acp-empty-cmds"]
	assert.False(t, exists, "empty array commands should not produce acpState entry")
}

func TestServeAgentsGet_ACPModelListOverridesModels(t *testing.T) {
	_, teardown := setupAgentTestEnv(t)
	defer teardown()

	// Add an ACP agent with CLI-discovered models
	acpAgent := &model.Agent{
		ID:        "acp-ml-override",
		Name:      "ACP ML Override",
		Backend:   "acp-test",
		Transport: "acp-stdio",
		Models:    []model.AgentModel{{ID: "cli-model", Name: "CLI Model", Default: true}},
	}
	model.Agents["acp-ml-override"] = acpAgent
	model.AgentList = append(model.AgentList, acpAgent)
	require.NoError(t, service.SaveAgent(service.DB, acpAgent))

	// Inject pool entry with model list that should override CLI-discovered models
	pool := ai.GetACPConnectionPool()
	entry := &ai.ACPConnEntry{}
	entry.SetCachedModelListState(&ai.ModelListState{
		CurrentModelID: "acp-model-1",
		Models: []model.AgentModel{
			{ID: "acp-model-1", Name: "ACP Model 1", Default: true},
			{ID: "acp-model-2", Name: "ACP Model 2"},
		},
	})
	pool.SetEntryForTest("acp-ml-override", entry)
	defer pool.CloseConnection("acp-ml-override")

	req := newRequest(t, http.MethodGet, "/api/agents", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeAgents, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Find the agent and verify models were overridden
	agents, ok := resp["agents"].([]any)
	require.True(t, ok)
	for _, a := range agents {
		agent := a.(map[string]any)
		if agent["id"] == "acp-ml-override" {
			models := agent["models"].([]any)
			assert.Len(t, models, 2)
			m0 := models[0].(map[string]any)
			assert.Equal(t, "acp-model-1", m0["id"], "models should be overridden by ACP model list")
			m1 := models[1].(map[string]any)
			assert.Equal(t, "acp-model-2", m1["id"])
		}
	}
}
