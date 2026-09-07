# AGENTS.md

## 项目概述

ClawBench 是面向手机 / 平板 / 桌面的多端 AI 工作台，移动端交互适配优先、桌面端完整支持，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、SSH 隧道端口转发、定时任务系统。

规格文档：`docs/spec/`

## 构建与运行

```bash
./build.sh                # 完整构建（Go 二进制 + Vue 前端 + Excalidraw 独立构建）
./build.sh --windows      # 交叉编译：Windows amd64
./build.sh --linux        # 交叉编译：Linux amd64
./build.sh --linux-arm64  # 交叉编译：Linux arm64
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

### 运维：僵尸进程清理

`./scripts/kill-zombies.sh` 清理僵尸（defunct）进程及其孤儿进程树。僵尸进程无法直接 kill，只能靠父进程 reap 或杀掉父进程后由 init 收养清理。

```bash
./scripts/kill-zombies.sh                  # dry-run：列出僵尸与将要杀的进程树
./scripts/kill-zombies.sh --kill           # 实际清理（带确认）
./scripts/kill-zombies.sh --kill --force   # 跳过确认
./scripts/kill-zombies.sh --port 8080      # 额外保护 8080 端口的服务器
```

**安全规则（脚本默认强制执行）：**

- **绝不触碰 20000 端口主服务器** 及其完整后代树（包括 `clawbench --acp` 会话派生的 vitest/build/worker 进程）——通过 `/proc` 树形遍历识别，非 `pgrep -f` 模糊匹配
- 僵尸父进程是 init（PID 1）时自动跳过（init 会自动 reap）
- 杀进程树按子孙先 TERM → 再 KILL 顺序，避免留下新僵尸
- `--kill-protected` 可显式覆盖保护（危险，谨慎使用）

## 客户端日志回传

前端 JS 与 Android 原生日志统一回传服务器，汇入**单文件** `{data-dir}/logs/client.log`，行内用 `[js]` / `[android]` 标记区分来源。

- **开启设置**：设置 → 调试（Debug）→「调试日志捕获」（`logCapture`，默认关）。开启后：
  - **App 模式（Android）**：前端 JS 日志跳过 console 与 native 桥，仅 HTTP 上报一份（单份，无 `WebView:LOG` 重复与 `[object Object]` 失真）；
  - **网页模式**：console 照常输出 + HTTP 上报；
  - 关闭时日志只在本地可见（logcat / console），不发服务器。
- **JS（`web/src/utils/appLog.ts`）**：批量 POST `/api/client-log`（2s / 200 条缓冲 / 200 条每请求），`source="js"` → `[js]` 行。
- **Android（`android/app/.../AppLog.java`）**：捕获开启时每 3s POST `/api/android-log`（legacy alias，旧 APK 兼容），`source="android"` → `[android]` 行。
- **服务端（`internal/handler/android_log.go`）**：`ServeClientLog` 统一写 `{LogDir}/logs/client.log`，行格式 `2006-01-02T15:04:05.000 [js] I/ChatStream: msg`（换行转义为 `\n`），50MiB 轮转到 `client.log.1`。端点无鉴权（仅写日志、不入库）。
- **查看**：`tail -f {data-dir}/logs/client.log`、`grep '\[js\]' {data-dir}/logs/client.log`（例：`tail -f /opt/clawbench-green-data/logs/client.log`）。

## 架构

### 后端（Go）

入口：`cmd/server/main.go`

核心包：

| 包 | 职责 |
|---|------|
| `internal/handler/` | HTTP 端点，所有 `/api/` 路由经 `middleware.Auth` 鉴权，聊天通过 WebSocket 流式传输 |
| `internal/service/` | 业务逻辑：聊天持久化、自动摘要、对话推荐、调度器、SQLite、Schema 迁移、Agent 存储、会话归档留存期自动清理（SessionCleanupWorker） |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）。14 个后端子包通过 `ai.RegisterBackend()` 注册。CLI/ACP 均支持无进度看门狗（NoProgressTimeout/stallTimeout），防止进程挂起。CodeBuddy ACP 含 Plugin Skills 竞态修复（预扫描+延迟重发）与 `~/.codebuddy/skills/` 技能扫描（YAML frontmatter 解析 → 斜杠命令 + 系统提示词注入） |
| `internal/model/` | 数据模型、后端注册表、模型发现、27 个 LLM Provider |
| `internal/speech/` | TTS：Edge TTS、Piper、Kokoro、MOSS-TTS-Nano |
| `internal/stt/` | STT（语音输入）：vLLM Whisper，流式/非流式双端点 |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类分析（ClusterWorker：Union-Find + Sørensen-Dice） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道，StreamHub 会话级扇出，Manager 广播+重连缓冲回放 |
| `internal/ssh/` | SSH 隧道服务器 |
| `internal/push/` | IM 机器人推送：`common/`（共享接口+会话命令）、`dingtalk/`（钉钉 Stream API）、`feishu/`（飞书 Lark SDK WebSocket+互动卡片） |
| `internal/proxy/` | HTTP 反向代理+端口转发 |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/summarize/` | 文本摘要、对话推荐（next-step recommendation） |
| `internal/system/` | 系统资源监控：CPU、内存、磁盘、网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：task、rag、migrate |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测 |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。

Composable 按域分组：Chat、Session、Terminal、File、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

组件按域分组：Chat、File、Terminal、Git、Session/Agent、Task、Settings、Common。

`web/vendor-build/excalidraw/` 是独立的 Excalidraw 编辑器构建（React），由 `build.sh` 单独构建到 `public/vendor/excalidraw/`，`.excalidraw` 文件通过 iframe 懒加载它，Vue 主包不包含 React 依赖。

`web/src/share/` 是文件分享链接的独立只读 SPA（类型分派渲染 + TOC + 下载），由 vite 多入口构建为 `share.html`，服务端在 `/share/{token}` 无鉴权公开（token 即凭证）。

## 开发规则

- **前端必须使用 appLog**：所有前端代码使用 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`（测试文件除外）。Tag 约定：短 PascalCase 模块名。
- **Android 必须使用 AppLog**：所有 Android 代码使用 `AppLog.d/i/w/e()`，禁止原始 `android.util.Log`（`AppLog.java` 本身和测试除外）。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
- **覆盖率门槛**：每 PR/推送到 main 强制执行——包级覆盖率不低于基线、变更行覆盖率 ≥ 80%。
- **推送前必须运行本地检查**：`./scripts/pre-push-checks.sh`

