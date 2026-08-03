# 大屏双栏布局（Big-Screen SplitView）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 ≥1024px 宽屏自动启用双栏布局：右侧恒显聊天，左侧纵向 Dock 切换其余标签，可拖动分隔线调宽（比例持久化），抽屉大屏限宽。

**Architecture:** 复用现有单实例面板组件。`.content-area` 内包 `col-left`/`col-right` 两容器 + 通用 `SplitView`（受控双栏容器）；新增 `useBigScreenLayout` 模块单例管理 `isBigScreen`（matchMedia 1024）/`leftTab`/`splitRatio` 与持久化；`useTabDrawer` 扩展为双激活标签（chat + leftTab）；App.vue 内新增纵向 Dock 块（方案一，复用 scoped 样式）；BottomSheet 全局限宽 + `wide` 逃逸。

**Tech Stack:** Vue 3 `<script setup lang="ts">`, Vitest + @vue/test-utils (jsdom), 现有 `useTabDrawer`/`TabPanel`/CSS 变量体系。

---

## 前置约定

- 所有命令在仓库根目录 `/home/xulongzhe/projects/clawbench` 执行。
- 单测命令：`./scripts/vitest-run.sh <相对路径>`（vitest 有僵尸 worker 防护，勿裸跑 `npx vitest`）。
- 类型检查：`npm run typecheck`；Lint：`npm run lint`。
- 测试环境 jsdom **无 `window.matchMedia`**，任何 matchMedia 使用都必须有守卫（本计划已内置）。
- 每步提交前先 `git status` 确认只暂存本任务文件。

---

### Task 1: 比例工具函数 `utils/splitRatio.ts`

**Files:**
- Create: `web/src/utils/splitRatio.ts`
- Test: `web/src/utils/__tests__/splitRatio.test.ts`

- [ ] **Step 1: Write the failing test**

创建 `web/src/utils/__tests__/splitRatio.test.ts`：

```ts
import { describe, expect, it } from 'vitest'
import { normalizeRatio, clampRatio, MIN_PANEL_WIDTH, DEFAULT_RATIO } from '@/utils/splitRatio'

describe('normalizeRatio', () => {
  it('returns DEFAULT_RATIO for non-finite / non-number input', () => {
    expect(normalizeRatio(Number.NaN)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio('0.3' as unknown as number)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio(undefined as unknown as number)).toBe(DEFAULT_RATIO)
    expect(normalizeRatio(Number.POSITIVE_INFINITY)).toBe(DEFAULT_RATIO)
  })

  it('clamps to [0, 1]', () => {
    expect(normalizeRatio(-0.5)).toBe(0)
    expect(normalizeRatio(1.7)).toBe(1)
    expect(normalizeRatio(0.4)).toBe(0.4)
  })
})

describe('clampRatio', () => {
  it('respects symmetric min widths on both sides', () => {
    // container 1000, minLeft=320, minRight=320 → left ∈ [320, 680]
    expect(clampRatio(0.1, 1000)).toBeCloseTo(0.32)   // 320/1000
    expect(clampRatio(0.9, 1000)).toBeCloseTo(0.68)   // 680/1000
    expect(clampRatio(0.5, 1000)).toBeCloseTo(0.5)
  })

  it('returns DEFAULT_RATIO when container is too small for two panels', () => {
    expect(clampRatio(0.3, MIN_PANEL_WIDTH * 2 - 10)).toBe(DEFAULT_RATIO)
  })

  it('returns DEFAULT_RATIO for invalid container width', () => {
    expect(clampRatio(0.3, 0)).toBe(DEFAULT_RATIO)
    expect(clampRatio(0.3, Number.NaN)).toBe(DEFAULT_RATIO)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/utils/__tests__/splitRatio.test.ts`
Expected: FAIL — `@/utils/splitRatio` module not found.

- [ ] **Step 3: Write implementation**

创建 `web/src/utils/splitRatio.ts`：

```ts
export const MIN_PANEL_WIDTH = 320
export const DEFAULT_RATIO = 0.5

/** Coerce an unknown value into a ratio in [0, 1]; defaults to DEFAULT_RATIO. */
export function normalizeRatio(raw: unknown): number {
  if (typeof raw !== 'number' || !Number.isFinite(raw)) return DEFAULT_RATIO
  return Math.min(1, Math.max(0, raw))
}

/**
 * Clamp a ratio so the left panel stays within [minLeft, containerWidth - minRight].
 * Returns DEFAULT_RATIO when the container can't hold two min-width panels.
 */
export function clampRatio(
  ratio: number,
  containerWidth: number,
  minLeft = MIN_PANEL_WIDTH,
  minRight = MIN_PANEL_WIDTH,
): number {
  if (!Number.isFinite(containerWidth) || containerWidth <= 0) return DEFAULT_RATIO
  if (containerWidth <= minLeft + minRight) return DEFAULT_RATIO
  const maxLeft = containerWidth - minRight
  const leftPx = Math.min(maxLeft, Math.max(minLeft, ratio * containerWidth))
  return leftPx / containerWidth
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/utils/__tests__/splitRatio.test.ts`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add web/src/utils/splitRatio.ts web/src/utils/__tests__/splitRatio.test.ts
git commit -m "feat: split-ratio clamp/normalize helpers"
```

---

### Task 2: `useBigScreenLayout` composable

**Files:**
- Create: `web/src/composables/useBigScreenLayout.ts`
- Test: `web/src/composables/__tests__/useBigScreenLayout.test.ts`

- [ ] **Step 1: Write the failing test**

创建 `web/src/composables/__tests__/useBigScreenLayout.test.ts`：

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  useBigScreenLayout,
  getBigScreenState,
  switchLeftTab,
  setSplitRatio,
  resetBigScreenState,
  registerBigScreenCallbacks,
  _setBigScreenForTest,
  _resetForTest,
  BIG_SCREEN_DOCK_TABS,
  LEFT_TAB_KEY,
  SPLIT_RATIO_KEY,
} from '@/composables/useBigScreenLayout'

beforeEach(() => {
  _resetForTest()
  resetBigScreenState()
  localStorage.clear()
})

describe('useBigScreenLayout', () => {
  it('matchMedia absent → isBigScreen stays false and does not throw', () => {
    const { isBigScreen } = useBigScreenLayout()
    expect(isBigScreen.value).toBe(false)
  })

  it('leftTab defaults to browse and is clamped to allowed tabs', () => {
    const { leftTab } = useBigScreenLayout()
    expect(leftTab.value).toBe('browse')
    expect(BIG_SCREEN_DOCK_TABS).toContain(leftTab.value)
  })

  it('switchLeftTab ignores invalid tabs and persists valid ones', () => {
    switchLeftTab('terminal')
    expect(localStorage.getItem(LEFT_TAB_KEY)).toBe('terminal')
    switchLeftTab('not-a-tab' as never)
    expect(localStorage.getItem(LEFT_TAB_KEY)).toBe('terminal')
  })

  it('switchLeftTab runs registered side-effects and activeTab setter, but only on change', () => {
    const sideEffects = vi.fn()
    const setActiveTab = vi.fn()
    registerBigScreenCallbacks({ sideEffects, setActiveTab })

    switchLeftTab('tasks')
    expect(setActiveTab).toHaveBeenCalledWith('tasks')
    expect(sideEffects).toHaveBeenCalledWith('tasks')

    switchLeftTab('tasks') // same tab → early return
    expect(sideEffects).toHaveBeenCalledTimes(1)
  })

  it('setSplitRatio normalizes and persists', () => {
    setSplitRatio(1.9)
    expect(Number(localStorage.getItem(SPLIT_RATIO_KEY))).toBe(1)
    setSplitRatio(0.35)
    expect(Number(localStorage.getItem(SPLIT_RATIO_KEY))).toBeCloseTo(0.35)
  })

  it('restores persisted leftTab on init', () => {
    localStorage.setItem(LEFT_TAB_KEY, 'settings')
    _resetForTest()
    const { leftTab } = useBigScreenLayout()
    expect(leftTab.value).toBe('settings')
  })

  it('big-screen mode makes getBigScreenState expose chat + leftTab as active tabs', () => {
    const { isBigScreen, leftTab } = getBigScreenState()
    _setBigScreenForTest(true)
    switchLeftTab('terminal')
    expect(isBigScreen.value).toBe(true)
    expect(leftTab.value).toBe('terminal')
  })
})

describe('useBigScreenLayout matchMedia wiring', () => {
  it('reflects matchMedia matches and change events', async () => {
    vi.resetModules()
    const listeners: Array<(e: { matches: boolean }) => void> = []
    const mql = {
      matches: true,
      addEventListener: (_t: string, cb: (e: { matches: boolean }) => void) => { listeners.push(cb) },
      removeEventListener: vi.fn(),
    }
    vi.stubGlobal('matchMedia', vi.fn(() => mql))
    const mod = await import('@/composables/useBigScreenLayout')
    expect(mod.getBigScreenState().isBigScreen.value).toBe(true)
    mql.matches = false
    listeners.forEach((cb) => cb({ matches: false }))
    expect(mod.getBigScreenState().isBigScreen.value).toBe(false)
    vi.unstubAllGlobals()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useBigScreenLayout.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write implementation**

创建 `web/src/composables/useBigScreenLayout.ts`：

```ts
import { ref } from 'vue'
import { normalizeRatio } from '@/utils/splitRatio'

export const BIG_SCREEN_MIN_WIDTH = 1024
export const LEFT_TAB_KEY = 'clawbench-bigscreen-left-tab'
export const SPLIT_RATIO_KEY = 'clawbench-bigscreen-split-ratio'
export const BIG_SCREEN_DOCK_TABS = ['browse', 'history', 'proxy', 'terminal', 'tasks', 'settings']

const isBigScreen = ref(false)
const leftTab = ref<string>('browse')
const splitRatio = ref(0.5)
let initialized = false
let sideEffects: ((tab: string) => void) | null = null
let setActiveTab: ((tab: string) => void) | null = null

function readPersistedLeftTab(): string {
  try {
    const v = localStorage.getItem(LEFT_TAB_KEY)
    if (v && BIG_SCREEN_DOCK_TABS.includes(v)) return v
  } catch {
    // localStorage may throw in restricted environments — fall through to default
  }
  return 'browse'
}

function initBigScreen() {
  if (initialized) return
  initialized = true
  leftTab.value = readPersistedLeftTab()
  try {
    const raw = Number(localStorage.getItem(SPLIT_RATIO_KEY))
    if (Number.isFinite(raw)) splitRatio.value = normalizeRatio(raw)
  } catch {
    // ignore
  }
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    const mql = window.matchMedia(`(min-width: ${BIG_SCREEN_MIN_WIDTH}px)`)
    isBigScreen.value = mql.matches
    const onChange = (e: MediaQueryListEvent) => { isBigScreen.value = e.matches }
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', onChange)
    } else if (typeof (mql as { addListener?: unknown }).addListener === 'function') {
      ;(mql as { addListener: (cb: (e: MediaQueryListEvent) => void) => void }).addListener(onChange)
    }
  }
}

/** Returns the shared big-screen state refs (initializes once). */
export function useBigScreenLayout() {
  initBigScreen()
  return { isBigScreen, leftTab, splitRatio }
}

/** Ref access for useTabDrawer (avoids importing refs eagerly at module scope). */
export function getBigScreenState() {
  initBigScreen()
  return { isBigScreen, leftTab }
}

export function registerBigScreenCallbacks(opts: { sideEffects?: (tab: string) => void; setActiveTab?: (tab: string) => void }) {
  sideEffects = opts.sideEffects ?? null
  setActiveTab = opts.setActiveTab ?? null
}

/** Switch the big-screen left column tab. Writes activeTab + side-effects via callbacks; does NOT call onTabSwitch. */
export function switchLeftTab(tab: string) {
  if (!BIG_SCREEN_DOCK_TABS.includes(tab)) return
  if (leftTab.value === tab) return
  leftTab.value = tab
  try {
    localStorage.setItem(LEFT_TAB_KEY, tab)
  } catch {
    // ignore
  }
  setActiveTab?.(tab)
  sideEffects?.(tab)
}

/** Normalize + persist the split ratio (persistence owned here, not in SplitView). */
export function setSplitRatio(ratio: number) {
  splitRatio.value = normalizeRatio(ratio)
  try {
    localStorage.setItem(SPLIT_RATIO_KEY, String(splitRatio.value))
  } catch {
    // ignore
  }
}

export function resetBigScreenState() {
  leftTab.value = 'browse'
  splitRatio.value = 0.5
  isBigScreen.value = false
  sideEffects = null
  setActiveTab = null
}

/** Test hooks — do not use in production code. */
export function _setBigScreenForTest(val: boolean) {
  isBigScreen.value = val
}
export function _resetForTest() {
  initialized = false
  resetBigScreenState()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useBigScreenLayout.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useBigScreenLayout.ts web/src/composables/__tests__/useBigScreenLayout.test.ts
git commit -m "feat: big-screen layout composable (matchMedia, leftTab, split ratio persistence)"
```

---

### Task 3: 通用 `SplitView` 双栏容器

**Files:**
- Create: `web/src/components/common/SplitView.vue`
- Test: `web/src/components/common/__tests__/SplitView.test.ts`

- [ ] **Step 1: Write the failing test**

创建 `web/src/components/common/__tests__/SplitView.test.ts`：

```ts
import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import SplitView from '@/components/common/SplitView.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

function mountSplit(props = {}) {
  return mount(SplitView, {
    props,
    slots: {
      left: '<div class="pane-left">L</div>',
      right: '<div class="pane-right">R</div>',
    },
    attachTo: document.body,
  })
}

let wrapper: VueWrapper | null = null
afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

describe('SplitView', () => {
  it('enabled=false: no divider, panes render inline', () => {
    wrapper = mountSplit({ enabled: false })
    expect(wrapper.find('.split-view__divider').exists()).toBe(false)
    expect(wrapper.find('.pane-left').text()).toBe('L')
    expect(wrapper.find('.pane-right').text()).toBe('R')
  })

  it('enabled=true: renders divider and left width follows ratio', async () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.4 })
    expect(wrapper.find('.split-view__divider').exists()).toBe(true)
    const left = wrapper.find('.split-view__left')
    await nextTick()
    expect(left.attributes('style')).toContain('width: 40%')
  })

  it('emits update:ratio on divider drag, clamped to min widths', async () => {
    wrapper = mountSplit({ enabled: true, ratio: 0.5 })
    const divider = wrapper.find('.split-view__divider').element as HTMLElement
    vi.spyOn(divider, 'setPointerCapture').mockImplementation(() => {})
    vi.spyOn(divider, 'releasePointerCapture').mockImplementation(() => {})

    Object.defineProperty(wrapper.find('.split-view').element, 'getBoundingClientRect', {
      configurable: true,
      value: () => ({ left: 0, width: 1000, top: 0, bottom: 0, height: 600, right: 1000, x: 0, y: 0, toJSON() {} }),
    })

    divider.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 100 }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))

    const emitted = wrapper.emitted('update:ratio') as Array<Array<number>>
    expect(emitted).toBeTruthy()
    // 100px / 1000 = 0.1, clamped up to 320/1000 = 0.32
    expect(emitted[emitted.length - 1][0]).toBeCloseTo(0.32)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/SplitView.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Write implementation**

创建 `web/src/components/common/SplitView.vue`：

```vue
<template>
  <div ref="rootRef" class="split-view" :class="{ 'split-view--active': enabled }">
    <div class="split-view__left" :style="leftStyle">
      <slot name="left" />
    </div>
    <div
      v-if="enabled"
      ref="dividerRef"
      class="split-view__divider"
      role="separator"
      aria-orientation="vertical"
      :aria-valuenow="Math.round(internalRatio * 100)"
      :aria-valuemin="Math.round(minLeftRatio)"
      :aria-valuemax="Math.round(maxLeftRatio)"
      :title="title"
      @pointerdown="onDividerPointerDown"
    >
      <div class="split-view__gutter-line" />
    </div>
    <div class="split-view__right">
      <slot name="right" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { clampRatio, normalizeRatio, MIN_PANEL_WIDTH } from '@/utils/splitRatio'

const props = withDefaults(defineProps<{
  enabled: boolean
  ratio?: number
  minLeft?: number
  minRight?: number
  gutterSize?: number
  title?: string
}>(), {
  ratio: 0.5,
  minLeft: MIN_PANEL_WIDTH,
  minRight: MIN_PANEL_WIDTH,
  gutterSize: 6,
  title: '拖动调整面板宽度',
})

const emit = defineEmits<{ (e: 'update:ratio', ratio: number): void }>()

const rootRef = ref<HTMLDivElement | null>(null)
const dividerRef = ref<HTMLDivElement | null>(null)
const internalRatio = ref(normalizeRatio(props.ratio))
const containerWidth = ref(0)
let dragActive = false
let observer: ResizeObserver | null = null

watch(() => props.ratio, (r) => {
  internalRatio.value = normalizeRatio(r)
})

const leftStyle = computed(() => {
  if (!props.enabled) return {}
  return { width: `${internalRatio.value * 100}%` }
})

const minLeftRatio = computed(() => (containerWidth.value > 0 ? props.minLeft / containerWidth.value : 0))
const maxLeftRatio = computed(() => (containerWidth.value > 0 ? 1 - props.minRight / containerWidth.value : 1))

function measureContainer() {
  if (rootRef.value) containerWidth.value = rootRef.value.getBoundingClientRect().width
}

function onMove(e: PointerEvent) {
  const rect = rootRef.value?.getBoundingClientRect()
  if (!rect || rect.width <= 0) return
  const raw = (e.clientX - rect.left) / rect.width
  const ratio = clampRatio(raw, rect.width, props.minLeft, props.minRight)
  internalRatio.value = ratio
  emit('update:ratio', ratio)
}

function onDividerPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  dragActive = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('split-view-dragging')
  onMove(e)
}

function onPointerMove(e: PointerEvent) {
  if (dragActive) onMove(e)
}

function onPointerUp(e: PointerEvent) {
  if (!dragActive) return
  dragActive = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('split-view-dragging')
}

onMounted(() => {
  measureContainer()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measureContainer)
    if (rootRef.value) observer.observe(rootRef.value)
  }
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})

onBeforeUnmount(() => {
  observer?.disconnect()
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  document.body.classList.remove('split-view-dragging')
})
</script>

<style scoped>
.split-view {
  position: relative;
  height: 100%;
  width: 100%;
}
.split-view--active {
  display: flex;
  flex-direction: row;
  align-items: stretch;
}
.split-view__left,
.split-view__right {
  position: absolute;
  inset: 0;
}
.split-view--active .split-view__left,
.split-view--active .split-view__right {
  position: relative;
  inset: auto;
  height: 100%;
}
.split-view--active .split-view__left {
  flex: 0 0 auto;
  min-width: 320px;
  max-width: calc(100% - 320px - 6px);
}
.split-view--active .split-view__right {
  flex: 1 1 auto;
  min-width: 320px;
}
/* Divider: narrow by default, wider hit-area + highlight on hover/touch */
.split-view__divider {
  position: relative;
  flex: 0 0 auto;
  width: 6px;
  cursor: col-resize;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
}
.split-view__divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -4px;
  right: -4px;
}
.split-view__divider:hover,
.split-view__divider:active {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
.split-view__gutter-line {
  position: absolute;
  left: 50%;
  top: 0;
  bottom: 0;
  width: 2px;
  transform: translateX(-50%);
  background: var(--border-color, rgba(0, 0, 0, 0.12));
  transition: background 0.15s ease;
}
.split-view__divider:hover .split-view__gutter-line,
.split-view__divider:active .split-view__gutter-line {
  background: var(--accent-color, #0066cc);
}
body.split-view-dragging {
  user-select: none;
  cursor: col-resize;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/SplitView.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/common/SplitView.vue web/src/components/common/__tests__/SplitView.test.ts
git commit -m "feat: generic SplitView two-pane container with draggable divider"
```

---

### Task 4: `useTabDrawer` 大屏双激活标签

**Files:**
- Modify: `web/src/composables/useTabDrawer.ts`
- Test: `web/src/composables/__tests__/useTabDrawer.test.ts`

- [ ] **Step 1: Write the failing test (append to existing file)**

在 `web/src/composables/__tests__/useTabDrawer.test.ts` 末尾追加：

```ts
import { _setBigScreenForTest, resetBigScreenState as resetBigScreen, switchLeftTab } from '@/composables/useBigScreenLayout'

describe('useTabDrawer big-screen awareness', () => {
  beforeEach(() => {
    resetBigScreen()
    _setBigScreenForTest(false)
  })

  it('big-screen: chat and leftTab drawers both open simultaneously', () => {
    _setBigScreenForTest(true)
    switchLeftTab('browse')
    const chatDrawer = useTabDrawer('chat')
    const browseDrawer = useTabDrawer('browse')

    chatDrawer.open()
    browseDrawer.open()
    expect(chatDrawer.effectiveOpen.value).toBe(true)
    expect(browseDrawer.effectiveOpen.value).toBe(true)

    switchLeftTab('terminal')
    expect(browseDrawer.effectiveOpen.value).toBe(false)
    expect(chatDrawer.effectiveOpen.value).toBe(true)
  })

  it('big-screen: autoRestore:false closes when leftTab switches away', () => {
    _setBigScreenForTest(true)
    switchLeftTab('browse')
    const drawer = useTabDrawer('browse', { autoRestore: false })
    drawer.open()
    expect(drawer.effectiveOpen.value).toBe(true)

    switchLeftTab('tasks')
    expect(drawer.effectiveOpen.value).toBe(false)
  })
})
```

> 注：该文件顶部已有 `beforeEach(() => { resetTabDrawerState() })`。新增的 `beforeEach` 块同样在每个用例前运行，二者不冲突（`resetTabDrawerState` 复位抽屉注册，`resetBigScreen` 复位大屏状态）。若同一 `describe` 内出现同名 `beforeEach` 冲突，请把新增 import 放到文件顶部，并把新增 beforeEach 合并进顶部现有 beforeEach。

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useTabDrawer.test.ts`
Expected: FAIL — 新用例失败（effectiveOpen 不认大屏）。

- [ ] **Step 3: Write implementation**

修改 `web/src/composables/useTabDrawer.ts`：

(a) 顶部新增 import（放在现有 import 之后）：

```ts
import { getBigScreenState } from './useBigScreenLayout'

const { isBigScreen, leftTab } = getBigScreenState()
```

(b) 替换 `effectiveOpen` 计算（原 L112）：

```ts
  const effectiveOpen = computed(() => {
    const tabActive =
      currentTab.value === tabId ||
      (isBigScreen.value && (tabId === 'chat' || tabId === leftTab.value))
    return tabActive && openRef.value
  })
```

(c) 替换 autoRestore:false watcher（原 L115-121）：

```ts
  // For autoRestore: false, close the drawer when its tab is no longer active
  // (narrow: currentTab changed; big-screen: leftTab changed away)
  if (!autoRestore) {
    const closeIfInactive = () => {
      if (!openRef.value) return
      const active =
        currentTab.value === tabId ||
        (isBigScreen.value && (tabId === 'chat' || tabId === leftTab.value))
      if (!active) openRef.value = false
    }
    watch(() => [currentTab.value, isBigScreen.value, leftTab.value], closeIfInactive)
  }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/composables/__tests__/useTabDrawer.test.ts`
Expected: PASS（原有用例 + 新增 2 个）。

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useTabDrawer.ts web/src/composables/__tests__/useTabDrawer.test.ts
git commit -m "feat: tab drawers treat chat + big-screen leftTab as simultaneously active"
```

---

### Task 5: BottomSheet 大屏限宽 + `wide` prop

**Files:**
- Modify: `web/src/components/common/BottomSheet.vue`
- Test: `web/src/components/common/__tests__/BottomSheet.test.ts`

- [ ] **Step 1: Write the failing test (append to existing file)**

在 `web/src/components/common/__tests__/BottomSheet.test.ts` 末尾追加（该文件已提供 `mountSheet` helper 与 `$` 查询）：

```ts
describe('BottomSheet wide prop', () => {
  it('adds bs-wide class to the panel when wide=true', () => {
    mountSheet({ wide: true })
    expect($('.bs-panel')?.classList.contains('bs-wide')).toBe(true)
  })

  it('does not add bs-wide class by default', () => {
    mountSheet({})
    expect($('.bs-panel')?.classList.contains('bs-wide')).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/BottomSheet.test.ts`
Expected: FAIL — `wide` prop 不存在。

- [ ] **Step 3: Write implementation**

修改 `web/src/components/common/BottomSheet.vue`：

(a) 模板面板 class（L13）：

```html
      <div class="bs-panel" :class="{ 'bs-leaving': leaving, 'bs-instant': instant, 'bs-compact': compact, 'bs-auto': auto, 'bs-handle-only': handleOnly, 'bs-wide': wide }">
```

(b) props（L39-52 区域，`noHeader` 声明之后新增）：

```ts
  wide: Boolean, // 大屏模式放宽面板宽度（默认 560px → 820px）
```

(c) `<style>` 块末尾（keyframes 之后任意位置）追加：

```css
/* Big-screen (≥1024px): constrain bottom-sheet width and center it.
   Keep narrow screens full-width. */
@media (min-width: 1024px) {
  .bs-panel {
    max-width: 560px;
    margin: 0 auto;
  }
  .bs-panel.bs-wide {
    max-width: 820px;
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `./scripts/vitest-run.sh web/src/components/common/__tests__/BottomSheet.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/common/BottomSheet.vue web/src/components/common/__tests__/BottomSheet.test.ts
git commit -m "feat: constrain bottom-sheet width on big screens with wide escape hatch"
```

---

### Task 6: 全局大屏样式 `big-screen.css` + 引入

**Files:**
- Create: `web/css/big-screen.css`
- Modify: `web/index.html`

- [ ] **Step 1: Create the stylesheet**

创建 `web/css/big-screen.css`：

```css
/* ==========================================================================
   Big-screen (≥1024px) two-column layout
   Activated via .big-screen class on .main-content (bound to useBigScreenLayout.isBigScreen)
   ========================================================================== */

/* Switch the single-column flex to a horizontal dock + split layout */
.main-content.big-screen {
  flex-direction: row;
}

/* Let the split area shrink when the vertical dock is present */
.main-content.big-screen .content-area {
  min-width: 0;
}
```

- [ ] **Step 2: Wire into index.html**

在 `web/index.html` 的 stylesheet 链接区（`layout.css` 之后）新增：

```html
    <link rel="stylesheet" href="/css/big-screen.css">
```

- [ ] **Step 3: Verify it loads**

Run: `npm run typecheck`
Expected: PASS（纯 CSS/HTML，无类型影响；此步用于确认无语法破坏）。

- [ ] **Step 4: Commit**

```bash
git add web/css/big-screen.css web/index.html
git commit -m "feat: big-screen layout global stylesheet"
```

---

### Task 7: App.vue 布局重构（SplitView + col-left/col-right + 模式感知路由）

**Files:**
- Modify: `web/src/App.vue`（模板 L22-131 区、脚本区、样式区）

此任务改动大，分 4 个子步骤，每步独立可验证。

- [ ] **Step 7.1: 脚本区 import 与状态**

在 `web/src/App.vue` 脚本顶部 import 区（L347-348 附近，`formatBadgeCount` import 之后）新增：

```ts
import SplitView from './components/common/SplitView.vue'
import {
  useBigScreenLayout,
  switchLeftTab,
  setSplitRatio,
  registerBigScreenCallbacks,
  BIG_SCREEN_DOCK_TABS,
} from './composables/useBigScreenLayout'
```

在 `const activeTab = ref('chat')`（L453）之后新增：

```ts
// ── Big-screen layout state ──
const { isBigScreen, leftTab, splitRatio } = useBigScreenLayout()

const chatActive = computed(() => (isBigScreen.value ? 'chat' : activeTab.value))
const leftPanelActive = computed(() => (isBigScreen.value ? leftTab.value : activeTab.value))
const panelIsActive = (tabId: string) =>
  isBigScreen.value ? leftTab.value === tabId : activeTab.value === tabId

function onSplitRatioChange(ratio: number) {
  setSplitRatio(ratio)
}
```

- [ ] **Step 7.2: `switchTab` 模式感知 + 模式同步 watcher**

替换 `function switchTab(tab)`（L474-497）为：

```ts
function switchTab(tab) {
  if (isBigScreen.value) {
    // Big-screen: chat is always visible; non-chat tabs route to the left column
    if (tab === 'chat') return
    switchLeftTab(tab)
    return
  }
  if (activeTab.value === tab) return
  activeTab.value = tab
  // Auto-close all drawers not belonging to the new tab
  onTabSwitch(tab)
  if (tab === 'browse') {
    store.loadFiles(store.state.currentDir)
  }
  if (tab === 'chat') {
    loadSessionsOnce()
  }
  if (tab === 'tasks') {
    store.state.taskUnreadCount = 0
    loadTasks()
  }
  // Close overflow menu on any tab switch
  overflowMenuOpen.value = false
}
```

在 `switchTab` 定义之后新增模式同步 watcher 与回调注册（`loadTasks` 已在 L637 解构，故本段必须放在 L637 `useTaskTab()` 之后；建议放在 `handleInlineOverflowClick` 之后）：

```ts
// Big-screen mode transitions: keep useTabDrawer's currentTab coherent
// (chat drawers work in wide mode; collapse returns to the last active tab).
watch(isBigScreen, (val) => {
  if (val) {
    // Continuity-first (Q1A): adopt activeTab if non-chat, else keep persisted leftTab
    const next = activeTab.value !== 'chat' ? activeTab.value : leftTab.value
    if (leftTab.value !== next) switchLeftTab(next)
    onTabSwitch('chat')
    overflowMenuOpen.value = false
  } else {
    onTabSwitch(activeTab.value)
  }
}, { immediate: true })

// Route leftTab side-effects (reuse narrow-mode behaviors) and sync activeTab (Q3B)
registerBigScreenCallbacks({
  setActiveTab: (tab) => { activeTab.value = tab },
  sideEffects: (tab) => {
    if (tab === 'browse') store.loadFiles(store.state.currentDir)
    if (tab === 'tasks') { store.state.taskUnreadCount = 0; loadTasks() }
  },
})
```

> 若 `watch` / `computed` 尚未导入，在 script 顶部 Vue import 处补充（App.vue 已大量使用二者，通常已导入）。

- [ ] **Step 7.3: 模板重构 content-area**

替换 `web/src/App.vue` 模板 L22-131（`<main class="main-content"> ... </main>` 整个块）为：

```html
      <main class="main-content" :class="{ 'big-screen': isBigScreen }">
        <!-- Big-screen vertical dock (non-chat tabs only) -->
        <div v-show="isBigScreen" class="big-dock">
          <div class="big-dock-center">
            <div class="dock-active-indicator big-dock-active-indicator" :style="bigDockIndicatorStyle"></div>
            <div v-for="tab in BIG_SCREEN_DOCK_TABS" :key="tab" class="dock-btn-wrap">
              <button class="dock-btn" :class="bigDockBtnClass(tab)" @click.stop="switchLeftTab(tab)" :title="bigDockTabTitle(tab)">
                <component :is="bigDockTabIcon(tab)" />
              </button>
              <span v-if="bigDockBadgeVisible(tab)" class="dock-badge dock-badge-count" :class="{ 'dock-badge-pop': bigDockBadgeAnim(tab) }" @animationend="bigDockBadgeAnimEnd(tab)">{{ formatBadgeCount(bigDockBadgeCount(tab)) }}</span>
            </div>
          </div>
        </div>

        <div class="content-area" id="contentArea">
          <SplitView
            :enabled="isBigScreen"
            :ratio="splitRatio"
            @update:ratio="onSplitRatioChange"
          >
            <template #left>
              <div class="col-left" v-show="isBigScreen || activeTab !== 'chat'">
                <!-- File Browse Tab (合一：目录浏览 + 文件覆盖预览) -->
                <TabPanel tabId="browse" :activeTab="leftPanelActive" :noHeader="true">
                  <div class="browse-panel">
                    <FileManagerContent
                      ref="fileManagerRef"
                      :entries="dirEntries"
                      :current-dir="currentDir"
                      :current-file="currentFile"
                      :show-hidden="showHidden"
                      :sort-field="sortField"
                      :sort-dir="sortDir"
                      :dir-loading="store.state.dirLoading"
                      :search-drawer="fileSearchDrawer"
                      :recent-drawer="recentFilesDrawer"
                      @navigate-dir="handleNavigateDir"
                      @navigate-back="handleNavigateBack"
                      @select-file="handleBrowseSelectFile"
                      @toggle-sort="handleToggleSort"
                      @toggle-hidden="toggleHidden"
                      @rename="handleRename"
                      @delete="handleDelete"
                      @batch-delete="handleBatchDelete"
                      @refresh="handleRefresh"
                      @open-terminal="handleOpenTerminal"
                    />
                    <FileOverlay
                      ref="fileOverlayRef"
                      :overlay-open="fileNav.overlayOpen.value"
                      :current-file="currentFile"
                      :file-loading="store.state.fileLoading"
                      :toc-open="tocDrawer.effectiveOpen.value"
                      :search-open="searchDrawer.effectiveOpen.value"
                      :markdown-view-mode="markdownViewMode"
                      :file-history-open="fileHistoryDrawer.effectiveOpen.value"
                      :toc-file="tocFile"
                      :pdf-outline="pdfOutline"
                      @delete="handleDelete($event)"
                      @show-details="detailsDrawer.open()"
                      @open-git-history="openFileHistory"
                      @toggle-toc="tocDrawer.toggle()"
                      @toggle-search="currentFile?.content && searchDrawer.toggle()"
                      @toggle-view="markdownViewMode = markdownViewMode === 'rendered' ? 'raw' : 'rendered'"
                      @refresh="handleRefresh"
                      @jump="scrollToLine"
                      @jump-page="handleJumpPdfPage"
                      @close-git-history="fileHistoryDrawer.close()"
                      @open-file="handleOverlayOpenFile"
                      @overlay-close="handleOverlayClose"
                      @open-recent-files="recentFilesDrawer.open()"
                    />
                  </div>
                </TabPanel>

                <!-- History Tab -->
                <TabPanel tabId="history" :activeTab="leftPanelActive" :noHeader="true">
                  <GitHistoryContent
                    mode="project"
                    :active="panelIsActive('history')"
                    @open-file="handleSelectFile"
                  />
                </TabPanel>

                <!-- Proxy Tab -->
                <TabPanel tabId="proxy" :activeTab="leftPanelActive" :noHeader="true">
                  <ProxyPanelContent />
                </TabPanel>

                <!-- Terminal Tab -->
                <TabPanel tabId="terminal" :activeTab="leftPanelActive" :noHeader="true">
                  <TerminalPanelContent
                    :requested-cwd="terminalRequestedCwd"
                    :active="panelIsActive('terminal')"
                    :platform-unsupported="isPlatformUnsupported"
                    @cwd-handled="terminalRequestedCwd = null"
                  />
                </TabPanel>

                <!-- Tasks Tab -->
                <TabPanel tabId="tasks" :activeTab="leftPanelActive" :noHeader="true">
                  <TaskTab :active="panelIsActive('tasks')" @open-file="handleTaskOpenFile" />
                </TabPanel>

                <!-- Settings Tab -->
                <TabPanel tabId="settings" :activeTab="leftPanelActive" :noHeader="true">
                  <SettingsPage :active="panelIsActive('settings')" />
                </TabPanel>
              </div>
            </template>

            <template #right>
              <div class="col-right" v-show="isBigScreen || activeTab === 'chat'">
                <!-- Chat Tab -->
                <TabPanel tabId="chat" :activeTab="chatActive">
                  <template #header>
                    <span class="bs-header-title"><AgentIcon v-if="sessionIdentity.currentAgentId.value" :backend="getAgentBackend(sessionIdentity.currentAgentId.value)" :name="getAgentName(sessionIdentity.currentAgentId.value)" :size="18" />{{ sessionIdentity.agentHeaderTitle.value }}</span>
                    <div v-if="sessionIdentity.currentSessionTitle.value" class="bs-header-description">
                      <HeaderMarquee :text="sessionIdentity.currentSessionTitle.value">{{ sessionIdentity.currentSessionTitle.value }}</HeaderMarquee>
                    </div>
                  </template>
                  <ChatPanelContent
                    :active="isBigScreen || activeTab === 'chat'"
                    :current-file="currentFile"
                    :current-dir="currentDir"
                    @open="switchTab('chat')"
                    @open-file="handleSelectFile"
                    @task-card-click="onTaskCardClick"
                    @open-acp-sessions="acpSessionDrawer.open()"
                    @open-session-search="sessionSearchDrawer.open()"
                  />
                </TabPanel>
              </div>
            </template>
          </SplitView>
        </div>
      </main>
```

> 注意：`FileOverlay` 的 `:markdown-view-mode` 等处绑定的 `markdownViewMode` 等变量需与现状一致；本计划中已按原模板保留全部绑定。替换时请以当前 L22-131 实际内容为准，仅做三类修改：(1) 外层包 `<SplitView>` 与 `col-left`/`col-right`；(2) `:activeTab` 由 `activeTab` 改为 `chatActive`/`leftPanelActive`；(3) 各面板 `:active` 改为 `panelIsActive(tabId)`；聊天面板 `:active` 改为 `isBigScreen || activeTab === 'chat'`。

- [ ] **Step 7.4: 新增 col-left / col-right 定位样式**

在 `web/src/App.vue` scoped 样式块（`<style scoped>` 内任意位置）新增：

```css
/* Big-screen split panes — positioned ancestors for the absolute TabPanels */
.col-left,
.col-right {
    position: relative;
    height: 100%;
}
```

- [ ] **Step 7.4b: 底部 Dock 大屏隐藏**

修改 `.bottom-dock-wrapper` 的 `v-show`（L196）：

```html
      <div v-if="isAuthenticated" v-show="!anyKeyboardActive && !isBigScreen" class="bottom-dock-wrapper">
```

- [ ] **Step 7.5: 编译验证**

Run: `npm run typecheck`
Expected: PASS。

Run: `npm run lint`
Expected: PASS（若报未使用变量等，修正后重跑）。

- [ ] **Step 7.6: Commit**

```bash
git add web/src/App.vue
git commit -m "feat: restructure content-area into big-screen SplitView with mode-aware tab routing"
```

---

### Task 8: App.vue 纵向 Dock 逻辑与样式

**Files:**
- Modify: `web/src/App.vue`（脚本辅助函数 + scoped 样式）

- [ ] **Step 1: 新增纵向 Dock 辅助函数**

在 `web/src/App.vue` `handleInlineOverflowClick` 定义（L1232-1238）之后新增：

```ts
// ── Big-screen vertical dock helpers ──
const bigScreenTabMeta = {
  browse: { icon: FolderOpen, titleKey: 'nav.fileManager' },
  history: { icon: GitBranch, titleKey: 'git.history.projectHistory' },
  tasks: overflowTabMeta.tasks,
  proxy: overflowTabMeta.proxy,
  terminal: overflowTabMeta.terminal,
  settings: overflowTabMeta.settings,
}

function bigDockTabIcon(tab) {
  return bigScreenTabMeta[tab]?.icon ?? FolderOpen
}
function bigDockTabTitle(tab) {
  return bigScreenTabMeta[tab] ? t(bigScreenTabMeta[tab].titleKey) : ''
}
function bigDockBtnClass(tab) {
  return {
    active: leftTab.value === tab,
    'has-unread': tab === 'tasks' && store.state.taskUnreadCount > 0 && leftTab.value !== 'tasks',
    'just-completed': tab === 'tasks' && store.state.taskJustCompleted && leftTab.value !== 'tasks',
    'has-running': tab === 'tasks' && store.state.taskRunning && leftTab.value !== 'tasks',
  }
}
function bigDockBadgeCount(tab) {
  switch (tab) {
    case 'history': return store.state.gitWorkingTreeChangeCount
    case 'tasks': return store.state.taskUnreadCount
    case 'terminal': return store.state.terminalSessionCount
    case 'proxy': return store.state.portForwardActiveCount
    default: return 0
  }
}
function bigDockBadgeVisible(tab) {
  return bigDockBadgeCount(tab) > 0 && leftTab.value !== tab
}
function bigDockBadgeAnim(tab) {
  switch (tab) {
    case 'history': return historyBadgeAnim.value
    case 'tasks': return taskBadgeAnim.value
    case 'terminal': return terminalBadgeAnim.value
    case 'proxy': return proxyBadgeAnim.value
    default: return false
  }
}
function bigDockBadgeAnimEnd(tab) {
  switch (tab) {
    case 'history': historyBadgeAnim.value = false; break
    case 'tasks': taskBadgeAnim.value = false; break
    case 'terminal': terminalBadgeAnim.value = false; break
    case 'proxy': proxyBadgeAnim.value = false; break
  }
}
const bigDockActiveIndex = computed(() => {
  const i = BIG_SCREEN_DOCK_TABS.indexOf(leftTab.value)
  return i >= 0 ? i : 0
})
const bigDockIndicatorStyle = computed(() => ({
  transform: `translate(-50%, ${bigDockActiveIndex.value * DOCK_STEP}px)`,
}))
```

> 前置：`historyBadgeAnim`/`taskBadgeAnim`/`terminalBadgeAnim`/`proxyBadgeAnim` 四个 ref 已在 App.vue 现有脚本中定义（底部 Dock 使用）。若命名不同，以实际定义名为准同步替换。

- [ ] **Step 2: 新增 scoped 样式**

在 `web/src/App.vue` scoped 样式块（`<style scoped>` 内，`.bottom-dock-wrapper` 之前或之后均可）新增：

```css
/* Big-screen vertical dock (left edge) */
.big-dock {
    flex-shrink: 0;
    width: 60px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-primary);
    border-right: 1px solid color-mix(in srgb, var(--border-color) 40%, transparent);
    -webkit-tap-highlight-color: transparent;
    user-select: none;
}

.big-dock-center {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
}

/* Vertical variant of the water-drop indicator (base .dock-active-indicator is in App.vue scoped) */
.big-dock-active-indicator {
    left: 50%;
    top: 0;
}
```

- [ ] **Step 3: 编译验证**

Run: `npm run typecheck`
Expected: PASS。

Run: `npm run lint`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add web/src/App.vue
git commit -m "feat: big-screen vertical dock with badges and active indicator"
```

---

### Task 9: 全量验证

**Files:** 无代码改动，仅验证。

- [ ] **Step 1: 运行全部前端测试**

Run: `./scripts/vitest-run.sh`
Expected: PASS（全部现有 + 新增用例）。

- [ ] **Step 2: 类型检查**

Run: `npm run typecheck`
Expected: PASS。

- [ ] **Step 3: Lint**

Run: `npm run lint`
Expected: PASS。

- [ ] **Step 4: 构建**

Run: `npm run build`
Expected: PASS（产物输出正常）。

- [ ] **Step 5: 手工功能核对清单（浏览器 ≥1024px）**

- [ ] 拉宽窗口进大屏：左侧出现纵向 Dock（browse/history/proxy/terminal/tasks/settings），无 chat 项；右侧恒为聊天；底部 Dock 消失
- [ ] 点击纵向 Dock 各按钮：左栏切换对应面板；当前项高亮 + 水珠指示条纵向移动
- [ ] 拖分隔线：左右实时变宽；最小宽度两侧均约 320px；刷新后比例保持（localStorage `clawbench-bigscreen-split-ratio`）
- [ ] 收窄窗口 <1024：回到单栏，显示最后使用的左栏标签；底部 Dock 恢复
- [ ] 大屏下打开会话抽屉（右栏）：居中限宽 560px，不横跨全宽；文件搜索抽屉（左栏）可打开
- [ ] 大屏下聊天流式输出：右侧持续自动滚动；在左栏文件管理器 Ctrl+←/→ 可切换会话（可接受行为）
- [ ] 切项目（AppHeader）：leftTab 保持；再进大屏仍生效
- [ ] 任务完成通知 → 左栏自动切到 tasks；chat 未读不影响 Dock（Dock 已隐藏）

- [ ] **Step 6: 最终提交**

```bash
git status
git log --oneline -12
```
确认本特性所有提交已就位，无多余未提交改动。
