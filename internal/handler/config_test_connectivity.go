package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"
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
	strMessages = "messages"
	strPiper    = "piper"
	strKokoro   = "kokoro"
	strMossNano = "moss-nano"
	strReqError = "error"
)

type connectivityTestRequest struct {
	Category string         `json:"category"` // "frp" | "summarize_voice" | "rag" | "dingtalk" | "port_forward" | "tts"
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
	case "stt":
		result = testSTT(ctx, req.Values)
	default:
		result = ConnectivityTestResult{Success: false, Message: "Unknown category: " + req.Category}
	}

	writeJSON(w, http.StatusOK, result)
}

// ── Helpers ──────────────────────────────────────────────────

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

// ── Summarize Voice ──────────────────────────────────────────

// testSummarizeVoice verifies the shared AI summary API connectivity. It is
// decoupled from the voice-summary (TTS) backend: the shared ai_summary config
// feeds both voice summarization and conversation recommendation, so the test
// always probes the ai_summary.api endpoint regardless of summarize.tts_backend.
func testSummarizeVoice(ctx context.Context, values map[string]any) ConnectivityTestResult {
	baseURL := resolveStringValue(values, "ai_summary.api.base_url", model.ConfigInstance.AISummary.API.BaseURL)
	apiKey := resolveStringValue(values, "ai_summary.api.key", model.ConfigInstance.AISummary.API.Key)
	modelName := resolveStringValue(values, "ai_summary.model", model.ConfigInstance.AISummary.Model)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "AI summary API base URL is required"}
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
	reqURL := summarize.BuildEndpointURL(baseURL, summarize.OpenAIChatCompletionsPath)

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
	reqURL := summarize.BuildEndpointURL(baseURL, summarize.AnthropicMessagesPath)

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

	normalized, normErr := rag.NormalizeEmbeddingBaseURL(baseURL)
	if normErr != nil {
		return ConnectivityTestResult{Success: false, Message: normErr.Error()}
	}
	baseURL = normalized

	// Probe with a real embedding request instead of a /v1/models listing.
	// Gateways (e.g. OneAPI) often don't enumerate every model in the models
	// list — or don't implement /v1/models at all — even though the embeddings
	// endpoint accepts them, so a models precheck reports a working service as
	// broken. A direct probe against /v1/embeddings is authoritative for the
	// exact endpoint + model + auth the feature will actually use. (STT follows
	// the same rule for /v1/audio/transcriptions.)
	client := rag.NewEmbeddingClient(baseURL, ragModel, apiKey)
	vector, err := client.Embed(ctx, "connectivity test")
	if err != nil {
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("RAG embedding failed: %v", err),
		}
	}
	if len(vector) == 0 {
		return ConnectivityTestResult{Success: false, Message: "RAG embedding returned an empty vector"}
	}

	return ConnectivityTestResult{
		Success: true,
		Message: fmt.Sprintf("RAG embedding succeeded for model '%s' (dim %d)", ragModel, len(vector)),
	}
}

// ── STT ──────────────────────────────────────────────────────

// testSTT tests connectivity to the vLLM STT (Whisper) service.
func testSTT(ctx context.Context, values map[string]any) ConnectivityTestResult {
	baseURL := resolveStringValue(values, "stt.base_url", model.ConfigInstance.STT.BaseURL)
	sttModel := resolveStringValue(values, "stt.model", model.ConfigInstance.STT.Model)
	apiKey := resolveStringValue(values, "stt.api_key", model.ConfigInstance.STT.APIKey)
	language := resolveStringValue(values, "stt.language", model.ConfigInstance.STT.Language)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "STT base URL is required"}
	}
	if sttModel == "" {
		sttModel = "openai/whisper-large-v3"
	}

	// Probe with a real transcription request instead of relying on
	// /v1/models. Gateways (e.g. OneAPI) often don't enumerate every model in
	// the models list even though the transcription endpoint accepts them, so
	// a direct probe against /v1/audio/transcriptions is authoritative for the
	// exact endpoint + model + auth the feature will actually use.
	body, contentType, err := buildSTTProbe(sttModel, language)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to build probe: %v", err)}
	}

	url := strings.TrimRight(baseURL, "/") + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Content-Type", contentType)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service unreachable at %s: %v", baseURL, err)}
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("STT service reachable, transcription accepted for model '%s'", sttModel)}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT auth failed (HTTP %d)", resp.StatusCode)}
	case resp.StatusCode == http.StatusNotFound:
		return ConnectivityTestResult{Success: false, Message: "STT endpoint or model not found (HTTP 404)"}
	}

	// Some servers reject an unknown model name with a 4xx body. Anything that
	// isn't auth/404/model-not-found means the request reached the
	// transcription handler with the model routed, which confirms connectivity.
	if isSTTModelNotFound(respBody) {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service reachable, but model '%s' not recognized", sttModel)}
	}
	return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("STT service reachable at %s (probe returned HTTP %d)", baseURL, resp.StatusCode)}
}

// buildSTTProbe builds a multipart transcription probe body (embedded "你好"
// mp3 + model/language fields) and returns the body and its Content-Type
// header.
func buildSTTProbe(sttModel, language string) (*bytes.Buffer, string, error) {
	audio := sttProbeAudio
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "probe.mp3")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("model", sttModel); err != nil {
		return nil, "", err
	}
	if language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}

// isSTTModelNotFound reports whether an STT error body indicates an unknown model.
func isSTTModelNotFound(body []byte) bool {
	s := strings.ToLower(string(body))
	for _, k := range []string{"model not found", "unknown model", "does not exist", "model_not_found", "no such model"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// makeMinimalWAV was removed — the STT probe now uses the embedded "你好" mp3
// (sttProbeAudio) so connectivity tests exercise real speech recognition.

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

// testPortForward reports whether port forwarding is usable AT ALL, which is
// not the same question as "is the SSH listener up".
//
// Port forwarding has two wires, and an h2-capable install forwards ports even
// with no SSH listener at all — that is the whole point of the h2 transport
// (one port, no mainPort+1). Keying this test off the SSH server alone reported
// "SSH tunnel server is not running" on a perfectly healthy h2-only install
// (port_forward.enabled: false), the same mistake the port-forward panel's
// "port forwarding unavailable" banner made.
//
// The h2 branch does NOT dial the main port: this handler runs INSIDE that
// server, so a loopback dial would always succeed and prove nothing. The
// registry is the real precondition — both /api/tunnel/stream and
// /api/tunnel/control refuse every request with 503 when it is nil
// (tunnel_stream.go, tunnel_control.go), and it is created unconditionally at
// startup (cmd/server/proxy_registry_gate.go).
//
// This proves the h2 PRECONDITION, not the h2 data plane: it does not transfer
// bytes over a duplex h2 stream. That needs a real tunnel client.
func testPortForward(ctx context.Context, _ map[string]any) ConnectivityTestResult {
	if sshSrv := GetSSHServer(); sshSrv != nil {
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

	// No SSH listener. Fall back to the h2 wire, which needs only the registry.
	if service.ProxyService != nil {
		return ConnectivityTestResult{
			Success: true,
			Message: fmt.Sprintf("Port forwarding is available over HTTP/2 on port %d", model.ServerPort),
		}
	}

	return ConnectivityTestResult{
		Success: false,
		Message: "Port forwarding is not available: no SSH tunnel server and no port registry",
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
