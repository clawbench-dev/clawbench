# 会话列表图钉固定侧栏 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 PC 宽屏下，为会话列表增加「图钉」固定侧栏功能，允许用户在抽屉模式与右侧常驻可拖拽侧栏模式间切换。

**Architecture:** 从 `SessionDrawer.vue` 抽取可复用的 `SessionList.vue`（行渲染+无限滚动+键盘导航）与 `SessionListHeader.vue`（标题栏）。新增 `SessionSidebar.vue` 作为右侧常驻列，由新单例 composable `useSessionSidebar.ts` 管理开关/宽度/持久化。侧栏打开时隐藏 `ChatInputBar` 的会话按钮并将 `openSessionTab` 桥接为收起侧栏。

**Tech Stack:** Vue 3 (script setup)、lucide-vue-next 图标、Vitest + @vue/test-utils、Vue I18n。

---

## 文件结构

- Create: `web/src/composables/useSessionSidebar.ts` — 侧栏单例状态（open/width/持久化/桥接）
- Create: `web/src/components/session/SessionList.vue` — 会话行列表（抽取自 SessionDrawer）
- Create: `web/src/components/session/SessionListHeader.vue` — 会话标题栏（计数条+动作区）
- Create: `web/src/components/session/SessionSidebar.vue` — 右侧常驻侧栏（含拖拽调宽）
- Modify: `web/src/components/session/SessionDrawer.vue` — 使用抽取的组件，header 加图钉，emit pin
- Modify: `web/src/components/chat/ChatInputBar.vue` — 会话按钮按 `sessionPanelOpen` 隐藏
- Modify: `web/src/components/chat/ChatPanelContent.vue` — 透传 `session-sidebar-open` 到 ChatInputBar
- Modify: `web/src/App.vue` — col-right 内嵌侧栏、绑定 SessionDrawer @pin、初始化 composable
- Modify: `web/src/i18n/locales/zh.ts` + `en.ts` — 图钉/侧栏文案
- Test: 对应 `__tests__/` 各测试文件

---

### Task 1: `useSessionSidebar.ts` composable

**Files:**
- Create: `web/src/composables/useSessionSidebar.ts`
- Test: `web/src/composables/__tests__/useSessionSidebar.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, beforeEach, vi } from 'vitest'
import { useSessionSidebar, _resetForTest, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'

const KEY = 'clawbench-session-sidebar'

describe('useSessionSidebar', () => {
  beforeEach(() => {
    _resetForTest()
    localStorage.clear()
  })

  it('defaults to open on wide screen with default width when no stored state', () => {
    const s = useSessionSidebar()
    expect(s.open.value).toBe(true)
    expect(s.width.value).toBe(280)
  })

  it('restores stored open state and width', () => {
    localStorage.setItem(KEY, JSON.stringify({ open: false, width: 340 }))
    const s = useSessionSidebar()
    expect(s.open.value).toBe(false)
    expect(s.width.value).toBe(340)
  })

  it('falls back to defaults when localStorage is corrupted', () => {
    localStorage.setItem(KEY, '{not-json')
    const s = useSessionSidebar()
    expect(s.open.value).toBe(true)
    expect(s.width.value).toBe(280)
  })

  it('clamps width to [MIN, MAX]', () => {
    const s = useSessionSidebar()
    s.setWidth(10)
    expect(s.width.value).toBe(SIDEBAR_MIN_WIDTH)
    s.setWidth(9000)
    expect(s.width.value).toBe(SIDEBAR_MAX_WIDTH)
  })

  it('setWidth persists to localStorage', () => {
    const s = useSessionSidebar()
    s.setWidth(300)
    const stored = JSON.parse(localStorage.getItem(KEY) || '{}')
    expect(stored.width).toBe(300)
  })

  it('openSidebar/closeSidebar persist state', () => {
    const s = useSessionSidebar()
    s.openSidebar()
    expect(localStorage.getItem(KEY)).toContain('"open":true')
    s.closeSidebar()
    expect(localStorage.getItem(KEY)).toContain('"open":false')
  })

  it('pinToSidebar opens sidebar', () => {
    const s = useSessionSidebar()
    s.closeSidebar()
    s.pinToSidebar()
    expect(s.open.value).toBe(true)
  })

  it('unpinToDrawer closes sidebar and calls registered openDrawer', () => {
    const s = useSessionSidebar()
    const openDrawer = vi.fn()
    s.registerOpenDrawer(openDrawer)
    s.pinToSidebar()
    s.unpinToDrawer()
    expect(s.open.value).toBe(false)
    expect(openDrawer).toHaveBeenCalled()
  })

  it('openSessionTabBridge toggles sidebar when open, else opens drawer', () => {
    const s = useSessionSidebar()
    const openDrawer = vi.fn()
    s.registerOpenDrawer(openDrawer)
    // Sidebar open → bridge toggles it closed (does NOT open drawer)
    s.open.value = true
    s.openSessionTabBridge()
    expect(s.open.value).toBe(false)
    expect(openDrawer).not.toHaveBeenCalled()
    // Sidebar closed → bridge opens drawer
    s.openSessionTabBridge()
    expect(openDrawer).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/composables/__tests__/useSessionSidebar.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write minimal implementation**

```ts
import { ref } from 'vue'

export const SESSION_SIDEBAR_KEY = 'clawbench-session-sidebar'
export const SIDEBAR_DEFAULT_WIDTH = 280
export const SIDEBAR_MIN_WIDTH = 220
export const SIDEBAR_MAX_WIDTH = 480

const open = ref(true)
const width = ref(SIDEBAR_DEFAULT_WIDTH)
let openDrawerFn: (() => void) | null = null
let initialized = false

function clampWidth(w: number): number {
  if (!Number.isFinite(w)) return SIDEBAR_DEFAULT_WIDTH
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, Math.round(w)))
}

function load() {
  try {
    const raw = localStorage.getItem(SESSION_SIDEBAR_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object') {
      open.value = parsed.open === true
      width.value = clampWidth(Number(parsed.width) || SIDEBAR_DEFAULT_WIDTH)
    }
  } catch {
    // corrupted storage → keep defaults
  }
}

function persist() {
  try {
    localStorage.setItem(SESSION_SIDEBAR_KEY, JSON.stringify({ open: open.value, width: width.value }))
  } catch {
    // ignore
  }
}

export function useSessionSidebar() {
  if (!initialized) {
    initialized = true
    load()
  }

  function openSidebar() {
    open.value = true
    persist()
  }
  function closeSidebar() {
    open.value = false
    persist()
  }
  function toggleSidebar() {
    open.value ? closeSidebar() : openSidebar()
  }
  function setWidth(w: number) {
    width.value = clampWidth(w)
    persist()
  }
  function pinToSidebar() {
    openSidebar()
  }
  function unpinToDrawer() {
    closeSidebar()
    openDrawerFn?.()
  }
  function registerOpenDrawer(fn: () => void) {
    openDrawerFn = fn
  }
  /** Bridge for openSessionTab: sidebar open → collapse it; else open the drawer. */
  function openSessionTabBridge() {
    if (open.value) {
      closeSidebar()
    } else {
      openDrawerFn?.()
    }
  }

  return {
    open,
    width,
    openSidebar,
    closeSidebar,
    toggleSidebar,
    setWidth,
    pinToSidebar,
    unpinToDrawer,
    registerOpenDrawer,
    openSessionTabBridge,
  }
}

/** Test hook — reset module state. */
export function _resetForTest() {
  initialized = false
  open.value = true
  width.value = SIDEBAR_DEFAULT_WIDTH
  openDrawerFn = null
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run web/src/composables/__tests__/useSessionSidebar.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/composables/useSessionSidebar.ts web/src/composables/__tests__/useSessionSidebar.test.ts
git commit -m "feat: 会话列表侧栏状态 composable（开关/宽度/持久化/桥接）"
```

---

### Task 2: `SessionListHeader.vue` + i18n

**Files:**
- Create: `web/src/components/session/SessionListHeader.vue`
- Test: `web/src/components/session/__tests__/SessionListHeader.test.ts`
- Modify: `web/src/i18n/locales/zh.ts`, `web/src/i18n/locales/en.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionListHeader from '@/components/session/SessionListHeader.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
}))

function mountHeader(props = {}) {
  return mount(SessionListHeader, {
    props: { sessionCount: 0, sessionMaxCount: 10, ...props },
  })
}

describe('SessionListHeader', () => {
  it('renders counter when maxCount > 0', () => {
    const wrapper = mountHeader({ sessionCount: 5, sessionMaxCount: 10 })
    expect(wrapper.find('.session-counter').exists()).toBe(true)
  })

  it('does not render counter when maxCount is 0', () => {
    const wrapper = mountHeader({ sessionMaxCount: 0 })
    expect(wrapper.find('.session-counter').exists()).toBe(false)
  })

  it('renders default action buttons (search + create)', async () => {
    const wrapper = mountHeader()
    const search = wrapper.find('.header-action-btn[data-action="search"]')
    const create = wrapper.find('.header-action-btn[data-action="create"]')
    expect(search.exists()).toBe(true)
    expect(create.exists()).toBe(true)
  })

  it('emits open-search when search clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="search"]').trigger('click')
    expect(wrapper.emitted('open-search')).toBeTruthy()
  })

  it('emits create when create clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="create"]').trigger('click')
    expect(wrapper.emitted('create')).toBeTruthy()
  })

  it('renders a leading extra button passed via slot', () => {
    const wrapper = mount(SessionListHeader, {
      props: { sessionCount: 0, sessionMaxCount: 0 },
      slots: { actions: '<button class="pin-stub" />' },
    })
    expect(wrapper.find('.pin-stub').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/session/__tests__/SessionListHeader.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write minimal implementation**

Create `web/src/components/session/SessionListHeader.vue`:

```vue
<template>
  <div class="bs-header session-list-header">
    <List :size="16" class="bs-header-icon" />
    <span class="bs-header-title">{{ t('session.title') }}</span>
    <div v-if="sessionMaxCount > 0" class="session-counter">
      <div class="session-counter-bar">
        <div class="session-counter-fill" :style="{ width: sessionPct + '%', background: sessionBarColor }"></div>
        <span class="session-counter-text">{{ sessionCount }}/{{ sessionMaxCount }}</span>
      </div>
    </div>
    <div class="session-header-actions">
      <slot name="actions" />
      <button class="header-action-btn" data-action="search" @click.stop="$emit('open-search')" :title="t('sessionSearch.title')">
        <Search :size="16" />
      </button>
      <button class="header-action-btn" data-action="create" @click.stop="$emit('create')" :title="t('session.newSession')">
        <Plus :size="16" />
      </button>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { List, Search, Plus } from 'lucide-vue-next'

const props = defineProps({
  sessionCount: { type: Number, default: 0 },
  sessionMaxCount: { type: Number, default: 0 },
})

defineEmits(['open-search', 'create'])

const { t } = useI18n()

const sessionPct = computed(() => props.sessionMaxCount > 0 ? Math.min((props.sessionCount / props.sessionMaxCount) * 100, 100) : 0)
const sessionBarColor = computed(() => {
  if (props.sessionCount >= props.sessionMaxCount && props.sessionMaxCount > 0) return '#ef4444'
  if (sessionPct.value >= 80) return '#f59e0b'
  return 'var(--accent-color, #0066cc)'
})
</script>

<style scoped>
.session-list-header {
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  box-shadow: none;
  cursor: default;
}
.session-counter {
  margin-left: auto;
  flex-shrink: 0;
}
.session-counter-bar {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 42px;
  height: 16px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
}
.session-counter-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: 8px;
  transition: width 0.3s ease, background 0.3s ease;
}
.session-counter-text {
  position: relative;
  z-index: 1;
  font-size: 9px;
  font-weight: 600;
  color: #fff;
  line-height: 1;
  letter-spacing: 0.3px;
  text-shadow: 0 0 2px rgba(0, 0, 0, 0.3);
}
.session-header-actions {
  display: inline-flex;
  align-items: center;
  flex-shrink: 0;
}
.header-action-btn {
  margin-left: 6px;
  width: 24px;
  height: 24px;
  border: none;
  background: none;
  color: var(--accent-color, #0066cc);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: background 0.15s;
}
.header-action-btn:hover {
  background: rgba(0, 102, 204, 0.1);
}
</style>
```

- [ ] **Step 4: Add i18n keys**

Add to `web/src/i18n/locales/zh.ts` under `session` block (line ~634, after `removeFailed`):

```ts
    pinToSidebar: '固定到侧栏',
    unpinToSidebar: '取消固定',
    closeSidebar: '关闭侧栏',
```

Add to `web/src/i18n/locales/en.ts` under `session` block:

```ts
    pinToSidebar: 'Pin to sidebar',
    unpinToSidebar: 'Unpin',
    closeSidebar: 'Close sidebar',
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `npx vitest run web/src/components/session/__tests__/SessionListHeader.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/session/SessionListHeader.vue web/src/components/session/__tests__/SessionListHeader.test.ts web/src/i18n/locales/zh.ts web/src/i18n/locales/en.ts
git commit -m "feat: 会话列表标题栏组件 + 图钉 i18n 文案"
```

---

### Task 3: `SessionList.vue`（从 SessionDrawer 抽取）

**Files:**
- Create: `web/src/components/session/SessionList.vue`
- Test: `web/src/components/session/__tests__/SessionList.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import SessionList from '@/components/session/SessionList.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
}))
vi.mock('@/composables/useLocale', () => ({
  useLocale: () => ({ currentLocale: { value: 'en' } }),
  gt: (key: string) => key,
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/stores/app', () => ({
  store: { state: { chatSessionPageSize: 10 } },
}))
const { mockGetAgentBackend, mockGetAgentName, mockDialogHolder, mockReconcileRunningSessions } = vi.hoisted(() => ({
  mockGetAgentBackend: vi.fn(() => ''),
  mockGetAgentName: vi.fn(() => 'Agent'),
  mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>), lastOptions: null as any },
  mockReconcileRunningSessions: vi.fn(),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: mockGetAgentBackend, getAgentName: mockGetAgentName }),
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: (m: string, o?: any) => { mockDialogHolder.lastOptions = o; return mockDialogHolder.confirm!(m, o) },
  }),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({ runningSessionsVersion: { value: 0 } }),
  reconcileRunningSessions: mockReconcileRunningSessions,
}))
vi.mock('@/utils/format', () => ({ formatRelativeTime: (d: string) => d || 'now' }))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: { name: 'ModalDialog', template: '<div class="modal-stub" />' },
}))
class MockIntersectionObserver {
  callback: any
  constructor(cb: any) { this.callback = cb }
  observe() {}
  disconnect() {}
  unobserve() {}
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

function sessionsFixture() {
  return {
    s1: { id: 's1', title: 'Session 1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli', model: 'gpt-4' },
    s2: { id: 's2', title: 'Session 2', updatedAt: '2025-01-02', agentId: 'agent-2', backend: 'acp' },
  }
}

describe('SessionList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    mockDialogHolder.lastOptions = null
  })

  async function mountList(props = {}) {
    const wrapper = mount(SessionList, {
      props: { currentSessionId: 's1', runningSessionIds: new Set(), ...props },
    })
    await flushPromises()
    return wrapper
  }

  it('renders sessions from API', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)
  })

  it('emits select with sessionId and backend', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.selectSession('s1', 'cli')
    expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli'])
  })

  it('marks running sessions from prop', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList({ runningSessionIds: new Set(['s1']) })
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessionsWithStatus[0].running).toBe(true)
  })

  it('emits archive after confirmation', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.archiveSession('s1')
    expect(wrapper.emitted('archive')).toBeTruthy()
  })

  it('emits destroy via dialog extra action', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    await wrapper.vm.archiveSession('s1')
    const onExtra = mockDialogHolder.lastOptions?.onExtraAction
    expect(typeof onExtra).toBe('function')
    onExtra()
    expect(wrapper.emitted('destroy')![0]).toEqual(['s1'])
  })

  it('loadMoreSessions appends sessions when hasMore', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: true }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.hasMore = true
    await wrapper.vm.loadMoreSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(2)
    expect(wrapper.vm.sessions[1].id).toBe('s2')
  })

  it('addSessionLocally prepends session', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    wrapper.vm.addSessionLocally({ id: 's9', title: 'S9', updatedAt: '2025-01-09', agentId: 'agent-1', backend: 'cli' })
    await nextTick()
    expect(wrapper.vm.sessions[0].id).toBe('s9')
  })

  it('handles fetch error gracefully', async () => {
    mockFetch.mockRejectedValue(new Error('network'))
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(0)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/session/__tests__/SessionList.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write minimal implementation**

Create `web/src/components/session/SessionList.vue`:

```vue
<template>
  <div class="session-list" ref="listRef">
    <LoadingIndicator v-if="loading" size="sm" :label="t('common.loading')" />
    <div v-else-if="sessions.length === 0" class="session-empty">{{ t('session.noSessions') }}</div>
    <template v-else>
      <div
        v-for="(session, idx) in sessionsWithStatus"
        :key="session.id"
        class="session-row"
        :class="{ active: session.id === currentSessionId, running: session.running, 'session-row-active': listNav.activeIndex.value === idx }"
      >
        <span v-if="session.running" class="session-running-line"></span>
        <div
          class="session-item"
          :class="{ active: session.id === currentSessionId }"
          @click="selectSession(session.id, session.backend)"
        >
          <span v-if="session.unreadCount > 0 || session.pendingApproval" class="session-item-badge"></span>
          <div class="session-item-info">
            <div class="session-item-header">
              <span class="session-item-title">{{ session.title }}</span>
            </div>
            <div class="session-item-meta">
              <span class="session-item-time">{{ formatRelativeTime(session.updatedAt) }}</span>
              <span class="session-item-agent"><AgentIcon :backend="getAgentBackend(session.agentId)" :name="getAgentName(session.agentId)" :size="12" /> {{ getAgentName(session.agentId) }}</span>
              <span v-if="session.model" class="session-item-model">{{ session.model }}</span>
            </div>
          </div>
        </div>
        <button class="session-archive-btn" :title="t('common.archive')" @click.stop="archiveSession(session.id)">
          <Archive :size="15" />
        </button>
      </div>
      <div ref="sentinelRef" class="session-list-sentinel"></div>
      <LoadingIndicator v-if="loadingMore" size="sm" inline :label="t('common.loading')" />
      <div v-else-if="!hasMore && sessions.length > 0" class="session-list-end"></div>
    </template>
  </div>
</template>

<script setup>
import { ref, watch, computed, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Archive } from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { useAgents } from '@/composables/useAgents'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { useDialog } from '@/composables/useDialog.ts'
import { useSessionIdentity, reconcileRunningSessions } from '@/composables/useSessionIdentity.ts'
import { formatRelativeTime } from '@/utils/format.ts'
import { store } from '@/stores/app.ts'

const props = defineProps({
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
})

const emit = defineEmits(['select', 'archive', 'destroy'])

const { t } = useI18n()
const { getAgentBackend, getAgentName } = useAgents()
const dialog = useDialog()
const { runningSessionsVersion } = useSessionIdentity()

const sessions = ref([])
const loading = ref(false)
const loadingMore = ref(false)
const hasMore = ref(false)
const listRef = ref(null)
const sentinelRef = ref(null)
let observer = null
const pageSize = computed(() => store.state.chatSessionPageSize || 10)

const sessionsWithStatus = computed(() => {
  void runningSessionsVersion.value
  return sessions.value.map(s => ({
    ...s,
    running: props.runningSessionIds.has(s.id),
  }))
})

async function loadSessions() {
  loading.value = true
  hasMore.value = false
  try {
    const resp = await fetch(`/api/ai/sessions?limit=${pageSize.value}`)
    const data = await resp.json()
    sessions.value = data.sessions || []
    reconcileRunningSessions(sessions.value)
    hasMore.value = !!data.hasMore
    if (typeof data.totalCount === 'number') store.state.sessionCount = data.totalCount
  } catch {
    sessions.value = []
  } finally {
    loading.value = false
    await nextTick()
    setupObserver()
  }
}

async function loadMoreSessions() {
  if (loadingMore.value || !hasMore.value) return
  loadingMore.value = true
  try {
    const last = sessions.value[sessions.value.length - 1]
    if (!last) return
    const resp = await fetch(`/api/ai/sessions?limit=${pageSize.value}&cursor=${encodeURIComponent(last.updatedAt)}&cursor_id=${encodeURIComponent(last.id)}`)
    const data = await resp.json()
    const more = data.sessions || []
    if (more.length > 0) sessions.value = [...sessions.value, ...more]
    hasMore.value = !!data.hasMore
  } catch {
    // ignore
  } finally {
    loadingMore.value = false
  }
}

function setupObserver() {
  if (observer) { observer.disconnect(); observer = null }
  if (!sentinelRef.value || !listRef.value) return
  observer = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && hasMore.value && !loadingMore.value) loadMoreSessions()
  }, { threshold: 0.1, rootMargin: '100px', root: listRef.value })
  observer.observe(sentinelRef.value)
}

function selectSession(sessionId, backend) {
  emit('select', sessionId, backend)
}

async function archiveSession(sessionId) {
  const isRunning = props.runningSessionIds.has(sessionId)
  const confirmMsg = isRunning ? t('session.confirmArchiveRunning') : t('session.confirmArchive')
  const confirmed = await dialog.confirm(confirmMsg, {
    dangerous: true,
    extraText: t('chat.archive.destroyBtn'),
    extraPrimedText: t('chat.archive.destroyBtnPrimed'),
    onExtraAction: () => emit('destroy', sessionId),
  })
  if (confirmed) {
    const session = sessions.value.find(s => s.id === sessionId)
    emit('archive', sessionId, session?.backend)
  }
}

function addSessionLocally(session) {
  if (!session) return
  if (sessions.value.some(s => s.id === session.id)) return
  sessions.value = [session, ...sessions.value]
}

const listNav = useListNav({
  getCount: () => sessionsWithStatus.value.length,
  onConfirm: (idx) => {
    const s = sessionsWithStatus.value[idx]
    if (s) selectSession(s.id, s.backend)
  },
  onActiveChange: scrollActiveIntoView,
})
useListKeys({ isOpen: () => props.isActive, nav: listNav })

function scrollActiveIntoView(index) {
  const items = document.querySelectorAll('.session-list .session-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
}

watch(sessionsWithStatus, () => listNav.reset())

defineExpose({ loadSessions, addSessionLocally })

onMounted(() => {
  loadSessions()
})
onUnmounted(() => {
  if (observer) { observer.disconnect(); observer = null }
})
</script>

<style scoped>
.session-list {
  overflow-y: auto;
  flex: 1;
  min-height: 0;
}
.session-empty {
  min-height: 40vh;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: 13px;
}
.session-item {
  position: relative;
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 0;
  min-height: 44px;
  padding: 10px 12px;
  border-top: 1px solid var(--border-color, #dee2e6);
  cursor: pointer;
}
.session-item.active {
  border-left: 4px solid var(--accent-color, #0066cc);
  padding-left: 8px;
}
.session-row.session-row-active {
  background: color-mix(in srgb, var(--text-primary) 6%, transparent);
  border-radius: 0;
}
.session-row.active { background: var(--accent-bg, rgba(0, 102, 204, 0.1)); }
.session-row.running { background: rgba(34, 197, 94, 0.05); }
.session-row.active.running {
  background: linear-gradient(135deg, rgba(0, 102, 204, 0.08), rgba(34, 197, 94, 0.1));
}
@media (hover: hover) {
  .session-row:hover { background: color-mix(in srgb, var(--text-primary) 6%, transparent); }
  .session-row.active.running:hover { background: color-mix(in srgb, var(--text-primary) 8%, transparent); }
}
.session-item-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}
.session-item-header {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
}
.session-item-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.session-item-title {
  font-size: 13px;
  color: var(--text-primary, #1a1a1a);
  font-weight: 500;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-item.active .session-item-title { color: var(--accent-color, #0066cc); }
.session-item-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--accent-color, #0066cc);
}
.session-running-line {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 1px;
  overflow: hidden;
}
.session-running-line::after {
  content: '';
  position: absolute;
  top: 0;
  left: -40%;
  width: 40%;
  height: 100%;
  background: linear-gradient(90deg, transparent, #22c55e, transparent);
  animation: scan-line 2s ease-in-out infinite;
}
@keyframes scan-line {
  0% { left: -40%; }
  100% { left: 100%; }
}
.session-row { display: flex; align-items: stretch; position: relative; }
.session-archive-btn {
  flex-shrink: 0;
  width: 34px;
  border: none;
  background: transparent;
  color: var(--text-muted, #999);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  border-top: 1px solid var(--border-color, #dee2e6);
  transition: background 0.15s, color 0.15s;
}
@media (hover: hover) {
  .session-archive-btn:hover { color: var(--accent-color, #0066cc); }
}
.session-archive-btn:active { color: var(--accent-color, #0066cc); }
.session-item-time { font-size: 11px; color: var(--text-muted, #999); }
.session-item-agent {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-secondary, #495057);
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
.session-item-model {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted, #999);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-list-sentinel { height: 1px; }
.session-list-end { height: 0; }
</style>
```

> Note: `useListKeys` needs an `isActive` prop. The `SessionList` must receive `isActive` from its parent (drawer/sidebar). **Add** `isActive: Boolean` to props and to the parent usages in Tasks 4-5.

- [ ] **Step 4: Add `isActive` prop to SessionList**

The component above uses `props.isActive`. Add it to the `defineProps`:

```ts
const props = defineProps({
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
})
```

- [ ] **Step 5: Run test to verify it passes**

Run: `npx vitest run web/src/components/session/__tests__/SessionList.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/session/SessionList.vue web/src/components/session/__tests__/SessionList.test.ts
git commit -m "feat: 抽取可复用会话列表组件 SessionList"
```

---

### Task 4: `SessionSidebar.vue`（含拖拽调宽）

**Files:**
- Create: `web/src/components/session/SessionSidebar.vue`
- Test: `web/src/components/session/__tests__/SessionSidebar.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionSidebar from '@/components/session/SessionSidebar.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }) }))
vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

vi.mock('@/components/session/SessionList.vue', () => ({
  default: { name: 'SessionList', template: '<div class="session-list-stub" />' },
}))
vi.mock('@/components/session/SessionListHeader.vue', () => ({
  default: { name: 'SessionListHeader', template: '<div class="header-stub" />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div />' },
}))
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: { name: 'ModalDialog', template: '<div />' },
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(true) }),
}))

describe('SessionSidebar', () => {
  beforeEach(() => { vi.clearAllMocks() })

  function mountSidebar(props = {}) {
    return mount(SessionSidebar, {
      props: { width: 280, currentSessionId: 's1', runningSessionIds: new Set(), ...props },
    })
  }

  it('renders header and list', () => {
    const wrapper = mountSidebar()
    expect(wrapper.find('.session-list-stub').exists()).toBe(true)
    expect(wrapper.find('.header-stub').exists()).toBe(true)
  })

  it('emits unpin when pin button clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('.sidebar-unpin-btn').trigger('click')
    expect(wrapper.emitted('unpin')).toBeTruthy()
  })

  it('emits close when close button clicked', async () => {
    const wrapper = mountSidebar()
    await wrapper.find('.sidebar-close-btn').trigger('click')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('emits resize on pointer drag with clamped width', () => {
    const wrapper = mountSidebar()
    const div = wrapper.find('.sidebar-divider')
    // Simulate divider pointerdown then window pointermove
    div.trigger('pointerdown', { button: 0, clientX: 600 })
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointerup'))
    expect(wrapper.emitted('resize')).toBeTruthy()
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/session/__tests__/SessionSidebar.test.ts`
Expected: FAIL (module not found).

- [ ] **Step 3: Write minimal implementation**

Create `web/src/components/session/SessionSidebar.vue`:

```vue
<template>
  <div class="session-sidebar" :style="{ width: `${width}px` }">
    <div
      ref="dividerRef"
      class="sidebar-divider"
      role="separator"
      aria-orientation="vertical"
      @pointerdown="onDividerPointerDown"
    />
    <div class="sidebar-inner">
      <SessionListHeader
        :session-count="sessionCount"
        :session-max-count="sessionMaxCount"
        @open-search="$emit('open-session-search')"
        @create="handleCreateClick"
      >
        <template #actions>
          <button class="header-action-btn sidebar-unpin-btn" @click.stop="$emit('unpin')" :title="t('session.unpinToSidebar')">
            <Pin :size="16" />
          </button>
          <button class="header-action-btn sidebar-close-btn" @click.stop="$emit('close')" :title="t('session.closeSidebar')">
            <PanelLeftClose :size="16" />
          </button>
        </template>
      </SessionListHeader>
      <SessionList
        ref="listRef"
        :current-session-id="currentSessionId"
        :running-session-ids="runningSessionIds"
        :is-active="isActive"
        @select="$emit('select', $event[0], $event[1])"
        @archive="handleArchive"
        @destroy="$emit('destroy', $event)"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pin, PanelLeftClose } from 'lucide-vue-next'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import { useAgents } from '@/composables/useAgents'
import { SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'
import { store } from '@/stores/app.ts'

const props = defineProps({
  width: { type: Number, default: 280 },
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  isActive: { type: Boolean, default: true },
})

const emit = defineEmits(['select', 'archive', 'destroy', 'unpin', 'close', 'resize', 'open-session-search', 'create'])

const { t } = useI18n()
const { agents, loadAgents } = useAgents()

const dividerRef = ref(null)
const listRef = ref(null)
let dragging = false

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

function clampWidth(w) {
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, w))
}

function onDividerPointerDown(e) {
  if (e.button !== 0) return
  dragging = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('session-sidebar-dragging')
}

function onPointerMove(e) {
  if (!dragging) return
  const root = document.querySelector('.session-sidebar')
  const rect = root?.getBoundingClientRect()
  if (!rect) return
  // Sidebar grows to the right; divider sits at its left edge.
  // Width = distance from sidebar's right edge to the pointer.
  const rightEdge = rect.right
  const newWidth = rightEdge - e.clientX
  emit('resize', clampWidth(newWidth))
}

function onPointerUp(e) {
  if (!dragging) return
  dragging = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('session-sidebar-dragging')
}

async function handleCreateClick() {
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
  } else {
    emit('create-agent-select')
  }
}

function handleArchive(sessionId) {
  emit('archive', sessionId)
}

onMounted(() => {
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})
onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  document.body.classList.remove('session-sidebar-dragging')
})

defineExpose({ loadSessions: () => listRef.value?.loadSessions(), addSessionLocally: (s) => listRef.value?.addSessionLocally(s) })
</script>

<style scoped>
.session-sidebar {
  position: relative;
  flex-shrink: 0;
  height: 100%;
  display: flex;
  background: var(--bg-secondary, #fff);
  border-left: 1px solid var(--border-color, #e5e5e5);
}
.sidebar-inner {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.sidebar-divider {
  position: relative;
  flex: 0 0 auto;
  width: 1px;
  margin: 0;
  cursor: col-resize;
  touch-action: none;
  z-index: 2;
  transition: width 0.15s ease, background 0.15s ease;
}
.sidebar-divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -6px;
  right: -6px;
}
.sidebar-divider:hover,
.sidebar-divider:active {
  width: 12px;
  margin: 0 -5.5px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
:global(body.session-sidebar-dragging) {
  user-select: none;
  cursor: col-resize;
}
</style>
```

> Note: `handleCreateClick` uses `agents` (a ref) and `loadAgents` from `useAgents`, matching the original `SessionDrawer` usage. The `create-agent-select` event is forwarded to App.vue to open the agent selector — App.vue must add a `@create-agent-select` handler (open the existing `AgentSelectorDrawer`/`sessionIdentity.openAgentSelector()`). If multi-agent selection in the sidebar is out of scope for this plan, forward `create-agent-select` to `sessionIdentity.openAgentSelector()` in App.vue.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run web/src/components/session/__tests__/SessionSidebar.test.ts`
Expected: PASS (mock SessionListHeader covers header emits; SessionList mocked so its own deps don't load).

- [ ] **Step 5: Commit**

```bash
git add web/src/components/session/SessionSidebar.vue web/src/components/session/__tests__/SessionSidebar.test.ts
git commit -m "feat: 会话列表右侧可拖拽侧栏组件 SessionSidebar"
```

---

### Task 5: 改造 `SessionDrawer.vue`（抽取组件 + 图钉）

**Files:**
- Modify: `web/src/components/session/SessionDrawer.vue`
- Test: `web/src/components/session/__tests__/SessionDrawer.test.ts`

- [ ] **Step 1: Update the failing test** (replace the `header actions` describe + add pin coverage)

In `web/src/components/session/__tests__/SessionDrawer.test.ts`, add a `pin` test and update `header actions` to reflect new button order. Also mock the new child components `SessionList` and `SessionListHeader` (add near the existing BottomSheet/ModalDialog mocks):

```ts
vi.mock('@/components/session/SessionList.vue', () => ({
  default: { name: 'SessionList', template: '<div class="session-list-stub" />', methods: { loadSessions: vi.fn(), addSessionLocally: vi.fn() } },
}))
vi.mock('@/components/session/SessionListHeader.vue', () => ({
  default: { name: 'SessionListHeader', template: '<div class="header-stub"><slot name="actions" /></div>' },
}))
vi.mock('@/composables/useWideScreenLayout', () => ({
  useWideScreenLayout: () => ({ isWideScreen: { value: true } }),
  getWideScreenState: () => ({ isWideScreen: { value: true } }),
}))
```

Add a new describe block near `header actions`:

```ts
describe('pin button', () => {
  it('emits pin when the pin button is clicked (wide screen)', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const pin = wrapper.find('.header-action-btn[data-action="pin"]')
    expect(pin.exists()).toBe(true)
    await pin.trigger('click')
    expect(wrapper.emitted('pin')).toBeTruthy()
  })

  it('does not render pin button on narrow screen', async () => {
    const { useWideScreenLayout } = await import('@/composables/useWideScreenLayout')
    ;(useWideScreenLayout() as any).isWideScreen.value = false
    const wrapper = mountDrawer()
    await flushPromises()
    expect(wrapper.find('.header-action-btn[data-action="pin"]').exists()).toBe(false)
  })
})
```

Update the existing `header actions` test's button indices: search button is now `[0]` only if no pin. Since the pin is in the header actions slot and the header test uses the drawer's own header, adjust:

```ts
it('emits open-session-search when the search button is clicked', async () => {
  const wrapper = mountDrawer()
  await flushPromises()
  const btn = wrapper.findAll('.header-action-btn').find(b => b.attributes('data-action') === 'search')
  await btn!.trigger('click')
  expect(wrapper.emitted('open-session-search')).toBeTruthy()
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/session/__tests__/SessionDrawer.test.ts`
Expected: FAIL (new pin button not present).

- [ ] **Step 3: Rewrite `SessionDrawer.vue` to use extracted components**

Replace the template header slot and list body with the new components. Keep the AgentSelectorDrawer + agent-loading logic. Full new file:

```vue
<template>
  <BottomSheet ref="bottomSheetRef" :open="open" auto :title="t('session.title')" @close="$emit('close')">
    <template #header>
      <SessionListHeader
        :session-count="sessionCount"
        :session-max-count="sessionMaxCount"
        @open-search="$emit('open-session-search')"
        @create="handleCreateClick"
      >
        <template #actions>
          <button v-if="isWideScreen" class="header-action-btn" data-action="pin" @click.stop="$emit('pin')" :title="t('session.pinToSidebar')">
            <Pin :size="16" />
          </button>
        </template>
      </SessionListHeader>
    </template>

    <SessionList
      ref="listRef"
      :current-session-id="currentSessionId"
      :running-session-ids="runningSessionIds"
      :is-active="open"
      @select="handleSelect"
      @archive="handleArchive"
      @destroy="$emit('destroy', $event)"
    />
  </BottomSheet>

  <!-- Agent selector drawer -->
  <AgentSelectorDrawer
    ref="agentSelectorRef"
    :open="agentSelectorDrawer.effectiveOpen.value"
    :title="t('session.selectAgent')"
    :default-badge="t('chat.sessionSetting.defaultBadge')"
    :set-default-title="t('session.setAsDefaultAgent')"
    @update:open="v => v ? agentSelectorDrawer.open() : agentSelectorDrawer.close()"
    @select="createSession"
  />
</template>

<script setup>
import { ref, watch, computed, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Pin } from 'lucide-vue-next'
import BottomSheet from '@/components/common/BottomSheet.vue'
import SessionList from '@/components/session/SessionList.vue'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useDialog } from '@/composables/useDialog.ts'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { store } from '@/stores/app.ts'

const { t } = useI18n()
const props = defineProps({
  open: Boolean,
  currentSessionId: String,
  runningSessionIds: { type: Set, default: () => new Set() },
  currentAgentId: String,
})

const emit = defineEmits(['close', 'select', 'create', 'archive', 'destroy', 'open-session-search', 'pin'])

const { isWideScreen } = useWideScreenLayout()

const bottomSheetRef = ref(null)
const agentSelectorRef = ref(null)
const listRef = ref(null)
const { loadAgents } = useAgents()
const agentSelectorDrawer = useTabDrawer('chat', { autoRestore: false })

const sessionCount = computed(() => store.state.sessionCount)
const sessionMaxCount = computed(() => store.state.sessionMaxCount)

defineExpose({ openAgentSelector, addSessionLocally })

async function openAgentSelector() {
  const { agents } = useAgents()
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

async function handleCreateClick() {
  const { agents } = useAgents()
  await loadAgents()
  if (agents.value.length === 1) {
    emit('create', agents.value[0].id)
    bottomSheetRef.value?.close()
    return
  }
  agentSelectorDrawer.open()
}

function createSession(agentId) {
  agentSelectorDrawer.close()
  emit('create', agentId)
  bottomSheetRef.value?.close()
}

function handleSelect(sessionId, backend) {
  emit('select', sessionId, backend)
  bottomSheetRef.value?.close()
}

function handleArchive(sessionId) {
  emit('archive', sessionId)
}

function addSessionLocally(session) {
  listRef.value?.addSessionLocally(session)
}

watch(() => props.open, async (val) => {
  if (val) {
    await Promise.all([loadAgents(), listRef.value?.loadSessions()])
  }
})
watch(() => store.state.sessionCount, async () => {
  if (props.open) listRef.value?.loadSessions()
})

onUnmounted(() => {})
</script>
```

> Note: The `archive` confirmation dialog was moved into `SessionList`. `SessionDrawer` now just forwards `archive`. The `runningSessionIds`/`runningSessionsVersion`/`sessionBarColor`/`sessionPct` computed moved to `SessionList`/`SessionListHeader`. Update tests that referenced removed internals (`sessions`, `loadSessions`, `sessionsWithStatus`, `sessionBarColor`, `loadMoreSessions`) to instead verify through the stubbed `SessionList` (or move those to `SessionList.test.ts`).

- [ ] **Step 4: Update the test file to match new internals**

The existing `SessionDrawer.test.ts` has many tests referencing removed `wrapper.vm.sessions`/`loadSessions`/`sessionsWithStatus`/`sessionBarColor`/`loadMoreSessions`/`addSessionLocally`/`selectSession`/`archiveSession`/`openAgentSelector`. These behaviors are now owned by `SessionList` (covered in Task 3). Rewrite `SessionDrawer.test.ts` to focus on drawer-specific concerns: rendering shell, header actions (search/create/pin), emit forwarding (select/archive/destroy/create), agent selector flow, and open-watcher reload. Keep the component mocks.

- [ ] **Step 5: Run test to verify it passes**

Run: `npx vitest run web/src/components/session/__tests__/SessionDrawer.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/session/SessionDrawer.vue web/src/components/session/__tests__/SessionDrawer.test.ts
git commit -m "refactor: SessionDrawer 抽取 SessionList/Header 并增加图钉按钮"
```

---

### Task 6: `ChatInputBar` 会话按钮按侧栏状态隐藏

**Files:**
- Modify: `web/src/components/chat/ChatInputBar.vue`
- Test: `web/src/components/chat/__tests__/ChatInputBar.test.ts`

- [ ] **Step 1: Update the failing test**

Add to `web/src/components/chat/__tests__/ChatInputBar.test.ts`:

```ts
it('hides the session button when sessionPanelOpen is true', () => {
  const wrapper = mountChatInputBar({ sessionPanelOpen: true })
  const btn = wrapper.findAll('.chat-action-btn').find(b => b.attributes('data-action') === 'session')
  expect(btn?.exists()).toBe(false)
})

it('shows the session button when sessionPanelOpen is false', () => {
  const wrapper = mountChatInputBar({ sessionPanelOpen: false })
  const btn = wrapper.findAll('.chat-action-btn').find(b => b.attributes('data-action') === 'session')
  expect(btn?.exists()).toBe(true)
})
```

(Adapt `mountChatInputBar` helper to accept extra props.)

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run web/src/components/chat/__tests__/ChatInputBar.test.ts`
Expected: FAIL (button not hidden).

- [ ] **Step 3: Implement**

In `web/src/components/chat/ChatInputBar.vue`:

Add prop (in the `defineProps` block, after `active`):

```js
  sessionPanelOpen: Boolean,
```

Update the session button (currently around line 9-13). Add `data-action="session"`, `v-show="!sessionPanelOpen"`, and `@click` — keep existing `@click`:

```html
<button class="chat-action-btn" data-action="session" v-show="!sessionPanelOpen"
  :class="{ 'has-unread': chatUnreadCount > 0, 'has-running': chatRunning }"
  @click="$emit('open-session-tab', 'sessions')"
  :title="t('chat.actions.session')">
  <List :size="14" />
</button>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run web/src/components/chat/__tests__/ChatInputBar.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/chat/ChatInputBar.vue web/src/components/chat/__tests__/ChatInputBar.test.ts
git commit -m "feat: 会话按钮按侧栏打开状态隐藏"
```

---

### Task 7: `ChatPanelContent` 透传 + App.vue 集成

**Files:**
- Modify: `web/src/components/chat/ChatPanelContent.vue`
- Modify: `web/src/App.vue`

- [ ] **Step 1: `ChatPanelContent.vue` — add prop + pass to ChatInputBar**

Add prop after `currentDir: String` in the `defineProps` block:

```js
  sessionSidebarOpen: Boolean,
```

Pass to `ChatInputBar` (in the template, add near `:active`):

```html
:session-panel-open="sessionSidebarOpen"
```

- [ ] **Step 2: `App.vue` — wire the sidebar + pin + bridge**

Import `SessionSidebar` and `useSessionSidebar`:

```js
import SessionSidebar from './components/session/SessionSidebar.vue'
import { useSessionSidebar } from './composables/useSessionSidebar.ts'
```

Initialize after `sessionIdentity` is defined (near line 753):

```js
const sessionSidebar = useSessionSidebar()
// Bridge: openSessionTab routes through the sidebar state.
// The drawer's open/close lives in sessionIdentity.sessionDrawer.
sessionSidebar.registerOpenDrawer(() => sessionIdentity.sessionDrawer.open())
```

In `<template #right>` (the `col-right` div, line ~220), after the `TabPanel`, insert the sidebar. Also make `col-right` a flex row so the chat panel flexes and the sidebar sits beside it:

```html
<div class="col-right" v-show="isWideScreen || activeTab === 'chat'" :class="{ 'chat-drop-active': chatDropActive }" ...>
  <div class="col-right-chat">
    <!-- Chat Tab -->
    <TabPanel tabId="chat" :activeTab="chatActive">
      <template #header>...</template>
      <ChatPanelContent ... :session-sidebar-open="sessionSidebar.open.value" ... />
    </TabPanel>
    <div v-if="chatDropActive" class="chat-drop-hint">...</div>
  </div>
  <SessionSidebar
    v-show="sessionSidebar.open.value && isWideScreen"
    :width="sessionSidebar.width.value"
    :current-session-id="sessionIdentity.currentSessionId.value"
    :running-session-ids="sessionIdentity.runningSessions.value"
    :is-active="isWideScreen"
    @resize="sessionSidebar.setWidth"
    @unpin="sessionSidebar.unpinToDrawer"
    @close="sessionSidebar.closeSidebar"
    @select="handleSessionSelect"
    @create="handleSessionCreate"
    @archive="handleSessionArchive"
    @destroy="handleSessionDestroy"
    @open-session-search="sessionSearchDrawer.open()"
    @create-agent-select="sessionIdentity.openAgentSelector"
  />
```

Add CSS for `col-right` flex layout. Update the `.col-left, .col-right` rule (line ~2460):

```css
.col-left,
.col-right {
  position: relative;
  height: 100%;
}
.col-right {
  display: flex;
  flex-direction: row;
}
.col-right-chat {
  position: relative;
  flex: 1;
  min-width: 0;
  height: 100%;
}
```

Update the `SessionDrawer` element (line ~275) to add `@pin`:

```html
<SessionDrawer
  ref="sessionDrawerRef"
  :open="sessionIdentity.sessionDrawer.effectiveOpen.value"
  :currentSessionId="sessionIdentity.currentSessionId.value"
  :runningSessionIds="sessionIdentity.runningSessions.value"
  :currentAgentId="sessionIdentity.currentAgentId.value"
  @close="sessionIdentity.sessionDrawer.close()"
  @select="handleSessionSelect"
  @create="handleSessionCreate"
  @archive="handleSessionArchive"
  @destroy="handleSessionDestroy"
  @open-session-search="sessionSearchDrawer.open()"
  @pin="handleDrawerPin"
/>
```

Add handler (near the other session handlers):

```js
function handleDrawerPin() {
  sessionIdentity.sessionDrawer.close()
  sessionSidebar.pinToSidebar()
}
```

The `handleSessionCreate` function currently does `sessionDrawerRef.value?.addSessionLocally(...)`. Since `addSessionLocally` is now exposed via `SessionDrawer` → `SessionList`, keep `sessionDrawerRef.value.addSessionLocally` and also add to the sidebar:

```js
async function handleSessionCreate(agentId) {
  await sessionIdentity.createSession(agentId)
  if (sessionDrawerRef.value && sessionIdentity.sessionDrawer.isOpen.value) {
    sessionDrawerRef.value.addSessionLocally({ ... })
  }
  sessionSidebar.addSessionLocally({
    id: sessionIdentity.currentSessionId.value,
    title: sessionIdentity.currentSessionTitle.value || '',
    backend: sessionIdentity.currentBackend.value || '',
    agentId: sessionIdentity.currentAgentId.value || '',
    model: sessionIdentity.currentModelName.value || '',
    updatedAt: new Date().toISOString(),
    unreadCount: 0,
  })
  sessionIdentity.sessionDrawer.close()
}
```

> Note: `useSessionSidebar` must expose `addSessionLocally` — this is implemented in Task 8. The sidebar's `SessionList` `addSessionLocally` is reached via the `sessionSidebarRef` bridge registered in Task 8.

- [ ] **Step 3: Run build/typecheck**

Run: `npx vue-tsc --noEmit -p web/tsconfig.json` (or the project's typecheck script).
Expected: no type errors.

- [ ] **Step 4: Run related tests**

Run: `npx vitest run web/src/components/chat/__tests__/ChatPanelContent.test.ts web/src/components/chat/__tests__/ChatInputBar.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/chat/ChatPanelContent.vue web/src/App.vue
git commit -m "feat: App 集成会话侧栏与图钉桥接"
```

---

### Task 8: 完善 `useSessionSidebar` 的 `addSessionLocally` 桥接

**Files:**
- Modify: `web/src/composables/useSessionSidebar.ts`
- Test: `web/src/composables/__tests__/useSessionSidebar.test.ts`

- [ ] **Step 1: Add failing test**

```ts
it('delegates addSessionLocally to registered callback', () => {
  const s = useSessionSidebar()
  const fn = vi.fn()
  s.registerAddSessionLocally(fn)
  s.addSessionLocally({ id: 'x' })
  expect(fn).toHaveBeenCalledWith({ id: 'x' })
})
```

- [ ] **Step 2: Run to verify failure**

Run: `npx vitest run web/src/composables/__tests__/useSessionSidebar.test.ts`
Expected: FAIL.

- [ ] **Step 3: Implement**

Add to `useSessionSidebar.ts`:

```ts
let addLocallyFn: ((session: any) => void) | null = null
...
function registerAddSessionLocally(fn: (session: any) => void) {
  addLocallyFn = fn
}
function addSessionLocally(session: any) {
  addLocallyFn?.(session)
}
```

Add to the returned object: `registerAddSessionLocally`, `addSessionLocally`. Reset in `_resetForTest` (`addLocallyFn = null`).

- [ ] **Step 4: Wire in App.vue**

In App.vue after mounting, bind the sidebar's list ref. Since the sidebar is `v-show` (always mounted when wide), its `addSessionLocally` delegating to `SessionList` ref works. In `handleSessionCreate`, use `sessionSidebar.addSessionLocally({...})` directly (replacing the `sessionSidebar.addSessionLocally?.` in Task 7). Register via App.vue once:

```js
// after SessionSidebar ref is available
watch(() => sessionSidebarRef.value, (ref) => {
  if (ref) {
    sessionSidebar.registerAddSessionLocally((s) => ref.addSessionLocally(s))
  }
}, { immediate: true })
```

Add `sessionSidebarRef` to the `<SessionSidebar ref="sessionSidebarRef" ...>`.

- [ ] **Step 5: Run tests**

Run: `npx vitest run web/src/composables/__tests__/useSessionSidebar.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/composables/useSessionSidebar.ts web/src/composables/__tests__/useSessionSidebar.test.ts web/src/App.vue
git commit -m "feat: 侧栏 addSessionLocally 桥接"
```

---

### Task 9: 全量验证

- [ ] **Step 1: Run all frontend tests**

Run: `npx vitest run`
Expected: all pass (including new SessionList, SessionListHeader, SessionSidebar, useSessionSidebar tests).

- [ ] **Step 2: Run typecheck**

Run: `npx vue-tsc --noEmit -p web/tsconfig.json` (or the repo's typecheck command from `package.json`).
Expected: no errors.

- [ ] **Step 3: Run lint**

Run: the repo's lint script (e.g. `npm run lint`).
Expected: clean.

- [ ] **Step 4: Manual smoke test**

Start dev server (`./dev-server.sh --fg`), open a PC-width window:
- Right of chat: session sidebar visible by default; width drag-resizable; state persists across refresh.
- Drawer pin → closes drawer, opens sidebar.
- Sidebar pin (Pin) → closes sidebar, opens drawer.
- Sidebar X → closes sidebar.
- Sidebar open → chat input "session" button hidden; Ctrl+K collapses sidebar instead of opening drawer.

---

## 自审

**Spec coverage:**
- 抽取 SessionList/SessionListHeader ✓ (Task 2-3)
- 侧栏 + 拖拽调宽 ✓ (Task 4)
- 抽屉标题栏图钉 ✓ (Task 5)
- 侧栏图钉/关闭 ✓ (Task 4)
- 持久化宽度/开关 ✓ (Task 1, Task 4 width via App.vue)
- 默认打开 ✓ (Task 1 `open=true`)
- 仅 PC 宽屏 ✓ (SessionDrawer 图钉 `v-if isWideScreen`; App.vue 侧栏 `v-show ... && isWideScreen`)
- 侧栏打开禁用抽屉：隐藏会话按钮 + 桥接 openSessionTab ✓ (Task 6, 7)
- 立即切换：pin → 关抽屉+开侧栏 ✓ (Task 7 handleDrawerPin)
- 两处图钉 ✓ (Task 4, 5)
- 测试 ✓ (各 Task)

**Type consistency:** `registerOpenDrawer`/`openSessionTabBridge`/`pinToSidebar`/`unpinToDrawer`/`setWidth`/`addSessionLocally`/`registerAddSessionLocally` consistent across Tasks 1, 7, 8. `SessionSidebar` props (`width`, `currentSessionId`, `runningSessionIds`, `isActive`) match App.vue usage. `SessionList` props (`currentSessionId`, `runningSessionIds`, `isActive`) match drawer/sidebar usage. `SessionListHeader` props (`sessionCount`, `sessionMaxCount`) match.

**Placeholder scan:** No TBD/TODO. Task 4's `handleCreateClick`/`getAgents` has a verification note — the engineer must confirm `useAgents` exposes `agents` and `loadAgents` (it does, per SessionDrawer original) and use `agents.value` there. Task 7 has a note about `addSessionLocally` resolved by Task 8.
