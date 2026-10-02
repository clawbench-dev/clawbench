# Android 16 灵动岛（Live Updates）

ClawBench Android 端在 Android 16+ 上通过 **Live Updates（实时更新通知）** 在状态栏、锁屏常驻展示会话状态——执行中 / 待审批 / 未读数量，类似 iOS 的灵动岛（Dynamic Island）。

---

## 是什么

Live Updates 是 Android 16（API 36）引入的通知类型，将**正在进行的任务**提升为状态栏 chip + 锁屏/通知抽屉顶部常驻卡片：

- 状态栏 chip：图标 + 短文本（如「执行中 2」），常驻显示
- 展开卡片：默认展开、不可折叠，显示完整分组（如「执行中 2 · 待审批 0 · 未读 3」）
- 通知必须标记 `ongoing`，用户无法手动清除

ClawBench 用它展示工作区会话状态，用户无需打开 App 即可随时看到是否有会话在跑、有待审批、有未读。

## 各厂商的叫法

不同厂商对同一套 Android 16 Live Updates 标准有自己的 UI 外壳和名称：

| 厂商 | 官方名称 | 说明 |
|------|---------|------|
| Google / Pixel | **Live Updates**（实时更新） | 原生叫法，Android 16 起 |
| Apple（iOS） | **Dynamic Island**（灵动岛） | "灵动岛"一词的起源，iPhone 14 Pro 挖孔动态区 |
| OPPO / OnePlus（ColorOS/OxygenOS） | **流体云**（Fluid Cloud） | 完整接入 Google Live Updates API，第三方 App 遵循规范即可适配 |
| 三星（One UI） | **Now Bar**（即时动态栏） | 锁屏底部胶囊条 |
| 小米（HyperOS） | **灵动岛 / 实时通知** | 挖孔胶囊展示 |
| vivo（OriginOS） | **原子通知**（Atoms） | 基于挖孔/胶囊的轻量通知 |
| 华为（HarmonyOS） | **实况窗**（Live Window） | 自研体系 |
| 荣耀（MagicOS） | **灵动胶囊**（Magic Capsule） | 挖孔胶囊展示 |

## 技术要点

### 成为 Live Update 的硬性条件

1. 通知样式必须是标准样式之一：`BigTextStyle` / `ProgressStyle` / `MetricStyle` / `CallStyle`
2. Manifest 声明非运行时权限：`android.permission.POST_PROMOTED_NOTIFICATIONS`
3. 显式请求提升：`EXTRA_REQUEST_PROMOTED_ONGOING` 或 `NotificationCompat.Builder#setRequestPromotedOngoing`
4. 必须 `ongoing`（`FLAG_ONGOING_EVENT`）
5. 必须有 `contentTitle`
6. 不得使用 RemoteViews、不得 `setGroupSummary`、不得 `setColorized(true)`
7. 通知渠道不得 `IMPORTANCE_MIN`

### 关键 API

| API | 作用 |
|-----|------|
| `NotificationManager.canPostPromotedNotifications()` | 检查当前能否发布提升通知 |
| `Notification.hasPromotableCharacteristics()` | 验证通知是否具备提升资格 |
| `Notification.FLAG_PROMOTED_ONGOING` | 通知是否已被提升 |
| `Settings.ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS` | 跳转实时更新权限设置（部分 ROM 不存在） |
| `Settings.ACTION_APP_NOTIFICATION_PROMOTION_SETTINGS` | 部分系统实际存在的权限设置 action |

### 权限（重点）

Live Updates 需要用户在系统设置中开启，且**每个 ROM 有一层独立的授权开关**：

- **AOSP / Pixel**：通知设置里的「实时更新 / 推广通知」权限
- **ColorOS 流体云**：对第三方 App **默认关闭**，需在设置 → 通知 → 流体云 中为 ClawBench 单独开启
- **三星 Now Bar**、**vivo 原子通知** 等同理

**如果未授权**：`canPostPromotedNotifications()` 返回 `false`，通知退化为普通 ongoing 通知（状态栏无 chip）。ClawBench 设置页开启「灵动岛」开关时若检测到无权限，会自动跳转对应权限设置页引导开启。

### ClawBench 实现

- 独立配置项：「灵动岛」开关（`live_update_enabled`，默认开启），与「桌面悬浮状态窗」互相独立，互不影响
- 数据源与悬浮窗共享 `/api/ai/sessions/overview` 与 WS 事件流
- 三态互斥显示：待审批 > 执行中 > 未读（chip 只显示最紧急的一个）
- 展开卡片固定显示三组数量（含 0）
- 无任何活跃会话时移除 chip（状态栏保持干净，直到下一个会话开始）
- 刷新节流 5 秒合并，`when` 计时由系统自行更新，耗电极低

## 相关代码

- `android/app/src/main/java/com/clawbench/app/LiveUpdateManager.java` — 核心实现
- `android/app/src/main/java/com/clawbench/app/BackgroundService.java` — 事件流与开关接线
- `android/app/src/main/java/com/clawbench/app/MainActivity.java` — JSBridge（`setLiveUpdateEnabled` / `canPostPromotedNotifications` / `openLiveUpdateSettings`）
- `web/src/composables/useSettingsConfig.ts` — 前端设置项
