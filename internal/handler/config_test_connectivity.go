package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/speech"
	"clawbench/internal/summarize"
)

// ConnectivityTestResult is the JSON response for POST /api/config/test.
type ConnectivityTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// JSON key constants for goconst compliance.
const (
	strAPI      = "api"
	strMessages = "messages"
	strPiper    = "piper"
	strKokoro   = "kokoro"
	strMossNano = "moss-nano"
	strReqError = "error"
)

type connectivityTestRequest struct {
	Category string         `json:"category"` // "frp" | "summarize_text" | "summarize_voice" | "rag" | "dingtalk" | "port_forward" | "tts"
	Values   map[string]any `json:"values"`   // Flat dot-path key-value map from the form
}

// ServeConfigTest handles POST /api/config/test — test connectivity for a settings category.
// It receives the form's current values (which may differ from saved config) and tests
// whether the specified service is reachable.
func ServeConfigTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var req connectivityTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "Invalid request body"})
		return
	}

	if req.Category == "" || req.Values == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "category and values are required"})
		return
	}

	var result ConnectivityTestResult
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	switch req.Category {
	case "frp":
		result = testFRP(ctx, req.Values)
	case "summarize_text":
		result = testSummarizeText(ctx, req.Values)
	case "summarize_voice":
		result = testSummarizeVoice(ctx, req.Values)
	case "rag":
		result = testRAG(ctx, req.Values)
	case "dingtalk":
		result = testDingTalk(ctx, req.Values)
	case "feishu":
		result = testFeishu(ctx, req.Values)
	case "port_forward":
		result = testPortForward(ctx, req.Values)
	case "tts":
		result = testTTS(ctx, req.Values)
	default:
		result = ConnectivityTestResult{Success: false, Message: "Unknown category: " + req.Category}
	}

	writeJSON(w, http.StatusOK, result)
}

// ── Helpers ──────────────────────────────────────────────────

// buildEndpointURL constructs a full API endpoint URL from a base URL and a default path.
// If the base URL already ends with the target suffix (e.g., "/chat/completions"), it's used as-is.
// If the base URL already ends with a path component that overlaps with the default path,
// only the remaining suffix is appended.
// Examples for defaultPath="/v1/chat/completions":
//   - "https://api.openai.com" → "https://api.openai.com/v1/chat/completions"
//   - "https://api.openai.com/v1" → "https://api.openai.com/v1/chat/completions"
//   - "https://api.openai.com/v1/chat/completions" → "https://api.openai.com/v1/chat/completions"
func buildEndpointURL(baseURL, defaultPath string) string {
	u := strings.TrimRight(baseURL, "/")
	// Extract the final path component (e.g., "/chat/completions")
	lastSlash := strings.LastIndex(defaultPath, "/")
	if lastSlash < 0 {
		return u + "/" + defaultPath
	}
	suffix := defaultPath[lastSlash:] // e.g., "/chat/completions"

	// If URL already ends with the full suffix, it's complete
	if strings.HasSuffix(u, suffix) {
		return u
	}

	// Check for partial overlap: split defaultPath into segments and find the longest match
	segments := strings.Split(strings.TrimLeft(defaultPath, "/"), "/")
	// Try matching from longest prefix to shortest
	for i := len(segments) - 1; i >= 1; i-- {
		prefix := "/" + strings.Join(segments[:i], "/")
		if strings.HasSuffix(u, prefix) {
			remaining := "/" + strings.Join(segments[i:], "/")
			return u + remaining
		}
	}

	return u + defaultPath
}

// resolveStringValue returns the value from the test request if present and not empty,
// otherwise falls back to the current config value.
// Empty strings fall back to config since the frontend may send "" for
// fields the user hasn't edited.
func resolveStringValue(values map[string]any, key string, currentConfigValue string) string {
	v, ok := values[key]
	if !ok {
		return currentConfigValue
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	// Empty string — fall back to current config value
	if s == "" {
		return currentConfigValue
	}
	return s
}

// resolveIntValue extracts an integer from the values map, falling back to default.
func resolveIntValue(values map[string]any, key string, defaultVal int) int {
	v, ok := values[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return defaultVal
		}
		return i
	}
	return defaultVal
}

// ── FRP ──────────────────────────────────────────────────────

func testFRP(ctx context.Context, values map[string]any) ConnectivityTestResult {
	addr := resolveStringValue(values, "frp.server_addr", model.ConfigInstance.FRP.ServerAddr)
	port := resolveIntValue(values, "frp.server_port", model.ConfigInstance.FRP.ServerPort)

	if addr == "" {
		return ConnectivityTestResult{Success: false, Message: "Server address is required"}
	}
	if port == 0 {
		port = 7000 // default frps port
	}

	target := fmt.Sprintf("%s:%d", addr, port)
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to %s: %v", target, err),
		}
	}
	_ = conn.Close()
	return ConnectivityTestResult{
		Success: true,
		Message: fmt.Sprintf("Successfully connected to %s", target),
	}
}

// ── Summarize Text ───────────────────────────────────────────

func testSummarizeText(ctx context.Context, values map[string]any) ConnectivityTestResult {
	backend := resolveStringValue(values, "summarize.backend", model.ConfigInstance.Summarize.Backend)
	if backend != strAPI {
		return ConnectivityTestResult{Success: true, Message: "Text summary backend is not API mode, no test needed"}
	}

	baseURL := resolveStringValue(values, "summarize.api.base_url", model.ConfigInstance.Summarize.API.BaseURL)
	apiKey := resolveStringValue(values, "summarize.api.key", model.ConfigInstance.Summarize.API.Key)
	modelName := resolveStringValue(values, "summarize.model", model.ConfigInstance.Summarize.Model)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "API base URL is required"}
	}
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	return testAPISummarizer(ctx, baseURL, apiKey, modelName)
}

// ── Summarize Voice ──────────────────────────────────────────

func testSummarizeVoice(ctx context.Context, values map[string]any) ConnectivityTestResult {
	ttsBackend := resolveStringValue(values, "summarize.tts_backend", model.ConfigInstance.Summarize.TTSBackend)
	if ttsBackend != strAPI {
		return ConnectivityTestResult{Success: true, Message: "Voice summary backend is not API mode, no test needed"}
	}

	baseURL := resolveStringValue(values, "summarize.tts_api.base_url", model.ConfigInstance.Summarize.TTSAPI.BaseURL)
	apiKey := resolveStringValue(values, "summarize.tts_api.key", model.ConfigInstance.Summarize.TTSAPI.Key)
	modelName := resolveStringValue(values, "summarize.tts_model", model.ConfigInstance.Summarize.TTSModel)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "TTS API base URL is required"}
	}
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	return testAPISummarizer(ctx, baseURL, apiKey, modelName)
}

// testAPISummarizer sends a minimal chat completion request to verify API connectivity.
// The API format (OpenAI vs Anthropic) is auto-detected from the base URL:
//   - URLs containing "anthropic.com" or ending with "/v1/messages" → Anthropic format
//   - All other URLs → OpenAI format
func testAPISummarizer(ctx context.Context, baseURL, apiKey, modelName string) ConnectivityTestResult {
	url := strings.TrimRight(baseURL, "/")

	client := &http.Client{Timeout: 10 * time.Second}

	if summarize.IsAnthropicURL(url) {
		return testAnthropicAPI(ctx, client, url, apiKey, modelName)
	}
	return testOpenAIAPI(ctx, client, url, apiKey, modelName)
}

func testOpenAIAPI(ctx context.Context, client *http.Client, baseURL, apiKey, modelName string) ConnectivityTestResult {
	// Build the full chat completions URL
	reqURL := buildEndpointURL(baseURL, "/v1/chat/completions")

	reqBody := map[string]any{
		"model":      modelName,
		strMessages:  []map[string]string{{"role": strUser, "content": "hi"}},
		"max_tokens": 1,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(string(body)))
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to connect to %s: %v", reqURL, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode == http.StatusOK {
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("API connection successful (model: %s)", modelName)}
	}

	// Try to extract error message from response
	var errResp map[string]any
	if json.Unmarshal(respBody, &errResp) == nil {
		if e, ok := errResp[strReqError].(map[string]any); ok {
			if msg, ok := e["message"].(string); ok {
				return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, msg)}
			}
		}
	}

	return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("API returned HTTP %d", resp.StatusCode)}
}

func testAnthropicAPI(ctx context.Context, client *http.Client, baseURL, apiKey, modelName string) ConnectivityTestResult {
	// Build the full messages URL
	reqURL := buildEndpointURL(baseURL, "/v1/messages")

	reqBody := map[string]any{
		"model":      modelName,
		strMessages:  []map[string]string{{"role": strUser, "content": "hi"}},
		"max_tokens": 1,
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(string(body)))
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if apiKey != "" {
		req.Header.Set("x-api-key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to connect to %s: %v", reqURL, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	if resp.StatusCode == http.StatusOK {
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("API connection successful (model: %s, format: anthropic)", modelName)}
	}

	var errResp map[string]any
	if json.Unmarshal(respBody, &errResp) == nil {
		if e, ok := errResp[strReqError].(map[string]any); ok {
			if msg, ok := e["message"].(string); ok {
				return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, msg)}
			}
		}
	}

	return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("API returned HTTP %d", resp.StatusCode)}
}

// ── RAG ──────────────────────────────────────────────────────

func testRAG(ctx context.Context, values map[string]any) ConnectivityTestResult {
	baseURL := resolveStringValue(values, "rag.base_url", model.ConfigInstance.RAG.BaseURL)
	ragModel := resolveStringValue(values, "rag.model", model.ConfigInstance.RAG.Model)
	apiKey := resolveStringValue(values, "rag.api_key", model.ConfigInstance.RAG.APIKey)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "RAG base URL is required"}
	}
	if ragModel == "" {
		ragModel = "bge-m3"
	}

	url := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("RAG service unreachable at %s", baseURL)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// Server doesn't implement /v1/models (some Ollama versions)
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("RAG service reachable at %s (model check not supported by server)", baseURL)}
	}

	if resp.StatusCode != http.StatusOK {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("RAG service returned HTTP %d", resp.StatusCode)}
	}

	// Check if the model is available
	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("RAG service reachable at %s, but could not parse models list", baseURL)}
	}

	for _, m := range modelsResp.Data {
		if m.ID == ragModel || strings.HasPrefix(m.ID, ragModel+":") {
			return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("RAG service reachable, model '%s' available", ragModel)}
		}
	}

	return ConnectivityTestResult{
		Success: false,
		Message: fmt.Sprintf("RAG service reachable at %s, but model '%s' not found", baseURL, ragModel),
	}
}

// dingtalkTokenURL is the DingTalk API URL for getting an access token.
// Can be overridden in tests.
//
//nolint:gosec // G101: this is a public API endpoint, not a credential
var dingtalkTokenURL = "https://oapi.dingtalk.com/gettoken"

// ── DingTalk ─────────────────────────────────────────────────

func testDingTalk(ctx context.Context, values map[string]any) ConnectivityTestResult {
	appKey := resolveStringValue(values, "dingtalk.app_key", model.ConfigInstance.DingTalk.AppKey)
	appSecret := resolveStringValue(values, "dingtalk.app_secret", model.ConfigInstance.DingTalk.AppSecret)

	if appKey == "" || appSecret == "" {
		return ConnectivityTestResult{Success: false, Message: "App Key and App Secret are required"}
	}

	url := fmt.Sprintf("%s?appkey=%s&appsecret=%s", dingtalkTokenURL, appKey, appSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to connect to DingTalk: %v", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	var tokenResp struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to parse DingTalk response: %v", err)}
	}

	if tokenResp.ErrCode == 0 {
		return ConnectivityTestResult{Success: true, Message: "DingTalk connection successful (token obtained)"}
	}

	return ConnectivityTestResult{
		Success: false,
		Message: fmt.Sprintf("DingTalk authentication failed: %s (code %d)", tokenResp.ErrMsg, tokenResp.ErrCode),
	}
}

// feishuTokenURL is the Feishu API URL for getting a tenant access token.
// Can be overridden in tests.
//
//nolint:gosec // G101: this is a public API endpoint, not a credential
var feishuTokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"

// ── Feishu ───────────────────────────────────────────────────

func testFeishu(ctx context.Context, values map[string]any) ConnectivityTestResult {
	appID := resolveStringValue(values, "feishu.app_id", model.ConfigInstance.Feishu.AppID)
	appSecret := resolveStringValue(values, "feishu.app_secret", model.ConfigInstance.Feishu.AppSecret)

	if appID == "" || appSecret == "" {
		return ConnectivityTestResult{Success: false, Message: "App ID and App Secret are required"}
	}

	reqBody := map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	}
	bodyJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuTokenURL, strings.NewReader(string(bodyJSON)))
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to connect to Feishu: %v", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	var tokenResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to parse Feishu response: %v", err)}
	}

	if tokenResp.Code == 0 {
		return ConnectivityTestResult{Success: true, Message: "Feishu connection successful (token obtained)"}
	}

	return ConnectivityTestResult{
		Success: false,
		Message: fmt.Sprintf("Feishu authentication failed: %s (code %d)", tokenResp.Msg, tokenResp.Code),
	}
}

// ── Port Forward ─────────────────────────────────────────────

func testPortForward(ctx context.Context, _ map[string]any) ConnectivityTestResult {
	sshSrv := GetSSHServer()
	if sshSrv == nil {
		return ConnectivityTestResult{Success: false, Message: "SSH tunnel server is not running"}
	}

	port := sshSrv.Port()
	if port == 0 {
		return ConnectivityTestResult{Success: false, Message: "SSH tunnel server is not listening"}
	}

	target := fmt.Sprintf("localhost:%d", port)
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("SSH tunnel server is not listening on port %d", port),
		}
	}
	_ = conn.Close()

	return ConnectivityTestResult{
		Success: true,
		Message: fmt.Sprintf("SSH tunnel server is listening on port %d", port),
	}
}

// ── TTS ──────────────────────────────────────────────────────

func testTTS(ctx context.Context, values map[string]any) ConnectivityTestResult {
	engine := resolveStringValue(values, "tts.engine", model.ConfigInstance.TTS.Engine)
	if engine == "" {
		engine = "edge"
	}

	switch engine {
	case "edge":
		return testTTSEdge(ctx, values)
	case strPiper:
		return testTTSPiper(values)
	case strKokoro:
		return testTTSKokoro(values)
	case strMossNano:
		return testTTSNano(values)
	default:
		return ConnectivityTestResult{Success: false, Message: "Unknown TTS engine: " + engine}
	}
}

func testTTSEdge(ctx context.Context, _ map[string]any) ConnectivityTestResult {
	// Test TLS connectivity to Edge TTS service
	dialer := &tls.Dialer{
		Config:    &tls.Config{MinVersion: tls.VersionTLS12},
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
	}
	conn, err := dialer.DialContext(ctx, "tcp", "speech.platform.bing.com:443")
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Edge TTS service unreachable: %v", err)}
	}
	_ = conn.Close()
	return ConnectivityTestResult{Success: true, Message: "Edge TTS service reachable"}
}

func testTTSPiper(values map[string]any) ConnectivityTestResult {
	// Check piper binary
	piperPath := resolvePiperBinary()
	if piperPath == "" {
		return ConnectivityTestResult{Success: false, Message: "Piper binary not found (checked .venv/bin/piper and $PATH)"}
	}

	// Check model file
	voice := resolveStringValue(values, "tts.voice", model.ConfigInstance.TTS.Voice)
	modelPath := resolveStringValue(values, "tts.piper.model_path", model.ConfigInstance.TTS.Piper.ModelPath)
	resolvedModel := speech.ResolveModelPath(voice, modelPath)
	if resolvedModel == "" {
		return ConnectivityTestResult{Success: false, Message: "Piper model path not configured and voice not set"}
	}
	if _, err := os.Stat(resolvedModel); err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Piper model file not found: %s", resolvedModel)}
	}

	return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("Piper ready (binary: %s, model: %s)", piperPath, resolvedModel)}
}

func testTTSKokoro(values map[string]any) ConnectivityTestResult {
	modelPath := resolveStringValue(values, "tts.kokoro.model_path", model.ConfigInstance.TTS.Kokoro.ModelPath)
	voicesPath := resolveStringValue(values, "tts.kokoro.voices_path", model.ConfigInstance.TTS.Kokoro.VoicesPath)

	resolvedModel, resolvedVoices := speech.ResolveKokoroPaths(modelPath, voicesPath)

	var errors []string

	// Check Python interpreter
	pythonPath := resolveKokoroPython()
	if pythonPath == "" {
		errors = append(errors, "Python interpreter not found (checked .venv/bin/python3)")
	}

	// Check model file
	if resolvedModel == "" {
		errors = append(errors, "Kokoro model path not configured")
	} else if _, err := os.Stat(resolvedModel); err != nil {
		errors = append(errors, fmt.Sprintf("Kokoro model file not found: %s", resolvedModel))
	}

	// Check voices file
	if resolvedVoices == "" {
		errors = append(errors, "Kokoro voices path not configured")
	} else if _, err := os.Stat(resolvedVoices); err != nil {
		errors = append(errors, fmt.Sprintf("Kokoro voices file not found: %s", resolvedVoices))
	}

	if len(errors) > 0 {
		return ConnectivityTestResult{Success: false, Message: strings.Join(errors, "; ")}
	}

	return ConnectivityTestResult{Success: true, Message: "Kokoro ready (Python found, model and voices files exist)"}
}

func testTTSNano(values map[string]any) ConnectivityTestResult {
	modelDir := resolveStringValue(values, "tts.moss_nano.model_dir", model.ConfigInstance.TTS.MossNano.ModelDir)
	resolvedDir := speech.ResolveMossNanoModelDir(modelDir)

	// Check binary
	binPath, _ := exec.LookPath("moss-tts-nano")
	if binPath == "" {
		// Try relative to executable
		if exePath, err := os.Executable(); err == nil {
			candidate := filepath.Join(filepath.Dir(exePath), ".venv/bin/moss-tts-nano")
			if _, err := os.Stat(candidate); err == nil {
				binPath = candidate
			}
		}
	}
	if binPath == "" {
		return ConnectivityTestResult{Success: false, Message: "MOSS-Nano binary not found (checked .venv/bin/moss-tts-nano and $PATH)"}
	}

	// Check model dir
	if resolvedDir != "" {
		if info, err := os.Stat(resolvedDir); err != nil || !info.IsDir() {
			return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("MOSS-Nano model directory not found: %s", resolvedDir)}
		}
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("MOSS-Nano ready (binary: %s, model dir: %s)", binPath, resolvedDir)}
	}

	// No model dir configured but binary found — MOSS-Nano can auto-download
	return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("MOSS-Nano binary found (%s), model will auto-download on first use", binPath)}
}

// resolvePiperBinary finds the piper binary path.
func resolvePiperBinary() string {
	// Check relative to executable first
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), piperCmd)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Check $PATH
	if p, err := exec.LookPath(strPiper); err == nil {
		return p
	}
	return ""
}

const piperCmd = ".venv/bin/piper"

// resolveKokoroPython finds the Python interpreter for Kokoro.
func resolveKokoroPython() string {
	// Check relative to executable first
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), ".venv/bin/python3")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Check $PATH
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return ""
}
