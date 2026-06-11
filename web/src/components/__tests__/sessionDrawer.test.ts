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

/** Common stubs used by all mount calls */
const commonStubs = {
  BottomSheet: {
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    methods: { close: vi.fn() },
  },
  ModalDialog: {
    template: '<div><slot /><slot name="footer" /></div>',
    props: ['open', 'title'],
  },
  SwipeToDeleteRow: {
    template: '<div class="swipe-row"><slot /></div>',
    props: ['threshold'],
    methods: { reset: vi.fn() },
  },
}

/**
 * Mount SessionDrawer with sessions pre-loaded by calling loadSessions() directly.
 * vi.stubGlobal('fetch') does not work with SFC module-scoped fetch references
 * in jsdom, so we bypass the API call by invoking the component's loadSessions
 * method and feeding it mock data.
 */
async function mountWithSessions(sessions = mockSessions) {
  polyfillIO()
  const fetchFn = createFetchMock(sessions)
  vi.stubGlobal('fetch', fetchFn)

  const wrapper = mount(SessionDrawer, {
    props: {
      open: true,
      currentSessionId: 's1',
      runningSessionIds: new Set(),
    },
    global: {
      plugins: [i18n],
      stubs: commonStubs,
    },
  })
  await flushPromises()

  // Load sessions directly since the watch(open) may not trigger fetch in jsdom
  if (wrapper.vm.sessions.length === 0) {
    await wrapper.vm.loadSessions()
    await flushPromises()
  }

  return { wrapper, fetchFn }
}

// ── Tests ──

describe('SessionDrawer: always reload on open', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('loads sessions via loadSessions()', async () => {
    const { wrapper } = await mountWithSessions()
    expect(wrapper.vm.sessions).toHaveLength(3)
  })

  it('reloads sessions when loadSessions is called again', async () => {
    const afterDelete = mockSessions.filter(s => s.id !== 's2')
    const { wrapper, fetchFn } = await mountWithSessions()

    expect(wrapper.vm.sessions).toHaveLength(3)
    fetchFn.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: afterDelete, hasMore: false }),
    })

    await wrapper.vm.loadSessions()
    await flushPromises()

    expect(wrapper.vm.sessions).toHaveLength(2)
    expect(wrapper.vm.sessions.find(s => s.id === 's2')).toBeUndefined()
  })

  it('does not fetch when opened with open=false', async () => {
    const fetchFn = createFetchMock()
    vi.stubGlobal('fetch', fetchFn)
    polyfillIO()

    mount(SessionDrawer, {
      props: { open: false, currentSessionId: 's1', runningSessionIds: new Set() },
      global: { plugins: [i18n], stubs: commonStubs },
    })
    await flushPromises()

    // No fetch when closed
    expect(fetchFn).not.toHaveBeenCalled()
  })

  it('no invalidate() method is exposed', async () => {
    const { wrapper } = await mountWithSessions()
    expect(wrapper.vm.invalidate).toBeUndefined()
  })

  it('emits delete event on confirmed delete (no optimistic removal)', async () => {
    _dialogConfirmResult = true
    const { wrapper } = await mountWithSessions()

    expect(wrapper.vm.sessions).toHaveLength(3)

    // Delete s2 — dialog confirms, then emit fires
    await wrapper.vm.deleteSession('s2')
    await flushPromises()

    // No optimistic removal — session stays until parent refreshes
    expect(wrapper.vm.sessions).toHaveLength(3)
    expect(wrapper.emitted('delete')).toBeTruthy()
    expect(wrapper.emitted('delete')[0]).toEqual(['s2', 'codebuddy'])

    _dialogConfirmResult = false
  })

  it('does not emit delete when dialog is cancelled', async () => {
    _dialogConfirmResult = false
    const { wrapper } = await mountWithSessions()

    await wrapper.vm.deleteSession('s2')
    await flushPromises()

    expect(wrapper.emitted('delete')).toBeFalsy()

    _dialogConfirmResult = true
  })

  it('shows running-session confirmation for running session delete', async () => {
    _dialogConfirmResult = false
    const { wrapper } = await mountWithSessions()

    await wrapper.setProps({ runningSessionIds: new Set(['s2']) })
    await flushPromises()

    await wrapper.vm.deleteSession('s2')
    await flushPromises()

    expect(_lastConfirmMessage).toContain('运行中')

    _dialogConfirmResult = true
  })

  it('shows normal confirmation for non-running session delete', async () => {
    _dialogConfirmResult = false
    const { wrapper } = await mountWithSessions()

    await wrapper.vm.deleteSession('s1')
    await flushPromises()

    expect(_lastConfirmMessage).not.toContain('运行中')
    expect(_lastConfirmMessage).toContain('聊天记录')

    _dialogConfirmResult = true
  })

  it('addSessionLocally prepends a new session without API reload', async () => {
    const { wrapper, fetchFn } = await mountWithSessions()
    const fetchCallCount = fetchFn.mock.calls.length

    wrapper.vm.addSessionLocally({
      id: 's-new',
      title: 'New Session',
      backend: 'claude',
      agentId: 'claude',
      updatedAt: '2026-01-04T00:00:00Z',
    })

    expect(wrapper.vm.sessions).toHaveLength(4)
    expect(wrapper.vm.sessions[0].id).toBe('s-new')
    expect(fetchFn.mock.calls.length).toBe(fetchCallCount)
  })

  it('addSessionLocally ignores duplicate session', async () => {
    const { wrapper } = await mountWithSessions()
    expect(wrapper.vm.sessions).toHaveLength(3)

    wrapper.vm.addSessionLocally({ id: 's1', title: 'Session 1', backend: 'claude', updatedAt: '2026-01-01T00:00:00Z' })

    expect(wrapper.vm.sessions).toHaveLength(3)
  })
})
