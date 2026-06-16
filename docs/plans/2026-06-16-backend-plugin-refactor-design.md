# AI 后端插件化重构方案

> 日期：2026-06-16
> 状态：设计完成，待实现

## 1. 背景与动机

移除 Gemini 后端时，尽管 Gemini 仅占 `internal/ai/gemini*.go` 几个文件，却影响了 **46 个文件** ——factory、discovery、common_stream、ACP 工具名称、前端 provider 列表、设置字段、i18n 等。这暴露了当前架构的核心问题：**后端实现分散在多个共享模块中，缺乏内聚性**。

### 当前痛点

| 问题 | 示例 |
|------|------|
| 新增/移除后端需修改多处 | factory switch、discovery 全局数组、common_stream remaps、ACP tool mapping、前端硬编码列表 |
| 共享代码与后端特定代码混在一起 | `normalizeToolName()` 包含所有后端的别名；`perAgentInputRemaps` 是全局 map |
| 后端无法独立测试 | 所有后端在同一个 package 中，测试互相影响 |
| 横切关注点扩散 | ACP 事件处理在 `acp_events.go` 中按 backend type 分支 |

## 2. 目标

1. **高内聚**：一个后端的所有代码（CLI 参数构造、stream parser、工具映射、模型发现、ACP 事件处理）位于一个子包中
2. **低耦合**：新增后端只需添加子包 + 在 `all.go` 注册，不修改框架代码
3. **可独立测试**：每个后端子包可独立运行单元测试
4. **前端 API 驱动**：后端列表、设置 schema 从 API 获取，不在前端硬编码
5. **渐进迁移**：一次迁移一个后端，每步可编译可测试

## 3. 目标目录结构

```
internal/ai/
├── interface.go              # AIBackend 接口（不变）
├── cli_backend.go            # CLIBackend 通用骨架（不变）
├── auto_resume.go            # AutoResumeBackend 装饰器（不变）
├── acp_backend.go            # ACPBackend 通用骨架（不变）
├── acp_*.go                  # ACP 通用基础设施（不变）
├── stream_parser.go          # StreamParser 通用接口（不变）
├── stream_json_parser.go     # StreamJSONParser（Kimi 格式，提升为共享组件）
├── common_stream.go          # normalizeToolName/Input 通用工具（保留核心，移除 per-agent remaps）
├── accumulate.go             # 块聚合（不变）
├── block_helpers.go          # 块辅助（不变）
├── factory.go                # 简化：从注册表查找，不再 switch/case
├── orphan.go                 # 孤儿进程清理（不变）
├── backends/
│   ├── all.go                # 集中式注册入口，import 触发 init()
│   ├── plugin.go             # BackendPlugin 接口定义
│   ├── registry.go           # 全局注册表 + Register/Lookup 函数
│   ├── claude/
│   │   ├── cli.go            # buildArgs, newParser, filterLine, preStart → CLIBackendConfig
│   │   ├── acp.go            # ProcessEvent, tool mappings → ACPConfig
│   │   ├── stream.go         # ClaudeStreamParser（从 claude_stream.go 迁移）
│   │   ├── stream_test.go
│   │   ├── cli_test.go
│   │   ├── acp_test.go
│   │   └── discovery.go      # DiscoverClaudeModels（从 model/discovery.go 迁移）
│   ├── codebuddy/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   ├── stream_test.go
│   │   └── discovery.go
│   ├── opencode/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   ├── tool.go           # OpenCode 工具名映射
│   │   └── discovery.go
│   ├── codex/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   ├── think.go          # Codex thinking 解析
│   │   ├── tool.go           # Codex 工具名映射
│   │   └── discovery.go
│   ├── qoder/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── vecli/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── deepseek/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   ├── tool.go
│   │   └── discovery.go
│   ├── pi/
│   │   ├── cli.go
│   │   ├── stream.go
│   │   ├── tool.go
│   │   └── discovery.go
│   ├── cline/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── kimi/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   └── discovery.go
│   ├── copilot/
│   │   ├── cli.go
│   │   ├── acp.go
│   │   ├── stream.go
│   │   └── discovery.go
│   └── mimo/
│       ├── cli.go
│       ├── acp.go
│       ├── stream.go
│       └── discovery.go
internal/model/
├── discovery.go               # 保留 BackendSpec 结构 + 通用发现逻辑，移除各后端的 Discover* 函数
└── provider_registry.go       # 保留（provider 级别的注册，与后端插件独立）
```

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

    // CLI 是 CLI 模式的配置。nil 表示该后端不支持 CLI（仅 ACP）。
    CLI *CLIPlugin

    // ACP 是 ACP 模式的配置。nil 表示该后端不支持 ACP。
    ACP *ACPPlugin

    // NeedsAutoResume 为 true 时，CLI 模式自动包装 AutoResumeBackend。
    NeedsAutoResume bool
}

// CLIPlugin 提供 CLI 模式的后端特定配置。
type CLIPlugin struct {
    // NewBackend 返回一个 CLIBackend 实例（已配置 buildArgs/newParser/filterLine/preStart）。
    NewBackend func() *ai.CLIBackend

    // ToolNameMap 是该后端的工具名归一化映射表（完整表，非增量）。
    // key: 后端原始工具名 → value: 规范名（如 "read_file" → "Read"）
    ToolNameMap map[string]string

    // InputRemaps 是该后端的工具输入字段重映射表（完整表，非增量）。
    // key: 原始字段名 → value: 目标字段名（如 "filePath" → "file_path"）
    InputRemaps map[string]string
}

// ACPPlugin 提供 ACP 模式的后端特定配置。
type ACPPlugin struct {
    // ProcessEvent 处理 ACP 事件并返回 StreamEvent。
    // 每个后端实现完整的事件处理逻辑，公共逻辑提取为 helper 函数。
    ProcessEvent func(agent *model.Agent, event acpEvent, state *ACPState) []ai.StreamEvent

    // ToolCallIDPrefixes 是该后端 ACP toolCallID 前缀到规范名的映射。
    // 如 Kimi: "read_file" → "Read"
    ToolCallIDPrefixes map[string]string

    // InputRemaps 是该后端 ACP 模式的工具输入字段重映射表。
    InputRemaps map[string]string
}

// ACPState 封装 ACP 连接的运行时状态，供 ProcessEvent 读写。
type ACPState struct {
    Mode             string
    ThinkingEffort   string
    Config           map[string]any
    Commands         []string
    Plan             *PlanState
    ToolCallBuffer   []ToolCallUpdate
}
```

### 4.2 注册表

```go
// backends/registry.go
package backends

import "fmt"

var plugins = make(map[string]*BackendPlugin)

// Register 将后端插件注册到全局注册表。
// 通常在子包的 init() 函数中调用。
// 重复注册会 panic（编程错误）。
func Register(p *BackendPlugin) {
    if _, exists := plugins[p.ID]; exists {
        panic(fmt.Sprintf("backend plugin already registered: %s", p.ID))
    }
    plugins[p.ID] = p
}

// Lookup 返回指定 ID 的后端插件，不存在返回 nil。
func Lookup(id string) *BackendPlugin {
    return plugins[id]
}

// All 返回所有已注册的后端插件。
func All() []*BackendPlugin {
    result := make([]*BackendPlugin, 0, len(plugins))
    for _, p := range plugins {
        result = append(result, p)
    }
    return result
}

// AllSpecs 返回所有已注册后端的 BackendSpec，用于填充 model.BackendRegistry。
func AllSpecs() []model.BackendSpec {
    specs := make([]model.BackendSpec, 0, len(plugins))
    for _, p := range plugins {
        specs = append(specs, p.Spec)
    }
    return specs
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
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

func NewBackend(backendType string) (AIBackend, error) {
    p := backends.Lookup(backendType)
    if p == nil {
        return nil, fmt.Errorf("unsupported backend type: %s", backendType)
    }

    var backend AIBackend
    if p.CLI != nil {
        backend = p.CLI.NewBackend()
    } else {
        return nil, fmt.Errorf("backend %s has no CLI support", backendType)
    }

    if p.NeedsAutoResume {
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

## 5. 后端子包示例：Kimi

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
            ProcessEvent:      processACPEvent,
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
}

func newCLIBackend() *ai.CLIBackend {
    return &ai.CLIBackend{
        BuildArgs:  buildArgs,
        NewParser:  newParser,
        FilterLine: filterLine,
    }
}

func buildArgs(req ai.ChatRequest) []string {
    // ... (从当前 kimi.go 迁移)
}

func newParser() ai.StreamParser {
    return &ai.StreamJSONParser{}
}

func filterLine(line string) string {
    // ... (从当前 kimi.go 迁移)
}
```

```go
// backends/kimi/acp.go
package kimi

import (
    "clawbench/internal/ai/backends"
    "clawbench/internal/model"
)

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

var kimiACPInputRemaps = map[string]string{
    "filePath": "file_path",
    "cmd":      "command",
    "dirPath":  "path",
}

func processACPEvent(agent *model.Agent, event acpEvent, state *backends.ACPState) []ai.StreamEvent {
    // 完整多态实现，公共逻辑调用 backends 公共 helper
    // ... (从当前 acp_events.go 中 Kimi 分支迁移)
}
```

## 6. 迁移策略

分阶段执行，每阶段可编译可测试。

### 阶段 0：搭建框架（~1h）

1. 创建 `internal/ai/backends/` 目录
2. 实现 `plugin.go`（接口定义）、`registry.go`（注册表）、`all.go`（空导入占位）
3. 实现 `registry.go` 中的 `AllSpecs()` → 临时写入 `model.BackendRegistry`
4. **验证**：`go build ./...` 通过，现有测试不受影响

### 阶段 1：迁移第一个后端 — Pi（最简单，仅 CLI，无 ACP）（~2h）

1. 创建 `backends/pi/` 子包
2. 迁移 `pi.go` + `pi_stream.go` + `pi_tool.go` + `pi_test.go` + `pi_stream_test.go` + `pi_tool_test.go`
3. 在 `all.go` 中添加 `_ "clawbench/internal/ai/backends/pi"` 导入
4. 从 `factory.go` 移除 Pi 的 switch case，改用 `backends.Lookup("pi")`
5. 迁移 `model/discovery.go` 中 `DiscoverPiModels` + `ParsePiModels` + 相关正则/常量
6. 从 `model.BackendRegistry` 移除 Pi 的硬编码 entry
7. **验证**：`go test ./internal/ai/... ./internal/model/...` 全部通过

### 阶段 2：迁移简单 CLI 后端（VeCLI, DeepSeek）（~3h）

每个后端步骤同阶段 1，但含 stream parser 和 tool mapping 迁移。

### 阶段 3：迁移 AutoResume CLI 后端（Claude, Codebuddy, Qoder, Cline, Kimi, Copilot, MiMo-Code）（~6h）

1. 迁移 CLI 配置 + stream parser
2. 迁移 `common_stream.go` 中该后端的 `perAgentInputRemaps` 条目到子包
3. 迁移 `normalizeToolName()` 中该后端的别名到子包的 `ToolNameMap`
4. **关键**：`buildBaseStreamArgs` 保留在 `common_stream.go` 作为共享 helper，子包调用它

### 阶段 4：迁移 ACP 后端（~6h）

1. 每个 ACP 后端实现 `ProcessEvent`
2. 迁移 `acp_events.go` 中按 backend type 分支的逻辑到对应子包
3. 迁移 `acp_tool_names.go` 中各后端的映射表到子包
4. ACP 公共 helper（debounce、state extract、tool name resolution fallback）保留在 `internal/ai/` 作为共享工具

### 阶段 5：迁移 Codex 和 OpenCode（特殊逻辑）（~3h）

- Codex: 特殊的 think 解析、tool 解析、多策略模型发现
- OpenCode: 独立 stream parser、工具名映射

### 阶段 6：前端 API 驱动（~4h）

1. 新增 `GET /api/backends` 端点，返回后端列表 + 设置 schema
2. 前端 `useSetup.ts` 的 provider 列表从 API 加载而非硬编码
3. 前端 `settingsFieldMap.ts` 的 summarize backend 选项从 API 获取
4. i18n key 动态化

### 阶段 7：清理（~1h）

1. 移除 `factory.go` 中的 switch/case（已全部迁移）
2. 移除 `common_stream.go` 中的 `perAgentInputRemaps`（已迁移到各子包）
3. 移除 `model/discovery.go` 中已迁移的 Discover* 函数
4. 更新 `docs/spec/core/ai-backend.md`

## 7. 关键设计决策记录

| # | 决策 | 选项 | 理由 |
|---|------|------|------|
| 1 | 插件化注册 | A) Plugin 注册 vs B) 声明式配置 | 代码即配置，类型安全，IDE 可发现 |
| 2 | 子包粒度 | CLI/ACP 分子包 vs 单文件 | CLI 和 ACP 逻辑差异大，分离更清晰；但同一 ID 统一注册，避免碎片 |
| 3 | 注册接口 | 一个大接口 vs 多个小接口 | 后端概念本身就是统一的，拆分接口增加理解成本 |
| 4 | ACP 事件处理 | 完全多态 vs switch 分支 | 每个后端差异足够大，公共 helper 比公共 switch 更灵活 |
| 5 | 工具名映射 | 完整映射表 vs 共享默认+覆盖 | 完整表自包含，无隐式依赖，新增后端无需理解全局默认 |
| 6 | 模型发现注册 | Register() 返回 BackendSpec vs 独立注册 | 后端和发现逻辑天然一对，放一起减少心智负担 |
| 7 | CLI/ACP 注册 | 一个插件两个字段 vs 两个独立插件 | 同一后端的 CLI/ACP 是同一事物的两种传输，统一管理 |
| 8 | 前端 | API 驱动 vs 硬编码 | 后端列表动态变化，前端不应硬编码 |
| 9 | 注册时机 | 集中式 all.go + init() vs 分散注册 | all.go 是唯一注册入口，import 列表一目了然 |

## 8. 风险与缓解

| 风险 | 缓解措施 |
|------|----------|
| 循环导入 | 子包只依赖 `internal/ai`（接口+骨架）和 `internal/model`，反向不依赖 |
| 迁移过程中测试覆盖下降 | 每阶段强制运行全量测试，迁移测试代码与实现代码同步 |
| StreamJSONParser 共享问题 | 保留在 `internal/ai/` 作为通用组件（Kimi/VeCLI 等共用） |
| init() 注册顺序不确定 | all.go 中 import 顺序无影响——注册表是 map，不依赖顺序 |
| 大接口演进困难 | 接口字段均为值类型（函数+map），新增字段只需在子包中填充 |

## 9. 不变的部分

以下组件**不迁移**到子包，保留在 `internal/ai/` 作为框架层：

- `AIBackend` 接口
- `CLIBackend` 通用骨架（进程管理、stdout 管道、上下文取消）
- `AutoResumeBackend` 装饰器
- `ACPBackend` 通用骨架（连接管理、debounce、crash diagnostics）
- `StreamParser` / `StreamJSONParser` 通用解析器
- `buildBaseStreamArgs` 共享参数构造 helper
- `normalizeToolName` / `normalizeToolInput` 通用归一化函数（核心逻辑保留，per-agent 映射表迁移到子包）
- 孤儿进程清理逻辑
