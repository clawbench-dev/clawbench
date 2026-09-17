# 应用内通知开关 + 推送通知页小节重排

## 背景

推送通知设置页（`settingsFieldMap.ts` 的 `notification` 分类）目前只有 3 个平铺开关 + 1 个推送模式面板，小节标题只有「提示音」一个。其中：

- `notificationSound`（任务完成提示音）——本地设置，guard 在 `playNotificationSound()` 内部
- `floatingStatusWindow`（桌面悬浮状态窗）——本地设置，appOnly
- `liveUpdate`（灵动岛）——本地设置，appOnly
- `push_mode` 面板（原生/钉钉/飞书/关闭）——服务端设置

而**应用内完成卡片**（`CompletionPopover`）目前没有任何开关：`App.vue:1159 handleCompletionEvent` 对符合条件的实时完成事件无条件 `push()`，会话与任务两类都弹。用户希望能在该面板关闭它。

## 需求决策（已与用户确认）

| 议题 | 决策 |
|---|---|
| 开关语义 | **只管应用内完成卡片**。提示音、浏览器/原生推送仍由各自开关控制，互不影响 |
| 覆盖范围 | **会话 + 任务都关**。一个开关管住所有应用内完成卡片 |
| 放置位置 | 与提示音合并为一个小节，标题改为**应用内通知** |
| 关闭时已弹出的卡片 | **保留，等它自己关掉**。只影响后续新卡片 |
| 小节划分 | 三节：应用内通知 / 桌面与系统 / 移动端通知 |
| 桌面与系统 | `floatingStatusWindow` + `liveUpdate` **保持独立卡片**（仍即时生效），单独一个标题 |
| 移动端通知 | 推送面板前加一个小节标题 |

## 目标布局

```
┌─ 应用内通知 ──────────────────────┐
│ 应用内通知           [switch]     │  ← 新增 inAppNotification
│ 任务完成提示音       [switch]     │  ← 原 notificationSound
├─ 桌面与系统 ──────────────────────┤   （appOnly，浏览器模式不渲染）
│ 桌面悬浮状态窗       [switch]     │
│ 灵动岛               [switch]     │
├─ 移动端通知 ──────────────────────┤
│ 推送模式        原生推送      >   │  ← push 面板（服务端，改完点保存）
│ ...（钉钉/飞书子字段、测试连通性）│
└───────────────────────────────────┘
```

## 实施

### 1. 新增本地设置字段 `inAppNotification`

**`web/src/composables/useSettingsConfig.ts`**
- `localDefaults` 增加 `inAppNotification: true`（默认开启，保持现有行为）
- `legacyKeys` **不新增条目**：该键无遗留 localStorage 键、无即时副作用，`setLocalConfig` 的 prefixed key 写入已足够持久化

**`web/src/components/settings/settingsFieldMap.ts`**（`notification` 分类）
- 新增 `inAppNotification` switch 项，`source: 'local'`，`sectionHeader: 'settings.items.inAppNotifySection'`，置于 `notificationSound` **之前**
- 现有三行的 `sectionHeader` 改为：
  - `notificationSound` → `settings.items.inAppNotifySection`
  - `floatingStatusWindow` / `liveUpdate` → `settings.items.desktopSystemSection`
- 推送面板 `config.titleKey` 新增 `settings.items.mobileNotifySection`

`cards` 分组逻辑（`SettingsCategory.vue:222-259`）按 `sectionHeader` 文本切卡：同标题的连续 item 合为一张卡，遇到 panel 时 `flush()`。因此上述顺序天然产出「应用内通知（2 行）→ 桌面与系统（2 行）→ 移动端通知（面板）」三张卡。

`appOnly` 过滤发生在 `renderList`（`SettingsCategory.vue:200`），早于分组，浏览器模式下「桌面与系统」卡整张消失（`cards` 只对已过滤列表分组），不会留下空标题。

### 2. 面板标题渲染（需确认点）

`shouldShowPanelTitle`（`SettingsCategory.vue:263-271`）对 mixed 分类恒返回 `true`，而 `SettingsGroupPanel.vue:5` 渲染标题的条件是 `showTitle && config.titleKey`。当前 `notification` 分类是 mixed（有平铺项 + 面板）但 push 面板**没有 `titleKey`**，所以现在不显示标题。补上 `titleKey` 后标题即出现——这是本设计期望的效果，无需改 `shouldShowPanelTitle`。

### 3. 守卫接入（App.vue）

在 `handleCompletionEvent` 的既有守卫链之后、`completionPopover.push()` 之前插入早返回：

```ts
// 应用内通知开关（本地设置，默认开）：关闭后不再弹完成卡片。
// 放在所有既有守卫之后，避免"关了开关"掩盖前台会话判断等其他逻辑。
if (localConfig.inAppNotification === false) return
```

位置：`App.vue:1172`（前台会话判断）之后、`App.vue:1174`（跨项目判断）之前或之后均可，但必须在两处 `push()` 之前。选择放在 `isSameProject` 计算**之前**，省掉无谓计算。

**不做的事**：不在 `useCompletionPopover.push()` 内部加 guard。理由：`push()` 是纯队列原语（`useCompletionPopover.test.ts` 13 个用例直接测它），把设置读取塞进去会让单测被迫 mock `useSettingsConfig`；且「关闭时保留已弹出卡片」要求 `active` 不受影响，在调用方早返回语义更直白。

`App.vue:994` 已解构 `localConfig`，无需补 import。

### 4. i18n

`web/src/i18n/locales/zh.ts` / `en.ts` 的 `settings.items` 下：

| key | zh | en |
|---|---|---|
| `inAppNotifySection` | 应用内通知 | In-App Notifications |
| `inAppNotification` | 应用内完成通知 | In-App Completion Card |
| `inAppNotificationDesc` | 会话或任务完成时在应用内弹出结果卡片；关闭后不再弹出，提示音与系统推送不受影响 | Show a result card in-app when a session or task finishes; when off, no card appears (alert sound and system push are unaffected) |
| `desktopSystemSection` | 桌面与系统 | Desktop & System |
| `mobileNotifySection` | 移动端通知 | Mobile Notifications |

`notificationSoundSection` 原 key 变为无引用——删除（`SettingsCategory.test.ts:242` 的 i18n mock 内同名条目一并删）。确认全仓无其他引用（当前仅 `settingsFieldMap.ts` 三处 + 测试 mock）。

### 5. 测试

**`web/src/components/settings/__tests__/settingsFieldMap.test.ts`**
- `notification` 分类含 `inAppNotification` 项，`source === 'local'`、`type === 'switch'`、`sectionHeader === 'settings.items.inAppNotifySection'`
- 三行 sectionHeader 的映射断言（应用内通知 ×2、桌面与系统 ×2）
- push 面板 `titleKey === 'settings.items.mobileNotifySection'`

**`web/src/components/settings/__tests__/SettingsCategory.test.ts`**
- 补 i18n mock：新增 5 个 key，删除 `notificationSoundSection`
- 新增：`inAppNotification` 行存在且 `type === 'switch'`
- 新增：切换 `inAppNotification` 调用 `setLocalConfig('inAppNotification', false)`
- 新增：卡片分组断言——`notification` 分类渲染出标题依次为「应用内通知」「桌面与系统」「移动端通知」的三张卡（用 `SettingsCard` 的 `title` prop 收集；注意 appOnly 项在非 app 模式被过滤，测试环境 `isAppMode` 默认 false，故需按需 mock）

**`web/src/composables/__tests__/useSettingsConfig.test.ts`**（已存在，1149 行；已有 `notificationSound` 默认值 + 持久化用例可照抄）
- `localConfig.inAppNotification` 默认 `true`（清掉 `clawbench-settings-inAppNotification` 后断言）
- `setLocalConfig('inAppNotification', false)` 写入 `clawbench-settings-inAppNotification` 且 `localConfig` 同步

**守卫行为测试**：`handleCompletionEvent` 定义在 `App.vue` 的 setup 内、未导出，无法直接单测。方案：不新增 App.vue 级测试（该文件目前无测试文件，引入成本高）。改为**在 `App.vue` 中把开关判断抽成模块级纯函数**并单测——但这样会为一行判断引入抽象，违背 YAGNI。

**决定**：接受 `App.vue` 内联判断无单测覆盖。作为补偿，在 `useSettingsConfig` 侧断言默认值（`true`），确保默认行为不变；并人工验证关闭开关后完成一次会话确认无卡片。若后续需要回归保护，再抽函数。

**手动验证清单**
1. 设置 → 推送通知：三张小节标题顺序正确；「桌面与系统」在浏览器模式下整卡消失
2. 关闭「应用内完成通知」→ 切到其他 Tab 触发一次会话完成 → 无卡片
3. 重新打开开关 → 再触发 → 卡片正常弹出
4. 卡片显示时关闭开关 → 当前卡片保留，关闭后不再有新卡片
5. 关闭开关不影响：提示音（开关仍开时仍响）、系统/IM 推送

### 6. 文档

- `docs/spec/features/push-notifications.md`「功能清单」补一条 `inAppNotification` 本地设置说明（与现有「通知音效开关」条目并列）
- `docs/spec/features/completion-popup.md`「功能清单」补一条：受 `inAppNotification` 本地设置门控

## 影响面

- 纯前端改动（`web/src/` + i18n + 文档），无 Go / Android 改动，无 OpenAPI 变更
- 无新依赖、无数据迁移（localStorage 新键，缺省即 `true`）
- 现有用户升级后行为不变（默认开启）
- 完成后按 AGENTS.md 要求跑 `npm run build`（`outDir` 为仓库根 `.clawbench-web/`），disk 模式立即生效，无需重启

## 风险

- **`localConfig` 在 App.vue 的响应性**：`localConfig` 是 `reactive()` 导出，`handleCompletionEvent` 每次调用时读 `localConfig.inAppNotification` 都是最新值，不存在缓存陈旧问题
- **`settingsFieldMap.test.ts:233` / `:247`** 断言 `notification` 分类「有 item 也有 panel」——新增 item 不破坏这两条，但需跑一遍确认
