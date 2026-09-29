import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'
import SessionDrawer from '@/components/session/SessionDrawer.vue'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

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
    template: '<div class="header-stub"><slot name="actions" /><slot name="actions-end" /></div>',
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

  describe('shared-sessions entry button', () => {
    it('is hidden when the project has no shared conversation', async () => {
      const { useSessionShare } = await import('@/composables/useSessionShare')
      const { resetSessionShareState } = useSessionShare()
      resetSessionShareState()

      const wrapper = mountDrawer()
      expect(wrapper.find('[data-action="shared-sessions"]').exists()).toBe(false)
    })

    it('appears as soon as one conversation is shared', async () => {
      const { useSessionShare } = await import('@/composables/useSessionShare')
      const { resetSessionShareState, markShared } = useSessionShare()
      resetSessionShareState()

      const wrapper = mountDrawer()
      expect(wrapper.find('[data-action="shared-sessions"]').exists()).toBe(false)

      markShared('s1')
      await nextTick()
      expect(wrapper.find('[data-action="shared-sessions"]').exists()).toBe(true)

      resetSessionShareState()
    })

    it('disappears again when the last share is revoked', async () => {
      const { useSessionShare } = await import('@/composables/useSessionShare')
      const { resetSessionShareState, markShared, markUnshared } = useSessionShare()
      resetSessionShareState()
      markShared('s1')

      const wrapper = mountDrawer()
      expect(wrapper.find('[data-action="shared-sessions"]').exists()).toBe(true)

      markUnshared('s1')
      await nextTick()
      expect(wrapper.find('[data-action="shared-sessions"]').exists()).toBe(false)

      resetSessionShareState()
    })

    // The glyph must be the conversation+share mark, not a plain conversation.
    it('uses the share glyph, not a plain conversation glyph', () => {
      const src = readFileSync(
        join(__dirname, '..', 'SessionDrawer.vue'),
        'utf8',
      )
      expect(src).toContain('MessageSquareShare')
      expect(src).not.toContain('MessagesSquare')
    })
  })

  describe('rendering shell', () => {
    it('renders SessionList and SessionListHeader stubs', () => {
      const wrapper = mountDrawer()
      expect(wrapper.find('.session-list-stub').exists()).toBe(true)
      expect(wrapper.find('.header-stub').exists()).toBe(true)
    })

    // Regression guard: this component renders three teleporting overlays
    // (BottomSheet, AgentSelectorDrawer, SharedSessionsDrawer). Left as three
    // top-level nodes it is a FRAGMENT, and a fragment root cannot inherit
    // fallthrough attributes — a host binding v-show (a `style` fallthrough)
    // gets "Extraneous non-props attributes (style)" and silently keeps the
    // overlay visible. SessionSidebar shipped exactly that bug when a sibling
    // drawer was added next to its root div.
    it('has a single root element so hosts can apply v-show/class fallthrough', () => {
      const src = readFileSync(
        join(__dirname, '..', 'SessionDrawer.vue'),
        'utf8',
      )
      // The template body, between the outermost <template> tags.
      const body = src.slice(src.indexOf('<template>') + 10, src.indexOf('</template>'))
      // Count top-level element starts: lines with exactly two leading spaces
      // followed by a tag. Comments and blanks are ignored.
      const roots = body
        .split('\n')
        .filter((l) => /^ {2}<[A-Za-z]/.test(l))
      expect(roots).toHaveLength(1)
      expect(roots[0]).toContain('div')
    })
  })

  describe('pin button', () => {
    it('renders a pin button and emits pin on click', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      const pin = wrapper.find('.header-action-btn[data-action="pin"]')
      expect(pin.exists()).toBe(true)
      await pin.trigger('click')
      expect(wrapper.emitted('pin')).toBeTruthy()
    })

    it('does not render the pin button on narrow screen (pinning disabled on non-wide)', async () => {
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

    it('opens the agent selector even for a single agent', async () => {
      mockAgentsHolder.list = [{ id: 'agent-1', name: 'Agent One', backend: 'cli' }]
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListHeaderStub).vm.$emit('create')
      await nextTick()
      // Direct create would be a one-tap mis-tap risk on mobile; the selector
      // must open even with one agent. 即使只有一个智能体也必须弹选择器，
      // 避免一键误触直接建空会话。
      expect(wrapper.emitted('create')).toBeUndefined()
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(true)
    })
  })

  describe('session list event forwarding', () => {
    it('forwards select and closes the bottom sheet', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('select', 's1', 'cli')
      await nextTick()
      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli', undefined])
      expect(BottomSheetStub.methods.close).toHaveBeenCalled()
    })

    it('forwards the owning project path for cross-project selections', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('select', 's1', 'cli', '/proj/other')
      await nextTick()
      expect(wrapper.emitted('select')![0]).toEqual(['s1', 'cli', '/proj/other'])
    })

    it('forwards archive with backend', async () => {
      const wrapper = mountDrawer()
      await nextTick()
      wrapper.findComponent(SessionListStub).vm.$emit('archive', 's1', 'cli')
      expect(wrapper.emitted('archive')).toBeTruthy()
      expect(wrapper.emitted('archive')![0]).toEqual(['s1', 'cli'])
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

    it('opens the selector even for a single agent', async () => {
      mockAgentsHolder.list = [{ id: 'agent-1', name: 'Agent One', backend: 'cli' }]
      const wrapper = mountDrawer()
      await flushPromises()
      await wrapper.vm.openAgentSelector()
      await nextTick()
      // Direct create would be a one-tap mis-tap risk on mobile; the selector
      // must open even with one agent. 即使只有一个智能体也必须弹选择器。
      expect(wrapper.emitted('create')).toBeUndefined()
      expect(wrapper.vm.agentSelectorDrawer.isOpen.value).toBe(true)
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
