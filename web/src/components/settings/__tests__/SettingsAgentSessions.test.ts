import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SettingsAgentSessions from '@/components/settings/SettingsAgentSessions.vue'
import { store } from '@/stores/app'

// ── Mocks ────────────────────────────────────────────────────

const { mockApiGet, mockConfirm, mockToastShow, mockAppLogE } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockConfirm: vi.fn(),
  mockToastShow: vi.fn(),
  mockAppLogE: vi.fn(),
}))

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { e: (...args: unknown[]) => mockAppLogE(...args), d: vi.fn(), i: vi.fn(), w: vi.fn() },
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm }),
}))
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const map: Record<string, string> = {
          'settings.items.agentSessionsSummary': `${params?.count ?? 0} conversations`,
          'settings.items.agentSessionsLoading': 'Loading…',
          'settings.items.agentSessionsLoadingMore': 'Loading more…',
          'settings.items.agentSessionsLoadFailed': 'Failed',
          'settings.items.agentSessionsEmpty': 'No conversations',
          'settings.items.agentSessionsEnd': 'No more',
          'settings.items.agentSessionsUntitled': 'Untitled',
          'settings.items.agentSessionsMissing': 'No longer exists',
          'settings.items.agentSessionsLoadMoreFailed': 'Load more failed',
          'settings.items.agentSessionsRetry': 'Retry',
          'settings.items.agentSessionsArchived': 'Archived',
          'settings.items.agentSessionsRoleChat': '1:1',
          'settings.items.agentSessionsRoleHost': 'Host',
          'settings.items.agentSessionsRoleHostNamed': `Host · ${params?.name ?? ''}`,
          'settings.items.agentSessionsRoleMember': 'Member',
          'settings.items.agentSessionsRoleMemberNamed': `Member · ${params?.name ?? ''}`,
          'settings.items.agentSessionsResume': 'Restore',
          'settings.items.agentSessionsResumeConfirm': `Restore "${params?.title ?? ''}"?`,
          'settings.items.agentSessionsResumeFailed': 'Restore failed',
          'common.confirm': 'OK',
        }
        return map[key] ?? key
      },
    }),
  }
})

// jsdom has no IntersectionObserver. The stub records every observe() call so a
// test can drive the "sentinel became visible" path by hand.
class MockIntersectionObserver {
  static instances: MockIntersectionObserver[] = []
  callback: IntersectionObserverCallback
  observed: Element[] = []
  constructor(cb: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.callback = cb
    // Recording the root is what lets a test catch "root is not the scroll
    // container" — a defect that is invisible to intersection simulation.
    this.root = (options?.root as Element | null) ?? null
    MockIntersectionObserver.instances.push(this)
  }
  root: Element | null = null
  observe(el: Element) { this.observed.push(el) }
  disconnect() { this.observed = [] }
  unobserve() {}
  /** Simulate the sentinel entering the viewport. */
  trigger(isIntersecting = true) {
    this.callback([{ isIntersecting } as IntersectionObserverEntry], this as unknown as IntersectionObserver)
  }
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

// ── Helpers ──────────────────────────────────────────────────

// Fixture shape MUST match the server's wire format (camelCase json tags). A
// PascalCase fixture would be self-consistent with a wrong client interface and
// therefore prove nothing — every row would render blank in the real app while
// these tests stayed green.
function entry(over: Record<string, unknown> = {}) {
  return {
    entryId: 'e1',
    sessionId: 's1',
    title: 'Chat one',
    projectPath: '/proj/alpha',
    sessionType: 'chat',
    role: 'chat',
    memberName: '',
    archived: false,
    updatedAt: '2026-01-01 10:00:00',
    ...over,
  }
}

function page(over: Record<string, unknown> = {}) {
  return {
    sessions: [entry()],
    hasMore: false,
    nextCursor: '',
    nextCursorId: '',
    total: 1,
    ...over,
  }
}

function mountPage(opts: { hotSwitchProject?: (path: string) => Promise<void> } = {}) {
  return mount(SettingsAgentSessions, {
    props: { agentId: 'agent-a' },
    global: {
      provide: {
        hotSwitchProject: opts.hotSwitchProject ?? (async () => {}),
      },
    },
  })
}

/** Dispatch a window event and return it (the component talks to App this way). */
function captureOpenSession() {
  const seen: Array<Record<string, unknown>> = []
  const handler = (e: Event) => { seen.push((e as CustomEvent).detail) }
  window.addEventListener('clawbench-open-session', handler)
  return { seen, stop: () => window.removeEventListener('clawbench-open-session', handler) }
}

// The component decides "same project?" against the app store, so the fixture
// path must match it — otherwise every row reads as cross-project and the
// archived-resume branch is never taken (the tests would silently exercise the
// wrong path).
const CURRENT_PROJECT = '/proj/alpha'

beforeEach(() => {
  vi.clearAllMocks()
  MockIntersectionObserver.instances = []
  store.state.projectRoot = CURRENT_PROJECT
  mockApiGet.mockResolvedValue(page())
  mockConfirm.mockResolvedValue(true)
})

// ── Tests ────────────────────────────────────────────────────

describe('SettingsAgentSessions', () => {
  // The wire contract is camelCase (the Go struct has json tags). Pinning it
  // here catches the failure mode where the fixture and the client interface
  // agree with each other but not with the server — that combination renders
  // every row blank in production while all component tests stay green.
  it('consumes the server\'s camelCase wire keys', async () => {
    mockApiGet.mockResolvedValue(page())
    const wrapper = mountPage()
    await flushPromises()

    // A title that only appears if `title` (not `Title`) was read.
    expect(wrapper.find('.settings-agent-sessions__row-title').text()).toBe('Chat one')
    // The project chip only renders from `projectPath`.
    expect(wrapper.find('.settings-agent-sessions__project').text()).toBe('alpha')
    // Role label only resolves from `role`.
    expect(wrapper.find('.settings-agent-sessions__role').text()).toBe('1:1')
  })

  it('requests the paged endpoint for the given agent', async () => {
    mountPage()
    await flushPromises()
    expect(mockApiGet).toHaveBeenCalledTimes(1)
    expect(String(mockApiGet.mock.calls[0][0])).toContain('/api/agents/agent-a/sessions')
    expect(String(mockApiGet.mock.calls[0][0])).toContain('limit=50')
  })

  it('renders the server total, not the number of loaded rows', async () => {
    // The header must equal the count the detail page shows, which is the
    // server's total — deriving it from the loaded page would read "1" while
    // more rows remain.
    mockApiGet.mockResolvedValue(page({ total: 37 }))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.find('.settings-agent-sessions__summary').text()).toContain('37 conversations')
  })

  it('shows the empty hint when the agent has no conversations', async () => {
    mockApiGet.mockResolvedValue(page({ sessions: [], total: 0 }))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.text()).toContain('No conversations')
  })

  it('shows the failure hint and a retry button when the first page fails', async () => {
    mockApiGet.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.text()).toContain('Failed')

    // A dead-end error would force the user to leave and re-enter the page.
    mockApiGet.mockResolvedValueOnce(page())
    await wrapper.find('.settings-agent-sessions__retry').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.settings-agent-sessions__row')).toHaveLength(1)
  })

  // A member row whose group row is gone is still counted (so it must be
  // listed) but has no conversation to open. It must not render as a clickable
  // row that silently dispatches an empty id.
  it('renders an entry with no session id as disabled, not clickable', async () => {
    mockApiGet.mockResolvedValue(page({
      sessions: [entry({ entryId: 'orphan', sessionId: '', projectPath: '', title: '' })],
    }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()

      const row = wrapper.find('.settings-agent-sessions__row')
      expect(row.attributes('disabled')).toBeDefined()
      expect(wrapper.find('.settings-agent-sessions__row-missing').exists()).toBe(true)
      await row.trigger('click')
      expect(capture.seen).toEqual([])
    } finally {
      capture.stop()
    }
  })

  // An append failure must NOT wipe the rows already on screen — only the
  // footer reports it, and the retry resumes from the same cursor.
  it('keeps the loaded rows when a later page fails, and retries from the same cursor', async () => {
    mockApiGet
      .mockResolvedValueOnce(page({ total: 2, hasMore: true, nextCursor: '2026-01-01 10:00:00', nextCursorId: 'e1' }))
      .mockRejectedValueOnce(new Error('blip'))
      .mockResolvedValueOnce(page({
        sessions: [entry({ entryId: 'e2', sessionId: 's2', title: 'Chat two' })],
        total: 2,
        hasMore: false,
      }))

    const wrapper = mountPage()
    await flushPromises()
    MockIntersectionObserver.instances.at(-1)!.trigger(true)
    await flushPromises()

    // The first row survived; the footer explains and offers a retry.
    expect(wrapper.findAll('.settings-agent-sessions__row')).toHaveLength(1)
    expect(wrapper.text()).toContain('Load more failed')
    expect(wrapper.find('.settings-agent-sessions__retry').exists()).toBe(true)

    await wrapper.find('.settings-agent-sessions__retry').trigger('click')
    await flushPromises()

    // Same cursor as the failed attempt — not restarted from page 1.
    expect(String(mockApiGet.mock.calls[2][0])).toContain('cursor_id=e1')
    expect(wrapper.findAll('.settings-agent-sessions__row')).toHaveLength(2)
  })

  // The three roles must be distinguishable in the row — a plain member and a
  // host both "use" the agent but mean different things.
  it('labels chat, host and member rows differently', async () => {
    mockApiGet.mockResolvedValue(page({
      sessions: [
        entry({ entryId: 'c', sessionId: 'c', role: 'chat' }),
        entry({ entryId: 'h', sessionId: 'g1', role: 'host', sessionType: 'group', memberName: 'Alice' }),
        entry({ entryId: 'm', sessionId: 'g2', role: 'member', sessionType: 'group', memberName: 'Bob' }),
      ],
      total: 3,
    }))
    const wrapper = mountPage()
    await flushPromises()

    const roles = wrapper.findAll('.settings-agent-sessions__role').map(r => r.text())
    expect(roles).toEqual(['1:1', 'Host · Alice', 'Member · Bob'])
    // Host gets its own modifier class so the two can be told apart by shape,
    // not only by hue (several themes share accent and orange).
    expect(wrapper.findAll('.settings-agent-sessions__role--host')).toHaveLength(1)
  })

  it('marks archived rows and shows the owning project', async () => {
    mockApiGet.mockResolvedValue(page({
      sessions: [entry({ archived: true, projectPath: '/somewhere/beta' })],
    }))
    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.find('.settings-agent-sessions__archived').exists()).toBe(true)
    expect(wrapper.find('.settings-agent-sessions__project').text()).toBe('beta')
  })

  // The page is cross-project, so the open event must carry the project path —
  // without it App opens the session in the CURRENT project and 404s.
  it('dispatches clawbench-open-session with the session id and project path', async () => {
    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      // Same project: the path is omitted (nothing to switch to).
      expect(capture.seen).toEqual([{ sessionId: 's1', projectPath: undefined }])
    } finally {
      capture.stop()
    }
  })

  it('opens the GROUP timeline row, not the member row, for a membership entry', async () => {
    // A group_member row is hidden and has no messages; opening it would show an
    // empty conversation.
    mockApiGet.mockResolvedValue(page({
      sessions: [entry({ entryId: 'member-row', sessionId: 'group-timeline', role: 'member', sessionType: 'group' })],
    }))
    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      expect(capture.seen[0].sessionId).toBe('group-timeline')
    } finally {
      capture.stop()
    }
  })

  // Archived chats cannot be read (GET history 404s), so they must be restored
  // first — and a cancelled confirm must not restore or navigate.
  it('resumes an archived row before opening it', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) })
    vi.stubGlobal('fetch', fetchMock)
    mockApiGet.mockResolvedValue(page({ sessions: [entry({ archived: true })] }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      await flushPromises()

      expect(mockConfirm).toHaveBeenCalledTimes(1)
      expect(fetchMock).toHaveBeenCalledWith('/api/ai/session/resume', expect.objectContaining({ method: 'POST' }))
      expect(capture.seen).toEqual([{ sessionId: 's1', projectPath: undefined }])
    } finally {
      capture.stop()
    }
  })

  it('does not restore or navigate when the resume confirm is cancelled', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    mockConfirm.mockResolvedValue(false)
    mockApiGet.mockResolvedValue(page({ sessions: [entry({ archived: true })] }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      await flushPromises()

      expect(fetchMock).not.toHaveBeenCalled()
      expect(capture.seen).toEqual([])
    } finally {
      capture.stop()
    }
  })

  // Resume is project-scoped (it validates against the cookie project), so a
  // cross-project archived row must SWITCH FIRST and only then restore. Doing it
  // the other way round 403s; skipping the restore leaves the session
  // unopenable.
  it('switches project BEFORE resuming a cross-project archived row', async () => {
    const calls: string[] = []
    const fetchMock = vi.fn().mockImplementation(() => {
      calls.push('resume')
      return Promise.resolve({ ok: true, json: async () => ({}) })
    })
    vi.stubGlobal('fetch', fetchMock)
    mockApiGet.mockResolvedValue(page({
      sessions: [entry({ archived: true, projectPath: '/proj/beta' })],
    }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage({ hotSwitchProject: async () => { calls.push('switch') } })
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      await flushPromises()

      expect(calls).toEqual(['switch', 'resume'])
      expect(capture.seen).toEqual([{ sessionId: 's1', projectPath: '/proj/beta' }])
    } finally {
      capture.stop()
    }
  })

  it('aborts without restoring or navigating when the project switch fails', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    mockApiGet.mockResolvedValue(page({
      sessions: [entry({ archived: true, projectPath: '/proj/beta' })],
    }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage({
        hotSwitchProject: async () => { throw new Error('nope') },
      })
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      await flushPromises()

      expect(fetchMock).not.toHaveBeenCalled()
      expect(capture.seen).toEqual([])
    } finally {
      capture.stop()
    }
  })

  it('does not navigate when the resume request fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) }))
    mockApiGet.mockResolvedValue(page({ sessions: [entry({ archived: true })] }))

    const capture = captureOpenSession()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await wrapper.find('.settings-agent-sessions__row').trigger('click')
      await flushPromises()

      expect(mockToastShow).toHaveBeenCalledWith('Restore failed', expect.any(Object))
      expect(capture.seen).toEqual([])
    } finally {
      capture.stop()
    }
  })

  // ── Lazy loading ────────────────────────────────────────────

  // The observer's root must be a SCROLLING ancestor. Passing the list element
  // (which does not scroll) makes the sentinel report "intersecting" forever and
  // every page is fetched at once — lazy loading silently becomes eager. The
  // mock cannot catch that (it never computes intersections), so this asserts
  // the root the component actually passed.
  it('observes the sentinel with a scrolling ancestor as root, not the list', async () => {
    mockApiGet.mockResolvedValue(page({ hasMore: true, nextCursor: '2026-01-01 10:00:00', nextCursorId: 'e1' }))
    const wrapper = mountPage()
    await flushPromises()

    const observer = MockIntersectionObserver.instances.at(-1)!
    const root = observer.root
    const listEl = wrapper.find('.settings-agent-sessions__list').element
    const sentinelEl = wrapper.find('.settings-agent-sessions__sentinel').element
    // jsdom computes no layout, so `getComputedStyle().overflowY` is empty and
    // the component falls back to the viewport (null). The point is that the
    // root is NOT an element inside the list: either of those makes the sentinel
    // trivially intersect itself and every page loads at once.
    expect(root).not.toBe(listEl)
    expect(root).not.toBe(sentinelEl)
  })
  it('loads the next page when the sentinel becomes visible', async () => {
    mockApiGet
      .mockResolvedValueOnce(page({ total: 2, hasMore: true, nextCursor: '2026-01-01 10:00:00', nextCursorId: 'e1' }))
      .mockResolvedValueOnce(page({
        sessions: [entry({ entryId: 'e2', sessionId: 's2', title: 'Chat two' })],
        total: 2,
        hasMore: false,
      }))

    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.findAll('.settings-agent-sessions__row')).toHaveLength(1)

    // The observer is (re)bound after the list renders; drive it directly.
    const observer = MockIntersectionObserver.instances.at(-1)!
    observer.trigger(true)
    await flushPromises()

    expect(mockApiGet).toHaveBeenCalledTimes(2)
    const second = String(mockApiGet.mock.calls[1][0])
    expect(second).toContain('cursor=2026-01-01')
    expect(second).toContain('cursor_id=e1')
    expect(wrapper.findAll('.settings-agent-sessions__row')).toHaveLength(2)
  })

  it('does not request another page when the list is exhausted', async () => {
    mockApiGet.mockResolvedValue(page({ hasMore: false }))
    mountPage()
    await flushPromises()

    // No observer should even be bound once hasMore is false.
    const bound = MockIntersectionObserver.instances.filter(o => o.observed.length > 0)
    expect(bound).toHaveLength(0)
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('drops a stale response that lands after the agent changed', async () => {
    // Agent A's request resolves AFTER the component has switched to agent B;
    // appending it would mix two agents' conversations.
    let resolveA: (v: unknown) => void = () => {}
    mockApiGet.mockImplementationOnce(() => new Promise((r) => { resolveA = r }))
    const wrapper = mountPage()
    await flushPromises()

    mockApiGet.mockResolvedValueOnce(page({
      sessions: [entry({ entryId: 'b1', sessionId: 'b1', title: 'Agent B chat' })],
      total: 1,
    }))
    await wrapper.setProps({ agentId: 'agent-b' })
    await flushPromises()

    resolveA(page({ sessions: [entry({ entryId: 'a1', sessionId: 'a1', title: 'Agent A chat' })], total: 1 }))
    await flushPromises()

    const titles = wrapper.findAll('.settings-agent-sessions__row-title').map(t => t.text())
    expect(titles).toEqual(['Agent B chat'])
  })
})
