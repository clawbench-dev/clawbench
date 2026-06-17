# AI 后端插件化重构方案

> 日期：2026-06-16（v2 — 评审修订版）
> 状态：**实施完成**（阶段 0-8 + 10 已完成，阶段 9 跳过）

## 1. 背景与动机

移除 Gemini 后端时，尽管 Gemini 仅占 `internal/ai/gemini*.go` 几个文件，却影响了 **46 个文件** ——factory、discovery、common_stream、ACP 工具名称、前端 provider 列表、设置字段、i18n 等。这暴露了当前架构的核心问题：**后端实现分散在多个共享模块中，缺乏内聚性**。

### 当前痛点

| 问题 | 示例 |
|------|------|
| 新增/移除后端需修改多处 | factory switch、discovery 全局数组、common_stream remaps、前端硬编码列表 |
| 共享代码与后端特定代码混在一起 | `normalizeToolName()` 包含所有后端的别名；`perAgentInputRemaps` 是全局 map |
| 后端无法独立测试 | 所有后端在同一个 package 中，测试互相影响 |
| 后端特定逻辑泄漏到共享骨架 | `injectAgentAPIKey()` 在 CLIBackend 中硬编码 `agent.Backend != "pi"` |
| 自定义后端无扩展点 | Codex 直接实现 AIBackend；VeCLI 包装 CLIBackend 重写 ExecuteStream — 均无注册路径 |

## 2. 目标

1. **高内聚**：一个后端的特定代码（CLI 参数构造、工具映射、模型发现、ACP 映射数据）位于一个子包中
2. **低耦合**：新增后端只需添加子包 + 在 `all.go` 注册，不修改框架代码
3. **可独立测试**：每个后端子包可独立运行单元测试
4. **支持多种后端模式**：CLIBackend 骨架、自定义 AIBackend、包装型后端均可注册
5. **渐进迁移**：一次迁移一个后端，每步可编译可测试

## 3. 目标目录结构

```
internal/ai/
├── interface.go              # AIBackend 接口（不变）
├── cli_backend.go            # CLIBackend 通用骨架（不变，移除 injectAgentAPIKey 中的 Pi 硬编码）
├── auto_resume.go            # AutoResumeBackend 装饰器（不变）
├── acp_backend.go            # ACPBackend 通用骨架（不变）
├── acp_*.go                  # ACP 通用基础设施（不变，含 acp_events.go 整体保留）
├── stream_parser.go          # StreamParser 通用接口（不变）
├── stream_json_parser.go     # StreamJSONParser 共享解析器（Kimi/Cline/Copilot/Qoder/Codebuddy 共用）
├── opencode_stream.go        # OpenCodeStreamParser 共享解析器（OpenCode/MiMo 共用）
├── common_stream.go          # 通用工具函数（normalizeToolName 保留为全局 fallback，per-agent 映射表迁移到子包）
├── accumulate.go             # 块聚合（不变）
├── block_helpers.go          # 块辅助（不变）
├── factory.go                # 简化：从注册表查找，支持 CLI/Custom 两种路径
├── orphan.go                 # 孤儿进程清理（不变）
├── backends/
│   ├── all.go                # 集中式注册入口，import 触发 init()
│   ├── plugin.go             # BackendPlugin 接口定义
│   ├── registry.go           # 全局注册表 + Register/Lookup/ResetForTest 函数
│   ├── claude/
│   │   ├── cli.go            # buildArgs, newParser, filterLine → CLIBackend 配置
│   │   ├── stream.go         # ClaudeStreamParser（从 claude_stream.go 迁移）
│   │   ├── stream_test.go
│   │   ├── cli_test.go
│   │   └── discovery.go      # DiscoverClaudeModels（从 model/discovery.go 迁移）
│   ├── codebuddy/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   ├── stream_test.go
│   │   └── discovery.go
│   ├── opencode/
│   │   ├── cli.go            # 使用共享 OpenCodeStreamParser
│   │   ├── tool.go           # OpenCode 工具名映射
│   │   └── discovery.go
│   ├── codex/
│   │   ├── custom.go         # 自定义后端：直接实现 AIBackend（Codex 不用 CLIBackend 骨架）
│   │   ├── stream.go         # CodexStreamParser + CodexStreamMessage 等类型
│   │   ├── think.go          # Codex thinking 解析
│   │   ├── tool.go           # Codex 工具名映射
│   │   ├── stream_test.go
│   │   └── discovery.go
│   ├── qoder/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── vecli/
│   │   ├── custom.go         # 包装型后端：VeCLIBackend 包装 CLIBackend + 后处理
│   │   ├── stream.go         # VeCLIStreamParser + SessionSummary 解析
│   │   └── discovery.go
│   ├── deepseek/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   ├── tool.go
│   │   └── discovery.go
│   ├── pi/
│   │   ├── cli.go            # 含 PreExecHook（API key 注入逻辑）
│   │   ├── stream.go
│   │   ├── tool.go
│   │   └── discovery.go
│   ├── cline/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── kimi/
│   │   ├── cli.go
│   │   ├── acp.go            # ACP 映射数据（ToolCallIDPrefixes + InputRemaps）
│   │   ├── stream.go         # 使用共享 StreamJSONParser
│   │   └── discovery.go
│   ├── copilot/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   └── discovery.go
│   └── mimo/
│       ├── cli.go            # 使用共享 OpenCodeStreamParser
│       └── discovery.go
internal/model/
├── discovery.go               # 保留 BackendSpec 结构 + 通用发现逻辑，移除各后端的 Discover* 函数
└── provider_registry.go       # 保留（provider 级别的注册，与后端插件独立）
```

### 与 v1 的关键差异

| 变更 | 原因 |
|------|------|
| Stream parser 保留在 `internal/ai/` | `StreamJSONParser` 被 5 个后端共用，`OpenCodeStreamParser` 被 2 个后端共用——不属于任何单一后端 |
| ACP 子包只含映射数据，不含 `ProcessEvent` | `acp_events.go` 778 行代码是后端无关的，零个 per-backend 分支，不应拆分 |
| `codex/custom.go`、`vecli/custom.go` | Codex 直接实现 AIBackend，VeCLI 包装 CLIBackend——都不是简单 CLIBackend |
| `pi/cli.go` 含 PreExecHook | `injectAgentAPIKey` 中 `agent.Backend != "pi"` 是后端特定逻辑泄漏 |
| mimo 无 `acp.go`、`stream.go` | MiMo 使用共享 OpenCodeStreamParser，无需独立文件 |

## 4. 核心接口设计

### 4.1 BackendPlugin 接口

```go
// backends/plugin.go
package backends

import (
    "clawbench/internal/ai"
    "clawbench/internal/model"
)

// BackendPlugin 是一个 AI 后端的完整自描述注册单元。
// 每个后端子包通过 init() 调用 Register() 将自己注册到全局注册表。
type BackendPlugin struct {
    // ID 是后端唯一标识，如 "claude"、"kimi"。对应 Agent.Backend 字段。
    ID string

    // Spec 描述后端的自动发现配置（命令检测、模型发现、ACP 支持、thinking levels）。
    // 框架将其收集到 model.BackendRegistry 供启动时使用。
    Spec model.BackendSpec

    // CLI 是 CLI 模式的配置。nil 表示该后端不支持 CLI。
    // 适用于使用 CLIBackend 骨架的后端（大多数后端）。
    CLI *CLIPlugin

    // Custom 是自定义后端工厂。nil 表示使用标准 CLI 或 ACP 路径。
    // 适用于直接实现 AIBackend 或包装 CLIBackend 的后端（如 Codex、VeCLI）。
    // 当 Custom 非空时，Factory 优先使用 Custom.NewBackend，忽略 CLI 字段。
    Custom *CustomPlugin

    // ACP 是 ACP 模式的映射数据。nil 表示该后端不支持 ACP。
    // ACP 事件处理逻辑保留在 internal/ai/ 作为共享基础设施，
    // 子包只注册映射数据（工具名、输入字段重映射）。
    ACP *ACPPlugin

    // NeedsAutoResume 为 true 时，CLI 模式自动包装 AutoResumeBackend。
    NeedsAutoResume bool
}

// CLIPlugin 提供 CLI 模式（基于 CLIBackend 骨架）的后端配置。
type CLIPlugin struct {
    // NewBackend 返回一个 CLIBackend 实例（已配置 buildArgs/newParser/filterLine/preStart）。
    NewBackend func() *ai.CLIBackend

    // ToolNameMap 是该后端的工具名归一化映射表（完整表，非增量）。
    // key: 后端原始工具名 → value: 规范名（如 "read_file" → "Read"）
    //
    // Stream parser 在构造时获取此映射表，通过闭包注入到 ParseLine 中，
    // 不再依赖全局 normalizeToolName() 的 switch/case。
    ToolNameMap map[string]string

    // InputRemaps 是该后端 CLI 模式的工具输入字段重映射表（完整表，非增量）。
    // key: 原始字段名 → value: 目标字段名（如 "filePath" → "file_path"）
    //
    // Stream parser 在构造时获取此映射表，直接传给 normalizeToolInput()，
    // 不再通过 getRemaps("kimi_cli") 全局查找。
    InputRemaps map[string]string

    // PreExecHook 在 CLI 子进程启动前调用，用于后端特定的环境注入。
    // 例如 Pi 的 API key 注入逻辑（从 injectAgentAPIKey 迁移）。
    // nil 表示不需要额外注入。
    PreExecHook func(cmd *exec.Cmd, req ai.ChatRequest)
}

// CustomPlugin 提供自定义后端工厂，用于不使用 CLIBackend 骨架的后端。
type CustomPlugin struct {
    // NewBackend 返回一个自定义 AIBackend 实例。
    // 适用场景：
    //   - Codex: 直接实现 AIBackend，有独立的 ExecuteStream
    //   - VeCLI: 包装 CLIBackend + 后处理（session-summary 解析）
    //   - 未来: 完全自定义的后端
    NewBackend func() ai.AIBackend
}

// ACPPlugin 提供 ACP 模式的映射数据。
// ACP 事件处理（mapACPSessionUpdate、mapACPToolCall 等）保留在 internal/ai/
// 作为共享基础设施。子包只注册后端特定的映射表，
// 共享事件处理代码在运行时从注册表查询这些映射表。
type ACPPlugin struct {
    // ToolCallIDPrefixes 是该后端 ACP toolCallID 前缀到规范名的映射。
    // 如 Kimi: "read_file" → "Read"
    // 合并到共享的 extractToolName() 中，替代当前硬编码的 acpToolCallIDPrefix。
    ToolCallIDPrefixes map[string]string

    // InputRemaps 是该后端 ACP 模式的工具输入字段重映射表。
    // 替代当前 perAgentInputRemaps 中 "{backend}_acp" 条目。
    InputRemaps map[string]string
}
```

### 4.2 注册表

```go
// backends/registry.go
package backends

import (
    "fmt"
    "sync"

    "clawbench/internal/model"
)

var (
    plugins   = make(map[string]*BackendPlugin)
    pluginsMu sync.RWMutex
)

// Register 将后端插件注册到全局注册表。
// 通常在子包的 init() 函数中调用。
// 重复注册会 panic（编程错误）。
func Register(p *BackendPlugin) {
    pluginsMu.Lock()
    defer pluginsMu.Unlock()
    if _, exists := plugins[p.ID]; exists {
        panic(fmt.Sprintf("backend plugin already registered: %s", p.ID))
    }
    plugins[p.ID] = p
}

// Lookup 返回指定 ID 的后端插件，不存在返回 nil。
func Lookup(id string) *BackendPlugin {
    pluginsMu.RLock()
    defer pluginsMu.RUnlock()
    return plugins[id]
}

// All 返回所有已注册的后端插件。
func All() []*BackendPlugin {
    pluginsMu.RLock()
    defer pluginsMu.RUnlock()
    result := make([]*BackendPlugin, 0, len(plugins))
    for _, p := range plugins {
        result = append(result, p)
    }
    return result
}

// AllSpecs 返回所有已注册后端的 BackendSpec，用于填充 model.BackendRegistry。
func AllSpecs() []model.BackendSpec {
    pluginsMu.RLock()
    defer pluginsMu.RUnlock()
    specs := make([]model.BackendSpec, 0, len(plugins))
    for _, p := range plugins {
        specs = append(specs, p.Spec)
    }
    return specs
}

// ResetForTest 清空注册表，仅用于测试。
// 允许测试隔离地注册/注销后端，避免 init() 全局副作用。
func ResetForTest() {
    pluginsMu.Lock()
    defer pluginsMu.Unlock()
    plugins = make(map[string]*BackendPlugin)
}

// LookupACPRemaps 查找指定后端的 ACP 输入重映射表。
// 如果后端没有注册 ACP remaps，返回 generic_acp fallback。
// 供共享 ACP 事件处理代码调用。
func LookupACPRemaps(backendID string) map[string]string {
    pluginsMu.RLock()
    defer pluginsMu.RUnlock()
    if p, ok := plugins[backendID]; ok && p.ACP != nil && p.ACP.InputRemaps != nil {
        return p.ACP.InputRemaps
    }
    return genericACPRemaps
}

// LookupACPToolCallIDPrefixes 查找指定后端的 ACP toolCallID 前缀映射。
// 供共享 extractToolName() 调用。
func LookupACPToolCallIDPrefixes(backendID string) map[string]string {
    pluginsMu.RLock()
    defer pluginsMu.RUnlock()
    if p, ok := plugins[backendID]; ok && p.ACP != nil {
        return p.ACP.ToolCallIDPrefixes
    }
    return nil
}

// genericACPRemaps 是 ACP 输入归一化的 fallback 映射表。
// 从当前 common_stream.go 中的 "generic_acp" 条目迁移。
var genericACPRemaps = map[string]string{
    "oldString": "old_string", "newString": "new_string",
    "dirPath": "path", "filePath": "file_path",
    "cellIndex": "cell_index", "cellType": "cell_type",
}
```

### 4.3 集中式注册入口

```go
// backends/all.go
package backends

import (
    _ "clawbench/internal/ai/backends/claude"
    _ "clawbench/internal/ai/backends/codebuddy"
    _ "clawbench/internal/ai/backends/opencode"
    _ "clawbench/internal/ai/backends/codex"
    _ "clawbench/internal/ai/backends/qoder"
    _ "clawbench/internal/ai/backends/vecli"
    _ "clawbench/internal/ai/backends/deepseek"
    _ "clawbench/internal/ai/backends/pi"
    _ "clawbench/internal/ai/backends/cline"
    _ "clawbench/internal/ai/backends/kimi"
    _ "clawbench/internal/ai/backends/copilot"
    _ "clawbench/internal/ai/backends/mimo"
)
```

### 4.4 简化后的 Factory

```go
// factory.go（重构后）
package ai

import (
    "fmt"
    "log/slog"

    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func NewBackend(backendType string) (AIBackend, error) {
    p := backends.Lookup(backendType)
    if p == nil {
        return nil, fmt.Errorf("unsupported backend type: %s", backendType)
    }

    var backend AIBackend

    // 优先使用 Custom 后端（Codex、VeCLI 等自定义实现）
    if p.Custom != nil {
        backend = p.Custom.NewBackend()
    } else if p.CLI != nil {
        backend = p.CLI.NewBackend()
    } else {
        return nil, fmt.Errorf("backend %s has no CLI or Custom support", backendType)
    }

    // AutoResume 包装（仅 CLI 模式）
    if p.NeedsAutoResume && p.Custom == nil {
        backend = &AutoResumeBackend{inner: backend}
    }

    return backend, nil
}

func NewBackendForAgentWithTransport(backendType, agentID, transportOverride string) (AIBackend, error) {
    if agentID != "" {
        if agent, ok := model.Agents[agentID]; ok {
            effectiveTransport := transportOverride
            if effectiveTransport == "" {
                effectiveTransport = agent.Transport
            }
            if effectiveTransport == "acp-stdio" {
                if agent.SupportsACP() {
                    p := backends.Lookup(backendType)
                    if p != nil && p.ACP != nil {
                        return NewACPBackend(agent)
                    }
                }
                slog.Warn("agent does not support acp-stdio transport, falling back to CLI", "agentID", agentID)
            }
        }
    }
    return NewBackend(backendType)
}
```

### 4.5 工具名归一化的分发机制

当前 `normalizeToolName()` 是一个巨大的 switch，包含所有后端的别名。重构后：

1. **每个子包定义完整的 ToolNameMap**（闭包注入到 parser）
2. **共享 parser 在构造时接受 ToolNameMap**，ParseLine 中使用闭包内的 map 替代全局 switch
3. **`normalizeToolName()` 保留为全局 fallback**——处理未被子包覆盖的通用别名，逐步瘦身

```go
// 子包中的 parser 构造（以 Kimi 为例）
func newParser(toolNameMap map[string]string) ai.StreamParser {
    return &ai.StreamJSONParser{
        ToolNameMap: toolNameMap, // 闭包注入
    }
}

// StreamJSONParser 中使用注入的 map
func (p *StreamJSONParser) ParseLine(line string) []StreamEvent {
    // ...
    if canonical, ok := p.ToolNameMap[toolName]; ok {
        // 使用子包映射表
    } else {
        // fallback 到全局 normalizeToolName()
    }
}
```

### 4.6 CLIBackend PreExecHook 集成

当前 `injectAgentAPIKey()` 硬编码 `agent.Backend != "pi"`。重构后：

1. **CLIBackend 增加可选的 PreExecHook 字段**
2. **Pi 子包注册 PreExecHook**，包含 API key 注入逻辑
3. **其他后端的 PreExecHook 为 nil**，跳过注入

```go
// CLIBackend 中的改动（cli_backend.go）
type CLIBackend struct {
    // ... 现有字段 ...
    preExecHook func(cmd *exec.Cmd, req ChatRequest) // 新增：后端特定环境注入
}

// ExecuteStream 中替换 injectAgentAPIKey 调用
if b.preExecHook != nil {
    b.preExecHook(cmd, req)
}

// Pi 子包注册
CLI: &backends.CLIPlugin{
    NewBackend:   newCLIBackend,
    ToolNameMap:  piToolNameMap,
    InputRemaps:  piInputRemaps,
    PreExecHook:  injectPiAPIKey, // 从 injectAgentAPIKey 迁移的 Pi 特定逻辑
},
```

### 4.7 ACP 映射数据的运行时查询

ACP 事件处理代码保留在 `internal/ai/`，运行时从注册表查询映射数据：

```go
// acp_events.go 中的改动
func mapACPToolCall(agent *model.Agent, msg json.RawMessage) []StreamEvent {
    // 查询注册表获取该后端的 toolCallID 前缀映射
    prefixes := backends.LookupACPToolCallIDPrefixes(agent.Backend)
    // 查询注册表获取该后端的 ACP 输入重映射
    remaps := backends.LookupACPRemaps(agent.Backend)
    // ... 其余逻辑不变 ...
}
```

## 5. 后端子包示例

### 5.1 Kimi（CLIBackend + ACP 映射数据）

```go
// backends/kimi/cli.go
package kimi

import (
    "clawbench/internal/ai"
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func init() {
    backends.Register(&backends.BackendPlugin{
        ID:  "kimi",
        Spec: model.BackendSpec{
            ID:                   "kimi",
            Backend:              "kimi",
            DefaultCmd:           "kimi",
            Name:                 "Kimi",
            Icon:                 "🌙",
            Specialty:            "Kimi AI 编码助手",
            DiscoverModelsFunc:   DiscoverKimiModels,
            ThinkingEffortLevels: []string{"off", "on"},
            AcpCommand:           "kimi acp",
        },
        CLI: &backends.CLIPlugin{
            NewBackend:   newCLIBackend,
            ToolNameMap:  kimiToolNameMap,
            InputRemaps:  kimiInputRemaps,
        },
        ACP: &backends.ACPPlugin{
            ToolCallIDPrefixes: kimiToolCallIDPrefixes,
            InputRemaps:       kimiACPInputRemaps,
        },
        NeedsAutoResume: true,
    })
}

var kimiToolNameMap = map[string]string{
    "read_file":          "Read",
    "write_file":         "Write",
    "edit_file":          "Edit",
    "replace":            "Edit",
    "run_shell_command":  "Bash",
    "list_directory":     "LS",
    "search_file":        "Grep",
    "search_directory":   "Grep",
    "glob":               "Glob",
    "ask":                "AskUserQuestion",
}

var kimiInputRemaps = map[string]string{
    "filePath": "file_path",
    "cmd":      "command",
    "exec":     "command",
    "dirPath":  "path",
    "dir_path": "path",
    "allow_multiple":  "replace_all",
    "is_background":   "run_in_background",
    "include_pattern": "glob",
    "name":            "skill",
}

func newCLIBackend() *ai.CLIBackend {
    return &ai.CLIBackend{
        BuildArgs:  buildArgs,
        NewParser:  func() ai.LineParser { return newParser(kimiToolNameMap) },
        FilterLine: filterLine,
    }
}
```

```go
// backends/kimi/acp.go
package kimi

var kimiToolCallIDPrefixes = map[string]string{
    "read_file":         "Read",
    "list_directory":    "LS",
    "glob":              "Glob",
    "run_shell_command": "Bash",
    "ask":               "AskUserQuestion",
    "write_file":        "Write",
    "edit_file":         "Edit",
    "replace":           "Edit",
    "search_file":       "Grep",
    "search_directory":  "Grep",
}

var kimiACPInputRemaps = map[string]string{} // Kimi ACP 无需输入重映射
```

### 5.2 Codex（自定义后端）

```go
// backends/codex/custom.go
package codex

import (
    "clawbench/internal/ai"
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func init() {
    backends.Register(&backends.BackendPlugin{
        ID:  "codex",
        Spec: model.BackendSpec{
            ID:                 "codex",
            Backend:            "codex",
            DefaultCmd:         "codex",
            Name:               "Codex",
            Icon:               "🦅",
            Specialty:          "OpenAI Codex CLI",
            DiscoverModelsFunc: DiscoverCodexModels,
        },
        Custom: &backends.CustomPlugin{
            NewBackend: func() ai.AIBackend { return &CodexBackend{} },
        },
        // 不设 CLI — Codex 直接实现 AIBackend
        // 不设 NeedsAutoResume — Codex 没有 ExitPlanMode
    })
}
```

### 5.3 VeCLI（包装型后端）

```go
// backends/vecli/custom.go
package vecli

import (
    "clawbench/internal/ai"
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func init() {
    backends.Register(&backends.BackendPlugin{
        ID:  "vecli",
        Spec: model.BackendSpec{
            ID:                 "vecli",
            Backend:            "vecli",
            DefaultCmd:         "vecli",
            Name:               "VeCLI",
            Icon:               "⚡",
            Specialty:          "VeCLI AI 编码助手",
            DiscoverModelsFunc: DiscoverVeCLIModels,
        },
        Custom: &backends.CustomPlugin{
            NewBackend: func() ai.AIBackend { return NewVeCLIBackend() },
        },
    })
}

// NewVeCLIBackend 创建 VeCLIBackend（包装 CLIBackend + session-summary 后处理）
// 内部创建 CLIBackend 实例并包装，与当前实现相同
func NewVeCLIBackend() *VeCLIBackend { /* ... */ }
```

### 5.4 Pi（CLIBackend + PreExecHook）

```go
// backends/pi/cli.go
package pi

import (
    "clawbench/internal/ai"
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func init() {
    backends.Register(&backends.BackendPlugin{
        ID:  "pi",
        Spec: model.BackendSpec{
            ID:                 "pi",
            Backend:            "pi",
            DefaultCmd:         "pi",
            Name:               "Pi",
            Icon:               "🥧",
            Specialty:          "Pi AI 编码助手",
            DiscoverModelsFunc: DiscoverPiModels,
        },
        CLI: &backends.CLIPlugin{
            NewBackend:   newCLIBackend,
            ToolNameMap:  piToolNameMap,
            InputRemaps:  piInputRemaps,
            PreExecHook:  injectPiAPIKey, // 从 injectAgentAPIKey 迁移
        },
    })
}

// injectPiAPIKey 从 injectAgentAPIKey 中提取的 Pi 特定逻辑
func injectPiAPIKey(cmd *exec.Cmd, req ai.ChatRequest) {
    // ... (从当前 cli_backend.go:301-339 迁移，移除 agent.Backend != "pi" 判断)
}
```

## 6. 迁移策略

分阶段执行，每阶段可编译可测试。

### 阶段 0：搭建框架（~1h）

1. 创建 `internal/ai/backends/` 目录
2. 实现 `plugin.go`（接口定义，含 CLIPlugin/CustomPlugin/ACPPlugin）、`registry.go`（注册表 + 查询函数）、`all.go`（空导入占位）
3. 在 `StreamJSONParser` 和 `OpenCodeStreamParser` 上添加 `ToolNameMap` 字段（可选，向后兼容）
4. 在 `CLIBackend` 上添加 `preExecHook` 字段
5. **验证**：`go build ./...` 通过，现有测试不受影响

### 阶段 1：迁移第一个后端 — Pi（最简单，CLI + PreExecHook）（~2h）

1. 创建 `backends/pi/` 子包
2. 迁移 `pi.go` + `pi_stream.go` + `pi_tool.go` + 测试文件
3. 迁移 `injectAgentAPIKey` 中 Pi 特定逻辑到 `pi/cli.go` 的 `injectPiAPIKey`
4. 迁移 `model/discovery.go` 中 `DiscoverPiModels` + `ParsePiModels` + 相关正则/常量
5. 在 `all.go` 中添加 `_ "clawbench/internal/ai/backends/pi"` 导入
6. 从 `factory.go` 移除 Pi 的 switch case，改用注册表查找
7. 从 `model.BackendRegistry` 移除 Pi 的硬编码 entry
8. **验证**：`go test ./internal/ai/... ./internal/model/...` 全部通过

### 阶段 2：迁移简单 CLI 后端（DeepSeek）（~2h）

步骤同阶段 1，含 stream parser 和 tool mapping 迁移。

### 阶段 3：迁移 VeCLI（包装型后端）（~2h）

1. 创建 `backends/vecli/` 子包
2. 迁移 `vecli.go`（VeCLIBackend 包装逻辑）+ `vecli_stream.go` + 测试文件
3. 使用 `CustomPlugin` 注册，而非 `CLIPlugin`
4. **验证**：VeCLI 的 session-summary 后处理逻辑不受影响

### 阶段 4：迁移 AutoResume CLI 后端（Claude, Codebuddy, Qoder, Cline, Kimi, Copilot）（~4h）

1. 迁移 CLI 配置 + stream parser 配置
2. 迁移 `common_stream.go` 中该后端的 `perAgentInputRemaps` 条目到子包 `InputRemaps`
3. 迁移 `normalizeToolName()` 中该后端的别名到子包 `ToolNameMap`
4. **关键**：`buildBaseStreamArgs` 保留在 `common_stream.go` 作为共享 helper，子包调用它
5. **关键**：共享 parser（StreamJSONParser）通过 ToolNameMap 闭包注入工具名映射

### 阶段 5：迁移 MiMo-Code（使用共享 OpenCodeStreamParser）（~1h）

1. 创建 `backends/mimo/` 子包
2. MiMo 使用共享 `OpenCodeStreamParser`，不需要独立 stream parser
3. 迁移 `mimo.go` 中的 `buildMimoStreamArgs` 和 `filterLine`
4. **注意**：MiMo 不依赖 `backends/opencode` 子包——直接使用 `internal/ai` 中的共享 parser

### 阶段 6：迁移 Codex（自定义后端）（~3h）

1. 创建 `backends/codex/` 子包
2. 迁移 `codex.go` + `codex_stream.go` + `codex_think.go` + `codex_tool.go` + 测试文件
3. 使用 `CustomPlugin` 注册——Codex 直接实现 AIBackend，不使用 CLIBackend 骨架
4. 迁移 `model/discovery.go` 中 Codex 的多策略模型发现（二进制 strings 扫描 + CLI 列表）

### 阶段 7：迁移 OpenCode（独立 stream parser + 工具映射）（~2h）

1. 创建 `backends/opencode/` 子包
2. 迁移 CLI 配置（使用共享 `OpenCodeStreamParser`）
3. 迁移 `opencode_tool.go` 中的工具名映射到 `ToolNameMap`
4. `OpenCodeStreamParser` 保留在 `internal/ai/`（MiMo 也使用）

### 阶段 8：迁移 ACP 映射数据（~2h）

1. 各后端子包添加 `acp.go`，注册 `ACPPlugin`（ToolCallIDPrefixes + InputRemaps）
2. 修改 `acp_events.go` 中的 `extractToolName` 和 `mapACPToolCall`，从注册表查询映射数据
3. 将 `acpToolCallIDPrefix` 和 `perAgentInputRemaps` 中的 `_acp` 条目迁移到子包
4. **保留** `generic_acp` 作为 fallback（在 `registry.go` 中）
5. **验证**：ACP 集成测试全部通过

### 阶段 9：前端 API 驱动（独立，非阻塞）（~4h+）

> **注意**：此阶段与后端重构解耦，可独立推进。后端重构的成功不依赖于此阶段。

1. 新增 `GET /api/backends` 端点，返回后端列表 + 设置 schema
2. 前端 `useSetup.ts` 的 provider 列表从 API 加载而非硬编码
3. 前端 `settingsFieldMap.ts` 的 summarize backend 选项从 API 获取
4. i18n key 动态化（需要独立的前端 API schema 设计）

### 阶段 10：清理（~1h）

1. 移除 `factory.go` 中的 `needsAutoResume()` 函数
2. 移除 `common_stream.go` 中的 `perAgentInputRemaps` 和 `getRemaps`
3. 移除 `common_stream.go` 中 `normalizeToolName` 里已迁移到子包的 case 分支（保留未覆盖的通用别名）
4. 移除 `model/discovery.go` 中已迁移的 Discover* 函数
5. 移除 `cli_backend.go` 中的 `injectAgentAPIKey` 函数
6. 更新 `docs/spec/core/ai-backend.md`

## 7. 关键设计决策记录

| # | 决策 | 选项 | 理由 |
|---|------|------|------|
| 1 | 插件化注册 | A) Plugin 注册 vs B) 声明式配置 | 代码即配置，类型安全，IDE 可发现 |
| 2 | 子包粒度 | CLI/ACP 分文件 vs 单文件 | CLI 和 ACP 数据差异大，分离更清晰；但同一 ID 统一注册，避免碎片 |
| 3 | 注册接口 | 一个大 struct vs 多个小接口 | 后端概念本身就是统一的，拆分接口增加理解成本 |
| 4 | ACP 事件处理 | **数据注册 vs 完全多态** | `acp_events.go` 778 行代码零个 per-backend 分支——拆分会制造重复。子包只注册映射数据，事件处理保留为共享基础设施 |
| 5 | 工具名映射 | 完整映射表 vs 共享默认+覆盖 | 完整表自包含，无隐式依赖，新增后端无需理解全局默认 |
| 6 | 模型发现注册 | Register() 返回 BackendSpec vs 独立注册 | 后端和发现逻辑天然一对，放一起减少心智负担 |
| 7 | CLI/ACP/Custom 三路径 | Custom 字段支持完全自定义后端 | Codex 直接实现 AIBackend、VeCLI 包装 CLIBackend——单一 CLIPlugin 路径无法表达 |
| 8 | 前端 | API 驱动 vs 硬编码 | 后端列表动态变化，前端不应硬编码（但独立于后端重构，不阻塞） |
| 9 | 注册时机 | 集中式 all.go + init() vs 分散注册 | all.go 是唯一注册入口，import 列表一目了然 |
| 10 | 共享 Parser 保留在框架层 | StreamJSONParser/OpenCodeStreamParser 不迁移 | 多后端共用，不属于任何单一后端子包 |
| 11 | 工具名归一化分发 | 闭包注入 ToolNameMap + 全局 fallback | Parser 构造时获取映射表，未覆盖的别名走全局 normalizeToolName() |
| 12 | PreExecHook 机制 | CLIPlugin 增加可选 hook | 消除 injectAgentAPIKey 中的后端特定硬编码 |

### 决策 #4 变更说明（v1 → v2）

v1 选择"完全多态"——每个后端子包实现完整 `ProcessEvent`。评审发现 `acp_events.go` 是后端无关的统一实现，拆分到 12 个子包会：
- 制造 ~12 × 60 行重复代码
- 丢失集中测试覆盖
- 增加维护负担

v2 改为"数据注册"——子包只注册映射数据（ToolCallIDPrefixes + InputRemaps），共享事件处理代码从注册表查询。这保留了 `acp_events.go` 的完整性和测试覆盖，同时实现了"新增后端不修改共享代码"的目标。

## 8. 风险与缓解

| 风险 | 缓解措施 |
|------|----------|
| 循环导入 | 子包只依赖 `internal/ai`（接口+骨架+共享 parser）和 `internal/model`，反向不依赖。MiMo 不依赖 `backends/opencode`——通过 `internal/ai` 中的共享 parser 间接复用 |
| 迁移过程中测试覆盖下降 | 每阶段强制运行全量测试，迁移测试代码与实现代码同步 |
| StreamJSONParser/OpenCodeStreamParser 共享问题 | 保留在 `internal/ai/` 作为通用组件，子包通过构造参数配置 |
| init() 注册影响测试隔离 | 提供 `ResetForTest()` 清空注册表，测试可独立控制注册哪些后端 |
| `normalizeToolName` 渐进瘦身 | 子包映射表优先匹配，全局函数作为 fallback。每迁移一个后端，删除对应 switch case |
| `generic_acp` fallback 丢失 | 保留在 `registry.go` 中作为默认 ACP remaps，所有未注册 ACP 映射的后端使用此 fallback |
| PreExecHook 引入新的调用时机 | Hook 在 `cmd.Start()` 前调用，与当前 `injectAgentAPIKey` 调用时机一致。nil hook 为 no-op，无性能影响 |
| 自定义后端与 AutoResume 的交互 | Custom 后端自行决定是否需要 AutoResume。当前 Codex/VeCLI 不需要，未来自定义后端可在 NewBackend 中自行包装 |

## 9. 不变的部分

以下组件**不迁移**到子包，保留在 `internal/ai/` 作为框架层：

- `AIBackend` 接口
- `CLIBackend` 通用骨架（进程管理、stdout 管道、上下文取消）
- `AutoResumeBackend` 装饰器
- `ACPBackend` 通用骨架（连接管理、debounce、crash diagnostics）
- `acp_events.go` 整体——ACP 事件处理是后端无关的共享基础设施
- `acp_tool_names.go` 中 `extractToolName` 核心逻辑（映射表数据从注册表查询，匹配算法保留）
- `StreamParser` / `StreamJSONParser` / `OpenCodeStreamParser` 通用解析器
- `buildBaseStreamArgs` 共享参数构造 helper
- `normalizeToolName` / `normalizeToolInput` 通用归一化函数（核心逻辑保留，per-agent 映射表迁移到子包）
- `generic_acp` fallback 映射
- 孤儿进程清理逻辑

## 10. 评审记录

### 评审结果（2026-06-16）

**评审人**：superpowers:code-reviewer

#### Critical 问题（已修复）

| # | 问题 | 修复 |
|---|------|------|
| C1 | CodexBackend 直接实现 AIBackend，设计缺少自定义后端路径 | 新增 `CustomPlugin`，Factory 优先检查 Custom |
| C2 | VeCLIBackend 包装 CLIBackend 重写 ExecuteStream，CLIPlugin 无法表达 | VeCLI 使用 `CustomPlugin` 注册 |
| C3 | ACP ProcessEvent 完全多态不现实——778 行代码零个 per-backend 分支 | 改为数据注册模式：子包注册映射数据，事件处理保留为共享基础设施 |

#### Important 问题（已修复）

| # | 问题 | 修复 |
|---|------|------|
| I1 | MiMo 使用 OpenCodeStreamParser，跨子包依赖 | `OpenCodeStreamParser` 保留在 `internal/ai/` 作为共享组件 |
| I2 | perAgentInputRemaps key 格式（`agent_mode`）迁移后分发机制不明 | ToolNameMap/InputRemaps 通过闭包注入到 parser 构造时 |
| I3 | normalizeToolName 被 CLI parser 和 ACP 共用，拆分非平凡 | 闭包注入 + 全局 fallback 渐进瘦身 |
| I4 | injectAgentAPIKey 硬编码 `agent.Backend != "pi"` | 新增 `CLIPlugin.PreExecHook`，Pi 迁移到子包 |
| I5 | generic_acp fallback 无归属 | 保留在 `registry.go` 作为默认 ACP remaps |
| I6 | 前端 API 驱动阶段估算不足且规格缺失 | 标记为独立非阻塞阶段，需要独立设计文档 |

#### Minor 问题（已修复）

| # | 问题 | 修复 |
|---|------|------|
| M1 | init() 注册影响测试隔离 | 新增 `ResetForTest()` 函数 |
| M2 | ACP 阶段工时估算过低 | 数据注册模式从 ~6h 降至 ~2h（不再拆分 ProcessEvent） |
| M3 | needsAutoResume 函数迁移步骤遗漏 | 在阶段 10 清理中明确列出 |
| M4 | ACPState 设计与实际状态管理不匹配 | 移除 ACPState struct（不再需要——ProcessEvent 不迁移） |

## 11. 实施状态（2026-06-17）

### 阶段完成情况

| 阶段 | 状态 | 备注 |
|------|------|------|
| 0: 搭建框架 | ✅ 完成 | `plugin.go`、`registry.go`、`all.go` 创建完毕 |
| 0b: 添加字段 | ✅ 完成 | `ToolNameMap`/`InputRemaps` 加到共享 parser；`PreExecHookFn` 加到 CLIBackend |
| 1-7: 迁移所有后端 | ✅ 完成 | 12 个后端全部迁移到子包 |
| 8: ACP 映射数据迁移 | ✅ 完成 | 各子包添加 `acp.go`，注册 ACPPlugin；共享事件处理从注册表查询 |
| 9: 前端 API 驱动 | ⏭️ 跳过 | 标记为独立非阻塞阶段，后续独立推进 |
| 10: 清理 | ✅ 完成 | 详见下方 |

### 阶段 10 清理完成项

| 清理项 | 状态 | 实际实现 |
|--------|------|----------|
| 移除 `needsAutoResume()` | ✅ | `factory.go` 中从 switch 改为 `backends.Lookup(id).NeedsAutoResume` |
| 移除 `perAgentInputRemaps` 和 `getRemaps` | ✅ | `common_stream.go` 中已删除，各子包提供 `InputRemaps` 变量 |
| 移除 `normalizeToolName` 中已迁移的 case | ✅ | 保留了全局 fallback，per-backend 分支已移除 |
| 移除 `model/discovery.go` 中已迁移的 Discover* 函数 | ✅ | 所有 Discover* 函数迁移到子包 `discovery.go`，通过 `model.RegisterDiscoverModelsFunc` 注册 |
| 移除 `injectAgentAPIKey` | ✅ | 完全移除，Pi 的 API key 注入迁移到 `backends/pi/cli.go` 的 `injectPiAPIKey` PreExecHook |
| ToolNameMap 注入到共享 parser | ✅ | `StreamJSONParser`、`OpenCodeStreamParser`、`DeepSeekStreamParser`、`PiStreamParser` 均接受 `ToolNameMap` 字段 |
| InputRemaps 注入到共享 parser | ✅ | 同上，所有 parser 均接受 `InputRemaps` 字段 |
| BackendSpec 中的 DiscoverModelsFunc 字段 | ⚠️ 保留 | 改为三层查找：spec.DiscoverModelsFunc → registry → ListModelsCmd+ParseModels |

### 架构偏差（设计与实现的差异）

| 偏差 | 原因 |
|------|------|
| **注册模式简化**：设计文档中 `backends.Register(&BackendPlugin{...})` 统一注册所有数据（Spec+CLI+ACP+Discovery），实际实现拆分为 `ai.RegisterBackend()` 注册后端 + `model.RegisterDiscoverModelsFunc()` 注册发现函数 | 循环导入限制：`model` 包不能导入 `backends/*` 子包，所以发现函数必须单独注册到 `model` 包的 registry |
| **BackendSpec 仍在 model 包**：设计文档设想 `Spec` 从子包收集到 `model.BackendRegistry`，实际 `BackendRegistry` 仍手动维护在 `model/discovery.go` | `BackendSpec` 被 `SyncDiscoverAgentsDB`、`MergeDiscoveredDataDB` 等 DB 逻辑直接引用，迁移到子包需要重构大量 model 包代码 |
| **stream parser 未迁移到子包**：设计文档中 Claude/Codebuddy 等有独立 `stream.go`，实际实现中 stream parser 保留在 `internal/ai/` 作为共享组件 | 流解析器被多个后端共用（StreamJSONParser 被 5+ 后端使用），拆分到子包会导致代码重复或循环依赖 |
| **测试中的 InputRemaps 重复**：由于 `internal/ai` 测试不能导入 `backends/*` 子包（循环导入），测试文件中需复制 InputRemaps 数据 | Go 的循环导入限制，无法在 `package ai` 测试中导入 `backends/kimi` 等 |
| **DeepSeek/OpenCode 无 discovery.go**：这两个后端使用 `ListModelsCmd+ParseModels` 模式，不需要注册 `DiscoverModelsFunc` | 设计文档中列出 `discovery.go`，但实际上它们通过 `BackendSpec` 的 `ParseModels` 字段工作，无需额外注册 |

### 文件清单（实际创建/修改）

**子包 cli.go 文件**（12 个）：
- `backends/claude/cli.go` + `acp.go` + `discovery.go`
- `backends/codebuddy/cli.go` + `discovery.go`
- `backends/opencode/cli.go` + `acp.go`
- `backends/codex/custom.go` + `discovery.go`
- `backends/qoder/cli.go` + `discovery.go`
- `backends/vecli/custom.go` + `discovery.go`
- `backends/deepseek/cli.go`
- `backends/pi/cli.go` + `discovery.go`
- `backends/cline/cli.go` + `discovery.go`
- `backends/kimi/cli.go` + `acp.go` + `discovery.go`
- `backends/copilot/cli.go` + `discovery.go`
- `backends/mimo/cli.go` + `discovery.go`

**框架层修改**：
- `ai/factory.go` — 简化为注册表查找
- `ai/cli_backend.go` — 移除 `injectAgentAPIKey`，添加 `GetAgentAPIKeyLoader`
- `ai/common_stream.go` — 移除 `perAgentInputRemaps` 和 `getRemaps`
- `ai/stream_json_parser.go` — 添加 `InputRemaps`/`ToolNameMap` 字段，移除 `getRemaps` 回退
- `ai/opencode_stream.go` — 添加 `InputRemaps` 字段
- `ai/deepseek_stream.go` — 添加 `InputRemaps` 字段
- `ai/pi_stream.go` — 添加 `InputRemaps` 字段
- `model/discovery.go` — 添加 `RegisterDiscoverModelsFunc`/`lookupDiscoverFunc` registry，移除所有 Discover* 函数 |
