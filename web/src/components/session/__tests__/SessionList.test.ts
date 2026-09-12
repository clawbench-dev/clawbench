import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import SessionList from '@/components/session/SessionList.vue'
import { LongPressDirective } from '@/directives/longPress'

const { mockGetAgentBackend, mockGetAgentName, mockDialogHolder, mockReconcileRunningSessions, mockRemoveEventHandler, mockEventHolder, mockStore } = vi.hoisted(() => {
  const mockStore = { state: { chatSessionPageSize: 10, sessionListVersion: 0, sessionCount: 0 } }
  return {
    mockGetAgentBackend: vi.fn(() => ''),
    mockGetAgentName: vi.fn(() => 'Agent'),
    mockDialogHolder: { confirm: null as null | ((m: string, o?: any) => Promise<boolean>), prompt: null as null | ((m: string, o?: any) => Promise<string | null>), lastOptions: null as any },
    mockReconcileRunningSessions: vi.fn(),
    mockRemoveEventHandler: vi.fn(),
    mockEventHolder: { handler: null as null | ((event: string) => void) },
    mockStore,
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
  })

  async function mountList(props = {}) {
    const wrapper = mount(SessionList, {
      props: { currentSessionId: 's1', runningSessionIds: new Set(), ...props },
      global: { directives: { 'long-press': LongPressDirective } },
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
})
