# AGENTS.md

## 项目概述

ClawBench 是移动优先的 AI 工作站，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、SSH 隧道端口转发、定时任务系统。

规格文档：`docs/spec/`

## 构建与运行

```bash
./build.sh                # 完整构建（Go 二进制 + Vue 前端）
./build.sh --windows      # 交叉编译：Windows amd64
./build.sh --linux        # 交叉编译：Linux amd64
./build.sh --darwin       # 交叉编译：macOS arm64

./dev-server.sh           # 开发模式（Vite HMR 代理到后端）
./dev-server.sh --fg      #   前台运行
./dev-server.sh --stop    #   停止
./dev-server.sh --restart #   重启

./clawbench               # 直接运行（前台，默认端口 20000）
./clawbench --port 8080   #   指定端口
./clawbench --data-dir /data/.clawbench  #   自定义数据目录

go build -o clawbench ./cmd/server   # 仅构建 Go 二进制
go test ./...                        # 所有 Go 测试
go test ./internal/ai/...            # 指定包测试
npm test                             # Vitest 前端测试

./scripts/pre-push-checks.sh              # 推送前全量检查（lint + test + build + typecheck + 覆盖率）
./scripts/pre-push-checks.sh --skip-coverage  # 跳过覆盖率门槛
./scripts/pre-push-checks.sh --skip-android   # 跳过 Android 覆盖率

./build.sh --restart              # 编译 + 后台重启 ClawBench（可在 Web 终端内执行）
./build.sh --restart-skip-build   # 跳过编译，仅重启
./build.sh --restart --restart-port=8080  # 重启并指定端口
```

## 架构

### 后端（Go）

入口：`cmd/server/main.go`

核心包：

| 包 | 职责 |
|---|------|
| `internal/handler/` | HTTP 端点，所有 `/api/` 路由经 `middleware.Auth` 鉴权，聊天通过 WebSocket 流式传输 |
| `internal/service/` | 业务逻辑：聊天持久化、自动摘要、调度器、SQLite、Schema 迁移、Agent 存储、API 密钥加密、会话归档留存期自动清理（SessionCleanupWorker） |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）。12 个后端子包通过 `ai.RegisterBackend()` 注册 |
| `internal/model/` | 数据模型、后端注册表、模型发现、28 个 LLM Provider |
| `internal/speech/` | TTS：Edge TTS、Piper、Kokoro、MOSS-TTS-Nano |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类分析（ClusterWorker：Union-Find + Sørensen-Dice） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道，StreamHub 会话级扇出，Manager 广播+重连缓冲回放 |
| `internal/ssh/` | SSH 隧道服务器 |
| `internal/push/` | IM 机器人推送：`common/`（共享接口+会话命令）、`dingtalk/`（钉钉 Stream API）、`feishu/`（飞书 Lark SDK WebSocket+互动卡片） |
| `internal/proxy/` | HTTP 反向代理+端口转发 |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/summarize/` | 文本摘要 |
| `internal/system/` | 系统资源监控：CPU、内存、磁盘、网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：task、rag、migrate |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测 |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。

Composable 按域分组：Chat、Session、Terminal、File、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

组件按域分组：Chat、File、Terminal、Git、Session/Agent、Task、Settings、Common。

## 开发规则

- **前端必须使用 appLog**：所有前端代码使用 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`（测试文件除外）。Tag 约定：短 PascalCase 模块名。
- **Android 必须使用 AppLog**：所有 Android 代码使用 `AppLog.d/i/w/e()`，禁止原始 `android.util.Log`（`AppLog.java` 本身和测试除外）。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
- **覆盖率门槛**：每 PR/推送到 main 强制执行——包级覆盖率不低于基线、变更行覆盖率 ≥ 80%。
- **推送前必须运行本地检查**：`./scripts/pre-push-checks.sh`

