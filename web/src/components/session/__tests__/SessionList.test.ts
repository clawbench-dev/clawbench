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
