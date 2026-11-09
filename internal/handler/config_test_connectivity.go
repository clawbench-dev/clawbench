package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawbench/internal/mcp"
	"clawbench/internal/model"
	"clawbench/internal/rag"
	"clawbench/internal/service"
	"clawbench/internal/speech"
	"clawbench/internal/stt"
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
	strOpenAI   = "openai"
	strReqError = "error"
)

// mcpProbeTimeout is the budget for one MCP connectivity test.
//
// Longer than the 15s used by the other categories because the probe performs
// a real handshake: a stdio server started through a launcher (npx / uvx /
// pipx) may have to download its package on the first run, which alone can take
// tens of seconds. The client's own timeout must stay above this (the settings
// page passes 60s for the mcp category).
const mcpProbeTimeout = 45 * time.Second

type connectivityTestRequest struct {
	Category string         `json:"category"` // "summarize_voice" | "rag" | "dingtalk" | "port_forward" | "tts"
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
	// MCP needs a longer budget than the other categories: a stdio server is
	// usually started by a launcher (npx / uvx / pipx) that may have to fetch
	// its package on first use, and the probe waits for a real handshake plus
	// tools/list. The generic 15s expired before a cold `npx -y` finished.
	timeout := 15 * time.Second
	if req.Category == "mcp" {
		timeout = mcpProbeTimeout
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	switch req.Category {
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
	case "mcp":
		result = testMCP(ctx, req.Values)
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

// ── Summarize Voice ──────────────────────────────────────────

// testSummarizeVoice verifies the shared AI summary API connectivity. It is
// decoupled from the voice-summary (TTS) backend: the shared ai_summary config
// feeds both voice summarization and conversation recommendation, so the test
// always probes the ai_summary.api endpoint regardless of summarize.tts_backend.
func testSummarizeVoice(ctx context.Context, values map[string]any) ConnectivityTestResult {
	modelName := resolveStringValue(values, "ai_summary.model", model.ConfigInstance.AISummary.Model)
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	// The provider is the supported configuration: its protocol also decides the
	// request shape, which a URL heuristic cannot recover for Anthropic-style
	// gateways that do not contain "anthropic" in the host.
	if p := model.GetProvider(summaryProviderID(values)); p != nil && p.BaseURL != "" {
		if p.Protocol != model.ProviderProtocolAnthropic && p.Protocol != model.ProviderProtocolOpenAI {
			return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Provider %q uses the %s protocol, which the summary model does not support", p.Name, p.Protocol)}
		}
		format := model.ProviderProtocolOpenAI
		if p.Protocol == model.ProviderProtocolAnthropic {
			format = model.ProviderProtocolAnthropic
		}
		return testAPISummarizer(ctx, p.BaseURL, p.APIKey, modelName, format)
	}

	// Legacy shape, kept until the startup migration rewrites config.yaml.
	baseURL := resolveStringValue(values, "ai_summary.api.base_url", model.ConfigInstance.AISummary.API.BaseURL)
	apiKey := resolveStringValue(values, "ai_summary.api.key", model.ConfigInstance.AISummary.API.Key)
	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "AI summary provider is required"}
	}
	return testAPISummarizer(ctx, baseURL, apiKey, modelName, "")
}

// summaryProviderID reads the draft value first, then the saved config.
func summaryProviderID(values map[string]any) int64 {
	if raw, ok := values["ai_summary.provider_id"]; ok {
		switch v := raw.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		}
	}
	if pid := model.ConfigInstance.AISummary.ProviderID; pid != nil {
		return *pid
	}
	return 0
}

// testAPISummarizer sends a minimal chat completion request to verify API connectivity.
// The API format (OpenAI vs Anthropic) is auto-detected from the base URL:
//   - URLs containing "anthropic.com" or ending with "/v1/messages" → Anthropic format
//   - All other URLs → OpenAI format
func testAPISummarizer(ctx context.Context, baseURL, apiKey, modelName, format string) ConnectivityTestResult {
	url := strings.TrimRight(baseURL, "/")

	client := &http.Client{Timeout: 10 * time.Second}

	if format == model.ProviderProtocolAnthropic || (format == "" && summarize.IsAnthropicURL(url)) {
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

// testSTT tests connectivity to the configured speech-to-text backend.
func testSTT(ctx context.Context, values map[string]any) ConnectivityTestResult {
	providerName := resolveStringValue(values, "stt.provider", model.ConfigInstance.STT.Provider)
	if providerName == "doubao" {
		return testDoubaoSTT(ctx, values)
	}
	if providerName == "alibaba" {
		return testAlibabaSTT(ctx, values)
	}

	baseURL := resolveStringValue(values, "stt.vllm.base_url", model.ConfigInstance.STT.VLLM.BaseURL)
	sttModel := resolveStringValue(values, "stt.vllm.model", model.ConfigInstance.STT.VLLM.Model)
	apiKey := resolveStringValue(values, "stt.vllm.api_key", model.ConfigInstance.STT.VLLM.APIKey)
	language := resolveStringValue(values, "stt.vllm.language", model.ConfigInstance.STT.VLLM.Language)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "STT base URL is required"}
	}
	if sttModel == "" {
		sttModel = "openai/whisper-large-v3"
	}

	// Probe by transcribing the embedded "你好" clip through the same provider
	// the feature uses, instead of only checking reachability: a 200 that
	// carries no text means the endpoint accepted the request but recognized
	// nothing, which is a broken configuration, not a working one. Going through
	// the provider also keeps the probe on the exact endpoint/model/auth (and
	// multipart shape) that real transcription uses.
	provider := stt.NewVLLMProvider(baseURL, sttModel, apiKey, language)
	provider.Filename = "probe.mp3"
	provider.HTTPClient = &http.Client{Timeout: 15 * time.Second}

	text, err := provider.Transcribe(ctx, bytes.NewReader(sttProbeAudio), "")
	if err != nil {
		return sttProbeFailure(baseURL, sttModel, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("STT returned an empty transcription for model '%s'", sttModel),
		}
	}
	return ConnectivityTestResult{
		Success: true,
		Message: fmt.Sprintf("STT transcription succeeded: %q (model: %s)", text, sttModel),
	}
}

// sttProbeFailure names the cause of a failed STT probe: auth, endpoint, model
// or transport. The provider reports non-2xx answers as *stt.APIError, so the
// classification does not depend on the error text.
func sttProbeFailure(baseURL, sttModel string, err error) ConnectivityTestResult {
	var apiErr *stt.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden:
			return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT auth failed (HTTP %d)", apiErr.Status)}
		case apiErr.Status == http.StatusNotFound:
			return ConnectivityTestResult{Success: false, Message: "STT endpoint or model not found (HTTP 404)"}
		case isSTTModelNotFound([]byte(apiErr.Body)):
			return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service reachable, but model '%s' not recognized", sttModel)}
		}
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("STT probe failed (HTTP %d): %s", apiErr.Status, strings.TrimSpace(apiErr.Body)),
		}
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service unreachable at %s: %v", baseURL, err)}
	}
	return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT probe failed: %v", err)}
}

// doubaoSTTBaseURL is the doubao speech API root used by the connectivity
// test. Overridable in tests; production always hits the real host.
//
//nolint:gochecknoglobals // test seam, mirrors dingtalkTokenURL
var doubaoSTTBaseURL = stt.DoubaoBaseURL

// testDoubaoSTT probes the Doubao (Volcano Engine) Seed-ASR endpoint with the
// embedded "你好" clip. Auth is X-Api-Key plus the purchased resource ID, and
// the response body carries the real status code — a reachable host with a
// revoked key still answers HTTP 200, so the body is authoritative.
func testDoubaoSTT(ctx context.Context, values map[string]any) ConnectivityTestResult {
	apiKey := resolveStringValue(values, "stt.doubao.api_key", model.ConfigInstance.STT.Doubao.APIKey)
	resourceID := resolveStringValue(values, "stt.doubao.resource_id", model.ConfigInstance.STT.Doubao.ResourceID)

	if apiKey == "" {
		return ConnectivityTestResult{Success: false, Message: "Doubao API key is required"}
	}

	provider := stt.NewDoubaoProvider(apiKey, resourceID)
	provider.BaseURL = doubaoSTTBaseURL
	provider.HTTPClient = &http.Client{Timeout: 30 * time.Second}

	text, err := provider.Transcribe(ctx, bytes.NewReader(sttProbeAudio), "")
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Doubao transcription failed: %v", err)}
	}
	if strings.TrimSpace(text) == "" {
		return ConnectivityTestResult{Success: false, Message: "Doubao transcription returned empty text"}
	}
	return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("Doubao transcription succeeded: %q", text)}
}

// testAlibabaSTT probes Alibaba Cloud Model Studio with the embedded "你好"
// clip. It goes through the same Qwen3-ASR-Flash path real uploads use, so a
// key from the wrong region, a missing model or a revoked key all surface as
// the actual HTTP answer instead of "the host is reachable".
func testAlibabaSTT(ctx context.Context, values map[string]any) ConnectivityTestResult {
	apiKey := resolveStringValue(values, "stt.alibaba.api_key", model.ConfigInstance.STT.Alibaba.APIKey)
	if apiKey == "" {
		return ConnectivityTestResult{Success: false, Message: "Alibaba Cloud API key is required"}
	}
	baseURL := resolveStringValue(values, "stt.alibaba.base_url", model.ConfigInstance.STT.Alibaba.BaseURL)
	language := resolveStringValue(values, "stt.alibaba.language", model.ConfigInstance.STT.Alibaba.Language)

	provider := stt.NewAlibabaProvider(apiKey)
	if baseURL != "" {
		provider.BaseURL = baseURL
	}
	provider.Language = language
	provider.HTTPClient = &http.Client{Timeout: 30 * time.Second}

	text, err := provider.Transcribe(ctx, bytes.NewReader(sttProbeAudio), "")
	if err != nil {
		return sttProbeFailure(provider.BaseURL, "qwen3-asr-flash", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ConnectivityTestResult{Success: false, Message: "Alibaba Cloud transcription returned empty text"}
	}
	return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("Alibaba Cloud transcription succeeded: %q", text)}
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
	case strOpenAI:
		return testTTSOpenAI(ctx, values)
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

// testTTSOpenAI probes an OpenAI-compatible /v1/audio/speech endpoint.
//
// It sends a real synthesis request with a one-word input rather than a HEAD
// or a /v1/models lookup: gateways frequently do not enumerate every model in
// the models list, and a bare connectivity check would not catch a wrong model
// name or a rejected voice — which is exactly what the user needs told.
//
// The probe asks for the raw audio body (`stream_format: "audio"`), not SSE:
// that is the shape every model supports, so a failure here means the
// endpoint / key / model is wrong, not that this particular model lacks SSE.
func testTTSOpenAI(ctx context.Context, values map[string]any) ConnectivityTestResult {
	baseURL := resolveStringValue(values, "tts.openai.base_url", model.ConfigInstance.TTS.OpenAI.BaseURL)
	apiKey := resolveStringValue(values, "tts.openai.api_key", model.ConfigInstance.TTS.OpenAI.APIKey)
	ttsModel := resolveStringValue(values, "tts.openai.model", model.ConfigInstance.TTS.OpenAI.Model)
	voice := resolveStringValue(values, "tts.openai.voice", model.ConfigInstance.TTS.OpenAI.Voice)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "OpenAI TTS base URL is required"}
	}
	if ttsModel == "" {
		return ConnectivityTestResult{Success: false, Message: "OpenAI TTS model is required"}
	}
	if voice == "" {
		return ConnectivityTestResult{Success: false, Message: "OpenAI TTS voice is required"}
	}

	payload, err := json.Marshal(map[string]any{
		"model":           ttsModel,
		"input":           "ok",
		"voice":           voice,
		"response_format": "mp3",
		"stream_format":   "audio",
	})
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to encode probe: %v", err)}
	}

	url := speech.ResolveOpenAISpeechURL(baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("Failed to create request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("OpenAI TTS service unreachable at %s: %v", baseURL, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("OpenAI TTS returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody))),
		}
	}

	// Drain a little of the body: a 200 with an empty body would otherwise look
	// like success while playback fails with no audio.
	audio, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(audio) == 0 {
		return ConnectivityTestResult{Success: false, Message: "OpenAI TTS returned an empty audio body"}
	}

	return ConnectivityTestResult{
		Success: true,
		Message: fmt.Sprintf("OpenAI TTS ready (model: %s, voice: %s)", ttsModel, voice),
	}
}

// testMCP probes one MCP server: it performs the real MCP handshake and lists
// tools, which is the only way to tell a working server from one that merely
// starts.
//
// The server to probe is named by "mcp.name"; the definition is taken from the
// form's "mcp.servers" (so an unsaved edit can be tested before saving) and
// falls back to the saved config when the form does not carry it. Probing the
// SAVED definition when the form changed would test the wrong thing — the user
// clicks test precisely to decide whether to save.
func testMCP(ctx context.Context, values map[string]any) ConnectivityTestResult {
	name := resolveStringValue(values, "mcp.name", "")
	if name == "" {
		return ConnectivityTestResult{Success: false, Message: "mcp.name is required"}
	}

	servers, err := mcpServersFromValues(values)
	if err != nil {
		return ConnectivityTestResult{Success: false, Message: err.Error()}
	}

	var target *model.MCPServer
	for i := range servers {
		if servers[i].Name == name {
			target = &servers[i]
			break
		}
	}
	if target == nil {
		// Name every available server: the user picked one that is not in the
		// list, and "not found" alone does not say what to pick instead.
		available := make([]string, 0, len(servers))
		for _, s := range servers {
			available = append(available, s.Name)
		}
		if len(available) == 0 {
			return ConnectivityTestResult{Success: false, Message: "no MCP servers are configured"}
		}
		return ConnectivityTestResult{
			Success: false,
			Message: fmt.Sprintf("MCP server %q not found (available: %s)", name, strings.Join(available, ", ")),
		}
	}

	res := mcp.Probe(ctx, *target)
	msg := res.Message
	if res.OK && res.ServerName != "" {
		msg = fmt.Sprintf("%s (server: %s %s)", msg, res.ServerName, res.ServerVersion)
	}
	if res.OK && len(res.Tools) > 0 {
		msg = fmt.Sprintf("%s: %s", msg, strings.Join(res.Tools, ", "))
		if res.ToolCount > len(res.Tools) {
			msg = fmt.Sprintf("%s … (%d total)", msg, res.ToolCount)
		}
	}
	return ConnectivityTestResult{Success: res.OK, Message: msg}
}

// mcpServersFromValues extracts the server list from the form values, falling
// back to the saved config. The form sends the whole list under "mcp.servers".
func mcpServersFromValues(values map[string]any) ([]model.MCPServer, error) {
	if raw, ok := values["mcp.servers"]; ok {
		if list, ok := raw.([]any); ok {
			return parseMCPServers(list)
		}
	}
	// No list in the form: fall back to what is saved.
	configMutex.RLock()
	saved := model.ConfigInstance.MCP.Servers
	configMutex.RUnlock()
	out := make([]model.MCPServer, len(saved))
	copy(out, saved)
	return out, nil
}
