# Design: 上下文感知的快捷键轮播 + 全部快捷键对话框

## 目标

将 PC/Web 顶栏的快捷键轮播（`ShortcutTipTicker`）改造为**上下文感知**：轮播内容根据当前打开的 tab 动态组合「当前页面快捷键 + 公共快捷键 + 常驻聊天快捷键」。同时**点击轮播区域**打开一个对话框，以**按作用上下文分组的多个表格**展示全部快捷键，便于用户速查。

## 背景与现状

- 现有轮播：`web/src/components/common/ShortcutTipTicker.vue`，数据源 `web/src/config/shortcutTips.ts`（扁平 `SHORTCUT_TIPS: ShortcutTipDef[]`），仅在 PC/Web 非 APP 模式渲染（`AppHeader.vue:35`）。
- 现有 `ShortcutTipDef`：`{ contextKey, keys?, actionKey }`（i18n 键）。测试 `web/src/config/__tests__/shortcutTips.test.ts` 强制每条 tip 的双语 key 都存在。
- 现有上下文解析：`App.vue` 用 `chatShortcutActive` / `fileManagerShortcutActive` 做焦点感知 gating（`App.vue:598-599`），依据 `useWideScreenLayout()` 的 `isWideScreen` / `leftTab` / `activePane` 与 `activeTab`。`App.vue` 已 `provide('activeTab', activeTab)`（`App.vue:1967`）。
- 复用壳组件：`ModalDialog.vue`（`modal-body` 自带 `overflow-y:auto` 滚动）。

## 范围

- 仅 PC/Web 非 APP 模式（沿用现有 `v-if="!isAppMode && localConfig.headerShortcutTips"`）。
- 数据模型、轮播组件、点击对话框、i18n、测试。
- **任务(tasks) tab 无快捷键**，不生成内容。

## 架构

### 1. 数据模型 `web/src/config/shortcutTips.ts`

```ts
export type ShortcutContext =
  | 'common'   // 公共/全局，任何 tab 都显示
  | 'chat'     // 常驻聊天（PC 模式聊天面板常驻，任何 tab 都显示）
  | 'browse'   // 文件管理器
  | 'view'     // 文件查看/编辑器
  | 'terminal' // 终端
  | 'history'  // Git 历史
  | 'tasks'    // 任务（当前无内容）
  | 'settings' // 设置
  | 'proxy'    // 端口转发

export interface ShortcutTipDef {
  context: ShortcutContext   // 所属分组
  contextKey: string
  keys?: string[]
  actionKey: string
}

/** 分组展示顺序（对话框表格顺序）。 */
export const SHORTCUT_CONTEXT_ORDER: ShortcutContext[]

/** 返回指定上下文下的轮播列表：common 恒包含 + chat 恒包含（常驻）+ ctx 自身的提示。 */
export function getShortcutTipsForContext(ctx: ShortcutContext): ShortcutTipDef[]

/** 返回全部提示（对话框用），已按 SHORTCUT_CONTEXT_ORDER 排序。 */
export function getAllShortcutTips(): ShortcutTipDef[]
```

`SHORTCUT_CONTEXT_ORDER`（含 i18n 分组标签 key）：
`common, chat, browse, view, terminal, history, settings, proxy, tasks`。

### 2. 轮播组件 `ShortcutTipTicker.vue`

- 新增 `context?: ShortcutContext` prop（默认 `'chat'`）。
- 展示列表由 `getShortcutTipsForContext(context)` 计算；保留 `tips?: ShortcutTipDef[]` prop 覆盖（供测试注入，优先级最高）。
- `watch` 扩展为同时观察 `context` 与 `tips`：任一变化 → 重置 `currentIndex=0`、清除动画状态并重排。
- 轮播根节点加 `cursor:pointer`，并允许 attribute fallthrough（供 AppHeader 挂 `@click` / `title`）。

### 3. 新组件 `web/src/components/common/ShortcutTipsDialog.vue`

复用 `ModalDialog` 外壳（`maxWidth` 取较大值，如 720；`title` = 本地化标题，含总数）：

- 遍历 `SHORTCUT_CONTEXT_ORDER`，对每个含提示的分组渲染一个**独立表格**：
  - 分组标题：本地化标签（公共 / 聊天 / 文件管理器 / …）。
  - 表头列：**按键**（`<kbd>` 渲染 `keys`；无 `keys` 显示 `—`）| **位置/前提**（`t(contextKey)`）| **说明**（`t(actionKey)`）。
- 无提示的分组不渲染表格。
- 全部无提示时显示空态文案。
- 关闭：Esc / 遮罩点击 / 关闭按钮（ModalDialog 自带）。

### 4. 接入 `AppHeader.vue`

- 解析上下文（与现有快捷键 gating 一致）：

```ts
const { isWideScreen, leftTab, activePane } = useWideScreenLayout()
const activeTab = inject<Ref<string>>('activeTab', ref('chat'))
const shortcutContext = computed<ShortcutContext>(() =>
  isWideScreen.value ? (activePane.value === 'right' ? 'chat' : leftTab.value)
                     : (activeTab.value as ShortcutContext))
```

- 渲染：`<ShortcutTipTicker :context="shortcutContext" @click="shortcutTipsOpen = true" title=… />`。
- 新增 `const shortcutTipsOpen = ref(false)`，并渲染 `<ShortcutTipsDialog :open="shortcutTipsOpen" @close="shortcutTipsOpen = false" />`。
- 轮播 `cursor:pointer` 样式落在 `AppHeader`（或 ticker scoped）中。

### 5. i18n（zh + en）

- 为每条新增 tip 补充 `appHeader.shortcutTip.contextXxx` / `actionXxx`。
- 新增分组标签：`appHeader.shortcutTipGroup.common/chat/browse/view/terminal/history/settings/proxy/tasks`。
- 新增对话框文案：`appHeader.shortcutTipsDialog.title`（含 `{count}` 总数）、`tableColumns.key/context/action`、`empty`。

## 提示内容（按分组）

- **common**：Ctrl+F 搜索；Esc 关闭弹层/对话框；Enter 确认对话框；↑/↓+Enter 列表导航。
- **chat（常驻）**：Enter/Shift+Enter 发送/换行；Ctrl+←/→ 切换会话；Ctrl+U 未读；Ctrl+K 会话列表；Ctrl+Delete 归档；Ctrl+↑/↓ 消息跳转；F9 按住录音；（保留原「推荐回复」两条）。
- **browse**：Ctrl+C/X/V 复制剪切粘贴；Delete/Shift+Delete 删除/强制删除；Ctrl+A 多选全选；F2 重命名；Alt+↑/Backspace 上级目录；Ctrl+R/F5 刷新；Ctrl+Shift+H 隐藏文件；Ctrl+Shift+M 多选模式；Ctrl+N / Ctrl+Shift+N 新建文件/文件夹；Ctrl+1/2 列表/网格。
- **view**：Ctrl+S 保存；Ctrl+Z / Ctrl+Y 撤销/重做；←/→ 图片切换；Ctrl+滚轮 缩放。
- **terminal**：Ctrl+C 中断；Ctrl+D 退出；Ctrl+L 清屏。
- **history**：↑/↓/Enter 提交列表导航。
- **settings**：Enter 确认内联编辑。
- **proxy**：Enter 保存端口表单；Esc 关闭。
- **tasks**：无。

## 错误处理与边界

- 空上下文/无提示：轮播不渲染、不报错。
- 仅一条：停留展示不跳变。
- `keys` 缺失：不渲染 `<kbd>`（对话框表格中显示 `—`）。
- 定时器卸载清理（沿用现有逻辑）。
- 宽屏 `activePane==='right'` 时上下文归为 `chat`；聊天提示始终存在，无重复。

## 测试

`web/src/config/__tests__/shortcutTips.test.ts` 扩展：
- `getShortcutTipsForContext`：`common` 恒含、`chat` 常驻恒含；`ctx` 只含自身+common+chat；`ctx='chat'` 不重复；顺序 = common+chat+ctx。
- 每条 tip 双语 key 完整性（沿用）。
- `getAllShortcutTips` 按 `SHORTCUT_CONTEXT_ORDER` 排序、无重复。

`web/src/components/common/__tests__/ShortcutTipTicker.test.ts` 扩展：
- 传入 `context` 后展示对应列表（common+chat+ctx）。
- `context` 变化 → 重置到第一条并重排。

新增 `web/src/components/common/__tests__/ShortcutTipsDialog.test.ts`：
- 按分组渲染多个表格（表头与行正确，`<kbd>` 渲染）。
- 无提示分组不渲染；整体无数据空态。

## 验收

- PC 顶栏轮播随当前 tab 切换内容：始终含公共 + 聊天快捷键，另含当前页面快捷键。
- 点击轮播打开「全部快捷键」对话框，按上下文分组展示多个表格，Esc/遮罩可关闭。
- 新增/调整提示仅改 i18n + 数据数组/分组。
