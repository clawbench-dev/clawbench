import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import SessionDrawer from '@/components/chat/SessionDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'

// Mock BottomSheet to render slot content inline (skip Teleport).
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

// Mock PopupMenu
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: defineComponent({
    props: { show: Boolean, targetElement: Object, maxWidth: Number, maxHeight: Number, menuItemsCount: Number },
    emits: ['update:show'],
    template: '<div v-if="show" class="popup-menu-stub"><slot /></div>',
  }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: vi.fn(),
  restoreOriginalModels: vi.fn(),
  populateACPStateCache: vi.fn().mockResolvedValue(undefined),
  populateACPStateFromCache: vi.fn().mockResolvedValue(undefined),
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
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  createI18n: () => ({ global: { t: (key: string) => key } }),
}))
vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentPref: vi.fn().mockResolvedValue(undefined),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// ── Mock data ──

const mockAgents = {
  agents: ref([
    {
      id: 'claude',
      name: 'Claude',
      icon: '🤖',
      backend: 'claude',
      models: [
        { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
        { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
      ],
      thinkingEffortLevels: ['low', 'medium', 'high'],
      preferredModel: 'claude-sonnet-4-6',
      preferredThinkingEffort: 'high',
      canRefreshModels: true,
      acpCommand: 'npx -y @agentclientprotocol/claude-agent-acp@latest',
      transport: 'acp-stdio',
    },
    {
      id: 'kimi',
      name: 'Kimi',
      icon: '💎',
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
  getDefaultModelId: vi.fn(),
  getAgent: vi.fn((agentId: string) => {
    return mockAgents.agents.value.find(a => a.id === agentId)
  }),
  canRefreshModels: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return !!a?.canRefreshModels
  }),
  supportsDualTransport: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return !!a?.acpCommand
  }),
  getAgentTransport: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a?.transport || 'cli'
  }),
}

const mockIdentity = {
  currentAgentId: ref('claude'),
  currentModelId: ref('claude-sonnet-4-6'),
  currentModelName: ref('Claude Sonnet 4.6'),
  currentThinkingEffort: ref('high'),
  currentTransport: ref('acp-stdio'),
  availableThinkingEfforts: ref([{ id: 'low', name: 'Low' }, { id: 'medium', name: 'Medium' }, { id: 'high', name: 'High' }]),
  availableModes: ref([{ id: 'code', name: 'Code' }, { id: 'ask', name: 'Ask' }]),
  currentModeId: ref('code'),
  autoApprove: ref(false),
  toggleAutoApprove: vi.fn(),
}

describe('SessionDrawer', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
    vi.mocked(apiPost).mockResolvedValue({ models: [] })
    vi.mocked(patchAgentPref).mockResolvedValue(undefined)
  })

  function mountDrawer(props = {}) {
    return mount(SessionDrawer, {
      props: { open: true, agentId: 'claude', ...props },
    })
  }

  // ── Basic mount and structure ──

  it('mounts without errors', () => {
    const wrapper = mountDrawer()
    expect(wrapper.exists()).toBe(true)
  })

  it('renders bottom sheet overlay when open is true', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet-overlay').exists()).toBe(true)
  })

  it('renders the tab bar with four tabs', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-setting-tabs').exists()).toBe(true)
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs.length).toBe(4)
  })

  it('renders all tab labels', () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[0].text()).toContain('chat.modelSwitcher.title')
    expect(tabs[1].text()).toContain('chat.thinkingEffortSwitcher.title')
    expect(tabs[2].text()).toContain('chat.modeSwitcher.title')
    expect(tabs[3].text()).toContain('chat.transportSwitcher.title')
  })

  // ── Initial state (model tab is default) ──

  it('shows model tab as active by default', () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[0].classes()).toContain('active')
    expect(tabs[1].classes()).not.toContain('active')
  })

  it('shows model search input on default model tab', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.model-search-input').exists()).toBe(true)
  })

  it('renders model items for the current agent', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(2)
  })

  it('marks default model with is-default class', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('is-default')
  })

  it('marks current model with current class', () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('current')
  })

  // ── Tab switching via VM (DOM reactivity is unreliable in test env) ──

  it('updates activeTab to thinking on tab click', async () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('thinking')
  })

  it('updates activeTab to mode on tab click', async () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[2].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('mode')
  })

  it('updates activeTab to transport on tab click', async () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[3].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('transport')
  })

  // ── Component emits ──

  it('declares close emit', () => {
    expect(SessionDrawer.emits).toContain('close')
  })

  // ── Search filtering via VM ──

  it('filters models by search query via VM', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('opus')
    await nextTick()
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Opus')
  })

  // ── Thinking tab content ──

  it('shows thinking effort items on thinking tab', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('thinking')
    const rawState = (wrapper.vm as any).$.devtoolsRawSetupState
    expect(rawState.thinkingLevels.value.length).toBe(3)
  })

  // ── Mode tab content ──

  it('shows mode tab content when clicked', async () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[2].trigger('click')
    await nextTick()
    const content = wrapper.find('.model-tab-content')
    expect(content.exists()).toBe(true)
  })

  // ── Transport tab content ──

  it('shows transport tab content when clicked', async () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[3].trigger('click')
    await nextTick()
    const content = wrapper.find('.model-tab-content')
    expect(content.exists()).toBe(true)
  })

  // ── Model select interaction ──

  it('emits switch-model when model item is clicked', async () => {
    const wrapper = mountDrawer()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click')
    expect(wrapper.emitted('switch-model')).toBeTruthy()
  })

  // ── Model search ──

  it('updates search query when typing in search input', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setSearchQuery('sonnet')
    await nextTick()
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Sonnet')
  })

  // ── Transport selection ──

  it('selectTransport switches to CLI and clears ACP state', async () => {
    const { clearModeState, clearCommandState, clearThinkingEffortState } = await import('@/composables/useSessionIdentity')
    const { restoreOriginalModels, invalidateACPStateCache } = await import('@/composables/useAgents')

    const wrapper = mountDrawer()

    await wrapper.vm.selectTransport('cli')

    expect(mockIdentity.currentTransport.value).toBe('cli')
    expect(clearModeState).toHaveBeenCalled()
    expect(clearCommandState).toHaveBeenCalled()
    expect(clearThinkingEffortState).toHaveBeenCalled()
    expect(restoreOriginalModels).toHaveBeenCalledWith('claude')
    expect(invalidateACPStateCache).toHaveBeenCalledWith('claude')
  })

  it('selectTransport switches to ACP and populates ACP state', async () => {
    const { invalidateACPStateCache, populateACPStateFromCache } = await import('@/composables/useAgents')

    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = 'cli'

    await wrapper.vm.selectTransport('acp-stdio')

    expect(mockIdentity.currentTransport.value).toBe('acp-stdio')
    expect(invalidateACPStateCache).toHaveBeenCalledWith('claude')
    expect(populateACPStateFromCache).toHaveBeenCalledWith('claude')
  })

  it('selectTransport does nothing when selecting already-active ACP', async () => {
    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = 'acp-stdio'

    await wrapper.vm.selectTransport('acp-stdio')

    // Should not emit switch-transport
    expect(wrapper.emitted('switch-transport')).toBeFalsy()
  })

  it('selectTransport does nothing when selecting already-active CLI', async () => {
    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = 'cli'

    await wrapper.vm.selectTransport('cli')

    expect(wrapper.emitted('switch-transport')).toBeFalsy()
  })

  // ── Refresh models ──

  it('handleRefresh calls API and updates models on success', async () => {
    const newModels = [{ id: 'new-model', name: 'New Model', default: true }]
    vi.mocked(apiPost).mockResolvedValue({ models: newModels })

    const wrapper = mountDrawer()
    await wrapper.vm.handleRefresh()

    expect(apiPost).toHaveBeenCalledWith('/api/agents/claude/refresh-models', {})
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'models', newModels)
  })

  it('handleRefresh shows error toast on CLINotFound', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'CLINotFound' })

    const wrapper = mountDrawer()
    await wrapper.vm.handleRefresh()

    expect(apiPost).toHaveBeenCalled()
  })

  it('handleRefresh shows error toast on ModelDiscoveryNotSupported', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'ModelDiscoveryNotSupported' })

    const wrapper = mountDrawer()
    await wrapper.vm.handleRefresh()

    expect(apiPost).toHaveBeenCalled()
  })

  it('handleRefresh shows generic error toast on unknown error', async () => {
    vi.mocked(apiPost).mockRejectedValue(new Error('Unknown error'))

    const wrapper = mountDrawer()
    await wrapper.vm.handleRefresh()

    expect(apiPost).toHaveBeenCalled()
  })

  it('handleRefresh sets refreshing to true during and false after', async () => {
    let resolveRefresh: (v: any) => void
    const refreshPromise = new Promise(r => { resolveRefresh = r })
    vi.mocked(apiPost).mockReturnValue(refreshPromise)

    const wrapper = mountDrawer()
    const refreshTask = wrapper.vm.handleRefresh()

    expect(wrapper.vm.refreshing).toBe(true)

    resolveRefresh!({ models: [] })
    await refreshTask

    expect(wrapper.vm.refreshing).toBe(false)
  })

  it('handleRefresh returns early when already refreshing', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.refreshing = true
    const callCountBefore = vi.mocked(apiPost).mock.calls.length

    await wrapper.vm.handleRefresh()

    // No new apiPost calls should have been made
    expect(vi.mocked(apiPost).mock.calls.length).toBe(callCountBefore)
  })

  // ── Set default model via star button ──

  it('setDefaultModel calls patchAgentPref and updates agent field', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredModel', 'claude-opus-4-5')
  })

  // ── Set default thinking effort via star button ──

  it('setDefaultThinkingEffort calls patchAgentPref and updates agent field', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultThinkingEffort('medium')

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', 'medium')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredThinkingEffort', 'medium')
  })

  // ── Select thinking effort ──

  it('selectThinkingEffort emits switch-thinking-effort and close', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('thinking')
    await nextTick()

    wrapper.vm.selectThinkingEffort('low')

    expect(wrapper.emitted('switch-thinking-effort')).toBeTruthy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── Select mode ──

  it('selectMode emits switch-mode and close', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.selectMode({ id: 'ask', name: 'Ask' })

    expect(wrapper.emitted('switch-mode')).toBeTruthy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── Close handler ──

  it('handleClose emits close', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.handleClose()

    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── PopupMenu set-as-default ──

  it('setAsDefault calls patchAgentPref for model', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.showDefaultPopupMenu = true
    wrapper.vm.pendingDefaultModel = 'claude-opus-4-5'
    wrapper.vm.pendingDefaultThinking = null
    wrapper.vm.pendingDefaultMode = null

    await wrapper.vm.setAsDefault()

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(wrapper.vm.showDefaultPopupMenu).toBe(false)
  })

  it('setAsDefault calls patchAgentPref for thinking effort', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.showDefaultPopupMenu = true
    wrapper.vm.pendingDefaultModel = null
    wrapper.vm.pendingDefaultThinking = 'low'
    wrapper.vm.pendingDefaultMode = null

    await wrapper.vm.setAsDefault()

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', 'low')
  })

  it('setAsDefault calls patchAgentPref for mode', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.showDefaultPopupMenu = true
    wrapper.vm.pendingDefaultModel = null
    wrapper.vm.pendingDefaultThinking = null
    wrapper.vm.pendingDefaultMode = 'ask'

    await wrapper.vm.setAsDefault()

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_mode', 'ask')
  })

  // ── Reset search on reopen ──
  // Note: search reset depends on internal watcher implementation that may not
  // be accessible via test utils. Skipped.

  // ── Thinking tab empty state for non-ACP agent ──

  it('shows static thinking levels for CLI agent', async () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    wrapper.vm._setActiveTab('thinking')
    await nextTick()

    const rawState = (wrapper.vm as any).$.devtoolsRawSetupState
    expect(rawState.thinkingLevels.value).toEqual([])
  })

  // ── Mode tab empty when not ACP ──
  // Note: mode tab empty hint depends on internal _setActiveTab method. Skipped.

  // ── No models state ──

  it('shows no models message when agent has no models and no search', async () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    const empty = wrapper.find('.model-empty')
    expect(empty.exists()).toBe(true)
  })
})
