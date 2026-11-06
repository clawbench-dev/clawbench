# 原生能力与平台边界

核查日期：2026-11-06。当前版本 `0.2.0`，工具链为 DevEco Studio 26.0.0.851 / HarmonyOS SDK 26.0.0.105，应用兼容与目标 API 23，编译 API 26。

本文中的 SDK 路径相对于 DevEco Studio 的 `sdk/default/`。这些是本轮实际使用的 SDK 声明和权限定义；后续 SDK 更新时应重新核查。编译器可以识别某个接口，并不表示当前签名配置具备调用资格。

## 当前实现与验证

arm64-v8a / x86_64 原生 NAPI 库与未签名 HAP 已构建。以下设备结果仅来自 HarmonyOS 7 / API 26 手机模拟器；不代表 ARM64 真机、API 23、完整业务或长后台验收。构建标识、最终测试数量和证据索引由维护记录保存。

| 能力 | 当前实现 | 本轮模拟器证据与剩余范围 |
| --- | --- | --- |
| 文件保存 | 认证 Cookie 隔离、流式下载、进度、取消、系统保存选择器与有上限的 Blob 保存 | 认证下载及 35 B 文件保存通过，进度 `done` 对应保存完成；大文件、重名、中文名与取消组合待验收 |
| 分享 | ShareKit 文本 / 文件导出；外部分享文件先入沙箱，确认服务器 / 项目后上传 | 35 B 文件分享面板与取消通过；实际接收方分享、外部分享导入完整流程待验收 |
| 剪贴板与选区 | 原生授权读取与写入，拒绝权限不假报成功；原生选区回调共享前端 | 授权读写与 Quote 通过；其他选区动作和复杂输入场景待验收 |
| 通知 | 系统授权、原生订阅、补拉、去重、已读撤销和校验后的点击导航 | 后台发通知及点击定位会话通过；其他事件类型、冷启动与进程重建待组合验收 |
| 摄像头 / 麦克风 | 可信来源校验与系统动态授权，向 ArkWeb 返回真实授权结果 | 音视频轨道 live 已验证；实际入会、对端收发、听记与后台行为待验收 |
| 录屏 / 系统音频 | ArkWeb `getDisplayMedia` 与 `onScreenCaptureRequest` 授权入口 | 仅视频和请求音频两种场景均授权后挂起，未取得可用流，不能计作通过 |
| SSH / HTTP2 | 异步 NAPI、正反向映射、按服务器保存配置、恢复、冲突处理与主机身份校验 | SSH / HTTP2 正向和反向分别各 6 轮数据通过，SSH 指纹确认、连接和已有映射自动恢复通过；真机与异常组合待验收 |
| 系统浏览器 | 打开转发地址及外链，使用独立 Cookie / 存储，不注入应用消息桥 | 不等价于 Android 内嵌 sandbox 的会话保持；打开、返回与重连业务待验收 |
| 日志与辅助 | 默认关闭的限量脱敏日志、常亮与离页释放、终端音量键桥 | 已接入；其他辅助功能与真机组合待验收 |

下载 RPC 的成功应答只表示操作已受理，前端必须等进度事件的 `done` 才结束保存状态。每页原生服务与消息端口带连接 / 文档代次，离页取消请求和待处理 Promise；通知登录、返回键及重连异常不能改动后来打开的页面。

录屏能力探测只报告浏览器入口是否存在，不是捕获成功证明。当前挂起问题仍未解决，不能将弹出授权对话框、同意授权或摄像头轨道 live 用作屏幕 / 系统音频共享通过的依据。

## 隧道与浏览器边界

SSH 通过 libssh2 + mbedTLS 实现，认证前检查按服务器保存的主机密钥，首次或变化时要求确认。HTTP2 通过 nghttp2 实现，TLS 保留主机名与受信 CA 校验。依赖版本、下载 SHA-256 和许可在原生构建配置中固定；不接受忽略 TLS 校验的替代方案。

SSH 与 HTTP2 的正向和反向分别各通过 6 轮模拟器数据测试，包含 2 MiB、慢读与并发，收发 SHA-256 一致。SSH 指纹 UI 与隔离测试服务公开密钥哈希核对一致后信任，实际状态为 `transport: ssh`、`connected: true`，已有正反向映射自动恢复通过。这证明已测试路径的数据完整性，不代表真机、所有 TLS、网络切换、错误恢复和长期连接场景都已通过。

半关闭测试链路中的 hdc 会提前关闭双向连接，无法区分客户端半关闭与测试转发层的影响。因此本轮半关闭记为未验收，需要换用能保留单向 EOF 的链路，不能将连接关闭或双向数据测试算作通过。

原生连接不在 UI 线程阻塞；离前台时不发起新连接或主机指纹确认，已有隧道只在应用进程继续获准运行时转发。系统浏览器有独立 Cookie / 存储，未实现 Android BrowserActivity 的内嵌 sandbox 会话语义；打开前的隧道就绪处理也不能提供无限后台保活。

## 悬浮会话状态

安卓实现使用跨应用悬浮窗显示运行中、待批准和未读会话的计数，展开后按项目分组，并携带会话和项目标识跳转。数据来源为 `GET /api/ai/sessions/overview` 与会话、任务事件。完整复刻包含后台显示、关闭与重新启用、分组列表、点击跳转和切服清理，不能以应用内部弹窗替代跨应用能力后宣称完成。

当前鸿蒙工程保持 `floatingStatus: false`，未加入需要特殊授权的悬浮窗权限，也未用空实现返回启用成功。

| 途径 | SDK 26 定义 | 当前判断 |
| --- | --- | --- |
| `window.createWindow` + `WindowType.TYPE_FLOAT` | `SYSTEM_FLOAT_WINDOW` 为 `system_grant`、`system_basic`，允许通过签名 profile 配置（`provisionEnable: true`） | 当前未配置相应权限资格，普通运行时授权对话框不能补足签名侧要求 |
| API 26 的 `floatView` | `FLOAT_VIEW` 为 `user_grant`，但权限级别仍为 `system_basic`，且需要 profile 许可 | 用户同意与签名资格是两层条件；不能因它是 `user_grant` 就视作普通应用可直接使用 |
| `floatingBall` | `USE_FLOAT_BALL` 为 `system_grant`、`system_basic`，需要 profile 许可 | 不能作为绕过上述门槛的通用途径 |

证据位置：

- `openharmony/toolchains/lib/PermissionDefinitions.json`：`SYSTEM_FLOAT_WINDOW`（本 SDK 第 615 行）、`USE_FLOAT_BALL`（第 7536 行）、`FLOAT_VIEW`（第 7565 行）的 `grantMode`、`availableLevel` 与 `provisionEnable`。
- `openharmony/ets/api/@ohos.window.d.ts`：`createWindow` 的权限说明（第 1850 / 1883 行）和 `201 Permission verification failed` 错误。
- `openharmony/ets/api/@ohos.abilityAccessCtrl.d.ts`：`requestPermissionsFromUser` 说明其用途为请求 `user_grant` 权限；该接口不会改变应用的 APL 或签名 profile。
- `openharmony/ets/api/@ohos.window.floatView.d.ts`：能力起始于 API 26；`start()` 要求 `FLOAT_VIEW`，并明确需收到 `STARTED` 回调才能确定真正启动成功。`start()` 的 Promise 返回不代表窗口已显示。
- 项目 `harmony/build-profile.json5` 的 `signingConfigs` 为空；本轮没有特殊权限授权 profile，也没有相关真机能力验收证据。

`availableType: NORMAL` 不等于 `availableLevel: normal`。前者说明可用的应用类型，后者决定权限等级。具备所需平台批准与签名授权后可以继续实现和验收，当前结论不是“鸿蒙永远不支持悬浮窗”。

若后续取得授权，实施时应将“用户启用该功能”和“窗口当前可见”分开保存：成功创建并确认启动后才能报告启用；回到前台或没有活动时隐藏，不擅自取消用户偏好；切服、退出与权限撤销时释放窗口、监听及旧事件。API 26 的 FloatView 还需独立处理低版本兼容、系统限制尺寸、前台创建条件和用户关闭后的恢复。

## 实况窗

当前保持 `liveUpdate: false`。Android Live Updates 的权限和接口不能直接在鸿蒙使用；鸿蒙对应的 Live View Kit 需要按应用场景申请权限，当前工程未取得此项准入。

证据位置：

- `hms/ets/api/@hms.core.liveview.liveViewManager.d.ts` 的 `LiveView.event` 字段（本 SDK 第 423 行）明确写明特定场景需申请相应权限，指向官方 `live-view-kit/guide/liveview-rights.md`。
- 同文件 `startLiveView` 的错误定义区分 `1003500004 LiveView is not enabled` 和 `1003500005 The right of liveView is not enabled`。用户是否打开实况窗与应用是否具备使用资格是两个不同条件。
- `openharmony/ets/api/notification/notificationContent.d.ts` 的 `systemLiveView` 说明第三方应用不能直接创建这种系统实况通知，不能用普通 NotificationKit 构造系统实况窗来绕过准入。

后续需由项目维护者确认适用业务场景并取得 Live View Kit 权限，再验证创建、版本递增更新、结束、系统撤销、前后台状态与平台展示范围。本轮没有把普通通知计作实况窗验收。

## 通知与后台范围

普通系统通知使用 NotificationKit 的动态授权及 wantAgent 点击拉起，不依赖上述悬浮窗或实况窗权限。当前实现提供按服务器隔离的原生事件订阅、补拉、去重、已读撤销和冷启动导航。

本轮模拟器已验证后台发送系统通知和点击后定位会话；未将这一结果扩展为所有通知类型、进程冷启动、长时锁屏或真机推送通过。通知点击期间若用户选择了其他服务器，旧密码查询、登录快照和连接失败会因代次失效而停止，不再覆盖新页面。

设置页在服务器推送模式之外独立显示系统通知授权与实际接收状态。`refreshStatus()` 通过 `notificationManager.isNotificationEnabled()` 查询，未获授权显示拒绝、系统查询异常显示未知；不会把查询失败误报为用户拒绝，也不会因此修改服务器 `push_mode`。返回应用时重新检查；系统撤销授权后停原生订阅，重新授权后恢复仍被用户选择的原生接收方式。异步读回带连接与设置代次保护，旧服务器或旧授权查询不能覆盖新状态。该 API 的 Promise 重载从 API 11 起提供，本轮 SDK 定义见 `openharmony/ets/api/@ohos.notificationManager.d.ts` 第 502 至 520 行。

授权已允许但原生接收未启用时，接收状态显示“已关闭”；接收已启用且无错误、前台暂未建立后台连接时显示“已就绪”，只有后台连接确认建立后才显示“已连接”。这几种状态独立于授权状态，不能把未连接直接当作权限拒绝或连接错误。

原生 WebSocket 只在应用进程仍获准运行期间工作。进程被系统挂起或结束后，不承诺立即收到通知；恢复时补拉事件。能力状态明确为 `backgroundDelivery: "while-process-running"`。没有以无关音频或其他后台任务类型制造常驻效果，也未声明已完成厂商推送接入或长时间锁屏验收。

通知携带的是原生生成的一次性随机标识；实际导航数据仅保存在应用偏好中，消费时核对已保存服务器清单，并在对应服务器登录和页面就绪后定位。首次获取完整历史事件和过期游标重建基线时，不弹出整批历史通知。

Forge 仓库变化使用服务端 `forge_event` 的 `data.event` 与 `data.item` 契约生成实时系统通知。议题、合并请求按编号定位，流水线按 `run_id` 与 `pipeline/run:<id>` 定位，并保留所属项目路径；冷启动时再次校验这些字段，重放事件不弹。当前服务端 pending 端点不持久化 Forge 事件，因此本实现不推进 Forge 补拉游标，也不承诺应用挂起后的仓库事件补发。Forge 未读状态仍由网页恢复时从服务器加载。
