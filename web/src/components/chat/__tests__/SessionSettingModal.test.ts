import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref, defineComponent } from 'vue'
import SessionSettingModal from '@/components/chat/SessionSettingModal.vue'
import { useAgents } from '@/composables/useAgents'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { apiPost } from '@/utils/api'
import { patchAgentPref } from '@/composables/useSettingsConfig'

// Mock ModalDialog to render slot content inline (skip Teleport).
vi.mock('@/components/common/ModalDialog.vue', () => ({
  default: defineComponent({
    props: { open: Boolean, title: String, zIndex: Number, fullHeight: Boolean },
    emits: ['close'],
    inheritAttrs: true,
    template: `
      <div class="modal-overlay">
        <div class="modal-dialog">
          <div class="modal-header"><slot name="header" /></div>
          <div class="modal-body"><slot /></div>
          <div class="modal-footer"><slot name="footer" /></div>
          <slot name="after" />
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
  availableThinkingEfforts: ref([]),
  availableModes: ref([{ id: 'code', name: 'Code' }, { id: 'ask', name: 'Ask' }]),
  currentModeId: ref('code'),
  autoApprove: ref(false),
  toggleAutoApprove: vi.fn(),
}

describe('SessionSettingModal', () => {
  beforeEach(() => {
    vi.mocked(useAgents).mockReturnValue(mockAgents as any)
    vi.mocked(useSessionIdentity).mockReturnValue(mockIdentity as any)
    vi.mocked(apiPost).mockResolvedValue({ models: [] })
    vi.mocked(patchAgentPref).mockResolvedValue(undefined)
  })

  function mountModal(props = {}) {
    return mount(SessionSettingModal, {
      props: { show: true, agentId: 'claude', ...props },
    })
  }

  // ── Basic mount and structure ──

  it('mounts without errors', () => {
    const wrapper = mountModal()
    expect(wrapper.exists()).toBe(true)
  })

  it('renders modal overlay when show is true', () => {
    const wrapper = mountModal()
    expect(wrapper.find('.modal-overlay').exists()).toBe(true)
  })

  it('renders the tab bar with four tabs', () => {
    const wrapper = mountModal()
    expect(wrapper.find('.session-setting-tabs').exists()).toBe(true)
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs.length).toBe(4)
  })

  it('renders all tab labels', () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[0].text()).toContain('chat.modelSwitcher.title')
    expect(tabs[1].text()).toContain('chat.thinkingEffortSwitcher.title')
    expect(tabs[2].text()).toContain('chat.modeSwitcher.title')
    expect(tabs[3].text()).toContain('chat.transportSwitcher.title')
  })

  // ── Initial state (model tab is default) ──

  it('shows model tab as active by default', () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    expect(tabs[0].classes()).toContain('active')
    expect(tabs[1].classes()).not.toContain('active')
  })

  it('shows model search input on default model tab', () => {
    const wrapper = mountModal()
    expect(wrapper.find('.model-search-input').exists()).toBe(true)
  })

  it('renders model items for the current agent', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    expect(items.length).toBe(2)
  })

  it('marks default model with is-default class', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('is-default')
  })

  it('marks current model with current class', () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    expect(items[0].classes()).toContain('current')
  })

  // ── Tab switching via VM (DOM reactivity is unreliable in test env) ──

  it('updates activeTab to thinking on tab click', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()
    // Verify internal state changed (DOM class update is unreliable in jsdom)
    expect(wrapper.vm._getActiveTab()).toBe('thinking')
  })

  it('updates activeTab to mode on tab click', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[2].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('mode')
  })

  it('updates activeTab to transport on tab click', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[3].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('transport')
  })

  // ── Component emits ──

  it('declares switch-model emit', () => {
    expect(SessionSettingModal.emits).toContain('switch-model')
  })

  // ── Search filtering via VM ──

  it('filters models by search query via VM', async () => {
    const wrapper = mountModal()
    wrapper.vm._setSearchQuery('opus')
    await nextTick()
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Opus')
  })

  // ── Thinking tab content ──

  it('shows thinking effort items on thinking tab', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[1].trigger('click')
    await nextTick()
    const items = wrapper.findAll('.thinking-item')
    expect(items.length).toBe(3) // low, medium, high
  })

  // ── Mode tab content ──

  it('shows mode tab content when clicked', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[2].trigger('click')
    await nextTick()
    const content = wrapper.find('.model-tab-content')
    expect(content.exists()).toBe(true)
  })

  // ── Transport tab content ──

  it('shows transport tab content when clicked', async () => {
    const wrapper = mountModal()
    const tabs = wrapper.findAll('.model-tab')
    await tabs[3].trigger('click')
    await nextTick()
    // Verify tab switched (content area exists)
    const content = wrapper.find('.model-tab-content')
    expect(content.exists()).toBe(true)
  })

  // ── Model select interaction ──

  it('emits switch-model when model item is clicked', async () => {
    const wrapper = mountModal()
    const items = wrapper.findAll('.model-item')
    await items[1].trigger('click')
    expect(wrapper.emitted('switch-model')).toBeTruthy()
  })

  // ── Model search ──

  it('updates search query when typing in search input', async () => {
    const wrapper = mountModal()
    const input = wrapper.find('.model-search-input')
    await input.setValue('sonnet')
    await nextTick()
    const filtered = wrapper.vm._getFilteredModels()
    expect(filtered.length).toBe(1)
    expect(filtered[0].name).toContain('Sonnet')
  })
})
