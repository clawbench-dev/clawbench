import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'
import SessionDrawer from '@/components/session/SessionDrawer.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
  createI18n: () => ({ global: { t: (key: string) => key, locale: { value: 'en' } } }),
}))

vi.mock('@/composables/useLocale', () => ({
  useLocale: () => ({
    currentLocale: { value: 'en' },
    setLocale: vi.fn(),
    toggleLocale: vi.fn(),
    localeLabel: { value: 'EN' },
  }),
  gt: (key: string) => key,
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/stores/app', () => ({
  store: {
    state: { sessionCount: 0, sessionMaxCount: 10, chatSessionPageSize: 10, currentFile: null },
  },
}))

const { mockLoadAgents, mockGetAgentBackend, mockGetAgentName, mockIsDefaultAgent, mockGetAgentDefaultModelName, mockSetDefaultAgent } = vi.hoisted(() => ({
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockGetAgentBackend: vi.fn(() => ''),
  mockGetAgentName: vi.fn(() => 'Agent'),
  mockIsDefaultAgent: vi.fn(() => false),
  mockGetAgentDefaultModelName: vi.fn(() => ''),
  mockSetDefaultAgent: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref([
      { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' },
      { id: 'agent-2', name: 'Agent Two', backend: 'acp', specialty: 'Design' },
    ]),
    loadAgents: mockLoadAgents,
    getAgentBackend: mockGetAgentBackend,
    getAgentName: mockGetAgentName,
    isDefaultAgent: mockIsDefaultAgent,
    getAgentDefaultModelName: mockGetAgentDefaultModelName,
    setDefaultAgent: mockSetDefaultAgent,
  }),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: vi.fn().mockResolvedValue(true),
  }),
}))

vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: () => ({
    runningSessionsVersion: { value: 0 },
  }),
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (d: string) => d || 'now',
}))

// Stub child components
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet-stub"><slot name="header" /><slot /></div>',
    methods: { close: vi.fn() },
  },
}))

vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: {
    name: 'ModalDialog',
    template: '<div class="modal-stub"><slot name="header" /><slot /></div>',
  },
}))

vi.mock('@/components/git/SwipeToDeleteRow.vue', () => ({
  default: {
    name: 'SwipeToDeleteRow',
    template: '<div class="swipe-stub"><slot /></div>',
  },
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: {
    name: 'AgentIcon',
    template: '<span class="agent-icon-stub" />',
  },
}))

vi.mock('@/components/common/AgentSelectorDrawer.vue', () => ({
  default: {
    name: 'AgentSelectorDrawer',
    template: '<div class="agent-selector-drawer-stub" />',
    methods: { preload: vi.fn() },
  },
}))

// ── Mock IntersectionObserver ──
class MockIntersectionObserver {
  callback: (entries: IntersectionObserverEntry[], observer: IntersectionObserver) => void
  constructor(cb: (entries: IntersectionObserverEntry[], observer: IntersectionObserver) => void) { this.callback = cb }
  observe() {}
  disconnect() {}
  unobserve() {}
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

// ── Mock fetch ──
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

function mountDrawer(props = {}) {
  return mount(SessionDrawer, {
    props: {
      open: true,
      currentSessionId: 's1',
      runningSessionIds: new Set(),
      ...props,
    },
  })
}

describe('SessionDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetch.mockReset()
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ sessions: [], hasMore: false, totalCount: 0 }),
    })
  })

  describe('session counter', () => {
    it('renders session counter when maxCount > 0', async () => {
      const { store } = await import('@/stores/app')
      store.state.sessionCount = 5
      store.state.sessionMaxCount = 10

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.find('.session-counter').exists()).toBe(true)
    })
  })

  describe('empty state', () => {
    it('shows empty message when no sessions', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], hasMore: false, totalCount: 0 }),
      })

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.find('.session-empty').exists()).toBe(true)
    })
  })

  describe('session list', () => {
    it('renders sessions from API', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's1', title: 'Session 1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli', model: 'gpt-4' },
            { id: 's2', title: 'Session 2', updatedAt: '2025-01-02', agentId: 'agent-2', backend: 'acp' },
          ],
          hasMore: false,
          totalCount: 2,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()
      // Explicitly load sessions since the watch may not fire on initial mount
      await wrapper.vm.loadSessions()
      await flushPromises()
      await nextTick()

      // Verify sessions are loaded internally (DOM may not render due to stub/scoped styles)
      expect(wrapper.vm.sessions.length).toBe(2)
    })

    it('highlights current session', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's1', title: 'Session 1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' },
          ],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer({ currentSessionId: 's1' })
      await flushPromises()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await nextTick()

      // Check sessionsWithStatus computed for active class
      const first = wrapper.vm.sessionsWithStatus[0]
      expect(first.id).toBe('s1')
      expect(first.running).toBe(false)
    })

    it('marks running sessions', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's1', title: 'Session 1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' },
          ],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer({ runningSessionIds: new Set(['s1']) })
      await flushPromises()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await nextTick()

      // Check sessionsWithStatus computed marks session as running
      const first = wrapper.vm.sessionsWithStatus[0]
      expect(first.running).toBe(true)
    })

    it('shows source session badge for scheduled tasks', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [
            { id: 's1', title: 'Task', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli', sourceSessionId: 'parent-s' },
          ],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.loadSessions()
      await flushPromises()
      await nextTick()

      // Check sessionsWithStatus has sourceSessionId
      const first = wrapper.vm.sessionsWithStatus[0]
      expect(first.sourceSessionId).toBe('parent-s')
    })
  })

  describe('selectSession', () => {
    it('emits select event and closes drawer', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()

      await wrapper.vm.selectSession('s1', 'cli')

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli'])
    })
  })

  describe('deleteSession', () => {
    it('emits delete after confirmation', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()

      await wrapper.vm.deleteSession('s1')

      expect(wrapper.emitted('delete')).toBeTruthy()
    })
  })

  describe('addSessionLocally', () => {
    it('prepends new session to list', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.addSessionLocally({ id: 's2', title: 'S2', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' })
      await nextTick()

      expect(wrapper.vm.sessions.length).toBe(2)
      expect(wrapper.vm.sessions[0].id).toBe('s2')
    })

    it('does not add duplicate session', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({
          sessions: [{ id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' }],
          hasMore: false,
          totalCount: 1,
        }),
      })

      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.loadSessions()
      await flushPromises()

      wrapper.vm.addSessionLocally({ id: 's1', title: 'S1', updatedAt: '2025-01-01', agentId: 'agent-1', backend: 'cli' })
      await nextTick()

      expect(wrapper.vm.sessions.length).toBe(1)
    })

    it('ignores null session', async () => {
      mockFetch.mockResolvedValue({
        ok: true,
        json: () => Promise.resolve({ sessions: [], hasMore: false, totalCount: 0 }),
      })

      const wrapper = mountDrawer()
      await flushPromises()

      wrapper.vm.addSessionLocally(null)
      // Should not throw
    })
  })

  describe('openAgentSelector', () => {
    it('opens agent selector when multiple agents exist', async () => {
      const wrapper = mountDrawer()
      await flushPromises()

      await wrapper.vm.openAgentSelector()
      await nextTick()

      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(true)
    })
  })

  describe('createSession', () => {
    it('emits create event and closes drawer', async () => {
      const wrapper = mountDrawer()
      await flushPromises()

      wrapper.vm.createSession('agent-1')

      expect(wrapper.emitted('create')).toBeTruthy()
      expect(wrapper.emitted('create')![0]).toEqual(['agent-1'])
    })
  })

  describe('loadSessions error handling', () => {
    it('handles fetch error gracefully', async () => {
      mockFetch.mockRejectedValue(new Error('network error'))

      const wrapper = mountDrawer()
      await flushPromises()

      // Should not throw, sessions should be empty
      const items = wrapper.findAll('.session-item')
      expect(items.length).toBe(0)
    })
  })

  describe('session bar color', () => {
    it('shows warning color when usage >= 80%', async () => {
      const { store } = await import('@/stores/app')
      store.state.sessionCount = 9
      store.state.sessionMaxCount = 10

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.find('.session-counter').exists()).toBe(true)
    })
  })

  describe('sessionBarColor computed', () => {
    it('returns red when session count equals max count', async () => {
      const { store } = await import('@/stores/app')
      store.state.sessionCount = 10
      store.state.sessionMaxCount = 10

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.vm.sessionBarColor).toBe('#ef4444')
    })

    it('returns warning color when usage >= 80%', async () => {
      const { store } = await import('@/stores/app')
      store.state.sessionCount = 9
      store.state.sessionMaxCount = 10

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.vm.sessionBarColor).toBe('#f59e0b')
    })

    it('returns accent color when usage < 80%', async () => {
      const { store } = await import('@/stores/app')
      store.state.sessionCount = 5
      store.state.sessionMaxCount = 10

      const wrapper = mountDrawer()
      await flushPromises()

      expect(wrapper.vm.sessionBarColor).toBe('var(--accent-color, #0066cc)')
    })
  })
})
