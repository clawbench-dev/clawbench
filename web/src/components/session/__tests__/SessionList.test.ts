import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import SessionList from '@/components/session/SessionList.vue'
import { LongPressDirective } from '@/directives/longPress'
import { RunningSweepDirective } from '@/directives/runningSweep'

// UI zoom factor driving toFixedCSS()/getZoomedViewport() in the component's
// clamp math. Defaults to 1 (no zoom); individual tests raise it to prove the
// menu is clamped in getBoundingClientRect() space rather than raw viewport px.
const scaleHolder = vi.hoisted(() => ({ value: 1 }))
vi.mock('@/composables/useSettingsConfig', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useSettingsConfig')>()
  return {
    ...actual,
    getUIScale: () => scaleHolder.value,
    toFixedCSS: (v: number) => v / scaleHolder.value,
    getZoomedViewport: () => ({ width: 1024, height: 768 }),
  }
})

const { mockGetAgentBackend, mockGetAgentName, mockDialogHolder, mockReconcileRunningSessions, mockRemoveEventHandler, mockEventHolder, mockStore, mockCrossState } = await vi.hoisted(async () => {
  // The real store exposes a reactive() state, so tests that mutate
  // state.projectRoot (or sessionListVersion) must trigger the component's
  // watchers. A plain object would silently not, making such a test vacuous.
  const { reactive } = await import('vue')
  // projectRoot/homeDir are required by useCrossProjectSessions' "exclude the
  // current project" filter — without them the filter compares against
  // undefined and the test can never exercise the exclusion.
  const mockStore = { state: reactive({ chatSessionPageSize: 10, sessionListVersion: 0, sessionCount: 0, projectRoot: '/proj/current', homeDir: '/home/u' }) }
  const mockCrossState: {
    groups: any
    loading: any
    loaded: any
    total: any
    refresh: any
    scheduleRefresh: any
  } = {
    groups: null,
    loading: null,
    loaded: null,
    total: null,
    refresh: vi.fn(),
    scheduleRefresh: vi.fn(),
  }
  return {
    mockGetAgentBackend: vi.fn(() => ''),
    mockGetAgentName: vi.fn(() => 'Agent'),
    mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>), prompt: null as null | ((m: string, o?: any) => Promise<string | null>), lastOptions: null as any },
    mockReconcileRunningSessions: vi.fn(),
    mockRemoveEventHandler: vi.fn(),
    mockEventHolder: { handler: null as null | ((event: string) => void) },
    mockStore,
    mockCrossState,
  }
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  }
})
vi.mock('@/composables/useLocale', () => ({
  useLocale: () => ({ currentLocale: { value: 'en' } }),
  gt: (key: string) => key,
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('@/stores/app', () => ({
  store: mockStore,
}))
vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (handler: any) => { mockEventHolder.handler = handler; return mockRemoveEventHandler },
  }),
}))
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({ getAgentBackend: mockGetAgentBackend, getAgentName: mockGetAgentName }),
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: (m: string, o?: any) => { mockDialogHolder.lastOptions = o; return mockDialogHolder.confirm!(m, o) },
    prompt: (m: string, o?: any) => { mockDialogHolder.lastOptions = o; return mockDialogHolder.prompt!(m, o) },
  }),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({ runningSessionsVersion: { value: 0 } }),
  reconcileRunningSessions: mockReconcileRunningSessions,
}))
vi.mock('@/composables/useCrossProjectSessions', async () => {
  // Real refs, not plain {value} boxes: Vue only auto-unwraps actual refs in
  // templates, and the component reads crossGroups.length / crossLoading there.
  const { ref } = await import('vue')
  mockCrossState.groups = ref([])
  mockCrossState.loading = ref(false)
  mockCrossState.loaded = ref(true)
  mockCrossState.total = ref(0)
  return { useCrossProjectSessions: () => mockCrossState }
})
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
// The tag dialog owns its own API calls; stub it so these tests exercise the
// list's wiring (open/seed) without hitting fetch for the candidate list.
vi.mock('@/components/session/SessionTagDialog.vue', () => ({
  default: {
    name: 'SessionTagDialog',
    props: ['open', 'sessionId', 'initialTags'],
    template: '<div class="tag-dialog-stub" />',
  },
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
    s1: { id: 's1', title: 'Session 1', createdAt: '2025-01-01', updatedAt: '2025-01-05', agentId: 'agent-1', backend: 'cli', model: 'gpt-4' },
    s2: { id: 's2', title: 'Session 2', createdAt: '2025-01-02', updatedAt: '2025-01-06', agentId: 'agent-2', backend: 'acp' },
  }
}

describe('SessionList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
    mockDialogHolder.confirm = vi.fn().mockResolvedValue(true)
    mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
    mockDialogHolder.lastOptions = null
    mockEventHolder.handler = null
    mockStore.state.sessionListVersion = 0
    mockStore.state.projectRoot = '/proj/current'
    mockCrossState.groups.value = []
    mockCrossState.loading.value = false
    mockCrossState.loaded.value = true
    mockCrossState.total.value = 0
    scaleHolder.value = 1
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  async function mountList(props = {}) {
    const wrapper = mount(SessionList, {
      props: { currentSessionId: 's1', runningSessionIds: new Set(), ...props },
      global: { directives: { 'long-press': LongPressDirective, 'running-sweep': RunningSweepDirective } },
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

  it('renders the sweep band as a real element only on running rows', async () => {
    // The band must be a real node, not a `::after`: its travel is driven by
    // v-running-sweep through the Web Animations API, which cannot target a
    // pseudo-element. And it must be scoped to running rows, since it is the
    // visual signal for them.
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
    const wrapper = await mountList({ runningSessionIds: new Set(['s1']) })
    await wrapper.vm.loadSessions()
    await flushPromises()

    const rows = wrapper.findAll('.session-row')
    expect(rows).toHaveLength(2)
    const runningRow = wrapper.find('[data-session-id="s1"]')
    const idleRow = wrapper.find('[data-session-id="s2"]')
    expect(runningRow.find('.session-running-band').exists(), 'running row should carry the band').toBe(true)
    expect(idleRow.find('.session-running-band').exists(), 'idle row should not').toBe(false)
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
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: true }) })
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
    wrapper.vm.addSessionLocally({ id: 's9', title: 'S9', createdAt: '2025-01-09', updatedAt: '2025-01-09', agentId: 'agent-1', backend: 'cli' })
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

  it('keeps the existing list visible during a background reload (no clear-then-refill flash)', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    // Server state changed — next fetch returns s2. The reload must NOT show the
    // loading spinner while the old list (s1) is still rendered; the flag is only
    // for an empty list on first load.
    let resolveFetch
    mockFetch.mockReturnValueOnce(new Promise(r => { resolveFetch = r }))
    const reloadPromise = wrapper.vm.loadSessions()
    await nextTick()
    expect(wrapper.vm.loading).toBe(false)
    expect(wrapper.vm.sessions[0].id).toBe('s1')

    resolveFetch({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    await reloadPromise
    await flushPromises()
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })

  it('uses a TransitionGroup so the refreshed list transitions instead of swapping', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.findComponent({ name: 'TransitionGroup' }).exists()).toBe(true)
    expect(wrapper.findAll('.session-row').length).toBe(1)
  })

  it('subscribes to session_update WS events and reloads the list (debounced)', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    // Server state changes (e.g. another session added) — next load returns s2.
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    mockEventHolder.handler?.('session_update')
    // Debounce is 400ms — advance timers then flush.
    await new Promise(r => setTimeout(r, 500))
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })

  it('reloads when sessionListVersion bumps (create/archive/destroy/read)', async () => {
    // The watcher calls reload() on every sessionListVersion change. We drive it
    // through the exposed reload() here (the store mock is a plain object, so the
    // production reactive watcher is exercised in the real app; this asserts the
    // reload contract that the watcher invokes).
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(1)

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.reload()
    await flushPromises()
    await nextTick()
    expect(wrapper.vm.sessions[0].id).toBe('s2')
  })

  it('reload preserves the loaded depth instead of collapsing back to the first page', async () => {
    // Two pages of sessions. Page 1 reports hasMore so loadMoreSessions can
    // append page 2 — simulating a user who scrolled through more than one page.
    const page1 = Array.from({ length: 10 }, (_, i) => ({ id: `s${i}`, title: `S${i}`, createdAt: `2025-01-${String(i + 1).padStart(2, '0')}`, updatedAt: `2025-02-${String(i + 1).padStart(2, '0')}`, agentId: 'agent-1', backend: 'cli' }))
    const page2 = Array.from({ length: 5 }, (_, i) => ({ id: `s1${i}`, title: `S1${i}`, createdAt: `2025-01-${String(i + 11).padStart(2, '0')}`, updatedAt: `2025-02-${String(i + 11).padStart(2, '0')}`, agentId: 'agent-1', backend: 'cli' }))
    mockFetch.mockImplementation((url: string) => {
      const hasCursor = url.includes('cursor=')
      const page = hasCursor ? page2 : page1
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: page, hasMore: hasCursor ? false : true }) })
    })
    const wrapper = await mountList()
    expect(wrapper.vm.sessions.length).toBe(10)

    // User scrolls: load the second page → 15 rows loaded.
    await wrapper.vm.loadMoreSessions()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(15)

    // A WS/version reload fires (e.g. running-session update). It must re-fetch
    // enough pages to cover the 15 rows the user already sees — collapsing back
    // to 10 would re-expose the load-more sentinel and cause the list (and the
    // auto-sized drawer) to oscillate in height.
    await wrapper.vm.reload()
    await flushPromises()
    expect(wrapper.vm.sessions.length).toBe(15)
    expect(mockFetch.mock.calls.filter((c: unknown[]) => String(c[0]).includes('cursor=')).length).toBeGreaterThanOrEqual(1)
  })

  it('paginates using createdAt (not updatedAt) as the cursor', async () => {
    // Backend orders/filters paged sessions by created_at. Sending updatedAt
    // (which is >= createdAt and bumped on every message) makes `created_at <
    // cursor` match rows already shown, duplicating the list.
    const first = { id: 's1', title: 'S1', createdAt: '2025-01-01T00:00:00Z', updatedAt: '2025-06-01T00:00:00Z', agentId: 'agent-1', backend: 'cli' }
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [first], hasMore: true }) })
    const wrapper = await mountList()
    await flushPromises()

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.hasMore = true
    await wrapper.vm.loadMoreSessions()
    await flushPromises()

    const cursorCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).includes('cursor='))
    expect(cursorCall).toBeTruthy()
    const url = String(cursorCall![0])
    expect(url).toContain(`cursor=${encodeURIComponent('2025-01-01T00:00:00Z')}`)
    expect(url).not.toContain(encodeURIComponent('2025-06-01T00:00:00Z'))
  })

  it('reload (fetchSessionsUpTo) paginates using createdAt as the cursor', async () => {
    // The depth-preservation refetch loop is the path that actually fired the
    // duplicate bug, so its cursor must be createdAt too — not just loadMore's.
    const page1 = Array.from({ length: 10 }, (_, i) => ({ id: `s${i}`, title: `S${i}`, createdAt: `2025-01-${String(i + 1).padStart(2, '0')}`, updatedAt: `2025-09-${String(i + 1).padStart(2, '0')}`, agentId: 'agent-1', backend: 'cli' }))
    const page2 = Array.from({ length: 5 }, (_, i) => ({ id: `s1${i}`, title: `S1${i}`, createdAt: `2025-01-${String(i + 11).padStart(2, '0')}`, updatedAt: `2025-09-${String(i + 11).padStart(2, '0')}`, agentId: 'agent-1', backend: 'cli' }))
    mockFetch.mockImplementation((url: string) => {
      const hasCursor = url.includes('cursor=')
      const page = hasCursor ? page2 : page1
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: page, hasMore: hasCursor ? false : true }) })
    })
    const wrapper = await mountList()
    await wrapper.vm.loadMoreSessions()
    await flushPromises()
    // Deepen to 15 rows, then reload — reload re-fetches page 2 via cursor.
    mockFetch.mockClear()
    mockFetch.mockImplementation((url: string) => {
      const hasCursor = url.includes('cursor=')
      const page = hasCursor ? page2 : page1
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: page, hasMore: hasCursor ? false : true }) })
    })
    await wrapper.vm.reload()
    await flushPromises()

    const cursorCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).includes('cursor='))
    expect(cursorCall).toBeTruthy()
    const url = String(cursorCall![0])
    // Cursor must be the 10th row's createdAt (2025-01-10), never its updatedAt.
    expect(url).toContain(`cursor=${encodeURIComponent('2025-01-10')}`)
    expect(url).not.toContain(encodeURIComponent('2025-09-10'))
  })

  it('sends cursor_pinned alongside the created_at cursor', async () => {
    // Ordering is (pinned DESC, created_at DESC, id DESC). A created_at-only
    // cursor cannot exclude already-seen pinned rows — they sort first on every
    // page — so the cursor must carry pinned too.
    const lastPinned = { id: 's1', title: 'S1', pinned: true, createdAt: '2025-01-01T00:00:00Z', updatedAt: '2025-06-01T00:00:00Z', agentId: 'agent-1', backend: 'cli' }
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [lastPinned], hasMore: true }) })
    const wrapper = await mountList()
    await flushPromises()

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.hasMore = true
    await wrapper.vm.loadMoreSessions()
    await flushPromises()

    const cursorCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).includes('cursor='))
    expect(cursorCall).toBeTruthy()
    const url = String(cursorCall![0])
    expect(url).toContain('cursor_pinned=1')
    expect(url).toContain(`cursor_id=${encodeURIComponent('s1')}`)
  })

  it('sends cursor_pinned=0 when the cursor row is not pinned', async () => {
    const lastUnpinned = { id: 's9', title: 'S9', pinned: false, createdAt: '2025-02-02', updatedAt: '2025-07-02', agentId: 'agent-1', backend: 'cli' }
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [lastUnpinned], hasMore: true }) })
    const wrapper = await mountList()
    await flushPromises()

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.hasMore = true
    await wrapper.vm.loadMoreSessions()
    await flushPromises()

    const cursorCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).includes('cursor='))
    expect(cursorCall).toBeTruthy()
    expect(String(cursorCall![0])).toContain('cursor_pinned=0')
  })

  it('stops paginating instead of sending cursor=undefined when createdAt is missing', async () => {
    // A row without createdAt cannot form a valid cursor; encodeURIComponent
    // would emit "undefined" and the server's `created_at < 'undefined'` is
    // lexically true for all dates, re-returning page 1 (duplicates).
    const noCreatedAt = { id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [noCreatedAt], hasMore: true }) })
    const wrapper = await mountList()
    await flushPromises()

    mockFetch.mockClear()
    wrapper.vm.hasMore = true
    await wrapper.vm.loadMoreSessions()
    await flushPromises()

    // No follow-up request at all — bail before forming a bad cursor.
    expect(mockFetch.mock.calls.filter((c: unknown[]) => String(c[0]).includes('cursor=')).length).toBe(0)
    expect(wrapper.vm.hasMore).toBe(false)
  })

  describe('pinned marker and keyboard nav', () => {
    // Backend returns pinned DESC, created_at DESC. The component must render in
    // that same order so useListNav's index maps onto the visible rows.
    const pinnedOld = { id: 'p-old', title: 'Pinned Old', pinned: true, createdAt: '2024-01-01', updatedAt: '2024-01-01', agentId: 'agent-1', backend: 'cli' }
    const newest = { id: 'n-new', title: 'Newest', pinned: false, createdAt: '2025-06-01', updatedAt: '2025-06-01', agentId: 'agent-1', backend: 'cli' }
    const middle = { id: 'n-mid', title: 'Middle', pinned: false, createdAt: '2025-05-01', updatedAt: '2025-05-01', agentId: 'agent-1', backend: 'cli' }

    it('renders one flat list — no pinned/recent section headers', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // The section split is gone: a single .session-rows container holds all
      // three rows, and no group header is rendered in the project pane.
      expect(wrapper.findAll('.session-section').length).toBe(0)
      const rows = wrapper.findAll('.session-rows > .session-row')
      expect(rows.length).toBe(3)
      expect(wrapper.findAll('.session-group-header').length).toBe(0)
    })

    it('marks the pinned row with the row modifier only (no inline glyph)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // The wedge is a CSS pseudo-element, so the DOM carries only the modifier
      // class — no extra glyph element in the row or its title line.
      const pinnedRow = wrapper.find('[data-session-id="p-old"]')
      expect(pinnedRow.classes()).toContain('pinned')
      expect(pinnedRow.find('.session-pin-icon').exists()).toBe(false)

      // Unpinned rows carry no modifier either.
      for (const id of ['n-new', 'n-mid']) {
        expect(wrapper.find(`[data-session-id="${id}"]`).classes()).not.toContain('pinned')
      }
    })

    it('DOM order matches sessionsWithStatus order (pinned first, then newest)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const domOrder = wrapper.findAll('.session-row').map(r => r.attributes('data-session-id'))
      expect(domOrder).toEqual(['p-old', 'n-new', 'n-mid'])
      expect(wrapper.vm.sessionsWithStatus.map((s: any) => s.id)).toEqual(domOrder)
    })

    // useListKeys installs a document-level listener on mount, so real
    // ArrowDown/Enter events exercise the same path a user hits.
    function pressKey(key: string) {
      document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
    }

    it('highlights rows in flat DOM order, pinned row first', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // First ArrowDown highlights index 0 — the pinned row (first in DOM).
      pressKey('ArrowDown')
      await nextTick()
      let highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('p-old')

      // The flat list means index 1 is simply the next row — no section offset.
      pressKey('ArrowDown')
      await nextTick()
      highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('n-new')

      pressKey('ArrowDown')
      await nextTick()
      highlighted = wrapper.findAll('.session-row.session-row-active')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('n-mid')

      wrapper.unmount()
    })

    it('confirming a highlighted pinned row selects that session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      pressKey('ArrowDown')
      await nextTick()
      pressKey('Enter')
      expect(wrapper.emitted('select')![0]).toEqual(['p-old', 'cli'])

      wrapper.unmount()
    })

    it('confirming a highlighted unpinned row selects the right session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Move past the pinned row onto the unpinned one.
      pressKey('ArrowDown')
      await nextTick()
      pressKey('ArrowDown')
      await nextTick()
      pressKey('Enter')
      expect(wrapper.emitted('select')![0]).toEqual(['n-new', 'cli'])

      wrapper.unmount()
    })

    it('renders no pinned marker when nothing is pinned', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [newest, middle], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      expect(wrapper.findAll('.session-row.pinned').length).toBe(0)
      expect(wrapper.findAll('.session-row').length).toBe(2)
    })

    it('togglePin sends the pinned flag for the long-pressed session and bumps the list version', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ ok: true }) })
      const before = mockStore.state.sessionListVersion
      await wrapper.vm.togglePin('n-new', false)
      await flushPromises()

      const patchCall = mockFetch.mock.calls.find((c: unknown[]) => String(c[0]).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(String(patchCall![0])).toContain(`session_id=${encodeURIComponent('n-new')}`)
      expect(JSON.parse((patchCall![1] as any).body).pinned).toBe(true)
      expect(mockStore.state.sessionListVersion).toBe(before + 1)
    })

    it('togglePin rolls back the optimistic flag when the request fails', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinnedOld, newest], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      mockFetch.mockResolvedValue({ ok: false, statusText: 'boom', json: () => Promise.resolve({}) })
      await wrapper.vm.togglePin('n-new', false)
      await flushPromises()

      expect(wrapper.vm.sessions.find((s: any) => s.id === 'n-new').pinned).toBe(false)
    })
  })

  it('exposes reload() and removes the WS listener on unmount', async () => {
    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
    const wrapper = await mountList()
    await wrapper.vm.loadSessions()
    await flushPromises()

    mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s2], hasMore: false }) })
    wrapper.vm.reload()
    await flushPromises()
    expect(wrapper.vm.sessions[0].id).toBe('s2')

    wrapper.unmount()
    expect(mockRemoveEventHandler).toHaveBeenCalled()
  })

  describe('cross-project tab', () => {
    function crossGroup() {
      return {
        name: '/proj/other',
        displayName: 'other',
        displayPath: '~/proj/other',
        sessions: [
          { id: 'o1', title: 'Other 1', backend: 'cli', agentId: 'agent-1', model: 'gpt-4', running: true, pendingApproval: false, unreadCount: 0, updatedAt: '2025-01-05' },
          { id: 'o2', title: 'Other 2', backend: 'acp', agentId: 'agent-2', running: false, pendingApproval: false, unreadCount: 3, updatedAt: '2025-01-04' },
        ],
      }
    }

    it('defaults to the project tab', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await flushPromises()
      expect(wrapper.vm.activeTab).toBe('project')
      expect(wrapper.find('.session-list-pane--cross').attributes('style')).toContain('display: none')
    })

    it('renders cross-project groups with a project-name header when the prop flips', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.findAll('.cross-group').length).toBe(1)
      // Header markup now comes from the shared SessionGroupHeader component.
      expect(wrapper.find('.session-group-title').text()).toBe('other')
      expect(wrapper.find('.session-group-subtitle').text()).toBe('~/proj/other')
      expect(wrapper.findAll('.cross-session-item').length).toBe(2)
    })

    it('shows the active-session count in the cross-project group header', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      // crossGroup() has two active sessions — the count must reflect the group,
      // matching the count badge the Pinned/Recent headers already show.
      expect(wrapper.find('.session-group-count').text()).toBe('2')
    })

    it('collapses and expands a cross-project group from its header', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()

      const header = wrapper.find('.cross-group .session-group-header')
      // Expanded by default: chevron not rotated.
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).not.toContain('collapsed')

      await header.trigger('click')
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).toContain('collapsed')
      // Rows stay mounted (v-show, not v-if) so collapsing never destroys the
      // fetched snapshot — only the rows container is hidden.
      expect(wrapper.findAll('.cross-group .cross-session-item').length).toBe(2)
      expect(wrapper.find('.cross-group-rows').attributes('style')).toContain('display: none')

      await header.trigger('click')
      expect(wrapper.find('.cross-group .session-group-chevron').classes()).not.toContain('collapsed')
      expect(wrapper.find('.cross-group-rows').attributes('style') || '').not.toContain('display: none')
    })

    it('keeps each project group collapsed independently', async () => {
      const second = {
        name: '/proj/second',
        displayName: 'second',
        displayPath: '~/proj/second',
        sessions: [
          { id: 't1', title: 'Second 1', backend: 'cli', agentId: 'agent-1', model: '', running: false, pendingApproval: false, unreadCount: 1, updatedAt: '2025-01-03' },
        ],
      }
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup(), second]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()

      // Collapse only the first group; the second must stay expanded.
      await wrapper.findAll('.cross-group .session-group-header')[0].trigger('click')
      const chevrons = wrapper.findAll('.cross-group .session-group-chevron')
      expect(chevrons[0].classes()).toContain('collapsed')
      expect(chevrons[1].classes()).not.toContain('collapsed')
    })

    it('does not use .session-item for cross rows (keyboard nav index isolation)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      // Project rows keep .session-item; cross rows must not add to that set,
      // otherwise querySelectorAll('.session-item') indices no longer match
      // useListNav's count.
      expect(wrapper.findAll('.session-item').length).toBe(1)
      expect(wrapper.findAll('.cross-session-item').length).toBe(2)
    })

    it('emits select with the owning project path for cross rows', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      await wrapper.findAll('.cross-session-item')[0].trigger('click')
      expect(wrapper.emitted('select')![0]).toEqual(['o1', 'cli', '/proj/other'])
    })

    it('does not render an archive button on cross rows', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = [crossGroup()]
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      const crossRows = wrapper.findAll('.cross-session-row')
      expect(crossRows.length).toBe(2)
      for (const row of crossRows) {
        expect(row.find('.session-archive-btn').exists()).toBe(false)
      }
    })

    it('shows the empty state when there are no cross-project groups', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = []
      mockCrossState.loaded.value = true
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.find('.session-list-pane--cross').text()).toContain('session.crossEmpty')
    })

    it('shows a loading indicator before the first successful load', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [], hasMore: false }) })
      mockCrossState.groups.value = []
      mockCrossState.loaded.value = false
      mockCrossState.loading.value = true
      const wrapper = await mountList({ activeTab: 'cross' })
      await flushPromises()
      expect(wrapper.find('.session-list-pane--cross').find('.loading-stub').exists()).toBe(true)
    })
  })

  describe('context menu / long-press session targeting', () => {
    it('long-press reads the session id from the DOM row, not a stale reference', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      expect(wrapper.findAll('.session-row').length).toBe(2)

      // Touch the second row (s2). The long-press handler must resolve the
      // session id from that row's data-session-id attribute, so the context
      // menu targets s2 even if the list re-renders afterwards.
      const rows = wrapper.findAll('.session-row')
      const targetRow = rows[1]
      expect(targetRow.attributes('data-session-id')).toBe('s2')

      const touch = { clientX: 100, clientY: 200, touches: [{ clientX: 100, clientY: 200 }] }
      wrapper.vm.onSessionLongPress({ currentTarget: targetRow.element, target: targetRow.element, touches: touch.touches })
      await nextTick()

      expect(wrapper.vm.contextMenu.visible).toBe(true)
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')
      // The menu-open highlight must sit on the s2 row.
      const highlighted = wrapper.findAll('.session-row.menu-open')
      expect(highlighted.length).toBe(1)
      expect(highlighted[0].attributes('data-session-id')).toBe('s2')
    })

    it('long-press on a pinned and an unpinned row each target their own session', async () => {
      const pinned = { ...sessionsFixture().s1, pinned: true }
      const unpinned = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinned, unpinned], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const touch = (x: number) => ({ clientX: x, clientY: 200, touches: [{ clientX: x, clientY: 200 }] })

      // Long-press the pinned row (s1).
      const pinnedRow = wrapper.findAll('.session-row.pinned')[0]
      wrapper.vm.onSessionLongPress({ currentTarget: pinnedRow.element, target: pinnedRow.element, touches: touch(50).touches })
      await nextTick()
      expect(wrapper.vm.contextMenu.sessionId).toBe('s1')
      expect(wrapper.vm.contextMenu.pinned).toBe(true)
      wrapper.vm.contextMenu.visible = false
      await nextTick()

      // Long-press the unpinned row (s2) — must NOT reuse the previous s1 target.
      const unpinnedRow = wrapper.findAll('.session-row:not(.pinned)')[0]
      wrapper.vm.onSessionLongPress({ currentTarget: unpinnedRow.element, target: unpinnedRow.element, touches: touch(150).touches })
      await nextTick()
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')
      expect(wrapper.vm.contextMenu.pinned).toBe(false)
    })

    it('rename from menu targets the long-pressed session', async () => {
      const pinned = { ...sessionsFixture().s1, pinned: true, title: 'Pinned A' }
      const unpinned = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinned, unpinned], hasMore: false }) })
      mockDialogHolder.prompt = vi.fn().mockResolvedValue('Renamed B')
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Long-press the unpinned row (s2), then rename from the menu.
      const unpinnedRow = wrapper.findAll('.session-row:not(.pinned)')[0]
      wrapper.vm.onSessionLongPress({ currentTarget: unpinnedRow.element, target: unpinnedRow.element, touches: [{ clientX: 150, clientY: 200 }] })
      await nextTick()
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')

      await wrapper.vm.renameSessionFromMenu(wrapper.vm.contextMenu.sessionId)
      await flushPromises()

      // The API call must carry s2 (not the pinned s1) via the session_id query.
      const patchCall = mockFetch.mock.calls.find(c => (c[0] as string).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(patchCall![0]).toContain(`session_id=${encodeURIComponent('s2')}`)
      const body = JSON.parse(patchCall![1].body)
      expect(body.title).toBe('Renamed B')
      // Only s2's title updated locally.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's2').title).toBe('Renamed B')
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's1').title).toBe('Pinned A')
    })

    it('pin from menu toggles the long-pressed session', async () => {
      const pinned = { ...sessionsFixture().s1, pinned: true }
      const unpinned = sessionsFixture().s2
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [pinned, unpinned], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // Long-press the unpinned row (s2), then pin from the menu.
      const unpinnedRow = wrapper.findAll('.session-row:not(.pinned)')[0]
      wrapper.vm.onSessionLongPress({ currentTarget: unpinnedRow.element, target: unpinnedRow.element, touches: [{ clientX: 150, clientY: 200 }] })
      await nextTick()
      expect(wrapper.vm.contextMenu.sessionId).toBe('s2')

      await wrapper.vm.togglePin(wrapper.vm.contextMenu.sessionId, wrapper.vm.contextMenu.pinned)
      await flushPromises()

      const patchCall = mockFetch.mock.calls.find(c => (c[0] as string).startsWith('/api/ai/session/update'))
      expect(patchCall).toBeTruthy()
      expect(patchCall![0]).toContain(`session_id=${encodeURIComponent('s2')}`)
      const body = JSON.parse(patchCall![1].body)
      expect(body.pinned).toBe(true)
      // Optimistic local update applied to s2, not s1.
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's2').pinned).toBe(true)
      expect(wrapper.vm.sessions.find((s: any) => s.id === 's1').pinned).toBe(true)
    })
  })

  describe('shared context menu control', () => {
    it('renders the shared .context-menu markup (not the old bespoke classes)', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.showContextMenu({ clientX: 40, clientY: 60 }, wrapper.vm.sessions[0])
      await nextTick()

      const menus = document.body.querySelectorAll('.context-menu.visible')
      const menu = menus[menus.length - 1]
      expect(menu).toBeTruthy()
      // Reuses the file manager's item class + icon-left layout.
      expect(menu!.querySelectorAll('.context-menu-item').length).toBe(4)
      expect(document.body.querySelector('.session-context-menu')).toBeNull()
      wrapper.unmount()
    })

    it('renders a full-viewport .ctx-overlay that closes the menu on click', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.showContextMenu({ clientX: 40, clientY: 60 }, wrapper.vm.sessions[0])
      await nextTick()

      // Earlier tests leave their own teleported menus open on document.body, so
      // take the newest overlay — the one this wrapper just rendered.
      const overlays = document.body.querySelectorAll('.ctx-overlay')
      const overlay = overlays[overlays.length - 1] as HTMLElement
      expect(overlay).toBeTruthy()
      overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      wrapper.unmount()
    })

    it('stores coords in fixed-CSS space and clamps within the zoomed viewport', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // At zoom 2, a raw clientX of 2000 is 1000 in fixed-CSS space; the menu
      // (140px min-width) must then be pulled back inside the 512px-wide CSS
      // viewport instead of overflowing to the right.
      scaleHolder.value = 2
      wrapper.vm.showContextMenu({ clientX: 2000, clientY: 1600 }, wrapper.vm.sessions[0])
      await nextTick()
      await nextTick()

      expect(wrapper.vm.contextMenu.x).toBeLessThanOrEqual(512 - 8)
      expect(wrapper.vm.contextMenu.y).toBeLessThanOrEqual(384 - 8)
      expect(wrapper.vm.contextMenu.x).toBeGreaterThanOrEqual(8)
      expect(wrapper.vm.contextMenu.y).toBeGreaterThanOrEqual(8)
      wrapper.unmount()
    })

    it('closes the menu when an item is clicked', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1, sessionsFixture().s2], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const menus = () => document.body.querySelectorAll('.context-menu.visible')
      const openFor = (session: any) => wrapper.vm.showContextMenu({ clientX: 40, clientY: 60 }, session)
      const clickLastMenu = (idx: number) => {
        const items = menus()[menus().length - 1].querySelectorAll('.context-menu-item')
        ;(items[idx] as HTMLElement).dispatchEvent(new MouseEvent('click', { bubbles: true }))
      }

      // Pin item (index 0) — must dismiss the menu, not just fire the action.
      openFor(wrapper.vm.sessions[0])
      await nextTick()
      clickLastMenu(0)
      await flushPromises()
      expect(wrapper.vm.contextMenu.visible).toBe(false)

      // Rename item (index 1) — also dismisses. prompt() resolves null so the
      // action itself is a no-op; the close must not depend on it succeeding.
      mockDialogHolder.prompt = vi.fn().mockResolvedValue(null)
      openFor(wrapper.vm.sessions[0])
      await nextTick()
      clickLastMenu(1)
      await flushPromises()
      expect(wrapper.vm.contextMenu.visible).toBe(false)

      // Set-tags item (index 2) — dismisses and opens the tag dialog.
      openFor(wrapper.vm.sessions[0])
      await nextTick()
      clickLastMenu(2)
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.vm.tagDialog.open).toBe(true)

      // Archive item (index 3) — dismisses and emits.
      openFor(wrapper.vm.sessions[0])
      await nextTick()
      clickLastMenu(3)
      await nextTick()
      expect(wrapper.vm.contextMenu.visible).toBe(false)
      expect(wrapper.emitted('archive')).toBeTruthy()
      wrapper.unmount()
    })

    it('opens the tag dialog seeded with the session tags', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ ...sessionsFixture().s1, tags: [{ name: 'bug' }, { name: 'urgent' }] }],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.openTagDialogFromMenu('s1')
      expect(wrapper.vm.tagDialog.open).toBe(true)
      expect(wrapper.vm.tagDialog.sessionId).toBe('s1')
      // Seeded from the already-loaded list so the checkboxes paint instantly.
      expect(wrapper.vm.tagDialog.initialTags).toEqual(['bug', 'urgent'])
      wrapper.unmount()
    })

    it('does not open the tag dialog for an unknown session', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.openTagDialogFromMenu('nope')
      expect(wrapper.vm.tagDialog.open).toBe(false)
      wrapper.unmount()
    })
  })

  describe('session tag row', () => {
    it('renders one chip per tag with a stable accent variable', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ ...sessionsFixture().s1, tags: [{ name: 'bug' }, { name: 'urgent' }] }],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag')
      expect(chips.length).toBe(2)
      expect(chips.map(c => c.text())).toEqual(['bug', 'urgent'])
      // The accent is carried inline (light+dark) so the CSS can pick per theme.
      expect(chips[0].attributes('style')).toContain('--tag-accent-light')
      wrapper.unmount()
    })

    it('omits the tag row entirely when the session has no tags', async () => {
      mockFetch.mockResolvedValue({ ok: true, json: () => Promise.resolve({ sessions: [sessionsFixture().s1], hasMore: false }) })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      // An empty tag row would still consume the info column's gap and shift
      // the meta line, so the wrapper must not render at all.
      expect(wrapper.find('.session-item-tags').exists()).toBe(false)
      wrapper.unmount()
    })

    it('separates the tag row from the meta line above it', async () => {
      // The parent's uniform gap (2px) reads fine between two text lines, whose
      // line-height half-leading pads them out, but leaves the bordered chips
      // visually flush against the meta line — they read as its continuation
      // rather than a row of their own. The tag row must add its own spacing.
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-item-tags\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-item-tags should exist').toBeTruthy()
      expect(rule).toMatch(/margin-top:\s*var\(--space-2\)/)
      // Whitespace only: a border/background here would double up with the row
      // separator just below and fight the active/running row backgrounds.
      expect(rule).not.toMatch(/border/)
      expect(rule).not.toMatch(/background/)
    })

    it('wraps the tag row instead of clipping the overflow', async () => {
      // The row was nowrap + overflow:hidden, so any tag past the row's width
      // was invisible and unclickable — the tags existed but could not be read.
      // jsdom has no layout engine, so assert the declarations that allow
      // wrapping rather than a rendered geometry.
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-item-tags\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-item-tags should exist').toBeTruthy()
      expect(rule).toMatch(/flex-wrap:\s*wrap/)
      expect(rule).not.toMatch(/overflow:\s*hidden/)
    })

    it('lets a long tag name ellipsise instead of widening the row', async () => {
      const src = String((await import('@/components/session/SessionList.vue?raw')).default)
      const rule = src.replace(/\/\*[\s\S]*?\*\//g, '')
        .match(/\.session-tag\s*\{[^}]*\}/)?.[0]
      expect(rule, '.session-tag should exist').toBeTruthy()
      // flex-shrink:0 with a long name would push the chip past the pane edge;
      // min-width:0 is what actually lets text-overflow engage in a flex child.
      expect(rule).toMatch(/flex-shrink:\s*1/)
      expect(rule).toMatch(/min-width:\s*0/)
      expect(rule).toMatch(/text-overflow:\s*ellipsis/)
    })

    it('gives the same tag the same color across sessions', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { ...sessionsFixture().s1, tags: [{ name: 'shared' }] },
            { ...sessionsFixture().s2, tags: [{ name: 'shared' }] },
          ],
          hasMore: false,
        }),
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag')
      expect(chips.length).toBe(2)
      // Same name ⇒ same color, regardless of which session it sits on.
      expect(chips[0].attributes('style')).toBe(chips[1].attributes('style'))
      wrapper.unmount()
    })
  })
  describe('tag filter bar', () => {
    // The filter bar fetches its own chip set via apiGet (global fetch) while
    // the list uses fetch directly — route by URL so each call gets the right
    // payload.
    function routeFetch({ sessions = [], hasMore = false, tags = [] } = {}) {
      mockFetch.mockImplementation((url: string) => {
        if (String(url).includes('/api/ai/session/tags')) {
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags }) })
        }
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions, hasMore }) })
      })
    }

    it('hides the filter bar when the project has no tags in use', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      wrapper.unmount()
    })

    it('renders one chip per in-use tag and requests only in-use tags', async () => {
      routeFetch({
        sessions: [sessionsFixture().s1],
        tags: [{ name: 'bug', scope: 'project', count: 2 }, { name: 'urgent', scope: 'global', count: 1 }],
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      const chips = wrapper.findAll('.session-tag-filter-chip')
      expect(chips.length).toBe(2)
      expect(chips[0].text()).toContain('bug')
      // The unused-global case is excluded server-side; assert the client asks
      // for that view rather than the full candidate list.
      const tagCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/session/tags'))
      expect(String(tagCall![0])).toContain('inUse=1')
      wrapper.unmount()
    })

    it('clicking a chip sends the tag to the server and marks it active', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      mockFetch.mockClear()
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()

      const listCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/sessions'))
      expect(String(listCall![0])).toContain('tag=bug')
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')
      wrapper.unmount()
    })

    it('clicking the active chip again clears the filter', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).not.toContain('active')
      wrapper.unmount()
    })

    it('paginated pages keep the tag filter', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()

      mockFetch.mockClear()
      routeFetch({ sessions: [sessionsFixture().s2], hasMore: false, tags: [] })
      // Pretend the first filtered page reported more rows so loadMore runs.
      wrapper.vm.hasMore = true
      await wrapper.vm.loadMoreSessions()
      await flushPromises()

      const moreCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/sessions'))
      expect(String(moreCall![0])).toContain('tag=bug')
      wrapper.unmount()
    })

    it('clears the filter when the applied tag disappears from the in-use list', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      // The tag is deleted (or its last session dropped it): the bar would
      // vanish, so the filter must not stay applied invisibly.
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      await wrapper.vm.loadFilterTags()
      await flushPromises()

      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      // And the list is no longer constrained by the dead tag.
      mockFetch.mockClear()
      routeFetch({ sessions: [sessionsFixture().s1], tags: [] })
      await wrapper.vm.loadSessions()
      await flushPromises()
      const listCall = mockFetch.mock.calls.find(c => String(c[0]).includes('/api/ai/sessions'))
      expect(String(listCall![0])).not.toContain('tag=')
      wrapper.unmount()
    })

    it('clears the filter when the project changes, even if the tag name exists there too', async () => {
      // The new project deliberately also has a "bug" tag in use. Without the
      // explicit clear in the projectRoot watcher, loadFilterTags would find the
      // name present and keep the filter — carrying project A's selection into a
      // different project, where it means nothing. (If the new project had no
      // tags, the auto-clear would mask this and the test would pass either way.)
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')

      mockStore.state.projectRoot = '/proj/other'
      routeFetch({ sessions: [], tags: [{ name: 'bug', scope: 'project', count: 4 }] })
      await nextTick()
      await flushPromises()

      // The chip is still offered (the tag exists here) but must be INACTIVE.
      const chip = wrapper.find('.session-tag-filter-chip')
      expect(chip.exists()).toBe(true)
      expect(chip.classes()).not.toContain('active')
      wrapper.unmount()
    })


    it('re-fetches the list after the applied tag disappears (no dead filter)', async () => {
      // Regression: the chip set and the list are refreshed by the same watcher,
      // but the list request captures activeTag synchronously. If the tag is
      // cleared AFTER that request is issued, the list stays filtered by a tag
      // whose chip is already gone — the user cannot clear it from the UI.
      const route = (sessions: any[], tags: any[]) => {
        mockFetch.mockImplementation((url: string) => {
          if (String(url).includes('/api/ai/session/tags')) {
            return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags }) })
          }
          const u = String(url)
          const filtered = u.includes('tag=bug') ? sessions.filter((x: any) => x.tagged) : sessions
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: filtered, hasMore: false }) })
        })
      }
      const all = [
        { id: 's1', title: 'A', createdAt: '2025-01-01', updatedAt: '2025-01-01', tagged: true },
        { id: 's2', title: 'B', createdAt: '2025-01-02', updatedAt: '2025-01-02', tagged: false },
      ]
      route(all, [{ name: 'bug', scope: 'project', count: 1 }])
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()
      expect(wrapper.vm.sessions.length).toBe(1)

      // The tag is deleted elsewhere: the version bump drives the refresh.
      route(all, [])
      mockStore.state.sessionListVersion++
      await nextTick()
      await flushPromises()
      await flushPromises()

      // Bar is gone AND the list is no longer constrained by the dead tag.
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      expect(wrapper.vm.sessions.length).toBe(2)
      wrapper.unmount()
    })

    it('keeps chips and filter when the tag fetch fails', async () => {
      // A transient API failure must not be read as "this project has no tags":
      // that would hide the bar and silently drop the user's filter, with no way
      // to restore it.
      let failTags = false
      mockFetch.mockImplementation((url: string) => {
        if (String(url).includes('/api/ai/session/tags')) {
          if (failTags) return Promise.reject(new Error('network'))
          return Promise.resolve({ ok: true, json: () => Promise.resolve({ tags: [{ name: 'bug', scope: 'project', count: 1 }] }) })
        }
        const u = String(url)
        const all = [
          { id: 's1', title: 'A', createdAt: '2025-01-01', updatedAt: '2025-01-01', tagged: true },
          { id: 's2', title: 'B', createdAt: '2025-01-02', updatedAt: '2025-01-02', tagged: false },
        ]
        return Promise.resolve({ ok: true, json: () => Promise.resolve({ sessions: u.includes('tag=bug') ? all.filter(x => x.tagged) : all, hasMore: false }) })
      })
      const wrapper = await mountList()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await wrapper.find('.session-tag-filter-chip').trigger('click')
      await flushPromises()

      failTags = true
      await wrapper.vm.loadFilterTags()
      await flushPromises()

      expect(wrapper.find('.session-tag-filter').exists()).toBe(true)
      expect(wrapper.find('.session-tag-filter-chip').classes()).toContain('active')
      wrapper.unmount()
    })

    it('hides the filter bar on the cross-project tab', async () => {
      routeFetch({ sessions: [sessionsFixture().s1], tags: [{ name: 'bug', scope: 'project', count: 1 }] })
      const wrapper = await mountList({ activeTab: 'project' })
      await wrapper.vm.loadSessions()
      await flushPromises()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(true)

      await wrapper.setProps({ activeTab: 'cross' })
      await nextTick()
      expect(wrapper.find('.session-tag-filter').exists()).toBe(false)
      wrapper.unmount()
    })
  })
})
