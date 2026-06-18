//nolint:goconst // JSON response field names are domain strings, not config constants
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/summarize"
)

// setupCompleteMu prevents duplicate agent creation from concurrent requests.
var setupCompleteMu sync.Mutex

// ---------- GET /api/setup/status ----------

// ServeSetupStatus returns whether the setup wizard is needed.
// Response: { needs_setup, embedded_agent, agent_version }
func ServeSetupStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	needsSetup := len(model.AgentList) == 0
	embeddedPath := model.EmbeddedAgentPath()
	embeddedAgent := embeddedPath != ""
	agentVersion := model.EmbeddedAgentVersion()

	writeJSON(w, http.StatusOK, map[string]any{
		"needs_setup":    needsSetup,
		"embedded_agent": embeddedAgent,
		"agent_version":  agentVersion,
	})
}

// ---------- GET /api/setup/providers ----------

// ServeSetupProviders returns the list of providers that can be configured
// via the setup wizard. Enterprise providers (WizardReady=false) are excluded.
func ServeSetupProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	providers := model.GetWizardProviders()
	result := make([]map[string]string, 0, len(providers))
	for _, p := range providers {
		result = append(result, map[string]string{
			"id":        p.ID,
			"name":      p.Name,
			"envVar":    p.EnvVar,
			"apiFormat": p.APIFormat,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers":            result,
		"custom_url_supported": true,
	})
}

// ---------- GET /api/setup/backends ----------

// ServeSetupBackends returns the list of AI backends supported by ClawBench.
// This is used by the setup wizard to show users what CLI agents can be auto-detected.
func ServeSetupBackends(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	type backendInfo struct {
		ID                   string   `json:"id"`
		Name                 string   `json:"name"`
		Icon                 string   `json:"icon"`
		Specialty            string   `json:"specialty"`
		DefaultCmd           string   `json:"default_cmd"`
		ThinkingEffortLevels []string `json:"thinking_effort_levels,omitempty"`
	}

	backends := make([]backendInfo, 0, len(model.GetBackendRegistry()))
	for _, spec := range model.GetBackendRegistry() {
		if spec.NoCLI {
			continue // skip non-CLI backends (e.g. mock)
		}
		backends = append(backends, backendInfo{
			ID:                   spec.ID,
			Name:                 spec.Name,
			Icon:                 spec.Icon,
			Specialty:            spec.Specialty,
			DefaultCmd:           spec.DefaultCmd,
			ThinkingEffortLevels: spec.ThinkingEffortLevels,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"backends": backends,
	})
}

// ---------- POST /api/setup/models ----------

// setupModelsRequest is the request body for POST /api/setup/models.
type setupModelsRequest struct {
	Provider  string `json:"provider"`
	CustomURL string `json:"custom_url"`
	APIKey    string `json:"api_key"`
	APIFormat string `json:"api_format"` // "openai" or "anthropic" (only for custom URL)
}

// ServeSetupModels lists available models for the selected provider.
// For providers with ModelsEndpoint: calls /v1/models via HTTP.
// For providers with KnownModels (Anthropic-format): returns hardcoded list.
//
//nolint:gocyclo // multiple provider resolution paths, each with distinct error handling
func ServeSetupModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	var req setupModelsRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Normalize: frontend sends "_custom" for custom URL mode — treat as empty
	if req.Provider == "_custom" {
		req.Provider = ""
	}

	// Custom URL mode: derive provider from api_format
	if req.Provider == "" {
		if req.CustomURL == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		// Validate custom URL format (auto-detects api_format from URL path)
		if errKey := validateCustomURL(req.CustomURL, req.APIFormat); errKey != "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, errKey)
			return
		}
		// Derive format from URL path (if api_format was auto-detected)
		req.APIFormat = detectAPIFormat(req.CustomURL, req.APIFormat)
		if req.APIFormat == "anthropic" {
			req.Provider = "anthropic"
		} else {
			req.Provider = "openai"
		}
	}

	spec := model.FindProviderSpec(req.Provider)
	if spec == nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderNotFound")
		return
	}

	// Custom URL mode: always return empty model list — the user's custom
	// endpoint may host entirely different models from the known provider's,
	// so auto-fetching is unreliable. Let the user enter model IDs manually.
	if req.CustomURL != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"models":               []map[string]any{},
			"summarize_model_hint": "",
			"error":                "Custom URL mode: please enter model IDs manually.",
		})
		return
	}

	// Anthropic-format providers: return KnownModels
	if len(spec.KnownModels) > 0 && spec.ModelsEndpoint == "" {
		models := make([]map[string]any, 0, len(spec.KnownModels))
		for _, m := range spec.KnownModels {
			models = append(models, map[string]any{
				"id":                m.ID,
				"name":              m.Name,
				"created":           0,
				"context_length":    m.ContextLength,
				"max_output_tokens": m.MaxOutputTokens,
				"supports_thinking": m.SupportsThinking,
				"cost_tier":         m.CostTier,
			})
		}
		hint := model.GetSummarizeModelHint(spec.KnownModels, nil)
		writeJSON(w, http.StatusOK, map[string]any{
			"models":               models,
			"summarize_model_hint": hint,
		})
		return
	}

	// OpenAI-compatible providers: call /v1/models endpoint
	modelsEndpoint := spec.ModelsEndpoint

	if modelsEndpoint == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ModelsEndpointNotAvailable")
		return
	}

	models, err := fetchModelsFromEndpoint(modelsEndpoint, req.APIKey)
	if err != nil {
		slog.Warn("failed to fetch models from endpoint", "provider", req.Provider, "endpoint", modelsEndpoint, "error", err)
		// Fallback to KnownModels if available
		if len(spec.KnownModels) > 0 {
			fallbackModels := make([]map[string]any, 0, len(spec.KnownModels))
			for _, m := range spec.KnownModels {
				fallbackModels = append(fallbackModels, map[string]any{
					"id":                m.ID,
					"name":              m.Name,
					"created":           0,
					"context_length":    m.ContextLength,
					"max_output_tokens": m.MaxOutputTokens,
					"supports_thinking": m.SupportsThinking,
					"cost_tier":         m.CostTier,
				})
			}
			hint := model.GetSummarizeModelHint(spec.KnownModels, nil)
			writeJSON(w, http.StatusOK, map[string]any{
				"models":               fallbackModels,
				"summarize_model_hint": hint,
				"error":                err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"models":               []map[string]any{},
			"summarize_model_hint": "",
			"error":                err.Error(),
		})
		return
	}

	hint := model.GetSummarizeModelHint(nil, models)
	writeJSON(w, http.StatusOK, map[string]any{
		"models":               models,
		"summarize_model_hint": hint,
	})
}

// ---------- POST /api/setup/verify ----------

// setupVerifyRequest is the request body for POST /api/setup/verify.
type setupVerifyRequest struct {
	Provider  string `json:"provider"`
	CustomURL string `json:"custom_url"`
	APIKey    string `json:"api_key"`
	Model     string `json:"model"`
	APIFormat string `json:"api_format"` // "openai" or "anthropic" (only for custom URL)
}

// ServeSetupVerify verifies the API key and model accessibility.
// For custom URL mode: sends a minimal HTTP request directly to the endpoint
// (OpenAI or Anthropic protocol based on URL path). This avoids shelling out
// to Pi CLI which doesn't natively support arbitrary custom endpoints.
// For built-in providers: uses the embedded Pi CLI as before.
//
//nolint:gocyclo // complex verify logic with multiple provider formats
func ServeSetupVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	var req setupVerifyRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.APIKey == "" || req.Model == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	// Normalize: frontend sends "_custom" for custom URL mode — treat as empty
	if req.Provider == "_custom" {
		req.Provider = ""
	}

	// Custom URL mode: derive provider from api_format
	if req.Provider == "" {
		// Validate custom URL format (auto-detects api_format from URL path)
		if req.CustomURL != "" {
			if errKey := validateCustomURL(req.CustomURL, req.APIFormat); errKey != "" {
				writeLocalizedErrorf(w, r, http.StatusBadRequest, errKey)
				return
			}
			req.APIFormat = detectAPIFormat(req.CustomURL, req.APIFormat)
		}
		if req.APIFormat == "anthropic" {
			req.Provider = "anthropic"
		} else {
			req.Provider = "openai"
		}
	}

	spec := model.FindProviderSpec(req.Provider)
	if spec == nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderNotFound")
		return
	}

	// Custom URL mode: verify via direct HTTP request (fast, no CLI dependency)
	if req.CustomURL != "" {
		verifyCustomURLHTTP(w, r, req)
		return
	}

	// Built-in provider mode: verify via Pi CLI
	piPath := model.EmbeddedAgentPath()
	if piPath == "" {
		writeLocalizedErrorf(w, r, http.StatusNotFound, "EmbeddedAgentNotFound")
		return
	}

	// Build Pi CLI command
	args := make([]string, 0, 9)
	args = append(args, "-p", "--mode", "json", "--provider", req.Provider, "--model", req.Model, "--no-tools", "ping")

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, piPath, args...)

	// Inject API key via environment variable
	envVar := spec.EnvVar
	if envVar != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s", envVar, req.APIKey))
	} else {
		cmd.Env = os.Environ()
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			errMsg = err.Error()
		}
		slog.Warn("setup verify failed", "provider", req.Provider, "model", req.Model, "error", err, "output", errMsg)

		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": "验证失败：API Key 无效或模型不可用。请检查后重试。",
			"model":   req.Model,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "配置验证成功！智能体工作正常。",
		"model":   req.Model,
	})
}

// ---------- POST /api/setup/complete ----------

// setupCompleteRequest is the request body for POST /api/setup/complete.
type setupCompleteRequest struct {
	Provider       string `json:"provider"`
	CustomURL      string `json:"custom_url"`
	APIFormat      string `json:"api_format"` // "openai" or "anthropic" (only for custom URL)
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	SummarizeModel string `json:"summarize_model"`
	AgentName      string `json:"agent_name"`
	AgentID        string `json:"agent_id"`
}

// ServeSetupComplete finalizes the setup wizard by creating the agent in the database,
// encrypting the API key, and writing Pi config files.
func ServeSetupComplete(w http.ResponseWriter, r *http.Request) { //nolint:gocyclo // multi-step setup completion
	if r.Method != http.MethodPost {
		writeLocalizedErrorf(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed")
		return
	}

	var req setupCompleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if req.APIKey == "" || req.Model == "" || req.AgentName == "" || req.AgentID == "" {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
		return
	}

	// Normalize: frontend sends "_custom" for custom URL mode — treat as empty
	if req.Provider == "_custom" {
		req.Provider = ""
	}

	// Custom URL mode: derive provider from api_format
	if req.Provider == "" {
		if req.CustomURL == "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, "InvalidRequestBody")
			return
		}
		// Validate custom URL format (auto-detects api_format from URL path)
		if errKey := validateCustomURL(req.CustomURL, req.APIFormat); errKey != "" {
			writeLocalizedErrorf(w, r, http.StatusBadRequest, errKey)
			return
		}
		// Derive format from URL path (if api_format was auto-detected)
		req.APIFormat = detectAPIFormat(req.CustomURL, req.APIFormat)
		if req.APIFormat == "anthropic" {
			req.Provider = "anthropic"
		} else {
			req.Provider = "openai"
		}
	}

	spec := model.FindProviderSpec(req.Provider)
	if spec == nil {
		writeLocalizedErrorf(w, r, http.StatusBadRequest, "ProviderNotFound")
		return
	}

	// Concurrency guard: prevent duplicate agent creation
	if !setupCompleteMu.TryLock() {
		writeLocalizedErrorf(w, r, http.StatusConflict, "Conflict")
		return
	}
	defer setupCompleteMu.Unlock()

	// Check for duplicate agent ID
	if _, exists := model.Agents[req.AgentID]; exists {
		writeLocalizedErrorf(w, r, http.StatusConflict, "AgentAlreadyExists")
		return
	}

	// Determine Pi binary path
	piPath := model.EmbeddedAgentPath()

	// 1. Write Pi config files (auth.json, settings.json) — best effort, don't block on failure
	if piPath != "" {
		writePiConfigFiles(req)
	}

	// 2. Insert agent + API key in a DB transaction for atomicity
	tx, err := service.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("failed to begin transaction", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}
	defer func() { _ = tx.Rollback() }()

	agent := &model.Agent{
		ID:                 req.AgentID,
		Name:               req.AgentName,
		Icon:               "🥧",
		Specialty:          "极简编程智能体",
		Backend:            "pi",
		Command:            piPath,
		PreferredModel:     req.Model,
		Source:             "setup",
		ModelsAutoDetected: false,
	}

	// Populate models from provider's KnownModels so the SessionSettingModal shows them immediately
	if len(spec.KnownModels) > 0 {
		agent.Models = model.KnownModelsToAgentModels(spec.KnownModels)
		agent.ModelsAutoDetected = true
	}

	// Populate ThinkingEffortLevels from backend spec
	if bspec := model.FindSpecByBackend("pi"); bspec != nil {
		agent.ThinkingEffortLevels = bspec.ThinkingEffortLevels
		agent.CanRefreshModels = model.CanDiscoverModels(*bspec)
	}

	if err := service.SaveAgent(tx, agent); err != nil {
		slog.Error("failed to save agent to DB", "agent_id", req.AgentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 3. Encrypt and store API key
	// For custom URL mode: store provider as agent ID so injectAgentAPIKey
	// knows to use --provider {agentID} instead of a built-in provider name.
	apiKeyProvider := req.Provider
	if req.CustomURL != "" {
		apiKeyProvider = req.AgentID
	}
	if err := service.SaveAgentAPIKey(tx, req.AgentID, apiKeyProvider, req.CustomURL, req.APIKey); err != nil {
		slog.Error("failed to save API key", "agent_id", req.AgentID, "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	if err := tx.Commit(); err != nil {
		slog.Error("failed to commit transaction", "error", err)
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
		return
	}

	// 4. Auto-configure summarize backend
	if spec.ChatEndpoint != "" && req.SummarizeModel != "" {
		configureSummarizeBackend(req, spec)
	} else {
		slog.Warn("skipping summarize backend configuration",
			"chat_endpoint", spec.ChatEndpoint, "summarize_model", req.SummarizeModel,
			"reason", map[bool]string{true: "empty model", false: "empty endpoint"}[req.SummarizeModel == ""])
	}

	// 5. Reload agents from DB into memory
	if err := service.LoadAgentsIntoMemory(service.DB); err != nil {
		slog.Error("failed to reload agents into memory", "error", err)
	}

	slog.Info("setup complete: agent created", "agent_id", req.AgentID, "provider", req.Provider, "model", req.Model)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":          true,
		"agent":            model.Agents[req.AgentID],
		"default_agent_id": model.GetDefaultAgentID(),
	})
}

// configureSummarizeBackend auto-configures the summarize backend after wizard completion.
// Writes config.yaml with backend:api settings and reinitializes the global summarizer.
func configureSummarizeBackend(req setupCompleteRequest, spec *model.ProviderSpec) {
	// Write summarize config to config.yaml
	chatEndpoint := spec.ChatEndpoint
	if req.CustomURL != "" {
		chatEndpoint = req.CustomURL
	}

	summarizePatch := map[string]any{
		"summarize": map[string]any{
			"backend": "api",
			"model":   req.SummarizeModel,
			"api": map[string]any{
				"base_url": chatEndpoint,
				"key":      req.APIKey,
				"format":   spec.APIFormat,
				"agent_id": req.AgentID,
			},
		},
	}

	if err := writeConfigYAML(summarizePatch); err != nil {
		slog.Warn("failed to write summarize config to config.yaml", "error", err)
		// Non-fatal: summarize will fall back to SimpleSummarizer
	}

	// Update in-memory config
	model.ConfigInstance.Summarize.Backend = "api"
	model.ConfigInstance.Summarize.Model = req.SummarizeModel
	model.ConfigInstance.Summarize.API.BaseURL = chatEndpoint
	model.ConfigInstance.Summarize.API.Key = req.APIKey
	model.ConfigInstance.Summarize.API.Format = spec.APIFormat
	model.ConfigInstance.Summarize.API.AgentID = req.AgentID

	// Reinitialize the global summarizer at runtime
	reinitSummarizer(req, spec)
}

// reinitSummarizer creates a new OpenAISummarizer/AnthropicSummarizer from the
// request data and updates the global summarizer.
func reinitSummarizer(req setupCompleteRequest, spec *model.ProviderSpec) {
	chatEndpoint := spec.ChatEndpoint
	if req.CustomURL != "" {
		chatEndpoint = req.CustomURL
	}

	apiKey := req.APIKey
	if apiKey == "" {
		slog.Warn("no API key available for summarize reinit", "agent_id", req.AgentID, "provider", req.Provider)
		return
	}

	// Create the appropriate summarizer based on API format
	var newSummarizer summarize.Summarizer
	switch spec.APIFormat {
	case "openai":
		newSummarizer = summarize.NewOpenAI(chatEndpoint, apiKey, req.SummarizeModel)
	case "anthropic":
		newSummarizer = summarize.NewAnthropic(chatEndpoint, apiKey, req.SummarizeModel)
	default:
		slog.Warn("unknown API format for summarize reinit", "format", spec.APIFormat)
		return
	}

	SetSummarizer(newSummarizer)
	slog.Info("reinitialized summarize backend", "provider", req.Provider, "model", req.SummarizeModel, "format", spec.APIFormat)
}

// ---------- Helper functions ----------

// verifyCustomURLHTTP verifies a custom URL endpoint by sending a minimal
// HTTP request directly (no Pi CLI). This is fast and works with any
// OpenAI-compatible or Anthropic-compatible endpoint.
func verifyCustomURLHTTP(w http.ResponseWriter, r *http.Request, req setupVerifyRequest) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var verifyErr error
	switch req.APIFormat {
	case "anthropic":
		verifyErr = verifyAnthropicHTTP(ctx, req.CustomURL, req.APIKey, req.Model)
	default: // openai
		verifyErr = verifyOpenAIHTTP(ctx, req.CustomURL, req.APIKey, req.Model)
	}

	if verifyErr != nil {
		slog.Warn("setup verify (HTTP) failed", "format", req.APIFormat, "url", req.CustomURL, "model", req.Model, "error", verifyErr)
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"message": "验证失败：API Key 无效或模型不可用。请检查后重试。",
			"model":   req.Model,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "配置验证成功！智能体工作正常。",
		"model":   req.Model,
	})
}

// verifyOpenAIHTTP sends a minimal OpenAI Chat Completions request to verify
// the endpoint is reachable and the API key + model are valid.
func verifyOpenAIHTTP(ctx context.Context, endpoint, apiKey, model string) error {
	reqBody := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 5,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain body to reuse connection
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API key invalid (status 401)")
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("model %q not found (status 404)", model)
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("access denied (status 403)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// verifyAnthropicHTTP sends a minimal Anthropic Messages request to verify
// the endpoint is reachable and the API key + model are valid.
func verifyAnthropicHTTP(ctx context.Context, endpoint, apiKey, model string) error {
	reqBody := map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 5,
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain body to reuse connection
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API key invalid (status 401)")
	}
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("model %q not found (status 404)", model)
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("access denied (status 403)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

// validateCustomURL checks that a custom URL is a valid HTTP(S) URL and
// its path suffix matches a known API format (OpenAI or Anthropic).
// If apiFormat is empty, it is auto-detected from the URL path.
// Returns an error message key if invalid, empty string if OK.
func validateCustomURL(customURL, apiFormat string) string {
	if customURL == "" {
		return ""
	}
	// Parse and validate URL scheme + host
	parsed, err := url.Parse(customURL)
	if err != nil {
		return "CustomURLInvalid"
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "CustomURLInvalidScheme"
	}
	if parsed.Host == "" {
		return "CustomURLInvalidHost"
	}
	// Auto-detect format from URL path if not specified
	detectedFormat := apiFormat
	if detectedFormat == "" {
		if strings.HasSuffix(parsed.Path, "/v1/messages") {
			detectedFormat = "anthropic" //nolint:ineffassign // used in else branch below
		} else if strings.HasSuffix(parsed.Path, "/chat/completions") {
			detectedFormat = "openai" //nolint:ineffassign // used in else branch below
		} else {
			return "CustomURLUnrecognizedFormat"
		}
	} else {
		// Validate path suffix matches the declared format
		switch detectedFormat {
		case "anthropic":
			if !strings.HasSuffix(parsed.Path, "/v1/messages") {
				return "CustomURLAnthropicFormat"
			}
		default: // openai
			if !strings.HasSuffix(parsed.Path, "/chat/completions") {
				return "CustomURLOpenAIFormat"
			}
		}
	}
	return ""
}

// detectAPIFormat returns the API format based on the URL path suffix.
// If apiFormat is already set (non-empty), it is returned as-is.
// Otherwise, the format is auto-detected from the URL path:
//   - ends with /v1/messages → "anthropic"
//   - ends with /chat/completions → "openai"
//   - otherwise → "openai" (default)
func detectAPIFormat(customURL, apiFormat string) string {
	if apiFormat != "" {
		return apiFormat
	}
	if strings.HasSuffix(customURL, "/v1/messages") {
		return "anthropic"
	}
	return "openai"
}

// deriveModelsURL replaces the last path segment of a URL with "models".
// E.g., "https://api.example.com/v1/chat/completions" → "https://api.example.com/v1/models"
func deriveModelsURL(baseURL string) string {
	lastSlash := strings.LastIndex(baseURL, "/")
	if lastSlash <= 0 {
		return ""
	}
	parent := baseURL[:lastSlash]
	return parent + "/models"
}

// fetchModelsFromEndpoint calls the OpenAI-compatible /v1/models endpoint
// and returns the parsed model list.
func fetchModelsFromEndpoint(endpoint, apiKey string) ([]model.ModelInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequestWithContext(context.Background(), "GET", endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	// Parse OpenAI /v1/models response
	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name,omitempty"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse models response: %w", err)
	}

	models := make([]model.ModelInfo, 0, len(result.Data))
	for _, m := range result.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, model.ModelInfo{
			ID:      m.ID,
			Name:    name,
			Created: m.Created,
		})
	}

	return models, nil
}

// writePiConfigFiles writes Pi CLI configuration files (auth.json, settings.json,
// and models.json for custom URL mode).
// These are best-effort writes — failures are logged but don't block setup completion.
func writePiConfigFiles(req setupCompleteRequest) {
	// Determine Pi config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get home dir for Pi config", "error", err)
		return
	}
	piConfigDir := filepath.Join(homeDir, ".pi", "agent")
	if err := os.MkdirAll(piConfigDir, 0o755); err != nil {
		slog.Warn("failed to create Pi config dir", "dir", piConfigDir, "error", err)
		return
	}

	// For custom URL mode: register a custom provider in models.json
	// so Pi knows which endpoint to connect to and which API format to use.
	if req.CustomURL != "" {
		writePiModelsJSON(piConfigDir, req)
	}

	// Write auth.json — merge with existing entries using Pi's structured format
	// Pi expects: { "provider": { "type": "api_key", "key": "..." } }
	// NOT the old flat format: { "ENV_VAR": "..." }
	authPath := filepath.Join(piConfigDir, "auth.json")
	authData := make(map[string]any)
	if existing, err := os.ReadFile(authPath); err == nil {
		_ = json.Unmarshal(existing, &authData)
	}
	// Use Pi provider ID as key:
	// - Built-in providers: e.g., "minimax-cn", "deepseek"
	// - Custom URL: use agent ID as the custom provider name (e.g., "custom-agent")
	authKey := req.Provider
	if req.CustomURL != "" {
		authKey = req.AgentID
	}
	authData[authKey] = map[string]string{
		"type": "api_key",
		"key":  req.APIKey,
	}
	authJSON, _ := json.Marshal(authData)
	if err := atomicWriteFile(authPath, authJSON, 0o600); err != nil {
		slog.Warn("failed to write Pi auth.json", "error", err)
	}

	// Write settings.json
	settingsData := map[string]string{
		"defaultProvider": authKey,
		"defaultModel":    req.Model,
	}
	settingsJSON, _ := json.Marshal(settingsData)
	if err := atomicWriteFile(filepath.Join(piConfigDir, "settings.json"), settingsJSON, 0o644); err != nil {
		slog.Warn("failed to write Pi settings.json", "error", err)
	}
}

// writePiModelsJSON registers a custom provider in Pi's models.json file.
// This tells Pi the baseUrl, API format, and model IDs for the custom endpoint.
// The provider name is the agent ID (e.g., "custom-agent").
func writePiModelsJSON(piConfigDir string, req setupCompleteRequest) {
	modelsPath := filepath.Join(piConfigDir, "models.json")

	// Read existing models.json
	modelsData := make(map[string]any)
	if existing, err := os.ReadFile(modelsPath); err == nil {
		_ = json.Unmarshal(existing, &modelsData)
	}

	// Ensure "providers" key exists
	providers, ok := modelsData["providers"].(map[string]any)
	if !ok {
		providers = make(map[string]any)
	}

	// Determine Pi API type from our api_format
	piAPI := "openai-completions"
	if req.APIFormat == "anthropic" {
		piAPI = "anthropic-messages"
	}

	// Derive baseUrl from the custom URL by stripping Pi's auto-appended path suffix.
	// Pi automatically appends path segments based on the API type:
	//   - openai-completions: appends /chat/completions  → strip /chat/completions
	//   - anthropic-messages: appends /v1/messages       → strip /v1/messages
	// Examples:
	//   "https://api.deepseek.com/v1/chat/completions"  → "https://api.deepseek.com/v1"
	//   "https://api.minimaxi.com/anthropic/v1/messages" → "https://api.minimaxi.com/anthropic"
	parsed, err := url.Parse(req.CustomURL)
	baseURL := ""
	if err == nil {
		path := parsed.Path
		if piAPI == "openai-completions" && strings.HasSuffix(path, "/chat/completions") {
			path = strings.TrimSuffix(path, "/chat/completions")
		} else if piAPI == "anthropic-messages" && strings.HasSuffix(path, "/v1/messages") {
			path = strings.TrimSuffix(path, "/v1/messages")
		}
		baseURL = fmt.Sprintf("%s://%s%s", parsed.Scheme, parsed.Host, path)
	}

	// Build provider entry
	modelEntries := []map[string]any{
		{
			"id":        req.Model,
			"reasoning": false,
			"input":     []string{"text"},
			"cost":      map[string]float64{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		},
	}
	// If summarize model is different, add it too
	if req.SummarizeModel != "" && req.SummarizeModel != req.Model {
		modelEntries = append(modelEntries, map[string]any{
			"id":        req.SummarizeModel,
			"reasoning": false,
			"input":     []string{"text"},
			"cost":      map[string]float64{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
		})
	}

	providerEntry := map[string]any{
		"baseUrl": baseURL,
		"api":     piAPI,
		"apiKey":  fmt.Sprintf("$%s", req.AgentID), // env var reference — set at runtime
		"models":  modelEntries,
	}

	providers[req.AgentID] = providerEntry
	modelsData["providers"] = providers

	modelsJSON, _ := json.MarshalIndent(modelsData, "", "  ")
	if err := atomicWriteFile(modelsPath, modelsJSON, 0o644); err != nil {
		slog.Warn("failed to write Pi models.json", "error", err)
	}
}

// atomicWriteFile writes data to a file atomically (write to .tmp, then rename).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
