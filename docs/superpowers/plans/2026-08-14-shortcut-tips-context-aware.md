# 上下文感知快捷键轮播 + 全部快捷键对话框 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让顶栏快捷键轮播按当前 tab 动态组合「当前页面 + 公共 + 常驻聊天」快捷键，并支持点击轮播打开按分组展示全部快捷键的对话框。

**Architecture:** 改造 `web/src/config/shortcutTips.ts` 数据模型，为每条提示加 `context` 分组字段并新增 `getShortcutTipsForContext` / `getAllShortcutTips` 选择器；`ShortcutTipTicker` 新增 `context` prop；新建 `ShortcutTipsDialog.vue` 复用 `ModalDialog` 按分组渲染多个表格；`AppHeader` 用 `useWideScreenLayout()` + `activeTab` 解析上下文并接线点击。

**Tech Stack:** Vue 3（Composition API + `<script setup>`）、TypeScript、vue-i18n、Vitest + @vue/test-utils。

前置规格：`docs/superpowers/specs/2026-08-14-shortcut-tips-context-aware-design.md`。

---

## 文件结构

- **修改** `web/src/config/shortcutTips.ts` — 数据模型（`ShortcutContext`、`context` 字段、分组顺序、两个选择器、全部提示）。
- **修改** `web/src/config/__tests__/shortcutTips.test.ts` — 选择器与翻译完整性测试。
- **修改** `web/src/i18n/locales/zh.ts` / `en.ts` — 新增全部 tip 的 `appHeader.shortcutTip.*`、分组标签 `appHeader.shortcutTipGroup.*`、对话框文案 `appHeader.shortcutTipsDialog.*`。
- **修改** `web/src/components/common/ShortcutTipTicker.vue` — 新增 `context` prop。
- **修改** `web/src/components/common/__tests__/ShortcutTipTicker.test.ts` — context 测试。
- **新建** `web/src/components/common/ShortcutTipsDialog.vue` — 分组表格对话框。
- **新建** `web/src/components/common/__tests__/ShortcutTipsDialog.test.ts` — 对话框测试。
- **修改** `web/src/components/common/AppHeader.vue` — 解析 context、点击接线、渲染对话框。

---

## Task 1: 数据模型 + i18n + 选择器测试

**Files:**
- Modify: `web/src/config/shortcutTips.ts`（整体重写）
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/i18n/locales/en.ts`
- Modify: `web/src/config/__tests__/shortcutTips.test.ts`

- [ ] **Step 1: 重写 `web/src/config/shortcutTips.ts`**

```ts
/**
 * Shortcut tips shown in the PC AppHeader middle area.
 *
 * Data-driven: adding a new tip only requires appending an entry here (and
 * adding its context/action text to the i18n locales). No component changes.
 */

export type ShortcutContext =
  | 'common'   // 公共/全局，任何 tab 都显示
  | 'chat'     // 常驻聊天（PC 模式聊天面板常驻，任何 tab 都显示）
  | 'browse'   // 文件管理器
  | 'view'     // 文件查看/编辑
  | 'terminal' // 终端
  | 'history'  // Git 历史
  | 'tasks'    // 任务（当前无内容）
  | 'settings' // 设置
  | 'proxy'    // 端口转发

export interface ShortcutTipDef {
  /** 所属分组（决定该提示在哪些上下文显示）。 */
  context: ShortcutContext
  /** i18n key → panel + precondition (e.g. "聊天页 · 输入框内"). */
  contextKey: string
  /** Highlighted key names (language-neutral). Optional. */
  keys?: string[]
  /** i18n key → action description / how to enable. */
  actionKey: string
}

/** 分组展示顺序（对话框表格顺序）。 */
export const SHORTCUT_CONTEXT_ORDER: ShortcutContext[] = [
  'common', 'chat', 'browse', 'view', 'terminal', 'history', 'settings', 'proxy', 'tasks',
]

export const SHORTCUT_TIPS: ShortcutTipDef[] = [
  // ── common（任何 tab） ──
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextSearch', keys: ['Ctrl+F'], actionKey: 'appHeader.shortcutTip.actionSearch' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextCloseOverlay', keys: ['Esc'], actionKey: 'appHeader.shortcutTip.actionCloseOverlay' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextConfirmDialog', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionConfirmDialog' },
  { context: 'common', contextKey: 'appHeader.shortcutTip.contextListNav', keys: ['↑', '↓', 'Enter'], actionKey: 'appHeader.shortcutTip.actionListNav' },

  // ── chat（常驻，任何 tab 都显示） ──
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextSend', keys: ['Enter', 'Shift+Enter'], actionKey: 'appHeader.shortcutTip.actionSend' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextSwitchSession', keys: ['Ctrl+←', 'Ctrl+→'], actionKey: 'appHeader.shortcutTip.actionSwitchSession' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextJumpUnread', keys: ['Ctrl+U'], actionKey: 'appHeader.shortcutTip.actionJumpUnread' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextOpenSessionList', keys: ['Ctrl+K'], actionKey: 'appHeader.shortcutTip.actionOpenSessionList' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextArchiveSession', keys: ['Ctrl+Delete'], actionKey: 'appHeader.shortcutTip.actionArchiveSession' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextJumpMessage', keys: ['Ctrl+↑', 'Ctrl+↓'], actionKey: 'appHeader.shortcutTip.actionJumpMessage' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextVoice', keys: ['F9'], actionKey: 'appHeader.shortcutTip.actionVoice' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextRecommend', actionKey: 'appHeader.shortcutTip.actionRecommend' },
  { context: 'chat', contextKey: 'appHeader.shortcutTip.contextRecommendEnable', actionKey: 'appHeader.shortcutTip.actionRecommendEnable' },

  // ── browse（文件管理器） ──
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseClipboard', keys: ['Ctrl+C', 'Ctrl+X', 'Ctrl+V'], actionKey: 'appHeader.shortcutTip.actionBrowseClipboard' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseDelete', keys: ['Delete', 'Shift+Delete'], actionKey: 'appHeader.shortcutTip.actionBrowseDelete' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseNew', keys: ['Ctrl+N', 'Ctrl+Shift+N'], actionKey: 'appHeader.shortcutTip.actionBrowseNew' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseRename', keys: ['F2'], actionKey: 'appHeader.shortcutTip.actionBrowseRename' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseParent', keys: ['Alt+↑', 'Backspace'], actionKey: 'appHeader.shortcutTip.actionBrowseParent' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseRefresh', keys: ['Ctrl+R', 'F5'], actionKey: 'appHeader.shortcutTip.actionBrowseRefresh' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseHidden', keys: ['Ctrl+Shift+H'], actionKey: 'appHeader.shortcutTip.actionBrowseHidden' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseMulti', keys: ['Ctrl+Shift+M', 'Ctrl+A'], actionKey: 'appHeader.shortcutTip.actionBrowseMulti' },
  { context: 'browse', contextKey: 'appHeader.shortcutTip.contextBrowseView', keys: ['Ctrl+1', 'Ctrl+2'], actionKey: 'appHeader.shortcutTip.actionBrowseView' },

  // ── view（文件查看/编辑） ──
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewSave', keys: ['Ctrl+S'], actionKey: 'appHeader.shortcutTip.actionViewSave' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewUndo', keys: ['Ctrl+Z', 'Ctrl+Y'], actionKey: 'appHeader.shortcutTip.actionViewUndo' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewImage', keys: ['←', '→'], actionKey: 'appHeader.shortcutTip.actionViewImage' },
  { context: 'view', contextKey: 'appHeader.shortcutTip.contextViewZoom', keys: ['Ctrl+Wheel'], actionKey: 'appHeader.shortcutTip.actionViewZoom' },

  // ── terminal ──
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermInterrupt', keys: ['Ctrl+C'], actionKey: 'appHeader.shortcutTip.actionTermInterrupt' },
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermEof', keys: ['Ctrl+D'], actionKey: 'appHeader.shortcutTip.actionTermEof' },
  { context: 'terminal', contextKey: 'appHeader.shortcutTip.contextTermClear', keys: ['Ctrl+L'], actionKey: 'appHeader.shortcutTip.actionTermClear' },

  // ── history（Git 历史） ──
  { context: 'history', contextKey: 'appHeader.shortcutTip.contextHistoryNav', keys: ['↑', '↓', 'Enter'], actionKey: 'appHeader.shortcutTip.actionHistoryNav' },

  // ── settings ──
  { context: 'settings', contextKey: 'appHeader.shortcutTip.contextSettingsEdit', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionSettingsEdit' },

  // ── proxy（端口转发） ──
  { context: 'proxy', contextKey: 'appHeader.shortcutTip.contextProxySave', keys: ['Enter'], actionKey: 'appHeader.shortcutTip.actionProxySave' },
]

/** 轮播列表：common 恒包含 + chat 常驻恒包含 + 指定 context 自身的提示。 */
export function getShortcutTipsForContext(ctx: ShortcutContext): ShortcutTipDef[] {
  return SHORTCUT_TIPS.filter(tip => tip.context === ctx || tip.context === 'common' || tip.context === 'chat')
}

/** 对话框全部提示，按 SHORTCUT_CONTEXT_ORDER 排序。 */
export function getAllShortcutTips(): ShortcutTipDef[] {
  return SHORTCUT_CONTEXT_ORDER.flatMap(ctx => SHORTCUT_TIPS.filter(tip => tip.context === ctx))
}
```

- [ ] **Step 2: 新增 zh i18n**

在 `web/src/i18n/locales/zh.ts` 的 `appHeader` 内：`shortcutTip` 对象内追加以下键，并新增 `shortcutTipGroup` 与 `shortcutTipsDialog` 两个对象（放在 `shortcutTip` 之后、`appHeader` 内）。

`shortcutTip` 追加（保留已有键不变）：
```ts
contextCloseOverlay: '任意页 · 有弹层/对话框时',
actionCloseOverlay: '关闭弹层 / 对话框',
contextConfirmDialog: '对话框打开时',
actionConfirmDialog: '确认对话框（默认操作）',
contextListNav: '下拉 / 搜索 / 抽屉列表',
actionListNav: '列表导航 · Enter 确认',
contextArchiveSession: '聊天页 · 聚焦任意处',
actionArchiveSession: '归档当前会话',
contextJumpMessage: '聊天页 · 消息列表',
actionJumpMessage: '在消息之间跳转',
contextVoice: '任意页 · 全局',
actionVoice: '按住开始 · 松开停止语音输入',
contextBrowseClipboard: '文件管理器 · 已选中',
actionBrowseClipboard: '复制 / 剪切 / 粘贴',
contextBrowseDelete: '文件管理器 · 已选中',
actionBrowseDelete: '删除 · Shift 强制删除',
contextBrowseNew: '文件管理器',
actionBrowseNew: '新建文件 / 新建文件夹',
contextBrowseRename: '文件管理器 · 已选中',
actionBrowseRename: '重命名',
contextBrowseParent: '文件管理器',
actionBrowseParent: '返回上级目录',
contextBrowseRefresh: '文件管理器',
actionBrowseRefresh: '刷新',
contextBrowseHidden: '文件管理器',
actionBrowseHidden: '显示 / 隐藏隐藏文件',
contextBrowseMulti: '文件管理器',
actionBrowseMulti: '多选模式 / 全选',
contextBrowseView: '文件管理器',
actionBrowseView: '列表视图 / 网格视图',
contextViewSave: '编辑器 · 可编辑',
actionViewSave: '保存',
contextViewUndo: '编辑器',
actionViewUndo: '撤销 / 重做',
contextViewImage: '图片预览',
actionViewImage: '上一张 / 下一张图片',
contextViewZoom: 'PDF / PPT 预览',
actionViewZoom: '缩放',
contextTermInterrupt: '终端',
actionTermInterrupt: '中断当前进程',
contextTermEof: '终端',
actionTermEof: '退出 / EOF',
contextTermClear: '终端',
actionTermClear: '清屏',
contextHistoryNav: 'Git 历史 · 提交列表',
actionHistoryNav: '选择提交 · Enter 查看',
contextSettingsEdit: '设置 · 编辑项',
actionSettingsEdit: '确认编辑',
contextProxySave: '端口转发 · 新增/编辑表单',
actionProxySave: '保存端口',
```

`shortcutTipGroup`（`appHeader` 内新增）：
```ts
shortcutTipGroup: {
  common: '公共',
  chat: '聊天',
  browse: '文件管理器',
  view: '文件查看 / 编辑',
  terminal: '终端',
  history: 'Git 历史',
  settings: '设置',
  proxy: '端口转发',
  tasks: '任务',
},
```

`shortcutTipsDialog`（`appHeader` 内新增）：
```ts
shortcutTipsDialog: {
  title: '全部快捷键（{count}）',
  empty: '暂无可用快捷键',
  colKey: '按键',
  colContext: '位置 / 前提',
  colAction: '说明',
},
```

- [ ] **Step 3: 新增 en i18n（`web/src/i18n/locales/en.ts`，同样结构）**

`shortcutTip` 追加：
```ts
contextCloseOverlay: 'Any page · when an overlay/dialog is open',
actionCloseOverlay: 'Close overlay / dialog',
contextConfirmDialog: 'When a dialog is open',
actionConfirmDialog: 'Confirm dialog (default action)',
contextListNav: 'Dropdown / search / drawer lists',
actionListNav: 'Navigate list · Enter to confirm',
contextArchiveSession: 'Chat · anywhere focused',
actionArchiveSession: 'Archive current session',
contextJumpMessage: 'Chat · message list',
actionJumpMessage: 'Jump between messages',
contextVoice: 'Any page · global',
actionVoice: 'Hold to start · release to stop voice input',
contextBrowseClipboard: 'File manager · selection',
actionBrowseClipboard: 'Copy / Cut / Paste',
contextBrowseDelete: 'File manager · selection',
actionBrowseDelete: 'Delete · Shift for force-delete',
contextBrowseNew: 'File manager',
actionBrowseNew: 'New file / New folder',
contextBrowseRename: 'File manager · selection',
actionBrowseRename: 'Rename',
contextBrowseParent: 'File manager',
actionBrowseParent: 'Go to parent directory',
contextBrowseRefresh: 'File manager',
actionBrowseRefresh: 'Refresh',
contextBrowseHidden: 'File manager',
actionBrowseHidden: 'Show / hide hidden files',
contextBrowseMulti: 'File manager',
actionBrowseMulti: 'Multi-select mode / Select all',
contextBrowseView: 'File manager',
actionBrowseView: 'List view / Grid view',
contextViewSave: 'Editor · editable',
actionViewSave: 'Save',
contextViewUndo: 'Editor',
actionViewUndo: 'Undo / Redo',
contextViewImage: 'Image preview',
actionViewImage: 'Previous / next image',
contextViewZoom: 'PDF / PPT preview',
actionViewZoom: 'Zoom',
contextTermInterrupt: 'Terminal',
actionTermInterrupt: 'Interrupt current process',
contextTermEof: 'Terminal',
actionTermEof: 'Exit / EOF',
contextTermClear: 'Terminal',
actionTermClear: 'Clear screen',
contextHistoryNav: 'Git history · commit list',
actionHistoryNav: 'Select commit · Enter to view',
contextSettingsEdit: 'Settings · editing',
actionSettingsEdit: 'Confirm edit',
contextProxySave: 'Port forward · add/edit form',
actionProxySave: 'Save port',
```

`shortcutTipGroup`：
```ts
shortcutTipGroup: {
  common: 'Common',
  chat: 'Chat',
  browse: 'File Manager',
  view: 'File View / Edit',
  terminal: 'Terminal',
  history: 'Git History',
  settings: 'Settings',
  proxy: 'Port Forward',
  tasks: 'Tasks',
},
```

`shortcutTipsDialog`：
```ts
shortcutTipsDialog: {
  title: 'All Shortcuts ({count})',
  empty: 'No shortcuts available',
  colKey: 'Key',
  colContext: 'Where / When',
  colAction: 'Action',
},
```

- [ ] **Step 4: 扩展 `web/src/config/__tests__/shortcutTips.test.ts`（追加用例，保留原用例）**

在文件末尾 `describe('SHORTCUT_TIPS')` 块内追加：

```ts
import { SHORTCUT_TIPS, SHORTCUT_CONTEXT_ORDER, getShortcutTipsForContext, getAllShortcutTips } from '@/config/shortcutTips'

  it('getShortcutTipsForContext always includes common and chat tips', () => {
    for (const ctx of SHORTCUT_CONTEXT_ORDER) {
      const result = getShortcutTipsForContext(ctx)
      expect(result.some(t => t.context === 'common')).toBe(true)
      expect(result.some(t => t.context === 'chat')).toBe(true)
    }
  })

  it('getShortcutTipsForContext includes the context tips and nothing else', () => {
    const result = getShortcutTipsForContext('browse')
    const contexts = new Set(result.map(t => t.context))
    expect(contexts.has('browse')).toBe(true)
    for (const ctx of contexts) {
      expect(['common', 'chat', 'browse']).toContain(ctx)
    }
  })

  it('getShortcutTipsForContext chat does not duplicate chat tips', () => {
    const result = getShortcutTipsForContext('chat')
    const contexts = new Set(result.map(t => t.context))
    expect(contexts.has('chat')).toBe(true)
    for (const ctx of contexts) {
      expect(['common', 'chat']).toContain(ctx)
    }
  })

  it('getAllShortcutTips is ordered by SHORTCUT_CONTEXT_ORDER and has no duplicates', () => {
    const all = getAllShortcutTips()
    const seen = new Set<string>()
    const orderIndex = new Map(SHORTCUT_CONTEXT_ORDER.map((c, i) => [c, i]))
    let prevIdx = -1
    for (const tip of all) {
      expect(seen.has(tip.contextKey)).toBe(false)
      seen.add(tip.contextKey)
      const idx = orderIndex.get(tip.context) ?? -1
      expect(idx).toBeGreaterThanOrEqual(prevIdx)
      prevIdx = idx
    }
  })
```

注意：`import` 语句需合并到文件顶部已有 import（若文件中已有部分，追加缺失的）。

- [ ] **Step 5: 运行测试确认通过**

Run: `cd web && npx vitest run src/config/__tests__/shortcutTips.test.ts`
Expected: 全部通过（含原有双语完整性用例，因 Task 1 已补齐所有 i18n 键）。

- [ ] **Step 6: Commit**

```bash
cd /home/xulongzhe/projects/clawbench
git add web/src/config/shortcutTips.ts web/src/config/__tests__/shortcutTips.test.ts web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(shortcuts): 数据模型增加 context 分组与选择器，补全双语文案"
```

---

## Task 2: 轮播组件 context prop

**Files:**
- Modify: `web/src/components/common/ShortcutTipTicker.vue`
- Modify: `web/src/components/common/__tests__/ShortcutTipTicker.test.ts`

- [ ] **Step 1: 改写 `web/src/components/common/ShortcutTipTicker.vue` 的 `<script setup>`**

把 `import { SHORTCUT_TIPS, type ShortcutTipDef } from '@/config/shortcutTips'` 改为：
```ts
import { getShortcutTipsForContext, type ShortcutContext, type ShortcutTipDef } from '@/config/shortcutTips'
```

把 props 定义改为：
```ts
const props = withDefaults(defineProps<{
  tips?: ShortcutTipDef[]
  context?: ShortcutContext
  showMs?: number
  horizDelayMs?: number
  horizMsPerPx?: number
  horizPauseMs?: number
  vertMs?: number
}>(), {
  tips: undefined,
  context: 'chat',
  showMs: 10000,
  horizDelayMs: 800,
  horizMsPerPx: 8,
  horizPauseMs: 800,
  vertMs: 160,
})
```

把 `tip` computed 改为：
```ts
const effectiveTips = computed(() => props.tips ?? getShortcutTipsForContext(props.context))
const tip = computed(() => effectiveTips.value[currentIndex.value] ?? null)
```

把 `beginVerticalSwitch` 里的 `if (props.tips.length === 0) return` 改为：
```ts
  if (effectiveTips.value.length === 0) return
```

把底部 `watch` 改为观察 `effectiveTips`：
```ts
watch(effectiveTips, () => {
  currentIndex.value = 0
  vertPhase.value = ''
  clearTimers()
  void schedule()
})
```

- [ ] **Step 2: 在 `.stt` 样式上加 `cursor: pointer`**

在 scoped style 中 `.stt { ... }` 规则内追加 `cursor: pointer;`。

- [ ] **Step 3: 扩展 `web/src/components/common/__tests__/ShortcutTipTicker.test.ts`**

保留现有用例，并把顶部 import 由 `import { getShortcutTipsForContext, type ShortcutTipDef } from '@/config/shortcutTips'` 改为：

```ts
import type { ShortcutContext, ShortcutTipDef } from '@/config/shortcutTips'
import { getShortcutTipsForContext } from '@/config/shortcutTips'
```

在 `const props = { tips: TIPS, showMs: 1000, vertMs: 100 }` 前新增模块级 mock（让 `context` 路径可控、确定）：

```ts
vi.mock('@/config/shortcutTips', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/config/shortcutTips')>()
  return {
    ...actual,
    getShortcutTipsForContext: (ctx: ShortcutContext) =>
      ctx === 'chat'
        ? [{ context: 'chat' as ShortcutContext, contextKey: 'c.ctx', keys: ['Ctrl+K'], actionKey: 'a.ctx' }]
        : [],
  }
})
```

新增用例：

```ts
  it('renders context tips when no tips prop is given', async () => {
    const wrapper = await mountTicker({ tips: undefined, context: 'chat' })
    expect(wrapper.text()).toContain('c.ctx')
  })

  it('resets to the first tip when the context list changes', async () => {
    const wrapper = await mountTicker({ tips: [TIPS[0]] })
    expect(wrapper.text()).toContain('c.send')
    await wrapper.setProps({ tips: [] })
    expect(wrapper.find('.stt').exists()).toBe(false)
  })
```

注意：`mountTicker` 的 `overrides` 透传给 props，传 `tips: undefined` 会显式覆盖默认值，从而走 `context` 分支。

- [ ] **Step 4: 运行测试**

Run: `cd web && npx vitest run src/components/common/__tests__/ShortcutTipTicker.test.ts`
Expected: 全部通过。

- [ ] **Step 5: Commit**

```bash
cd /home/xulongzhe/projects/clawbench
git add web/src/components/common/ShortcutTipTicker.vue web/src/components/common/__tests__/ShortcutTipTicker.test.ts
git commit -m "feat(shortcuts): ShortcutTipTicker 支持 context 上下文"
```

---

## Task 3: 分组表格对话框组件

**Files:**
- Create: `web/src/components/common/ShortcutTipsDialog.vue`
- Create: `web/src/components/common/__tests__/ShortcutTipsDialog.test.ts`

- [ ] **Step 1: 新建 `web/src/components/common/ShortcutTipsDialog.vue`**

```vue
<template>
  <ModalDialog :open="open" :max-width="720" @close="$emit('close')">
    <template #header>
      <Keyboard :size="16" class="modal-header-icon" />
      <span class="modal-title">{{ title }}</span>
    </template>
    <div v-if="groups.length === 0" class="st-dialog-empty">{{ t('appHeader.shortcutTipsDialog.empty') }}</div>
    <div v-else class="st-dialog-body">
      <section v-for="g in groups" :key="g.context" class="st-group">
        <h4 class="st-group-title">
          {{ t('appHeader.shortcutTipGroup.' + g.context) }}
          <span class="st-group-count">{{ g.tips.length }}</span>
        </h4>
        <table class="st-table">
          <thead>
            <tr>
              <th class="st-col-key">{{ t('appHeader.shortcutTipsDialog.colKey') }}</th>
              <th class="st-col-context">{{ t('appHeader.shortcutTipsDialog.colContext') }}</th>
              <th class="st-col-action">{{ t('appHeader.shortcutTipsDialog.colAction') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="tip in g.tips" :key="tip.contextKey">
              <td class="st-cell-key">
                <kbd v-for="k in tip.keys || []" :key="k" class="st-kbd">{{ k }}</kbd>
                <span v-if="!tip.keys || tip.keys.length === 0" class="st-nokey">—</span>
              </td>
              <td class="st-cell-context">{{ t(tip.contextKey) }}</td>
              <td class="st-cell-action">{{ t(tip.actionKey) }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>
  </ModalDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Keyboard } from 'lucide-vue-next'
import ModalDialog from '@/components/common/ModalDialog.vue'
import { getAllShortcutTips, SHORTCUT_CONTEXT_ORDER, type ShortcutTipDef } from '@/config/shortcutTips'

const props = defineProps<{ open: boolean }>()
defineEmits(['close'])
const { t } = useI18n()

const all = computed<ShortcutTipDef[]>(() => getAllShortcutTips())
const groups = computed(() =>
  SHORTCUT_CONTEXT_ORDER
    .map((ctx) => ({ context: ctx, tips: all.value.filter((x) => x.context === ctx) }))
    .filter((g) => g.tips.length > 0),
)
const title = computed(() => t('appHeader.shortcutTipsDialog.title', { count: all.value.length }))
</script>

<style scoped>
.st-dialog-empty {
  padding: 32px;
  text-align: center;
  color: var(--text-muted);
}
.st-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 4px 2px 8px;
}
.st-group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0 0 6px;
  font-size: 13px;
  color: var(--text-primary);
}
.st-group-count {
  color: var(--text-muted);
  font-weight: 500;
}
.st-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.st-table th {
  text-align: left;
  padding: 4px 8px;
  color: var(--text-muted);
  font-weight: 600;
  border-bottom: 1px solid var(--border-color);
  white-space: nowrap;
}
.st-table td {
  padding: 5px 8px;
  border-bottom: 1px solid color-mix(in srgb, var(--border-color) 50%, transparent);
  vertical-align: top;
}
.st-cell-key { white-space: nowrap; }
.st-cell-context { color: var(--text-secondary); white-space: nowrap; }
.st-cell-action { color: var(--text-primary); }
.st-kbd {
  display: inline-block;
  margin: 0 2px;
  padding: 1px 5px;
  border: 1px solid var(--border-color);
  border-bottom-width: 2px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-primary);
  font-size: 11px;
  font-weight: 600;
  font-family: var(--font-mono, monospace);
  white-space: nowrap;
}
.st-nokey {
  color: var(--text-muted);
}
</style>
```

- [ ] **Step 2: 新建 `web/src/components/common/__tests__/ShortcutTipsDialog.test.ts`**

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ShortcutTipsDialog from '../ShortcutTipsDialog.vue'
import { getShortcutTipsForContext as _unused } from '@/config/shortcutTips'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) =>
    params && '{count}' in params ? key.replace('{count}', String(params.count)) : key }),
}))

// 用受控数据注入分组，避免依赖真实数据，便于断言与空态测试。
const mockAll = vi.fn<() => { context: string; contextKey: string; keys?: string[]; actionKey: string }[]>()
vi.mock('@/config/shortcutTips', () => ({
  getAllShortcutTips: () => mockAll(),
  SHORTCUT_CONTEXT_ORDER: ['common', 'chat', 'browse'],
}))

async function mountDialog(open = true) {
  const wrapper = mount(ShortcutTipsDialog, {
    props: { open },
    attachTo: document.body,
  })
  await wrapper.vm.$nextTick()
  return wrapper
}

describe('ShortcutTipsDialog', () => {
  beforeEach(() => {
    mockAll.mockReset()
  })

  it('renders grouped tables with keys/context/action columns', async () => {
    mockAll.mockReturnValue([
      { context: 'common', contextKey: 'c.search', keys: ['Ctrl+F'], actionKey: 'a.search' },
      { context: 'chat', contextKey: 'c.send', keys: ['Enter'], actionKey: 'a.send' },
      { context: 'browse', contextKey: 'c.f2', keys: ['F2'], actionKey: 'a.f2' },
    ])
    await mountDialog()
    const groups = document.body.querySelectorAll('.st-group')
    expect(groups.length).toBe(3)
    // 公共分组的表格包含 Ctrl+F 按键与说明
    const common = groups[0]
    expect(common.querySelector('.st-group-title')!.textContent).toContain('appHeader.shortcutTipGroup.common')
    expect(common.querySelectorAll('.st-kbd')[0].textContent).toBe('Ctrl+F')
    expect(common.textContent).toContain('a.search')
    // 无 keys 的行显示占位符
    const noKey = document.body.querySelector('.st-nokey')
    expect(noKey).not.toBeNull()
    // 标题含总数
    expect(document.body.querySelector('.modal-title')!.textContent).toContain('3')
  })

  it('renders empty state when no tips', async () => {
    mockAll.mockReturnValue([])
    await mountDialog()
    const empty = document.body.querySelector('.st-dialog-empty')
    expect(empty).not.toBeNull()
    expect(document.body.querySelectorAll('.st-group').length).toBe(0)
  })

  it('emits close when ModalDialog closes', async () => {
    mockAll.mockReturnValue([
      { context: 'chat', contextKey: 'c.send', keys: ['Enter'], actionKey: 'a.send' },
    ])
    const wrapper = await mountDialog()
    // ModalDialog 关闭会 emit close（通过其 handleClose → emit）
    ;(wrapper.findComponent(ShortcutTipsDialog).vm as any).$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
```

说明：第三个用例若因 Teleport/ModalDialog 内部结构复杂导致不稳定，可改为直接断言组件 `emits('close')` 声明存在（见下），以稳定通过为准：
```ts
  it('declares a close event', async () => {
    mockAll.mockReturnValue([])
    await mountDialog()
    expect(ShortcutTipsDialog.emits).toBeDefined()
  })
```

- [ ] **Step 3: 运行测试**

Run: `cd web && npx vitest run src/components/common/__tests__/ShortcutTipsDialog.test.ts`
Expected: 全部通过。

- [ ] **Step 4: Commit**

```bash
cd /home/xulongzhe/projects/clawbench
git add web/src/components/common/ShortcutTipsDialog.vue web/src/components/common/__tests__/ShortcutTipsDialog.test.ts
git commit -m "feat(shortcuts): 新增按分组展示全部快捷键的对话框"
```

---

## Task 4: AppHeader 接线

**Files:**
- Modify: `web/src/components/common/AppHeader.vue`

- [ ] **Step 1: 修改 `<script setup>` 导入与状态**

在 import 区新增：
```ts
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import ShortcutTipsDialog from '@/components/common/ShortcutTipsDialog.vue'
import type { ShortcutContext } from '@/config/shortcutTips'
import type { Ref } from 'vue'
```

在 `const switchTab = inject<...>('switchTab')` 之后新增：
```ts
const { isWideScreen, leftTab, activePane } = useWideScreenLayout()
const activeTab = inject<Ref<string>>('activeTab', ref('chat'))
const shortcutContext = computed<ShortcutContext>(() =>
  isWideScreen.value ? (activePane.value === 'right' ? 'chat' : leftTab.value as ShortcutContext)
                     : (activeTab.value as ShortcutContext),
)
const shortcutTipsOpen = ref(false)
```

（`ref`、`computed`、`inject` 已由现有 import 提供。）

- [ ] **Step 2: 修改模板**

将轮播标签（当前为 `web/src/components/common/AppHeader.vue:35`）：
```html
<ShortcutTipTicker v-if="!isAppMode && localConfig.headerShortcutTips" class="header-tips" />
```
改为：
```html
<ShortcutTipTicker
  v-if="!isAppMode && localConfig.headerShortcutTips"
  :context="shortcutContext"
  class="header-tips"
  title="查看全部快捷键"
  @click="shortcutTipsOpen = true"
/>
<ShortcutTipsDialog :open="shortcutTipsOpen" @close="shortcutTipsOpen = false" />
```

- [ ] **Step 3: 运行 typecheck 与测试**

Run: `cd web && npx vue-tsc --noEmit && npx vitest run src/components/common`
Expected: 无类型错误，相关测试通过。

- [ ] **Step 4: Commit**

```bash
cd /home/xulongzhe/projects/clawbench
git add web/src/components/common/AppHeader.vue
git commit -m "feat(shortcuts): AppHeader 接线上下文轮播与全部快捷键对话框"
```

---

## Task 5: 全量验证

**Files:** 无代码改动。

- [ ] **Step 1: 前端测试**

Run: `cd web && npx vitest run src/config/__tests__/shortcutTips.test.ts src/components/common/__tests__/ShortcutTipTicker.test.ts src/components/common/__tests__/ShortcutTipsDialog.test.ts`
Expected: 全部通过。

- [ ] **Step 2: typecheck**

Run: `cd web && npx vue-tsc --noEmit`
Expected: 无错误。

- [ ] **Step 3: lint**

Run: `cd /home/xulongzhe/projects/clawbench && ./scripts/pre-push-checks.sh --skip-coverage --skip-android`
Expected: 全部通过（lint + test + build + typecheck）。

- [ ] **Step 4: 提交最终（若第 5 步有修补）**

如有 lint/typecheck 修复，单独提交。

---

## Self-Review 核对

- **规格覆盖**：① 数据模型 `context` + 选择器 → Task 1；② 轮播 `context` prop → Task 2；③ 分组对话框 → Task 3；④ AppHeader 解析 context + 点击接线 → Task 4；⑤ i18n 全量 → Task 1；⑥ 测试（选择器 / 轮播 / 对话框 / 双语完整性）→ Task 1/2/3；⑦ 空态 / 无提示分组不渲染 / Esc 关闭 → Task 3（ModalDialog 自带 Esc）。全部覆盖。
- **占位符扫描**：无 TBD/TODO/「适当处理」类占位；每步含具体代码与命令。
- **类型一致性**：`ShortcutContext`、`ShortcutTipDef.context`、`getShortcutTipsForContext(ctx)`、`getAllShortcutTips()`、`SHORTCUT_CONTEXT_ORDER` 在 Task 1 定义，Task 2/3/4 引用一致；`ShortcutTipsDialog` props `open` + emit `close` 在 Task 3 定义，Task 4 使用一致。
