# 消息展示模式配置（摘要/原文）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在设置面板「聊天」类别下新增「消息展示模式」配置（摘要模式 / 原文模式），作为聊天消息展示的全局默认值，单条消息的手动切换仍可覆盖。

**Architecture:** 纯前端本地配置（localStorage，key `messageDisplayMode`，默认 `'summary'`）。改造 `shouldShowSummary()` 接受全局默认模式参数；原文模式下对「有摘要但内容被剥离」的消息通过新增 `ensure-content` 事件链触发现有 `ensureMessageContent` 懒加载全文，加载期间用摘要占位。

**Tech Stack:** Vue 3 (`<script setup>`), TypeScript, localStorage, vitest + @vue/test-utils。

参考 spec：`docs/superpowers/specs/2026-08-17-message-display-mode-design.md`

---

## 前置知识（实现者必读）

- **`shouldShowSummary`**（`web/src/utils/chatSessionUtils.ts:105`）是唯一渲染决策点。现有逻辑：无摘要→false；blocks 空（内容被 `view=summary` 剥离）→true；否则 `msg.showingSummary !== false`（默认摘要）。
- **`localConfig`**（`web/src/composables/useSettingsConfig.ts:267`）是模块级 `reactive` 单例，组件直接 `import { localConfig }` 即可响应式读取。`localDefaults`（:245）定义默认值。
- **懒加载机制**：`ChatPanelContent.vue` `ensureMessageContent`（:990）按需 `GET /api/rag/message?id=...`，填充 `msg.blocks`/`msg.files`，`_loadingOriginal` 做去重与加载指示（meta bar 的「加载原文中...」）。
- **事件流**：`ChatMessageItem` → `ChatMessageList` → `ChatPanelContent` 逐层转发。`toggle-summary` 现有链路：`ChatMessageItem.vue:38` → `ChatMessageList.vue:73` → `ChatPanelContent.vue:28` → `handleToggleSummary`（:951）。新事件 `ensure-content` 复用此链路。
- **测试命令**（项目根目录执行）：单文件 `npx vitest run web/src/<path>.test.ts`；全量 `npm test`；类型检查 `npm run typecheck`；lint `npm run lint`。
- **单条偏好语义**：`msg.showingSummary` 存用户显式选择（`undefined`=未选择），由 `parseMessages` 在会话内保留。全局配置仅作 `undefined` 时的默认。

---

## 文件结构

| 文件 | 改动 |
|---|---|
| `web/src/composables/useSettingsConfig.ts` | `localDefaults` 增 `messageDisplayMode: 'summary'` |
| `web/src/composables/__tests__/useSettingsConfig.test.ts` | 新增默认值 + 持久化用例 |
| `web/src/i18n/locales/zh.ts` | 新增 3 个 i18n key |
| `web/src/i18n/locales/en.ts` | 新增 3 个 i18n key |
| `web/src/components/settings/settingsFieldMap.ts` | `chat` 类别「消息与历史」区新增 select 项 |
| `web/src/components/settings/__tests__/settingsFieldMap.test.ts` | 新增结构断言用例 |
| `web/src/utils/chatSessionUtils.ts` | `shouldShowSummary` 增 `defaultMode` 参数 |
| `web/src/utils/__tests__/chatSessionUtils.test.ts` | 新增全局默认分支用例 |
| `web/src/components/chat/ChatMessageItem.vue` | 读全局模式、`ensure-content` 触发、加载占位 |
| `web/src/components/chat/ChatMessageList.vue` | 转发 `ensure-content` |
| `web/src/components/chat/ChatPanelContent.vue` | 绑定 `ensure-content`；`handleToggleSummary` 传全局模式 |
| `web/src/components/chat/__tests__/ChatMessageItem.test.ts` | mock `useSettingsConfig` + 懒加载/占位用例 |

---

### Task 1: 本地配置项 `messageDisplayMode`

**Files:**
- Modify: `web/src/composables/useSettingsConfig.ts:245-264`（`localDefaults`）
- Test: `web/src/composables/__tests__/useSettingsConfig.test.ts`

- [ ] **Step 1: 写失败测试**

在 `web/src/composables/__tests__/useSettingsConfig.test.ts` 的 `describe('useSettingsConfig')` 内、`localConfig has default keys` 用例之后新增：

```ts
it('localConfig has messageDisplayMode defaulting to summary', () => {
    const { localConfig } = useSettingsConfig()
    localStorage.removeItem('clawbench-settings-messageDisplayMode')
    expect('messageDisplayMode' in localConfig).toBe(true)
    expect(localConfig.messageDisplayMode).toBe('summary')
})

it('setLocalConfig persists messageDisplayMode to localStorage', () => {
    const { localConfig, setLocalConfig } = useSettingsConfig()

    setLocalConfig('messageDisplayMode', 'original')
    expect(localConfig.messageDisplayMode).toBe('original')
    expect(localStorage.getItem('clawbench-settings-messageDisplayMode')).toBe('"original"')

    localStorage.removeItem('clawbench-settings-messageDisplayMode')
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/composables/__tests__/useSettingsConfig.test.ts -t messageDisplayMode`
Expected: FAIL — `localConfig.messageDisplayMode` 为 `undefined`（键不存在），`'messageDisplayMode' in localConfig` 为 false。

- [ ] **Step 3: 实现**

在 `web/src/composables/useSettingsConfig.ts` 的 `localDefaults`（`:245-264`）对象内增加一行：

```ts
const localDefaults: Record<string, string | boolean | number | null> = {
  theme: 'auto',
  terminalTheme: 'auto',
  locale: 'zh',
  autoSpeech: false,
  showHidden: false,
  wordWrap: true,
  lineNumbers: false,
  stickyScroll: true,
  fileView: 'list',
  messageDisplayMode: 'summary',
  terminalFontSize: 12,
  logCapture: false,
  swipeSession: false,
  preventScreenLock: true,
  sortField: null,
  sortDir: 'asc',
  uiScale: 1,
  recentFilesCount: 10,
  headerShortcutTips: true,
}
```

不注册 `legacyKeys`（无既有键可迁移，无 sideEffect）。

- [ ] **Step 4: 运行测试确认通过**

Run: `npx vitest run web/src/composables/__tests__/useSettingsConfig.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/src/composables/useSettingsConfig.ts web/src/composables/__tests__/useSettingsConfig.test.ts
git commit -m "feat(web): add messageDisplayMode local config default"
```

---

### Task 2: i18n 文案

**Files:**
- Modify: `web/src/i18n/locales/zh.ts:1431-1434` 附近
- Modify: `web/src/i18n/locales/en.ts:1430-1433` 附近

- [ ] **Step 1: 中文文案**

在 `web/src/i18n/locales/zh.ts` 的 `chatPageSizeDesc: '向上滚动加载更多消息时的每页数量',` 之后新增：

```ts
      messageDisplayMode: '消息展示模式',
      messageDisplayModeDesc: '有摘要的消息默认展示方式；单条消息仍可单独切换',
      messageDisplayModeSummary: '摘要模式',
      messageDisplayModeOriginal: '原文模式',
```

- [ ] **Step 2: 英文文案**

在 `web/src/i18n/locales/en.ts` 的 `chatPageSizeDesc: 'Messages loaded per page when scrolling up',` 之后新增：

```ts
      messageDisplayMode: 'Message display mode',
      messageDisplayModeDesc: 'Default view for messages with a summary; individual messages can still be toggled',
      messageDisplayModeSummary: 'Summary mode',
      messageDisplayModeOriginal: 'Original text',
```

- [ ] **Step 3: 确认无其他引用破坏**

Run: `npx vitest run web/src/i18n/__tests__/navKeys.test.ts web/src/config/__tests__/shortcutTips.test.ts`
Expected: PASS（i18n key 校验无回归）

- [ ] **Step 4: 提交**

```bash
git add web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(web): add message display mode i18n labels"
```

---

### Task 3: 设置面板「聊天」类别新增 select 项

**Files:**
- Modify: `web/src/components/settings/settingsFieldMap.ts:125-138`（`chat` 数组）
- Test: `web/src/components/settings/__tests__/settingsFieldMap.test.ts`

- [ ] **Step 1: 写失败测试**

在 `web/src/components/settings/__tests__/settingsFieldMap.test.ts` 的 `describe('settingsFieldMap')` 内、`categoryItems covers all expected categories` 用例之后新增：

```ts
it('chat category has a local messageDisplayMode select with summary/original options', () => {
    const chatEntries = categoryItems['chat']
    const entry = chatEntries.find(e => e.type === 'item' && e.spec.key === 'messageDisplayMode')
    expect(entry).toBeDefined()
    if (entry!.type !== 'item') throw new Error('expected item')
    expect(entry.spec.source).toBe('local')
    expect(entry.spec.type).toBe('select')
    expect(entry.spec.sectionHeader).toBe('settings.items.chatMessageSection')
    const values = (entry.spec.options ?? []).map(o => o.value)
    expect(values).toEqual(['summary', 'original'])
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/components/settings/__tests__/settingsFieldMap.test.ts -t messageDisplayMode`
Expected: FAIL — `entry` 为 `undefined`。

- [ ] **Step 3: 实现**

在 `web/src/components/settings/settingsFieldMap.ts` 的 `chat` 数组（:125-138）中，`swipeSession` 项（:128）之后、`chatInitialMessages` 项（:129）之前插入：

```ts
    { type: 'item', spec: { labelKey: 'settings.items.messageDisplayMode', descriptionKey: 'settings.items.messageDisplayModeDesc', key: 'messageDisplayMode', type: 'select', source: 'local', sectionHeader: 'settings.items.chatMessageSection', options: [
      { labelKey: 'settings.items.messageDisplayModeSummary', value: 'summary' },
      { labelKey: 'settings.items.messageDisplayModeOriginal', value: 'original' },
    ]}},
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npx vitest run web/src/components/settings/__tests__/settingsFieldMap.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add web/src/components/settings/settingsFieldMap.ts web/src/components/settings/__tests__/settingsFieldMap.test.ts
git commit -m "feat(web): add message display mode setting to chat category"
```

---

### Task 4: `shouldShowSummary` 支持全局默认模式

**Files:**
- Modify: `web/src/utils/chatSessionUtils.ts:105-112`
- Test: `web/src/utils/__tests__/chatSessionUtils.test.ts:288-309`

- [ ] **Step 1: 写失败测试**

在 `web/src/utils/__tests__/chatSessionUtils.test.ts` 的 `shouldShowSummary returns false when there is no summary` 用例（:288）之后新增：

```ts
it('shouldShowSummary respects global default mode when user has no explicit preference', () => {
    // default 'summary' preserves current behavior
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] })).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'summary')).toBe(true)
    // global original mode → show full text
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }] }, 'original')).toBe(false)
})

it('shouldShowSummary with original default and stripped content returns false to trigger lazy load', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [] }, 'original')).toBe(false)
    expect(shouldShowSummary({ summary: 'sum', blocks: [], showingSummary: undefined }, 'original')).toBe(false)
})

it('shouldShowSummary with summary default and stripped content returns true', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [] })).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [] }, 'summary')).toBe(true)
})

it('shouldShowSummary keeps explicit preference overriding global original mode', () => {
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: true }, 'original')).toBe(true)
    expect(shouldShowSummary({ summary: 'sum', blocks: [{ type: 'text', text: 'x' }], showingSummary: false }, 'summary')).toBe(false)
})

it('shouldShowSummary keeps forcing summary for stripped content with explicit original preference', () => {
    // Regression preserved: stream interrupted → summary generated async after
    // showingSummary=false; stripped content (blocks empty) must still show summary.
    expect(shouldShowSummary({ summary: 'late sum', blocks: [], showingSummary: false }, 'original')).toBe(true)
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/utils/__tests__/chatSessionUtils.test.ts -t shouldShowSummary`
Expected: FAIL — `shouldShowSummary` 现有实现不接受第二个参数，默认模式分支不生效。

- [ ] **Step 3: 实现**

将 `web/src/utils/chatSessionUtils.ts:105-112` 的 `shouldShowSummary` 整体替换为：

```ts
export type MessageDisplayMode = 'summary' | 'original'

export function shouldShowSummary(
  msg: Record<string, unknown> | { summary?: unknown; blocks?: unknown; showingSummary?: unknown },
  defaultMode: MessageDisplayMode = 'summary',
): boolean {
  const hasSummary = msg.summary != null && msg.summary !== ''
  if (!hasSummary) return false
  const blocksArr = msg.blocks as unknown as Array<unknown> | undefined
  const blocksEmpty = !blocksArr || blocksArr.length === 0
  // Explicit per-message preference wins whenever content is available. When
  // content was stripped (view=summary) the summary is the only thing we can
  // render, so fall back to it regardless of preference (stream-interruption
  // regression, see comment above the function).
  if (msg.showingSummary !== undefined) {
    if (blocksEmpty) return true
    return msg.showingSummary !== false
  }
  // No explicit preference: use the global default. In original mode with
  // stripped content we return false so the component triggers a lazy fetch
  // of the full content.
  if (blocksEmpty) return defaultMode === 'summary'
  return defaultMode === 'summary'
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `npx vitest run web/src/utils/__tests__/chatSessionUtils.test.ts`
Expected: PASS（新旧用例全部通过）

- [ ] **Step 5: 提交**

```bash
git add web/src/utils/chatSessionUtils.ts web/src/utils/__tests__/chatSessionUtils.test.ts
git commit -m "feat(web): shouldShowSummary supports global default display mode"
```

---

### Task 5: 原文模式懒加载 + 加载占位

**Files:**
- Modify: `web/src/components/chat/ChatMessageItem.vue`
- Modify: `web/src/components/chat/ChatMessageList.vue:55-78, 171`
- Modify: `web/src/components/chat/ChatPanelContent.vue:28, 951-967, 185`
- Test: `web/src/components/chat/__tests__/ChatMessageItem.test.ts`

- [ ] **Step 1: 写失败测试**

在 `web/src/components/chat/__tests__/ChatMessageItem.test.ts` 顶部（`vi.mock` 区，`vi.mock('@/stores/app', ...)` 之后）新增 mock，替换 SummaryToggle stub 使其暴露 `showingSummary` prop：

```ts
vi.mock('@/composables/useSettingsConfig', () => ({
    localConfig: { messageDisplayMode: 'summary' },
}))
```

并将现有 SummaryToggle mock（:87-89）替换为：

```ts
vi.mock('@/components/common/SummaryToggle.vue', () => ({
    default: { name: 'SummaryToggle', props: ['showingSummary'], template: '<span class="summary-toggle-stub" />' },
}))
```

然后在 `describe('ChatMessageItem')` 的 `summary toggle scroll anchoring` describe 之后新增：

```ts
describe('global message display mode (original lazy-load)', () => {
    beforeEach(async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'summary'
    })

    it('emits ensure-content for a stripped message when global mode is original', async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'original'
        const wrapper = createWrapper({
            msg: { id: 'ec1', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false },
        })
        await wrapper.vm.$nextTick()
        expect(wrapper.emitted('ensure-content')).toBeTruthy()
        expect(wrapper.emitted('ensure-content')![0]).toEqual([expect.objectContaining({ id: 'ec1' })])
    })

    it('does not emit ensure-content in summary mode', async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'summary'
        const wrapper = createWrapper({
            msg: { id: 'no-ec', role: 'assistant', content: '', blocks: [], summary: 'Summary', streaming: false },
        })
        await wrapper.vm.$nextTick()
        expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('does not emit ensure-content when blocks are already present', async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'original'
        const wrapper = createWrapper({
            msg: { id: 'has-blocks', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Full' }], summary: 'Summary', streaming: false },
        })
        await wrapper.vm.$nextTick()
        expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('does not emit ensure-content when an explicit preference exists', async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'original'
        const wrapper = createWrapper({
            msg: { id: 'pref', role: 'assistant', content: '', blocks: [], summary: 'Summary', showingSummary: true, streaming: false },
        })
        await wrapper.vm.$nextTick()
        expect(wrapper.emitted('ensure-content')).toBeUndefined()
    })

    it('keeps summary as placeholder while lazy-loading and releases it once content arrives', async () => {
        const cfg = await import('@/composables/useSettingsConfig')
        ;(cfg.localConfig as any).messageDisplayMode = 'original'
        const wrapper = createWrapper({
            msg: { id: 'pl1', role: 'assistant', content: '', blocks: [], summary: 'Summary', _loadingOriginal: true, streaming: false },
        })
        let toggle = wrapper.findComponent({ name: 'SummaryToggle' })
        expect(toggle.props('showingSummary')).toBe(true)
        await wrapper.setProps({
            msg: { id: 'pl1', role: 'assistant', content: '', blocks: [{ type: 'text', text: 'Full' }], summary: 'Summary', _loadingOriginal: false, streaming: false },
        })
        toggle = wrapper.findComponent({ name: 'SummaryToggle' })
        expect(toggle.props('showingSummary')).toBe(false)
    })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `npx vitest run web/src/components/chat/__tests__/ChatMessageItem.test.ts -t "global message display mode"`
Expected: FAIL — 组件尚未 emit `ensure-content`，`showingSummary` 逻辑未变。

- [ ] **Step 3: 实现 ChatMessageItem.vue**

3a. 顶部 import（`<script setup>` 内，`import { ref, inject, computed, nextTick } from 'vue'` 改为同时引入 `watch`；`import { shouldShowSummary } from '@/utils/chatSessionUtils.ts'` 后新增 localConfig import）：

```ts
import { ref, inject, computed, nextTick, watch } from 'vue'
import { localConfig } from '@/composables/useSettingsConfig'
```

3b. `defineEmits`（:163）增加 `'ensure-content'`：

```ts
const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'remove-pending', 'fork-from-message'])
```

3c. `msgText` computed（:209-215）改为传全局模式：

```ts
const displayMode = computed<MessageDisplayMode>(() =>
    (localConfig.messageDisplayMode as MessageDisplayMode) || 'summary'
)

const msgText = computed(() => {
  if (props.msg?.role !== 'assistant') return ''
  const text = extractSpeakableText(props.msg?.blocks || [])
  if (text) return text
  if (shouldShowSummary(props.msg, displayMode.value) && props.msg?.summary) return props.msg.summary
  return ''
})
```

3d. `showSummary` computed（:220）与懒加载触发逻辑替换为：

```ts
// Whether to render the summary view. Computed from message state (summary
// exists, content stripped), the user's explicit preference, and the global
// default display mode. While the full text is being lazily fetched in
// original mode, keep showing the summary as a placeholder so the message
// bubble is never blank.
const showSummary = computed(() => {
  if (!props.msg) return false
  if (props.msg._loadingOriginal) return true
  return shouldShowSummary(props.msg, displayMode.value)
})

// In global original mode, a summarized message whose content was stripped by
// view=summary has nothing to render in original view — request the full text
// once. Guarded by _loadingOriginal and blocks-present to fire exactly once.
const needsLazyOriginal = computed(() =>
  displayMode.value === 'original' &&
  props.msg?.summary != null && props.msg.summary !== '' &&
  (!props.msg.blocks || props.msg.blocks.length === 0) &&
  props.msg.showingSummary === undefined &&
  props.msg._loadingOriginal !== true
)

watch(needsLazyOriginal, (needs) => {
  if (needs && props.msg) emit('ensure-content', props.msg)
}, { immediate: true })
```

`MessageDisplayMode` 从 `@/utils/chatSessionUtils` import：

```ts
import { shouldShowSummary, type MessageDisplayMode } from '@/utils/chatSessionUtils.ts'
```

- [ ] **Step 4: 实现 ChatMessageList.vue 转发**

在 `web/src/components/chat/ChatMessageList.vue`：
- `ChatMessageItem` 标签上 `@toggle-summary="$emit('toggle-summary', $event)"`（:73）之后新增：

```html
      @ensure-content="$emit('ensure-content', $event)"
```

- `defineEmits`（:171）数组增加 `'ensure-content'`：

```ts
const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'file-open', 'load-more', 'task-card-click', 'send-message', 'remove-pending', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'fork-from-message'])
```

- [ ] **Step 5: 实现 ChatPanelContent.vue 绑定 + 修复 handleToggleSummary**

5a. `@toggle-summary="handleToggleSummary"`（:28）之后新增绑定：

```html
      @ensure-content="(msg) => ensureMessageContent(msg)"
```

5b. import 区（:185 附近 `import { applySummaryUpdate, shouldShowSummary } from '@/utils/chatSessionUtils.ts'`）后新增：

```ts
import { localConfig } from '@/composables/useSettingsConfig'
```

5c. `handleToggleSummary`（:951-967）改用全局模式计算当前态（否则原文模式下点击「摘要」会因 `shouldShowSummary` 默认摘要导致不切换）：

```ts
async function handleToggleSummary(msgId) {
    const msg = messages.value.find(m => m.id === msgId)
    if (!msg) return
    // Historical messages with no summary: generate one on demand first.
    if (msg.summary == null || msg.summary === '') {
        await generateMessageSummary(msg)
        return
    }
    const mode = (localConfig.messageDisplayMode === 'original' ? 'original' : 'summary')
    const showingNow = shouldShowSummary(msg, mode)
    // Switching FROM summary TO original: if blocks weren't loaded (content omitted in view=summary), fetch the full message.
    if (showingNow && (!msg.blocks || msg.blocks.length === 0)) {
        await ensureMessageContent(msg)
    }
    // Record the user's explicit preference. If they were showing the summary,
    // toggle to original; otherwise toggle to summary.
    msg.showingSummary = !showingNow
}
```

- [ ] **Step 6: 运行测试确认通过**

Run: `npx vitest run web/src/components/chat/__tests__/ChatMessageItem.test.ts`
Expected: PASS（新旧用例全部通过）

- [ ] **Step 7: 类型检查 + lint**

Run: `npm run typecheck`
Expected: PASS（无类型错误）

Run: `npm run lint`
Expected: PASS（无 lint 错误）

- [ ] **Step 8: 提交**

```bash
git add web/src/components/chat/ChatMessageItem.vue web/src/components/chat/ChatMessageList.vue web/src/components/chat/ChatPanelContent.vue web/src/components/chat/__tests__/ChatMessageItem.test.ts
git commit -m "feat(web): lazy-load full text in original display mode"
```

---

### Task 6: 全量验证

- [ ] **Step 1: 全量前端测试**

Run: `npm test`
Expected: 全部 PASS，无超时（`vitest-run.sh` 自带 watchdog）。

- [ ] **Step 2: 类型检查 + lint 复验**

Run: `npm run typecheck && npm run lint`
Expected: PASS

- [ ] **Step 3: 关联 Go 测试冒烟（确认无后端改动被意外触碰）**

Run: `go test ./internal/... 2>&1 | tail -20`
Expected: 与改动前一致（无回归）。若本地 Go 环境缺失可跳过并说明。

---

## 自审记录

- **Spec 覆盖**：配置项（Task 1）✓、设置面板项（Task 3）✓、i18n（Task 2）✓、`shouldShowSummary`（Task 4）✓、懒加载+占位（Task 5）✓、数据流/错误处理/边界（Task 5 触发条件与占位、Task 4 回归用例）✓、测试（Task 1/3/4/5）✓。
- **占位符扫描**：全部步骤含可执行代码与精确命令，无 TBD/TODO。
- **类型一致性**：`MessageDisplayMode` 在 Task 4 定义、Task 5 复用；`shouldShowSummary(msg, defaultMode)` 签名在 Task 4 变更、Task 5 三处调用全部传入 `displayMode.value`；事件名 `ensure-content` 在 Task 5 三层一致。