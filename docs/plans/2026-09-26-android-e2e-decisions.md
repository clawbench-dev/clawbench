# Android E2E harness 决策记录（h2 流隧道）

> 状态：**记录已定决策 + 未决产品问题**（本文件不改代码）
> 日期：2026-09-26
> 分支：`feat/ssh-ws-forward`
> 相关：`docs/plans/2026-09-25-h2-tunnel-design.md`、`docs/plans/2026-09-25-h2-tunnel-implementation.md`、`android-e2e/README.md`

本文件只记录**非业务决策**（为什么这么做）与**刻意未决的产品问题**（留待产品负责人），不是设计文档。所有事实均已对着仓库核验，行号为本文写作时的 HEAD（`f72b68cf`）。

---

## 1. 背景

`feat/ssh-ws-forward` 分支承载 h2 流隧道特性：在主端口（20000）上用 HTTP/2 承载 TCP 端口转发（`-L` / `-R`），网络层只需放行一个端口，SSH 通道保留为兜底。

特性完成时该分支有 **986 个 Android 单元测试**（`a4da7d5b` 时实测 `@Test` 计数 986），但**没有任何 instrumented/UI 测试**——`android/app/src/` 下只有 `debug/ main/ test/`，**没有 `androidTest/` 目录**——也没有端到端覆盖。为此新增了 `android-e2e/` 测试台（Docker 模拟器 + Appium），并把隧道测试扩到三层：Go 服务端、Android 单元、E2E。

---

## 2. 已定决策

### 2.1 模拟器镜像：`budtmo/docker-android:emulator_9.0`（Android 9 / API 28）

**理由：** 与仓库既有的 Robolectric `@Config(sdk = 28)` 约定对齐（`MainActivityApkInstallTest.java:50`、`ThemePaletteTest.java:37`、`MainActivityLanguageTest.java:31`、`BackgroundServiceStalePortsTest.java:50` 等）。

**后果：** 该镜像自带 **Chrome 69**（`69.0.3497.100`）。能自动化 Chrome 69 的 chromedriver 只有 2.42–2.44，而 Appium 3.x 是 W3C-only，2.44 是 JSONWP-first——两者协议不匹配。因此引入 **chromedriver 2.44 钉版**（`prepare-assets.sh` 下载 `chromedriver-244`）+ 一个 JSONWP 兼容垫片 `android-e2e/scripts/chromedriver-shim.py`（改写 `GET /status`，注入 `ready:true` 并报告 `build.version < 75`，让 Appium 的 `syncProtocol` 协商到 JSONWP）。`Dockerfile.emulator` 把 2.44 烤进镜像，Appium 通过 `appium:chromedriverExecutable` 指向垫片。

### 2.2 镜像拉取策略：客户端并行 range 拉取（`pull-image.sh`）

**理由：** 本开发容器的 docker CLI 连的是**宿主机** daemon，其配置的 HTTP 代理会 reset 每一个对外 HTTPS CONNECT，`docker pull` 直接失败；registry 镜像站在容器内有直连快出口，但单连接被限速。于是 `pull-image.sh` 起一个临时容器跑 `oci-pull-parallel.py`（24 连接分块拉取），把 tar 用 `docker cp` 拷出，再 `docker load`。

**后果：** CI 上用**普通 `docker pull`**（`.github/workflows/ci.yml` 的 "Pull emulator image" 步骤）——GitHub runner 的 daemon 有直接出口，不需要这套绕行；`pull-image.sh` 在镜像已存在时**空转**（`docker image inspect` 早退），所以 CI 预拉之后 `run.sh` 再调它是无操作。

### 2.3 不用 bind mount：改用 `docker build` + `docker cp` 交付文件

**理由：** 同上——docker CLI 连宿主机 daemon，`-v ./local:/container` 会按**宿主机**文件系统解析路径，挂出来是空目录。`docker build` 的构建上下文由客户端流式上传，不受此影响。

**后果：** APK / chromedriver / mock server / 测试代码都**烤进镜像**而不是挂载。这在 CI runner 上同样成立，所以是刻意保留的方案，不是临时妥协。

### 2.4 模拟器启动后禁用软键盘（IME）

**理由：** 登录页在 WebView 里，body 是 `min-height:100dvh; overflow:hidden; display:flex; align-items:center`（`android/app/src/main/assets/login.html:690-696`），而 `MainActivity` 是 `android:windowSoftInputMode="adjustResize"`（`AndroidManifest.xml:39`）。WebdriverIO 聚焦输入框时 WebView 自身的 `requestFocus` 会唤起 IME（`hw.keyboard=yes` 压不住，`mShowExplicitlyRequested=true`），窗口按 IME 高度收缩，垂直居中的表单塌成 ~3px 细条，下一次交互报 "element not interactable"。实测该缺陷导致 **3 次运行 1 次失败**（`1fd6925f` 提交信息："1 failure in 3 runs, ~789s each"）。

**后果：** `run.sh` 在启动后、跑测试前禁用所有 IME（step 6/8）；测试本身也用 `setValue` 驱动 DOM（不需要真键盘）。**应用侧的 CSS 脆弱性原样保留**——那是产品改动，不在本分支决定范围内。

### 2.5 CI 门控：`workflow_dispatch` 或 `run-e2e` 标签，`continue-on-error: true`

**理由：** 与既有的 Playwright `e2e` job 完全一致（`ci.yml:390-391` vs `:483-484`）。成本高（9.1 GB 镜像 + ~6 分钟启动 + 测试），且是新引入的 job，先以"只报红、不挡合并"的方式观察稳定性。

**后果：** 该 job 目前是**建议性**的；稳定后把 `continue-on-error` 翻成 `false` 即可（一行改动）。

### 2.6 Tier 1 资产暂存简化（本文件同批实施）

**改动前：** CI 的 "Stage harness assets" 步骤**无条件**执行 `./android-e2e/scripts/prepare-assets.sh --with-server`，含 Tier 1。原因：`run.sh` 当时做的是**裸 `compose build`**，会构建所有服务，而 `Dockerfile.server` 硬 COPY `assets/clawbench-server`——Tier 1 的暂存不产出该文件，全新 checkout（`assets/` 被 gitignore）下 COPY 失败、整个 run 在模拟器启动前就挂掉；本地被上一次 Tier 2 遗留的旧二进制掩盖。

**改动后：** `ce4c649c` 让 `run.sh` **只构建所选 tier 的服务**（`run.sh:121-125`：Tier 1 = `emulator mock runner`；Tier 2 = `emulator server server-no-h2 target runner`；`all` = 全部五个），Tier 1 不再触碰 `server` 服务，也就不再需要 `--with-server`。于是 CI 的暂存步骤改为**按 tier 分派**（`all|2` 带 `--with-server`，`1` 不带），省下 Tier 1 的一次 Go 链接（~10-20 s）。

**注意：** PR 运行没有 `github.event.inputs`，表达式回退到 `all`（`${{ github.event.inputs.android_e2e_tier || 'all' }}`），`all` 需要真实服务器，所以回退是安全的。

### 2.7 `b9dc9d0e` 的提交信息**不重写**

**理由：** `b9dc9d0e` 的提交信息把一个"空的会话级隧道错误"归因于 `TunnelErrorKind.isConnectionLevel()==false`。该归因已被证伪：真实原因是**每条连接的 dial 失败路径从不写 `BackgroundService.lastError`**（`H2PortForwardTransport.serve()` 捕获 `TunnelException` 后只记日志、`closeBoth()` 返回——该文件全文无 `lastError` 引用）；而 `isConnectionLevel()` 本身是**死代码**（见 §3.4）。修正一个注释不值得重写已发布的提交历史（需 force-push）。

**后果：** 更正落在代码注释（`6e3a3de9` 重写了 `tunnel.forward.mjs` 的那段注释）与本文件里。

### 2.8 协调者 spec 中的一条错误指令被实施者推翻

**记录（流程说明）：** 协调者的 spec 要求实施者在"target down"用例里**断言会话级错误类型非空**（"there the error fields SHOULD be asserted (a non-empty error type is the whole point)"）。这会让套件**必红**——因为该路径本就不写会话级错误，断言非空等于断言一个 bug。实施者正确地改为：断言**每条连接**的 logcat 分类（`TARGET_UNREACHABLE` + `502`），并保持**会话级错误为空**。

**为什么值得记：** 它证明测试台的断言是独立推理出来的，不是照抄 spec 实现。

---

## 3. 未决产品问题（本文件不做决定）

以下都是**行为变更**，故刻意保留原样。每条给出发现、证据（file:line）与选项，不替产品选。

### 3.1 `SameSite=Lax` 导致 `getCookie()` 读不到会话 Cookie

**发现：** 在 API 28 / Chrome 69 上，`CookieManager.getCookie()` 会**省略任何带 `SameSite=Lax`（或 `Strict`）的 Cookie**，无论写入者是 app 的 `setCookie()` 还是页面的 `document.cookie`（实测 2×2）。服务端**恒定**设置 `SameSite=Lax`（`internal/handler/auth.go:241`），所以 app 读不到自己刚拿到的会话 Cookie，该镜像上**全部 14 个 `getCookie()` 调用点**都处于未认证状态：隧道、原生 WS 推送（`BackgroundService.connectNativeWs`）、待处理事件（`PendingEventsWorker`）、下载（`MainActivity.downloadFileViaManager`）、分享（`shareFile`/`shareFiles`）、`ShareIn`、沙箱（`openInSandbox`）、`AppLog` 中继。

隧道实现本身**是正确的**——由"去掉 `SameSite` 的对照服务器"证明（`go build -overlay`，生产源码未改），该对照下真实登录能端到端送字节、无需任何夹具。

**选项：**
- (a) **登录时持久化 token**，不再回读 `CookieManager`（`MainActivity.handleAuthResponse` 已拿到 `Set-Cookie`）。一次修复全部 14 个调用点——`android-e2e/README.md` 的推荐。
- (b) 去掉会话 Cookie 的 `SameSite`。
- (c) 保持现状，靠测试台夹具。

**未验证：** 是否在**现代 Android** 上复现——只测过 API 28。

### 3.2 `-L` 端口校验不对称

**发现：** `addReversePortForward` 校验端口（`BackgroundService.java:2111`：`serverPort <= 0 || serverPort > 65535 || targetPort <= 0 || targetPort > 65535`），但 `addPortForward`（`BackgroundService.java:1866`）与桥 `MainActivity.addForwardedPort`（`MainActivity.java:2749`）**不校验**。明显非法的值（-1、65536）会一路到达 `new InetSocketAddress(LOOPBACK, localPort)`（`H2PortForwardTransport.java:157`），抛出**非受检**的 `IllegalArgumentException`，而不是类型化失败。端口 0 对 `-R` 合法（OS 分配），对 `-L` 不合法（`H2PortForwardTransportTest.addLocal_portZero_isAcceptedAsAnEphemeralRequest` 钉住了当前"接受 0"的行为）。

**选项：** 给 `-L` 补上与 reverse 路径对称的校验；或把该非受检异常记为预期行为。

### 3.3 超长控制行会终止整条控制流

**发现：** `H2TunnelStream.java:882` 用 `source.readUtf8LineStrict(MAX_CONTROL_LINE)` 读控制行，超限时抛 `EOFException`，而 `:883-886` 把它当作**干净 EOF** 处理并 `return`——于是控制流结束，客户端已绑定的**每一个**反向端口全部被释放。这与紧接其下的注释（`:888-894`："Malformed line: skip it … One bad byte must not drop every reverse port"）**自相矛盾**。

服务端有同样的 64 KiB 上界：`internal/handler/tunnel_control.go:401`（`scanner.Buffer(make([]byte, 0, 4096), 64*1024)`），超长行使 `Scan()` 返回 `bufio.ErrTooLong`、`readControlLoop` 结束，同样释放全部已绑定端口（由 `TestTunnelControl_OverlongControlLineDropsStream` 钉住）。

**选项：** 跳过坏行而非结束流；或接受并记录该行为。

### 3.4 死的错误分类代码

**发现：** `TunnelErrorKind.uiType()`（`TunnelErrorKind.java:106`）与 `isConnectionLevel()`（`:122`）**零生产调用点**（只有定义与单元测试）。`BackgroundService` 只存 `e.getMessage()`（`BackgroundService.java:1960` 等），所以 h2 传输的精确错误类型从未到达前端；`getErrorType()`（`BackgroundService.java:682`）再从这条消息的小写子串匹配里**重新推导** `auth|network|hostkey|unknown`。`isConnectionLevel()` 的分类本身是正确的，但没被使用——它编码的正是"会话级拆除策略"所需的那条判断。

**选项：** 把分类后的错误类型接到桥；或删掉这两个死方法。

### 3.5 `tunnelGuard()` 硬编码 `SSHPort = model.ServerPort + 1`

**发现：** `internal/handler/tunnel_control.go:318-321` 的 `tunnelGuard()` 把 `SSHPort` 设为 `model.ServerPort + 1`，而非**配置的** SSH 端口。因此自定义的 `port_forward.port` 只能靠 `SetReservedPorts` 得到保护。已由 `TestTunnelGuard_ReservesMainPortPlusOneNotCustomSSHPort`（`internal/handler/tunnel_control_test.go:1112`）钉住，未改动。

**选项：** 改用配置的端口；或记录该行为。

---

## 4. 验证状态

| 层 | 结果 |
|---|---|
| Tier 1 E2E | 5 个用例（`android-e2e/tests/smoke.login.mjs`） |
| Tier 2 E2E | **31** 个用例，跨 3 个 spec 文件（`tunnel.server.mjs` 10 + `tunnel.forward.mjs` 13 + `tunnel.reverse.mjs` 8） |
| Android 单元 | 分支总计 **1032** 个 `@Test`（`origin/main` 为 804；CI 接线时 `a4da7d5b` 为 986） |
| Go 隧道/服务端 | `internal/tunnel`（`-race`）ok；`internal/handler -run TestTunnel*`（`-race`，24s）ok；`cmd/server` ok |

**CI job 状态：** 已在本地验证（`yaml.safe_load` 解析 + actionlint），但**从未在 GitHub 上真正跑过**（分支未推送）。

**已知盲区：**
- `tls` 线上路径**从未被覆盖**——测试服务器是纯 HTTP，只观测到 `h2c` 胜出（E2E 断言同时接受 `tls`/`h2c`，实际打印值恒为 `h2c`）。
- `internal/handler` **全量**（非 `-run TestTunnel*`）在本环境有**与分支无关的**预存失败：`TestServeFontFile_UnreadableFile`（root 下 `chmod 000` 不生效的环境性失败）以及 ACP/队列/STT 若干用例的 data race（`internal/service/database.go` 的 `SetDBForTest` 与写入 goroutine 竞争）。这些文件相对 `origin/main` **逐字节未改**，隔离单跑同样复现，不属本分支引入。
- 测试台夹具（可读的、无 `SameSite` 的 Cookie）是必要的，因为 §3.1 的镜像缺陷；套件**不断言该缺陷存在**，所以 app 侧读修好后它仍保持绿色。
