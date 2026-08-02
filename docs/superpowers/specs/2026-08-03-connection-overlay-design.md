# 统一全屏状态遮罩（ConnectionOverlay）设计

日期：2026-08-03
状态：已批准（用户确认）

## 背景与问题

当前 WebSocket 断开时，仅 APP Header 的服务器图标变黄闪烁（`AppHeader.vue` 的
`status-dot-reconnecting` + `status-pulse` 动画），提示不够醒目。用户希望改为全屏遮罩，
显示「服务器连接中断，重连中」的加载状态，重连成功后消失。

同时存在一个服务器重启遮罩（`SettingsPage.vue` 的 `.restart-overlay`），已确认全屏
（`position: fixed; inset: 0; z-index: 9999`，`Teleport to="body"`），能盖住 APP Header。

用户要求：**统一成同一个遮罩组件与样式**。

## 现状代码

| 位置 | 说明 |
|------|------|
| `useGlobalEvents.ts:412` | `wsStatus` computed：`'connected' \| 'reconnecting' \| 'disconnected'`，模块级单例 |
| `AppHeader.vue:90-94` | `statusDotClass` 依据 `wsStatus` 返回连接/重连/断连样式 |
| `SettingsPage.vue:39-46` | 重启遮罩模板（Teleport + spinner + 文案） |
| `SettingsPage.vue:282-292` | `.restart-overlay` 全屏样式 |
| `useSettingsNavigation.ts:45` | `restartingOverlay` 局部 ref（per-call） |
| `useSettingsNavigation.ts:88-116` | `pollUntilServerUp()` 轮询 `/api/agents`，控制 `restartingOverlay` |

## 设计

### 1. 新建组件 `web/src/components/common/ConnectionOverlay.vue`

统一的全屏状态遮罩：

- `Teleport to="body"`，`position: fixed; inset: 0; z-index: 9999`。
- 复用现有重启遮罩视觉：半透明背景 + blur + 居中卡片 + spinner（`@keyframes spin`）。
- 卡片内容：**服务器图标 + spinner（加载态）+ 状态文字**。
- 两种模式，**优先级：重启 > 断连重连**：
  - 重启模式 → 文案「正在重启，请稍候…」（复用 `settings.restartingPleaseWait`）
  - 断连模式 → 文案「连接断开，正在重连…」（新增 i18n key）
- **1.5s 延迟**：断连（`reconnecting` / `disconnected`）后持续 1.5s 仍未恢复才显示；
  期间恢复则不显示。重启模式立即显示（用户主动操作）。

### 2. 状态共享

- 将 `useSettingsNavigation.ts` 的 `restartingOverlay` 提升为**模块级 ref**
  （仿照同文件已有 `guards` 模块级 registry 模式），`useSettingsNavigation()` 仍返回它，
  保证 SettingsPage 与全局遮罩共享同一状态。
- 遮罩组件同时读取：
  - `wsStatus` ← `useGlobalEvents()`
  - `restartingOverlay` ← `useSettingsNavigation()`

### 3. 挂载与清理

- `App.vue` 认证分支内挂载 `<ConnectionOverlay />`。
- 删除 `SettingsPage.vue` 中旧重启遮罩模板与 `.restart-overlay` CSS。
- **首屏保护**：仅在「曾经连接成功过」后才允许断连遮罩出现，避免刷新页面闪现遮罩。

### 4. 文案（i18n）

新增断连文案 key（中英双语），放在合适分组：

- zh：`连接断开，正在重连…`
- en：`Connection lost, reconnecting…`

### 5. 测试

- 新增 `ConnectionOverlay.test.ts`：
  - 断连后延迟 1.5s 才显示
  - 1.5s 内恢复连接则不显示
  - 重启模式立即显示且优先于断连模式
  - 首屏（从未连接成功）不显示
  - 断连显示服务器图标 + spinner + 正确文案
  - 重启显示「正在重启，请稍候…」文案
  - 重连成功（`connected`）后遮罩消失
- 更新 `useSettingsNavigation.test.ts`：模块级 `restartingOverlay` 需在测试间重置。
- 更新 `SettingsPage.test.ts`：移除/调整对旧 `.restart-overlay` 的断言（若有）。

## 非目标（YAGNI）

- 不改动 `AppHeader.vue` 服务器图标的颜色逻辑（遮罩覆盖时不可见，恢复后仍显示状态色）。
- 不引入全局 UI store / 状态管理库。
- 不处理登录页（LoginView）场景——登录前无 WS 连接。

## 验收标准

1. WebSocket 断开持续 1.5s → 全屏遮罩出现，盖住 APP Header，含服务器图标 + spinner + 「连接断开，正在重连…」。
2. 1.5s 内重连成功 → 无遮罩。
3. 重连成功 → 遮罩消失。
4. 设置页触发重启 → 立即显示同一遮罩，文案为「正在重启，请稍候…」。
5. 刷新页面（从未连接成功）→ 不闪现断连遮罩。
6. 相关单元测试通过，`npm test` 通过。
