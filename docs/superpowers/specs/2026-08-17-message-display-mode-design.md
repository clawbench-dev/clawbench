# Design: 聊天消息展示模式配置（摘要模式 / 原文模式）

## 目标

在设置面板「聊天」类别下新增**消息展示模式**配置，提供两种全局默认展示方式：**摘要模式**（默认，保持现状）与**原文模式**。该配置作为全局默认值，用户对单条消息的「摘要/原文」手动切换仍可覆盖全局默认，且切换按钮与现有行为保持不变。

## 背景与现状

- 每条消息已有「摘要/原文」切换按钮（`SummaryToggle.vue`），`msg.showingSummary` 存单条显式偏好（`undefined` = 未选择），会话内解析时保留（`chatSessionUtils.ts` `parseMessages`）。
- 渲染决策集中在 `web/src/utils/chatSessionUtils.ts:105` 的 `shouldShowSummary()`：有摘要且未手动切换时**一律显示摘要**——即当前默认行为是摘要模式。
- 历史加载固定用 `view=summary`（`useChatSession.ts` 429/474/584），后端会剥离正文内容（`scanMessagesView` → `enrichMessagesWithSummaries` → `summarizeContentForView`），导致摘要消息 `blocks` 为空。
- 已有懒加载机制：`ChatPanelContent.vue` `ensureMessageContent` 按需 `GET /api/rag/message?id=...` 拉取全文并填充 `msg.blocks`，失败时回退显示摘要。
- **不存在**任何全局消息展示模式配置（搜索 `displayMode`/`messageMode`/`summaryMode`/`摘要模式`/`原文` 确认）。

## 范围

- 纯前端本地配置（localStorage），无后端改动。
- 影响范围：聊天历史中「有摘要的消息」的默认展示方式。
- **不改变**：单条切换按钮与 `msg.showingSummary` 语义、无摘要消息（始终原文）、流式新消息（无摘要，原文）、TaskExecDetail 的摘要/原文 tab。

## 架构

### 1. 配置项 `web/src/composables/useSettingsConfig.ts`

- `localDefaults` 新增：`messageDisplayMode: 'summary'`。
- 值域：`'summary' | 'original'`，存 localStorage（`LOCAL_PREFIX + 'messageDisplayMode'`），即时生效，无需重启。
- 不注册 `legacyKeys`（无既有键可迁移）。

### 2. 设置面板项 `web/src/components/settings/settingsFieldMap.ts`

在 `chat` 类别数组（125–138 行）的「消息与历史」（`settings.items.chatMessageSection`）区新增 select 项，仿照现有 `fileView`（147–150 行）写法：

```ts
{ labelKey: 'settings.items.messageDisplayMode',
  key: 'messageDisplayMode',
  type: 'select',
  source: 'local',
  sectionHeader: 'settings.items.chatMessageSection',
  options: [
    { labelKey: 'settings.items.messageDisplayModeSummary', value: 'summary' },
    { labelKey: 'settings.items.messageDisplayModeOriginal', value: 'original' },
  ]},
```

### 3. i18n `web/src/i18n/locales/zh.ts` / `en.ts`

| key | zh | en |
|---|---|---|
| `settings.items.messageDisplayMode` | 消息展示模式 | Message display mode |
| `settings.items.messageDisplayModeSummary` | 摘要模式 | Summary mode |
| `settings.items.messageDisplayModeOriginal` | 原文模式 | Original text |

### 4. 渲染决策 `web/src/utils/chatSessionUtils.ts` `shouldShowSummary`

增加参数 `defaultMode: 'summary' | 'original'`（默认 `'summary'`，不传则保持现状），决策逻辑：

```ts
export function shouldShowSummary(msg, defaultMode = 'summary'): boolean {
  const hasSummary = msg.summary != null && msg.summary !== ''
  if (!hasSummary) return false                              // 无摘要 → 原文
  const blocksEmpty = !msg.blocks || msg.blocks.length === 0
  if (msg.showingSummary !== undefined) {                    // 单条显式偏好（覆盖全局）
    if (blocksEmpty) return true                             // 内容被剥离 → 回退摘要（保留既有行为）
    return msg.showingSummary !== false
  }
  if (blocksEmpty) return defaultMode === 'summary'          // 原文模式 → 组件触发懒加载全文
  return defaultMode === 'summary'
}
```

关键点：`blocksEmpty`（内容被 `view=summary` 剥离）时**不再无条件返回 true**——仅在摘要模式（含默认）下返回 true；原文模式下返回 false，交由组件触发懒加载。单条显式偏好 + 内容被剥离时保持现状（回退摘要，覆盖 `streaming` 中断后摘要异步生成的既有边界场景，`ChatPanelContent.handleToggleSummary` 流程不变）。

调用方 `ChatMessageItem.vue` 传入全局配置值（`localConfig.messageDisplayMode`）。更新对应单元测试。

### 5. 原文模式懒加载 `ChatPanelContent.vue` / `ChatMessageItem.vue`

- 新增触发条件（`ChatMessageItem.vue` computed + watcher）：全局原文模式 且 无单条偏好 且 有摘要 且 `blocks` 为空 且 未在加载 → `emit('ensure-content', msg)`，由 `ChatPanelContent.vue` 处理为 `ensureMessageContent(msg)`（新增事件绑定，仿照现有 `toggle-summary` 事件流）。
- **加载占位**：`msg._loadingOriginal === true` 期间 `showSummary` 计算为 `true`，继续渲染摘要作为占位，避免空白气泡；元信息栏显示现有「加载原文中...」（`chat.message.loadingOriginal`）。
- 加载完成：`msg.blocks` 填充、`_loadingOriginal` 置回 false → `showSummary` 变 `false` → 自动切换为原文（缓存复用，不再重复请求）。
- 加载失败：沿用现有 `ensureMessageContent` 错误处理（`_loadingOriginal` 复位、保留摘要占位与单条切换能力）。

### 6. 数据流

```
设置修改 → setLocalConfig('messageDisplayMode', v) → localStorage 持久化
        → 聊天视图 shouldShowSummary(defaultMode) 读取全局默认 → 生效
原文模式 → 消息可见 → ensureMessageContent → GET /api/rag/message?id=...
        → msg.blocks 填充 → 显示原文（缓存复用）
```

## 错误处理与边界

- localStorage 不可写（隐私模式等）：`setLocalConfig` 已有 try/catch，静默降级为会话内生效。
- 懒加载失败：沿用现有 `ensureMessageContent` 错误处理，回退摘要显示。
- 未配置过的新用户：默认 `'summary'`，行为与现状完全一致。

## 测试

- `web/src/utils/chatSessionUtils.test.ts`：`shouldShowSummary` 新增用例——全局默认摘要/原文各分支、单条偏好覆盖全局、无摘要→原文、blocks 空→摘要。
- `useSettingsConfig` 相关测试：`messageDisplayMode` 默认值、`setLocalConfig` 读写持久化。
- `settingsFieldMap` 结构测试（如存在）：确认聊天类别包含新项、key 唯一、i18n 键存在。