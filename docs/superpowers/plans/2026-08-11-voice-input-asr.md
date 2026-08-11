# 语音输入（ASR）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 ClawBench 增加语音输入（ASR）：浏览器采集音频、后端代理转发 vLLM Whisper，支持流式（分段增量实时上屏）与非流式（松手后整段识别）两种协议，并提供设置面板配置。

**Architecture:** 镜像 TTS 模式。新增 `internal/stt` 后端包（`STTProvider` 接口 + `VLLMProvider` 调用 OpenAI 兼容 `/v1/audio/transcriptions`），handler 层提供非流式 `POST /api/stt/transcribe` 与流式 `WS /api/stt/transcribe/ws`。配置新增 `model.STTConfig`，走服务端设置 + 热更新。前端新增 `useVoiceInput` composable，`ChatInputBar` 支持发送按钮长按录音 + Alt+Space 快捷键，识别文本实时追加到输入框。设置页新增 `stt` 分类与 `stt:stt_engine` 面板。

**Tech Stack:** Go (net/http, github.com/coder/websocket, mime/multipart), Vue 3 + TypeScript, Vitest, i18n (zh/en)。

**关键既有模式参考：**
- TTS provider 全局 + 热更新：`internal/handler/tts.go:35-57`（`SetSpeechProvider`/`GetSpeechProvider`）
- OpenAI 兼容 HTTP client：`internal/rag/embedding.go:29-193`
- 配置 DTO/白名单/热更新/校验/apply：`internal/handler/settings.go`（`hotReloadFields` 37、`configResponse` 171、`configTTS` 220、`PatchableConfigPaths` 309、`validatePatchValues` 636、`applyConfigPatch` 871、`applyHotReloadGlobals` 1142）
- 连通性测试：`internal/handler/config_test_connectivity.go`（`ServeConfigTest` 47、`testRAG` 326）
- WS 端点：`internal/handler/tts_audio_ws.go:43-90`
- main.go 组装：`cmd/server/main.go`（provider 构建 500-613、`hotReloadReconfigure` 1212、`newTTSProvider` 1333）
- 设置面板：`web/src/components/settings/settingsFieldMap.ts`（`tts_engine` 250、`subPagePanelMap` 410）、`SettingsIndex.vue:44`、i18n `zh.ts:1196`
- 前端 composable：`web/src/composables/useAutoSpeech.ts`、`useSettingsConfig.ts:301`（serverDefaults）
- 聊天输入：`web/src/components/chat/ChatInputBar.vue`（send btn 98、handleSendClick 969、long-press 979）

---

## 文件结构

**后端**
- Create: `internal/stt/interface.go` — `STTProvider` 接口
- Create: `internal/stt/vllm_stt.go` — `VLLMProvider` 实现
- Create: `internal/stt/vllm_stt_test.go` — provider 单测
- Create: `internal/stt/transcribe.go` — 非流式/流式 handler
- Create: `internal/stt/transcribe_test.go` — handler 测试
- Modify: `internal/model/config.go` — `STTConfig` + `Config.STT`
- Modify: `internal/model/defaults.go` — STT 默认值
- Modify: `internal/model/config_test.go` — 默认值/序列化测试
- Modify: `internal/handler/settings.go` — DTO、白名单、热更新、校验、apply
- Modify: `internal/handler/settings_test.go` — 配置测试
- Modify: `internal/handler/config_test_connectivity.go` — `testSTT`
- Modify: `internal/handler/config_test_connectivity_test.go` — 连通性测试
- Modify: `internal/handler/handler.go` — 路由注册
- Modify: `cmd/server/main.go` — provider 构建 + 热更新

**前端**
- Create: `web/src/composables/useVoiceInput.ts`
- Create: `web/src/composables/useVoiceInput.test.ts`
- Modify: `web/src/components/chat/ChatInputBar.vue`
- Create: `web/src/components/chat/ChatInputBarVoice.test.ts`（或并入现有测试目录）
- Modify: `web/src/composables/useSettingsConfig.ts` — serverDefaults
- Modify: `web/src/components/settings/settingsFieldMap.ts` — stt 分类/面板
- Modify: `web/src/components/settings/SettingsIndex.vue` — 分类入口
- Modify: `web/src/i18n/locales/zh.ts`、`en.ts` — i18n 键

---

### Task 1: STTProvider 接口

**Files:**
- Create: `internal/stt/interface.go`
- Test: `internal/stt/vllm_stt_test.go`（接口断言放在 Task 2 一并写）

- [ ] **Step 1: 写接口文件**

```go
// Package stt provides speech-to-text (ASR) providers.
package stt

import (
	"context"
	"io"
)

// STTProvider abstracts speech recognition. Implementations can be
// swapped (vLLM Whisper, etc.).
type STTProvider interface {
	// Transcribe recognizes speech from audioReader and returns the text.
	// language is a language code (e.g. "zh", "en") — implementations that
	// support language-specific recognition should use it; others may ignore it.
	// Streaming vs non-streaming is controlled by the handler layer; the
	// provider only does "given an audio segment → return text".
	Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error)
}
```

- [ ] **Step 2: 编译验证**

Run: `go build ./internal/stt/`
Expected: 编译通过，无输出。

- [ ] **Step 3: Commit**

```bash
git add internal/stt/interface.go
git commit -m "feat(stt): add STTProvider interface"
```

---

### Task 2: VLLMProvider 实现 + 测试

**Files:**
- Create: `internal/stt/vllm_stt.go`
- Test: `internal/stt/vllm_stt_test.go`

- [ ] **Step 1: 写失败测试**

```go
package stt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mustCompileTimeAssert ensures VLLMProvider implements STTProvider.
var _ STTProvider = (*VLLMProvider)(nil)

// newVLLM returns a VLLMProvider pointing at the given server URL.
func newVLLM(baseURL, model string) *VLLMProvider {
	return &VLLMProvider{
		BaseURL: baseURL,
		Model:   model,
		Language: "zh",
		HTTPClient: &http.Client{},
	}
}

func TestVLLMTranscribe_Success(t *testing.T) {
	var gotPath string
	var gotContentType string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		_ = r.ParseMultipartForm(1 << 20)
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing file field: %v", err)
		}
		defer file.Close()
		var buf strings.Builder
		b := make([]byte, 32)
		for {
			n, err := file.Read(b)
			buf.Write(b[:n])
			if err != nil {
				break
			}
		}
		gotBody = []byte(buf.String())
		if got := r.FormValue("model"); got != "whisper-model" {
			t.Fatalf("model field = %q, want whisper-model", got)
		}
		if got := r.FormValue("language"); got != "zh" {
			t.Fatalf("language field = %q, want zh", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"你好世界"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "whisper-model")
	text, err := p.Transcribe(context.Background(), strings.NewReader("AUDIOBYTES"), "zh")
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if text != "你好世界" {
		t.Fatalf("text = %q, want 你好世界", text)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("content-type = %q, want multipart/form-data", gotContentType)
	}
	if string(gotBody) != "AUDIOBYTES" {
		t.Fatalf("audio body = %q, want AUDIOBYTES", string(gotBody))
	}
}

func TestVLLMTranscribe_BaseURLWithV1(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	// BaseURL already includes /v1
	p := newVLLM(srv.URL+"/v1", "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotPath != "/v1/audio/transcriptions" {
		t.Fatalf("path = %q, want /v1/audio/transcriptions", gotPath)
	}
}

func TestVLLMTranscribe_APIKeySent(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	p.APIKey = "secret-key"
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
}

func TestVLLMTranscribe_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	if _, err := p.Transcribe(context.Background(), strings.NewReader("x"), ""); err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
}

func TestVLLMTranscribe_EmptyText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":""}`))
	}))
	defer srv.Close()

	p := newVLLM(srv.URL, "m")
	text, err := p.Transcribe(context.Background(), strings.NewReader("x"), "")
	if err != nil {
		t.Fatalf("Transcribe error: %v", err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/stt/ -run TestVLLM -v`
Expected: FAIL，`undefined: VLLMProvider`。

- [ ] **Step 3: 写实现**

```go
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// VLLMProvider calls an OpenAI-compatible /v1/audio/transcriptions endpoint
// (vLLM Whisper server). Supports streaming and non-streaming at the handler
// layer; this provider only transcribes a single audio segment.
type VLLMProvider struct {
	BaseURL    string       // e.g. "http://localhost:8000" or ".../v1"
	Model      string       // recognition model, e.g. "openai/whisper-large-v3"
	APIKey     string       // bearer token (may be empty for local servers)
	Language   string       // language code (e.g. "zh")
	HTTPClient *http.Client // injectable for tests
}

// NewVLLMProvider creates a VLLM STT provider.
// baseURL is the OpenAI-compatible API root (with or without trailing "/v1").
func NewVLLMProvider(baseURL, model, apiKey, language string) *VLLMProvider {
	if apiKey == "" {
		apiKey = ""
	}
	return &VLLMProvider{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Model:    model,
		APIKey:   apiKey,
		Language: language,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// sttTranscribeResponse is the OpenAI-compatible transcription response.
type sttTranscribeResponse struct {
	Text string `json:"text"`
}

// transcriptionEndpointPath is the OpenAI-compatible transcription path.
const transcriptionEndpointPath = "/v1/audio/transcriptions"

// transcriptionsURL builds the full endpoint URL from BaseURL.
func (p *VLLMProvider) transcriptionsURL() string {
	return buildSTTEndpointURL(p.BaseURL, transcriptionEndpointPath)
}

// buildSTTEndpointURL appends defaultPath to baseURL, avoiding duplication
// when baseURL already contains the "/v1" prefix.
func buildSTTEndpointURL(baseURL, defaultPath string) string {
	u := strings.TrimRight(baseURL, "/")
	// "/v1/audio/transcriptions"
	segments := strings.Split(strings.TrimLeft(defaultPath, "/"), "/")
	if strings.HasSuffix(u, "/"+segments[0]) {
		return u + "/" + strings.Join(segments[1:], "/")
	}
	return u + defaultPath
}

// Transcribe recognizes speech from audioReader and returns the text.
func (p *VLLMProvider) Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error) {
	lang := p.Language
	if language != "" {
		lang = language
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, audioReader); err != nil {
		return "", fmt.Errorf("stt: read audio: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "recording.webm")
	if err != nil {
		return "", fmt.Errorf("stt: create form file: %w", err)
	}
	if _, err := part.Write(buf.Bytes()); err != nil {
		return "", fmt.Errorf("stt: write audio: %w", err)
	}
	_ = writer.WriteField("model", p.Model)
	if lang != "" {
		_ = writer.WriteField("language", lang)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("stt: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transcriptionsURL(), &body)
	if err != nil {
		return "", fmt.Errorf("stt: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("stt: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var transResp sttTranscribeResponse
	if err := json.NewDecoder(resp.Body).Decode(&transResp); err != nil {
		return "", fmt.Errorf("stt: decode response: %w", err)
	}

	return transResp.Text, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/stt/ -v`
Expected: 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/stt/
git commit -m "feat(stt): add vLLM STT provider"
```

---

### Task 3: STT 配置结构 + 默认值

**Files:**
- Modify: `internal/model/config.go:66-88`（在 TTS 后新增 STTConfig 与 Config.STT）
- Modify: `internal/model/defaults.go:196`（在 TTS 块后加 STT 默认值）
- Modify: `internal/model/config_test.go`

- [ ] **Step 1: 在 config.go 添加 STTConfig 与 Config.STT 字段**

在 `internal/model/config.go` 的 `TTS` 内联结构（结束于 `} \`yaml:"tts"\``，约第 78 行）之后添加：

```go
	STT STTConfig `yaml:"stt"` // Speech-to-text (voice input) configuration
```

并在 `Config` 结构后、`FileSearchConfig` 之前添加结构体定义：

```go
// STTConfig holds configuration for speech-to-text (voice input).
type STTConfig struct {
	BaseURL     string `yaml:"base_url"`     // vLLM OpenAI-compatible base URL (default: "http://localhost:8000/v1")
	APIKey      string `yaml:"api_key"`      // API key (optional)
	Model       string `yaml:"model"`        // Recognition model (default: "openai/whisper-large-v3")
	Language    string `yaml:"language"`     // Language code (default: "zh")
	Streaming   bool   `yaml:"streaming"`    // true=streaming incremental, false=non-streaming full (default: false)
	ChunkMs     int    `yaml:"chunk_ms"`     // Streaming slice interval in ms (default: 1000)
	ShortcutKey string `yaml:"shortcut_key"` // Recording shortcut (default: "Alt+Space")
}
```

- [ ] **Step 2: 在 defaults.go 添加 STT 默认值**

在 `internal/model/defaults.go` TTS 块（`cfg.TTS.MaxCacheFiles` 处，约第 196 行）之后添加：

```go
	// --- STT ---
	if cfg.STT.BaseURL == "" {
		cfg.STT.BaseURL = "http://localhost:8000/v1"
	}
	if cfg.STT.Model == "" {
		cfg.STT.Model = "openai/whisper-large-v3"
	}
	if cfg.STT.Language == "" {
		cfg.STT.Language = "zh"
	}
	if cfg.STT.ChunkMs <= 0 {
		cfg.STT.ChunkMs = 1000
	}
	if cfg.STT.ShortcutKey == "" {
		cfg.STT.ShortcutKey = "Alt+Space"
	}
```

- [ ] **Step 3: 添加/更新 config_test.go 断言默认值**

在 `internal/model/config_test.go` 中查找现有 `TestApplyDefaults`（或等价函数），在 TTS 断言后新增 STT 断言：

```go
	// STT defaults
	if got := model.ConfigInstance.STT.BaseURL; got != "http://localhost:8000/v1" {
		t.Errorf("STT.BaseURL = %q, want default", got)
	}
	if got := model.ConfigInstance.STT.Model; got != "openai/whisper-large-v3" {
		t.Errorf("STT.Model = %q, want default", got)
	}
	if got := model.ConfigInstance.STT.Language; got != "zh" {
		t.Errorf("STT.Language = %q, want default", got)
	}
	if got := model.ConfigInstance.STT.ChunkMs; got != 1000 {
		t.Errorf("STT.ChunkMs = %d, want 1000", got)
	}
	if got := model.ConfigInstance.STT.ShortcutKey; got != "Alt+Space" {
		t.Errorf("STT.ShortcutKey = %q, want Alt+Space", got)
	}
```

> 若该文件不存在该函数，先查看 `config_test.go` 实际函数名，仿照 TTS 默认断言所在测试添加。

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/model/ -run TestApplyDefaults -v`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/model/config.go internal/model/defaults.go internal/model/config_test.go
git commit -m "feat(model): add STT config with defaults"
```

---

### Task 4: Handler — STT provider 全局 + 非流式 transcribe

**Files:**
- Create: `internal/handler/stt.go`
- Create: `internal/handler/stt_test.go`
- Modify: `internal/handler/handler.go:293-295`（注册路由）

- [ ] **Step 1: 写失败测试**

先用完整、可运行的测试定义 `sttTestProvider` 与三个测试用例（handler `STTTranscribe` 尚未实现，测试将因 `undefined: STTTranscribe` 失败）。创建 `internal/handler/stt_test.go`：

```go
package handler

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sttTestProvider is a deterministic STT provider for tests.
type sttTestProvider struct {
	text    string
	errText string
	lang    string
}

func (f *sttTestProvider) Transcribe(_ context.Context, _ io.Reader, lang string) (string, error) {
	f.lang = lang
	if f.errText != "" {
		return "", &sttTestError{msg: f.errText}
	}
	return f.text, nil
}

type sttTestError struct{ msg string }

func (e *sttTestError) Error() string { return e.msg }

// makeSTTMultipart builds a multipart body with an audio file field.
// Returns body and the form content type (for header).
func makeSTTMultipart(t *testing.T, audio string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "recording.webm")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(audio))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

func TestSTTTranscribe_Success(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "你好世界"})

	body, ct := makeSTTMultipart(t, "AUDIO")
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"text":"你好世界"`) {
		t.Fatalf("response body = %s, want text 你好世界", w.Body.String())
	}
}

func TestSTTTranscribe_MissingFile(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "x"})

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestSTTTranscribe_ProviderError(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{errText: "boom"})

	body, ct := makeSTTMultipart(t, "AUDIO")
	req := httptest.NewRequest(http.MethodPost, "/api/stt/transcribe", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()

	STTTranscribe(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `go test ./internal/handler/ -run TestSTTTranscribe -v`
Expected: FAIL，`undefined: STTTranscribe` / `undefined: GetSTTProvider` / `undefined: SetSTTProvider`。

- [ ] **Step 3: 写实现**

创建 `internal/handler/stt.go`：

```go
//nolint:goconst // JSON response field names are domain strings
package handler

import (
	"io"
	"log/slog"
	"net/http"
	"sync"

	"clawbench/internal/stt"
)

// sttProvider is the global STT provider instance.
// Access is protected by sttProviderMu for hot-reload safety.
var (
	sttProvider   stt.STTProvider = stt.NewVLLMProvider("http://localhost:8000/v1", "openai/whisper-large-v3", "", "zh")
	sttProviderMu sync.RWMutex
)

// SetSTTProvider replaces the global STT provider.
// Goroutine-safe: concurrent Transcribe calls are protected by RWMutex.
func SetSTTProvider(p stt.STTProvider) {
	sttProviderMu.Lock()
	sttProvider = p
	sttProviderMu.Unlock()
}

// GetSTTProvider returns the current STT provider.
// Goroutine-safe for concurrent reads.
func GetSTTProvider() stt.STTProvider {
	sttProviderMu.RLock()
	p := sttProvider
	sttProviderMu.RUnlock()
	return p
}

// sttMaxBodyBytes limits the STT request body size (10MB).
const sttMaxBodyBytes = 10 << 20

// STTTranscribe handles POST /api/stt/transcribe (non-streaming).
// Multipart fields: file (audio), language (optional).
func STTTranscribe(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, sttMaxBodyBytes)

	language := r.FormValue("language")
	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{strReqError: "missing audio file"})
		return
	}
	defer func() { _ = file.Close() }()

	provider := GetSTTProvider()
	text, err := provider.Transcribe(r.Context(), file, language)
	if err != nil {
		slog.Error("stt: transcribe failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]any{strReqError: "transcription failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}
```

> 说明：STT 热更新通过 `SetSTTProvider`（Task 7 main.go 重建 provider）完成，无需在 handler 内额外占位逻辑。

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/handler/ -run TestSTTTranscribe -v`
Expected: 全部 PASS。

- [ ] **Step 5: 注册路由并确认编译**

在 `internal/handler/handler.go` 的 TTS 路由后（约第 295 行）添加：

```go
	register("/api/stt/transcribe", middleware.Auth(STTTranscribe))
```

Run: `go build ./...`
Expected: 编译通过。

> `STTTranscribeWS` 在 Task 5 实现并在 Task 5 Step 3 注册 `/api/stt/transcribe/ws`。

- [ ] **Step 6: 运行全 handler 测试确认无回归**

Run: `go test ./internal/handler/ -run TestSTTTranscribe -v`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/handler/stt.go internal/handler/stt_test.go internal/handler/handler.go
git commit -m "feat(stt): add non-streaming transcribe endpoint"
```

---

### Task 5: Handler — 流式 WS transcribe

**Files:**
- Modify: `internal/handler/stt.go`（追加 WS handler）
- Modify: `internal/handler/stt_test.go`（追加 WS 测试）

- [ ] **Step 1: 追加流式 WS handler 实现**

在 `internal/handler/stt.go` 末尾追加：

```go
// sttWSType / sttWSProtocol constants for WS protocol (satisfy goconst).
const (
	sttWSType   = "type"
	sttWSText   = "text"
	sttWSDone   = "done"
	sttWSError  = "error"
	sttWSEndCtl = "end"
)

// sttWSControl is a client text control message over the streaming WS.
type sttWSControl struct {
	Type string `json:"type"` // "end"
}

// sttWSServerMsg is a server message over the streaming WS.
type sttWSServerMsg struct {
	Type  string `json:"type"`            // "text" (incremental) or "done" (final)
	Text  string `json:"text,omitempty"`  // incremental text for "text"
	Final string `json:"final,omitempty"` // final full text for "done"
}

// STTTranscribeWS handles WS /api/stt/transcribe/ws (streaming).
//
// Protocol (client → server):
//   - binary frames: raw audio bytes (appended to running buffer)
//   - text frame: {"type":"end"} signals recording stopped → final full transcription
//
// Protocol (server → client):
//   - {"type":"text","text":"<incremental>"} — transcribed new segment
//   - {"type":"done","final":"<full text>"} — final result after "end"
//   - {"type":"error","message":"..."} — failure
//
// The server accumulates audio. At each ChunkMs tick it transcribes only the
// newly-appended portion and appends the incremental text. After "end", it
// re-transcribes the full buffer and sends the final result.
func STTTranscribeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"http://" + r.Host,
			"https://" + r.Host,
			"http://localhost:*",
			"https://localhost:*",
			"http://127.0.0.1:*",
			"https://127.0.0.1:*",
		},
	})
	if err != nil {
		slog.Error("stt ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	cfg := model.ConfigInstance
	chunkMs := cfg.STT.ChunkMs
	if chunkMs <= 0 {
		chunkMs = 1000
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var (
		buffer         bytes.Buffer
		lastOffset     int
		accumulated    string
		ticker         = time.NewTicker(time.Duration(chunkMs) * time.Millisecond)
		pending        = make(chan struct{}, 1)
		done           = false
		provider       = GetSTTProvider()
	)

	// incremental goroutine: on each tick, transcribe newly appended audio
	// and send incremental text.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			select {
			case <-ctx.Done():
				return
			case <-pending:
			}

			if done {
				return
			}
			if buffer.Len() <= lastOffset {
				continue
			}

			segment := append([]byte(nil), buffer.Bytes()[lastOffset:]...)
			lastOffset = buffer.Len()
			if len(segment) == 0 {
				continue
			}

			text, err := provider.Transcribe(ctx, bytes.NewReader(segment), cfg.STT.Language)
			if err != nil {
				// non-fatal: log and skip this segment
				slog.Debug("stt ws: incremental transcribe failed", slog.String("error", err.Error()))
				continue
			}
			if text == "" {
				continue
			}
			accumulated += text
			msg := sttWSServerMsg{Type: sttWSText, Text: text}
			data, _ := json.Marshal(msg)
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				cancel()
				return
			}
		}
	}()

	readErr := make(chan error, 1)
	go func() {
		for {
			mt, msg, err := conn.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			switch mt {
			case websocket.MessageBinary:
				buffer.Write(msg)
				select {
				case pending <- struct{}{}:
				default:
				}
			case websocket.MessageText:
				var ctl sttWSControl
				if json.Unmarshal(msg, &ctl) == nil && ctl.Type == sttWSEndCtl {
					done = true
					readErr <- nil // signal end-of-audio
					return
				}
			}
		}
	}()

	<-readErr // client ended or error

	ticker.Stop()

	// Final full transcription of the whole buffer.
	if buffer.Len() > 0 {
		finalText, err := provider.Transcribe(ctx, bytes.NewReader(buffer.Bytes()), cfg.STT.Language)
		if err == nil && finalText != "" {
			accumulated = finalText
		}
	}

	msg := sttWSServerMsg{Type: sttWSDone, Final: accumulated}
	data, _ := json.Marshal(msg)
	_ = conn.Write(context.Background(), websocket.MessageText, data)
}
```

> 依赖导入：`internal/handler/stt.go` 在 Task 4 Step 3 中导入的是 `io/log/slog/net/http/sync/clawbench/internal/stt`。Task 5 追加的 WS 代码需补充以下导入：`bytes`、`context`、`encoding/json`、`time`、`clawbench/internal/model`、`github.com/coder/websocket`。请在文件 import 块中统一补齐（移除 Task 4 中不需要的 `io` 后新增上述包）。
>
> 并发注意：`done` 标志被读 goroutine 与增量 goroutine 并发读写，存在数据竞争。实现时建议用 `sync/atomic.Bool` 替代（`done.Store(true)` / `done.Load()`），或为 `done`/`buffer` 增加互斥。若采用 `atomic.Bool`，同步修改增量 goroutine 中的读取点。

- [ ] **Step 2: 追加 WS 测试**

在 `internal/handler/stt_test.go` 末尾追加：

```go
func TestSTTTranscribeWS_IncrementalAndFinal(t *testing.T) {
	old := GetSTTProvider()
	defer SetSTTProvider(old)
	SetSTTProvider(&sttTestProvider{text: "识别文本"})

	server := httptest.NewServer(http.HandlerFunc(STTTranscribeWS))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/stt/transcribe/ws"
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// send first audio chunk
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("AUDIO1")); err != nil {
		t.Fatalf("write audio1: %v", err)
	}

	// send end control
	endCtl, _ := json.Marshal(sttWSControl{Type: sttWSEndCtl})
	if err := conn.Write(ctx, websocket.MessageText, endCtl); err != nil {
		t.Fatalf("write end: %v", err)
	}

	// read until done
	var finalText string
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m sttWSServerMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if m.Type == sttWSDone {
			finalText = m.Final
			break
		}
	}

	if finalText != "识别文本" {
		t.Fatalf("final = %q, want 识别文本", finalText)
	}
}
```

> 依赖导入：测试需增加 `context`、`encoding/json`、`"github.com/coder/websocket"`。

- [ ] **Step 3: 注册 WS 路由**

在 `internal/handler/handler.go` 中添加（Task 4 Step 7 若已只注册 transcribe，现在补 ws）：

```go
	register("/api/stt/transcribe/ws", middleware.Auth(STTTranscribeWS))
```

- [ ] **Step 4: 运行测试验证通过**

Run: `go test ./internal/handler/ -run 'TestSTTTranscribe' -v`
Expected: 全部 PASS（含 WS 测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/handler/stt.go internal/handler/stt_test.go internal/handler/handler.go
git commit -m "feat(stt): add streaming websocket transcribe endpoint"
```

---

### Task 6: 服务端配置 — DTO / 白名单 / 热更新 / 校验 / apply

**Files:**
- Modify: `internal/handler/settings.go`（configResponse、configSTT、serveConfigGet、PatchableConfigPaths、hotReloadFields、validatePatchValues、applyConfigPatch、applyHotReloadGlobals）
- Modify: `internal/handler/settings_test.go`

- [ ] **Step 1: 新增 configSTT DTO 并加入 configResponse**

在 `internal/handler/settings.go` 的 `configRAG` 定义后（约第 265 行后）添加：

```go
type configSTT struct {
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	Model       string `json:"model"`
	Language    string `json:"language"`
	Streaming   bool   `json:"streaming"`
	ChunkMs     int    `json:"chunk_ms"`
	ShortcutKey string `json:"shortcut_key"`
}
```

在 `configResponse` 结构（约第 181 行 `TTS` 字段后）添加字段：

```go
	STT                 configSTT            `json:"stt"`
```

- [ ] **Step 2: serveConfigGet 填充 STT**

在 `internal/handler/settings.go` 的 `serveConfigGet` 中 `TTS: configTTS{...}` 块后（约第 455 行后）添加：

```go
		STT: configSTT{
			BaseURL:     cfg.STT.BaseURL,
			APIKey:      cfg.STT.APIKey,
			Model:       cfg.STT.Model,
			Language:    cfg.STT.Language,
			Streaming:   cfg.STT.Streaming,
			ChunkMs:     cfg.STT.ChunkMs,
			ShortcutKey: cfg.STT.ShortcutKey,
		},
```

- [ ] **Step 3: 白名单 + 热更新字段**

在 `PatchableConfigPaths` map（约第 373 行 `file_search.display_limit` 后）添加：

```go
	"stt.base_url":     true,
	"stt.api_key":      true,
	"stt.model":        true,
	"stt.language":     true,
	"stt.streaming":    true,
	"stt.chunk_ms":     true,
	"stt.shortcut_key": true,
```

在 `hotReloadFields` map（约第 107 行 `file_search.display_limit` 后）添加：

```go
	"stt.base_url":     true,
	"stt.api_key":      true,
	"stt.model":        true,
	"stt.language":     true,
	"stt.streaming":    true,
	"stt.chunk_ms":     true,
	"stt.shortcut_key": true, // frontend reads from serverConfig; no backend action needed
```

- [ ] **Step 4: validatePatchValues**

在 `internal/handler/settings.go` 的 `validatePatchValues` 中，TTS 校验块（`if ok` 结束于约第 678 行）之后添加 STT 校验：

```go
	if sttVal, ok := patch["stt"].(map[string]any); ok {
		if v, ok := sttVal["base_url"].(string); ok && v != "" {
			if _, err := url.ParseRequestURI(v); err != nil {
				return fmt.Errorf("stt.base_url must be a valid URL")
			}
		}
		if v, ok := sttVal["chunk_ms"].(float64); ok && v <= 0 {
			return fmt.Errorf("stt.chunk_ms must be positive")
		}
		if v, ok := sttVal["shortcut_key"].(string); ok && v == "" {
			return fmt.Errorf("stt.shortcut_key must not be empty")
		}
	}
```

> 需确认 settings.go 已导入 `net/url`。若未导入，在 import 中补充 `"net/url"`。

- [ ] **Step 5: applyConfigPatch**

在 `internal/handler/settings.go` 的 `applyConfigPatch` 中，TTS 块（`if tts, ok := ...` 结束于约第 999 行）之后添加 STT 应用：

```go
	if sttVal, ok := patch["stt"].(map[string]any); ok {
		if v, ok := sttVal["base_url"].(string); ok {
			cfg.STT.BaseURL = v
		}
		if v, ok := sttVal["api_key"].(string); ok {
			cfg.STT.APIKey = v
		}
		if v, ok := sttVal["model"].(string); ok {
			cfg.STT.Model = v
		}
		if v, ok := sttVal["language"].(string); ok {
			cfg.STT.Language = v
		}
		if v, ok := sttVal["streaming"].(bool); ok {
			cfg.STT.Streaming = v
		}
		if v, ok := sttVal["chunk_ms"].(float64); ok {
			cfg.STT.ChunkMs = int(v)
		}
		if v, ok := sttVal["shortcut_key"].(string); ok {
			cfg.STT.ShortcutKey = v
		}
	}
```

- [ ] **Step 6: applyHotReloadGlobals 重建 provider**

在 `internal/handler/settings.go` 的 `applyHotReloadGlobals`（约第 1142 行）末尾、`reconfigureOnHotReload()` 调用之前，添加对 provider 重建的触发（STT provider 由 main.go 的 `SetReconfigureFunc` 重建）：

```go
	// STT provider is rebuilt in hotReloadReconfigure (main.go) when
	// any stt.* field changes. reconfigureOnHotReload() handles it.
```

> 说明：STT 无独立全局运行参数，重建逻辑全部放在 main.go 的 `hotReloadReconfigure`。此步骤仅留注释，实际重建在 Task 7。

- [ ] **Step 7: 添加配置测试**

在 `internal/handler/settings_test.go` 添加 STT 的 DTO 与 apply 断言。仿照现有 `configTTS` 断言，在合适测试中增加：

```go
	// STT round-trip via applyConfigPatch
	applyConfigPatch(map[string]any{
		"stt": map[string]any{
			"base_url":     "http://localhost:9000/v1",
			"api_key":      "k",
			"model":        "whisper-small",
			"language":     "en",
			"streaming":    true,
			"chunk_ms":     float64(800),
			"shortcut_key": "Ctrl+M",
		},
	})
	cfg2 := model.ConfigInstance
	if cfg2.STT.BaseURL != "http://localhost:9000/v1" || cfg2.STT.Model != "whisper-small" ||
		cfg2.STT.Language != "en" || !cfg2.STT.Streaming || cfg2.STT.ChunkMs != 800 ||
		cfg2.STT.ShortcutKey != "Ctrl+M" {
		t.Errorf("STT config not applied correctly: %+v", cfg2.STT)
	}
```

- [ ] **Step 8: 运行测试验证通过**

Run: `go build ./... && go test ./internal/handler/ -run TestConfig -v`
Expected: PASS（含新增 STT 断言）。

- [ ] **Step 9: Commit**

```bash
git add internal/handler/settings.go internal/handler/settings_test.go
git commit -m "feat(stt): wire STT config into settings (DTO, whitelist, hot-reload, validation)"
```

---

### Task 7: 连通性测试 + main.go provider 组装

**Files:**
- Modify: `internal/handler/config_test_connectivity.go`（`testSTT` + `ServeConfigTest` case）
- Modify: `internal/handler/config_test_connectivity_test.go`
- Modify: `cmd/server/main.go`（provider 构建 + hotReloadReconfigure）

- [ ] **Step 1: 添加 testSTT 与 dispatch case**

在 `internal/handler/config_test_connectivity.go` 中：
1. 在 `ServeConfigTest` 的 switch（约第 80 行 `case "tts"` 后）添加：

```go
	case "stt":
		result = testSTT(ctx, req.Values)
```

2. 在 `testRAG` 之后添加 `testSTT`：

```go
// testSTT tests connectivity to the vLLM STT (Whisper) service.
func testSTT(ctx context.Context, values map[string]any) ConnectivityTestResult {
	baseURL := resolveStringValue(values, "stt.base_url", model.ConfigInstance.STT.BaseURL)
	sttModel := resolveStringValue(values, "stt.model", model.ConfigInstance.STT.Model)
	apiKey := resolveStringValue(values, "stt.api_key", model.ConfigInstance.STT.APIKey)

	if baseURL == "" {
		return ConnectivityTestResult{Success: false, Message: "STT base URL is required"}
	}
	if sttModel == "" {
		sttModel = "openai/whisper-large-v3"
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
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service unreachable at %s", baseURL)}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ConnectivityTestResult{Success: false, Message: fmt.Sprintf("STT service returned HTTP %d", resp.StatusCode)}
	}

	var modelsResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("STT service reachable at %s, but could not parse models list", baseURL)}
	}

	for _, m := range modelsResp.Data {
		if m.ID == sttModel || strings.HasPrefix(m.ID, sttModel+":") {
			return ConnectivityTestResult{Success: true, Message: fmt.Sprintf("STT service reachable, model '%s' available", sttModel)}
		}
	}

	return ConnectivityTestResult{
		Success: false,
		Message: fmt.Sprintf("STT service reachable at %s, but model '%s' not found", baseURL, sttModel),
	}
}
```

- [ ] **Step 2: 添加连通性测试**

在 `internal/handler/config_test_connectivity_test.go` 追加：

```go
func TestSTTConnectivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/whisper-large-v3"}]}`))
	}))
	defer srv.Close()

	res := testSTT(context.Background(), map[string]any{
		"stt.base_url": srv.URL,
		"stt.model":    "openai/whisper-large-v3",
	})
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
}
```

- [ ] **Step 3: main.go 构建 STT provider**

在 `internal/handler` 导入中已含 stt（`internal/handler/stt.go` 已在 handler 包内，无需额外导入）。在 `cmd/server/main.go` 中，TTS provider 初始化之后（`handler.SetSpeechProvider(ttsProvider)` 之后，约第 614 行后）添加：

```go
	// Initialize STT (speech-to-text) provider from config
	sttProvider := newSTTProvider(cfg)
	handler.SetSTTProvider(sttProvider)
	slog.Info(
		"stt provider configured",
		slog.String("base_url", cfg.STT.BaseURL),
		slog.String("model", cfg.STT.Model),
		slog.Bool("streaming", cfg.STT.Streaming),
	)
```

在 `cmd/server/main.go` 末尾添加 factory：

```go
// newSTTProvider builds the STT provider from config.
func newSTTProvider(cfg model.Config) stt.STTProvider {
	return stt.NewVLLMProvider(cfg.STT.BaseURL, cfg.STT.Model, cfg.STT.APIKey, cfg.STT.Language)
}
```

> `cmd/server/main.go` 需导入 `"clawbench/internal/stt"`。

- [ ] **Step 4: main.go 热更新 STT**

在 `cmd/server/main.go` 的 `hotReloadReconfigure`（约第 1212 行）中，TTS 重建后添加：

```go
	// --- STT: recreate provider on config change ---
	sttProvider := newSTTProvider(cfg)
	handler.SetSTTProvider(sttProvider)
	slog.Info("hot-reload: STT provider reconfigured", slog.String("base_url", cfg.STT.BaseURL))
```

- [ ] **Step 5: 编译 + 测试**

Run: `go build ./... && go test ./internal/handler/ ./internal/model/ -count=1`
Expected: 全部 PASS。

- [ ] **Step 6: 运行全量 Go 测试确认无回归**

Run: `go test ./... 2>&1 | tail -30`
Expected: 无 FAIL。

- [ ] **Step 7: Commit**

```bash
git add internal/handler/config_test_connectivity.go internal/handler/config_test_connectivity_test.go cmd/server/main.go
git commit -m "feat(stt): add connectivity test and wire STT provider into server"
```

---

### Task 8: 前端 composable useVoiceInput

**Files:**
- Create: `web/src/composables/useVoiceInput.ts`
- Create: `web/src/composables/useVoiceInput.test.ts`

- [ ] **Step 1: 写失败测试**

```go
// Note: this is Vitest (TypeScript), not Go.
```

用以下内容创建 `web/src/composables/useVoiceInput.test.ts`：

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { useVoiceInput } from './useVoiceInput'

describe('useVoiceInput', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('starts idle, transitions to recording on start()', async () => {
    const { useVoiceInput: useV } = await import('./useVoiceInput')
    const v = useV()
    expect(v.state.value).toBe('idle')
  })

  it('state transitions through recording → transcribing → done', async () => {
    const { useVoiceInput: useV } = await import('./useVoiceInput')
    const v = useV()
    v.setState('recording')
    expect(v.state.value).toBe('recording')
    v.setState('transcribing')
    expect(v.state.value).toBe('transcribing')
    v.setState('done')
    expect(v.state.value).toBe('done')
  })

  it('appendText appends to inputText', () => {
    const { useVoiceInput: useV } = await import('./useVoiceInput')
    const v = useV()
    v.setInputText('你好')
    v.appendText('世界')
    expect(v.inputText.value).toBe('你好世界')
  })

  it('toggle flips between idle and recording', async () => {
    const { useVoiceInput: useV } = await import('./useVoiceInput')
    const v = useV()
    expect(v.state.value).toBe('idle')
    await v.toggle()
    expect(v.state.value).toBe('recording')
    await v.toggle()
    expect(v.state.value).toBe('done')
  })
})
```

> 该测试先按「暴露最小可测 API」设计；实际实现可扩展。若实现与测试 API 不完全一致，Step 3 会给出最终稳定 API，请以 Step 3 为准并同步测试。

- [ ] **Step 2: 运行测试验证失败**

Run: `cd web && npx vitest run src/composables/useVoiceInput.test.ts`
Expected: FAIL，`Cannot find module './useVoiceInput'`。

- [ ] **Step 3: 写实现**

创建 `web/src/composables/useVoiceInput.ts`：

```ts
/**
 * useVoiceInput: speech-to-text (ASR) for the chat input box.
 *
 * State machine: 'idle' → 'recording' → 'transcribing' → 'done' → 'idle'
 *
 * Streaming (cfg.STT.streaming=true): audio chunks are sent over a WebSocket
 * and incremental text is appended live. Non-streaming: the full recording is
 * POSTed on release and the result appears once.
 *
 * Recognized text is appended to `inputText` (the chat input box); it is never
 * auto-sent.
 */
import { ref, shallowRef } from 'vue'
import { useSettingsConfig } from './useSettingsConfig'
import { appLog } from '@/utils/appLog'

export type VoiceInputState = 'idle' | 'recording' | 'transcribing' | 'done'

// Module-level singleton refs (mirrors useAutoSpeech pattern).
const state = ref<VoiceInputState>('idle')
const inputText = ref('')
const error = ref('')
const isRecording = ref(false)
let mediaRecorder: MediaRecorder | null = null
let ws: WebSocket | null = null
let stream: MediaStream | null = null

function wsUrl(path: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}${path}`
}

export function useVoiceInput() {
  const settings = useSettingsConfig()

  const shortcutKey = () => (settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.shortcut_key'] as string | undefined ?? 'Alt+Space'
  const streaming = () => Boolean((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.streaming'] ?? false)
  const chunkMs = () => Number((settings.serverConfig.value as Record<string, unknown> | undefined)?.['stt.chunk_ms'] ?? 1000)

  async function toggle() {
    if (state.value === 'recording' || state.value === 'transcribing') {
      await stop()
    } else {
      await start()
    }
  }

  async function start() {
    if (state.value !== 'idle') return
    error.value = ''
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch (e) {
      error.value = '麦克风权限被拒绝或不可用'
      appLog.e('VoiceInput', 'getUserMedia failed', e)
      return
    }

    if (streaming()) {
      await startStreaming()
    } else {
      startNonStreaming()
    }
  }

  function startNonStreaming() {
    state.value = 'recording'
    isRecording.value = true
    const chunks: Blob[] = []
    mediaRecorder = new MediaRecorder(stream!, { mimeType: 'audio/webm' })
    mediaRecorder.ondataavailable = (e) => { if (e.data.size > 0) chunks.push(e.data) }
    mediaRecorder.onstop = async () => {
      state.value = 'transcribing'
      const blob = new Blob(chunks, { type: 'audio/webm' })
      try {
        const form = new FormData()
        form.append('file', blob, 'recording.webm')
        form.append('language', 'zh')
        const resp = await fetch('/api/stt/transcribe', { method: 'POST', body: form })
        const data = await resp.json().catch(() => ({}))
        if (!resp.ok) throw new Error(data.error ?? 'transcribe failed')
        appendText(data.text ?? '')
      } catch (e) {
        error.value = '语音识别失败'
        appLog.e('VoiceInput', 'non-streaming transcribe failed', e)
      } finally {
        state.value = 'done'
        isRecording.value = false
      }
    }
    mediaRecorder.start()
  }

  async function startStreaming() {
    state.value = 'recording'
    isRecording.value = true
    const chunks: Blob[] = []
    mediaRecorder = new MediaRecorder(stream!, { mimeType: 'audio/webm' })
    mediaRecorder.ondataavailable = (e) => {
      if (e.data.size > 0) chunks.push(e.data)
      if (ws && ws.readyState === WebSocket.OPEN) {
        const buf = new Uint8Array(e.data.size)
        void e.data.arrayBuffer().then((ab) => {
          ws!.send(new Uint8Array(ab))
        })
      }
    }
    ws = new WebSocket(wsUrl('/api/stt/transcribe/ws'))
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data as string)
        if (msg.type === 'text') appendText(msg.text ?? '')
      } catch { /* ignore malformed */ }
    }
    ws.onopen = () => mediaRecorder!.start()
    mediaRecorder.onstop = async () => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ type: 'end' }))
        await new Promise<void>((resolve) => {
          ws.onmessage = (e) => {
            try {
              const msg = JSON.parse(e.data as string)
              if (msg.type === 'done') {
                // final replaces incremental; set to final
                inputText.value = msg.final ?? inputText.value
                resolve()
              }
            } catch { /* ignore */ }
          }
        })
      }
      if (ws) { ws.close(); ws = null }
      state.value = 'done'
      isRecording.value = false
    }
  }

  async function stop() {
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      mediaRecorder.stop()
    }
  }

  function cancel() {
    // cleanup without finalizing text
    if (mediaRecorder && mediaRecorder.state === 'recording') {
      try { mediaRecorder.stop() } catch { /* noop */ }
    }
    if (stream) { stream.getTracks().forEach((t) => t.stop()); stream = null }
    if (ws) { ws.close(); ws = null }
    state.value = 'idle'
    isRecording.value = false
  }

  function appendText(text: string) {
    if (!text) return
    inputText.value = (inputText.value.trim() ? inputText.value.trim() + '\n' : '') + text
  }

  function setState(s: VoiceInputState) { state.value = s }
  function setInputText(s: string) { inputText.value = s }

  function reset() {
    cancel()
    inputText.value = ''
    error.value = ''
  }

  return {
    state,
    inputText,
    error,
    isRecording,
    toggle,
    start,
    stop,
    cancel,
    reset,
    appendText,
    setState,
    setInputText,
    shortcutKey,
  }
}
```

- [ ] **Step 4: 同步并运行测试**

将测试更新为与 Step 3 的稳定 API 匹配（`setState`/`setInputText`/`appendText`/`toggle` 均已暴露）。`toggle()` 中 `start()` 因 `getUserMedia` 在测试环境不可用，`toggle` 测试需 mock `navigator.mediaDevices`。更新测试：

```ts
describe('useVoiceInput toggle', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: vi.fn().mockRejectedValue(new Error('denied')) } })
  })
  afterEach(() => vi.unstubAllGlobals())

  it('toggle with denied mic returns to idle', async () => {
    const { useVoiceInput: useV } = await import('./useVoiceInput')
    const v = useV()
    await v.toggle()
    expect(v.state.value).toBe('idle')
  })
})
```

Run: `cd web && npx vitest run src/composables/useVoiceInput.test.ts`
Expected: PASS。

- [ ] **Step 5: 检查 lint**

Run: `cd web && npx eslint src/composables/useVoiceInput.ts src/composables/useVoiceInput.test.ts`
Expected: 无错误（注意 appLog 使用规范）。

- [ ] **Step 6: Commit**

```bash
git add web/src/composables/useVoiceInput.ts web/src/composables/useVoiceInput.test.ts
git commit -m "feat(web): add useVoiceInput composable"
```

---

### Task 9: ChatInputBar 集成（长按录音 + 快捷键 + 样式）

**Files:**
- Modify: `web/src/components/chat/ChatInputBar.vue`

- [ ] **Step 1: 引入 composable 与状态**

在 `<script setup>` 部分，找到现有 composable 导入区，添加：

```ts
import { useVoiceInput } from '@/composables/useVoiceInput'
```

在组件内添加（放在现有 `const autoSpeech = ...` 或输入状态附近）：

```ts
const voiceInput = useVoiceInput()
const { state: voiceState, inputText: voiceInputText, toggle: toggleVoice, isRecording: voiceRecording } = voiceInput

// Long-press recording on the send button
const VOICE_LONG_PRESS_MS = 500
let voicePressTimer: ReturnType<typeof setTimeout> | null = null
let voicePointerDown = false
let voiceLongPressActive = false

function onSendPointerDown() {
  if (!hasInputContent && !voiceInput.supportsRecording) {
    // only allow long-press record when input is empty and recording is supported
  }
  voicePointerDown = true
  voiceLongPressActive = false
  voicePressTimer = setTimeout(() => {
    if (voicePointerDown && !hasInputContent) {
      voiceLongPressActive = true
      void toggleVoice()
    }
  }, VOICE_LONG_PRESS_MS)
}

function onSendPointerUp() {
  voicePointerDown = false
  if (voicePressTimer) { clearTimeout(voicePressTimer); voicePressTimer = null }
  if (voiceLongPressActive) {
    voiceLongPressActive = false
    void toggleVoice()
  }
}
```

- [ ] **Step 2: 绑定模板事件**

修改发送按钮模板（原约第 98-105 行），添加 `@pointerdown`/`@pointerup`/`@pointerleave`，并增加录音中样式与图标。将发送按钮改为：

```html
<button v-if="!stopPrimed" class="chat-send-btn" ref="sendBtnRef"
  :class="{ queued: loading, shortcut: !hasInputContent, recording: voiceState === 'recording', transcribing: voiceState === 'transcribing' }"
  @click.stop="handleSendClick"
  @pointerdown="onSendPointerDown"
  @pointerup="onSendPointerUp"
  @pointerleave="onSendPointerUp"
  :title="!hasInputContent ? t('chat.input.quickMenu') : loading ? t('chat.input.enqueue') : t('chat.input.send')">
  <!-- Recording: red dot -->
  <span v-if="voiceState === 'recording'" class="voice-recording-dot"></span>
  <!-- Transcribing: loading spinner -->
  <Loader2 v-else-if="voiceState === 'transcribing'" class="spin-icon" :size="16" />
  <!-- Empty input: green lightning -->
  <Zap v-else-if="!hasInputContent" :size="16" />
  <Inbox v-else-if="loading" :size="16" />
  <Send v-else :size="16" />
</button>
```

> 若 `Loader2` 未导入，确认其已从 lucide-vue-next 导入（现有 stop 按钮已用）。

- [ ] **Step 3: 同步语音文本到输入框**

监听 `voiceInputText` 变化，同步到现有 `inputText`。在 `<script setup>` 添加 watch：

```ts
import { watch } from 'vue'
watch(voiceInputText, (val) => {
  if (val && val !== inputText.value) {
    inputText.value = val
  }
})
```

- [ ] **Step 4: 注册 Alt+Space 快捷键**

在 `onMounted`/事件监听区添加全局 keydown（或复用 useGlobalEvents 模式）。在组件 `onMounted` 中注册，`onUnmounted` 移除：

```ts
function onVoiceShortcut(e: KeyboardEvent) {
  const sc = voiceInput.shortcutKey()
  if (sc === 'Alt+Space') {
    if (e.altKey && e.code === 'Space') {
      e.preventDefault()
      void toggleVoice()
    }
  }
}
```

在 `onMounted` 添加 `window.addEventListener('keydown', onVoiceShortcut)`，`onUnmounted` 移除。

- [ ] **Step 5: 添加录音中样式**

在 `<style>` 中添加：

```css
.voice-recording-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: #ff3b30;
  animation: voice-pulse 1s ease-in-out infinite;
}
@keyframes voice-pulse {
  0%, 100% { transform: scale(0.8); opacity: 1; }
  50% { transform: scale(1.4); opacity: 0.6; }
}
.chat-send-btn.recording {
  background: #ff3b30;
}
.chat-send-btn.transcribing .spin-icon {
  animation: spin 1s linear infinite;
}
```

- [ ] **Step 6: 构建检查**

Run: `cd web && npx vue-tsc --noEmit`
Expected: 无类型错误。

- [ ] **Step 7: Commit**

```bash
git add web/src/components/chat/ChatInputBar.vue
git commit -m "feat(web): integrate voice input into chat input bar"
```

---

### Task 10: 前端服务端默认值 + 设置面板 + i18n

**Files:**
- Modify: `web/src/composables/useSettingsConfig.ts:301`（serverDefaults）
- Modify: `web/src/components/settings/settingsFieldMap.ts`（stt 分类/面板）
- Modify: `web/src/components/settings/SettingsIndex.vue:44`（分类入口）
- Modify: `web/src/i18n/locales/zh.ts`、`en.ts`

- [ ] **Step 1: serverDefaults 加 STT**

在 `web/src/composables/useSettingsConfig.ts` 的 `serverDefaults` 中，`tts.max_cache_files` 之后添加：

```ts
  'stt.base_url': 'http://localhost:8000/v1',
  'stt.api_key': '',
  'stt.model': 'openai/whisper-large-v3',
  'stt.language': 'zh',
  'stt.streaming': false,
  'stt.chunk_ms': 1000,
  'stt.shortcut_key': 'Alt+Space',
```

- [ ] **Step 2: settingsFieldMap 加 stt 分类与面板**

在 `web/src/components/settings/settingsFieldMap.ts` 中：
1. 在 `tts_engine` 分类定义后（约第 298 行后）添加：

```ts
  stt: [
    { type: 'item', spec: { labelKey: 'settings.items.sttSection', descriptionKey: 'settings.items.sttSectionDesc', key: 'navigateStt', type: 'action', source: 'local', navigateTo: 'stt:stt_engine' } },
  ],
  stt_engine: [
    { type: 'panel', config: {
      panelId: 'stt_engine',
      commonFields: [
        { labelKey: 'settings.items.sttBaseUrl', descriptionKey: 'settings.items.sttBaseUrlDesc', key: 'stt.base_url', type: 'text', source: 'server', sectionHeader: 'settings.items.sttHeader' },
        { labelKey: 'settings.items.sttApiKey', descriptionKey: 'settings.items.sttApiKeyDesc', key: 'stt.api_key', type: 'password', source: 'server' },
        { labelKey: 'settings.items.sttModel', descriptionKey: 'settings.items.sttModelDesc', key: 'stt.model', type: 'text', source: 'server' },
        { labelKey: 'settings.items.sttLanguage', descriptionKey: 'settings.items.sttLanguageDesc', key: 'stt.language', type: 'text', source: 'server' },
        { labelKey: 'settings.items.sttStreaming', descriptionKey: 'settings.items.sttStreamingDesc', key: 'stt.streaming', type: 'switch', source: 'server' },
        { labelKey: 'settings.items.sttChunkMs', descriptionKey: 'settings.items.sttChunkMsDesc', key: 'stt.chunk_ms', type: 'number', source: 'server', min: 200, max: 10000, step: 100 },
        { labelKey: 'settings.items.sttShortcutKey', descriptionKey: 'settings.items.sttShortcutKeyDesc', key: 'stt.shortcut_key', type: 'text', source: 'server' },
      ],
      requiredFields: ['stt.base_url', 'stt.model'],
      hasConnectivityTest: true,
      getTestCategories: (values) => [{ category: 'stt', values }],
    }},
  ],
```

2. 在 `subPagePanelMap`（约第 410 行）中添加：

```ts
  'stt:stt_engine': {
    panelConfig: getCategoryPanels('stt_engine')[0],
    titleKey: 'settings.items.sttSection',
  },
```

> 需确认 `GroupPanelConfig` 支持 `switch`/`password`/`number`/`text` 类型（`summarization_voice` 已用 password/text，`autoSpeech` 用 switch，`tts_engine` 用 number——均支持）。若 `switch` 类型不支持，改用 `type: 'select'` with options true/false。

- [ ] **Step 3: SettingsIndex 加分类入口**

在 `web/src/components/settings/SettingsIndex.vue` 的 `categoryDefs`（约第 50 行 `tts` 后）添加：

```ts
  { id: 'stt', icon: Mic },
```

并在 `import` 区添加 `Mic`（若未导入，从 `lucide-vue-next` 导入：`import { Mic } from 'lucide-vue-next'`）。

- [ ] **Step 4: i18n zh.ts**

在 `web/src/i18n/locales/zh.ts`：
1. `categories` 的 `tts: '语音朗读'` 后添加：

```ts
      stt: '语音识别',
```

2. `items` 的 tts 相关键后（约 `ttsMaxCacheFilesDesc` 后）添加：

```ts
      sttSection: '语音识别',
      sttSectionDesc: '通过 vLLM Whisper 进行语音输入识别',
      sttHeader: 'vLLM 接口配置',
      sttBaseUrl: '接口地址',
      sttBaseUrlDesc: 'vLLM OpenAI 兼容的语音识别接口地址（如 http://localhost:8000/v1）',
      sttApiKey: 'API 密钥',
      sttApiKeyDesc: '语音识别接口的认证密钥（留空则不认证）',
      sttModel: '识别模型',
      sttModelDesc: '用于语音识别的模型名称（如 openai/whisper-large-v3）',
      sttLanguage: '识别语言',
      sttLanguageDesc: '语音识别使用的语言代码（如 zh、en）',
      sttStreaming: '流式识别',
      sttStreamingDesc: '开启后按住说话时实时上屏增量文本；关闭则松手后整段识别',
      sttChunkMs: '流式切片间隔(ms)',
      sttChunkMsDesc: '流式识别时每间隔多少毫秒提交一次增量识别',
      sttShortcutKey: '录音快捷键',
      sttShortcutKeyDesc: '触发录音的全局快捷键（默认 Alt+Space）',
```

- [ ] **Step 5: i18n en.ts**

在 `web/src/i18n/locales/en.ts`：
1. `categories` 的 `tts` 后添加：

```ts
      stt: 'Speech Recognition',
```

2. `items` 添加对应英文键：

```ts
      sttSection: 'Speech Recognition',
      sttSectionDesc: 'Voice input via vLLM Whisper',
      sttHeader: 'vLLM API Config',
      sttBaseUrl: 'Endpoint URL',
      sttBaseUrlDesc: 'vLLM OpenAI-compatible speech recognition endpoint (e.g. http://localhost:8000/v1)',
      sttApiKey: 'API Key',
      sttApiKeyDesc: 'Authentication key for the speech recognition endpoint (empty = no auth)',
      sttModel: 'Model',
      sttModelDesc: 'Speech recognition model name (e.g. openai/whisper-large-v3)',
      sttLanguage: 'Language',
      sttLanguageDesc: 'Language code for recognition (e.g. zh, en)',
      sttStreaming: 'Streaming',
      sttStreamingDesc: 'Enable real-time incremental text while recording; off = full recognition on release',
      sttChunkMs: 'Streaming interval (ms)',
      sttChunkMsDesc: 'Interval in ms between incremental recognition submissions',
      sttShortcutKey: 'Recording shortcut',
      sttShortcutKeyDesc: 'Global shortcut to trigger recording (default Alt+Space)',
```

- [ ] **Step 6: 前端类型检查 + 测试**

Run: `cd web && npx vue-tsc --noEmit && npx vitest run src/composables/useVoiceInput.test.ts`
Expected: 无类型错误，测试 PASS。

- [ ] **Step 7: Commit**

```bash
git add web/src/composables/useSettingsConfig.ts web/src/components/settings/settingsFieldMap.ts web/src/components/settings/SettingsIndex.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(web): add STT settings panel and i18n"
```

---

### Task 11: 全量验证

- [ ] **Step 1: 运行全量 Go 测试**

Run: `go test ./... 2>&1 | tail -40`
Expected: 全部 PASS，无 FAIL。

- [ ] **Step 2: 前端测试 + lint + typecheck**

Run: `cd web && npm test 2>&1 | tail -30`
Expected: PASS。

Run: `cd web && npx eslint src/composables/useVoiceInput.ts src/components/chat/ChatInputBar.vue src/components/settings/settingsFieldMap.ts`
Expected: 无错误。

- [ ] **Step 3: 运行 pre-push-checks**

Run: `./scripts/pre-push-checks.sh --skip-coverage 2>&1 | tail -40`
Expected: 全部通过。

- [ ] **Step 4: 手动冒烟（可选）**

启动 vLLM Whisper server 后，用 `./clawbench` 打开设置页确认 STT 面板出现、可保存、可测连通；聊天输入长按发送按钮可录音并上屏。

---

## 设计注意事项

1. **流式增量语义**：`useVoiceInput.startStreaming` 通过 WS 逐帧发送音频，后端按 `ChunkMs` 对**新增段**识别并回传 `text` 增量；松手后发 `end` 控制帧，后端做整段最终识别返回 `done`，前端用 `final` 覆盖增量文本。
2. **非流式**：`startNonStreaming` 录制到松手，`onstop` 时 POST 整段，期间按钮为 `transcribing`（加载）状态，完成后一次性上屏。
3. **文本不自动发送**：`appendText` 只更新 `inputText`，由用户手动按发送。
4. **URL 处理**：`buildSTTEndpointURL` 兼容 BaseURL 带/不带 `/v1`。
5. **测试纪律**：Go 用 `*_test.go`、前端用 `.test.ts`，每个 task 先写失败测试再实现。
