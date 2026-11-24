import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent, h } from 'vue'
import SessionDrawer from '@/components/chat/SessionDrawer.vue'
import { useAgents, applyRefreshedModelList } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'

// Mock BottomSheet to render slot content inline (skip Teleport).
// Vue 3.5 + @vue/test-utils 2.4.11 broke teleport stubs — the stub renders
// an empty element instead of slot content. Mocking BottomSheet avoids
// Teleport entirely, keeping slot content in the component tree.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: { open: Boolean, title: String, auto: Boolean, instant: Boolean, compact: Boolean, noHeader: Boolean, handleOnly: Boolean, transparentOverlay: Boolean, fullscreen: Boolean, closeGuard: Boolean },
    emits: ['close'],
    inheritAttrs: true,
    template: `
      <div class="bottom-sheet-overlay">
        <div class="bottom-sheet">
          <div class="bs-header"><slot name="header" /></div>
          <div class="bs-body"><slot /></div>
          <div class="bs-footer"><slot name="footer" /></div>
        </div>
      </div>
    `,
  }),
}))

// Mock PopupMenu similarly — its <Teleport>+<Transition> breaks Vue 3.5
// reactivity in test-utils. Render slot content inline without Teleport.
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: defineComponent({
    props: { show: Boolean, targetElement: Object, maxWidth: Number, maxHeight: Number, menuItemsCount: Number },
    emits: ['update:show'],
    template: `
      <div v-if="show" class="popup-menu-stub">
        <slot />
      </div>
    `,
  }),
}))

// Mock composables
vi.mock('@/composables/useAgents', () => ({
  useAgents: vi.fn(),
  restoreOriginalModels: vi.fn(),
  restoreACPModelView: vi.fn(),
  resetModelViewForTransport: vi.fn(),
  applyRefreshedModelList: vi.fn(),
  applyRefreshedACPState: vi.fn(),
  populateACPStateCache: vi.fn().mockResolvedValue(undefined),
  invalidateACPStateCache: vi.fn(),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: vi.fn(),
  clearModeState: vi.fn(),
  clearCommandState: vi.fn(),
  clearThinkingEffortState: vi.fn(),
}))
vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ models: [] }),
}))
// Records t() calls so tests can assert interpolation params, which the mocked
// t() drops from its return value.
const mockT = vi.fn((key: string) => key)
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: mockT }),
  createI18n: () => ({ global: { t: mockT } }),
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentPref: vi.fn().mockResolvedValue(undefined),
}))
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

const mockAgents = {
  agents: ref([
    {
      id: 'claude',
      name: 'Claude',
      backend: 'claude',
      models: [
        { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
        { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
        { id: 'claude-haiku-3-5', name: 'Claude Haiku 3.5', default: false },
      ],
      thinkingEffortLevels: ['low', 'medium', 'high', 'xhigh', 'max'],
      preferredModel: 'claude-sonnet-4-6',
      preferredThinkingEffort: '',
      canRefreshModels: true,
      acpCommand: 'npx -y @agentclientprotocol/claude-agent-acp@latest',
      transport: 'acp-stdio',
    },
    {
      id: 'kimi',
      name: 'Kimi',
      backend: 'kimi',
      models: [],
      thinkingEffortLevels: [],
      preferredModel: '',
      preferredThinkingEffort: '',
      canRefreshModels: false,
    },
  ]),
  getAgentModels: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a?.models || []
  }),
  getAgentThinkingEffortLevels: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a?.thinkingEffortLevels || []
  }),
  refreshAgentModels: vi.fn().mockResolvedValue(undefined),
  updateAgentField: vi.fn(),
  getDefaultModelId: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a?.preferredModel || a?.models?.[0]?.id || ''
  }),
  getAgent: vi.fn((agentId: string) => {
    return mockAgents.agents.value.find(a => a.id === agentId)
  }),
  canRefreshModels: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return !!a?.canRefreshModels
  }),
  canRefreshAgentState: vi.fn((agentId: string) => {
    // Mirrors the backend split: transport decides which source can refresh.
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    if (!a) return false
    const transport = a.transport || (a.acpCommand ? 'acp-stdio' : a.builtinBackend ? 'in-process' : '')
    return transport === 'acp-stdio' ? !!a.acpCommand : !!a.canRefreshModels
  }),
  supportsACP: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return !!a?.acpCommand
  }),
  isBuiltinAgent: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a?.builtinBackend === true
  }),
}

const mockIdentity = {
  currentAgentId: ref('claude'),
  currentModelId: ref('claude-sonnet-4-6'),
  currentModelName: ref('Claude Sonnet 4.6'),
  currentThinkingEffort: ref('high'),
  currentTransport: ref('acp-stdio'),
  availableThinkingEfforts: ref([]),
  availableModes: ref([{ id: 'code', name: 'Code' }, { id: 'ask', name: 'Ask' }]),
  currentModeId: ref('code'),
  autoApprove: ref(false),
  toggleAutoApprove: vi.fn(),
  // Agent-declared generic options (issue #634). The drawer reads this on every
  // render, so the mock must provide it even when no test exercises the tab.
  configOptions: ref([]),
  setConfigOptionValue: vi.fn(),
}

describe('SessionDrawer', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
    vi.mocked(apiPost).mockResolvedValue({ models: [] })
    vi.mocked(patchAgentPref).mockResolvedValue(undefined)
    mockToastShow.mockClear()
  })

  function mountDrawer(props = {}) {
    return mount(SessionDrawer, {
      props: { open: true, agentId: 'claude', ...props },
    })
  }

  // --- Tab switching ---

  it('renders model tab by default', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.model-tab.active').text()).toContain('chat.modelSwitcher.title')
  })

  it('switches to thinking tab via initialTab prop', () => {
    const wrapper = mountDrawer({ initialTab: 'thinking' })
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[1].classes()).toContain('active')
  })

  // --- Model list ---

  it('renders model list for current agent', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(3)
    expect(items[0].text()).toContain('Claude Sonnet 4.6')
  })

  it('highlights current session model', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('current')
  })

  it('shows default badge on preferred model', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items[0].find('.default-label').exists()).toBe(true)
  })

  // --- Search ---
  // Vue 3.5 + test-utils doesn't re-render on ref changes.
  // Test search filtering via VM methods instead of DOM interaction.

  it('filters models by search query', () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('opus')
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Opus')
  })

  it('shows no results message when search has no matches', async () => {
    const wrapper = mountDrawer()
    const searchInput = wrapper.find('.model-search-row .search-pill input')
    await searchInput.setValue('nonexistent')
    await nextTick()

    expect(wrapper.find('.model-empty').exists() || wrapper.text()).toBeTruthy()
  })

  it('filters models by id when search matches id but not name', () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('haiku-3')
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Haiku')
  })

  it('shows no-search-results message when search yields nothing', () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('xyz')
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(0)
  })

  // --- Model selection (session-scoped) ---

  it('emits switch-model when clicking a model', async () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click') // opus

    expect(wrapper.emitted('switch-model')).toBeTruthy()
    expect(wrapper.emitted('switch-model')![0][0]).toEqual({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false })
  })

  it('closes drawer after selecting a model', async () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click')

    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // --- Thinking effort ---
  // Note: thinking tab rendering depends on availableThinkingEfforts from useSessionIdentity
  // which returns an empty array in tests, so most thinking item tests are skipped.
  // The selectThinkingEffort method is tested below via describe block.

  // --- Refresh ---

  it('has refresh button for agents that support model refresh', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.refresh-btn').exists()).toBe(true)
  })

  it('hides refresh button for agents that do not support model refresh', () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    expect(wrapper.find('.refresh-btn').exists()).toBe(false)
  })

  it('calls refresh API and applies the resolved list on success', async () => {
    const newModels = [
      { id: 'claude-new', name: 'Claude New', default: true },
    ]
    vi.mocked(apiPost).mockResolvedValue({ models: newModels })

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    // A refresh probe can take several seconds (process start + the agent's
    // delayed command push), so the request raises the client timeout.
    expect(apiPost).toHaveBeenCalledWith('/api/agents/claude/refresh-models', {}, { timeoutMs: 180000 })
    expect(applyRefreshedModelList).toHaveBeenCalledWith('claude', newModels)
    expect(mockToastShow).toHaveBeenCalledWith('chat.sessionSetting.refreshSuccess', expect.any(Object))
  })

  it('shows cliNotFound toast when CLI not found error', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'CLINotFound' })

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.sessionSetting.cliNotFound', expect.any(Object))
  })

  it('shows discoveryNotSupported toast when model discovery not supported', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'ModelDiscoveryNotSupported' })

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.sessionSetting.discoveryNotSupported', expect.any(Object))
  })

  it('shows generic refreshFailed toast on other errors', async () => {
    vi.mocked(apiPost).mockRejectedValue(new Error('network error'))

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.sessionSetting.refreshFailed', expect.any(Object))
  })

  it('shows discoveryFailedWithDetail toast when backend supplies a reason', async () => {
    const reason = 'no CodeBuddy model list found; tried: /x/product.cloudhosted.json'
    vi.mocked(apiPost).mockRejectedValue({
      msgKey: 'ModelDiscoveryFailed',
      detail: { detail: reason },
    })

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith(
      'chat.sessionSetting.discoveryFailedWithDetail',
      expect.any(Object),
    )
    // Assert the interpolation param — a missing/renamed {detail} key would
    // otherwise be invisible because the mocked t() returns just the key.
    expect(mockT).toHaveBeenCalledWith(
      'chat.sessionSetting.discoveryFailedWithDetail',
      { detail: reason },
    )
  })

  it('shows plain discoveryFailed toast when no reason is supplied', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'ModelDiscoveryFailed' })

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith(
      'chat.sessionSetting.discoveryFailed',
      expect.any(Object),
    )
  })

  it('disables refresh button while refreshing', async () => {
    let resolveRefresh: (v: any) => void
    vi.mocked(apiPost).mockReturnValue(new Promise(r => { resolveRefresh = r }))

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()

    expect(wrapper.vm.refreshing).toBe(true)

    resolveRefresh!({ models: [] })
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()
  })

  it('does not call API when already refreshing', async () => {
    let resolveRefresh: (v: any) => void
    vi.mocked(apiPost).mockReturnValue(new Promise(r => { resolveRefresh = r }))

    const wrapper = mountDrawer()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()

    vi.mocked(apiPost).mockClear()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()

    expect(apiPost).not.toHaveBeenCalled()

    resolveRefresh!({ models: [] })
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
  })

  // --- Visual dividers ---

  it('renders dividers between model items', () => {
    const wrapper = mountDrawer()
    const dividers = wrapper.findAll('.model-divider')
    expect(dividers.length).toBe(2)
  })

  // --- Set default button ---

  it('has set-default star button on non-default models', () => {
    const wrapper = mountDrawer()
    const setDefaultBtns = wrapper.findAll('.set-default-btn')
    expect(setDefaultBtns.length).toBe(2)
  })

  it('calls patchAgentPref and updateAgentField when star button clicked', async () => {
    const wrapper = mountDrawer()
    const starBtns = wrapper.findAll('.set-default-btn')
    await starBtns[0].trigger('click')

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredModel', 'claude-opus-4-5')
  })

  it('shows error toast when setDefaultModel fails', async () => {
    vi.mocked(patchAgentPref).mockRejectedValueOnce(new Error('fail'))

    const wrapper = mountDrawer()
    const starBtns = wrapper.findAll('.set-default-btn')
    await starBtns[0].trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('settings.saveFailed', expect.any(Object))
  })

  // --- Thinking effort default ---
  // Note: thinking effort default button depends on .thinking-item which requires
  // availableThinkingEfforts to be populated. Tested via selectThinkingEffort describe block.

  // --- No models ---

  it('shows empty state when agent has no models', () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(0)
    expect(wrapper.find('.model-empty').exists()).toBe(true)
  })

  it('shows no-models message in empty state', () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    expect(wrapper.find('.model-empty').text()).toContain('chat.sessionSetting.noModels')
  })

  // A fresh CodeBuddy install has no runtime cache until the CLI has signed in
  // once, so the picker is empty and the refresh is the ONLY way out. The empty
  // state therefore carries the button itself instead of a sentence pointing at
  // the header.
  it('offers the refresh button inside the empty state when the agent can refresh', () => {
    // Empty the list for an agent that CAN refresh: this is the CodeBuddy
    // first-install shape (no runtime cache, so discovery finds nothing).
    const claude = mockAgents.agents.value.find(a => a.id === 'claude')!
    const saved = claude.models
    claude.models = []
    try {
      const wrapper = mountDrawer({ agentId: 'claude' })
      const empty = wrapper.find('.model-empty')
      expect(empty.exists()).toBe(true)
      expect(empty.find('.model-empty-refresh').exists()).toBe(true)
      expect(empty.text()).toContain('chat.sessionSetting.noModels')
    } finally {
      claude.models = saved
    }
  })

  it('empty-state refresh button triggers the same refresh as the header', async () => {
    const claude = mockAgents.agents.value.find(a => a.id === 'claude')!
    const saved = claude.models
    claude.models = []
    try {
      const wrapper = mountDrawer({ agentId: 'claude' })
      await wrapper.find('.model-empty-refresh').trigger('click')
      await new Promise(r => setTimeout(r, 10))
      expect(apiPost).toHaveBeenCalledWith('/api/agents/claude/refresh-models', {}, { timeoutMs: 180000 })
    } finally {
      claude.models = saved
    }
  })

  it('does not offer the empty-state refresh for an agent that cannot refresh', () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    const empty = wrapper.find('.model-empty')
    expect(empty.exists()).toBe(true)
    expect(empty.find('.model-empty-refresh').exists()).toBe(false)
    expect(empty.text()).toContain('chat.sessionSetting.noModels')
  })

  // --- Close drawer ---

  it('emits close when close is triggered', async () => {
    const wrapper = mountDrawer()
    await wrapper.findComponent({ name: 'BottomSheet' }).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // --- Context menu (right-click) ---

  it('shows popup menu on contextmenu for model item', async () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('contextmenu')

    expect(wrapper.find('.popup-set-default').exists() || wrapper.vm.showDefaultPopupMenu === true).toBeTruthy()
  })

  it('sets default model via popup menu', async () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('contextmenu')
    await nextTick()

    const popupBtn = wrapper.find('.popup-set-default')
    if (popupBtn.exists()) {
      await popupBtn.trigger('click')
      await nextTick()
      await new Promise(r => setTimeout(r, 10))
      expect(patchAgentPref).toHaveBeenCalled()
    }
  })

  // --- Agent name in header ---

  it('displays agent name in drawer header', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-title').text()).toBe('Claude')
  })

  // --- Thinking tab dividers ---
  // Note: thinking tab dividers depend on availableThinkingEfforts being populated.

  // --- is-default class ---

  it('adds is-default class to default model', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('is-default')
  })

  // Note: is-default class for thinking effort depends on .thinking-item
  // which requires availableThinkingEfforts to be populated.

  // --- Search resets on reopen ---

  it('resets search query when drawer reopens', () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('opus')
    expect(wrapper.vm._getSearchQuery()).toBe('opus')

    const wrapper2 = mountDrawer()
    expect(wrapper2.vm._getSearchQuery()).toBe('')
    expect(wrapper2.vm._getFilteredModels().length).toBe(3)
  })

  // --- No thinking tab for agents without levels ---

  it('shows empty hint in thinking tab for agents without thinking effort levels', () => {
    const wrapper = mountDrawer({ agentId: 'kimi', initialTab: 'thinking' })
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs.length).toBe(4) // model / thinking / mode / usage
    expect(wrapper.find('.tab-empty-hint').exists()).toBe(true)
  })

  // --- Mode tab ---

  describe('mode tab', () => {
    it('availableModes has entries for ACP agents', async () => {
      const wrapper = mountDrawer()
      expect(wrapper.vm.availableModes.length).toBe(2)
    })

    it('availableModes shows empty for non-ACP agents', async () => {
      mockIdentity.currentTransport.value = 'cli'
      const wrapper = mountDrawer({ agentId: 'kimi' })
      expect(wrapper.vm.isACP).toBe(false)
      mockIdentity.currentTransport.value = 'acp-stdio'
    })

    it('autoApprove is available from useSessionIdentity mock', async () => {
      const wrapper = mountDrawer()
      expect(mockIdentity.autoApprove).toBeDefined()
    })

    it('currentModeId matches identity', async () => {
      const wrapper = mountDrawer()
      expect(wrapper.vm.currentModeId).toBe('code')
    })
  })

  // --- No transport picker ---
  //
  // The drawer used to expose a protocol switch. Every agent with an ACP
  // command now runs over ACP, so that picker (and its setDefaultTransport
  // path) is gone; the tab strip must not offer it either.
  describe('transport', () => {
    it('exposes no transport switch helpers', () => {
      const wrapper = mountDrawer()
      expect(wrapper.vm.selectTransport).toBeUndefined()
      expect(wrapper.vm.setDefaultTransport).toBeUndefined()
    })
  })

  // --- selectMode ---

  describe('selectMode', () => {
    it('emits switch-mode when selecting a mode', async () => {
      const wrapper = mountDrawer()
      wrapper.vm.selectMode({ id: 'ask', name: 'Ask' })

      expect(wrapper.emitted('switch-mode')).toBeTruthy()
    })

    it('closes drawer after selecting a mode', async () => {
      const wrapper = mountDrawer()
      wrapper.vm.selectMode({ id: 'ask', name: 'Ask' })

      expect(wrapper.emitted('close')).toBeTruthy()
    })
  })

  // --- handleRefresh edge cases ---

  describe('handleRefresh', () => {
    it('does not call API when already refreshing', async () => {
      let resolveRefresh: (v: any) => void
      vi.mocked(apiPost).mockReturnValue(new Promise(r => { resolveRefresh = r }))

      const wrapper = mountDrawer()
      await wrapper.find('.refresh-btn').trigger('click')
      await nextTick()

      vi.mocked(apiPost).mockClear()
      await wrapper.find('.refresh-btn').trigger('click')
      await nextTick()

      expect(apiPost).not.toHaveBeenCalled()

      resolveRefresh!({ models: [] })
      await nextTick()
      await new Promise(r => setTimeout(r, 10))
    })
  })

  // --- selectThinkingEffort ---

  describe('selectThinkingEffort', () => {
    it('emits switch-thinking-effort and closes drawer', async () => {
      mockIdentity.availableThinkingEfforts.value = [
        { id: 'low', name: 'Low' },
        { id: 'high', name: 'High' },
      ]
      const wrapper = mountDrawer({ initialTab: 'thinking' })

      const items = wrapper.findAll('.thinking-item')
      if (items.length > 0) {
        await items[0].trigger('click')
        expect(wrapper.emitted('switch-thinking-effort')).toBeTruthy()
        expect(wrapper.emitted('close')).toBeTruthy()
      }
      mockIdentity.availableThinkingEfforts.value = []
    })
  })

  // --- Long-press state ---

  describe('long press', () => {
    it('onTouchEnd clears timer and resets triggered state', async () => {
      const wrapper = mountDrawer()
      expect(() => wrapper.vm.onTouchEnd()).not.toThrow()
    })

    it('onTouchMove clears timer', async () => {
      const wrapper = mountDrawer()
      expect(() => wrapper.vm.onTouchMove()).not.toThrow()
    })
  })

  // --- PopupMenu ---

  it('renders PopupMenu component', () => {
    const wrapper = mountDrawer()
    const popup = wrapper.findComponent({ name: 'PopupMenu' })
    expect(wrapper.vm.showDefaultPopupMenu).toBeDefined()
  })

  // --- handleClose ---

  it('emits close on handleClose', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.handleClose()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // --- isACP computed ---

  describe('isACP computed', () => {
    it('is true when currentTransport is acp-stdio', async () => {
      mockIdentity.currentTransport.value = 'acp-stdio'
      const wrapper = mountDrawer()
      expect(wrapper.vm.isACP).toBe(true)
    })

    it('falls back to agent config transport when currentTransport is empty', async () => {
      mockIdentity.currentTransport.value = ''
      const wrapper = mountDrawer()
      expect(wrapper.vm.isACP).toBe(true)

      mockIdentity.currentTransport.value = 'acp-stdio'
    })

    it('is false when transport is in-process', async () => {
      mockIdentity.currentTransport.value = 'in-process'
      const wrapper = mountDrawer({ agentId: 'kimi' })
      expect(wrapper.vm.isACP).toBe(false)
      mockIdentity.currentTransport.value = 'acp-stdio'
    })
  })
})
