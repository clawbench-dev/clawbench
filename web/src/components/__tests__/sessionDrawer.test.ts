import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

// ── Module mocks — must be before import ──

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: { value: [{ id: 'claude', name: 'Claude', icon: '🤖', backend: 'claude', specialty: '' }] },
    loadAgents: vi.fn().mockResolvedValue(undefined),
    getAgentIcon: vi.fn().mockReturnValue('🤖'),
    getAgentName: vi.fn().mockReturnValue('Claude'),
    isDefaultAgent: vi.fn().mockReturnValue(true),
    getAgentDefaultModelName: vi.fn().mockReturnValue(''),
    getAgentModels: vi.fn().mockReturnValue([]),
    getAgentThinkingEffortLevels: vi.fn().mockReturnValue([]),
  }),
}))

// ── Configurable dialog mock ──
let _dialogConfirmResult = false
let _lastConfirmMessage = ''
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    confirm: vi.fn().mockImplementation((msg: string) => {
      _lastConfirmMessage = msg
      return Promise.resolve(_dialogConfirmResult)
    }),
  }),
}))

vi.mock('@/composables/useSessionIdentity.ts', () => ({
  useSessionIdentity: () => ({
    runningSessionsVersion: { value: 0 },
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      chatSessionPageSize: 10,
    },
  },
}))

// ── Import after mocks ──

import SessionDrawer from '@/components/session/SessionDrawer.vue'

// ── Test data ──

const mockSessions = [
  { id: 's1', title: 'Session 1', backend: 'claude', updatedAt: '2026-01-01T00:00:00Z' },
  { id: 's2', title: 'Session 2', backend: 'codebuddy', updatedAt: '2026-01-02T00:00:00Z' },
  { id: 's3', title: 'Session 3', backend: 'claude', updatedAt: '2026-01-03T00:00:00Z' },
]

function createFetchMock(sessions = mockSessions) {
  return vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve({ sessions, hasMore: false }),
  })
}

// ── i18n ──

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      session: { title: '会话', newSession: '新建', selectAgent: '选择AI', confirmDelete: '确定删除此会话及其所有聊天记录?', confirmDeleteRunning: '此会话正在运行中，删除将终止运行并清除记录，确定删除?', running: '运行中', noSessions: '暂无会话' },
      common: { loading: '加载中', delete: '删除', cancel: '取消' },
    },
  },
})

// ── Helpers ──

/** Polyfill IntersectionObserver for jsdom */
function polyfillIO() {
  class MockIO { constructor() {} observe() {} unobserve() {} disconnect() {} }
  vi.stubGlobal('IntersectionObserver', MockIO)
}

/**
 * Mount SessionDrawer with open=false, then transition to open=true.
 * This matches the real usage where the drawer opens via prop change
 * (the internal watch(open) doesn't use { immediate: true }).
 */
async function mountOpenDrawer(fetchFn = createFetchMock()) {
  vi.stubGlobal('fetch', fetchFn)
  polyfillIO()

  const wrapper = mount(SessionDrawer, {
    props: {
      open: false,
      currentSessionId: 's1',
      runningSessionIds: new Set(),
    },
    global: {
      plugins: [i18n],
      stubs: {
        BottomSheet: {
          template: '<div><slot name="header" /><slot /></div>',
          props: ['open', 'auto', 'title'],
          methods: { close: vi.fn() },
        },
        ModalDialog: {
          template: '<div><slot /><slot name="footer" /></div>',
          props: ['open', 'title'],
        },
      },
    },
  })

  // Open the drawer — triggers watch(open) → loadSessions
  await wrapper.setProps({ open: true })
  await flushPromises()

  return { wrapper, fetchFn }
}

// ── Tests ──

describe('SessionDrawer: always reload on open', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads sessions from API when opened', async () => {
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    expect(fetchFn).toHaveBeenCalledWith(expect.stringContaining('/api/ai/sessions'))
    expect(wrapper.vm.sessions).toHaveLength(3)
  })

  it('reloads sessions every time the drawer is opened', async () => {
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    // First open: 1 fetch call
    expect(fetchFn).toHaveBeenCalledTimes(1)
    fetchFn.mockClear()

    // Close and reopen
    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    // Second open: another fetch call — always reloads
    expect(fetchFn).toHaveBeenCalledTimes(1)
    expect(wrapper.vm.sessions).toHaveLength(3)
  })

  it('shows new session after create (simulated by API returning updated list)', async () => {
    // First open: 2 sessions. Second open: 3 sessions (after create).
    const initialSessions = mockSessions.slice(0, 2)
    const afterCreateSessions = mockSessions

    let callCount = 0
    const fetchFn = vi.fn().mockImplementation(() => {
      callCount++
      const sessions = callCount <= 1 ? initialSessions : afterCreateSessions
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ sessions, hasMore: false }),
      })
    })

    const { wrapper } = await mountOpenDrawer(fetchFn)
    expect(wrapper.vm.sessions).toHaveLength(2)

    // Close and reopen — simulates user creating a session then reopening the list
    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    // New session appears
    expect(wrapper.vm.sessions).toHaveLength(3)
  })

  it('shows updated list after delete (simulated by API returning updated list)', async () => {
    // First open: 3 sessions. Second open: 2 sessions (after delete).
    const afterDeleteSessions = mockSessions.filter(s => s.id !== 's2')

    let callCount = 0
    const fetchFn = vi.fn().mockImplementation(() => {
      callCount++
      const sessions = callCount <= 1 ? mockSessions : afterDeleteSessions
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ sessions, hasMore: false }),
      })
    })

    const { wrapper } = await mountOpenDrawer(fetchFn)
    expect(wrapper.vm.sessions).toHaveLength(3)

    // Close and reopen — simulates user deleting a session then reopening the list
    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    // Deleted session gone
    expect(wrapper.vm.sessions).toHaveLength(2)
    expect(wrapper.vm.sessions.find(s => s.id === 's2')).toBeUndefined()
  })

  it('shows replacement session after delete-current + auto-create', async () => {
    const afterDeleteSessions = [
      { id: 's2', title: 'Session 2', backend: 'codebuddy', updatedAt: '2026-01-02T00:00:00Z' },
      { id: 's-replacement', title: 'New Session', backend: 'claude', updatedAt: '2026-01-04T00:00:00Z' },
    ]

    let callCount = 0
    const fetchFn = vi.fn().mockImplementation(() => {
      callCount++
      const sessions = callCount <= 1 ? mockSessions : afterDeleteSessions
      return Promise.resolve({
        ok: true,
        json: () => Promise.resolve({ sessions, hasMore: false }),
      })
    })

    const { wrapper } = await mountOpenDrawer(fetchFn)
    expect(wrapper.vm.sessions).toHaveLength(3)

    await wrapper.setProps({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.vm.sessions).toHaveLength(2)
    expect(wrapper.vm.sessions.find(s => s.id === 's1')).toBeUndefined()
    expect(wrapper.vm.sessions.find(s => s.id === 's-replacement')).toBeDefined()
  })

  it('does not fetch when opened with open=false', async () => {
    const fetchFn = createFetchMock()
    vi.stubGlobal('fetch', fetchFn)
    polyfillIO()

    mount(SessionDrawer, {
      props: { open: false, currentSessionId: 's1', runningSessionIds: new Set() },
      global: {
        plugins: [i18n],
        stubs: {
          BottomSheet: {
            template: '<div><slot name="header" /><slot /></div>',
            props: ['open', 'auto', 'title'],
            methods: { close: vi.fn() },
          },
          ModalDialog: {
            template: '<div><slot /><slot name="footer" /></div>',
            props: ['open', 'title'],
          },
        },
      },
    })
    await flushPromises()

    // No fetch when closed
    expect(fetchFn).not.toHaveBeenCalled()
  })

  it('no invalidate() method is exposed', async () => {
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    // invalidate should NOT exist on the component instance
    expect(wrapper.vm.invalidate).toBeUndefined()
  })

  it('removes session from local array on delete (no API reload)', async () => {
    _dialogConfirmResult = true
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    expect(wrapper.vm.sessions).toHaveLength(3)
    const fetchCallCount = fetchFn.mock.calls.length

    // Simulate clicking delete on s2 — dialog confirms, then local array is updated
    await wrapper.vm.deleteSession('s2')
    await flushPromises()

    // Session removed from local array without additional API call
    expect(wrapper.vm.sessions).toHaveLength(2)
    expect(wrapper.vm.sessions.find(s => s.id === 's2')).toBeUndefined()
    expect(fetchFn.mock.calls.length).toBe(fetchCallCount)

    _dialogConfirmResult = false
  })

  it('shows running-session confirmation for running session delete', async () => {
    _dialogConfirmResult = false
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    // Re-mount with s2 as a running session
    await wrapper.setProps({ runningSessionIds: new Set(['s2']) })
    await flushPromises()

    // Try to delete the running session
    await wrapper.vm.deleteSession('s2')
    await flushPromises()

    // Should show the running-session confirmation message
    expect(_lastConfirmMessage).toContain('运行中')

    _dialogConfirmResult = true
  })

  it('shows normal confirmation for non-running session delete', async () => {
    _dialogConfirmResult = false
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    // No running sessions — runningSessionIds is empty
    await wrapper.vm.deleteSession('s1')
    await flushPromises()

    // Should show the normal confirmation message
    expect(_lastConfirmMessage).not.toContain('运行中')
    expect(_lastConfirmMessage).toContain('聊天记录')

    _dialogConfirmResult = true
  })

  it('addSessionLocally prepends a new session without API reload', async () => {
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    expect(wrapper.vm.sessions).toHaveLength(3)
    const fetchCallCount = fetchFn.mock.calls.length

    // Add a new session locally
    wrapper.vm.addSessionLocally({
      id: 's-new',
      title: 'New Session',
      backend: 'claude',
      agentId: 'claude',
      updatedAt: '2026-01-04T00:00:00Z',
    })

    // New session appears at the top, no API call
    expect(wrapper.vm.sessions).toHaveLength(4)
    expect(wrapper.vm.sessions[0].id).toBe('s-new')
    expect(fetchFn.mock.calls.length).toBe(fetchCallCount)
  })

  it('addSessionLocally ignores duplicate session', async () => {
    const fetchFn = createFetchMock()
    const { wrapper } = await mountOpenDrawer(fetchFn)

    expect(wrapper.vm.sessions).toHaveLength(3)

    // Try adding an existing session
    wrapper.vm.addSessionLocally({ id: 's1', title: 'Session 1', backend: 'claude', updatedAt: '2026-01-01T00:00:00Z' })

    // No duplicate added
    expect(wrapper.vm.sessions).toHaveLength(3)
  })
})
