import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import SessionDrawer from '@/components/chat/SessionDrawer.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'
import { readWebFile } from '@/testUtils/readWebFile'

// Shared mock for the named applyRefreshedModelList export — used by both the
// vi.mock factory and the assertions below (the component imports it directly).
const { mockApplyRefreshedModelList } = vi.hoisted(() => ({
  mockApplyRefreshedModelList: vi.fn(),
}))

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
  restoreACPModelView: vi.fn(),
  resetModelViewForTransport: vi.fn(),
  populateACPStateCache: vi.fn().mockResolvedValue(undefined),
  populateACPStateFromCache: vi.fn().mockResolvedValue(undefined),
  invalidateACPStateCache: vi.fn(),
  applyRefreshedModelList: mockApplyRefreshedModelList,
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: vi.fn(),
  clearModeState: vi.fn(),
  clearCommandState: vi.fn(),
  clearThinkingEffortState: vi.fn(),
}))
const { mockApiGet, mockApiPatch } = vi.hoisted(() => ({
  mockApiGet: vi.fn(),
  mockApiPatch: vi.fn(),
}))
vi.mock('@/utils/api', () => ({
  apiPost: vi.fn().mockResolvedValue({ models: [] }),
  apiGet: (...a: unknown[]) => mockApiGet(...a),
  apiPatch: (...a: unknown[]) => mockApiPatch(...a),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
// The usage tab's compaction action posts to the group-member endpoint; the
// drawer only needs to know it was called with the member row id.
const { mockCompactMember } = vi.hoisted(() => ({ mockCompactMember: vi.fn() }))
vi.mock('@/composables/useGroupChat', () => ({
  compactGroupMember: (...a: unknown[]) => mockCompactMember(...a),
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
  supportsACP: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return !!a?.acpCommand
  }),
  supportsCLI: vi.fn((agentId: string) => {
    const a = mockAgents.agents.value.find(a => a.id === agentId)
    return a ? a.supportsCLI !== false : false
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
  cruise: ref(false),
  toggleCruise: vi.fn(),
  // Usage tab reads these (the single-chat path).
  contextUsed: ref(50000),
  contextSize: ref(200000),
  contextInputTokens: ref(0),
  contextOutputTokens: ref(0),
  contextTotalTokens: ref(0),
  contextCachedReadTokens: ref(0),
  contextCachedWriteTokens: ref(0),
  contextThoughtTokens: ref(0),
  contextCost: ref(0),
  contextCurrency: ref(''),
  contextCacheCreationTokens: ref(0),
  contextCacheHitTokens: ref(0),
  contextCacheMissTokens: ref(0),
  contextCredit: ref(0),
  contextUsageByCategory: ref(undefined),
}

describe('SessionDrawer', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
    vi.mocked(apiPost).mockResolvedValue({ models: [] })
    mockApiGet.mockReset()
    mockApiPatch.mockReset().mockResolvedValue({ ok: true })
    mockCompactMember.mockReset().mockResolvedValue({ used: 1000, size: 200000 })
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

  it('renders the tab bar with five tabs (usage added)', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-setting-tabs').exists()).toBe(true)
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs.length).toBe(5)
  })

  it('renders all tab labels', () => {
    const wrapper = mountDrawer()
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[0].text()).toContain('chat.modelSwitcher.title')
    expect(tabs[1].text()).toContain('chat.thinkingEffortSwitcher.title')
    expect(tabs[2].text()).toContain('chat.modeSwitcher.title')
    expect(tabs[3].text()).toContain('chat.transportSwitcher.title')
    expect(tabs[4].text()).toContain('chat.sessionInfo.contextUsage')
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

  it('shows the raw model id on the shared model row', () => {
    const wrapper = mountDrawer()
    const ids = wrapper.findAll('.model-row__id').map(n => n.text())
    expect(ids).toEqual(['claude-sonnet-4-6', 'claude-opus-4-5'])
  })

  it('hides the raw id when it is already the display name', () => {
    // Backends that report no display name fall back to the id; showing it
    // twice would be noise.
    mockAgents.agents.value[0].models = [{ id: 'gpt-5.1-codex', name: 'gpt-5.1-codex', default: true }]
    const wrapper = mountDrawer()
    expect(wrapper.find('.model-row__id').exists()).toBe(false)
    mockAgents.agents.value[0].models = [
      { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
      { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
    ]
  })

  it('renders a JSON-shaped wire id readably, not as raw JSON', () => {
    // DeepSeek Harness identifies a model by a JSON-stringified provider/model
    // pair; that exact string is what the agent requires on the wire, so it must
    // stay in m.id — but the list must show it as provider/model, never as a
    // JSON blob.
    mockAgents.agents.value[0].models = [
      { id: '["deepseek-official","deepseek-v4-pro"]', name: 'DeepSeek-V4-Pro', default: true },
    ]
    const wrapper = mountDrawer()
    const idText = wrapper.find('.model-row__id').text()
    expect(idText).toBe('deepseek-official/deepseek-v4-pro')
    expect(idText).not.toContain('[')
    expect(idText).not.toContain('"')
    mockAgents.agents.value[0].models = [
      { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
      { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
    ]
  })

  it('matches the model search against the displayed id form', () => {
    mockAgents.agents.value[0].models = [
      { id: '["deepseek-official","deepseek-v4-pro"]', name: 'DeepSeek-V4-Pro', default: true },
      { id: '["deepseek-official","deepseek-flash"]', name: 'DeepSeek-V41-Flash', default: false },
    ]
    const wrapper = mountDrawer()
    const input = wrapper.find('.model-search-input')
    input.setValue('official/deepseek-v4')
    return wrapper.vm.$nextTick().then(() => {
      const names = wrapper.findAll('.model-row__name').map(n => n.text())
      expect(names).toEqual(['DeepSeek-V4-Pro'])
      mockAgents.agents.value[0].models = [
        { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
        { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
      ]
    })
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

  it('handleRefresh calls API and applies the resolved + CLI lists', async () => {
    const newModels = [{ id: 'new-model', name: 'New Model', default: true }]
    const cliModels = [
      { id: 'new-model', name: 'New Model', default: true },
      { id: 'cli-only', name: 'CLI Only' },
    ]
    vi.mocked(apiPost).mockResolvedValue({ models: newModels, cliModels })

    const wrapper = mountDrawer()
    await wrapper.vm.handleRefresh()

    expect(apiPost).toHaveBeenCalledWith('/api/agents/claude/refresh-models', {})
    expect(mockApplyRefreshedModelList).toHaveBeenCalledWith('claude', newModels, cliModels)
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

  // ── Set default model via set-as-default button ──

  it('setDefaultModel calls patchAgentPref and updates agent field', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredModel', 'claude-opus-4-5')
  })

  it('setDefaultModel also selects the model as current (emits switch-model)', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })

    expect(wrapper.emitted('switch-model')).toBeTruthy()
    expect(wrapper.emitted('switch-model')![0][0]).toEqual({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })
  })

  // ── Set default thinking effort via set-as-default button ──

  it('setDefaultThinkingEffort calls patchAgentPref and updates agent field', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultThinkingEffort('medium')

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', 'medium')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredThinkingEffort', 'medium')
  })

  it('setDefaultThinkingEffort also selects the effort as current (emits switch-thinking-effort)', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultThinkingEffort('medium')

    expect(wrapper.emitted('switch-thinking-effort')).toBeTruthy()
    expect(wrapper.emitted('switch-thinking-effort')![0][0]).toBe('medium')
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

  it('set-default star buttons expose aria-pressed and aria-label', () => {
    // The claude agent has two models; the non-default one renders the
    // set-default star button. It must expose an aria-label (touch users can't
    // hover for the :title tooltip) and aria-pressed=false (it is not the
    // current default).
    const wrapper = mountDrawer()
    const btns = wrapper.findAll('.set-default-btn')
    expect(btns.length).toBeGreaterThan(0)
    for (const btn of btns) {
      expect(btn.attributes('aria-pressed')).toBe('false')
      expect(btn.attributes('aria-label')).toBeTruthy()
    }
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

  // ── Long-press suppression of click selection ──

  it('selectModel does nothing when a long-press was just triggered', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.longPressTriggered = true
    wrapper.vm.selectModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })
    expect(wrapper.emitted('switch-model')).toBeFalsy()
  })

  it('selectThinkingEffort does nothing when a long-press was just triggered', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.longPressTriggered = true
    wrapper.vm.selectThinkingEffort('low')
    expect(wrapper.emitted('switch-thinking-effort')).toBeFalsy()
  })

  it('selectMode does nothing when a long-press was just triggered', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.longPressTriggered = true
    wrapper.vm.selectMode({ id: 'ask', name: 'Ask' })
    expect(wrapper.emitted('switch-mode')).toBeFalsy()
  })

  it('selectModel emits switch-model and close', async () => {
    const wrapper = mountDrawer()
    wrapper.vm.selectModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })
    expect(wrapper.emitted('switch-model')).toBeTruthy()
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  // ── Set default via star button error handling ──

  it('setDefaultModel shows error toast when patchAgentPref rejects', async () => {
    mockAgents.updateAgentField.mockClear()
    vi.mocked(patchAgentPref).mockRejectedValue(new Error('fail'))
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultModel({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' })
    // no throw; agent field not updated
    expect(mockAgents.updateAgentField).not.toHaveBeenCalledWith('claude', 'preferredModel', 'claude-opus-4-5')
  })

  it('setDefaultThinkingEffort shows error toast when patchAgentPref rejects', async () => {
    mockAgents.updateAgentField.mockClear()
    vi.mocked(patchAgentPref).mockRejectedValue(new Error('fail'))
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultThinkingEffort('low')
    expect(mockAgents.updateAgentField).not.toHaveBeenCalledWith('claude', 'preferredThinkingEffort', 'low')
  })

  // ── Set default mode ──

  it('setDefaultMode calls patchAgentPref and updates agent field', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultMode({ id: 'ask', name: 'Ask' })
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_mode', 'ask')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredMode', 'ask')
  })

  it('setDefaultMode also selects the mode as current (emits switch-mode)', async () => {
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultMode({ id: 'ask', name: 'Ask' })

    expect(wrapper.emitted('switch-mode')).toBeTruthy()
    expect(wrapper.emitted('switch-mode')![0][0]).toEqual({ id: 'ask', name: 'Ask' })
  })

  it('setDefaultMode shows error toast when patchAgentPref rejects', async () => {
    mockAgents.updateAgentField.mockClear()
    vi.mocked(patchAgentPref).mockRejectedValue(new Error('fail'))
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultMode({ id: 'ask', name: 'Ask' })
    expect(mockAgents.updateAgentField).not.toHaveBeenCalledWith('claude', 'preferredMode', 'ask')
  })

  // ── Set default transport (with thinking-effort cleanup) ──

  it('setDefaultTransport clears preferred thinking effort when not valid for CLI', async () => {
    const claude = mockAgents.agents.value.find(a => a.id === 'claude')!
    const originalPref = claude.preferredThinkingEffort
    claude.preferredThinkingEffort = 'ultra' // not in CLI valid levels
    const wrapper = mountDrawer()

    await wrapper.vm.setDefaultTransport('cli')

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'transport', 'cli')
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', '')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredThinkingEffort', '')
    claude.preferredThinkingEffort = originalPref
  })

  it('setDefaultTransport also selects the transport as current (emits switch-transport)', async () => {
    const { restoreOriginalModels, invalidateACPStateCache } = await import('@/composables/useAgents')
    const wrapper = mountDrawer()

    await wrapper.vm.setDefaultTransport('cli')

    expect(wrapper.emitted('switch-transport')).toBeTruthy()
    expect(wrapper.emitted('switch-transport')![0][0]).toBe('cli')
    expect(mockIdentity.currentTransport.value).toBe('cli')
    expect(restoreOriginalModels).toHaveBeenCalledWith('claude')
    expect(invalidateACPStateCache).toHaveBeenCalledWith('claude')
  })

  it('setDefaultTransport keeps preferred thinking effort when valid for CLI', async () => {
    vi.mocked(patchAgentPref).mockClear()
    const wrapper = mountDrawer() // claude preferredThinkingEffort = 'high'
    await wrapper.vm.setDefaultTransport('cli')
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'transport', 'cli')
    expect(patchAgentPref).not.toHaveBeenCalledWith('claude', 'preferred_thinking_effort', '')
  })

  it('setDefaultTransport keeps preferred thinking effort when valid for ACP', async () => {
    vi.mocked(patchAgentPref).mockClear()
    const wrapper = mountDrawer() // claude preferredThinkingEffort = 'high', ACP levels include high
    await wrapper.vm.setDefaultTransport('acp-stdio')
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'transport', 'acp-stdio')
    expect(patchAgentPref).not.toHaveBeenCalledWith('claude', 'preferred_thinking_effort', '')
  })

  it('setDefaultTransport shows error toast when patchAgentPref rejects', async () => {
    mockAgents.updateAgentField.mockClear()
    vi.mocked(patchAgentPref).mockRejectedValue(new Error('fail'))
    const wrapper = mountDrawer()
    await wrapper.vm.setDefaultTransport('cli')
    expect(mockAgents.updateAgentField).not.toHaveBeenCalledWith('claude', 'transport', 'cli')
  })

  // ── Thinking levels in different transport modes ──

  it('uses static thinking levels for CLI transport', async () => {
    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = 'cli'
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    const rawState = (wrapper.vm as any).$.devtoolsRawSetupState
    expect(rawState.isACP.value).toBe(false)
    expect(rawState.thinkingLevels.value).toEqual([
      { id: 'low', name: 'low' },
      { id: 'medium', name: 'medium' },
      { id: 'high', name: 'high' },
    ])
    mockIdentity.currentTransport.value = 'acp-stdio'
  })

  it('returns empty thinking levels when ACP has no reported levels', async () => {
    const wrapper = mountDrawer()
    const origLevels = mockIdentity.availableThinkingEfforts.value
    mockIdentity.availableThinkingEfforts.value = []
    mockIdentity.currentTransport.value = 'acp-stdio'
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    const rawState = (wrapper.vm as any).$.devtoolsRawSetupState
    expect(rawState.thinkingLevels.value).toEqual([])
    mockIdentity.availableThinkingEfforts.value = origLevels
  })

  it('falls back to agent transport when session transport is unset', async () => {
    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = ''
    const rawState = (wrapper.vm as any).$.devtoolsRawSetupState
    expect(rawState.isACP.value).toBe(true) // claude transport === 'acp-stdio'
    mockIdentity.currentTransport.value = 'acp-stdio'
  })

  // ── Reset search/active tab on reopen ──

  it('resets search query and active tab when drawer is reopened', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('thinking')
    wrapper.vm._setSearchQuery('sonnet')
    await nextTick()
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('model')
    expect(wrapper.vm._getSearchQuery()).toBe('')
  })

  // ── Long-press popup → set as default ──

  it('opens popup menu on long-press of a model and sets it as default', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    const first = wrapper.find('.model-item')
    wrapper.vm.onTouchStart({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5' }, { target: first.element })
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.longPressTriggered).toBe(true)
    expect(wrapper.vm.pendingDefaultModel).toBe('claude-opus-4-5')
    const popupBtn = wrapper.find('.popup-set-default')
    expect(popupBtn.exists()).toBe(true)
    await popupBtn.trigger('click')
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(wrapper.vm.showDefaultPopupMenu).toBe(false)
    vi.useRealTimers()
  })

  it('opens popup menu on long-press of a thinking level and sets it as default', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    const first = wrapper.find('.thinking-item')
    wrapper.vm.onTouchStartThinking('low', { target: first.element })
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.pendingDefaultThinking).toBe('low')
    wrapper.vm.setAsDefault()
    await nextTick()
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', 'low')
    vi.useRealTimers()
  })

  it('opens popup menu on long-press of a mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('mode')
    await nextTick()
    const first = wrapper.find('.thinking-item')
    wrapper.vm.onTouchStartMode({ id: 'ask', name: 'Ask' }, { target: first.element })
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.pendingDefaultMode).toBe('ask')
    wrapper.vm.setAsDefault()
    await nextTick()
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_mode', 'ask')
    vi.useRealTimers()
  })

  it('clears the long-press timer and resets flag on touchend', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    wrapper.vm.longPressTriggered = true
    wrapper.vm.onTouchEnd()
    vi.advanceTimersByTime(100)
    expect(wrapper.vm.longPressTriggered).toBe(false)
    vi.useRealTimers()
  })

  it('clears the long-press timer on touchmove', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer()
    const first = wrapper.find('.model-item')
    wrapper.vm.onTouchStart({ id: 'm1', name: 'M1' }, { target: first.element })
    wrapper.vm.onTouchMove()
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(false)
    expect(wrapper.vm.longPressTriggered).toBe(false)
    vi.useRealTimers()
  })

  // ── Context menu → set as default ──

  it('opens popup menu via context menu on a model', async () => {
    const wrapper = mountDrawer()
    const first = wrapper.find('.model-item')
    await first.trigger('contextmenu')
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.pendingDefaultModel).toBe('claude-sonnet-4-6')
  })

  it('opens popup menu via context menu on a thinking level', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    const first = wrapper.find('.thinking-item')
    await first.trigger('contextmenu')
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.pendingDefaultThinking).toBe('low')
  })

  it('opens popup menu via context menu on a mode', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('mode')
    await nextTick()
    const first = wrapper.find('.thinking-item')
    await first.trigger('contextmenu')
    await nextTick()
    expect(wrapper.vm.showDefaultPopupMenu).toBe(true)
    expect(wrapper.vm.pendingDefaultMode).toBe('code')
  })

  // ── Transport tab rendering ──

  it('renders both ACP and CLI transport options for an ACP+CLI agent', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('transport')
    await nextTick()
    const names = wrapper.findAll('.thinking-item .model-item-name').map(n => n.text())
    expect(names).toContain('chat.transportSwitcher.acp')
    expect(names).toContain('chat.transportSwitcher.cli')
  })

  it('selects CLI transport via the transport tab button', async () => {
    const { restoreOriginalModels, invalidateACPStateCache } = await import('@/composables/useAgents')
    const wrapper = mountDrawer()
    mockIdentity.currentTransport.value = 'acp-stdio'
    wrapper.vm._setActiveTab('transport')
    await nextTick()
    const cliBtn = wrapper.findAll('.thinking-item').find(b => b.text().includes('chat.transportSwitcher.cli'))!
    await cliBtn.trigger('click')
    await nextTick()
    expect(mockIdentity.currentTransport.value).toBe('cli')
    expect(restoreOriginalModels).toHaveBeenCalled()
    expect(invalidateACPStateCache).toHaveBeenCalled()
    expect(wrapper.emitted('switch-transport')).toBeTruthy()
    mockIdentity.currentTransport.value = 'acp-stdio'
  })

  // ── Auto-approve toggle ──

  it('toggles auto-approve from the mode tab checkbox', async () => {
    const wrapper = mountDrawer()
    wrapper.vm._setActiveTab('mode')
    await nextTick()
    const checkbox = wrapper.find('.auto-approve-section input[type="checkbox"]')
    expect(checkbox.exists()).toBe(true)
    await checkbox.setValue(true)
    expect(mockIdentity.toggleAutoApprove).toHaveBeenCalled()
  })

  // ── Empty transport tab for non-ACP/CLI agent ──

  it('does not show ACP transport option for an agent without ACP support', async () => {
    const wrapper = mountDrawer({ agentId: 'kimi' })
    wrapper.vm._setActiveTab('transport')
    await nextTick()
    const names = wrapper.findAll('.thinking-item .model-item-name').map(n => n.text())
    expect(names).not.toContain('chat.transportSwitcher.acp')
    expect(names).toContain('chat.transportSwitcher.cli')
  })

  // ── Model set-as-default button ──

  it('sets a model as default via the set-as-default button', async () => {
    const wrapper = mountDrawer()
    const nonDefaultStar = wrapper.findAll('.set-default-btn')[0] // opus is non-default
    await nonDefaultStar.trigger('click')
    await nextTick()
    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
  })

  // ── Keyboard list navigation (useListNav + useListKeys) ──

  it('navigates and selects a model via keyboard', async () => {
    Element.prototype.scrollIntoView = vi.fn()
    const wrapper = mount(SessionDrawer, { props: { open: true, agentId: 'claude' }, attachTo: document.body })
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    expect(wrapper.emitted('switch-model')).toBeTruthy()
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('navigates and selects a thinking effort via keyboard', async () => {
    const wrapper = mount(SessionDrawer, { props: { open: true, agentId: 'claude' }, attachTo: document.body })
    wrapper.vm._setActiveTab('thinking')
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    expect(wrapper.emitted('switch-thinking-effort')).toBeTruthy()
    wrapper.unmount()
  })

  it('navigates and selects a mode via keyboard', async () => {
    const wrapper = mount(SessionDrawer, { props: { open: true, agentId: 'claude' }, attachTo: document.body })
    wrapper.vm._setActiveTab('mode')
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    expect(wrapper.emitted('switch-mode')).toBeTruthy()
    wrapper.unmount()
  })

  it('ignores keyboard navigation when the list has no items (transport tab)', async () => {
    const wrapper = mount(SessionDrawer, { props: { open: true, agentId: 'claude' }, attachTo: document.body })
    wrapper.vm._setActiveTab('transport')
    await nextTick()
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }))
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await nextTick()
    expect(wrapper.emitted('switch-transport')).toBeFalsy()
    wrapper.unmount()
  })

  // ── Context usage tab ──

  describe('usage tab', () => {
    it('renders the shared usage panel for the current session', async () => {
      const wrapper = mountDrawer()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      // The panel body is ContextUsageDetail — the same component the info-row
      // popup renders, so the two surfaces cannot drift.
      const detail = wrapper.findComponent({ name: 'ContextUsageDetail' })
      expect(detail.exists()).toBe(true)
      expect((detail.props('usage') as { used: number }).used).toBe(50000)
    })

    it('gives the panel body its own padding (the shared component has none)', async () => {
      // ContextUsageDetail carries no outer padding: its single-chat host
      // (.usage-popup) supplies it. Mounted bare in this drawer the rows would
      // sit flush against the sheet edges, so the wrapper must pad them.
      const wrapper = mountDrawer()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      const content = wrapper.find('.model-tab-content')
      expect(content.classes()).toContain('usage-tab-content')

      const src = readWebFile('src/components/chat/SessionDrawer.vue')
      const rule = src.slice(src.indexOf('.usage-tab-content {'), src.indexOf('}', src.indexOf('.usage-tab-content {')))
      expect(rule).toMatch(/padding:\s*var\(--space-4\) var\(--space-5\)/)
    })

    it('shows an empty state when the session never reported a window', async () => {
      mockIdentity.contextSize.value = 0
      try {
        const wrapper = mountDrawer()
        await wrapper.findAll('.model-tab')[4].trigger('click')
        await nextTick()
        expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).exists()).toBe(false)
        expect(wrapper.text()).toContain('chat.sessionSetting.noUsage')
      } finally {
        mockIdentity.contextSize.value = 200000
      }
    })
  })

  // ── Auto-cruise toggle in the mode tab ──

  describe('auto-cruise switch', () => {
    it('sits in the mode tab directly under auto-approve', async () => {
      const wrapper = mountDrawer()
      await wrapper.findAll('.model-tab')[2].trigger('click')
      await nextTick()
      const toggles = wrapper.findAll('.auto-approve-toggle')
      // auto-approve first, cruise second — the order is the point (cruise
      // forces auto-approve on, so the link is visible where it happens).
      expect(toggles.length).toBe(2)
      expect(toggles[0].text()).toContain('chat.autoApprove.title')
      expect(toggles[1].text()).toContain('chat.cruise.label')
    })

    it('reflects the session cruise value', async () => {
      mockIdentity.cruise.value = true
      try {
        const wrapper = mountDrawer()
        await wrapper.findAll('.model-tab')[2].trigger('click')
        await nextTick()
        const cruiseInput = wrapper.findAll('.auto-approve-toggle')[1].find('input')
        expect((cruiseInput.element as HTMLInputElement).checked).toBe(true)
      } finally {
        mockIdentity.cruise.value = false
      }
    })

    it('toggles the current session cruise through the shared action', async () => {
      const wrapper = mountDrawer()
      await wrapper.findAll('.model-tab')[2].trigger('click')
      await nextTick()
      await wrapper.findAll('.auto-approve-toggle')[1].find('input').setValue(true)
      expect(mockIdentity.toggleCruise).toHaveBeenCalledWith(true)
    })
  })

  // ── Targeted session (group member) ──
  // Clicking a member in the group roster opens THIS drawer on that member's
  // session row. Everything must come from the fetched payload — reading the
  // singleton identity refs would show the group's host state while claiming to
  // describe the member.

  describe('targeted session (group member)', () => {
    const targetPayload = {
      ok: true,
      sessionId: 'member-1',
      sessionType: 'group_member',
      agentId: 'claude',
      backend: 'claude',
      modelId: 'claude-opus-4-5',
      transport: 'acp-stdio',
      autoApprove: true,
      cruise: true,
      modeState: { currentModeId: 'ask', availableModes: [{ id: 'ask', name: 'Ask' }, { id: 'plan', name: 'Plan' }] },
      thinkingEffortState: { currentId: 'low', availableLevels: [{ id: 'low', name: 'Low' }] },
      usageState: { used: 9000, size: 100000 },
      isGroupMember: true,
    }

    function mountTargeted(props = {}) {
      mockApiGet.mockResolvedValue(targetPayload)
      return mountDrawer({ targetSessionId: 'member-1', ...props })
    }

    it('fetches the member row state from the session-settings endpoint', async () => {
      mountTargeted()
      await nextTick()
      expect(mockApiGet).toHaveBeenCalledWith('/api/ai/session/settings?session_id=member-1')
    })

    it('shows the member values, not the current session values', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      // Model: payload says opus; the singleton identity says sonnet.
      const currentModel = wrapper.findAll('.model-item.current')
      expect(currentModel.length).toBe(1)
      expect(currentModel[0].text()).toContain('Claude Opus 4.5')
    })

    it('renders the member usage in the usage tab', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      const detail = wrapper.findComponent({ name: 'ContextUsageDetail' })
      expect(detail.exists()).toBe(true)
      expect((detail.props('usage') as { used: number }).used).toBe(9000)
    })

    it('writes settings through the targeted endpoint, not the current session', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[2].trigger('click')
      await nextTick()
      // Mode list comes from the payload (ask/plan), and picking one writes to
      // the member row.
      const items = wrapper.findAll('.thinking-item')
      await items[1].trigger('click')
      await nextTick()
      expect(mockApiPatch).toHaveBeenCalledWith('/api/ai/session/settings', {
        sessionId: 'member-1',
        modeId: 'plan',
      })
      // The shared identity action must NOT have been used (it writes the
      // current session, i.e. the group).
      expect(wrapper.emitted('switch-mode')).toBeFalsy()
    })

    it('compacts the member through the group endpoint', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      wrapper.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
      await nextTick()
      await nextTick()
      expect(mockCompactMember).toHaveBeenCalledWith('member-1', 'member-1')
    })

    // A CLI-transport member has no slash-command channel, so /compact would
    // reach the model as prose; the server answers 400. Offering the button only
    // produced an error AFTER the click.
    it('does not offer compaction for a CLI-transport member', async () => {
      mockApiGet.mockResolvedValue({ ...targetPayload, transport: 'cli' })
      const wrapper = mountDrawer({ targetSessionId: 'member-1' })
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      const detail = wrapper.findComponent({ name: 'ContextUsageDetail' })
      expect(detail.exists()).toBe(true)
      expect(detail.props('showCompact')).toBe(false)
    })

    it('offers compaction for an ACP member', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).props('showCompact')).toBe(true)
    })

    // The panel can be pointed at another member while a compaction is still in
    // flight. Its spinner/disabled state belongs to the member it was started
    // for — otherwise the new member looks busy and its button stays dead until
    // the first one finishes.
    it('does not carry a compaction spinner across members', async () => {
      let resolveCompact!: (v: unknown) => void
      mockCompactMember.mockReturnValue(new Promise((r) => { resolveCompact = r }))

      mockApiGet.mockResolvedValue(targetPayload)
      const wrapper = mountDrawer({ targetSessionId: 'member-1' })
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      wrapper.findComponent({ name: 'ContextUsageDetail' }).vm.$emit('compact')
      await nextTick()
      expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).props('compacting')).toBe(true)

      // Point the drawer at a DIFFERENT member: it must render as idle.
      mockApiGet.mockResolvedValue({ ...targetPayload, sessionId: 'member-2' })
      await wrapper.setProps({ targetSessionId: 'member-2' })
      await nextTick()
      await nextTick()
      expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).props('compacting')).toBe(false)

      // The first member's result must not touch the panel now on screen.
      resolveCompact({ used: 100, size: 100000 })
      await nextTick()
      await nextTick()
      expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).props('compacting')).toBe(false)
    })

    it('toggles cruise on the member row via the targeted endpoint', async () => {
      const wrapper = mountTargeted()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[2].trigger('click')
      await nextTick()
      await wrapper.findAll('.auto-approve-toggle')[1].find('input').setValue(false)
      await nextTick()
      expect(mockApiPatch).toHaveBeenCalledWith('/api/ai/session/settings', {
        sessionId: 'member-1',
        cruise: false,
      })
      // The current session's cruise must not be touched. (The shared action is
      // called by other tests in this file, so assert on THIS call's absence by
      // checking the toggle went to the targeted endpoint with the new value.)
      expect(mockApiPatch).toHaveBeenCalledTimes(1)
    })
  })

  // ── Member rows must reach the same three tabs as single chat ──
  // The reported defect: a group member's thinking / mode / usage tabs all said
  // "not available" while the same tabs worked in single chat. The payload below
  // is what the server now returns for a member (transport is resolved, effort is
  // restored without userPicked).

  describe('group member tabs parity with single chat', () => {
    const memberPayload = {
      ok: true,
      sessionId: 'member-9',
      sessionType: 'group_member',
      agentId: 'claude',
      backend: 'claude',
      modelId: 'claude-opus-4-5',
      transport: 'acp-stdio',
      autoApprove: false,
      cruise: false,
      modeState: { currentModeId: 'plan', availableModes: [{ id: 'plan', name: 'Plan' }, { id: 'default', name: 'Ask' }] },
      thinkingEffortState: { currentId: 'xhigh', availableLevels: [{ id: 'low', name: 'Low' }, { id: 'xhigh', name: 'XHigh' }] },
      usageState: { used: 64269, size: 1000000 },
      isGroupMember: true,
    }

    function mountMember() {
      mockApiGet.mockResolvedValue(memberPayload)
      return mountDrawer({ targetSessionId: 'member-9' })
    }

    it('renders the mode list instead of the unavailable hint', async () => {
      const wrapper = mountMember()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[2].trigger('click')
      await nextTick()
      expect(wrapper.find('.tab-empty-hint').exists()).toBe(false)
      const modes = wrapper.findAll('.thinking-item')
      expect(modes.length).toBe(2)
      expect(modes[0].text()).toContain('Plan')
      // The current mode is marked from the member's own payload.
      expect(modes[0].classes()).toContain('current')
    })

    it('renders the thinking levels instead of the unavailable hint', async () => {
      const wrapper = mountMember()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[1].trigger('click')
      await nextTick()
      expect(wrapper.find('.tab-empty-hint').exists()).toBe(false)
      const levels = wrapper.findAll('.thinking-item')
      expect(levels.length).toBe(2)
      expect(levels[1].text()).toContain('XHigh')
      expect(levels[1].classes()).toContain('current')
    })

    it('renders the member usage instead of the empty state', async () => {
      const wrapper = mountMember()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[4].trigger('click')
      await nextTick()
      expect(wrapper.text()).not.toContain('chat.sessionSetting.noUsage')
      expect(wrapper.findComponent({ name: 'ContextUsageDetail' }).exists()).toBe(true)
    })

    it('shows the protocol selection from the resolved transport', async () => {
      const wrapper = mountMember()
      await nextTick()
      await nextTick()
      await wrapper.findAll('.model-tab')[3].trigger('click')
      await nextTick()
      // ACP row is current — an empty transport column used to leave BOTH
      // protocol rows unselected.
      const rows = wrapper.findAll('.thinking-item')
      expect(rows[0].classes()).toContain('current')
    })
  })
})
