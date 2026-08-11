# 语音输入（ASR）设计文档

日期：2026-08-11

## 1. 背景与目标

为 ClawBench 增加语音输入（ASR，Speech-to-Text）能力，走**后端代理 vLLM Whisper** 模式，镜像已有 TTS（语音朗读）的后端集成、服务端配置、热更新与设置面板模式。

### 核心交互（用户需求）
- 按快捷键后，聊天发送按钮变为"录音中"样式。
- 文字**实时上屏**（流式协议下按住说话时实时显示识别文字）。
- 对**非流式**接口：松手后按钮变加载状态，识别完毕一次性上屏。
- 支持 vLLM 标准接口，流式与非流式 2 种协议。
- 配置添加到配置页；新增一个语音识别面板，内部配置逻辑参考语音朗读（TTS）。

### 已确认的决策
1. **后端代理**（与 TTS 一致）：浏览器 MediaRecorder 采集 → 流式上传 Go 后端 → 转发 vLLM `/v1/audio/transcriptions` → 返回文本。走 `/api/` 鉴权，配置存服务端并热更新。
2. **流式协议 = 分段增量识别**：录制中按片段时间切片，累积提交，仅识别**新增音频段**并**追加**上屏（文本单调增长、不覆盖、不抖动）；松手后做一次完整识别修正最终文本。
3. **非流式协议 = 整段一次 POST**：按住期间不请求，松手后整段上传识别，一次性上屏。
4. **触发方式**：发送按钮**长按（≥500ms）**录音、松手结束；另支持**全局快捷键 Alt+Space**（默认，设置可改）切换。
5. **识别文本归宿**：放入聊天输入框（textarea），**不自动发送**，供用户审阅/拼接/编辑后手动发送。

## 2. 架构

```
[浏览器 MediaRecorder]
   │ 采集音频 (webm/opus)
   ▼
[ChatInputBar.vue + useVoiceInput composable]
   │ 流式: WS /api/stt/transcribe/ws 逐帧上传
   │ 非流式: POST /api/stt/transcribe (multipart)
   ▼
[Go handler /api/stt/*  (middleware.Auth)]
   │ 转发到 vLLM OpenAI 兼容 /v1/audio/transcriptions
   ▼
[internal/stt VLLMProvider]
   │ 返回识别文本
   ▼
[前端] 文本追加到 textarea
```

### 后端包 `internal/stt/`（镜像 `internal/speech/`）

新增独立包 `internal/stt/`，不直接复用 `internal/speech/`（语义不同：TTS 合成 vs ASR 识别），但复用其结构风格。

**`interface.go`**
```go
package stt

// STTProvider 抽象语音识别。实现可切换（vLLM Whisper 等）。
type STTProvider interface {
    // Transcribe 从 audioReader 读取一段音频并识别为文本。
    // language 为语言码（如 "zh","en"），实现可忽略。
    // 返回识别文本。流式/非流式由 handler 层控制，provider 只做
    // "给定一段音频 → 返回文本"。
    Transcribe(ctx context.Context, audioReader io.Reader, language string) (string, error)
}
```

**`vllm_stt.go`** — `VLLMProvider`：
- 字段：`BaseURL`、`APIKey`、`Model`、`Language`、`Streaming bool`、`ChunkMs int`。
- 调用 OpenAI 兼容 `/v1/audio/transcriptions`，multipart：`file`、`model`、`language`。
- `BaseURL` 支持 `http://host:port/v1` 或 `http://host:port`（无 `/v1` 时自动补全），镜像 RAG/Summarize 的 URL 处理。
- 流式与�on非流式在 handler 层区分；provider 不感知，简化实现与测试。

### 新增路由（`internal/handler/handler.go`）

- `POST /api/stt/transcribe` — 非流式：multipart 音频，返回 `{ "text": "..." }`。处理逻辑：解析 multipart → 调 `STTProvider.Transcribe` → JSON 返回。
- `WS /api/stt/transcribe/ws` — 流式：WebSocket 双向。客户端逐帧发送二进制音频 + 结束标记；服务端对每片累积提交识别，回传增量文本 JSON 事件。松手（客户端发结束帧）后做最终完整识别。

两者均包 `middleware.Auth`。

### 服务端配置（镜像 TTS）

**`internal/model/config.go`** — 新增：
```go
type STTConfig struct {
    BaseURL    string `yaml:"base_url"`    // vLLM OpenAI 兼容基址（默认 http://localhost:8000/v1）
    APIKey     string `yaml:"api_key"`     // API key（可选）
    Model      string `yaml:"model"`       // 识别模型（默认 openai/whisper-large-v3）
    Language   string `yaml:"language"`    // 语言码（默认 "zh"）
    Streaming  bool   `yaml:"streaming"`   // true=流式分段增量，false=非流式整段（默认 false）
    ChunkMs    int    `yaml:"chunk_ms"`    // 流式切片间隔（默认 1000）
    ShortcutKey string `yaml:"shortcut_key"` // 录音快捷键（默认 "Alt+Space"）
}
```
`Config` 新增字段 `STT STTConfig \`yaml:"stt"\``。

**`internal/model/defaults.go`** — `ApplyDefaults()` 加 STT 默认值（上面注释中的默认）。

**`internal/handler/settings.go`**：
- `configResponse` DTO 加 `configSTT`。
- `PatchableConfigPaths` 白名单加 `stt.*` 各路径。
- `hotReloadFields` 加 STT 相关（BaseURL/APIKey/Model/Language/Streaming/ChunkMs 热更新重建 provider；ShortcutKey 为纯前端配置，前端从 serverConfig 读取）。
- `validatePatchValues` 校验：BaseURL 合法 URL、Model 非空。
- `applyConfigPatch` 处理 STT 字段。

**`internal/handler/config_test_connectivity.go`** — `ServeConfigTest` 加 `stt` category → `testSTT`：向 `${BaseURL}/models` 或 `/v1/models` 发 GET，校验 200 与 model 存在（镜像 `testTTS`/`testSummarize`）。

**`cmd/server/main.go`**：
- 启动时 `newSTTProvider(cfg)` → `handler.SetSTTProvider(provider)`。
- `hotReloadReconfigure` 中重建 STT provider。
- `internal/handler` 新增 `SetSTTProvider`/`GetSTTProvider`（RWMutex，镜像 TTS 的 `SetSpeechProvider`）。

## 3. 前端

### `web/src/composables/useVoiceInput.ts`（新）
单例 composable，管理录音/识别生命周期。状态机：`'idle' | 'recording' | 'transcribing' | 'done'`。

- `toggle()`：Alt+Space 或长按触发的切换入口。idle→开始录音，recording→结束。
- 录音：`navigator.mediaDevices.getUserMedia({ audio: true })` + `MediaRecorder`（`audio/webm;codecs=opus`）。首次弹授权。
- 流式：MediaRecorder `ondataavailable` 累积到 buffer，按 `ChunkMs` 切片，经 WS 上传新增段，回传增量文本追加到 `inputText`。
- 非流式：松手时结束 MediaRecorder，把完整 blob POST `/api/stt/transcribe`，期间 `state='transcribing'`（按钮加载），返回后一次性上屏。
- 暴露：`state`、`transcribing`、`start`、`stop`、`error`、`result`、`appendToInput`。

### `web/src/components/chat/ChatInputBar.vue`
- 新增麦克风/录音状态：
  - 发送按钮**长按 ≥500ms** 开始录音（`@pointerdown` 起 timer，`@pointerup`/`@pointerleave` 取消或结束）。
  - 录音中按钮渲染"录音中"样式（红点/脉冲，`.recording` class）。
  - 非流式松手后按钮变加载（`transcribing`，复用现有 spinner `Loader2`）。
- 全局快捷键 **Alt+Space**：在 `useGlobalEvents.ts` 或 `useVoiceInput` 注册 keydown，阻断默认行为并 `toggle()`。
- 录音中需显示动态 placeholder（"正在录音…松开结束"）或隐藏打字提示。
- 识别文本通过 `appendToInput` 追加到 `inputText`（与现有输入可拼接），不自动发送。

### 设置面板（镜像 TTS）
- `web/src/components/settings/settingsFieldMap.ts`：新增 `stt` 分类 → `stt:stt_engine` 子页（`GroupPanelConfig`）：`entrySelector`/`commonFields` = `stt.base_url`、`stt.api_key`、`stt.model`、`stt.language`、`stt.streaming`(switch)、`stt.chunk_ms`、`stt.shortcut_key`；`hasConnectivityTest: true`。
- `SettingsIndex.vue` `categoryDefs` 加 `stt`（Mic 图标）。
- `subPagePanelMap` 加 `'stt:stt_engine'`。
- i18n：`zh.ts`/`en.ts` 加 `settings.items.stt*` 键。

### 麦克风权限与失败处理
- 授权拒绝/设备不可用 → 顶部 toast + `state` 复位。
- 识别失败（vLLM 不可达、无返回）→ toast 错误，按钮复位。
- WS 断连重连（镜像现有 WS 连接封装）。

## 4. 测试

### Go
- `internal/stt`：`vllm_stt_test.go` — mock `http.Client`（`httptest` server）验证 multipart 请求体、BaseURL `/v1` 补全、响应解析、错误传播。
- `internal/handler`：非流式 `transcribe` 测试（multipart 上传 → 返回 text）；流式 WS 分段测试（发音频帧 → 收增量文本 → 结束帧 → 最终文本）；配置 DTO/白名单/校验/热更新。

### 前端（`.test.ts`）
- `useVoiceInput`：状态机转换（idle→recording→transcribing→done）、流式增量追加、非流式整段、错误复位。
- `ChatInputBar`：长按计时开始/取消、录音中样式、快捷键 Alt+Space 切换、非流式加载态。

## 5. 边界与范围
- 仅文本识别，不自动发送（已确认）。
- 快捷键默认 Alt+Space，设置可改，走 serverConfig 下发。
- 识别语言由配置 `stt.language` 决定。
- 不做说话人分离/标点后处理（YAGNI），vLLM 返回即上屏。
