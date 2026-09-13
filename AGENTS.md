# AGENTS.md

## 项目概述

ClawBench 是面向手机 / 平板 / 桌面的多端 AI 工作台，移动端交互适配优先、桌面端完整支持，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、SSH 隧道端口转发、计划任务系统。

规格文档：`docs/spec/`（模块索引见 `docs/spec/README.md`）。

## 构建与运行

```bash
# 构建
./build.sh                                            # Go 二进制 + Vue 前端 + Excalidraw
./build.sh --windows|--linux|--linux-arm64|--darwin   # 交叉编译
go build -o clawbench ./cmd/server                    # 仅 Go 二进制

# 开发
./dev-server.sh [--fg|--stop|--restart]               # Vite HMR 代理到后端
./clawbench [--port 8080] [--data-dir /data/.clawbench]   # 直接运行，默认端口 20000

# 测试与检查
go test ./...                                         # 单包：go test ./internal/ai/...
npm test                                              # Vitest 前端测试
./scripts/pre-push-checks.sh [--skip-coverage|--skip-android]   # 推送前全量检查

# 编译并后台重启（可在 Web 终端内执行）
./build.sh --restart [--restart-skip-build] [--restart-port=8080]
```

### 运维：僵尸进程清理

`./scripts/kill-zombies.sh` 清理僵尸（defunct）进程及其孤儿进程树。默认 dry-run 列出僵尸与将要杀的进程树；`--kill` 实际清理（`--force` 跳过确认）；`--port 8080` 额外保护 8080 端口的服务器。

**安全规则（脚本默认强制执行）：**

- **绝不触碰 20000 端口主服务器** 及其完整后代树（包括 `clawbench --acp` 会话派生的 vitest/build/worker 进程）——通过 `/proc` 树形遍历识别，非 `pgrep -f` 模糊匹配
- 僵尸父进程是 init（PID 1）时自动跳过（init 会自动 reap）
- 杀进程树按子孙先 TERM → 再 KILL 顺序，避免留下新僵尸
- `--kill-protected` 可显式覆盖保护（危险，谨慎使用）

## 客户端日志回传

前端 JS 与 Android 原生日志统一回传服务器，汇入**单文件** `{data-dir}/logs/client.log`，行内用 `[js]` / `[android]` 标记区分来源。

- **开关**：设置 → 调试 →「调试日志捕获」（`logCapture`，默认关）。开启后 App 模式（Android）的 JS 日志仅 HTTP 上报一份（跳过 console 与 native 桥，避免 `WebView:LOG` 重复与 `[object Object]` 失真），网页模式则 console + HTTP 双份；关闭时日志只在本地（logcat / console）可见。
- **JS（`web/src/utils/appLog.ts`）**：批量 POST `/api/client-log`（2s / 200 条缓冲 / 200 条每请求），`source="js"` → `[js]` 行。
- **Android（`android/app/.../AppLog.java`）**：捕获开启时每 3s POST `/api/client-log`，`source="android"` → `[android]` 行。
- **服务端（`internal/handler/android_log.go`）**：`ServeClientLog` 统一写 `{LogDir}/logs/client.log`，行格式 `2006-01-02T15:04:05.000 [js] I/ChatStream: msg`（换行转义为 `\n`），50MiB 轮转到 `client.log.1`。端点无鉴权（仅写日志、不入库）。
- **查看**：`tail -f {data-dir}/logs/client.log`、`grep '\[js\]' {data-dir}/logs/client.log`。

## 架构

### 后端（Go）

入口：`cmd/server/main.go`。各包深度设计见 `docs/spec/`。

| 包 | 职责 |
|---|------|
| `internal/handler/` | 全部 `/api/` HTTP 端点（经 `middleware.Auth` 鉴权）+ WebSocket 聊天流式推送 |
| `internal/api/` | `go:embed` OpenAPI 规格，按 operationId 渲染内置斜杠命令注入给 AI 的接口提示片段 |
| `internal/wallpaper/` | 壁纸校验 / 缩放 / 编码 + 磁盘布局与生效解析；handler 与 service worker 共用。缩放上限取舍见源码注释 |
| `internal/service/` | 业务逻辑：聊天持久化、摘要与推荐的调度、调度器、SQLite、Schema 迁移、Agent 存储、用量聚合、会话截断；含 SessionCleanupWorker / BingWallpaperWorker 等后台 worker |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）；14 个后端子包；CLI/ACP 均支持无进度看门狗 |
| `internal/model/` | 数据模型、后端注册表、模型发现、27 个 LLM Provider |
| `internal/speech/` + `internal/stt/` | 语音：TTS（Edge / Piper / Kokoro / MOSS-TTS-Nano）与 STT（vLLM Whisper，流式 + 非流式） |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类（ClusterWorker） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道：StreamHub 会话级扇出，Manager 广播 + 重连缓冲回放 |
| `internal/ssh/` + `internal/proxy/` | SSH 隧道服务器；HTTP 反向代理 + 端口转发 |
| `internal/push/` | IM 机器人推送：`common/`（共享接口 + 会话命令）、`dingtalk/`（Stream API）、`feishu/`（Lark SDK WebSocket + 互动卡片） |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/summarize/` | 摘要与推荐的底层引擎（多后端 provider、多 pass 压缩、`StripMarkdown`、`RecommendNextStep`） |
| `internal/system/` | 系统资源监控：CPU / 内存 / 磁盘 / 网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：task、rag、upgrade-replace |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测 |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。

Composable 与组件均按域分组（Chat、Session、Terminal、File、Git、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System）。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

`web/vendor-build/excalidraw/` 是独立的 Excalidraw 编辑器构建（React），由 `build.sh` 单独构建到 `public/vendor/excalidraw/`，`.excalidraw` 文件通过 iframe 懒加载，Vue 主包不含 React 依赖。

`web/src/share/` 是文件分享链接的独立只读 SPA（类型分派渲染 + TOC + 下载），由 vite 多入口构建为 `share.html`，服务端在 `/share/{token}` 无鉴权公开（token 即凭证）。

## 开发规则

- **日志必须用封装**：前端一律 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`；Android 一律 `AppLog.d/i/w/e()`，禁止 `android.util.Log`。两者的自身实现与测试除外。Tag 约定：短 PascalCase 模块名。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
- **改动 HTTP 接口必须同步 OpenAPI 文档**：任何新增 / 删除 / 修改 `/api/` 端点（路径、方法、鉴权、参数、请求 / 响应字段、状态码）都必须同步更新 `internal/api/openapi.yaml`（已从 `docs/spec/api/` 迁入以支持 `go:embed`）。
  - 字段名、参数名、方法**必须从 handler 代码里抄**（`decodeJSON` 结构体的 JSON tag、`r.URL.Query().Get(...)`、`requireMethod(...)` / `switch r.Method`），**禁止凭路由名望文生义**。
  - 路由唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`；`internal/handler/openapi_drift_test.go` 双向校验路径与鉴权（**不校验字段名**）。
  - 完整维护清单见 `docs/spec/api/README.md`。
- **纯前端改动完成后必须自觉编译**：若只涉及 `web/src/`、`web/index.html` 等（未动 Go / Android），跑通测试后直接构建并同步 embed 目录，供用户立即在浏览器 / App 中测试，无需用户再要求：

  ```bash
  cd web && npm run build        # 或项目根目录：npm run build
  rm -rf internal/frontend/dist && cp -r public internal/frontend/dist
  ```

  `internal/frontend/dist` 是 gitignore 的构建产物，不同步则运行中的服务看不到改动，不进提交。
- **覆盖率门槛**：每 PR / 推送到 main 强制执行——包级覆盖率不低于基线、变更行覆盖率 ≥ 80%。
- **推送前必须运行本地检查**：`./scripts/pre-push-checks.sh`
