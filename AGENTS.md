# AGENTS.md

## 项目概述

ClawBench 是面向手机 / 平板 / 桌面的多端 AI 工作台，移动端交互适配优先、桌面端完整支持，将 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder CLI、VeCLI、CodeWhale、MiMo-Code、Pi、Copilot、Kimi、Antigravity、Grok Build、ZCode）封装为 Web 平台。Go 后端调用 CLI 工具，通过 WebSocket 流式传输 JSON 事件；Vue 3 前端实时渲染。支持 ACP (Agent Client Protocol) stdio 传输（含桥接适配器）、SSH 隧道端口转发、任务系统（含 GitHub/GitLab 事件触发）。

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

前端 JS、Android 原生与 Electron 主进程日志统一回传服务器，汇入**单文件** `{data-dir}/logs/client.log`，行内用 `[js]` / `[android]` / `[electron]` 标记区分来源。

- **开关**：设置 → 调试 →「调试日志捕获」（`logCapture`，默认关）。开启后 App 模式（Android）的 JS 日志仅 HTTP 上报一份（跳过 console 与 native 桥，避免 `WebView:LOG` 重复与 `[object Object]` 失真），网页模式则 console + HTTP 双份；关闭时日志只在本地（logcat / console）可见。
- **JS（`web/src/utils/appLog.ts`）**：批量 POST `/api/client-log`（2s / 200 条缓冲 / 200 条每请求），`source="js"` → `[js]` 行。
- **Android（`android/app/.../AppLog.java`）**：捕获开启时每 3s POST `/api/client-log`，`source="android"` → `[android]` 行；请求带 WebView 会话 Cookie（同进程读取）。
- **Electron 主进程（`desktop/src/main/clientLog.ts`）**：同样的 2s / 200 条缓冲，`source="electron"` → `[electron]` 行；认证用 Electron cookie jar 里的会话 Cookie（`session.defaultSession.cookies`，按 `_clawbench_session` 后缀匹配，因服务端名字带端口前缀）。同时写本地 `{userData}/desktop.log`。未捕获异常/未处理拒绝经 `recordError` 一并上报（此前只进 stdout，关掉终端即丢失）。`native:log` 桥（渲染进程 → 主进程）落同一个文件——桌面没有 logcat 兜底，这条链是它唯一的本地副本。
  - **桌面不跳过 console**：`appLog.emit()` 的 `singleHttp` 分支只对 Android 生效。桌面若照搬，`desktop.log` 会因监听 `console-message` 却拿不到 console 输出而**恒为空**（实测：捕获开启时写 5 条 appLog 仍 0 字节）。因此桌面保留 console，改为跳过 native 桥转发——否则同一行会在 `desktop.log` 与 `client.log` 各出现两次。
- **服务端（`internal/handler/android_log.go`）**：`ServeClientLog` 统一写 `{LogDir}/logs/client.log`，行格式 `2006-01-02T15:04:05.000 [js] I/ChatStream: msg`，50MiB 轮转到 `client.log.1`。
  - **需鉴权**：这是向服务端文件追加写入的原语，匿名调用者可伪造日志行或反复触发轮转以销毁上一代日志。三个客户端上报时均已登录（JS 中继受 `logCapture` 门控且仅在已认证的应用内启用；Android `startCapture` 由登录后的 WebView 桥触发；Electron 主进程需读到会话 Cookie，未登录时静默跳过）。
  - **字段清洗**：`Msg`/`Tag`/`Level`/`Source` 全部转义换行、CR 与 NUL 并截断——只转义 `Msg` 会让 `Tag` 可伪造整行。`Source` 有独立的长度上限（32）而非复用 level 的 8：`"electron"` 恰好 8 字节，复用会让下一个更长的来源名被静默截断成另一个来源。
  - **上限**：单请求 200 条、总计 256 KiB。**不限流**——磁盘已由字节上限与 50 MiB 文件上限封死，限流不改变该边界；且 429 会被两端客户端静默丢弃（`appLog.ts` 不重试），徒增丢日志风险。
- **查看**：`tail -f {data-dir}/logs/client.log`、`grep '\[js\]' {data-dir}/logs/client.log`、`grep '\[electron\]' {data-dir}/logs/client.log`。

## 架构

### 后端（Go）

入口：`cmd/server/main.go`。各包深度设计见 `docs/spec/`。

| 包 | 职责 |
|---|------|
| `internal/handler/` | 全部 `/api/` HTTP 端点（经 `middleware.Auth` 鉴权）+ WebSocket 聊天流式推送 |
| `internal/api/` | `go:embed` OpenAPI 规格，按 operationId 渲染内置斜杠命令注入给 AI 的接口提示片段 |
| `internal/wallpaper/` | 壁纸校验 / 缩放 / 编码 + 磁盘布局与生效解析；handler 与 service worker 共用。缩放上限取舍见源码注释 |
| `internal/gitignore/` | 判定「git 是否会跟踪该路径」：go-git 模式引擎 + 来自 index 的三条规则（已跟踪文件/含已跟踪文件的目录永不忽略、祖先被排除则整体忽略、自身最后一条匹配）。文件管理器灰显与 cloc 排除共用；按真实 `git check-ignore` 差分验证 |
| `internal/service/` | 业务逻辑：聊天持久化、摘要与推荐的调度、调度器（cron + 事件触发任务）、SQLite、Schema 迁移、Agent 存储、用量聚合、会话截断；会话运行态收敛在 `session_runner.go`（单一 owner runner，运行态与可取消性同源），AI 回合编排唯一实现 `run_turn.go`，请求构造唯一实现 `chat_request.go`，队列兜底回收 `queue_reaper.go`；含 SessionCleanupWorker / BingWallpaperWorker / ForgePoller（forge 变化轮询）、桌面端升级检查（`desktop_upgrade.go`）等后台 worker |
| `internal/ai/` + `backends/` | AI 后端抽象：`AIBackend` → `CLIBackend`（CLI+行解析）或 `ACPBackend`（JSON-RPC over stdio）；14 个后端子包；CLI/ACP 均支持无进度看门狗 |
| `internal/model/` | 数据模型、后端注册表、模型发现（`ModelSource` 注册表 + 单一合并点 `ResolveModels`）、27 个 LLM Provider |
| `internal/speech/` + `internal/stt/` | 语音：TTS（Edge / Piper / Kokoro / MOSS-TTS-Nano）与 STT（vLLM Whisper，流式 + 非流式） |
| `internal/rag/` | RAG：SQLite + sqlite-vec 向量存储 + FTS5 全文检索，OpenAI 兼容嵌入 API；消息聚类（ClusterWorker） |
| `internal/terminal/` | Web 终端：PTY 会话、环形缓冲回放、多标签 |
| `internal/ws/` | WebSocket 事件通道：StreamHub 会话级扇出，Manager 广播 + 重连缓冲回放；`delivery_stats.go` 记录按原因的投递丢弃计数（`GET /api/ws/delivery-stats`），关键事件在通道满时等待空位（ACP 来源除外） |
| `internal/ssh/` + `internal/proxy/` | SSH 隧道服务器；HTTP 反向代理 + 端口转发 |
| `internal/forge/` | GitHub/GitLab 集成：平台无关的只读 `Provider` 抽象（统一 Issue/PR/Comment/Pipeline 模型）+ `github/`（go-github）/ `gitlab/`（轻量 REST client）adapter；remote URL 解析（host 与 scheme 分离解析）、per-host 令牌桶限流、事件推导引擎。**无 host 安全闸门**（内网/自建实例一律放行，风险提示在前端绑定弹窗） |
| `internal/push/` | IM 机器人推送：`common/`（共享接口 + 会话命令）、`dingtalk/`（Stream API）、`feishu/`（Lark SDK WebSocket + 互动卡片） |
| `internal/symbol/` | 基于 tree-sitter 的代码符号提取（纯 Go，无 CGO） |
| `internal/summarize/` | 摘要与推荐的底层引擎（多后端 provider、多 pass 压缩、`StripMarkdown`、`RecommendNextStep`） |
| `internal/system/` | 系统资源监控：CPU / 内存 / 磁盘 / 网络实时采集与推送 |
| `internal/cli/` | AI Agent 自助命令：仅剩 upgrade-replace（自升级内部机制）；task/rag 业务子命令已移除，改由 `/cb-*` 内置斜杠命令直调 HTTP API |
| `internal/middleware/` | 鉴权、请求日志、panic 恢复、请求 ID |
| `internal/platform/` | 跨平台路径解析、Shell 检测、二进制替换原语（`ReplaceBinary`，升级路径共用） |

### 前端（Vue 3 + TypeScript）

源码根：`web/src/`。无 Vue Router，基于抽屉的单页布局。单一 `reactive()` store (`stores/app.ts`)。

Composable 与组件均按域分组（Chat、Session、Terminal、File、Git、Navigation/Gesture、Settings、Agent、Task、Infrastructure、System）。新建 composable 须放 `web/src/composables/` 并以 `useXxx` 命名，测试用 `*.test.ts` 同目录或 `__tests__/`。

宽屏 Dock 页签定义在 `web/src/composables/dockTabs.ts`（单一注册表，渲染集合与切换白名单都从它派生），图标单独放 `dockTabMeta.ts`。`dockTabs.ts` 必须保持零 import（`useWideScreenLayout` 依赖它，而多个测试文件对 `lucide-vue-next` 做了窄 mock）。

`web/vendor-build/excalidraw/` 是独立的 Excalidraw 编辑器构建（React），由 `build.sh` 单独构建到 `.clawbench-web/vendor/excalidraw/`，`.excalidraw` 文件通过 iframe 懒加载，Vue 主包不含 React 依赖。

`web/src/share/` 是文件分享链接的独立只读 SPA（类型分派渲染 + TOC + 下载），由 vite 多入口构建为 `share.html`，服务端在 `/share/{token}` 无鉴权公开（token 即凭证）。

### 桌面端（Electron）

源码根：`desktop/src/main/`。桌面端是纯"壳"，复用服务器 + Web 前端全部业务逻辑，仅提供 Web 环境之外的桌面能力（尤其是**窗口最小化/隐藏时仍能弹系统通知**——浏览器标签被冻结时页面内 `Notification` 不会触发）。主进程模块通过 IPC（`native:*`）暴露为 `window.ClawBenchNative`，与 Android WebView 共用同一套前端接口（`web/src/utils/clawbenchNative.ts`）：

| 模块 | 职责 |
|------|------|
| `window.ts` | 主窗口创建、原生上下文菜单（cut/copy/paste 走 OS role，copy-link/copy-image 按语言翻译）、外部链接拦截交给默认浏览器 |
| `bridge.ts` | IPC 桥：服务器列表/凭据、SSH 端口映射、文件下载、分享、系统通知、主题、语言、日志捕获、屏幕常亮 |
| `tunnel.ts` | ssh2 客户端，读取 `/api/ssh/info` 建立 SSH 端口映射 |
| `download.ts` | 文件下载（保存对话框 + 下载后定位）、URL/Blob 下载 |
| `notification.ts` | 原生系统通知，点击导航到会话/任务（冷启动挂起派发）。窗口**可见且未最小化**时**抑制通知**（刻意不看焦点：窗口开着就不打扰）——用户开着应用，通知只会重复应用内完成卡片；判定必须在主进程做，渲染层的 `document.hasFocus()` 在最小化/隐藏窗口里仍可能为真 |
| `clientLog.ts` | 主进程日志回传：缓冲 POST `/api/client-log`（`source="electron"`）+ 写 `{userData}/desktop.log`；镜像渲染进程 console，`recordError` 上报未捕获异常 |
| `identity.ts` | `APP_USER_MODEL_ID`（Windows toast 身份），**必须与 `electron-builder.yml` 的 `appId` 一致**——该 yml 不随包分发，运行时读不到，漂移会让 Windows 通知静默消失；`identity.test.ts` 守住 |
| `updater.ts` | 升级检查：请求**服务端** `/api/desktop/latest`（不查 npm）；语义化版本比较，降级不误报 |
| `install.ts` | 自升级安装：多候选 URL 依次降级下载 → SRI 校验 → 解压 zip（剥顶层包装目录、拒绝路径穿越、**恢复可执行位**）→ 侧装到 `~/.clawbench-desktop/app-<version>/` → 翻转 `current` 指针 |
| `secrets.ts` / `store.ts` | safeStorage 加密存密码、electron-store 持久化服务器列表/主题/语言 |
| `powersave.ts` | 屏幕常亮（powerSaveBlocker） |

**自升级采用"侧装 + 指针"而非原地替换**：运行中的进程无法覆盖自身（Windows 上尤其如此）。`install.ts` 把新版本解压到独立目录并改写 `~/.clawbench-desktop/current`；`npm/desktop-main/bin/clawbench-desktop.js` 启动时读该指针决定运行哪个版本（指针缺失/目录不存在则回退到 npm 包自带版本），因此失败可回滚、旧版本保留。

**分发以 GitHub Release 为准，npm 只是可选渠道**：桌面端与服务端同版本发布，`/api/desktop/latest` 直接返回服务端自身版本 + Release 资产地址，**不查询任何外部服务**（不查 npm、不查 GitHub API）。这消除了对 npm 的依赖——Electron 44 的运行时让 tarball 达到 ~120MiB，逼近 npm 的体积上限。每个平台返回**候选 URL 列表**（国内镜像优先、直连 github.com 兜底），客户端与前端都取首个可用项。

`tag` 为空表示当前是 dev/未打标签构建（无对应 Release），此时 `downloads` 为空、客户端隐藏下载入口——不要把它当错误处理。

构建与发布：`desktop/` 用 electron-builder 的 `dir` target 产出免安装目录，由 `release.yml` 的 `build-desktop-*` 四个 job 打包为 zip 挂 GitHub Release。资产名必须与 `internal/service/desktop_upgrade.go` 的 `desktopAssetName` 完全一致，否则下载 404。`publish-npm-desktop` 仍发 `@xulongzhe/clawbench-desktop` 与 linux/win 平台包，但**带体积门控**（超 100MiB 跳过并 warning，不再让整个 release 失败）；darwin 从不发 npm。CI 构建前需 `ELECTRON_MIRROR` 走镜像，否则 `@electron/get` 从 GitHub 下载常中断。

## 开发规则

- **日志必须用封装**：前端一律 `appLog.d/i/w/e()`（`@/utils/appLog`），禁止原始 `console.*`；Android 一律 `AppLog.d/i/w/e()`，禁止 `android.util.Log`。两者的自身实现与测试除外。Tag 约定：短 PascalCase 模块名。
- **功能和 Bug 修复必须包含单元测试**：Go 用 `*_test.go`，前端用 `.test.ts`，放在对应代码旁。测试须验证具体行为，非泛化快乐路径。
- **改动 HTTP 接口必须同步 OpenAPI 文档**：任何新增 / 删除 / 修改 `/api/` 端点（路径、方法、鉴权、参数、请求 / 响应字段、状态码）都必须同步更新 `internal/api/openapi.yaml`（已从 `docs/spec/api/` 迁入以支持 `go:embed`）。
  - 字段名、参数名、方法**必须从 handler 代码里抄**（`decodeJSON` 结构体的 JSON tag、`r.URL.Query().Get(...)`、`requireMethod(...)` / `switch r.Method`），**禁止凭路由名望文生义**。
  - 路由唯一来源是 `internal/handler/handler.go` 的 `RegisterRoutes`；`internal/handler/openapi_drift_test.go` 双向校验路径与鉴权（**不校验字段名**）。
  - 完整维护清单见 `docs/spec/api/README.md`。
- **纯前端改动完成后必须自觉编译**：若只涉及 `web/src/`、`web/index.html` 等（未动 Go / Android），跑通测试后直接构建，供用户立即在浏览器 / App 中测试，无需用户再要求：

  ```bash
  npm run build        # 仓库根目录（web/package.json 的 build 会 cd .. 转调同一脚本）
  ```

  vite 的 `outDir` 是仓库根 `.clawbench-web/`（**不叫 `public`**，见下），服务端在 CWD 存在该目录时走 disk 模式（`frontend.GetFS()`）直接读它，因此构建后**立即生效，无需重启**。`internal/frontend/dist` 仅在 CWD 无该目录时被读取（单二进制分发场景）。
- **前端产物目录必须叫 `.clawbench-web/`**：这个路径是**相对进程 CWD** 解析的，所以名字必须唯一到不可能与无关目录撞车。`public` 这种通用名踩过真实的坑（issue #461）：macOS 自带 `~/Public`，而 APFS 默认大小写不敏感，于是从家目录启动时 `os.Stat("public")` 命中了系统目录，服务端放弃内嵌前端去读那个空目录，所有页面 404、App 卡在启动页。改名同时让老安装遗留的旧 `public/` 自动失效（不再被任何代码读取），无需用户清理。改名前请勿换回通用名；`TestDiskDirName_DoesNotCollideWithUnrelatedDirs` 会拒绝一批常见目录名。
- **前端编译完后必须嵌入到程序中**：`go:embed` 只在**编译期**读盘（`internal/frontend/embed.go` 的 `//go:embed all:dist`），所以磁盘上换了前端文件，**运行中的进程不会看到**——必须重新编译并重启。完整链路：

  ```bash
  ./build.sh --restart     # 构建前端 → 同步 embed → 重编 Go → 重启（一条命令全覆盖）
  ```

  只做纯前端迭代时，`npm run build` 后 disk 模式会立即生效（无需重启）；但要**分发**二进制、或要让 embed 内容跟上，必须走 `build.sh`。
- **embed 只在两种情况下需要手动处理**：
  - **APK**：`ServeAPK` 恒定读 `EmbeddedFS()`（`internal/handler/apk.go`），**不查 CWD**——`/api/apk` 下发的是**构建 Go 二进制时**嵌入的 APK，与磁盘 `.clawbench-web/` 无关，换了 APK 必须重编二进制才生效。且 APK 必须放进 `.clawbench-web/assets/`（源在 `public` 时代的同一相对位置）：`build.sh` 会 `rm -rf internal/frontend/dist && cp -r .clawbench-web internal/frontend/dist`，只存在于 `dist/` 的 APK 会被冲掉（实测发生过，`/api/apk` 静默退回 404），放在构建输出目录的 `assets/` 才随 `cp -r` 带回。
  - **可分发二进制**：`go build` / 交叉编译前必须同步 embed（`cp -r .clawbench-web internal/frontend/dist`），否则二进制内没有前端。
- **覆盖率门槛**：每 PR / 推送到 main 强制执行——包级覆盖率不低于基线、变更行覆盖率 ≥ 80%。
- **推送前必须运行本地检查**：`./scripts/pre-push-checks.sh`
- **跑全量测试前先评估成本，且不得与并发 agent 争抢**：主工作区可能同时有多个 agent 在改同一棵树，全量测试是**共享的稀缺资源**——实测全量 vitest 约 14 分钟、`go test ./internal/service` 约 128 秒、前端构建 1.5–4 分钟；并发时彼此争抢 CPU/内存，会把对方拖到超时（同一命令并发下撞 600s 超时，单独跑仅 128s）并产生假失败。
  - **先评估要不要跑全量**：能用隔离测试回答的问题（`npx vitest run <file>`、`go test ./internal/<pkg>/ -run <TestName>`）就不要跑全量。判「是否我引入的回归」一律先隔离单跑。
  - **跑之前先检查他人是否已在跑**：`ps -eo pid,ppid,etime,cmd | grep -E "vitest|go test|npm run build" | grep -v grep`。有则 `sleep` 等待其结束后再跑，不要并发起跑。
  - **不重复跑同一个全量**：复用已有结果，例如 `./scripts/check-go-coverage.sh --skip-test` 复用已生成的 `coverage.out`。
  - **绝不同时跑 `npm run build` 与全量 vitest**：会 OOM 被 Killed，并把结果污染成成片假失败。必须串行。
  - **判据**：若一个验证动作**不改变结论**，就不该跑。跑之前问「这个结果会让我改代码吗」，不会就别跑。
  - **baseline 陈旧导致的 Tier 1 失败不要靠补测试去「修」**：先确认归属（比对失败包是否被自己改过）；Tier 1-only 是 non-blocking（脚本以 `exit 2` 区分）。
