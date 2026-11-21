# AGENTS.md

## 这是什么

ClawBench 是多端 AI 工作台：把一批 AI CLI 工具（CodeBuddy、Claude Code、OpenCode、Codex、Qoder、Kimi、Cursor、Copilot 等 21 个）封装成 Web 平台。Go 后端调 CLI，WebSocket 推流，Vue 3 前端实时渲染。另有群聊、会议与 AI 听记、SSH 隧道、任务系统、邮件、RAG 等模块。

**规格文档在 `docs/spec/`（索引：[`docs/spec/README.md`](docs/spec/README.md)）。本文件只写「不写就会做错事」的内容，凡是可以从代码或 spec 查到的，一律不给。**

## 常用命令

```bash
./build.sh                              # Go + Vue + Excalidraw 全量构建
./build.sh --restart [--restart-skip-build]   # 编译并后台重启
./dev-server.sh [--fg|--stop|--restart]       # Vite HMR
go build -o clawbench ./cmd/server      # 仅 Go 二进制
./clawbench [--port 8080] [--data-dir /tmp/x] # 直接运行，默认端口 20000

go test ./...                           # 单包：go test ./internal/ai/...
npm test                                # Vitest
./scripts/pre-push-checks.sh            # 推送前全量检查
```

## 五条硬约束

**1. 不用 cgo。** 所有产物 `CGO_ENABLED=0`，发布流水线和 `build.sh` 都是。SQLite 用 `modernc.org/sqlite`，FTS5 已编入，代码里没有 `import "C"`。开了 cgo 产物就不再只由我们的代码决定——issue #719 就是这么来的（linux 版在无 AVX 的 CPU 上启动即崩，Go 侧信息全正常）。要新增 cgo 依赖先在 issue 说清理由并改本节。代价：纯 Go 域名解析绕开 NSS，`.local` 和只靠 NSS 插件解析的内网地址会解析不到。

**2. 一个数据目录只能跑一个服务。** 启动时取独占锁 `.instance.lock`，已有实例在跑就等 30 秒后拒绝启动。原因不是抢端口——两个实例共用一个数据库会互相破坏：启动恢复把对方正在跑的那一轮改写成「已取消 + 服务重启中断」。本地做实验必须显式给 `--data-dir /tmp/<临时目录>`。

**3. 全文合并 PR，绝不用 squash。** `gh pr merge --merge`。squash 丢掉每步「为什么这么改」的历史，出问题没法二分、没法单独回滚。开了 Auto-merge 也要确认方法不是 SQUASH。

**4. 跑全量测试前先评估成本，不得与并发 agent 争抢。** 主工作区可能同时有多个 agent。实测全量 vitest 约 14 分钟、`go test ./internal/service` 约 128 秒，并发会互相拖到超时并产生假失败。
- 能用隔离测试回答的（`npx vitest run <file>`、`go test ./internal/<pkg>/ -run <T>`）就不跑全量。
- 跑前检查：`ps -eo pid,ppid,etime,cmd | grep -E "vitest|go test|npm run build" | grep -v grep`，有则等。
- **绝不并行跑 `npm run build` 与全量 vitest**——会 OOM 被 Killed，结果污染成成片假失败。
- 判据：**这个验证动作不改变结论，就不该跑。**

**5. 日志必须走封装。** 前端 `appLog.d/i/w/e()`（禁止裸 `console.*`），Android `AppLog.*`（禁止 `android.util.Log`），桌面 `clientLog`。自身实现与测试除外。三个客户端日志汇入 `{data-dir}/logs/client.log`，开关是设置里的「调试日志捕获」（`logCapture`，默认关）。**反直觉的一条**：Android 跳过 console、桌面不跳过——桌面照搬会让 `desktop.log` 恒为空。细节见 [`docs/spec/client/android-integration.md`](docs/spec/client/android-integration.md)。

## 架构：只记这些

**后端入口** `cmd/server/main.go`，HTTP 端点在 `internal/handler/`，全部业务逻辑在 `internal/service/`（最重）。各包职责查 [`docs/spec/README.md`](docs/spec/README.md)，不要凭包名猜。

改 `internal/service/` 前先读 [`docs/spec/core/`](docs/spec/core/) 与 [`docs/spec/features/`](docs/spec/features/)。四条改了会真出问题的：
- **会话运行态只有一个归属**（`session_runner.go`），「能不能取消」与「在不在跑」同源——不要另起一份运行态。
- **项目以整数 id 为身份**，作用域表存 `project_id` 而非路径；`project_id=0` 是全局标签哨兵。
- **排队消息存独立表 `queued_messages`**，出队才落 `chat_history`，claim 在同一事务里 DELETE + INSERT——这是「历史行 id 顺序恒等于对话顺序」的唯一保证。
- **群成员是隐藏的 `chat_sessions` 行**，消息只存群会话。

**AI 后端**在 `internal/ai/` + `backends/`（21 个后端子包）。改之前**必读** [`docs/spec/core/ai-backend.md`](docs/spec/core/ai-backend.md)（77KB，是这个项目最复杂的一块）。四条最容易踩：MCP 注入有**四条**建会话路径（new/load/resume/SyncLoadSession），漏一条表现为「进程重启后工具消失」；**不做帧级内容去重**（wire 上「重试」和「模型又输出一遍」字节相同，误删过真实内容，已整体移除）；后端差异用**能力位**表达，不要写 `if backend == "xxx"`；ACP 连接缓存用 leaf lock `stateMu`，**绝不在 RPC 期间持有**。

**内置智能体**在 `internal/agentcore/` + `ai/backends/builtin/`（进程内实现，不走 CLI 也不走 ACP）。**完整设计见 [`docs/plans/2026-11-16-builtin-agent-design.md`](docs/plans/2026-11-16-builtin-agent-design.md)，改之前先读**——权限模型、路径边界、上下文压缩全在里面，最易改错的三条：权限按能力位而非工具白名单（「只读 bash」靠命令白名单挡不住）；**零值 = 全关**（漏配必须 fail closed，与外部后端默认全权限相反）；未授权的工具**根本不声明给模型**。

**前后端镜像对**（改一侧必须同步另一侧，否则同一段文本前后端解析不一致）：
- `internal/askquestion/` ↔ `web/src/utils/askQuestion.ts`
- `internal/grouprouting/` ↔ `web/src/utils/groupRouting.ts`

两侧由共享语料 `testdata/parity_corpus.json` 双向固化，有测试钉住。

**前端**：`web/src/`，无 Vue Router，基于抽屉的单页布局，单一 `reactive()` store。**同一时刻只允许一个标签页跑应用**（`useSingleTab.ts` 选举，第二标签页只显示阻塞屏）——服务端按 `client_id` 键控 WS 订阅槽，两标签页会互顶 socket（实测 17 分钟 1402 次重连）。合入口径、平台判定（HOST / INPUT / VIEWPORT 三条正交轴，**没有 `isPC`**）见 [`docs/spec/client/frontend-architecture.md`](docs/spec/client/frontend-architecture.md)。

**界面缩放只有根字号一个机制**（`utils/uiScale.ts` 写 `--ui-scale`，几何尺寸用 `--dp` 表达）。CSS `zoom` 与 Electron `setZoomFactor` **都已废弃**——不缩放布局视口，会让 `100vw`/`dvh`/媒体查询全部失真。**改样式前必读 [`docs/spec/client/design-guide.md`](docs/spec/client/design-guide.md)**。

**桌面端**是纯壳（`desktop/src/main/`），复用服务器 + Web 全部逻辑，只多做 Web 做不到的事（尤其**最小化时仍能弹系统通知**）。完整设计见 [`docs/spec/client/desktop-client.md`](docs/spec/client/desktop-client.md)。改之前必须知道：
- **捕获源声明是一次性的**（`set-pending-capture-source` 消费后立即清空），否则任何页面后续都能静默抓屏；共享期间要关掉后台节流，否则最小化后编码器停摆、观众画面冻结。
- **主进程没有 `document.cookie`**，必须经 `clientLog.getCookieHeaderForUrl(url)` 取**全部**适用 Cookie（只取 session 名会缺 `clawbench_project` → 403）。
- **悬浮胶囊拖动由主进程驱动**，不能用 `-webkit-app-region: drag`（会把拖拽区交给 OS 当标题栏，DOM 收不到 click）。
- **通知只在「窗口可见且未最小化」时抑制**，刻意不看焦点；判定必须在主进程做。
- **协议号只在破坏性契约变更时 +1**。`release.yml` 的 `client-guard` 靠这个语义决定「编还是搬」；**契约文件改了但协议号没变 → release 直接失败**。
- **`APP_USER_MODEL_ID` 必须与 `electron-builder.yml` 的 `appId` 一致**，漂移会让 Windows 通知静默消失。**不能改 `productName`**（会挪 userData 目录、丢配置）。

**桌面壳 ≠ 手机 App 模式**：两者都经原生桥识别为原生环境，但省电策略相反——Android 退到后台会被挂起，故隐藏时断开 WebSocket；桌面窗口只是最小化、进程仍在跑，断开 WS 等于自断通知来源。门控写成 `isAppMode && !isDesktopApp`，漏一层就让最小化后的推送静默失效。

**Windows 融合版**（一体包自带服务端）：服务端以 `--managed` 启动，`/api/upgrade/start` 与 `/api/config/restart` 一律 403，`/api/upgrade/check` 返回 `managed: true`——这两条路会换掉安装包里的二进制，客户端就管不住进程了；**版本拦截里的「下载」也一样拒绝**（下载源都是瘦包，会把 `server/` 弄丢，融合版只能整包重装）。内嵌服务端**不是普通服务器条目**（存 `embeddedServerUrl`，不在 `servers` 列表），旧版本当普通条目过，导致删除后自动补回、改名造假条目、改密码无效。退出靠关 stdin（`--exit-on-stdin-close`）。详见 [`docs/spec/client/desktop-client.md`](docs/spec/client/desktop-client.md)。

**鸿蒙端**（`harmony/`，实验性，不进 release 流水线）：壳里只有系统能力，界面是同一份 Vue 前端。**新增原生事件必须加进 bootstrap 的 `nativeEvents` 白名单**，否则静默丢弃。宿主身份独立于 Android（`isHarmonyApp` 与 `isAndroidApp` 互斥，误并会启用安卓专属补偿与 APK 更新入口）。原生隧道只能由原生层做（ArkWeb 建不了 SSH），**不接受跳过 TLS / 主机校验换连通**。能力表报 `false` 的项是签名级权限受限，**不要用应用内弹层或空实现冒充**。口径以 `docs/harmonyos/FEATURE_PARITY.md` 为准。

## 开发规则

**加功能（页签 / 服务端总开关）先读 [`docs/plugin-dev/`](docs/plugin-dev/README.md)。** 前端 `web/src/plugins/<id>/`，后端 `internal/plugin/<id>/` 并在 `all.go` 加一行。

**改 HTTP 接口必须同步 `internal/api/openapi.yaml`。** 字段名、参数名、方法**必须从 handler 代码里抄**，禁止凭路由名望文生义。路由唯一来源是 `RegisterRoutes`，`openapi_drift_test.go` 双向校验路径与鉴权（不校验字段名）。

**功能和 Bug 修复必须带单元测试**（Go `*_test.go`，前端 `.test.ts`，放代码旁），验证具体行为而非快乐路径。遇到脆皮测试当场修掉——脆皮 = **结果取决于与断言无关的因素**；修法是消除不确定性，**不是**调大超时压绿；若超出任务范围，留 `TODO(flaky): <原因>` 并在交付说明里点出。

**E2E**（`e2e/`，Playwright）：涉及跨组件流程或真实后端行为时补 spec。CI 每 PR 跑全量 3 浏览器（3 个并行 job，约 13 分钟）；**本地只跑本次新增或受影响的 spec**，全量约 33 分钟且会与并发 agent 争资源。改了 Go 代码先 `go build -o clawbench ./cmd/server && go build -o acp-mock ./cmd/acp-mock`，否则跑的是旧二进制。

**纯前端改动完成后必须自觉构建**（`npm run build`），供用户立即在浏览器/App 里测，不要等用户要求。产物目录必须叫 `.clawbench-web/`——这个路径**相对进程 CWD** 解析，通用名踩过真坑（issue #461：macOS 的 `~/Public` + APFS 大小写不敏感，从家目录启动时命中系统目录，所有页面 404）。**`go:embed` 只在编译期读盘**，改了前端要让 embed 跟上或要分发二进制，必须走 `build.sh`；APK 恒定读 embed，换了 APK 必须重编二进制，且要放在 `.clawbench-web/assets/`。

**覆盖率**：包级不低于基线，变更行 ≥ 80%。推送前跑 `./scripts/pre-push-checks.sh`。
