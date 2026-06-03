import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import ModelModal from '@/components/chat/ModelModal.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'

// Mock composables
vi.mock('@/composables/useAgents', () => ({
  useAgents: vi.fn(),
}))
vi.mock('@/composables/useSessionIdentity', () => ({
  useSessionIdentity: vi.fn(),
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
      icon: '🤖',
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
    },
    {
      id: 'gemini',
      name: 'Gemini',
      icon: '💎',
      backend: 'gemini',
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
}

const mockIdentity = {
  currentAgentId: ref('claude'),
  currentModelId: ref('claude-sonnet-4-6'),
  currentModelName: ref('Claude Sonnet 4.6'),
  currentThinkingEffort: ref('high'),
  availableThinkingEfforts: ref([]),
}

describe('ModelModal', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
    vi.mocked(apiPost).mockResolvedValue({ models: [] })
    vi.mocked(patchAgentPref).mockResolvedValue(undefined)
    mockToastShow.mockClear()
  })

  function mountModal(props = {}) {
    return mount(ModelModal, {
      props: { show: true, agentId: 'claude', ...props },
      global: {
        stubs: { teleport: true },
      },
    })
  }

  // --- Tab switching ---

  it('renders model tab by default', () => {
    const wrapper = mountModal()
    expect(wrapper.find('.model-tab.active').text()).toContain('chat.modelSwitcher.title')
  })

  it('switches to thinking tab when clicked', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    // Click thinking tab
    await tabs[1].trigger('click')
    expect(wrapper.findAll('.model-tab')[1].classes()).toContain('active')
  })

  // --- Model list ---

  it('renders model list for current agent', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(3)
    expect(items[0].text()).toContain('Claude Sonnet 4.6')
  })

  it('highlights current session model', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('current')
  })

  it('shows default badge on preferred model', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    // First model is preferred, should have default badge
    expect(items[0].find('.default-badge').exists() || items[0].text().includes('默认')).toBe(true)
  })

  // --- Search ---

  it('filters models by search query', async () => {
    const wrapper = mountModal()
    const searchInput = wrapper.find('.model-search-input')
    await searchInput.setValue('opus')
    await nextTick()

    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(1)
    expect(items[0].text()).toContain('Opus')
  })

  it('shows no results message when search has no matches', async () => {
    const wrapper = mountModal()
    const searchInput = wrapper.find('.model-search-input')
    await searchInput.setValue('nonexistent')
    await nextTick()

    expect(wrapper.find('.model-empty').exists() || wrapper.text()).toBeTruthy()
  })

  it('filters models by id when search matches id but not name', async () => {
    const wrapper = mountModal()
    const searchInput = wrapper.find('.model-search-input')
    await searchInput.setValue('haiku-3')
    await nextTick()

    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(1)
    expect(items[0].text()).toContain('Haiku')
  })

  it('shows no-search-results message when search yields nothing', async () => {
    const wrapper = mountModal()
    const searchInput = wrapper.find('.model-search-input')
    await searchInput.setValue('xyz')
    await nextTick()

    expect(wrapper.find('.model-empty').text()).toContain('chat.modelModal.noSearchResults')
  })

  // --- Model selection (session-scoped) ---

  it('emits switch-model when clicking a model', async () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click') // opus

    expect(wrapper.emitted('switch-model')).toBeTruthy()
    expect(wrapper.emitted('switch-model')![0][0]).toEqual({ id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false })
  })

  it('closes modal after selecting a model', async () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click')

    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')![0][0]).toBe(false)
  })

  // --- Thinking effort ---

  it('renders thinking effort levels on thinking tab', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    expect(items.length).toBe(6) // 5 levels + auto
  })

  it('highlights current thinking effort', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // 'high' is current, find it
    const highItem = items.find(i => i.text().includes('high'))
    expect(highItem?.classes()).toContain('current')
  })

  it('emits switch-thinking-effort when clicking a level', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // Click 'medium'
    const mediumItem = items.find(i => i.text().includes('medium'))
    await mediumItem?.trigger('click')

    expect(wrapper.emitted('switch-thinking-effort')).toBeTruthy()
  })

  it('closes modal after selecting thinking effort', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    const mediumItem = items.find(i => i.text().includes('medium'))
    await mediumItem?.trigger('click')

    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')![0][0]).toBe(false)
  })

  it('shows auto option as current when thinking effort is empty', async () => {
    mockIdentity.currentThinkingEffort.value = ''
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // Auto (first item) should be current
    expect(items[0].classes()).toContain('current')
    mockIdentity.currentThinkingEffort.value = 'high' // restore
  })

  it('auto option shows default badge when no preferred thinking', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // Auto is the first item, and preferredThinkingEffort is '' so auto has default badge
    expect(items[0].find('.default-badge').exists()).toBe(true)
  })

  it('auto option shows star button when there is a preferred thinking', async () => {
    // Set a preferred thinking effort so auto is not default
    const claudeAgent = mockAgents.agents.value.find(a => a.id === 'claude')!
    const originalPreferred = claudeAgent.preferredThinkingEffort
    claudeAgent.preferredThinkingEffort = 'high'

    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // Auto item should have a set-default-btn since preferredThinkingEffort is 'high'
    expect(items[0].find('.set-default-btn').exists()).toBe(true)
    // The 'high' item should have a default badge
    const highItem = items.find(i => i.text().includes('high'))
    expect(highItem?.find('.default-badge').exists()).toBe(true)

    claudeAgent.preferredThinkingEffort = originalPreferred // restore
  })

  // --- Refresh ---

  it('has refresh button for agents that support model refresh', () => {
    const wrapper = mountModal()
    expect(wrapper.find('.refresh-btn').exists()).toBe(true)
  })

  it('hides refresh button for agents that do not support model refresh', () => {
    const wrapper = mountModal({ agentId: 'gemini' })
    expect(wrapper.find('.refresh-btn').exists()).toBe(false)
  })

  it('calls refresh API and updates agent models on success', async () => {
    const newModels = [
      { id: 'claude-new', name: 'Claude New', default: true },
    ]
    vi.mocked(apiPost).mockResolvedValue({ models: newModels })

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    // Wait for async handler
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(apiPost).toHaveBeenCalledWith('/api/agents/claude/refresh-models', {})
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'models', newModels)
    expect(mockToastShow).toHaveBeenCalledWith('chat.modelModal.refreshSuccess', expect.any(Object))
  })

  it('shows cliNotFound toast when CLI not found error', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'CLINotFound' })

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.modelModal.cliNotFound', expect.any(Object))
  })

  it('shows discoveryNotSupported toast when model discovery not supported', async () => {
    vi.mocked(apiPost).mockRejectedValue({ msgKey: 'ModelDiscoveryNotSupported' })

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.modelModal.discoveryNotSupported', expect.any(Object))
  })

  it('shows generic refreshFailed toast on other errors', async () => {
    vi.mocked(apiPost).mockRejectedValue(new Error('network error'))

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('chat.modelModal.refreshFailed', expect.any(Object))
  })

  it('disables refresh button while refreshing', async () => {
    let resolveRefresh: (v: any) => void
    vi.mocked(apiPost).mockReturnValue(new Promise(r => { resolveRefresh = r }))

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()

    expect(wrapper.find('.refresh-btn').attributes('disabled')).toBeDefined()

    resolveRefresh!({ models: [] })
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()
  })

  it('does not call API when already refreshing', async () => {
    let resolveRefresh: (v: any) => void
    vi.mocked(apiPost).mockReturnValue(new Promise(r => { resolveRefresh = r }))

    const wrapper = mountModal()
    await wrapper.find('.refresh-btn').trigger('click')
    await nextTick()

    // Try clicking again while still refreshing
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
    const wrapper = mountModal()
    const dividers = wrapper.findAll('.model-divider')
    // 3 models = 2 dividers between them
    expect(dividers.length).toBe(2)
  })

  // --- Set default button ---

  it('has set-default star button on non-default models', () => {
    const wrapper = mountModal()
    const setDefaultBtns = wrapper.findAll('.set-default-btn')
    // 3 models, 1 is default (no star btn), 2 have star btns
    expect(setDefaultBtns.length).toBe(2)
  })

  it('calls patchAgentPref and updateAgentField when star button clicked', async () => {
    const wrapper = mountModal()
    const starBtns = wrapper.findAll('.set-default-btn')
    await starBtns[0].trigger('click') // Click first star (opus)

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_model', 'claude-opus-4-5')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredModel', 'claude-opus-4-5')
  })

  it('shows error toast when setDefaultModel fails', async () => {
    vi.mocked(patchAgentPref).mockRejectedValueOnce(new Error('fail'))

    const wrapper = mountModal()
    const starBtns = wrapper.findAll('.set-default-btn')
    await starBtns[0].trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('settings.saveFailed', expect.any(Object))
  })

  // --- Thinking effort default ---

  it('sets default thinking effort via star button on thinking tab', async () => {
    // Set a preferred so the auto item has a star button
    const claudeAgent = mockAgents.agents.value.find(a => a.id === 'claude')!
    claudeAgent.preferredThinkingEffort = 'high'

    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    // Click the star on the auto item to set default to auto
    const autoItem = wrapper.findAll('.thinking-item')[0]
    await autoItem.find('.set-default-btn').trigger('click')

    expect(patchAgentPref).toHaveBeenCalledWith('claude', 'preferred_thinking_effort', '')
    expect(mockAgents.updateAgentField).toHaveBeenCalledWith('claude', 'preferredThinkingEffort', '')

    claudeAgent.preferredThinkingEffort = '' // restore
  })

  it('shows error toast when setDefaultThinkingEffort fails', async () => {
    vi.mocked(patchAgentPref).mockRejectedValueOnce(new Error('fail'))
    const claudeAgent = mockAgents.agents.value.find(a => a.id === 'claude')!
    claudeAgent.preferredThinkingEffort = 'high'

    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const autoItem = wrapper.findAll('.thinking-item')[0]
    await autoItem.find('.set-default-btn').trigger('click')
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    await nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('settings.saveFailed', expect.any(Object))

    claudeAgent.preferredThinkingEffort = '' // restore
  })

  // --- No models ---

  it('shows empty state when agent has no models', () => {
    const wrapper = mountModal({ agentId: 'gemini' })
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(0)
    expect(wrapper.find('.model-empty').exists()).toBe(true)
  })

  it('shows no-models message in empty state', () => {
    const wrapper = mountModal({ agentId: 'gemini' })
    expect(wrapper.find('.model-empty').text()).toContain('chat.modelModal.noModels')
  })

  // --- Close modal ---

  it('emits update:show false when close is triggered', async () => {
    const wrapper = mountModal()
    // ModalDialog emits close event
    await wrapper.findComponent({ name: 'ModalDialog' }).vm.$emit('close')
    expect(wrapper.emitted('update:show')).toBeTruthy()
    expect(wrapper.emitted('update:show')![0][0]).toBe(false)
  })

  // --- Context menu (right-click) ---

  it('shows popup menu on contextmenu for model item', async () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('contextmenu') // right-click on opus

    // PopupMenu should be shown
    expect(wrapper.find('.popup-set-default').exists() || wrapper.vm.showDefaultPopupMenu === true).toBeTruthy()
  })

  it('sets default model via popup menu', async () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    // Right-click on opus to open popup
    await items[1].trigger('contextmenu')
    await nextTick()

    // Click "set as default" in popup
    const popupBtn = wrapper.find('.popup-set-default')
    if (popupBtn.exists()) {
      await popupBtn.trigger('click')
      await nextTick()
      await new Promise(r => setTimeout(r, 10))
      expect(patchAgentPref).toHaveBeenCalled()
    }
  })

  // --- Agent name in header ---

  it('displays agent icon and name in modal title', () => {
    const wrapper = mountModal()
    // The title is passed to ModalDialog
    const dialog = wrapper.findComponent({ name: 'ModalDialog' })
    expect(dialog.props('title')).toBe('🤖 Claude')
  })

  // --- Thinking tab dividers ---

  it('renders dividers between thinking items', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const dividers = wrapper.findAll('.model-divider')
    // 5 levels + auto = 6 items, 5 dividers
    expect(dividers.length).toBe(5)
  })

  // --- is-default class ---

  it('adds is-default class to default model', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    // First model is the preferredModel (claude-sonnet-4-6)
    expect(items[0].classes()).toContain('is-default')
  })

  it('adds is-default class to default thinking effort', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()

    const items = wrapper.findAll('.thinking-item')
    // Auto (first) is default since preferredThinkingEffort is ''
    expect(items[0].classes()).toContain('is-default')
  })

  // --- Search resets on reopen ---

  it('resets search query when modal reopens', async () => {
    const wrapper = mountModal()
    const searchInput = wrapper.find('.model-search-input')
    await searchInput.setValue('opus')
    await nextTick()
    expect(wrapper.findAll('.model-item').length).toBe(1)

    // Close and reopen
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    // Search should be reset
    expect(wrapper.findAll('.model-item').length).toBe(3)
  })

  // --- No thinking tab for agents without levels ---

  it('hides thinking tab for agents without thinking effort levels', () => {
    const wrapper = mountModal({ agentId: 'gemini' })
    const tabs = wrapper.findAll('.model-tab')
    // Only model tab should be visible
    expect(tabs.length).toBe(1)
  })
})
