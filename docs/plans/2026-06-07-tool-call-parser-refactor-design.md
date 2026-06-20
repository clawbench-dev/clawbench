# Tool Call 解析重构设计

## 背景

当前 `internal/ai/` 中，各智能体的工具调用解析逻辑内嵌在 `ParseLine` / `mapACPSessionUpdate` 的大 switch-case 中，存在以下问题：

1. **StreamParser 过于庞大**（`ParseLine` 方法约 360 行）— 工具调用解析分散在 5 个分支中
2. **归一化逻辑散落各处** — remap 表在每个解析器内联定义
3. **输入提取策略不统一** — ACP 有 4 层 fallback，CLI 各自处理
4. **测试困难** — 无法单独测试工具调用提取逻辑

## 目标

将每个智能体的工具调用解析从流解析器中拆出，变成独立的函数，放在 `xxx_tool.go` 文件中。解析完成后输出统一的 `*ToolCall` 结构。

## 核心原则

1. 每个智能体一个 `xxx_tool.go` 文件，与流解析器 `xxx_stream.go` 物理分离
2. 函数签名统一，接收具体类型指针，返回 `*ToolCall` 或 `[]StreamEvent`
3. 归一化集中到 `common_stream.go` 的共享层
4. ACP 和 CLI 都按智能体拆分提取逻辑，仅共享归一化函数

## ACP 与 CLI 的关系

同一智能体在两种模式下的原始数据结构**完全不同**，不可复用提取逻辑：

- **CLI 模式**：从 TUI 私有 JSON 格式逆向提取（每个 TUI 格式不同）
- **ACP 模式**：从 ACP 协议统一字段中提取，但不同智能体填充的字段内容和命名习惯完全不同

**共享点**：`normalizeToolName` + `normalizeToolInput` — 不管数据从哪来，最终都映射到同一套规范字段名。

### ACP 各智能体原始数据差异（来自生产数据库实测）

| 差异点 | Claude ACP | OpenCode ACP | CodeBuddy ACP | Gemini ACP |
|--------|-----------|-------------|---------------|-----------|
| `_meta` 私有扩展 | `claudeCode.toolName` + `claudeCode.toolResponse` | ❌ 无 | `codebuddy.ai/toolName` + trace IDs | ❌ 无 |
| 工具名来源 | `_meta.claudeCode.toolName`（首选）→ `extractToolName` fallback | `title` + `kind` | `_meta.codebuddy.ai/toolName`（首选）→ `extractToolName` fallback | `toolCallId` 前缀 |
| rawInput 字段名 | `file_path` (snake_case) | `filePath` (camelCase) | 混合/流式增量 | ❌ 无 |
| rawOutput 格式 | 字符串 | 嵌套对象 `{metadata, output}` | 字符串 | ❌ 无 |
| Content 类型 | diff + text | text | text | ❌ 无 |
| Locations | ✅ (edit) | ✅ (read) | ❌ | ✅ (read/search) |

> **注意**：`_meta` 字段在 ACP SDK 中定义为 `Meta map[string]any`（JSON key: `_meta`），当前代码未读取它。
> 但生产数据库证实 Claude 和 CodeBuddy 的 ACP 智能体确实在 `_meta` 中发送了工具名信息。
> 重构后应优先从 `_meta` 读取工具名（更精确），`extractToolName` 作为 fallback。

示例数据：

**Claude ACP Edit:**
```json
{
  "_meta": {"claudeCode": {"toolName": "Edit"}},
  "kind": "edit",
  "rawInput": {"file_path": "...", "old_string": "...", "new_string": "...", "replace_all": false},
  "content": [{"type": "diff", "path": "...", "oldText": "...", "newText": "..."}],
  "locations": [{"path": "..."}],
  "title": "Edit web/src/...",
  "toolCallId": "call_function_xxx_1"
}
```

**OpenCode ACP Read:**
```json
{
  "kind": "read",
  "rawInput": {"filePath": "/home/xulongzhe/..."},
  "locations": [{"path": "..."}],
  "title": "read",
  "toolCallId": "call_00_xxx"
}
```

**Gemini ACP Read:**
```json
{
  "kind": "read",
  "locations": [{"path": "/home/.../README.md"}],
  "title": "README.md",
  "toolCallId": "read_file-1780629348181-1"
}
```

**CodeBuddy ACP Bash:**
```json
{
  "_meta": {"codebuddy.ai/toolName": "Bash", "codebuddy.ai/messageId": "...", ...},
  "kind": "other",
  "rawInput": {},
  "title": "Bash",
  "toolCallId": "019e95ac..."
}
```

## 文件结构

### 新增文件

#### CLI 层

| 文件 | 职责 | 函数 |
|------|------|------|
| `claude_tool.go` | Claude 协议族（Claude/CodeBuddy/Qoder/Cline/Copilot）工具调用解析 | `parseClaudeStreamToolEvent`, `parseClaudeAssistantToolUse`, `parseClaudeUserToolResult` |
| `gemini_tool.go` | Gemini/Kimi 工具调用解析 | `parseGeminiToolUse`, `parseGeminiToolResult` |
| `opencode_tool.go` | OpenCode 工具调用解析 | `parseOpenCodeToolEvent` |
| `codex_tool.go` | Codex 工具调用解析 | `parseCodexToolStart`, `parseCodexToolComplete`, `emitBashToolCall` |
| `deepseek_tool.go` | DeepSeek 工具调用解析 | `parseDeepSeekToolUse`, `parseDeepSeekToolResult` |
| `pi_tool.go` | Pi 工具调用解析 + edits 嵌套归一化 | `parsePiToolCallEnd`, `parsePiToolExecutionEnd`, `normalizePiEditInput` |

#### ACP 层

| 文件 | 职责 | 函数 |
|------|------|------|
| `acp_tool.go` | ACP 路由分发 + 共享工具函数 | `parseACPToolCall(backend, tc)`, `parseACPToolCallUpdate(backend, tcu)`, `parseGenericACPToolCall`, `parseGenericACPToolCallUpdate`, `extractACPToolOutput`, `extractACPToolOutputFromContent` |
| `acp_claude_tool.go` | Claude/Qoder ACP 工具调用提取 | `parseClaudeACPToolCall`, `parseClaudeACPToolCallUpdate` |
| `acp_opencode_tool.go` | OpenCode ACP 工具调用提取 | `parseOpenCodeACPToolCall`, `parseOpenCodeACPToolCallUpdate` |
| `acp_codebuddy_tool.go` | CodeBuddy ACP 工具调用提取 | `parseCodeBuddyACPToolCall`, `parseCodeBuddyACPToolCallUpdate` |
| `acp_gemini_tool.go` | Gemini ACP 工具调用提取（含 Kimi ACP） | `parseGeminiACPToolCall`, `parseGeminiACPToolCallUpdate` |

### 修改文件

| 文件 | 改动 |
|------|------|
| `common_stream.go` | 新增 `perAgentInputRemaps` + `getRemaps()` |
| `stream_parser.go` | 删除工具调用相关逻辑（约 300 行），ParseLine 只保留 thinking/text 路由 + 委托调用 |
| `gemini_stream.go` | 删除工具调用内联逻辑，委托 `gemini_tool.go` |
| `opencode_stream.go` | 同上，委托 `opencode_tool.go` |
| `codex_stream.go` | 同上，委托 `codex_tool.go` |
| `deepseek_stream.go` | 同上，委托 `deepseek_tool.go`，删除 `normalizeDeepSeekInput` |
| `pi_stream.go` | 删除工具调用 + `normalizePiInput` / `normalizePiEditInput`，委托 `pi_tool.go` |
| `acp_events.go` | 删除工具调用核心逻辑，委托 `acp_tool.go`；保留事件路由和 debouncer。`mapACPSessionUpdate` 签名增加 `backend string` 参数 |
| `acp_client.go` | 传递 `c.connRef.agent.Backend` 给 `mapACPSessionUpdate` |
| `acp_debounce.go` | `toolCallDebouncer` 新增 `backend string` 字段，`handleToolCallUpdate` 传递 backend 给解析函数 |

### 不改动的文件

- `interface.go` — `StreamEvent` / `ToolCall` / `ContentBlock` 结构不变
- `accumulate.go` — `AccumulateBlock` 不变
- `vecli.go` / `vecli_stream.go` — 纯文本无工具调用
- `cline.go`, `copilot.go`, `qoder.go`, `codebuddy.go` — 只配 `newParser: StreamParser`，不动

> **备注**：Cline/Copilot/Qoder/CodeBuddy 的 CLI 模式都共享 `StreamParser`（即 `claude_tool.go`），
> 而 CodeBuddy/Qoder 的 ACP 模式分别路由到 `acp_codebuddy_tool.go` 和 `acp_claude_tool.go`。

## 函数签名设计

### CLI 层 — 简单智能体（Gemini/Pi/DeepSeek/OpenCode/Codex）

```go
// 输入：已反序列化的具体类型指针
// 输出：归一化后的 *ToolCall，nil 表示不包含工具调用

func parseGeminiToolUse(msg *GeminiStreamMessage) *ToolCall
func parseGeminiToolResult(msg *GeminiStreamMessage) *ToolCall

func parsePiToolCallEnd(evt *PiAssistantMessageEvent) *ToolCall
func parsePiToolExecutionEnd(msg *PiStreamMessage) *ToolCall

func parseDeepSeekToolUse(msg *DeepSeekStreamMessage) *ToolCall
func parseDeepSeekToolResult(msg *DeepSeekStreamMessage) *ToolCall

func parseOpenCodeToolEvent(msg *OpenCodeStreamMessage) *ToolCall

func parseCodexToolStart(msg *CodexStreamMessage) *ToolCall
func parseCodexToolComplete(msg *CodexStreamMessage) *ToolCall
```

### CLI 层 — Claude 协议族（StreamParser）

Claude 流式模式下单条输入可能产出多个事件，用 `[]StreamEvent`。

注意：这些函数会修改传入的 `activeTools` / `emittedToolInputEmpty` 等 map，
**不是纯函数**，而是有状态的辅助函数。将状态作为参数显式传入，
使函数本身不持有状态，方便测试。

```go
// ClaudeStreamToolState 封装 StreamParser 的工具调用相关状态，
// 作为参数传给工具解析函数，使函数本身不持有状态。
type ClaudeStreamToolState struct {
    ActiveTools           map[int]*ToolCall
    ActiveToolResults     map[int]*toolResultAccum
    ActiveThinkingBlocks  map[int]bool
    EmittedToolInputEmpty map[string]bool
    ReceivedPartialToolUse bool
    ReceivedPartial        bool
    ReceivedPartialThinking bool
}

// 从 stream_event 的 content_block_start/delta/stop 解析工具调用
func parseClaudeStreamToolEvent(evt *ClaudeStreamMessage, state *ClaudeStreamToolState) []StreamEvent

// 从 assistant verbose message 解析工具调用（补充流式遗漏的 input）
func parseClaudeAssistantToolUse(msg *ClaudeStreamMessage, state *ClaudeStreamToolState) []StreamEvent

// 从 user message 解析 tool_result
func parseClaudeUserToolResult(msg *ClaudeStreamMessage) []StreamEvent
```

> **类型说明**：实际代码中使用 `ClaudeStreamMessage`（含 `Type` 字段区分 "assistant"/"user"），
> 不存在独立的 `ClaudeAssistantMessage` / `ClaudeUserMessage` 类型。

### ACP 层 — 路由分发

```go
// acp_tool.go — 路由 + 共享工具函数

// 路由使用 agent.Backend（如 "claude", "gemini"），
// 不是 agent.ID（agent.ID 是用户自定义名称，如 "my-claude"）。
func parseACPToolCall(backend string, tc acp.SessionUpdateToolCall) *ToolCall {
    switch backend {
    case "claude", "qoder":
        return parseClaudeACPToolCall(tc)
    case "codebuddy":
        return parseCodeBuddyACPToolCall(tc)
    case "opencode":
        return parseOpenCodeACPToolCall(tc)
    case "gemini", "kimi":
        return parseGeminiACPToolCall(tc)
    default:
        return parseGenericACPToolCall(tc)
    }
}

func parseACPToolCallUpdate(backend string, tcu acp.SessionToolCallUpdate) *ToolCall {
    switch backend {
    case "claude", "qoder":
        return parseClaudeACPToolCallUpdate(tcu)
    case "codebuddy":
        return parseCodeBuddyACPToolCallUpdate(tcu)
    case "opencode":
        return parseOpenCodeACPToolCallUpdate(tcu)
    case "gemini", "kimi":
        return parseGeminiACPToolCallUpdate(tcu)
    default:
        return parseGenericACPToolCallUpdate(tcu)
    }
}

// 共享输出提取函数
func extractACPToolOutput(rawOutput any) string
func extractACPToolOutputFromContent(content []acp.ToolCallContent) string

// Generic fallback — 使用当前 extractToolName 逻辑 + RawInput/Content/Title/Locations fallback 链
func parseGenericACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall
func parseGenericACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall
```

### ACP 层 — 各智能体提取函数

```go
// acp_claude_tool.go
func parseClaudeACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    // Claude ACP 特点：
    // - 工具名从 _meta.claudeCode.toolName 获取（首选），fallback 到 extractToolName
    // - rawInput 字段名已经是 snake_case（file_path, old_string, new_string）
    // - content 可能有 diff 类型
    // - rawOutput 是纯文本字符串
    // - _meta.claudeCode.toolResponse 包含 stdout/stderr/interrupted（completed 时）
    name := extractMetaToolName(tc.Meta, "claudeCode") // 从 _meta.claudeCode.toolName 读取
    if name == "" {
        name = extractToolName(tc.Title, tc.Kind, string(tc.ToolCallId))
    }
    ...
}
func parseClaudeACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall { ... }

// acp_opencode_tool.go
func parseOpenCodeACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    // OpenCode ACP 特点：
    // - 无 _meta
    // - rawInput 用 camelCase（filePath → file_path 需归一化）
    // - rawOutput 是嵌套对象 {metadata: {...}, output: "..."}
    // - 工具名从 title + kind 提取
    ...
}
func parseOpenCodeACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall { ... }

// acp_codebuddy_tool.go
func parseCodeBuddyACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    // CodeBuddy ACP 特点：
    // - 工具名从 _meta["codebuddy.ai/toolName"] 获取（首选），fallback 到 extractToolName
    // - rawInput 流式增量（每次 tool_call_update 只包含部分内容）
    // - 字段名可能混合 camelCase/snake_case
    name := extractMetaToolNameFlat(tc.Meta, "codebuddy.ai/toolName") // 从 _meta 读取
    if name == "" {
        name = extractToolName(tc.Title, tc.Kind, string(tc.ToolCallId))
    }
    ...
}
func parseCodeBuddyACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall { ... }

// acp_gemini_tool.go
func parseGeminiACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    // Gemini ACP 特点：
    // - 完全没有 rawInput / rawOutput / _meta
    // - 工具名从 toolCallId 前缀推断（read_file-, glob-, list_directory- 等）
    // - 输入从 locations + title 推断
    // - 输出从 content 推断
    ...
}
func parseGeminiACPToolCallUpdate(tcu acp.SessionToolCallUpdate) *ToolCall { ... }
```

### ACP 层 — _meta 提取工具函数

```go
// acp_tool.go

// extractMetaToolName 从 _meta 的嵌套对象中提取工具名。
// 用于 Claude ACP: _meta.claudeCode.toolName → "Edit"
func extractMetaToolName(meta map[string]any, namespace string) string {
    if meta == nil { return "" }
    ns, ok := meta[namespace]
    if !ok { return "" }
    m, ok := ns.(map[string]any)
    if !ok { return "" }
    name, _ := m["toolName"].(string)
    return name
}

// extractMetaToolNameFlat 从 _meta 的顶层 key 中提取工具名。
// 用于 CodeBuddy ACP: _meta["codebuddy.ai/toolName"] → "Bash"
func extractMetaToolNameFlat(meta map[string]any, key string) string {
    if meta == nil { return "" }
    name, _ := meta[key].(string)
    return name
}
```

## 归一化层

### perAgentInputRemaps

在 `common_stream.go` 中集中定义。

`normalizeToolInput` 已有 `defaultMappings`（`filePath→file_path`, `cmd→command`, `exec→command`），
per-agent remaps 只需包含**各智能体特有的覆盖项**，通用映射由 defaultMappings 处理。

```go
var perAgentInputRemaps = map[string]map[string]string{
    // CLI 层
    "opencode_cli": {"oldString": "old_string", "newString": "new_string"},
    "deepseek_cli": {
        "path": "file_path", "search": "old_string", "replace": "new_string",
        "filePaths": "file_paths", "dirPath": "path",
    },
    "pi_cli": {"path": "file_path"},
    // ACP 层
    "claude_acp":    {}, // Claude ACP rawInput 已经是 snake_case，defaultMappings 够用
    "opencode_acp":  {"oldString": "old_string", "newString": "new_string"},
    "codebuddy_acp": {},
    "gemini_acp":    {}, // Gemini ACP 无 rawInput，归一化在推断阶段完成
    "generic_acp": { // 当前 acp_events.go 内联的完整 remap 表，用于 generic fallback
        "oldString": "old_string", "newString": "new_string",
        "dirPath": "path", "filePath": "file_path",
        "cellIndex": "cell_index", "cellType": "cell_type",
    },
}

func getRemaps(key string) map[string]string {
    return perAgentInputRemaps[key]
}
```

> **注意**：`filePath→file_path` 已在 `defaultMappings` 中，per-agent remaps 无需重复。
> 但 `generic_acp` 保留完整映射表作为安全网，确保新智能体通过 generic 路径时也能正确归一化。

### 调用方式

```go
// gemini_tool.go (CLI)
func parseGeminiToolUse(msg *GeminiStreamMessage) *ToolCall {
    return &ToolCall{
        Name:  normalizeToolName(msg.Name),
        ID:    msg.ID,
        Input: string(normalizeToolInput(msg.Input, getRemaps("generic_acp"))),
        Done:  true,
    }
}

// acp_claude_tool.go (ACP) — rawInput 已是 snake_case，只走 defaultMappings
func parseClaudeACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    ...
    if tc.RawInput != nil {
        inputBytes, _ := json.Marshal(tc.RawInput)
        normalized, _ := normalizeToolInput(inputBytes, getRemaps("claude_acp"))
        tool.Input = string(normalized)
    }
    ...
}

// acp_tool.go generic fallback — 使用完整 remap 表
func parseGenericACPToolCall(tc acp.SessionUpdateToolCall) *ToolCall {
    ...
    if tc.RawInput != nil {
        inputBytes, _ := json.Marshal(tc.RawInput)
        normalized, _ := normalizeToolInput(inputBytes, getRemaps("generic_acp"))
        tool.Input = string(normalized)
    }
    ...
}
```

### 特殊处理

- **Pi 的 `edits` 嵌套归一化**（`oldText→old_string`, `newText→new_string`）保留在 `pi_tool.go`，因为它是 Pi 特有的嵌套结构，不是简单的顶层字段重命名
- **DeepSeek 的 per-tool remap 条件**（`edit_file` 用不同 remap 表）用 `getRemaps("deepseek_cli")` 统一覆盖，不再 switch-case
- **Claude ACP 的工具名提取** 优先从 `_meta.claudeCode.toolName` 读取，`extractToolName` 作为 fallback
- **CodeBuddy ACP 的工具名提取** 优先从 `_meta["codebuddy.ai/toolName"]` 读取，`extractToolName` 作为 fallback
- **Gemini ACP 的输入推断** 保留在 `acp_gemini_tool.go`，从 `locations` + `title` + `toolCallId` 前缀推断

## Debouncer 改造

当前 `toolCallDebouncer` 在 `handleToolCallUpdate` 中直接调用 `mapACPToolCallUpdate(tcu)`。
重构后需要传递 `backend` 参数。

```go
// acp_debounce.go

type toolCallDebouncer struct {
    ch     chan<- StreamEvent
    conn   *ACPConn
    backend string // 新增：从 conn.agent.Backend 获取
}

func newToolCallDebouncer(ch chan<- StreamEvent, conn *ACPConn) *toolCallDebouncer {
    backend := ""
    if conn != nil && conn.agent != nil {
        backend = conn.agent.Backend
    }
    return &toolCallDebouncer{ch: ch, conn: conn, backend: backend}
}

// handleToolCallUpdate 内部改为：
func (d *toolCallDebouncer) handleToolCallUpdate(tcu acp.SessionToolCallUpdate) {
    ...
    event := mapACPToolCallUpdate(d.backend, tcu)
    ...
}
```

## ParseLine / mapACPSessionUpdate 重构后的骨架

### StreamParser

```go
func (p *StreamParser) ParseLine(line string, ch chan<- StreamEvent) {
    // ... JSON 反序列化 ...
    switch msg.Type {
    case "stream_event":
        // thinking/text 直接发事件
        state := &ClaudeStreamToolState{
            ActiveTools:           p.activeTools,
            ActiveToolResults:     p.activeToolResults,
            ActiveThinkingBlocks:  p.activeThinkingBlocks,
            EmittedToolInputEmpty: p.emittedToolInputEmpty,
            ReceivedPartialToolUse: &p.receivedPartialToolUse,
            ReceivedPartial:        &p.receivedPartial,
            ReceivedPartialThinking: &p.receivedPartialThinking,
        }
        for _, ev := range parseClaudeStreamToolEvent(&msg, state) {
            ch <- ev
        }
    case "assistant":
        for _, ev := range parseClaudeAssistantToolUse(&msg, state) {
            ch <- ev
        }
    case "user":
        for _, ev := range parseClaudeUserToolResult(&msg) {
            ch <- ev
        }
    }
}
```

### GeminiStreamParser

```go
func (p *GeminiStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
    // ... JSON 反序列化 ...
    switch msg.Type {
    case "tool_use":
        if tc := parseGeminiToolUse(&msg); tc != nil {
            ch <- StreamEvent{Type: "tool_use", Tool: tc}
        }
    case "tool_result":
        if tc := parseGeminiToolResult(&msg); tc != nil {
            ch <- StreamEvent{Type: "tool_result", Tool: tc}
        }
    }
}
```

### acp_events.go

```go
// 签名增加 backend 参数（使用 agent.Backend，非 agent.ID）
func mapACPSessionUpdate(update acp.SessionUpdate, ch chan<- StreamEvent, ctx context.Context, conn *ACPConn, deb *toolCallDebouncer, backend string) {
    switch {
    case update.ToolCall != nil:
        if tc := parseACPToolCall(backend, *update.ToolCall); tc != nil {
            ch <- StreamEvent{Type: "tool_use", Tool: tc}
        }
    case update.ToolCallUpdate != nil:
        // debounce + parseACPToolCallUpdate(backend, tcu)
        ...
    }
}
```

### acp_client.go

```go
// 使用 agent.Backend（如 "claude", "gemini"）而非 agent.ID（用户自定义名称）
backend := ""
if c.connRef != nil && c.connRef.agent != nil {
    backend = c.connRef.agent.Backend
}
mapACPSessionUpdate(n.Update, ch, ctx, c.connRef, deb, backend)
```

## 测试策略

### 新增测试

每个 `xxx_tool.go` 的函数可以直接单元测试：

```go
// acp_claude_tool_test.go
func TestParseClaudeACPToolCall(t *testing.T) {
    tc := acp.SessionUpdateToolCall{
        Meta:      map[string]any{"claudeCode": map[string]any{"toolName": "Edit"}},
        ToolCallId: "call_function_xxx_1",
        Title:     "Edit",
        Kind:      acp.ToolKindEdit,
        RawInput:  map[string]any{"file_path": "/tmp/a.txt", "old_string": "foo", "new_string": "bar"},
    }
    result := parseClaudeACPToolCall(tc)
    assert.Equal(t, "Edit", result.Name)
    assert.Contains(t, result.Input, "file_path")
}

// acp_gemini_tool_test.go
func TestParseGeminiACPToolCall(t *testing.T) {
    tc := acp.SessionUpdateToolCall{
        ToolCallId: "read_file-1780629348181-1",
        Kind:       acp.ToolKindRead,
        Locations:  []acp.ToolCallLocation{{Path: "/home/.../README.md"}},
        Title:      "README.md",
    }
    result := parseGeminiACPToolCall(tc)
    assert.Equal(t, "Read", result.Name)
    assert.Contains(t, result.Input, "file_path")
}
```

### 测试迁移计划

| 现有测试文件 | 迁移策略 |
|------------|---------|
| `acp_test.go` 中 `mapACPToolCall` 的测试 | 移到 `acp_claude_tool_test.go`（带 `_meta`）+ `acp_tool_test.go`（generic fallback） |
| `acp_test.go` 中 `mapACPToolCallUpdate` 的测试 | 同上，按是否有 `_meta` 分别测试 |
| `acp_test.go` 中 `mapACPSessionUpdate` 的集成测试 | 保留在 `acp_test.go`（或 `acp_events_test.go`），更新签名增加 `backend` 参数 |
| `deepseek_stream_test.go` 中 `normalizeDeepSeekInput` 的测试 | 移到 `deepseek_tool_test.go` |
| `*_stream_test.go` 中工具调用解析的测试 | 移到对应的 `*_tool_test.go` |

> **辅助函数**：提供 `wrapToolCall(tc *ToolCall, eventType string) StreamEvent` 帮助迁移
> 现有基于 `StreamEvent.Type` 的断言。

### Orphan tool_call_update 处理

各 per-agent ACP 函数必须处理"孤儿"事件（tool_call_update 没有 preceding tool_call）。
例如 ACP 会话 resume 时，可能会收到已完成工具的 completed update。
函数应通过 `toolCallId` 创建 ToolCall 并设置 `Done=true`，不假设所有字段都存在。

## 数据流总览

```
CLI 模式
  cli_backend.go → LineParser.ParseLine
    ├── StreamParser.ParseLine → claude_tool.go 函数 → *ToolCall / []StreamEvent
    ├── GeminiStreamParser.ParseLine → gemini_tool.go 函数 → *ToolCall
    ├── OpenCodeStreamParser.ParseLine → opencode_tool.go 函数 → *ToolCall
    ├── CodexStreamParser.ParseLine → codex_tool.go 函数 → *ToolCall
    ├── DeepSeekStreamParser.ParseLine → deepseek_tool.go 函数 → *ToolCall
    ├── PiStreamParser.ParseLine → pi_tool.go 函数 → *ToolCall
    └── VeCLIStreamParser.ParseLine → (纯文本，无工具调用)
    → StreamEvent{Tool: *ToolCall} → AccumulateBlock → ContentBlock

ACP 模式
  acp_client.go → mapACPSessionUpdate(backend)
    └── acp_tool.go 路由分发（使用 agent.Backend）
        ├── "claude"/"qoder"  → acp_claude_tool.go → *ToolCall
        ├── "codebuddy"       → acp_codebuddy_tool.go → *ToolCall
        ├── "opencode"        → acp_opencode_tool.go → *ToolCall
        ├── "gemini"/"kimi"   → acp_gemini_tool.go → *ToolCall
        └── default           → acp_tool.go generic fallback → *ToolCall
    → StreamEvent{Tool: *ToolCall} → AccumulateBlock → ContentBlock

共享层
  common_stream.go
    ├── normalizeToolName (已有)
    ├── normalizeToolInput (已有，含 defaultMappings)
    ├── perAgentInputRemaps (新增)
    └── getRemaps (新增)
```

## 评审修正记录

基于 code-reviewer 评审，修正以下问题：

| 评审项 | 修正 |
|-------|------|
| C1: `_meta` 字段当前代码未读取 | 确认 `_meta` 在 ACP SDK（`Meta map[string]any`）和生产数据中存在。重构后优先从 `_meta` 读取工具名 |
| C2: 当前 ACP 代码是全通用的 | 保持按智能体路由设计，为未来差异预留扩展点 |
| C3: Debouncer 需要传递 backend | 新增 Debouncer 改造章节，`toolCallDebouncer` 增加 `backend` 字段 |
| I1: Kimi 缺失 | 路由中增加 `"kimi"` 路由到 `acp_gemini_tool.go` |
| I2: 测试迁移未详细规划 | 新增测试迁移计划章节 |
| I3: parseClaudeStreamToolEvent 非纯函数 | 引入 `ClaudeStreamToolState` 结构体，显式声明状态依赖 |
| I4: cellIndex/cellType 遗漏 | 在 `generic_acp` remap 中补充 |
| I5: agent.ID vs agent.Backend | 路由改用 `agent.Backend`，文档明确说明 |
| S1: ClaudeAssistantMessage/ClaudeUserMessage 不存在 | 修正为使用 `ClaudeStreamMessage` |
| S3: Copilot/Cline 未在修改列表 | 新增备注说明它们共享 claude_tool.go |
| S4: Resume 场景未处理 | 新增 Orphan 事件处理说明 |
| S5: defaultMappings 重复 | per-agent remaps 只包含特有覆盖项，通用映射由 defaultMappings 处理 |
