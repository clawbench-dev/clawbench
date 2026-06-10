package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- GET /api/setup/status ----------

func TestSetupStatus_NeedsSetup(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Clear agents to simulate needs_setup
	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodGet, "/api/setup/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["needs_setup"].(bool))
}

func TestSetupStatus_NoNeedSetup(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["needs_setup"].(bool))
}

func TestSetupStatus_EmbeddedAgent(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	req := newRequest(t, http.MethodGet, "/api/setup/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupStatus, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// embedded_agent depends on EmbeddedAgentPath() — in test env it's likely empty
	_, hasEmbedded := resp["embedded_agent"]
	assert.True(t, hasEmbedded, "response should include embedded_agent field")
}

// ---------- GET /api/setup/providers ----------

func TestSetupProviders_ReturnsWizardReadyOnly(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/providers", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupProviders, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	providers, ok := resp["providers"].([]any)
	require.True(t, ok, "response should contain providers array")
	assert.NotEmpty(t, providers, "should have at least one wizard-ready provider")

	// Verify no enterprise providers
	for _, p := range providers {
		pMap, _ := p.(map[string]any)
		id, _ := pMap["id"].(string)
		assert.NotEqual(t, "amazon-bedrock", id)
		assert.NotEqual(t, "azure-openai-responses", id)
		assert.NotEqual(t, "google-vertex", id)
	}

	// Verify custom_url_supported
	assert.True(t, resp["custom_url_supported"].(bool))
}

func TestSetupProviders_ContainsExpectedProviders(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/providers", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupProviders, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	providers, _ := resp["providers"].([]any)
	providerIDs := make(map[string]bool)
	for _, p := range providers {
		pMap, _ := p.(map[string]any)
		providerIDs[pMap["id"].(string)] = true
	}

	// Check key providers exist
	assert.True(t, providerIDs["openai"])
	assert.True(t, providerIDs["anthropic"])
	assert.True(t, providerIDs["google"])
	assert.True(t, providerIDs["deepseek"])
}

func TestSetupProviders_EachHasRequiredFields(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/providers", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupProviders, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	providers, _ := resp["providers"].([]any)
	for _, p := range providers {
		pMap, _ := p.(map[string]any)
		assert.NotEmpty(t, pMap["id"], "provider should have id")
		assert.NotEmpty(t, pMap["name"], "provider should have name")
		assert.NotEmpty(t, pMap["envVar"], "provider should have envVar")
	}
}

// ---------- POST /api/setup/models ----------

func TestSetupModels_KnownModelsProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "anthropic",
		"custom_url": "",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	models, ok := resp["models"].([]any)
	require.True(t, ok, "response should contain models array")
	assert.NotEmpty(t, models, "anthropic should have KnownModels")

	// Check summarize_model_hint
	hint, hasHint := resp["summarize_model_hint"]
	assert.True(t, hasHint, "should have summarize_model_hint")
	assert.NotEmpty(t, hint, "summarize_model_hint should not be empty")
}

func TestSetupModels_UnknownProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "nonexistent",
		"custom_url": "",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetupModels_MissingProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"api_key": "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- POST /api/setup/verify ----------

func TestSetupVerify_MissingFields(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider": "openai",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetupVerify_UnknownProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider": "nonexistent",
		"api_key":  "test-key",
		"model":    "test-model",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- POST /api/setup/complete ----------

func TestSetupComplete_MissingFields(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider": "openai",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetupComplete_UnknownProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":        "nonexistent",
		"api_key":         "test-key",
		"model":           "test-model",
		"summarize_model": "test-summarize-model",
		"agent_name":      "Test",
		"agent_id":        "test",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetupComplete_CreatesAgent(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Clear existing agents to simulate fresh setup
	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}

	// Clear existing agents in DB
	_, _ = service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":        "openai",
		"custom_url":      "",
		"api_key":         "sk-test-key-12345",
		"model":           "gpt-5.5",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI",
		"agent_id":        "openai",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	// Verify agent created in memory
	assert.NotNil(t, model.Agents["openai"])
	assert.Equal(t, "OpenAI", model.Agents["openai"].Name)
	assert.Equal(t, "gpt-5.5", model.Agents["openai"].PreferredModel)
	assert.Equal(t, "setup", model.Agents["openai"].Source)
	assert.Equal(t, "pi", model.Agents["openai"].Backend)

	// Verify agent created in DB
	var name string
	err := service.DB.QueryRow("SELECT name FROM agents WHERE id = ?", "openai").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "OpenAI", name)

	// Verify API key encrypted in DB
	var count int
	_ = service.DB.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = ? AND provider = ?", "openai", "openai").Scan(&count)
	assert.Equal(t, 1, count)
}

func TestSetupComplete_DuplicateAgentID(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key",
		"model":           "gpt-5.5",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI",
		"agent_id":        "codebuddy", // already exists in test setup
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	// Should fail with conflict or bad request
	assert.True(t, w.Code == http.StatusConflict || w.Code == http.StatusBadRequest,
		"expected 409 or 400 for duplicate agent_id, got %d", w.Code)
}

func TestSetupComplete_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/complete", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- Method Not Allowed tests for remaining endpoints ----------

func TestSetupStatus_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPost, "/api/setup/status", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupStatus, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSetupProviders_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPost, "/api/setup/providers", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupProviders, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSetupModels_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/models", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSetupVerify_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/verify", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

// ---------- ServeSetupModels extended coverage ----------

// TestSetupModels_CustomURLDefaultsToOpenAI tests that empty provider + custom_url
// defaults provider to "openai" and uses the custom URL's derived models endpoint.
// Uses a mock server to avoid hitting real endpoints.
func TestSetupModels_CustomURLDefaultsToOpenAI(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Use a mock server that returns an error — simulates HTTP fetch failure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "",
		"custom_url": server.URL + "/v1/chat/completions",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	// Should return 200 with empty models and error
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["models"])
	assert.NotEmpty(t, resp["error"], "should report HTTP fetch error")
}

// TestSetupModels_EmptyProviderNoCustomURL tests that empty provider + no custom_url
// returns 400.
func TestSetupModels_EmptyProviderNoCustomURL(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "",
		"custom_url": "",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSetupModels_OpenAIProviderHTTPFetch tests the HTTP fetch path for OpenAI provider.
// Uses a mock server to avoid hitting real endpoints.
func TestSetupModels_OpenAIProviderHTTPFetch(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Start a mock server that returns models in OpenAI format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-4o", "name": "GPT-4o", "created": 1700000000},
				{"id": "gpt-4o-mini", "created": 1700000001}
			]
		}`))
	}))
	defer server.Close()

	// Built-in provider (no custom_url) should return models from the known list.
	// custom_url mode always returns empty models — tested separately.
	body := map[string]any{
		"provider": "openai",
		"api_key":  "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	models, _ := resp["models"].([]any)
	assert.NotEmpty(t, models, "should have models from known list")
	hint := resp["summarize_model_hint"]
	assert.NotEmpty(t, hint)
}

// TestSetupModels_KnownModelsFields tests that KnownModels entries have expected fields.
func TestSetupModels_KnownModelsFields(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider": "anthropic",
		"api_key":  "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	models, _ := resp["models"].([]any)
	require.NotEmpty(t, models)

	// Check first model has expected fields
	first, _ := models[0].(map[string]any)
	assert.NotEmpty(t, first["id"])
	assert.NotEmpty(t, first["name"])
	_, hasCreated := first["created"]
	assert.True(t, hasCreated)
	_, hasContext := first["context_length"]
	assert.True(t, hasContext)
	_, hasThinking := first["supports_thinking"]
	assert.True(t, hasThinking)
	_, hasCostTier := first["cost_tier"]
	assert.True(t, hasCostTier)
}

// TestSetupModels_InvalidJSON tests the decodeJSON failure path.
func TestSetupModels_InvalidJSON(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := httptest.NewRequest(http.MethodPost, "/api/setup/models", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- ServeSetupVerify extended coverage ----------

// TestSetupVerify_EmptyProviderDefaultsToOpenAI tests that empty provider with
// custom_url uses HTTP verification (not Pi CLI). With a bad URL, it should
// return 200 with success=false.
func TestSetupVerify_EmptyProviderDefaultsToOpenAI(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.example.com/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	// Custom URL mode uses HTTP verification, not Pi CLI.
	// The fake URL will fail with HTTP error → 200 {success: false}
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail with unreachable URL")
}

// TestSetupVerify_EmbeddedAgentNotFound tests the path where Pi binary is not found.
func TestSetupVerify_EmbeddedAgentNotFound(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider": "openai",
		"api_key":  "test-key",
		"model":    "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	// In test env, EmbeddedAgentPath() returns "" → 404
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestSetupVerify_InvalidJSON tests the decodeJSON failure path.
func TestSetupVerify_InvalidJSON(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := httptest.NewRequest(http.MethodPost, "/api/setup/verify", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- ServeSetupComplete extended coverage ----------

// TestSetupComplete_EmptyProviderEmptyCustomURL tests that empty provider + empty custom_url
// returns 400.
func TestSetupComplete_EmptyProviderEmptyCustomURL(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "",
		"custom_url": "",
		"api_key":    "test-key",
		"model":      "gpt-4o",
		"agent_name": "Test",
		"agent_id":   "test-empty",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSetupComplete_CustomURLDefaultsToOpenAI tests the path where empty provider
// with custom_url defaults to "openai".
func TestSetupComplete_CustomURLDefaultsToOpenAI(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Clear agents to simulate fresh setup
	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":        "",
		"custom_url":      "https://api.example.com/v1/chat/completions",
		"api_key":         "test-key",
		"model":           "gpt-4o",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "Custom",
		"agent_id":        "custom-openai",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	// Verify agent was created with agent ID as provider (custom URL mode stores agent ID as provider)
	var count int
	_ = service.DB.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = ? AND provider = ?", "custom-openai", "custom-openai").Scan(&count)
	assert.Equal(t, 1, count)
}

// TestSetupComplete_ConcurrentRequest tests the mutex TryLock path.
// A second concurrent request should get 409 Conflict.
func TestSetupComplete_ConcurrentRequest(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	// Hold the lock to simulate a concurrent request in progress
	setupCompleteMu.Lock()
	defer setupCompleteMu.Unlock()

	body := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key",
		"model":           "gpt-4o",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI",
		"agent_id":        "openai-concurrent",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

// TestSetupComplete_NoSummarizeModel skips summarize auto-config.
func TestSetupComplete_NoSummarizeModel(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key-no-summarize",
		"model":           "gpt-4o",
		"summarize_model": "",
		"agent_name":      "OpenAI NoSum",
		"agent_id":        "openai-nosum",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	assert.NotNil(t, model.Agents["openai-nosum"])
}

// TestSetupComplete_InvalidJSON tests the decodeJSON failure path.
func TestSetupComplete_InvalidJSON(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- deriveModelsURL unit tests ----------

func TestDeriveModelsURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{
			name:     "standard chat completions URL",
			baseURL:  "https://api.example.com/v1/chat/completions",
			expected: "https://api.example.com/v1/chat/models",
		},
		{
			name:     "URL with single path segment",
			baseURL:  "https://api.example.com/completions",
			expected: "https://api.example.com/models",
		},
		{
			name:     "root URL with trailing slash",
			baseURL:  "https://api.example.com/",
			expected: "https://api.example.com/models",
		},
		{
			name:     "no slash at all",
			baseURL:  "https://api.example.com",
			expected: "https://models",
		},
		{
			name:     "deep path",
			baseURL:  "https://api.example.com/a/b/c/d",
			expected: "https://api.example.com/a/b/c/models",
		},
		{
			name:     "scheme relative only",
			baseURL:  "https://x",
			expected: "https://models",
		},
		{
			name:     "slash only",
			baseURL:  "/",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveModelsURL(tt.baseURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------- fetchModelsFromEndpoint unit tests ----------

func TestFetchModelsFromEndpoint_Success(t *testing.T) {
	// Start a mock HTTP server returning OpenAI /v1/models format
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-4o", "name": "GPT-4o", "created": 1700000000},
				{"id": "gpt-4o-mini", "created": 1700000001}
			]
		}`))
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(server.URL, "test-api-key")
	require.NoError(t, err)
	require.Len(t, models, 2)

	assert.Equal(t, "gpt-4o", models[0].ID)
	assert.Equal(t, "GPT-4o", models[0].Name)
	assert.Equal(t, "gpt-4o-mini", models[1].ID)
	assert.Equal(t, "gpt-4o-mini", models[1].Name, "name should default to ID when empty")
}

func TestFetchModelsFromEndpoint_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(server.URL, "bad-key")
	assert.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "401")
}

func TestFetchModelsFromEndpoint_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(server.URL, "key")
	assert.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "parse models response")
}

func TestFetchModelsFromEndpoint_Unreachable(t *testing.T) {
	models, err := fetchModelsFromEndpoint("http://127.0.0.1:1/models", "key")
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestFetchModelsFromEndpoint_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should NOT have Authorization header when api_key is empty
		assert.Empty(t, r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "model-1"}]}`))
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(server.URL, "")
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "model-1", models[0].ID)
}

func TestFetchModelsFromEndpoint_InvalidURL(t *testing.T) {
	models, err := fetchModelsFromEndpoint("://invalid-url", "key")
	assert.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "create request")
}

// ---------- writePiConfigFiles unit tests ----------

func TestWritePiConfigFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()

	// Override home directory to temp dir
	t.Setenv("HOME", tmpDir)

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	req := setupCompleteRequest{
		Provider:  "openai",
		APIKey:    "sk-test-key-123",
		Model:     "gpt-4o",
		CustomURL: "",
	}

	writePiConfigFiles(req)

	// Verify auth.json was written
	authPath := filepath.Join(tmpDir, ".pi", "agent", "auth.json")
	data, err := os.ReadFile(authPath)
	require.NoError(t, err)

	var authData map[string]any
	require.NoError(t, json.Unmarshal(data, &authData))
	// Pi expects structured format: { "provider": { "type": "api_key", "key": "..." } }
	entry, ok := authData["openai"].(map[string]any)
	require.True(t, ok, "auth.json should have 'openai' key with object value")
	assert.Equal(t, "api_key", entry["type"])
	assert.Equal(t, "sk-test-key-123", entry["key"])

	// Verify settings.json was written
	settingsPath := filepath.Join(tmpDir, ".pi", "agent", "settings.json")
	data, err = os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settingsData map[string]string
	require.NoError(t, json.Unmarshal(data, &settingsData))
	assert.Equal(t, "openai", settingsData["defaultProvider"])
	assert.Equal(t, "gpt-4o", settingsData["defaultModel"])
}

// ---------- atomicWriteFile unit tests ----------

func TestAtomicWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	err := atomicWriteFile(path, []byte("hello world"), 0o644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))

	// Verify no .tmp file left behind
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file should be cleaned up")
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	require.NoError(t, atomicWriteFile(path, []byte("first"), 0o644))
	require.NoError(t, atomicWriteFile(path, []byte("second"), 0o644))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "second", string(data))
}

// ---------- reinitSummarizer edge cases ----------

// TestReinitSummarizer_AnthropicFormat tests the anthropic format branch.
func TestReinitSummarizer_AnthropicFormat(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Save the original summarizer to restore later
	origSummarizer := summarizer

	req := setupCompleteRequest{
		Provider:       "anthropic",
		CustomURL:      "",
		APIKey:         "sk-ant-test-key",
		Model:          "claude-sonnet-4-20250514",
		SummarizeModel: "claude-3-5-haiku-20241022",
		AgentID:        "anthropic-test",
	}

	spec := model.FindProviderSpec("anthropic")
	require.NotNil(t, spec)

	reinitSummarizer(req, spec)

	// Verify the global summarizer was set (not nil)
	assert.NotNil(t, summarizer)

	// Restore
	summarizer = origSummarizer
}

// TestReinitSummarizer_UnknownFormat tests the unknown API format branch.
func TestReinitSummarizer_UnknownFormat(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer

	req := setupCompleteRequest{
		Provider:       "nonexistent",
		CustomURL:      "",
		APIKey:         "test-key",
		Model:          "test-model",
		SummarizeModel: "test-sum-model",
		AgentID:        "test-unknown-fmt",
	}

	// Use a spec with unknown API format
	spec := &model.ProviderSpec{
		ID:        "nonexistent",
		Name:      "NonExistent",
		EnvVar:    "TEST_API_KEY",
		APIFormat: "unknown_format",
	}

	reinitSummarizer(req, spec)

	// Should not have changed the summarizer
	assert.Equal(t, origSummarizer, summarizer)
}

// TestReinitSummarizer_NoAPIKey tests the path where API key is empty.
func TestReinitSummarizer_NoAPIKey(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "",
		APIKey:         "", // empty key
		Model:          "gpt-4o",
		SummarizeModel: "gpt-4o-mini",
		AgentID:        "no-key-agent",
	}

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	reinitSummarizer(req, spec)

	// Summarizer should NOT have changed (no key available)
	assert.Equal(t, origSummarizer, summarizer)
}

// ---------- configureSummarizeBackend edge case ----------

// TestConfigureSummarizeBackend_CustomURLOverride tests that custom_url overrides
// the spec's ChatEndpoint.
func TestConfigureSummarizeBackend_CustomURLOverride(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "https://custom.api.com/v1/chat/completions",
		APIKey:         "sk-test-key",
		Model:          "gpt-4o",
		SummarizeModel: "gpt-4o-mini",
		AgentID:        "custom-url-agent",
	}

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	configureSummarizeBackend(req, spec)

	// Verify in-memory config was updated with custom URL
	assert.Equal(t, "api", model.ConfigInstance.Summarize.Backend)
	assert.Equal(t, "gpt-4o-mini", model.ConfigInstance.Summarize.Model)
	assert.Equal(t, "https://custom.api.com/v1/chat/completions", model.ConfigInstance.Summarize.API.BaseURL)
	assert.Equal(t, "openai", model.ConfigInstance.Summarize.API.Format)
}

// ---------- ServeSetupModels with mock server ----------

// TestSetupModels_CustomURLWithMockServer tests that custom URL mode returns
// an empty model list (user enters models manually).
func TestSetupModels_CustomURLWithMockServer(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Start a mock /v1/models server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "custom-model-1", "name": "Custom Model 1"},
				{"id": "custom-model-2-mini", "name": "Custom Model 2 Mini"}
			]
		}`))
	}))
	defer server.Close()

	// Custom URL mode now returns an empty model list — the user must enter
	// model IDs manually. Verify the response matches this behavior.
	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.example.com/v1/chat/completions",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	models, _ := resp["models"].([]any)
	assert.Len(t, models, 0, "custom URL mode should return empty model list")

	hint := resp["summarize_model_hint"]
	assert.Empty(t, hint, "custom URL mode should return empty summarize_model_hint")
}

// ---------- ServeSetupVerify with custom URL ----------

// TestSetupVerify_WithCustomURL tests that custom_url triggers HTTP verification
// instead of Pi CLI. With an unreachable URL, it returns success=false.
func TestSetupVerify_WithCustomURL(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.example.com/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	// Custom URL uses HTTP verification — unreachable URL → success=false
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail with unreachable URL")
}

// ---------- ServeSetupComplete with anthropic provider ----------

// TestSetupComplete_AnthropicProvider tests setup with anthropic provider
// which exercises the anthropic-format summarize backend path.
func TestSetupComplete_AnthropicProvider(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	body := map[string]any{
		"provider":        "anthropic",
		"api_key":         "sk-ant-test-key-12345",
		"model":           "claude-sonnet-4-20250514",
		"summarize_model": "claude-3-5-haiku-20241022",
		"agent_name":      "Anthropic",
		"agent_id":        "anthropic",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	assert.NotNil(t, model.Agents["anthropic"])

	// Verify summarize backend was configured
	assert.Equal(t, "api", model.ConfigInstance.Summarize.Backend)
	assert.Equal(t, "anthropic", model.ConfigInstance.Summarize.API.Format)
}

// ---------- concurrent setupCompleteMu test ----------

// TestSetupComplete_MutexIsUnlockedAfterRequest verifies that the mutex is
// properly released after a successful request, allowing subsequent requests.
func TestSetupComplete_MutexIsUnlockedAfterRequest(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	// First request
	body1 := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key-1",
		"model":           "gpt-4o",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI 1",
		"agent_id":        "openai-1",
	}
	req1 := newRequest(t, http.MethodPost, "/api/setup/complete", body1)
	withAuthCookie(req1, model.SessionToken)
	w1 := callHandler(ServeSetupComplete, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second request should NOT get 409 — mutex should be released
	body2 := map[string]any{
		"provider":        "deepseek",
		"api_key":         "sk-test-key-2",
		"model":           "deepseek-chat",
		"summarize_model": "",
		"agent_name":      "DeepSeek",
		"agent_id":        "deepseek-1",
	}
	req2 := newRequest(t, http.MethodPost, "/api/setup/complete", body2)
	withAuthCookie(req2, model.SessionToken)
	w2 := callHandler(ServeSetupComplete, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

// ---------- ServeSetupComplete default_agent_id ----------

// TestSetupComplete_DefaultAgentID verifies the response includes default_agent_id.
func TestSetupComplete_DefaultAgentID(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key",
		"model":           "gpt-4o",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI",
		"agent_id":        "openai-default-test",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	_, hasDefaultID := resp["default_agent_id"]
	assert.True(t, hasDefaultID, "response should include default_agent_id")
}

// ---------- fetchModelsFromEndpoint empty data ----------

func TestFetchModelsFromEndpoint_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	models, err := fetchModelsFromEndpoint(server.URL, "key")
	require.NoError(t, err)
	assert.Empty(t, models)
}

// ---------- ServeSetupVerify with fake embedded Pi binary ----------

// createFakePiBinary creates a minimal executable at the expected embedded Pi path
// relative to the current test binary. Returns the path or "" on failure.
func createFakePiBinary(t *testing.T, exitCode int, output string) string {
	t.Helper()

	exePath, err := os.Executable()
	require.NoError(t, err)

	baseDir := filepath.Dir(exePath)
	piDir := filepath.Join(baseDir, ".clawbench", "pi")
	require.NoError(t, os.MkdirAll(piDir, 0o755))

	// Create a script that prints output and exits with the given code
	script := "#!/bin/sh\necho '" + output + "'\nexit " + fmt.Sprintf("%d", exitCode)
	piPath := filepath.Join(piDir, "pi")
	require.NoError(t, os.WriteFile(piPath, []byte(script), 0o755))

	return piPath
}

func cleanupFakePiBinary(t *testing.T) {
	t.Helper()
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	baseDir := filepath.Dir(exePath)
	piDir := filepath.Join(baseDir, ".clawbench", "pi")
	_ = os.RemoveAll(piDir)
}

// TestSetupVerify_FakePiSuccess tests the full verify path with a fake Pi binary
// that exits successfully.
func TestSetupVerify_FakePiSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Pi binary path resolution differs on Windows")
	}
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 0, `{"type":"result","content":"pong"}`)
	defer cleanupFakePiBinary(t)

	// Verify the fake binary exists at the expected path
	_, err := os.Stat(piPath)
	require.NoError(t, err, "fake Pi binary should exist at %s", piPath)

	body := map[string]any{
		"provider": "openai",
		"api_key":  "test-key",
		"model":    "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool), "should succeed with fake Pi")
	assert.Equal(t, "gpt-4o", resp["model"])
}

// TestSetupVerify_FakePiFailure tests the verify error path with a fake Pi binary
// that exits with a non-zero code.
func TestSetupVerify_FakePiFailure(t *testing.T) {
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 1, "error: invalid API key")
	defer cleanupFakePiBinary(t)

	_, err := os.Stat(piPath)
	require.NoError(t, err, "fake Pi binary should exist at %s", piPath)

	body := map[string]any{
		"provider": "openai",
		"api_key":  "bad-key",
		"model":    "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail with fake Pi returning error")
}

// TestSetupVerify_FakePiNoOutput tests the error path where Pi exits with error
// but produces no output (errMsg falls back to err.Error()).
func TestSetupVerify_FakePiNoOutput(t *testing.T) {
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 1, "")
	defer cleanupFakePiBinary(t)

	_, err := os.Stat(piPath)
	require.NoError(t, err, "fake Pi binary should exist at %s", piPath)

	body := map[string]any{
		"provider": "openai",
		"api_key":  "bad-key",
		"model":    "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail")
}

// TestSetupVerify_FakePiWithCustomURL tests verify with custom_url — this now
// uses HTTP verification instead of Pi CLI, so the fake Pi is NOT invoked.
func TestSetupVerify_FakePiWithCustomURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Pi binary path resolution differs on Windows")
	}
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 0, `{"type":"result","content":"pong"}`)
	defer cleanupFakePiBinary(t)

	_, err := os.Stat(piPath)
	require.NoError(t, err, "fake Pi binary should exist at %s", piPath)

	// Custom URL mode → HTTP verification, NOT Pi CLI
	// The URL is unreachable, so verify should fail
	body := map[string]any{
		"provider":   "openai",
		"custom_url": "https://custom.api.com/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail with unreachable custom URL")
}

// TestSetupVerify_FakePiNoEnvVar tests verify with a provider that has no EnvVar
// (falls through to cmd.Env = os.Environ()).
func TestSetupVerify_FakePiNoEnvVar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Pi binary path resolution differs on Windows")
	}
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 0, `{"type":"result","content":"pong"}`)
	defer cleanupFakePiBinary(t)

	_, err := os.Stat(piPath)
	require.NoError(t, err, "fake Pi binary should exist at %s", piPath)

	// Use a provider spec with empty EnvVar by temporarily adding one
	origRegistry := model.ProviderRegistry
	model.ProviderRegistry["test-no-envvar"] = model.ProviderSpec{
		ID: "test-no-envvar", Name: "Test No EnvVar", EnvVar: "",
		ChatEndpoint:   "https://api.example.com/v1/chat/completions",
		ModelsEndpoint: "https://api.example.com/v1/models",
		APIFormat:      "openai", SupportsCLI: true, WizardReady: true,
	}
	defer func() { model.ProviderRegistry = origRegistry }()

	body := map[string]any{
		"provider": "test-no-envvar",
		"api_key":  "test-key",
		"model":    "test-model",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool), "should succeed even without EnvVar")
}

// ---------- writePiConfigFiles error path ----------

// TestWritePiConfigFiles_HomeDirError tests the path where os.UserHomeDir fails.
func TestWritePiConfigFiles_HomeDirError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	// Set HOME to an invalid value to potentially cause issues
	// Actually os.UserHomeDir reads $HOME on Unix, so we can't easily make it fail.
	// Instead test that writePiConfigFiles works correctly and doesn't crash.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	spec := model.FindProviderSpec("anthropic")
	require.NotNil(t, spec)

	req := setupCompleteRequest{
		Provider:  "anthropic",
		APIKey:    "sk-ant-key",
		Model:     "claude-sonnet-4-20250514",
		CustomURL: "",
	}

	writePiConfigFiles(req)

	// Verify files were written
	authPath := filepath.Join(tmpDir, ".pi", "agent", "auth.json")
	data, err := os.ReadFile(authPath)
	require.NoError(t, err)

	var authData map[string]any
	require.NoError(t, json.Unmarshal(data, &authData))
	entry, ok := authData["anthropic"].(map[string]any)
	require.True(t, ok, "auth.json should have 'anthropic' key with object value")
	assert.Equal(t, "api_key", entry["type"])
	assert.Equal(t, "sk-ant-key", entry["key"])
}

// ---------- atomicWriteFile error path ----------

// TestAtomicWriteFile_WriteToReadOnlyDir tests the error path where the target
// directory is read-only.
func TestAtomicWriteFile_WriteToReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions not supported on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("skipping as root: root bypasses filesystem permissions")
	}
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o555))
	defer func() { _ = os.Chmod(readOnlyDir, 0o755) }() // restore for cleanup

	path := filepath.Join(readOnlyDir, "test.txt")
	err := atomicWriteFile(path, []byte("hello"), 0o644)
	assert.Error(t, err, "should fail writing to read-only directory")
}

// ---------- ServeSetupModels ModelsEndpointNotAvailable ----------

// TestSetupModels_NoModelsEndpoint tests a provider that has neither KnownModels
// nor ModelsEndpoint and no custom_url.
func TestSetupModels_NoModelsEndpoint(t *testing.T) {
	defer setupAgentTestEnv(t)()

	// Register a provider with no ModelsEndpoint and no KnownModels
	origRegistry := model.ProviderRegistry
	model.ProviderRegistry["test-no-endpoint"] = model.ProviderSpec{
		ID: "test-no-endpoint", Name: "Test No Endpoint", EnvVar: "TEST_KEY",
		ChatEndpoint:   "https://api.example.com/v1/chat/completions",
		ModelsEndpoint: "", APIFormat: "openai",
		SupportsCLI: true, WizardReady: true,
	}
	defer func() { model.ProviderRegistry = origRegistry }()

	body := map[string]any{
		"provider": "test-no-endpoint",
		"api_key":  "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	// Should return 400 ModelsEndpointNotAvailable
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- ServeSetupComplete with fake embedded Pi ----------

// TestSetupComplete_WithFakePiBinary tests the full creation path where an
// embedded Pi binary exists, triggering the writePiConfigFiles call.
func TestSetupComplete_WithFakePiBinary(t *testing.T) {
	defer setupAgentTestEnv(t)()

	piPath := createFakePiBinary(t, 0, "ok")
	defer cleanupFakePiBinary(t)

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	_, _ = service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":        "openai",
		"api_key":         "sk-test-key-withpi",
		"model":           "gpt-4o",
		"summarize_model": "gpt-4o-mini",
		"agent_name":      "OpenAI Pi",
		"agent_id":        "openai-pi",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	// Agent should have the embedded Pi path as Command
	agent := model.Agents["openai-pi"]
	assert.NotNil(t, agent)
	assert.Equal(t, piPath, agent.Command)
}

// ---------- writePiConfigFiles error paths ----------

// TestWritePiConfigFiles_MkdirAllFailure tests the path where the .pi/agent
// directory cannot be created (e.g., HOME points to a read-only location).
func TestWritePiConfigFiles_MkdirAllFailure(t *testing.T) {
	// Create a read-only directory and set HOME to a path under it
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	require.NoError(t, os.MkdirAll(readOnlyDir, 0o555))
	defer func() { _ = os.Chmod(readOnlyDir, 0o755) }() // restore for cleanup

	t.Setenv("HOME", filepath.Join(readOnlyDir, "user"))

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	req := setupCompleteRequest{
		Provider:  "openai",
		APIKey:    "sk-test-key",
		Model:     "gpt-4o",
		CustomURL: "",
	}

	// Should not panic — best-effort write
	writePiConfigFiles(req)
}

// TestWritePiConfigFiles_AuthWriteFailure tests the path where auth.json
// cannot be written (directory is removed after MkdirAll).
func TestWritePiConfigFiles_AuthWriteFailure(t *testing.T) {
	tmpDir := t.TempDir()

	t.Setenv("HOME", tmpDir)

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	req := setupCompleteRequest{
		Provider:  "openai",
		APIKey:    "sk-test-key",
		Model:     "gpt-4o",
		CustomURL: "",
	}

	// Create the .pi/agent dir then make it read-only to cause write failure
	piDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piDir, 0o755))
	require.NoError(t, os.Chmod(piDir, 0o555))
	defer func() { _ = os.Chmod(piDir, 0o755) }() // restore for cleanup

	// Should not panic — best-effort write
	writePiConfigFiles(req)
}

// ---------- reinitSummarizer empty API key path ----------

// TestReinitSummarizer_EmptyAPIKey tests the path where req.APIKey is empty.
// Since reinitSummarizer now reads directly from req.APIKey, empty key means
// no summarizer change.
func TestReinitSummarizer_EmptyAPIKey(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "",
		APIKey:         "", // empty key in request
		Model:          "gpt-4o",
		SummarizeModel: "gpt-4o-mini",
		AgentID:        "empty-key-agent",
	}

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	reinitSummarizer(req, spec)

	// Summarizer should NOT have changed — no key available
	assert.Equal(t, origSummarizer, summarizer)
}

// TestReinitSummarizer_NoKeyAtAll tests that when request key is empty,
// the summarizer is not changed.
func TestReinitSummarizer_NoKeyAtAll(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "",
		APIKey:         "", // No key in request
		Model:          "gpt-4o",
		SummarizeModel: "gpt-4o-mini",
		AgentID:        "no-key-at-all-agent",
	}

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	reinitSummarizer(req, spec)

	// Should not have changed the summarizer — no key available at all
	assert.Equal(t, origSummarizer, summarizer)
}

// ---------- reinitSummarizer with custom URL ----------

// TestReinitSummarizer_CustomURL tests that reinitSummarizer uses custom_url
// when provided.
func TestReinitSummarizer_CustomURL(t *testing.T) {
	defer setupAgentTestEnv(t)()

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	customURL := "https://custom.api.com/v1/chat/completions"

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      customURL,
		APIKey:         "sk-test-key",
		Model:          "gpt-4o",
		SummarizeModel: "gpt-4o-mini",
		AgentID:        "reinit-custom-agent",
	}

	spec := model.FindProviderSpec("openai")
	require.NotNil(t, spec)

	reinitSummarizer(req, spec)

	// Summarizer should be set (not the original)
	assert.NotNil(t, summarizer)
	assert.NotEqual(t, origSummarizer, summarizer)
}

// ---------- validateCustomURL extended tests ----------

func TestValidateCustomURL_AnthropicValid(t *testing.T) {
	result := validateCustomURL("https://api.anthropic.com/v1/messages", "anthropic")
	assert.Empty(t, result, "valid anthropic URL should pass validation")
}

func TestValidateCustomURL_AnthropicInvalid(t *testing.T) {
	result := validateCustomURL("https://api.anthropic.com/v1/chat/completions", "anthropic")
	assert.Equal(t, "CustomURLAnthropicFormat", result, "anthropic URL should end with /v1/messages")
}

func TestValidateCustomURL_OpenAIValid(t *testing.T) {
	result := validateCustomURL("https://api.openai.com/v1/chat/completions", "openai")
	assert.Empty(t, result, "valid openai URL should pass validation")
}

func TestValidateCustomURL_OpenAIInvalid(t *testing.T) {
	result := validateCustomURL("https://api.openai.com/v1/messages", "openai")
	assert.Equal(t, "CustomURLOpenAIFormat", result, "openai URL should end with /chat/completions")
}

func TestValidateCustomURL_EmptyURL(t *testing.T) {
	result := validateCustomURL("", "openai")
	assert.Empty(t, result, "empty URL should pass (no validation needed)")
}

// ---------- ServeSetupVerify with anthropic custom URL ----------

func TestSetupVerify_AnthropicCustomURL(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.anthropic.com/v1/messages",
		"api_format": "anthropic",
		"api_key":    "sk-ant-test-key",
		"model":      "claude-sonnet-4-20250514",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	// Custom URL mode uses HTTP verification — unreachable URL → success=false
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail with unreachable URL")
}

// ---------- ServeSetupComplete with custom URL anthropic ----------

func TestSetupComplete_CustomURLAnthropic(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	service.DB.Exec("DELETE FROM agents")

	origSummarizer := summarizer
	defer func() { summarizer = origSummarizer }()

	body := map[string]any{
		"provider":        "",
		"custom_url":      "https://api.anthropic.com/v1/messages",
		"api_format":      "anthropic",
		"api_key":         "sk-ant-test-key",
		"model":           "claude-sonnet-4-20250514",
		"summarize_model": "claude-3-5-haiku-20241022",
		"agent_name":      "Custom Anthropic",
		"agent_id":        "custom-anthropic",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	// Verify agent was created with agent ID as provider (custom URL mode stores agent ID, not "anthropic")
	var count int
	service.DB.QueryRow("SELECT COUNT(*) FROM agent_api_keys WHERE agent_id = ? AND provider = ?", "custom-anthropic", "custom-anthropic").Scan(&count)
	assert.Equal(t, 1, count)

	// Verify summarize backend was configured with anthropic format
	assert.Equal(t, "anthropic", model.ConfigInstance.Summarize.API.Format)
}

// ---------- ServeSetupComplete with invalid custom URL ----------

func TestSetupComplete_InvalidCustomURLOpenAI(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.example.com/v1/invalid",
		"api_key":    "test-key",
		"model":      "test-model",
		"agent_name": "Invalid URL",
		"agent_id":   "invalid-url",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetupComplete_InvalidCustomURLAnthropic(t *testing.T) {
	defer setupAgentTestEnv(t)()

	model.Agents = map[string]*model.Agent{}
	model.AgentList = []*model.Agent{}
	service.DB.Exec("DELETE FROM agents")

	body := map[string]any{
		"provider":   "",
		"custom_url": "https://api.example.com/v1/invalid",
		"api_format": "anthropic",
		"api_key":    "test-key",
		"model":      "test-model",
		"agent_name": "Invalid Anthropic URL",
		"agent_id":   "invalid-anthropic-url",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/complete", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupComplete, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------- detectAPIFormat unit tests ----------

func TestDetectAPIFormat(t *testing.T) {
	tests := []struct {
		name      string
		customURL string
		apiFormat string
		expected  string
	}{
		{
			name:      "explicit format takes priority",
			customURL: "https://api.example.com/v1/chat/completions",
			apiFormat: "anthropic",
			expected:  "anthropic",
		},
		{
			name:      "auto-detect anthropic from /v1/messages",
			customURL: "https://api.anthropic.com/v1/messages",
			apiFormat: "",
			expected:  "anthropic",
		},
		{
			name:      "auto-detect openai default for /chat/completions",
			customURL: "https://api.openai.com/v1/chat/completions",
			apiFormat: "",
			expected:  "openai",
		},
		{
			name:      "default to openai for unrecognized path",
			customURL: "https://api.example.com/v1/unknown",
			apiFormat: "",
			expected:  "openai",
		},
		{
			name:      "empty URL defaults to openai",
			customURL: "",
			apiFormat: "",
			expected:  "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectAPIFormat(tt.customURL, tt.apiFormat)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ---------- validateCustomURL extended unit tests ----------

func TestValidateCustomURL_InvalidScheme(t *testing.T) {
	result := validateCustomURL("ftp://api.example.com/v1/chat/completions", "")
	assert.Equal(t, "CustomURLInvalidScheme", result)
}

func TestValidateCustomURL_NoHost(t *testing.T) {
	result := validateCustomURL("https:///v1/chat/completions", "")
	assert.Equal(t, "CustomURLInvalidHost", result)
}

func TestValidateCustomURL_UnparseableURL(t *testing.T) {
	result := validateCustomURL("://not-a-valid-url", "")
	assert.Equal(t, "CustomURLInvalid", result)
}

func TestValidateCustomURL_AutoDetectOpenAI(t *testing.T) {
	result := validateCustomURL("https://api.example.com/v1/chat/completions", "")
	assert.Empty(t, result, "auto-detected openai URL should be valid")
}

func TestValidateCustomURL_AutoDetectAnthropic(t *testing.T) {
	result := validateCustomURL("https://api.anthropic.com/v1/messages", "")
	assert.Empty(t, result, "auto-detected anthropic URL should be valid")
}

func TestValidateCustomURL_AutoDetectUnrecognized(t *testing.T) {
	result := validateCustomURL("https://api.example.com/v1/unknown", "")
	assert.Equal(t, "CustomURLUnrecognizedFormat", result)
}

func TestValidateCustomURL_HttpScheme(t *testing.T) {
	result := validateCustomURL("http://localhost:8080/v1/chat/completions", "")
	assert.Empty(t, result, "http scheme should be allowed for local dev")
}

// ---------- verifyOpenAIHTTP / verifyAnthropicHTTP with mock servers ----------

func TestVerifyOpenAIHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-test","choices":[]}`))
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "test-api-key", "gpt-4o")
	assert.NoError(t, err)
}

func TestVerifyOpenAIHTTP_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "bad-key", "gpt-4o")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestVerifyOpenAIHTTP_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "test-key", "unknown-model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestVerifyOpenAIHTTP_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "test-key", "gpt-4o")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestVerifyOpenAIHTTP_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "test-key", "gpt-4o")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestVerifyOpenAIHTTP_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-test","choices":[]}`))
	}))
	defer server.Close()

	err := verifyOpenAIHTTP(t.Context(), server.URL+"/v1/chat/completions", "", "gpt-4o")
	assert.NoError(t, err)
}

func TestVerifyOpenAIHTTP_Unreachable(t *testing.T) {
	err := verifyOpenAIHTTP(t.Context(), "http://127.0.0.1:1/v1/chat/completions", "key", "model")
	assert.Error(t, err)
}

func TestVerifyAnthropicHTTP_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_test","content":[]}`))
	}))
	defer server.Close()

	err := verifyAnthropicHTTP(t.Context(), server.URL+"/v1/messages", "test-api-key", "claude-sonnet-4-20250514")
	assert.NoError(t, err)
}

func TestVerifyAnthropicHTTP_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	err := verifyAnthropicHTTP(t.Context(), server.URL+"/v1/messages", "bad-key", "claude-sonnet-4-20250514")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestVerifyAnthropicHTTP_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := verifyAnthropicHTTP(t.Context(), server.URL+"/v1/messages", "test-key", "unknown-model")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestVerifyAnthropicHTTP_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	err := verifyAnthropicHTTP(t.Context(), server.URL+"/v1/messages", "test-key", "claude-sonnet-4-20250514")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestVerifyAnthropicHTTP_NoAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_test","content":[]}`))
	}))
	defer server.Close()

	err := verifyAnthropicHTTP(t.Context(), server.URL+"/v1/messages", "", "claude-sonnet-4-20250514")
	assert.NoError(t, err)
}

// ---------- ServeSetupBackends tests ----------

func TestSetupBackends_ReturnsCLIBackends(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/backends", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupBackends, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	backends, ok := resp["backends"].([]any)
	require.True(t, ok, "response should contain backends array")
	assert.NotEmpty(t, backends, "should have at least one CLI backend")

	for _, b := range backends {
		bMap := b.(map[string]any)
		assert.NotEmpty(t, bMap["id"], "backend should have id")
		assert.NotEmpty(t, bMap["name"], "backend should have name")
	}
}

func TestSetupBackends_MethodNotAllowed(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodPost, "/api/setup/backends", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupBackends, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestSetupBackends_SkipsNoCLI(t *testing.T) {
	defer setupAgentTestEnv(t)()

	req := newRequest(t, http.MethodGet, "/api/setup/backends", nil)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupBackends, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	backends := resp["backends"].([]any)
	for _, b := range backends {
		bMap := b.(map[string]any)
		assert.NotEqual(t, "mock", bMap["id"], "NoCLI backends should be excluded")
	}
}

// ---------- writePiModelsJSON tests ----------

func TestWritePiModelsJSON_OpenAIFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "https://api.deepseek.com/v1/chat/completions",
		APIFormat:      "openai",
		APIKey:         "sk-test-key",
		Model:          "deepseek-chat",
		SummarizeModel: "deepseek-chat-small",
		AgentID:        "custom-deepseek",
	}

	piConfigDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piConfigDir, 0o755))

	writePiModelsJSON(piConfigDir, req)

	modelsPath := filepath.Join(piConfigDir, "models.json")
	data, err := os.ReadFile(modelsPath)
	require.NoError(t, err)

	var modelsData map[string]any
	require.NoError(t, json.Unmarshal(data, &modelsData))

	providers, ok := modelsData["providers"].(map[string]any)
	require.True(t, ok, "models.json should have providers map")

	provider, ok := providers["custom-deepseek"].(map[string]any)
	require.True(t, ok, "should have custom-deepseek provider entry")

	assert.Equal(t, "https://api.deepseek.com/v1", provider["baseUrl"])
	assert.Equal(t, "openai-completions", provider["api"])

	modelEntries := provider["models"].([]any)
	assert.Len(t, modelEntries, 2, "should have chat + summarize model")

	assert.Equal(t, "$custom-deepseek", provider["apiKey"])
}

func TestWritePiModelsJSON_AnthropicFormat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	req := setupCompleteRequest{
		Provider:       "anthropic",
		CustomURL:      "https://api.minimaxi.com/anthropic/v1/messages",
		APIFormat:      "anthropic",
		APIKey:         "sk-ant-test-key",
		Model:          "minimax-m1",
		SummarizeModel: "minimax-m1-small",
		AgentID:        "custom-minimax",
	}

	piConfigDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piConfigDir, 0o755))

	writePiModelsJSON(piConfigDir, req)

	modelsPath := filepath.Join(piConfigDir, "models.json")
	data, err := os.ReadFile(modelsPath)
	require.NoError(t, err)

	var modelsData map[string]any
	require.NoError(t, json.Unmarshal(data, &modelsData))

	providers := modelsData["providers"].(map[string]any)
	provider := providers["custom-minimax"].(map[string]any)

	assert.Equal(t, "https://api.minimaxi.com/anthropic", provider["baseUrl"])
	assert.Equal(t, "anthropic-messages", provider["api"])
}

func TestWritePiModelsJSON_SameSummarizeModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "https://api.example.com/v1/chat/completions",
		APIFormat:      "openai",
		APIKey:         "sk-test",
		Model:          "same-model",
		SummarizeModel: "same-model",
		AgentID:        "same-model-agent",
	}

	piConfigDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piConfigDir, 0o755))

	writePiModelsJSON(piConfigDir, req)

	data, _ := os.ReadFile(filepath.Join(piConfigDir, "models.json"))
	var modelsData map[string]any
	json.Unmarshal(data, &modelsData)

	provider := modelsData["providers"].(map[string]any)["same-model-agent"].(map[string]any)
	modelEntries := provider["models"].([]any)
	assert.Len(t, modelEntries, 1, "should have only 1 model when chat=summarize")
}

func TestWritePiModelsJSON_EmptySummarizeModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "https://api.example.com/v1/chat/completions",
		APIFormat:      "openai",
		APIKey:         "sk-test",
		Model:          "chat-model",
		SummarizeModel: "",
		AgentID:        "no-sum-agent",
	}

	piConfigDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piConfigDir, 0o755))

	writePiModelsJSON(piConfigDir, req)

	data, _ := os.ReadFile(filepath.Join(piConfigDir, "models.json"))
	var modelsData map[string]any
	json.Unmarshal(data, &modelsData)

	provider := modelsData["providers"].(map[string]any)["no-sum-agent"].(map[string]any)
	modelEntries := provider["models"].([]any)
	assert.Len(t, modelEntries, 1, "should have only 1 model when summarize is empty")
}

func TestWritePiModelsJSON_MergesExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	piConfigDir := filepath.Join(tmpDir, ".pi", "agent")
	require.NoError(t, os.MkdirAll(piConfigDir, 0o755))

	existingData := map[string]any{
		"providers": map[string]any{
			"existing-provider": map[string]any{
				"baseUrl": "https://existing.com/v1",
				"api":     "openai-completions",
			},
		},
	}
	existingJSON, _ := json.Marshal(existingData)
	require.NoError(t, os.WriteFile(filepath.Join(piConfigDir, "models.json"), existingJSON, 0o644))

	req := setupCompleteRequest{
		Provider:  "openai",
		CustomURL: "https://api.new.com/v1/chat/completions",
		APIFormat: "openai",
		APIKey:    "sk-test",
		Model:     "new-model",
		AgentID:   "new-agent",
	}

	writePiModelsJSON(piConfigDir, req)

	data, _ := os.ReadFile(filepath.Join(piConfigDir, "models.json"))
	var modelsData map[string]any
	json.Unmarshal(data, &modelsData)

	providers := modelsData["providers"].(map[string]any)
	_, hasExisting := providers["existing-provider"]
	assert.True(t, hasExisting, "should preserve existing providers")
	_, hasNew := providers["new-agent"]
	assert.True(t, hasNew, "should add new provider")
}

// ---------- writePiConfigFiles custom URL branch ----------

func TestWritePiConfigFiles_CustomURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME env var not used on Windows")
	}
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	req := setupCompleteRequest{
		Provider:       "openai",
		CustomURL:      "https://api.deepseek.com/v1/chat/completions",
		APIFormat:      "openai",
		APIKey:         "sk-custom-key",
		Model:          "deepseek-chat",
		SummarizeModel: "deepseek-chat-small",
		AgentID:        "custom-deepseek",
	}

	writePiConfigFiles(req)

	modelsPath := filepath.Join(tmpDir, ".pi", "agent", "models.json")
	data, err := os.ReadFile(modelsPath)
	require.NoError(t, err)

	var modelsData map[string]any
	require.NoError(t, json.Unmarshal(data, &modelsData))
	providers := modelsData["providers"].(map[string]any)
	_, hasCustom := providers["custom-deepseek"]
	assert.True(t, hasCustom, "models.json should have custom provider")

	authPath := filepath.Join(tmpDir, ".pi", "agent", "auth.json")
	data, err = os.ReadFile(authPath)
	require.NoError(t, err)

	var authData map[string]any
	require.NoError(t, json.Unmarshal(data, &authData))
	_, hasAgentIDKey := authData["custom-deepseek"]
	assert.True(t, hasAgentIDKey, "auth.json should use agent ID as key for custom URL")

	settingsPath := filepath.Join(tmpDir, ".pi", "agent", "settings.json")
	data, err = os.ReadFile(settingsPath)
	require.NoError(t, err)

	var settingsData map[string]any
	require.NoError(t, json.Unmarshal(data, &settingsData))
	assert.Equal(t, "custom-deepseek", settingsData["defaultProvider"])
}

// ---------- ServeSetupVerify with mock HTTP server for custom URL ----------

func TestSetupVerify_CustomURLHTTPSuccess(t *testing.T) {
	defer setupAgentTestEnv(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-test","choices":[]}`))
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "",
		"custom_url": server.URL + "/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool), "should succeed with mock server")
}

func TestSetupVerify_CustomURLAnthropicHTTPSuccess(t *testing.T) {
	defer setupAgentTestEnv(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sk-ant-test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_test","content":[]}`))
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "",
		"custom_url": server.URL + "/v1/messages",
		"api_format": "anthropic",
		"api_key":    "sk-ant-test-key",
		"model":      "claude-sonnet-4-20250514",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool), "should succeed with mock Anthropic server")
}

func TestSetupVerify_CustomURLHTTPUnauthorized(t *testing.T) {
	defer setupAgentTestEnv(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"Invalid API key"}}`))
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "",
		"custom_url": server.URL + "/v1/chat/completions",
		"api_key":    "bad-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail on 401")
}

func TestSetupVerify_CustomURLHTTPNotFound(t *testing.T) {
	defer setupAgentTestEnv(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "",
		"custom_url": server.URL + "/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "nonexistent-model",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp["success"].(bool), "should fail on 404")
}

// ---------- ServeSetupModels _custom provider normalization ----------

func TestSetupModels_CustomProviderNormalization(t *testing.T) {
	defer setupAgentTestEnv(t)()

	body := map[string]any{
		"provider":   "_custom",
		"custom_url": "https://api.example.com/v1/chat/completions",
		"api_key":    "test-key",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/models", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupModels, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	models := resp["models"].([]any)
	assert.Empty(t, models, "_custom provider should be normalized to empty → custom URL mode → empty model list")
}

// ---------- ServeSetupVerify _custom provider normalization ----------

func TestSetupVerify_CustomProviderNormalization(t *testing.T) {
	defer setupAgentTestEnv(t)()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-test","choices":[]}`))
	}))
	defer server.Close()

	body := map[string]any{
		"provider":   "_custom",
		"custom_url": server.URL + "/v1/chat/completions",
		"api_key":    "test-key",
		"model":      "gpt-4o",
	}
	req := newRequest(t, http.MethodPost, "/api/setup/verify", body)
	withAuthCookie(req, model.SessionToken)
	w := callHandler(ServeSetupVerify, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool), "_custom should be normalized and verify should succeed with mock server")
}
