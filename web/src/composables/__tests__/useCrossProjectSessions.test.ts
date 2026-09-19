import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick, reactive } from 'vue'
import { flushPromises } from '@vue/test-utils'

// The state must be reactive: the composable watches store.state.projectRoot /
// sessionListVersion at module top level, and Vue cannot track a plain object.
// It is created inside the mock factory because that runs when the composable
// module imports '@/stores/app' — earlier than this file's own module body
// (ES imports are hoisted above the `mockStore.state = ...` assignment).
const { mockStore } = vi.hoisted(() => ({
  mockStore: { state: null as any },
}))

vi.mock('@/stores/app', () => {
  mockStore.state = reactive({ projectRoot: '/proj/a', homeDir: '/home/u', sessionListVersion: 0 })
  return { store: mockStore }
})

vi.mock('@/utils/appLog', () => ({ appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() } }))

const { mockOnEvent, mockEventHolder } = vi.hoisted(() => ({
  mockOnEvent: vi.fn(),
  mockEventHolder: { handler: null as null | ((event: string) => void) },
}))
vi.mock('@/composables/useGlobalEvents', () => ({
  useGlobalEvents: () => ({
    onEvent: (handler: (event: string) => void) => {
      mockEventHolder.handler = handler
      mockOnEvent(handler)
      return () => {}
    },
  }),
}))

const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

import {
  useCrossProjectSessions,
  refresh,
  scheduleRefresh,
  resetCrossProjectSessionsForTest,
  abbreviatePath,
} from '@/composables/useCrossProjectSessions'

function overviewPayload(projects: Array<{ name: string; sessions: any[] }>) {
  return { ok: true, json: () => Promise.resolve({ projects, total: projects.length }) }
}

function session(id: string, updatedAt: string, extra: Record<string, unknown> = {}) {
  return { id, title: `T-${id}`, running: false, pendingApproval: false, unreadCount: 0, updatedAt, ...extra }
}

describe('useCrossProjectSessions', () => {
  beforeEach(() => {
    resetCrossProjectSessionsForTest()
    vi.clearAllMocks()
    mockFetch.mockReset()
    mockStore.state.projectRoot = '/proj/a'
    mockStore.state.homeDir = '/home/u'
    mockStore.state.sessionListVersion = 0
  })

  afterEach(() => {
    resetCrossProjectSessionsForTest()
  })

  it('excludes the current project and keeps other groups', async () => {
    mockFetch.mockResolvedValue(overviewPayload([
      { name: '/proj/a', sessions: [session('a1', '2026-01-02')] },
      { name: '/proj/b', sessions: [session('b1', '2026-01-01')] },
    ]))
    await refresh()
    const { groups, total } = useCrossProjectSessions()
    expect(groups.value.map(g => g.name)).toEqual(['/proj/b'])
    expect(total.value).toBe(1)
  })

  it('drops groups that have no sessions after filtering', async () => {
    mockFetch.mockResolvedValue(overviewPayload([
      { name: '/proj/a', sessions: [session('a1', '2026-01-02')] },
    ]))
    await refresh()
    expect(useCrossProjectSessions().groups.value).toEqual([])
  })

  it('sorts groups by path case-insensitively and sessions by updatedAt desc', async () => {
    mockFetch.mockResolvedValue(overviewPayload([
      { name: '/proj/Zeta', sessions: [session('z1', '2026-01-01')] },
      { name: '/proj/alpha', sessions: [
        session('a-old', '2026-01-01'),
        session('a-new', '2026-03-01'),
        session('a-mid', '2026-02-01'),
      ] },
    ]))
    await refresh()
    const { groups } = useCrossProjectSessions()
    expect(groups.value.map(g => g.name)).toEqual(['/proj/alpha', '/proj/Zeta'])
    expect(groups.value[0].sessions.map(s => s.id)).toEqual(['a-new', 'a-mid', 'a-old'])
  })

  it('maps backend/agentId/model through for row rendering', async () => {
    mockFetch.mockResolvedValue(overviewPayload([
      { name: '/proj/b', sessions: [session('b1', '2026-01-01', { backend: 'codebuddy', agentId: 'cb', model: 'gpt-5' })] },
    ]))
    await refresh()
    const s = useCrossProjectSessions().groups.value[0].sessions[0]
    expect(s.backend).toBe('codebuddy')
    expect(s.agentId).toBe('cb')
    expect(s.model).toBe('gpt-5')
  })

  it('derives displayName from basename', async () => {
    mockFetch.mockResolvedValue(overviewPayload([
      { name: '/home/u/projects/clawbench', sessions: [session('c1', '2026-01-01')] },
    ]))
    await refresh()
    const g = useCrossProjectSessions().groups.value[0]
    expect(g.displayName).toBe('clawbench')
    expect(g.displayPath).toBe('~/projects/clawbench')
  })

  it('keeps the previous snapshot when the request fails', async () => {
    mockFetch.mockResolvedValueOnce(overviewPayload([
      { name: '/proj/b', sessions: [session('b1', '2026-01-01')] },
    ]))
    await refresh()
    expect(useCrossProjectSessions().groups.value).toHaveLength(1)

    mockFetch.mockRejectedValueOnce(new Error('network down'))
    await refresh()
    expect(useCrossProjectSessions().groups.value).toHaveLength(1)
    expect(useCrossProjectSessions().groups.value[0].sessions[0].id).toBe('b1')
  })

  it('treats a non-ok response as a failure', async () => {
    mockFetch.mockResolvedValueOnce(overviewPayload([
      { name: '/proj/b', sessions: [session('b1', '2026-01-01')] },
    ]))
    await refresh()
    mockFetch.mockResolvedValueOnce({ ok: false, status: 500, json: () => Promise.resolve({}) })
    await refresh()
    expect(useCrossProjectSessions().groups.value).toHaveLength(1)
  })

  it('marks loaded only after a successful fetch', async () => {
    mockFetch.mockRejectedValueOnce(new Error('boom'))
    await refresh()
    expect(useCrossProjectSessions().loaded.value).toBe(false)
    mockFetch.mockResolvedValueOnce(overviewPayload([]))
    await refresh()
    expect(useCrossProjectSessions().loaded.value).toBe(true)
  })

  it('refreshes when the session list version changes', async () => {
    mockFetch.mockResolvedValue(overviewPayload([]))
    await refresh()
    const before = mockFetch.mock.calls.length
    mockStore.state.sessionListVersion++
    await nextTick()
    expect(mockFetch.mock.calls.length).toBeGreaterThan(before)
  })

  it('refreshes when the current project changes', async () => {
    mockFetch.mockResolvedValue(overviewPayload([]))
    await refresh()
    const before = mockFetch.mock.calls.length
    mockStore.state.projectRoot = '/proj/other'
    await nextTick()
    expect(mockFetch.mock.calls.length).toBeGreaterThan(before)
  })

  it('keeps the module-level watchers alive after a component unmounts (S1 regression)', async () => {
    // The watchers are registered at module top level, not inside a component
    // scope. Mounting and unmounting a consumer must not detach them — a
    // component-scoped watcher would be destroyed on project switch (which
    // rebuilds the whole subtree) and the list would silently stop updating.
    const { mount } = await import('@vue/test-utils')
    const Consumer = { template: '<div />', setup() { return useCrossProjectSessions() } }
    const wrapper = mount(Consumer)
    wrapper.unmount()

    mockFetch.mockResolvedValue(overviewPayload([]))
    const before = mockFetch.mock.calls.length
    mockStore.state.projectRoot = '/proj/after-unmount'
    await nextTick()
    expect(mockFetch.mock.calls.length).toBeGreaterThan(before)
  })

  it('registers a WS session_update subscription at module load', () => {
    expect(mockEventHolder.handler).toBeTypeOf('function')
  })

  it('debounces WS-triggered refreshes', async () => {
    vi.useFakeTimers()
    try {
      mockFetch.mockResolvedValue(overviewPayload([]))
      await refresh()
      const before = mockFetch.mock.calls.length
      scheduleRefresh()
      scheduleRefresh()
      scheduleRefresh()
      vi.advanceTimersByTime(400)
      await Promise.resolve()
      expect(mockFetch.mock.calls.length).toBe(before + 1)
    } finally {
      vi.useRealTimers()
    }
  })

  // ── Request coalescing ──
  //
  // A project switch fires several triggers at once (projectRoot, and a
  // sessionListVersion bump from the session reload). Before coalescing, each
  // one issued its own /api/ai/sessions/overview — a measured switch sent it
  // five times.
  //
  // The guarantee is a BOUND, not a single request: N concurrent callers cost
  // at most two fetches — the one already in flight, plus one trailing pass so
  // a change that arrived mid-flight is still reflected. Dropping the trailing
  // pass would be cheaper but would silently miss a session update that landed
  // during the fetch.
  describe('request coalescing', () => {
    it('bounds N concurrent callers to at most two requests', async () => {
      let resolveFetch: (v: unknown) => void = () => {}
      mockFetch.mockReturnValueOnce(new Promise((res) => { resolveFetch = res }))
      mockFetch.mockResolvedValue(overviewPayload([]))

      const a = refresh()
      const b = refresh()
      const c = refresh()
      resolveFetch(overviewPayload([]))
      await Promise.all([a, b, c])

      // Not three — the in-flight request is shared.
      expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('does not stack trailing passes under a burst of triggers', async () => {
      let resolveFetch: (v: unknown) => void = () => {}
      mockFetch.mockReturnValueOnce(new Promise((res) => { resolveFetch = res }))
      mockFetch.mockResolvedValue(overviewPayload([]))

      const first = refresh()
      for (let i = 0; i < 5; i++) refresh()
      resolveFetch(overviewPayload([]))
      await first

      // One in-flight + one trailing pass, regardless of how many callers joined.
      expect(mockFetch).toHaveBeenCalledTimes(2)
    })

    it('issues a single request when both switch signals fire together', async () => {
      mockFetch.mockResolvedValue(overviewPayload([]))
      await refresh()
      const before = mockFetch.mock.calls.length

      // A project switch bumps both the project and (via the session reload) the
      // list version. Watching them as one source makes Vue run a single
      // callback, so this must be exactly one refresh — not one per signal.
      mockStore.state.projectRoot = '/proj/other'
      mockStore.state.sessionListVersion++
      // flushPromises, not nextTick: the coalescing loop awaits the fetch and
      // may issue a trailing pass in a later microtask. Asserting too early
      // would count the in-flight request only and pass even if a second
      // refresh had been queued.
      await flushPromises()

      expect(mockFetch.mock.calls.length).toBe(before + 1)
    })

    it('still delivers a later trigger that arrives after the burst', async () => {
      mockFetch.mockResolvedValue(overviewPayload([]))
      await refresh()
      const before = mockFetch.mock.calls.length

      // Sequential (not overlapping) triggers must each get their own fetch —
      // coalescing must not swallow them.
      mockStore.state.projectRoot = '/proj/one'
      await nextTick()
      await nextTick()
      mockStore.state.projectRoot = '/proj/two'
      await nextTick()
      await nextTick()

      expect(mockFetch.mock.calls.length).toBe(before + 2)
    })
  })

  describe('abbreviatePath', () => {
    it('replaces the home prefix with ~', () => {
      expect(abbreviatePath('/home/u/projects/x', '/home/u')).toBe('~/projects/x')
    })
    it('leaves paths outside home untouched', () => {
      expect(abbreviatePath('/opt/data/x', '/home/u')).toBe('/opt/data/x')
    })
    it('handles Windows separators', () => {
      expect(abbreviatePath('C:\\Users\\me\\proj', 'C:/Users/me')).toBe('~/proj')
    })
    it('middle-truncates very long paths', () => {
      const long = '/very/deeply/nested/path/that/keeps/going/and/going/project'
      const out = abbreviatePath(long, '')
      expect(out.length).toBeLessThanOrEqual(32)
      expect(out).toContain('…')
    })
  })
})
