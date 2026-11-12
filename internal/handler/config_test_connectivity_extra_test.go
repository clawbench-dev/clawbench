package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/ssh"
)

// TestServeConfigTest_STTCategory covers the "stt" branch of the
// ServeConfigTest switch, which dispatches to testSTT.
func TestServeConfigTest_STTCategory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	body := fmt.Sprintf(`{"category":"stt","values":{"stt.provider":"vllm","stt.vllm.base_url":%q,"stt.vllm.model":"m"}}`, srv.URL)
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)

	assert.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success, result.Message)
	assert.Contains(t, result.Message, "你好")
}

// TestSTTConnectivity_EmptyBaseURL covers the missing base URL guard in testSTT.
func TestSTTConnectivity_EmptyBaseURL(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": "",
		"stt.vllm.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "required")
}

// TestSTTConnectivity_DefaultModel covers the default model fallback
// ("openai/whisper-large-v3") when no model is provided.
func TestSTTConnectivity_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "openai/whisper-large-v3", r.FormValue("model"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": srv.URL,
	})
	assert.True(t, res.Success, res.Message)
}

// TestSTTConnectivity_WithAPIKey covers setting the Authorization header
// when an API key is configured.
func TestSTTConnectivity_WithAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": srv.URL,
		"stt.vllm.model":    "m",
		"stt.vllm.api_key":  "test-api-key",
	})
	assert.True(t, res.Success, res.Message)
}

// TestSTTConnectivity_Unreachable covers the client.Do network error path.
func TestSTTConnectivity_Unreachable(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": "http://127.0.0.1:19999",
		"stt.vllm.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "unreachable")
}

// TestSTTConnectivity_NewRequestError covers the invalid-URL branch: the probe
// must report it rather than panicking or reporting success.
func TestSTTConnectivity_NewRequestError(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": ":",
		"stt.vllm.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "create request")
}

// TestIsSTTModelNotFound covers both the true (keyword-matched) and false
// return paths of isSTTModelNotFound.
func TestIsSTTModelNotFound(t *testing.T) {
	trueCases := []string{
		`{"error":"model not found"}`,
		`{"error":"unknown model"}`,
		`{"error":"model does not exist"}`,
		`{"error":"model_not_found"}`,
		`{"error":"no such model"}`,
	}
	for _, tc := range trueCases {
		assert.True(t, isSTTModelNotFound([]byte(tc)), "expected true for %q", tc)
	}

	falseCases := []string{
		`{"error":"internal server error"}`,
		"hello world",
		`{"error":"invalid audio"}`,
		"",
	}
	for _, fc := range falseCases {
		assert.False(t, isSTTModelNotFound([]byte(fc)), "expected false for %q", fc)
	}
}

// ── Doubao (Volcano Engine Seed-ASR) branch ───────────────────

// withDoubaoTestServer points the doubao connectivity probe at srv and restores
// the production host afterwards.
func withDoubaoTestServer(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	prev := doubaoSTTBaseURL
	doubaoSTTBaseURL = srv.URL
	t.Cleanup(func() {
		doubaoSTTBaseURL = prev
		srv.Close()
	})
}

// TestSTTConnectivity_DoubaoSuccess asserts the probe really sends the embedded
// speech clip with the X-Api-Key / resource headers, and reports the recognized
// text. A silence-only probe would pass auth but prove nothing about ASR.
func TestSTTConnectivity_DoubaoSuccess(t *testing.T) {
	var gotKey, gotResource string
	var gotAudio []byte
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		gotResource = r.Header.Get("X-Api-Resource-Id")
		var body struct {
			Audio struct {
				Format string `json:"format"`
				Data   string `json:"data"`
			} `json:"audio"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "mp3", body.Audio.Format)
		decoded, err := base64.StdEncoding.DecodeString(body.Audio.Data)
		require.NoError(t, err)
		gotAudio = decoded
		_, _ = w.Write([]byte(`{"header":{"code":20000000,"message":"OK"},"result":{"text":"你好"}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":           "doubao",
		"stt.doubao.api_key":     "key-123",
		"stt.doubao.resource_id": "volc.seedasr.sauc.duration",
	})

	assert.True(t, res.Success, res.Message)
	assert.Contains(t, res.Message, "你好")
	assert.Equal(t, "key-123", gotKey)
	assert.Equal(t, "volc.seedasr.sauc.duration", gotResource)
	assert.Equal(t, sttProbeAudio, gotAudio, "probe must upload the real speech clip")
}

// TestSTTConnectivity_DoubaoRequiresAPIKey covers the missing-key guard.
func TestSTTConnectivity_DoubaoRequiresAPIKey(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":       "doubao",
		"stt.doubao.api_key": "",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "API key")
}

// TestSTTConnectivity_DoubaoBodyErrorIsFailure covers the failure path: the API
// answers HTTP 200 with a non-success body code, which must not read as success.
func TestSTTConnectivity_DoubaoBodyErrorIsFailure(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":45000030,"message":"requested resource not granted"}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":           "doubao",
		"stt.doubao.api_key":     "key-123",
		"stt.doubao.resource_id": "volc.bigasr.auc",
	})

	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "45000030")
}

// TestSTTConnectivity_DoubaoEmptyTranscriptIsFailure covers a 200/OK response
// that carries no text — that is a broken probe, not a working engine.
func TestSTTConnectivity_DoubaoEmptyTranscriptIsFailure(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":20000000},"result":{"text":"  "}}`))
	})

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":       "doubao",
		"stt.doubao.api_key": "key-123",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "empty")
}

// TestServeConfigTest_STTDoubaoDispatches covers provider dispatch through the
// HTTP entry point rather than calling testSTT directly.
func TestServeConfigTest_STTDoubaoDispatches(t *testing.T) {
	withDoubaoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"header":{"code":20000000},"result":{"text":"你好"}}`))
	})

	body := `{"category":"stt","values":{"stt.provider":"doubao","stt.doubao.api_key":"k"}}`
	r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(body))
	w := httptest.NewRecorder()
	ServeConfigTest(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	var result ConnectivityTestResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	assert.True(t, result.Success, result.Message)
}

// ── Alibaba Cloud Model Studio branch ───────────────────────

// TestSTTConnectivity_AlibabaSuccess asserts the probe uses the OpenAI-compatible
// transcription path with the real speech clip inline as a data URI.
func TestSTTConnectivity_AlibabaSuccess(t *testing.T) {
	var gotPath, gotAuth string
	var gotModel, gotData string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content []struct {
					InputAudio struct {
						Data string `json:"data"`
					} `json:"input_audio"`
				} `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotModel = body.Model
		if len(body.Messages) > 0 && len(body.Messages[0].Content) > 0 {
			gotData = body.Messages[0].Content[0].InputAudio.Data
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"你好"}}]}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":         "alibaba",
		"stt.alibaba.api_key":  "sk-dash",
		"stt.alibaba.base_url": srv.URL,
		"stt.alibaba.language": "zh",
	})

	assert.True(t, res.Success, res.Message)
	assert.Contains(t, res.Message, "你好")
	assert.Equal(t, "/compatible-mode/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer sk-dash", gotAuth)
	assert.Equal(t, "qwen3-asr-flash", gotModel)

	const prefix = "data:audio/mpeg;base64,"
	require.True(t, strings.HasPrefix(gotData, prefix), gotData)
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(gotData, prefix))
	require.NoError(t, err)
	assert.Equal(t, sttProbeAudio, decoded, "probe must upload the real speech clip")
}

// TestSTTConnectivity_AlibabaRequiresAPIKey covers the missing-key guard.
func TestSTTConnectivity_AlibabaRequiresAPIKey(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":        "alibaba",
		"stt.alibaba.api_key": "",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "API key")
}

// TestSTTConnectivity_AlibabaAuthFailure classifies a rejected key by HTTP
// status rather than by the message text.
func TestSTTConnectivity_AlibabaAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"InvalidApiKey","message":"bad key"}}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":         "alibaba",
		"stt.alibaba.api_key":  "bad",
		"stt.alibaba.base_url": srv.URL,
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "auth failed")
}

// TestSTTConnectivity_Tencent probes through the real signed request path and
// checks the embedded speech clip is what got uploaded.
func TestSTTConnectivity_Tencent(t *testing.T) {
	var gotAction, gotData string
	var gotDataLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAction = r.Header.Get("X-TC-Action")
		var body struct {
			Data    string `json:"Data"`
			DataLen int    `json:"DataLen"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		gotData = body.Data
		gotDataLen = body.DataLen
		_, _ = w.Write([]byte(`{"Response":{"Result":"你好","RequestId":"req"}}`))
	}))
	defer srv.Close()

	prev := tencentSTTBaseURL
	tencentSTTBaseURL = srv.URL
	t.Cleanup(func() { tencentSTTBaseURL = prev })

	res := testSTT(context.Background(), map[string]any{
		"stt.provider":           "tencent",
		"stt.tencent.secret_id":  "AKIDtest",
		"stt.tencent.secret_key": "sk-test",
		"stt.tencent.app_id":     "1250000000",
	})

	assert.True(t, res.Success, res.Message)
	assert.Contains(t, res.Message, "你好")
	assert.Equal(t, "SentenceRecognition", gotAction)
	assert.Equal(t, len(sttProbeAudio), gotDataLen)
	decoded, err := base64.StdEncoding.DecodeString(gotData)
	require.NoError(t, err)
	assert.Equal(t, sttProbeAudio, decoded, "probe must upload the real speech clip")
}

// TestSTTConnectivity_TencentRequiresCredentials covers the missing-key guard.
func TestSTTConnectivity_TencentRequiresCredentials(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":           "tencent",
		"stt.tencent.secret_id":  "",
		"stt.tencent.secret_key": "sk",
		"stt.tencent.app_id":     "1",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "SecretId")
}

// TestSTTConnectivity_TencentRequiresAppID names the field the realtime API
// needs but the file API does not, so a half-configured account is caught here
// instead of at the first recording.
func TestSTTConnectivity_TencentRequiresAppID(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":           "tencent",
		"stt.tencent.secret_id":  "AKIDtest",
		"stt.tencent.secret_key": "sk",
		"stt.tencent.app_id":     "",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "AppID")
}

// ── Summarize Voice: provider branch ────────────────────────

// The provider branch of testSummarizeVoice: its protocol decides the request
// shape, and a protocol the summarizer cannot speak must be refused by name
// (a URL heuristic cannot recover that).
func TestTestSummarizeVoice_UsesBoundProviderProtocol(t *testing.T) {
	prevCfg := model.ConfigInstance
	model.ConfigInstance = model.Config{}
	t.Cleanup(func() { model.ConfigInstance = prevCfg; model.ReplaceProviders(nil) })

	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()

	model.ReplaceProviders([]*model.Provider{{
		ID: 7, Name: "Anthropic GW", Protocol: model.ProviderProtocolAnthropic,
		BaseURL: srv.URL, APIKey: "sk-p", AuthMode: model.ProviderAuthAuto,
	}})

	res := testSummarizeVoice(context.Background(), map[string]any{
		"ai_summary.provider_id": float64(7),
		"ai_summary.model":       "claude-haiku",
	})
	assert.True(t, res.Success, res.Message)
	assert.Equal(t, "/v1/messages", gotPath, "the provider protocol must pick the request shape")
	assert.Equal(t, "sk-p", gotKey)
}

func TestTestSummarizeVoice_RejectsGeminiProvider(t *testing.T) {
	prevCfg := model.ConfigInstance
	model.ConfigInstance = model.Config{}
	t.Cleanup(func() { model.ConfigInstance = prevCfg; model.ReplaceProviders(nil) })

	model.ReplaceProviders([]*model.Provider{{
		ID: 8, Name: "Gem", Protocol: model.ProviderProtocolGemini,
		BaseURL: "https://gem.example", APIKey: "k", AuthMode: model.ProviderAuthAuto,
	}})

	res := testSummarizeVoice(context.Background(), map[string]any{
		"ai_summary.provider_id": float64(8),
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "gemini")
	assert.Contains(t, res.Message, "Gem", "the message must name the provider")
}

// summaryProviderID accepts every numeric shape JSON can produce plus the saved
// config, and 0 when nothing is configured.
func TestSummaryProviderID(t *testing.T) {
	prevCfg := model.ConfigInstance
	model.ConfigInstance = model.Config{}
	t.Cleanup(func() { model.ConfigInstance = prevCfg })

	assert.Equal(t, int64(0), summaryProviderID(map[string]any{}))
	assert.Equal(t, int64(3), summaryProviderID(map[string]any{"ai_summary.provider_id": float64(3)}))
	assert.Equal(t, int64(4), summaryProviderID(map[string]any{"ai_summary.provider_id": int64(4)}))
	assert.Equal(t, int64(5), summaryProviderID(map[string]any{"ai_summary.provider_id": 5}))

	// A non-numeric draft value falls through to the saved config.
	saved := int64(9)
	model.ConfigInstance.AISummary.ProviderID = &saved
	assert.Equal(t, int64(9), summaryProviderID(map[string]any{"ai_summary.provider_id": "not-a-number"}))
}

// ── OpenAI / Anthropic probe: error envelopes ───────────────

// The vendor's message is the whole point of the test button; a body without
// one must fall back to the status code instead of printing an empty reason.
func TestTestOpenAIAPI_ErrorWithMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	res := testOpenAIAPI(context.Background(), &http.Client{}, srv.URL, "k", "m")
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "401")
	assert.Contains(t, res.Message, "invalid api key")
}

// A 2xx is success even when the body is unparseable: the probe only asks
// whether the endpoint accepts the request.
func TestTestAnthropicAPI_AcceptsEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := testAnthropicAPI(context.Background(), &http.Client{}, srv.URL, "k", "m")
	assert.True(t, res.Success, res.Message)
}

// ── Port forwarding: SSH listener that is not actually listening ──

// A server object whose port nobody is bound to: the dial fails, so the answer
// must be "not listening on port N" rather than an SSH success.
func TestTestPortForward_ServerPortClosed(t *testing.T) {
	origSSH := sshServerRef
	origRegistry := service.ProxyService
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	sshServerRef = ssh.NewServer(model.PortForwardConfig{Enabled: true, Port: port}, 20000, "pw", nil)
	service.ProxyService = nil
	defer func() { sshServerRef = origSSH; service.ProxyService = origRegistry }()

	res := testPortForward(context.Background(), map[string]any{})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, fmt.Sprintf("port %d", port))
}

// A transport failure names the endpoint so the user can tell a typo in the
// base URL from a wrong key.
func TestSTTConnectivity_UnreachableNamesEndpoint(t *testing.T) {
	res := testSTT(context.Background(), map[string]any{
		"stt.provider":      "vllm",
		"stt.vllm.base_url": "http://127.0.0.1:1",
		"stt.vllm.model":    "m",
	})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "127.0.0.1:1")
}

// A body that mentions the model but is not a known-model phrase must not be
// mistaken for one; the status code is reported instead.
func TestSTTConnectivity_UnclassifiedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal failure"}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{"stt.provider": "vllm", "stt.vllm.base_url": srv.URL, "stt.vllm.model": "m"})
	assert.False(t, res.Success)
	assert.Contains(t, res.Message, "500")
	assert.Contains(t, res.Message, "internal failure")
}

// ── HTTP entry-point dispatch ──────────────────────────────

// The category switch must reach the summarize_voice and port_forward probes
// through the real handler, not only through a direct call.
func TestServeConfigTest_DispatchesSummarizeAndPortForward(t *testing.T) {
	prevCfg := model.ConfigInstance
	model.ConfigInstance = model.Config{}
	t.Cleanup(func() { model.ConfigInstance = prevCfg; model.ReplaceProviders(nil) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	cases := []struct {
		category string
		values   map[string]any
	}{
		{"summarize_voice", map[string]any{"ai_summary.api.base_url": srv.URL, "ai_summary.api.key": "k"}},
		{"port_forward", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"category": tc.category, "values": tc.values})
			require.NoError(t, err)
			r := httptest.NewRequest(http.MethodPost, "/api/config/test", strings.NewReader(string(body)))
			w := httptest.NewRecorder()
			ServeConfigTest(w, r)

			require.Equal(t, http.StatusOK, w.Code)
			var result ConnectivityTestResult
			require.NoError(t, json.NewDecoder(w.Body).Decode(&result))
			if tc.category == "summarize_voice" {
				assert.True(t, result.Success, result.Message)
			} else {
				// The outcome depends on the host's listeners; the point is
				// that the category is dispatched at all.
				assert.NotContains(t, result.Message, "Unknown category")
			}
		})
	}
}

// A value that is neither a number nor a numeric string falls back to the
// default instead of being coerced to 0.
func TestResolveIntValue_NonNumericTypesFallBack(t *testing.T) {
	assert.Equal(t, 7, resolveIntValue(map[string]any{"k": true}, "k", 7))
	assert.Equal(t, 7, resolveIntValue(map[string]any{"k": []any{1}}, "k", 7))
}

// An endpoint that cannot even be turned into a request is reported by both API
// probes, so the settings page shows the reason instead of a bare failure.
func TestAPISummarizer_UnbuildableRequests(t *testing.T) {
	client := &http.Client{}

	openai := testOpenAIAPI(context.Background(), client, ":", "k", "m")
	assert.False(t, openai.Success)
	assert.Contains(t, openai.Message, "Failed to create request")

	anthropic := testAnthropicAPI(context.Background(), client, ":", "k", "m")
	assert.False(t, anthropic.Success)
	assert.Contains(t, anthropic.Message, "Failed to create request")

	tts := testTTSOpenAI(context.Background(), map[string]any{
		"tts.openai.base_url": ":", "tts.openai.model": "m", "tts.openai.voice": "alloy",
	})
	assert.False(t, tts.Success)
	assert.Contains(t, tts.Message, "Failed to create request")
}

// Same for the IM probes: a malformed token URL must be reported as a request
// error, not as a connection failure.
func TestIMProbes_UnbuildableRequests(t *testing.T) {
	origDing := dingtalkTokenURL
	dingtalkTokenURL = ":"
	t.Cleanup(func() { dingtalkTokenURL = origDing })

	ding := testDingTalk(context.Background(), map[string]any{
		"dingtalk.app_key": "k", "dingtalk.app_secret": "s",
	})
	assert.False(t, ding.Success)
	assert.Contains(t, ding.Message, "Failed to create request")

	origFeishu := feishuTokenURL
	feishuTokenURL = ":"
	t.Cleanup(func() { feishuTokenURL = origFeishu })

	feishu := testFeishu(context.Background(), map[string]any{
		"feishu.app_id": "id", "feishu.app_secret": "s",
	})
	assert.False(t, feishu.Success)
	assert.Contains(t, feishu.Message, "Failed to create request")
}
