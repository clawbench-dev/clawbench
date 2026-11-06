# ClawBench 鸿蒙端开发指南

更新日期：2026-11-06

状态：当前开发版 `0.2.4 / 204`，沿用包名 `com.clawbench.harmony`，支持手机和平板。本版本不接入 Push Kit，启动、登录和已有能力不依赖云端配置。未签名构建与 270 项回归测试通过；系统冻结或结束进程后的后台通知仍为已知限制，未作为修复通过。

0.2.3 修复评审发现的登录凭据恢复、真实宿主桥契约、TLS 错误分类和双语反馈，并补充上游测试与编译入口。保留安卓登录页、全面屏及已有原生系统能力；0.2.1 的启动通知授权和 0.2.2 的原生推送配置同步属于此前实现。构建脚本自动导出 `release/应用名-版本号-签名状态.hap`，目录始终位于项目根目录，不随调用位置变化。构建和设备证据以维护记录为准；后台消息问题及后续章节中尚未验收的内容仍是开放要求。

## 1. 目标与验收口径

目标是在 HarmonyOS 原生应用中完整复刻安卓客户端，包括界面、业务功能、系统交互、异常处理和状态恢复。不能仅以“可以打开服务端网页”作为完成标准。

- 页面信息架构、底部 Dock、弹窗、主题、语言、字体、交互反馈以安卓客户端为对照。
- 聊天、群聊、项目、文件、终端、Git、任务、会议、邮件和设置均进入对齐范围。
- 上传下载、分享、通知、端口映射、媒体权限、录屏、返回手势等原生能力单独验收。
- 系统不支持或需要额外资格的能力记录为“受限”，写明原因和替代体验；不能计为完整复刻。
- 浏览器测试、模拟器测试、真机测试分别记录。编译成功不等于真机可用，前台可用不等于锁屏后可用。

详细进度见 [功能对齐清单](FEATURE_PARITY.md)。阶段拆分只决定开发顺序，不缩减最终目标。

## 2. 已确认的安卓实现

本节依据上游提交 `252b5289424fcf86cf935b722e09fa48d8ecee32` 的静态核查；该提交在 2026-11-06 同步时为默认分支 HEAD，不代表已运行安卓客户端。

| 事实 | 本地上游源码位置，相对于 `clawbench-source code/` |
| --- | --- |
| 安卓主界面由 WebView 承载，原生层加载服务器页面 | `android/app/src/main/java/com/clawbench/app/MainActivity.java` |
| 原生对象名为 `window.ClawBenchNative` | 同上，`addJavascriptInterface` 注册处 |
| 安卓登录页本身也是打包的 HTML，由 WebView 加载 | `android/app/src/main/assets/login.html`、`url-utils.js`，`MainActivity.java` 的 `LOGIN_HTML_URL` |
| 统一桥接口和包装函数由前端维护 | `web/src/utils/clawbenchNative.ts` |
| 原生宿主判断目前覆盖 Android 和 Electron，非桌面原生宿主被归类为 Android | `web/src/composables/useAppMode.ts`、`usePlatformDetect.ts` |
| 页面是 Vue 单页应用，返回逻辑采用自有状态机 | `docs/spec/client/unified-back-navigation.md` |
| 安卓后台事件、隧道、补偿轮询由原生模块负责 | `BackgroundService.java`、`PendingEventsWorker.java`，位于上述 Java 目录 |
| 安卓支持屏幕共享、独立浏览器、悬浮状态和实时通知 | `ScreenShareService.java`、`BrowserActivity.java`、`FloatingStatusController.java`、`LiveUpdateManager.java` |
| 连接前读取 `/api/health`，比较客户端协议号 | `MainActivity.java`、`android/app/build.gradle` |
| 当前本地快照的协议号为 `1` | `internal/version/client_protocol.txt` |
| API 路由和接口规格可从服务端核对 | `internal/handler/handler.go`、`internal/api/openapi.yaml` |

原生桥文档存在简写与旧命名，实施时以 `clawbenchNative.ts`、Java 实现和实际调用方共同确认契约。例如 `downloadBlob` 的参数顺序是 `(base64Data, fileName)`，不能照概述文档反向传参。

`package.json` 的版本字段不能用来认定上游发布版本。当前参考目录由锁定提交的归档覆盖而来，详见 [上游基线记录](UPSTREAM_BASELINE.md)。

## 3. 技术路线

首选方案为 Stage 模型应用，使用 ArkTS / ArkUI 编写原生壳，由 ArkWeb 承载服务端现有 Web 界面，通过兼容 `ClawBenchNative` 的桥接层提供鸿蒙系统能力。

```text
HarmonyOS 应用
  ArkUI / UIAbility: 启动、权限与系统生命周期
    独立本地 ArkWeb: 安卓登录页资源 + 登录消息桥
    远程 ArkWeb: ClawBench Vue 工作台 + HarmonyOS 宿主桥
      凭据 / 文件 / 通知 / 生命周期 / 隧道 / 媒体
        ClawBench 服务端 HTTP + WebSocket
```

这样可以保持安卓和鸿蒙的页面结构、主题与业务行为一致，主要移植工作集中在原生宿主和平台判断。AI CLI、Go 服务端、任务执行和服务器文件系统继续运行在服务端。

第一阶段沿用安卓的远程页面加载模式。离线时必须能打开本地连接页、查看已保存服务器并重试，不承诺离线运行服务端业务。若以后打包 Web 资源，需要单独解决 API 基址、Cookie、跨域、WebSocket、缓存和升级兼容问题。

### 3.1 平台识别必须先改

现有 `isAppMode && !isDesktopApp` 会把鸿蒙识别为安卓。不能只注入 `isNativeApp() = true` 就认为适配完成。

已增加明确的宿主类型与能力查询，同时兼容旧宿主：

- 宿主类型区分 `web`、`android`、`electron`、`harmonyos`。
- 分开判断宿主、输入方式和视口宽度，延续上游三轴设计。
- Android 专属权限引导、APK 更新、OEM 设置、WebView CSS 补偿不得自动作用于鸿蒙。音量键使用单独的 `volumeKeys` 能力，由鸿蒙自己的按键事件实现。
- 普通原生共享能力按能力判断；新能力缺失时返回明确的不支持状态。
- 在页面初始化前完成宿主识别。对旧服务器前端设置兼容门槛，不能靠伪造安卓 UA 掩盖问题。

适配已整理成 `patches/upstream/0001-harmonyos-host.patch`，并通过本机代理在模拟器验证。共享服务器尚未替换前端。仅在鸿蒙壳中新增方法不能改变旧前端的分类逻辑，旧前端因此保持网页模式；0.1.2 已移除原生工具栏，冷启动、本地返回和适配前端的管理入口提供服务器选择。

详细路线和框架评估见 [安卓界面移植与框架评估](ANDROID_UI_MIGRATION.md)。

### 3.2 工程边界

目录结构如下；复杂系统服务按阶段继续补齐：

```text
README.md
docs/
  HARMONYOS_DEVELOPMENT_GUIDE.md
  FEATURE_PARITY.md
  MAINTENANCE.md
  UPSTREAM_BASELINE.md
harmony/                       # 已创建的 DevEco Studio 工程
  AppScope/
  entry/src/main/
    ets/entryability/
    ets/pages/
    ets/bridge/
    ets/services/
    ets/models/
    resources/
patches/upstream/               # 可审阅的必要上游适配差异
scripts/                       # 实际验证过的构建和适配脚本
local/                         # 私有配置，忽略
artifacts/                     # 构建与本地验收材料，忽略
clawbench-source code/          # 上游参考源码，忽略
```

原生桥只做参数校验、分发和结果编码；存储、网络、通知、文件、隧道、媒体各自拥有生命周期。避免把所有业务放进一个页面或 UIAbility。

### 3.3 全面屏与键盘

0.1.3 在主窗口启用 `setWindowLayoutFullScreen(true)`，系统状态栏与导航指示器继续可见。页面从系统查询状态栏、挖孔和导航指示器的实际避让矩形，以各边最大值转换为 vp 后统一留白；不累加重叠区域，不写死高度，不把 Web 组件再次扩展进交互安全区。监听 `avoidAreaChange` / `windowSizeChange`，离页移除监听。

键盘采用 ArkUI `KeyboardAvoidMode.RESIZE`。键盘出现时不再额外预留底部导航区，Web 百分比尺寸随可用窗口缩小，避免重复扣减；登录页允许纵向滚动，短屏和软键盘下表单仍可访问。

`system-bars.js` 只在可信宿主页面运行，读取实际顶栏 / Dock / 页面背景，通过既有消息端口更新系统栏背景和有对比度的黑白图标。颜色须为六位十六进制，原生再次校验；离页停止观察与迟到更新。不改变共享前端的字号、缩放或安全区 CSS。

### 3.4 原生外观与双语范围

`EntryAbility.onCreate()` 当前显式设置 `COLOR_MODE_DARK`，用于保持原生启动、连接和加载界面的深色外观；这不是跟随系统主题的实现。登录页和远程工作台继续使用自己的网页主题，系统栏颜色由实际页面同步。原生弹窗和控件随网页浅色主题完整切换尚未完成，不能把网页主题切换算作原生主题全部对齐。

0.2.3 通过 `models/NativeMessages.ts` 统一连接、凭据存取、通知、文件和分享等错误的中英文文本，并按当前保存的语言显示。语言切换即时更新文件操作反馈、通知标题与接收状态错误，不必重新连接服务器。未知 SDK 错误不直接当作密码错误；连接错误先转换为稳定代码，再交给登录页显示。新增语言键必须同时提供中英文并覆盖错误路径，不修改打包后的生成资源来替代源码。

## 4. 环境与工程初始化

1. 安装并检查 DevEco Studio、HarmonyOS SDK、配套 Node.js、ohpm、Hvigor 与 hdc。以所选 SDK 的官方配套要求为准。
2. 确认目标为 HarmonyOS 应用，记录最低系统版本、API 版本、设备类型和实际真机系统版本；不要直接套用 Android SDK 级别或 OpenHarmony 的可用性结论。
3. 在 `harmony/` 创建 Stage 模型的 ArkTS 工程，确定稳定的 bundleName、模块名、版本名和递增版本号。
4. 提交工程配置及依赖锁文件；SDK 路径、账号和签名密码放在忽略的本地文件或本机环境中。
5. 先完成最小应用构建、安装和启动，然后引入 ArkWeb；每完成一个可构建版本，都更新维护记录并本地提交。

已确认 DevEco Studio 26.0.0.851、配套 SDK 26.0.0.105 / API 26。当前配置兼容 API 23、目标 API 23、编译 API 26；使用 API 23 的 HTTP 禁止自动重定向选项。包内 `pack.info` 已核对，最低系统实际可运行性仍需 API 23 真机验证。

0.2.4 的 `AppScope/app.json5` 沿用 `bundleName: "com.clawbench.harmony"`、`versionName: "0.2.4"` 和 `versionCode: 204`；`entry/src/main/module.json5` 的 `deviceTypes` 为 `["phone", "tablet"]`。`compileSdkVersion` 使用当前 DevEco 接受的 `"26.0.0"` 格式，兼容和目标版本保持 `"6.1.0(23)"`。手机和平板的配置支持尚不能替代布局、交互与系统能力的设备验收。

普通偏好与敏感数据分开存储。服务器名称、主题、语言可使用偏好存储；密码和令牌采用所选 SDK 提供的安全资产存储或密钥保护方案。具体 API 与权限须在实施时对照官方文档和真机确认。

## 5. 原生桥与数据边界

### 5.1 桥接契约

以 `web/src/utils/clawbenchNative.ts` 为契约入口，结合安卓实际暴露的方法逐项建立测试。不是接口中的每个方法都需要移植：Electron 独占的方法应明确标为不适用。

| 方法组 | 主要方法或事件 | 鸿蒙要求 |
| --- | --- | --- |
| 宿主与启动 | `isNativeApp`、`getAppVersion`、`dismissSplash` | 宿主识别正确；原生启动遮罩有独立超时和失败恢复 |
| 服务器与认证 | `getServerList`、`saveServerNamed`、`getSavedServerConfig`、`connectToServer`、`getPassword`、`setSSHPassword` | 按服务器隔离凭据，断网仍可切换服务器；网页重新认证后更新当前隧道密码 |
| 外观与屏幕 | `getLanguage`、`setLanguage`、`getTheme`、`setTheme`、`setKeepScreenOn` | 原生与网页同步；退出相应业务后释放常亮 |
| 导航与选区 | `getPendingNavigation`、`openSession`、`showSelectionMenu` | 冷启动等待登录和页面就绪，选区位置适应缩放 |
| 下载与分享 | `downloadFileWithProgress`、`cancelDownload`、`downloadBlob`、`shareFile`、`shareFiles`、`shareText` | URI 授权、取消、进度、失败清理、中文文件名 |
| 隧道 | `addForwardedPort`、反向映射、`getForwardedPorts`、`reconnectTunnelAsync` | 真实可达性、端口冲突、连接代次与重连恢复 |
| 事件与通知 | `setNativePushEnabled`、`updateLastSeenEventId`、`dismissEventNotification` | 重放去重，已读撤销，点击定位 |
| 状态展示 | `setFloatingWindowEnabled`、`setLiveUpdateEnabled` | 评估系统能力，不用空实现报告成功 |
| 录屏 | `isScreenCaptureSupported`、`startScreenCapture`、`getScreenCaptureState`、`takeScreenFrame`、`takeSystemAudio` | 用户授权、捕获状态、停止释放、性能与后台验证 |
| 调试 | `startLogCapture`、`stopLogCapture`、`log` | 默认关闭回传，凭据脱敏，容量上限 |

同步读取与异步动作不可随意互换。`isNativeApp()`、语言等启动判断依赖同步结果；其他部分由前端 `await` 包装兼容。鸿蒙代理若只能异步提供某些结果，应在可信页面启动前准备同步快照，或显式修改调用契约。

远程工作台提供同步宿主 / 外观快照和异步消息端口，使用 `ClawBenchHarmony` 名称，仅适配后的前端读取。原生层不向远程页面提供已保存密码：`getPassword()` 返回空字符串，登录在原生层完成，网页使用 ArkWeb Cookie。凭据过期后的重新认证完整流程仍需继续验收。

0.2.3 补齐共享接口要求的 `setSSHPassword(password): Promise<void>`。真实 bootstrap 发出 RPC，原生在校验当前文档和连接后立即更新会话内隧道密码并注册日志脱敏，再按调用时捕获的服务器地址串行保存安全凭据。已接收的写入不会因页面关闭而丢弃，也不会改写新服务器；保存、删除和后续登录共用写入队列，重新打开登录页先等待在途写入完成。存储失败通过错误应答和用户提示报告，不退回已失效的旧隧道密码。现有网页调用可能不等待 Promise，因此 bootstrap 另行观察拒绝并发出 `clawbench-native-error`，同时保留调用方 `await` 时的失败语义。

`tests/harmony-bootstrap.test.mjs` 直接执行打包资源，并使用 TypeScript AST 读取共享 `ClawBenchNative` 接口，检查每个非可选成员真实存在且方法可调用。接口文件缺失时测试失败，不静默跳过。该测试支持本地参考源码布局和上游 `harmony/tests` 布局；方法参数、结果与失败传播另由行为测试覆盖，成员齐全不代表所有平台能力都已验收。

0.1.2 的本地登录文档复用安卓同步 `ClawBenchNative` 读取接口，动作改为等待异步端口结果。保存的密码用当前登录会话内的随机引用代替，认证时仅原生层解析真实值；错误认证不覆盖旧密码。独立本地控制器只注册只读初始化方法，不注册系统操作代理，并用 CSP 禁止网络和子 frame。资源来源、保留内容与同步步骤见 [安卓登录页资源](ANDROID_LOGIN_ASSETS.md)。

保留实际调用所依赖的参数顺序、返回 JSON 字段、错误语义和事件名称。例如下载回调 `clawbench-download-progress` 使用 `id`、`received`、`total`、`done`、`error`、`cancelled`。本地下载取消或页面切换后，迟到回调不能更新新页面任务。

安卓部分方法以事件报告结果，而不是直接返回成功。鸿蒙可以采用统一的异步适配，但必须调整对应调用方并测试，不能把 `undefined` 统一判为失败或成功。

### 5.2 可信页面限制

- 原生代理仅向可信的本地连接页和当前选定服务器的顶层应用页面开放。
- 外站、预览内容、第三方 iframe 和端口映射浏览器不得读取主应用密码或调用高权限桥。
- 导航和重定向时重新检查实际来源；服务端主页面里嵌入的不可信内容仍需隔离。
- 若所选 SDK 无法满足所需的 frame 隔离，应改用独立 Web 容器或更窄的消息通道，不能仅依赖前端 `window.top` 自检。
- 参数校验覆盖 URL、端口、文件名、长度和路径；注入 JavaScript 时用结构化序列化，禁止直接拼接用户文本。

### 5.3 认证和服务器切换

认证接口和字段从 `internal/handler/handler.go`、对应 handler 与 OpenAPI 核对，不能凭经验添加 `/api/login`。先读取 `/api/health` 确认目标为 ClawBench，再执行实际认证流程。

Web、原生 HTTP、原生 WebSocket、下载和隧道所使用的认证状态必须一致，按目标服务器隔离。Cookie 不能只复制一个固定名称；下载还可能依赖项目作用域 Cookie。跨服务器或外部下载链接不得携带当前服务器凭据。

切换服务器时按顺序停止旧连接和任务订阅、切换凭据及项目上下文、建立新连接、恢复新服务器自己的状态；旧连接迟到的事件必须被连接代次过滤。

0.2.3 将密码读取失败限制在对应服务器条目。登录快照保留名称和地址，以 `credentialState: "unavailable"` 标记该条目并撤销失效引用，其他服务器仍可选择。手动输入的密码和原生保存引用使用结构化类型区分，即使真实密码恰好以引用前缀开头也不会被错误解析。

登录页的“忽略已保存凭据”只作用于本次应用会话：不读取、写入或删除安全存储中的密码，仍可管理服务器元数据并手动认证。它不清空原有密码，也不跳过服务端认证。密码读取或写入异常、取消、通知跳转和重试均保留明确的目标服务器，不能因快照回退而重新选中上一次使用的服务器。完整重启后按正常凭据模式重新进入。

尚未保存但有效的目标地址可作为临时登录条目显示，创建快照不会自行持久化该条目；慢密码读取期间返回操作仍能取消连接。保存中的删除操作和后续凭据写入按顺序处理，避免删除后又被旧写入恢复。

本地快照的协议号为 `1`，未来以锁定上游版本为准。鸿蒙版本名不能直接与安卓版本名比较；旧服务器缺少协议字段时，需要明确的兼容策略和诊断提示。禁止把安卓 APK 下载入口作为鸿蒙升级入口。

## 6. 系统能力移植

| 安卓机制或需求 | 鸿蒙实施方向 | 必须验证的边界 |
| --- | --- | --- |
| WebView | ArkWeb Web 组件及控制器 | 注入时序、Cookie、WebSocket、缓存、下载、页面权限 |
| SharedPreferences / 凭据 | 偏好存储与安全资产存储分离 | 重启保持、多服务器隔离、删除与登出 |
| 系统返回键 | UIAbility / 页面返回入口连接 Web 返回状态机 | 顶层弹窗先关闭，一次手势只消费一次 |
| 文件选择、拍照、分享 | 系统选择器、相机和系统分享能力 | 取消、权限拒绝、多文件、临时 URI 生命周期 |
| 通知 | 系统通知及相应应用拉起机制 | 授权、冷启动跳转、已读撤销、多服务器归属 |
| BackgroundService / WorkManager | 前后台生命周期、事件补拉；再评估合法后台任务或推送方案 | 锁屏、挂起、进程重启，不假定无限保活 |
| SSH / HTTP2 隧道 | 独立传输接口，验证可用的库或原生实现 | 双向流、控制流、TLS / h2c、反向映射、半关闭、重连 |
| MediaProjection | 鸿蒙屏幕捕获及媒体管线的可行性验证 | 每次授权、音频能力、帧率、旋转、锁屏与停止 |
| 悬浮窗 / Android Live Updates | 评估目标系统允许的窗口、通知或实况能力 | API、权限、应用资格和真机行为，不能直接宣称等价 |
| 独立进程 BrowserActivity | 隔离的浏览器上下文或系统浏览器 | 主应用 Cookie、存储和桥不能泄漏 |
| 音量键和 OEM 设置 | 鸿蒙系统能力或清楚标识的平台替代 | 不显示无效安卓设置入口 |

上述是设计方向，不表示每种系统能力已确认可用。尤其是后台驻留、跨应用悬浮、系统音频和实时状态，需要在所选 SDK、设备及应用资格条件下验证。

### 6.1 网络与生命周期

必须覆盖用户输入的局域网 HTTP、正常 HTTPS、自签名证书、证书变化、网络切换和代理环境。网络权限、明文访问和证书处理分别核实；禁止全局忽略证书错误，也不能关闭验证后声称 HTTPS 验收通过。

当前明确采用系统受信证书及主机名校验，不提供应用内自签名证书信任例外或证书信任库。0.2.3 将 SDK 的证书校验、客户端证书、TLS 协商、CA 配置、DNS、超时和网络权限错误分别转换为可本地化的失败类型；`/api/health` 返回的应用、协议和字段类型也要验证，不能把非 ClawBench 响应当作可登录服务器。设置页只有在宿主实际提供 `forgetTrustedCerts` 方法时才显示证书管理入口；鸿蒙当前不提供该能力。

前后台共享服务端业务状态，不以 WebView 是否存活判断 AI 任务是否结束。恢复时重新建立事件订阅、根据游标补拉并刷新必要快照；按服务器和事件标识去重，避免重复通知。

0.2.3 在服务器选择 `native` 且系统授权允许时，前台也保留独立原生 WebSocket，只抑制前台系统通知。切到后台不再先断开连接等待配置请求；后台每 15 秒执行一次单飞 pending 补拉，回前台、关闭、切服或销毁后清除轮询。首次绑定或启用时等待一次静默基线，空历史也持久记录已初始化；HTTP 游标仅由 pending 返回的行推进，网页和实时事件只记录已见状态。同会话及有效 `message_id` 的完成回复跨两条路径去重；游标过期或不存在时按旧游标筛出较新的历史行，不再把所有新事件一起静默丢弃。

这些修复已有定向代码回归，仍使用 `backgroundDelivery: "while-process-running"`。没有新增常驻后台权限、系统调度扩展或 PushKit 接入，不能保证系统挂起或结束进程后的即时通知。本次未完成设备安装与启动复验，用户报告的后台问题仍需真机验证。

鸿蒙后台任务必须符合实际业务类型和系统限制，不能把安卓前台服务原样翻译成常驻定时器，也不能为保活虚报音频任务。若需要厂商推送或服务端配合，应单列依赖和改动范围。

0.2.4 停止 Push Kit 接入，恢复原应用身份，保留进程内 WebSocket 与 HTTP 补拉。真机诊断发现后台进程归属 `freezer:/Frozen`，仅保留最近任务不足以保证这些机制执行。系统级投递还需匹配云端身份与签名、token 登记更新和解绑、服务端发送授权、消息分类权益、导航与跨通道去重；客户端和服务端整条链路未实现，不能靠开通服务或重新打包解决。按维护者决定，本 PR 明确保留这一限制，不增加不匹配业务的常驻后台任务。

### 6.2 文件、媒体和隧道

文件传输采用流式处理和取消机制，限制中间缓冲；大文件不要整体 Base64 装入内存。保留小文件兼容桥时也要明确大小限制。

Web 媒体请求需要同时满足页面来源条件和系统权限。HTTP 下的页面媒体能力、自签名 HTTPS 下的行为应分别测试，不能把网络可达等同于麦克风可用。

### 6.3 当前原生实现入口

以下路径相对于 `harmony/entry/src/main/ets/`。纯策略放在 `models/`，实际 SDK 调用位于 `services/`，由 `pages/Index.ets` 按当前文档和服务器代次接线。

| 入口 | 已实现行为与边界 |
| --- | --- |
| `FileTransferService.ets` | 原生流式下载、进度与取消、系统保存选择器、Blob 保存、ShareKit 文本与文件分享；响应和队列有上限，拒绝自动重定向 |
| `ShareImportService.ets` | 只读取 ShareKit 授权的外部文件 URI；先复制到沙箱，显示服务器 / 项目 / 文件确认，再经已有上传接口导入；切换或取消不附加迟到结果 |
| `NativeCookies.ts` | 从真实登录 Set-Cookie 记录会话作用域，只附带匹配的会话、项目与聊天 Cookie，不按公网端口猜 Cookie 名 |
| `MediaPermissionService.ets`、`ScreenCaptureService.ets` | 同时校验可信页面与系统授权；录屏经 ArkWeb `getDisplayMedia` / `onScreenCaptureRequest`，不伪造 Android JPEG / PCM 拉帧接口 |
| `FilePickerService.ets` | 文档、照片 / 视频与拍摄选择；取消回传空列表，离页释放临时拍摄文件 |
| `ClipboardService.ets` | 原生读写剪贴板，读取时请求权限并检查当前文档；权限拒绝不会报告复制成功 |
| `NativeEventService.ets` | 原生事件订阅、补拉、去重、已读撤销和通知点击；授权与服务器推送偏好分开，后台仅限进程仍被系统允许运行 |
| `NativeLogService.ets` | 默认关闭，启用后限量回传脱敏诊断日志；停止或离页销毁请求和缓存 |
| `TunnelService.ets`、`TunnelPolicy.ts` | NAPI 异步 SSH / HTTP2、正反向映射、端口冲突、单次在途恢复、按服务器保存配置及 SSH 主机密钥确认；系统浏览器打开前等待监听就绪 |

`harmony-bootstrap.js` 只转发白名单事件。下载 RPC 的应答表示“已受理”，进度事件的 `done` 才表示保存结束；不能把前者直接显示为下载成功。旧页面离开后拒绝所有待处理 Promise，迟到的消息通道不会重新执行操作。

跨应用悬浮状态和实况窗仍报告不支持，原因是当前应用没有对应平台资格 / 签名权限，详见 [原生能力与平台边界](NATIVE_CAPABILITIES.md)。不能以普通通知或应用内弹层代替后计为完成。

隧道通过 `harmony/entry/src/main/cpp/` 构建 libssh2、nghttp2 与 mbedTLS，NAPI 在工作线程执行连接与转发，支持 arm64-v8a 和 x86_64。依赖版本、下载 SHA-256 与许可证固定保存；首次构建需要联网获取这些公开依赖。TLS 校验主机名和受信 CA，不允许忽略证书错误；SSH 在认证前核对按服务器保存的主机密钥，首次或变化时要求确认。

SSH 与 HTTP2 需要分别验证；普通 HTTP2 请求成功不能证明双向隧道可用。已有连接可在进程获准运行时继续转发，后台不新建连接或弹指纹确认；前台恢复后单次重连并恢复配置。进程挂起或结束仍会影响连接，不承诺常驻。设备数据检查和未完成边界见维护记录。

原生状态与配置必须一致：删除映射同时清理监听器、运行态和持久化数据；重连单次在途；UI 线程不执行阻塞连接；释放旧连接不能关闭新代次连接。

## 7. UI 对齐与测试数据

改 UI 前阅读上游 `docs/spec/client/design-guide.md`。以运行中的同版本安卓客户端为最终对照，现有 `docs/user-guide/screenshots-mobile/` 仅作辅助，截图可能落后于源码。

- 同一服务器、同一业务数据、同一主题与语言下进行对比。
- 覆盖手机竖屏、横屏、平板或折叠屏、键盘弹出、较大系统字体和安全区域。
- 覆盖底部 Dock、输入框、抽屉嵌套、弹窗、长标题、长路径、选区菜单和滚动恢复。
- 保持上游图标、主题 token、字号层级和交互语义；不要另做一套近似页面替代复杂原功能。
- 每项至少记录正常、空、加载、失败和权限拒绝等适用状态。

服务器地址及测试凭据只存于被忽略的本地配置。文档、截图、日志、测试夹具和提交消息使用占位数据，禁止复制真实账户资料。

## 8. 开发阶段与出口条件

| 阶段 | 工作 | 结束条件 |
| --- | --- | --- |
| M0 基线与环境 | 锁定上游、确认工具链、建立功能表、创建工程 | 最小 HAP 可构建安装启动；记录环境和首次本地提交 |
| M1 连接与壳 | 本地连接页、多服务器、协议检查、ArkWeb、宿主识别 | 冷启动、登录、失败重试、切换服务器及页面就绪稳定 |
| M2 核心业务 | 聊天、群聊、项目、文件、终端、Git、任务、邮件、设置 | 正常流程与关键失败场景通过，两端截图可比较 |
| M3 系统能力 | 上传下载、分享、通知、后台恢复、主题、选区、返回 | 真机权限与恢复测试通过，通知能正确定位 |
| M4 复杂能力 | SSH / HTTP2、独立浏览器、会议、录屏、状态展示 | 每项有真机证据；未完成或受限项仍保持开放 |
| M5 完整验收 | 全清单回归、长时场景、升级、发布检查 | 必需项逐项通过；任何平台限制显式列出，不宣称无差异完成 |

每阶段均执行“实现 -> 对应测试 -> 构建 -> 更新维护文档 -> 本地 commit”。不要把全部模块堆到最后才第一次构建。

## 9. 构建与版本管理

已验证的本地入口（使用 DevEco 配套 ohpm 与 Hvigor，不临时下载未知构建器）：

```powershell
./scripts/build-harmonyos.ps1
```

0.2.4 当前已完成未签名构建：

| 项目 | 已核对结果 |
| --- | --- |
| 应用身份 | `com.clawbench.harmony`，版本 `0.2.4 / 204` |
| 设备类型 | `phone`、`tablet` |
| 产物 | `release/ClawBench-0.2.4-unsigned.hap` |
| 大小 | 8,533,694 字节 |
| SHA-256 | `8942bd7ff6cca94cb5075cfbf0b2484db143c0118f603ad1644a1265bc061a8a` |
| 签名与设备验收 | 未签名；本轮未完成新包设备安装和推送投递验收 |

以上校验值仅对应未签名产物。签名完成后的 HAP 必须重新计算大小与哈希，并另记签名、设备安装和验证结果，不能沿用此处数值。

在仓库根目录首次安装和执行鸿蒙 Node 测试：

```sh
npm ci --prefix harmony
npm --prefix harmony test
```

需要 Node.js 24。本地维护布局将测试和测试包放在仓库根，接口契约测试读取参考源码中的 `web/src/utils/clawbenchNative.ts`；上游集成布局将测试和独立 `package.json` 放在 `harmony/`，应从上游根运行 `npm ci --prefix harmony`、`npm --prefix harmony test`。同步脚本按目标布局调整上方命令。共享 Vue 前端继续使用上游自己的安装、Vitest、类型检查和构建入口，两个测试包不可混用。

上游 `HarmonyOS` 工作流自动运行 Node 契约与 C++ 核心回归，并将共享接口声明纳入路径触发条件；这些作业不编译 ArkTS。`workflow_dispatch` 的 `build_hap` 默认关闭，只有手动启用时才使用带 `self-hosted`、`Windows`、`X64`、`harmonyos-sdk-26` 标签的 runner，要求仓库变量 `DEVECO_ROOT` 指向预配 SDK 26 的 DevEco 安装。该作业构建并保存未签名 HAP，PR 事件不会调度它。维护者只应选择已审阅提交，runner 不应存放签名密钥；本轮尚未实际执行该远端编译作业。

若所建工程由 IDE 配套 Hvigor 启动，应记录并使用实际命令，不为匹配示例临时下载未知版本。安装签名 HAP 时使用实际产物绝对路径，并明确指定测试设备；多设备连接时不能默认任意一台。

每次构建记录：

- 应用版本名、递增版本号、bundleName 和构建模式。
- 锁定的上游 commit/tag、客户端协议号、适配补丁版本。
- DevEco Studio、SDK/API、Node.js、ohpm、Hvigor 版本。
- 实际构建命令、结果、产物路径、SHA-256、是否签名。
- 测试设备、系统版本、通过项、失败项和未测试项。

签名密码、私钥、证书、真实服务器配置不得进入工程公共配置或 Git。无签名 HAP 不能描述为已可安装或已通过设备验证。

提交前显式选择文件，避免 `git add .` 把意外文件加入索引：

```powershell
git diff --check
git status --short
git add -- harmony docs scripts patches README.md .gitignore .gitattributes
git diff --cached --check
git diff --cached --name-only
git diff --cached
git commit -m "feat: implement HarmonyOS connection flow"
```

该示例适用于相应目录已经存在的后续阶段；本次初始化只暂存实际创建的文档和规则文件。完成构建后更新 [维护记录](MAINTENANCE.md) 和 [对齐清单](FEATURE_PARITY.md)，再提交代码与文档。失败构建如需保存排查点，提交说明必须写明未通过，不能冒充完成版本。

## 10. 验证策略

| 层级 | 重点 | 不能代替的验证 |
| --- | --- | --- |
| 单元 / 契约测试 | URL 归一化、服务器隔离、状态机、桥参数、事件去重、取消与错误映射 | 设备权限、ArkWeb 内核、真实网络 |
| Web 集成测试 | 页面导航、业务流程、宿主能力分支 | 原生桥实际行为和系统后台策略 |
| 鸿蒙模拟器 | 应用启动、布局、基本桥接与生命周期 | 真机录屏、通知、后台、耗电 |
| 鸿蒙真机 | 端到端功能、权限、网络切换、锁屏、恢复、媒体和隧道 | 更长时间或其他系统版本的稳定性 |

优先覆盖本次改动及其依赖流程，不为文档修改运行全量业务测试。异步测试等待实际条件，不用固定睡眠掩盖竞态。

建议第一轮真机回归包含：首次登录、错误密码、断网重连、重启应用、服务器切换、聊天中退后台、文件传输取消、通知冷启动跳转、嵌套弹窗返回、网络切换后的端口恢复、录屏开始与停止。对后台与长会话另外进行 30 分钟和 60 分钟场景测试，记录实际时长和结果，不把建议时长当作已验证结论。

测试期间不改变共享服务器设置、升级服务器或终止现有任务，除非后续任务明确授权。需要隔离 Go 服务时使用独立端口及数据目录，不能与运行实例共用数据库。

## 11. 上游同步与发布边界

上游地址：`https://github.com/clawbench-dev/clawbench`。

遵循维护要求：需要更新时，从 GitHub 下载当时最新源码，覆盖本地 `clawbench-source code/`，不创建旧版源码备份。本轮评审修复继续使用已锁定基线，未另行覆盖参考源码。

2026-11-06 已下载并逐文件覆盖更新到锁定提交，没有创建旧源码备份；4151 个归档文件与目标清单一致，没有陈旧文件。后续同步流程：

1. 查询上游默认分支当时的完整 commit，确定本轮要覆盖的目标；记录它，而不是仅记“latest”。
2. 检查参考目录是否存在尚未保存的适配工作；必要差异先整理到可跟踪的补丁中，不能静默覆盖未保存修改。
3. 下载该 commit 的归档至临时目录，检查下载结果、归档路径和预期关键文件。
4. 校验实际目标绝对路径确实为本项目的参考目录，再替换内容并清除上游已删除的陈旧文件。不得触及仓库根 `.git`、`harmony/` 或私有配置。
5. 更新 [上游基线记录](UPSTREAM_BASELINE.md)，重新检查桥、API、权限、页面变化，修订补丁与功能表。
6. 执行受影响测试和构建，更新维护记录并本地提交。

完整上游 commit 已记录在 [上游基线记录](UPSTREAM_BASELINE.md) 与补丁清单中。后续升级必须同步更新该值；不能用文件指纹或 npm 版本字段替代源码提交。

### 11.1 同步已审阅的贡献改动

在独立维护仓库中，`scripts/sync-upstream-review.ps1 -UpstreamDirectory <上游检出目录> -WhatIf` 可只读检查本轮同步计划；去掉 `-WhatIf` 才写入。脚本不下载上游、不重置或删除文件、不提交、不推送。它只读取 Git 纳管或未被忽略的 `harmony/`、`docs/`、`tests/`，外加独立测试清单与锁文件、构建脚本以及明确列出的三个网页文件。该脚本不复制到上游工程。

目标映射为：`docs/` 到 `docs/harmonyos/`，根 `tests/` 到 `harmony/tests/`，根测试包到 `harmony/package*.json`，`build-hap.ps1` 到 `scripts/build-harmonyos.ps1`。鸿蒙测试中的 `../harmony/` 导入相应调整；文档中的鸿蒙 Node 命令改为上游包前缀，供共享 Vue 构建的命令仍保持原义。脚本在写入前校验所有路径、符号链接 / 重解析点与测试包元数据，拒绝越界或覆盖不同用途的测试包。

上游 `harmony/README.md` 和 `.github/workflows/harmonyos.yml` 由贡献分支单独维护，不被同步脚本覆盖。运行同步后必须在两个仓库分别审阅差异、执行对应测试并本地提交；新增或删除网页适配文件时，显式修改允许清单和补丁清单，不能扩大为整个参考目录的复制。

发布源码不包含整个原始源码目录。需要适配上游时提交最小补丁、锁定基线和应用说明；下游复现时下载相同基线再应用补丁。不能把必要修改长期藏在被忽略的参考目录中。

本地上游 `LICENSE` 标明 MIT。复用 logo、安卓登录 HTML 和地址工具，并在 `docs/licenses/ClawBench-MIT.txt` 保留声明；必要前端适配以最小补丁发布。仅将应用实际需要的资源放入工程，不提交整个上游源码目录。后续继续核对各第三方依赖和资源自身的许可。

源码发布前审查已跟踪文件和提交历史，检查账户、密码、令牌、私钥、服务器地址、截图和日志。`.gitignore` 不能移除已经被跟踪的敏感内容；若发现泄漏，停止发布并处理相应凭据及历史。

仅发布经过审查的 Git 内容。例如下面是未来发布时的导出命令，本次未执行：

```powershell
git archive --format=zip --output=../ClawBench-harmonyos-source.zip HEAD
```

导出前必须确认 HEAD 对应目标版本，导出后检查压缩包文件列表。不直接压缩整个工作目录。

## 12. 实施前应阅读的材料

本地上游材料，均位于 `clawbench-source code/`：

- `docs/spec/client/android-integration.md`
- `docs/spec/client/multi-server.md`
- `docs/spec/client/design-guide.md`
- `docs/spec/client/unified-back-navigation.md`
- `docs/user-guide/README.md`
- `web/src/utils/clawbenchNative.ts`
- `internal/handler/handler.go`
- `internal/api/openapi.yaml`

官方入口：

- HarmonyOS 开发文档：`https://developer.huawei.com/consumer/cn/doc/`
- ArkWeb 开发指南：`https://developer.huawei.com/consumer/cn/doc/harmonyos-guides-V5/arkweb-V5`
- 后台长时任务：`https://developer.huawei.com/consumer/cn/doc/harmonyos-guides/continuous-task`

联网核对日期为 2026-11-06。旧 `web-app-development` 页面本次返回 404，已修正入口。已对实际使用的 ArkWeb 消息端口、文件选择、安全存储和 HTTP 接口核对配套 SDK 类型并构建；通知和媒体等候选能力仍需独立核实与验收。
