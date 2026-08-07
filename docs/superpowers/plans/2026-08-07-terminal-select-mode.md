# 终端三态交互（浏览 / 手势 / 选区）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在移动端终端内实现自由划选复制，用「浏览 / 手势 / 选区」三态手势按钮取代「复制输出」按钮 + `OutputDrawer` 抽屉。

**Architecture:** 把 `useTerminalGestures` 的 `enabled: boolean` 改为 `mode: 'browse'|'gesture'|'selection'`（默认 browse），`applyState()` 按 mode 挂不同 touch 监听。选区态新增一组 selection 监听，把触摸坐标换算成视口行号，通过 `GestureCallbacks` 回调给 `TerminalPanelContent`，由其调 `term.selectLines()` 渲染选区；划选后出现悬浮复制条，复用 `copyText` 复制。

**Tech Stack:** Vue 3 (`<script setup>`), xterm.js (`@xterm/xterm`), Vitest, TypeScript, vue-i18n。

---

## 文件结构

- Modify: `web/src/composables/useTerminalGestures.ts` — 三态重构 + selection 监听 + 纯函数 `clientYToViewportRow`
- Modify: `web/src/components/terminal/TerminalPanelContent.vue` — 按钮循环、选区接线、悬浮复制条、移除抽屉
- Modify: `web/src/composables/useTerminalTabs.ts` — 新增 `onTermCreated` 选项（订阅 onSelectionChange）
- Modify: `web/src/i18n/locales/zh.ts`、`en.ts` — 新增三态/复制标签
- Rewrite: `web/src/components/__tests__/terminalGestures.test.ts` — 迁移到 mode API + 新增 selection 测试
- Delete: `web/src/components/terminal/OutputDrawer.vue`

---

### Task 1: `useTerminalGestures` 三态重构（mode + cycleMode + setMode + applyState）

**Files:**
- Modify: `web/src/composables/useTerminalGestures.ts`

- [ ] **Step 1: 写失败测试（TerminalMode 类型 + 三态循环）**

先加测试（放在 `terminalGestures.test.ts` 的 `describe('useTerminalGestures')` 块内）：

```ts
import { TerminalMode } from '@/composables/useTerminalGestures'

it('cycles through browse → gesture → selection → browse by default', () => {
  const { gestures } = setupGestures()
  expect(gestures.mode.value).toBe('browse')

  gestures.cycleMode()
  expect(gestures.mode.value).toBe('gesture')

  gestures.cycleMode()
  expect(gestures.mode.value).toBe('selection')

  gestures.cycleMode()
  expect(gestures.mode.value).toBe('browse')
})

it('setMode switches to the requested mode and no-ops when identical', () => {
  const { gestures } = setupGestures()
  gestures.setMode('selection')
  expect(gestures.mode.value).toBe('selection')
  gestures.setMode('selection') // no-op
  expect(gestures.mode.value).toBe('selection')
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npx vitest run src/components/__tests__/terminalGestures.test.ts`
Expected: 编译失败，`TerminalMode` / `cycleMode` / `setMode` / `mode` 不存在。

- [ ] **Step 3: 实现三态核心**

在 `web/src/composables/useTerminalGestures.ts` 顶部（第 1 行 import 之后）新增类型与模式常量：

```ts
export type TerminalMode = 'browse' | 'gesture' | 'selection'
const MODE_ORDER: TerminalMode[] = ['browse', 'gesture', 'selection']

/** 把触摸绝对 Y 坐标换算成视口行号（0..viewportRows-1），越界时 clamp。 */
export function clientYToViewportRow(
  clientY: number,
  containerTop: number,
  cellHeight: number,
  viewportRows: number,
): number {
  if (cellHeight <= 0 || viewportRows <= 0) return 0
  const row = Math.floor((clientY - containerTop) / cellHeight)
  return Math.max(0, Math.min(viewportRows - 1, row))
}
```

把现有 `const enabled = ref(true)`（第 52 行）替换为：

```ts
const mode = ref<TerminalMode>('browse')
```

新增 selection 状态变量（放在 `let currentDirection` 附近）：

```ts
let selectionListenersAttached = false
let selectionActive = false
let selectionAnchorRow = -1
```

`applyState()`（第 438-449 行）整体替换为：

```ts
  function applyState() {
    const el = elementRef.value
    detachListeners()
    detachDisabledScrollListeners()
    detachSelectionListeners()
    if (mode.value === 'gesture') {
      attachListeners()
      if (el) el.style.touchAction = 'manipulation'
    } else if (mode.value === 'browse') {
      attachDisabledScrollListeners()
      if (el) el.style.touchAction = 'auto'
    } else {
      attachSelectionListeners()
      if (el) el.style.touchAction = 'none'
    }
  }

  function setMode(m: TerminalMode) {
    if (m === mode.value) return
    mode.value = m
    resetGestureState()
    resetDisabledScrollState()
    lastTapTime = 0
    selectionActive = false
    selectionAnchorRow = -1
    applyState()
  }

  function cycleMode() {
    const idx = MODE_ORDER.indexOf(mode.value)
    setMode(MODE_ORDER[(idx + 1) % MODE_ORDER.length])
  }
```

`attach()`（第 464-468 行）改为只调 `applyState()`（避免重复 detach 逻辑）：

```ts
  function attach() {
    detachListeners()
    detachDisabledScrollListeners()
    detachSelectionListeners()
    applyState()
  }
```

`detach()`（第 471-476 行）补上 `detachSelectionListeners()`：

```ts
  function detach() {
    detachListeners()
    detachDisabledScrollListeners()
    detachSelectionListeners()
    const el = elementRef.value
    if (el) el.style.touchAction = ''
  }
```

删除原 `toggle()`（第 451-459 行）。

return（第 478-483 行）替换为：

```ts
  return {
    attach,
    detach,
    mode,
    setMode,
    cycleMode,
  }
```

> 注意：此步引用了尚未实现的 `attachSelectionListeners` / `detachSelectionListeners`，需在 Task 2 补齐后才能编译通过。可先在此步用临时空函数占位，或把 Task 1、2 一起实现后统一编译。建议：本步实现核心，Task 2 补齐 selection 监听后一并运行测试。

- [ ] **Step 4: 同步更新 `shouldPreventTerminalContextMenu` 调用约定**

`shouldPreventTerminalContextMenu(enabled: boolean)` 签名保持不变，但调用方改为传 `mode !== 'browse'`（见 Task 4）。本文件无需改。

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useTerminalGestures.ts
git commit -m "feat(terminal): three-mode gesture state (browse/gesture/selection)"
```

---

### Task 2: `useTerminalGestures` selection 监听 + 回调

**Files:**
- Modify: `web/src/composables/useTerminalGestures.ts`

- [ ] **Step 1: 扩展 `GestureCallbacks` 接口**

在 `export interface GestureCallbacks`（第 3-14 行）中新增：

```ts
  /** 选区模式：读取当前终端的 cell 高度（px），用于坐标→行号换算。 */
  getCellHeight?: () => number
  /** 选区模式：手指按下，报告锚点视口行号。 */
  onSelectionStart?: (row: number) => void
  /** 选区模式：拖动中，报告 [锚点行, 当前行]（均为视口行号）。 */
  onSelectionExtend?: (anchorRow: number, currentRow: number) => void
  /** 选区模式：手指抬起。 */
  onSelectionEnd?: () => void
```

- [ ] **Step 2: 写失败测试（selection 手势 → 回调行号）**

在 `terminalGestures.test.ts` 新增独立 `describe('selection mode')` 块：

```ts
describe('selection mode', () => {
  function setupSelection() {
    const el = document.createElement('div')
    // 模拟 600px 高、cell 高度 20px → 30 个视口行
    Object.defineProperty(el, 'getBoundingClientRect', {
      value: () => ({ top: 100, bottom: 700, height: 600, left: 0, width: 0, right: 0, x: 0, y: 100, toJSON: () => ({}) }),
    })
    document.body.appendChild(el)

    const starts: number[] = []
    const extends_: Array<[number, number]> = []
    let ends = 0
    const gestures = useTerminalGestures(ref(el), {
      getCellHeight: () => 20,
      onSelectionStart: (row) => starts.push(row),
      onSelectionExtend: (a, c) => extends_.push([a, c]),
      onSelectionEnd: () => ends++,
    })
    gestures.setMode('selection')
    gestures.attach()
    activeGestures = gestures

    return { el, starts, extends_, ends, gestures }
  }

  it('maps a vertical drag into viewport row selection callbacks', () => {
    const { el, starts, extends_, ends } = setupSelection()

    // clientY 120 → (120-100)/20 = 行 1
    dispatchTouch(el, 'touchstart', [makeTouch(50, 120)])
    expect(starts).toEqual([1])

    // clientY 240 → 行 7
    dispatchTouch(el, 'touchmove', [makeTouch(50, 240)])
    expect(extends_).toEqual([[1, 7]])

    dispatchTouch(el, 'touchend', [], [makeTouch(50, 240)])
    expect(ends).toBe(1)
  })

  it('clamps rows into the viewport bounds', () => {
    const { el, starts, extends_ } = setupSelection()

    dispatchTouch(el, 'touchstart', [makeTouch(50, 120)]) // 行 1
    dispatchTouch(el, 'touchmove', [makeTouch(50, 800)]) // 行 35 → clamp 到 29
    expect(extends_).toEqual([[1, 29]])
  })

  it('sets touchAction none and attaches selection listeners in selection mode', () => {
    const { el, gestures } = setupSelection()
    expect(gestures.mode.value).toBe('selection')
    expect(el.style.touchAction).toBe('none')
  })

  it('switching away from selection clears the selection anchor', () => {
    const { el, extends_, gestures } = setupSelection()
    gestures.setMode('browse')
    // browse 模式下单指拖动走 scroll，不发 selection 回调
    dispatchTouch(el, 'touchstart', [makeTouch(80, 100)])
    dispatchTouch(el, 'touchmove', [makeTouch(84, 140)])
    expect(extends_).toEqual([])
  })
})
```

- [ ] **Step 3: 运行确认失败**

Run: `npx vitest run src/components/__tests__/terminalGestures.test.ts`
Expected: selection 测试失败（无 `getCellHeight` 处理 / 无 selection 监听）。

- [ ] **Step 4: 实现 selection 监听器**

在 `useTerminalGestures.ts` 中（`onDisabledTouchCancel` 之后）新增：

```ts
  function onSelectionTouchStart(e: TouchEvent) {
    if (e.touches.length !== 1) return
    preventNativeTouch(e)
    const el = elementRef.value
    if (!el) return
    const touch = e.touches[0]
    const rect = el.getBoundingClientRect()
    const cellH = callbacks.getCellHeight?.() ?? 0
    const viewportRows = cellH > 0 ? Math.max(1, Math.floor(rect.height / cellH)) : 1
    selectionAnchorRow = clientYToViewportRow(touch.clientY, rect.top, cellH, viewportRows)
    selectionActive = true
    callbacks.onSelectionStart?.(selectionAnchorRow)
  }

  function onSelectionTouchMove(e: TouchEvent) {
    if (e.touches.length !== 1 || !selectionActive) return
    preventNativeTouch(e)
    const el = elementRef.value
    if (!el) return
    const touch = e.touches[0]
    const rect = el.getBoundingClientRect()
    const cellH = callbacks.getCellHeight?.() ?? 0
    const viewportRows = cellH > 0 ? Math.max(1, Math.floor(rect.height / cellH)) : 1
    const current = clientYToViewportRow(touch.clientY, rect.top, cellH, viewportRows)
    callbacks.onSelectionExtend?.(selectionAnchorRow, current)
  }

  function onSelectionTouchEnd() {
    selectionActive = false
    selectionAnchorRow = -1
    callbacks.onSelectionEnd?.()
  }

  function onSelectionTouchCancel() {
    selectionActive = false
    selectionAnchorRow = -1
  }

  function attachSelectionListeners() {
    if (selectionListenersAttached) return
    const el = elementRef.value
    if (!el) return
    el.addEventListener('touchstart', onSelectionTouchStart, { passive: false })
    el.addEventListener('touchmove', onSelectionTouchMove, { passive: false })
    el.addEventListener('touchend', onSelectionTouchEnd, { passive: false })
    el.addEventListener('touchcancel', onSelectionTouchCancel, { passive: false })
    selectionListenersAttached = true
  }

  function detachSelectionListeners() {
    if (!selectionListenersAttached) return
    const el = elementRef.value
    if (!el) return
    el.removeEventListener('touchstart', onSelectionTouchStart)
    el.removeEventListener('touchmove', onSelectionTouchMove)
    el.removeEventListener('touchend', onSelectionTouchEnd)
    el.removeEventListener('touchcancel', onSelectionTouchCancel)
    selectionListenersAttached = false
  }
```

> 注意：`onSelectionTouchEnd` 不带参数，但 `removeEventListener('touchend', onSelectionTouchEnd)` 需要匹配 add 时的引用——二者是同一函数引用，OK。若 TS 报事件处理器签名不匹配，可保持 `(e: TouchEvent) => void` 签名并在体内忽略 `e`：

```ts
  function onSelectionTouchEnd(_e: TouchEvent) {
    selectionActive = false
    selectionAnchorRow = -1
    callbacks.onSelectionEnd?.()
  }
```

- [ ] **Step 5: 运行全部 gesture 测试**

Run: `npx vitest run src/components/__tests__/terminalGestures.test.ts`
Expected: selection 新测试全绿。

- [ ] **Step 6: Commit**

```bash
git add web/src/composables/useTerminalGestures.ts
git commit -m "feat(terminal): add selection-mode touch listeners"
```

---

### Task 3: 迁移并完善 `terminalGestures.test.ts` 到 mode API

**Files:**
- Rewrite: `web/src/components/__tests__/terminalGestures.test.ts`

现有测试依赖 `gestures.toggle()` 与 `gestures.enabled.value`。Task 1 已删除它们，需迁移。机械替换映射如下（逐条替换，无其他改动）：

| 旧写法 | 新写法 |
|--------|--------|
| `gestures.enabled.value` | `gestures.mode.value === 'gesture'` |
| `gestures.toggle()`（进入 disabled/scroll 态） | `gestures.setMode('browse')` |
| `gestures.toggle()`（返回 enabled 态） | `gestures.setMode('gesture')` |
| setupGestures 中 `gestures.attach()` 之前/之后 | 加 `gestures.setMode('gesture')`，使默认手势行为测试保持生效 |

- [ ] **Step 1: 修改 `setupGestures` 默认进入 gesture 态**

`setupGestures()`（第 45-69 行）在 `gestures.attach()` 之后加一行：

```ts
  gestures.attach()
  gestures.setMode('gesture') // gesture 相关测试默认从手势态开始
  activeGestures = gestures
```

- [ ] **Step 2: 逐条替换 `enabled` / `toggle` 引用**

按上表替换下列位置（这些是现有测试，需保留原断言意图）：
- 第 181、198、210、219、377、387、400、417、425 行的 `gestures.toggle()` → `gestures.setMode('browse')`
- 第 221 行 `gestures.toggle()` → `gestures.setMode('gesture')`
- 第 187、212、220、223 行 `gestures.enabled.value` → `gestures.mode.value === 'gesture'`
- 第 187、212 行的反向断言相应取反（`toBe(false)` → `toBe(false)` 语义一致，因 `browse` 模式下该表达式为 false，无需改值）

> 第 187 行 `expect(gestures.enabled.value).toBe(false)` → `expect(gestures.mode.value === 'gesture').toBe(false)`。
> 第 212 行同理。
> 第 220 行 `toBe(false)`、第 223 行 `toBe(true)` 同理映射。

- [ ] **Step 3: 补充 default-mode 断言**

新增一条测试确认默认态为 browse：

```ts
it('defaults to browse mode', () => {
  const { gestures } = setupGestures()
  expect(gestures.mode.value).toBe('browse')
})
```

> 注意：因 `setupGestures` 现在会 `setMode('gesture')`，这条要新建一个不 setMode 的 helper 或在断言前不调用 setup 内的 setMode。更稳妥做法：把 `setMode('gesture')` 改为 `setupGestures(initialMode = 'gesture')` 参数化，默认仍 gesture，仅此条测试传 `'browse'`。

将 `setupGestures` 改为：

```ts
function setupGestures(initialMode: TerminalMode = 'gesture') {
  const el = document.createElement('div')
  document.body.appendChild(el)
  // ... 原有 sent/hints/zoomDeltas/scrollDeltas 定义 ...
  const gestures = useTerminalGestures(ref(el), { /* 原回调 */ })
  gestures.attach()
  if (initialMode !== 'browse') gestures.setMode(initialMode)
  activeGestures = gestures
  return { el, sent, hints, zoomDeltas, scrollDeltas, gestures }
}
```

新增测试：

```ts
it('defaults to browse mode', () => {
  const { gestures } = setupGestures('browse')
  expect(gestures.mode.value).toBe('browse')
})
```

- [ ] **Step 4: 运行测试**

Run: `npx vitest run src/components/__tests__/terminalGestures.test.ts`
Expected: 全部通过（含 Task 2 的 selection 块）。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/__tests__/terminalGestures.test.ts
git commit -m "test(terminal): migrate gesture tests to three-mode API"
```

---

### Task 4: `TerminalPanelContent` 按钮三态循环 + 视觉 + i18n

**Files:**
- Modify: `web/src/components/terminal/TerminalPanelContent.vue`
- Modify: `web/src/i18n/locales/zh.ts`
- Modify: `web/src/i18n/locales/en.ts`

- [ ] **Step 1: 加 i18n 键（zh）**

在 `zh.ts` 的 terminal 段（第 1597-1601 行附近）替换：

```ts
    gestures: '手势',
    gesturesOn: '手势已开启',
    gesturesOff: '手势已关闭',
    copyOutput: '复制输出',
    noOutput: '没有可复制的输出',
```

为：

```ts
    modes: '终端模式',
    modeBrowse: '浏览模式',
    modeGesture: '手势模式',
    modeSelection: '选区模式',
    selectedChars: '已选 {n} 字符',
    copied: '已复制',
```

- [ ] **Step 2: 加 i18n 键（en）**

在 `en.ts` terminal 段（对应位置）替换：

```ts
    gestures: 'Gestures',
    gesturesOn: 'Gestures enabled',
    gesturesOff: 'Gestures disabled',
    copyOutput: 'Copy Output',
    noOutput: 'No output to copy',
```

为：

```ts
    modes: 'Terminal mode',
    modeBrowse: 'Browse mode',
    modeGesture: 'Gesture mode',
    modeSelection: 'Selection mode',
    selectedChars: '{n} chars selected',
    copied: 'Copied',
```

- [ ] **Step 3: 改手势按钮模板**

`TerminalPanelContent.vue` 第 91 行替换为：

```html
        <button class="toolbar-btn modifier gesture-toggle" :class="{ active: gestures.mode.value === 'gesture', 'mode-selection': gestures.mode.value === 'selection' }" @click="handleModeCycle" @contextmenu.prevent :title="t('terminal.modes')">
          <HandIcon v-if="gestures.mode.value !== 'selection'" :size="14" />
          <TextCursorInputIcon v-else :size="14" />
        </button>
```

- [ ] **Step 4: 导入 TextCursorInputIcon**

第 204 行 import 中新增 `TextCursorInput`（`Copy as CopyIcon` 先保留，Task 6 移除按钮时一并删除）：

```ts
import { Zap as ZapIcon, Hand as HandIcon, Hash as HashIcon, Plus as PlusIcon, MoreVertical as MoreVerticalIcon, SquareTerminal as TerminalIcon, Settings, Copy as CopyIcon, TextCursorInput as TextCursorInputIcon } from 'lucide-vue-next'
```

- [ ] **Step 5: `visibleKeys` 判定改为 mode**

第 305-308 行替换：

```ts
const visibleKeys = computed(() => {
  if (gestures.mode.value !== 'gesture') return selectedKeys.value
  return selectedKeys.value.filter(def => !GESTURE_HIDDEN_KEYS.has(def.id))
})
```

- [ ] **Step 6: `handleGestureToggle` → `handleModeCycle`**

第 324-328 行替换：

```ts
function handleModeCycle() {
  gestures.cycleMode()
  const m = gestures.mode.value
  const label = m === 'browse' ? t('terminal.modeBrowse') : m === 'gesture' ? t('terminal.modeGesture') : t('terminal.modeSelection')
  toast.show(label, { icon: m === 'selection' ? '✂️' : '✋', type: 'info', duration: 1200 })
  focusTerminal()
}
```

- [ ] **Step 7: mode watch + contextMenu**

第 457 行 `watch(() => gestures.enabled.value, ...)` 改为：

```ts
watch(() => gestures.mode.value, () => nextTick(refreshToolbarFade))
```

第 570 行 `shouldPreventTerminalContextMenu(gestures.enabled.value)` 改为：

```ts
    if (shouldPreventTerminalContextMenu(gestures.mode.value !== 'browse')) {
```

- [ ] **Step 8: 加 mode-selection 边框样式**

在 `<style scoped>` 中 `.gesture-toggle` 相关样式附近新增：

```css
.gesture-toggle.mode-selection {
  outline: 2px solid var(--accent-color);
  outline-offset: -2px;
  border-radius: 6px;
}
```

- [ ] **Step 9: 运行 typecheck + 相关测试**

Run: `npx vue-tsc --noEmit -p web/tsconfig.json`（或项目 typecheck 命令，见 `package.json`）
Run: `npx vitest run src/components/__tests__/terminalPanelSelection.test.ts src/components/__tests__/terminalGestures.test.ts`
Expected: 通过（若报 `TextCursorInputIcon` 未用、`handleGestureToggle` 残留引用，检查第 91 行与第 324 行已同步）。

- [ ] **Step 10: Commit**

```bash
git add web/src/components/terminal/TerminalPanelContent.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat(terminal): three-mode gesture toggle button with selection visuals"
```

---

### Task 5: 选区接线 + 悬浮复制条

**Files:**
- Modify: `web/src/composables/useTerminalTabs.ts`
- Modify: `web/src/components/terminal/TerminalPanelContent.vue`

- [ ] **Step 1: `useTerminalTabs` 新增 `onTermCreated` 选项**

在 `useTerminalTabs` 的 `opts` 类型（第 37 行 `getXtermTheme` 之后）中新增字段：

```ts
    getXtermTheme: () => Record<string, unknown>
    /** xterm 实例创建后回调（用于订阅选区变化等）。 */
    onTermCreated?: (term: TerminalType) => void
```

在 `createTab` 中，`reactive` 构建 tab 之后、`session.setCallbacks` 之前调用：

```ts
    opts.onTermCreated?.(term)
```

- [ ] **Step 2: 写失败测试（onTermCreated 被调用）**

在 `web/src/composables/__tests__/useTerminalTabs.test.ts` 中，把 `createTabManager`（第 104-121 行）的 `overrides` 类型扩展，并在 opts 透传 `onTermCreated`：

```ts
function createTabManager(overrides?: {
  onCloseSessionViaHttp?: (sessionId: string) => void
  onExit?: (tabId: string) => void
  onError?: (tabId: string, message: string, code: string) => void
  onTermCreated?: (term: unknown) => void
}) {
  return useTerminalTabs(
    (cwd?: string) => `ws://localhost:8080/api/terminal/ws${cwd ? `?cwd=${cwd}` : ''}`,
    {
      fontSize: ref(14),
      getXtermTheme: () => ({}),
      errorMessages: defaultErrorMessages,
      onCloseSessionViaHttp: overrides?.onCloseSessionViaHttp,
      onExit: overrides?.onExit,
      onError: overrides?.onError,
      onTermCreated: overrides?.onTermCreated,
      toast: vi.fn(),
    },
  )
}
```

新增测试（放在该文件的 `describe` 内，参考现有 `createTab` 测试写法）：

```ts
it('calls onTermCreated with the xterm instance when a tab is created', () => {
  const created: unknown[] = []
  const mgr = createTabManager({ onTermCreated: (term) => created.push(term) })
  mgr.createTab()
  expect(created).toHaveLength(1)
})
```

- [ ] **Step 3: 运行确认失败**

Run: `npx vitest run src/composables/__tests__/useTerminalTabs.test.ts`
Expected: 失败（无 `onTermCreated`）。

- [ ] **Step 4: `TerminalPanelContent` 传入 onTermCreated + selection 接线**

在 `useTerminalTabs` 调用（第 383-401 行）的 opts 中新增 `onTermCreated`，并在其上方定义 selection 状态与回调。在 `tabManager` 定义之前新增：

```ts
const selectionActive = ref(false)
const selectedText = ref('')

function updateSelectionFromTerm(term: TerminalType) {
  const text = term.getSelection() ?? ''
  selectionActive.value = text.length > 0
  selectedText.value = text
}

function handleSelectionStart() {
  /* anchor 由 onSelectionExtend 行号覆盖，无需额外处理 */
}

function handleSelectionExtend(anchorRow: number, currentRow: number) {
  const term = activeTab.value?.xterm
  if (!term) return
  const buffer = term.buffer.active
  const ydisp = buffer.ydisp
  const startLine = Math.max(0, ydisp + Math.min(anchorRow, currentRow))
  const endLine = Math.min(buffer.length - 1, ydisp + Math.max(anchorRow, currentRow))
  term.selectLines(startLine, endLine)
  updateSelectionFromTerm(term)
}
```

在 `useTerminalTabs` opts 中新增：

```ts
  onTermCreated: (term) => {
    term.onSelectionChange(() => updateSelectionFromTerm(term))
  },
```

在 gestures 调用（第 436-454 行）的 callbacks 中新增：

```ts
    getCellHeight: () => activeTab.value?.xterm?.dimensions.css.cell.height ?? 0,
    onSelectionStart: handleSelectionStart,
    onSelectionExtend: handleSelectionExtend,
    onSelectionEnd: () => {},
```

> 需要 import `Terminal` 类型：`import type { Terminal as TerminalType } from '@xterm/xterm'`（若未导入）。

- [ ] **Step 5: 离开选区态 / 切 tab 时清除选区**

在现有 `watch(() => gestures.mode.value, ...)`（第 457 行）基础上追加清除逻辑，或新增 watch：

```ts
watch(() => gestures.mode.value, (m) => {
  nextTick(refreshToolbarFade)
  if (m !== 'selection') {
    activeTab.value?.xterm?.clearSelection()
    selectionActive.value = false
    selectedText.value = ''
  }
})
```

在 `watch(activeTabId, ...)`（第 461-463 行）中追加：

```ts
watch(activeTabId, () => {
  activeTab.value?.xterm?.clearSelection()
  selectionActive.value = false
  selectedText.value = ''
  nextTick(() => nextTick(() => gestures.attach()))
})
```

- [ ] **Step 6: 悬浮复制条模板**

在终端视口 div 结束（第 74 行 `</div>`）之后、`<!-- Virtual key toolbar -->`（第 76 行）之前插入：

```html
    <Transition name="copy-bar">
      <div v-if="selectionActive" class="selection-copy-bar">
        <span class="selection-copy-count">{{ t('terminal.selectedChars', { n: selectedText.length }) }}</span>
        <button class="selection-copy-btn" @click="handleCopySelection" @contextmenu.prevent>{{ t('common.copy') }}</button>
      </div>
    </Transition>
```

- [ ] **Step 7: `handleCopySelection`**

新增函数（放在 `handleModeCycle` 之后）：

```ts
function handleCopySelection() {
  const text = selectedText.value
  if (!text) return
  copyText(text, () => {
    toast.show(t('terminal.copied'), { icon: '✅', type: 'success' })
    activeTab.value?.xterm?.clearSelection()
    selectionActive.value = false
    selectedText.value = ''
    gestures.setMode('browse')
  })
}
```

import `copyText`：

```ts
import { copyText } from '@/utils/clipboard'
```

- [ ] **Step 8: 悬浮复制条样式**

在 `<style scoped>` 新增：

```css
.selection-copy-bar {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 10px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--accent-color) 90%, black);
  color: #fff;
  font-size: 12px;
  z-index: 20;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.25);
}
.selection-copy-count {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.selection-copy-btn {
  border: none;
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
  padding: 4px 14px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
}
.copy-bar-enter-active,
.copy-bar-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.copy-bar-enter-from,
.copy-bar-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
```

- [ ] **Step 9: 运行 typecheck + 测试**

Run: typecheck + `npx vitest run src/composables/__tests__/useTerminalTabs.test.ts src/components/__tests__/terminalGestures.test.ts`
Expected: 通过。

- [ ] **Step 10: Commit**

```bash
git add web/src/composables/useTerminalTabs.ts web/src/components/terminal/TerminalPanelContent.vue
git commit -m "feat(terminal): drag-to-select with floating copy bar"
```

---

### Task 6: 移除「复制输出」按钮 + `OutputDrawer`

**Files:**
- Modify: `web/src/components/terminal/TerminalPanelContent.vue`
- Delete: `web/src/components/terminal/OutputDrawer.vue`

- [ ] **Step 1: 删除按钮**

`TerminalPanelContent.vue` 第 116-119 行（`<!-- Copy output button -->` 按钮）整体删除：

```html
            <!-- Copy output button -->
            <button class="toolbar-btn btn-action" @click="handleCopyOutput" :title="t('terminal.copyOutput')">
              <CopyIcon :size="14" />
            </button>
```

- [ ] **Step 2: 删除模板中的 OutputDrawer**

第 163-169 行整体删除：

```html
    <!-- Output text drawer — copy visible terminal output -->
    <OutputDrawer
      :open="outputDrawer.effectiveOpen.value"
      :output-text="outputDrawerText"
      :font-size="fontSize"
      @close="outputDrawer.close()"
    />
```

- [ ] **Step 3: 删除 script 中的相关声明**

删除第 181 行 `import OutputDrawer ...`。
删除第 248-249 行：

```ts
const outputDrawer = useTabDrawer('terminal')
const outputDrawerText = ref('')
```

删除 `handleCopyOutput`（原第 679-706 行）整个函数。
删除第 204 行 import 中的 `Copy as CopyIcon`（Task 4 已改）。

- [ ] **Step 4: 删除组件文件**

```bash
rm web/src/components/terminal/OutputDrawer.vue
```

- [ ] **Step 5: 检查残留引用**

Run: `rg -n "OutputDrawer|outputDrawer|outputDrawerText|handleCopyOutput|copyOutput|noOutput|CopyIcon" web/src`
Expected: 无输出（除 i18n 已删键、以及 `terminal.copyOutput`/`noOutput` 已从 zh/en 移除）。

- [ ] **Step 6: 运行 typecheck + 全量终端相关测试**

Run: typecheck + `npx vitest run src/components/__tests__/terminal`
Expected: 通过。

- [ ] **Step 7: Commit**

```bash
git add web/src/components/terminal/TerminalPanelContent.vue web/src/components/terminal/OutputDrawer.vue web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "refactor(terminal): remove copy-output button and OutputDrawer"
```

---

### Task 7: 全量验证

- [ ] **Step 1: 前端 lint + typecheck + 单测**

Run: `cd web && npm run lint`
Run: typecheck（见 package.json 脚本）
Run: `npx vitest run`

- [ ] **Step 2: 覆盖/推送检查**

Run: `./scripts/pre-push-checks.sh`
Expected: 全绿（覆盖率门槛：变更行覆盖率 ≥ 80%）。若因删除/新增拉低包覆盖率，补充缺失用例后重跑。

- [ ] **Step 3: 手动冒烟**

`./dev-server.sh --restart` 后：
1. 移动端打开终端，默认「浏览」态，单指上下滑滚动。
2. 点手势按钮切到「手势」态，滑动发方向键、双指缩放正常。
3. 再点切到「选区」态（按钮有描边、图标变化），单指上下拖动划出高亮选区，底部出现复制条，显示字符数。
4. 点复制 → toast「已复制」→ 选区清除、回到「浏览」态。
5. 确认工具栏已无「复制输出」按钮、无抽屉弹出。

- [ ] **Step 4: Commit（如有剩余改动）**

```bash
git add -A && git commit -m "chore(terminal): finalize select-mode verification"
```
