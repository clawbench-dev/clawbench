import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
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

const {
  mockLoadAgents,
  mockAgentsHolder,
  wideScreen,
  BottomSheetStub,
  AgentSelectorDrawerStub,
  SessionListStub,
  SessionListHeaderStub,
} = vi.hoisted(() => ({
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockAgentsHolder: { list: [] as any[] },
  wideScreen: { isWideScreen: null as any, leftTab: null as any },
  BottomSheetStub: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet-stub"><slot name="header" /><slot /></div>',
    methods: { close: vi.fn() },
  },
  AgentSelectorDrawerStub: {
    name: 'AgentSelectorDrawer',
    template: '<div class="agent-selector-drawer-stub" />',
    methods: { preload: vi.fn() },
  },
  SessionListStub: {
    name: 'SessionList',
    template: '<div class="session-list-stub" />',
    methods: { loadSessions: vi.fn(), addSessionLocally: vi.fn() },
  },
  SessionListHeaderStub: {
    name: 'SessionListHeader',
    template: '<div class="header-stub"><slot name="actions" /></div>',
  },
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: { value: mockAgentsHolder.list },
    loadAgents: mockLoadAgents,
  }),
}))

vi.mock('@/composables/useWideScreenLayout', async () => {
  const { ref } = await import('vue')
  wideScreen.isWideScreen = ref(true)
  wideScreen.leftTab = ref('browse')
  return {
    useWideScreenLayout: () => ({ isWideScreen: wideScreen.isWideScreen }),
    getWideScreenState: () => ({ isWideScreen: wideScreen.isWideScreen, leftTab: wideScreen.leftTab }),
  }
})

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (d: string) => d || 'now',
}))

// Stub child components
vi.mock('@/components/common/BottomSheet.vue', () => ({ default: BottomSheetStub }))

vi.mock('@/components/common/AgentSelectorDrawer.vue', () => ({ default: AgentSelectorDrawerStub }))

vi.mock('@/components/session/SessionList.vue', () => ({ default: SessionListStub }))

vi.mock('@/components/session/SessionListHeader.vue', () => ({ default: SessionListHeaderStub }))

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
    mockAgentsHolder.list = [
      { id: 'agent-1', name: 'Agent One', backend: 'cli' },
      { id: 'agent-2', name: 'Agent Two', backend: 'acp' },
    ]
    wideScreen.isWideScreen.value = true
  })

  describe('rendering shell', () => {
    it('renders SessionList and SessionListHeader stubs', () => {
      const wrapper = mountDrawer()
      expect(wrapper.find('.session-list-stub').exists()).toBe(true)
      expect(wrapper.find('.header-stub').exists()).toBe(true)
    })
  })

  describe('pin button', () => {
    it('renders a pin button on wide screen and emits pin on click', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      const pin = wrapper.find('.header-action-btn[data-action="pin"]')
      expect(pin.exists()).toBe(true)
      await pin.trigger('click')
      expect(wrapper.emitted('pin')).toBeTruthy()
    })

    it('does not render the pin button on narrow screen', async () => {
      wideScreen.isWideScreen.value = false
      const wrapper = mountDrawer()
      await nextTick()
      expect(wrapper.find('.header-action-btn[data-action="pin"]').exists()).toBe(false)
    })
  })

  describe('header action forwarding', () => {
    it('forwards open-search to open-session-search', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListHeaderStub).vm.$emit('open-search')
      expect(wrapper.emitted('open-session-search')).toBeTruthy()
    })

    it('opens the agent selector for create when multiple agents exist', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListHeaderStub).vm.$emit('create')
      await nextTick()
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(true)
    })

    it('emits create directly for a single agent', async () => {
      mockAgentsHolder.list = [{ id: 'agent-1', name: 'Agent One', backend: 'cli' }]
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListHeaderStub).vm.$emit('create')
      await nextTick()
      expect(wrapper.emitted('create')).toBeTruthy()
      expect(wrapper.emitted('create')![0]).toEqual(['agent-1'])
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(false)
    })
  })

  describe('session list event forwarding', () => {
    it('forwards select and closes the bottom sheet', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('select', 's1', 'cli')
      await nextTick()
      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli'])
      expect(BottomSheetStub.methods.close).toHaveBeenCalled()
    })

    it('forwards archive', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('archive', 's1')
      expect(wrapper.emitted('archive')).toBeTruthy()
      expect(wrapper.emitted('archive')![0]).toEqual(['s1'])
    })

    it('forwards destroy', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('destroy', 's1')
      expect(wrapper.emitted('destroy')).toBeTruthy()
      expect(wrapper.emitted('destroy')![0]).toEqual(['s1'])
    })
  })

  describe('agent selector / create', () => {
    it('opens the selector when multiple agents exist', async () => {
      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.openAgentSelector()
      await nextTick()
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(true)
    })

    it('emits create directly for a single agent', async () => {
      mockAgentsHolder.list = [{ id: 'agent-1', name: 'Agent One', backend: 'cli' }]
      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.openAgentSelector()
      await nextTick()
      expect(wrapper.emitted('create')).toBeTruthy()
      expect(wrapper.emitted('create')![0]).toEqual(['agent-1'])
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(false)
    })
  })

  describe('open watcher', () => {
    it('reloads sessions and agents when the drawer opens', async () => {
      const wrapper = mountDrawer({ open: false })
      await flushPromises()
      await wrapper.setProps({ open: true })
      await flushPromises()
      expect(mockLoadAgents).toHaveBeenCalled()
      expect(SessionListStub.methods.loadSessions).toHaveBeenCalled()
    })
  })

  describe('addSessionLocally', () => {
    it('forwards to the SessionList stub', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      const session = { id: 's2', title: 'S2', updatedAt: '2025-01-02', agentId: 'agent-1', backend: 'cli' }
      wrapper.vm.addSessionLocally(session)
      expect(SessionListStub.methods.addSessionLocally).toHaveBeenCalledWith(session)
    })
  })

  describe('lifecycle', () => {
    it('unmounts cleanly without throwing', async () => {
      const wrapper = mountDrawer()
      await flushPromises()
      expect(() => wrapper.unmount()).not.toThrow()
    })
  })
})
