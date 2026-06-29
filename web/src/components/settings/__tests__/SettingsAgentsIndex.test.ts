import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref } from 'vue'
import SettingsAgentsIndex from '@/components/settings/SettingsAgentsIndex.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      settings: {
        items: {
          defaultAgent: 'Default Agent',
          defaultAgentDesc: 'Default agent description',
          agentNoAgents: 'No agents available',
          agentCopy: 'Copy',
          agentCopyTitle: 'Duplicate Agent',
          agentCopyPlaceholder: 'Enter new agent name',
          agentCopyConfirm: 'Duplicate',
          agentCopied: 'Agent duplicated',
          agentCopyFailed: 'Duplicate failed',
          agentCopyEmptyName: 'Name cannot be empty',
          agentName: 'Name',
          agentRescan: 'Rescan',
          agentRescanning: 'Scanning...',
          agentRescanSuccess: 'Rescan complete',
          agentRescanFailed: 'Rescan failed',
        },
        saveFailed: 'Save failed',
      },
      common: {
        ok: 'OK',
        cancel: 'Cancel',
      },
    },
  },
})

// Mock useAgents
const mockAgents = ref<any[]>([])
const mockDefaultAgentId = ref('agent-1')
const mockLoadAgents = vi.fn()
const mockDuplicateAgent = vi.fn()
const mockRescanAgents = vi.fn()
const mockSetDefaultAgent = vi.fn()
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: mockAgents,
    defaultAgentId: mockDefaultAgentId,
    loadAgents: (...args: unknown[]) => mockLoadAgents(...args),
    duplicateAgent: (...args: unknown[]) => mockDuplicateAgent(...args),
    rescanAgents: (...args: unknown[]) => mockRescanAgents(...args),
    setDefaultAgent: (...args: unknown[]) => mockSetDefaultAgent(...args),
  }),
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: (...args: any[]) => mockToastShow(...args) }),
}))

// Mock lucide-vue-next
vi.mock('lucide-vue-next', () => ({
  ChevronRight: { name: 'ChevronRight', template: '<span class="icon-chevron" />' },
  Copy: { name: 'Copy', template: '<span class="icon-copy" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
}))

// Mock CopyAgentDialog
vi.mock('@/components/settings/CopyAgentDialog.vue', () => ({
  default: { name: 'CopyAgentDialog', template: '<div class="mock-copy-agent-dialog" />' },
}))

// Mock SettingsItem
vi.mock('@/components/settings/SettingsItem.vue', () => ({
  default: { name: 'SettingsItem', template: '<div class="mock-settings-item" />' },
}))

function mountIndex() {
  return mount(SettingsAgentsIndex, {
    global: { plugins: [i18n] },
  })
}

describe('SettingsAgentsIndex', () => {
  beforeEach(() => {
    mockLoadAgents.mockReset()
    mockDuplicateAgent.mockReset()
    mockRescanAgents.mockReset()
    mockSetDefaultAgent.mockReset()
    mockToastShow.mockReset()
    mockAgents.value = []
    mockDefaultAgentId.value = 'agent-1'
  })

  it('calls loadAgents on mount', () => {
    mountIndex()
    expect(mockLoadAgents).toHaveBeenCalledWith(true)
  })

  it('renders empty message when no agents', () => {
    mockAgents.value = []
    const wrapper = mountIndex()
    expect(wrapper.text()).toContain('No agents available')
  })

  it('renders agent rows', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: 'coding', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: 'analysis', sortOrder: 1 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.text()).toContain('CodeBuddy')
    expect(wrapper.text()).toContain('Claude')
  })

  it('emits navigate with agent ID on row click', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const row = wrapper.find('.settings-agents-index__row')
    await row.trigger('click')
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['agents:agent-1'])
  })

  it('renders copy button for each agent', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__icon-btn').exists()).toBe(true)
  })

  it('renders rescan row', () => {
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__rescan-row').exists()).toBe(true)
  })

  it('clicking rescan row calls rescanAgents', async () => {
    mockRescanAgents.mockResolvedValue(undefined)
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('.settings-agents-index__rescan-row')
    await rescanRow.trigger('click')
    expect(mockRescanAgents).toHaveBeenCalled()
  })

  it('clicking copy button does not emit navigate', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const copyBtn = wrapper.find('.settings-agents-index__icon-btn')
    await copyBtn.trigger('click')
    expect(wrapper.emitted('navigate')).toBeFalsy()
  })

  it('clicking copy button opens CopyAgentDialog', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const copyBtn = wrapper.find('.settings-agents-index__icon-btn')
    await copyBtn.trigger('click')
    await wrapper.vm.$nextTick()

    const vm = wrapper.vm as any
    expect(vm.$.setupState.copyingAgent).toEqual({ id: 'agent-1', name: 'CodeBuddy' })
  })

  it('rescan failure shows error toast', async () => {
    mockRescanAgents.mockRejectedValueOnce(new Error('fail'))
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('.settings-agents-index__rescan-row')
    await rescanRow.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Rescan failed', expect.any(Object))
  })

  it('rescan success shows success toast', async () => {
    mockRescanAgents.mockResolvedValueOnce(undefined)
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('.settings-agents-index__rescan-row')
    await rescanRow.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Rescan complete', expect.any(Object))
  })

  it('rescan sets rescanning state while scanning', async () => {
    const wrapper = mountIndex()
    const vm = wrapper.vm as any

    expect(vm.$.setupState.rescanning).toBe(false)

    mockRescanAgents.mockResolvedValueOnce(undefined)
    await vm.$.setupState.handleRescan()
    await wrapper.vm.$nextTick()

    expect(vm.$.setupState.rescanning).toBe(false)
  })

  it('agents are sorted by sortOrder', () => {
    mockAgents.value = [
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: '', sortOrder: 1 },
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const names = wrapper.findAll('.settings-agents-index__name')
    expect(names[0].text()).toBe('CodeBuddy')
    expect(names[1].text()).toBe('Claude')
  })

  it('copy confirmed calls duplicateAgent and shows success toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    mockDuplicateAgent.mockResolvedValueOnce(undefined)

    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    vm.$.setupState.copyingAgent = { id: 'agent-1', name: 'CodeBuddy' }
    await vm.$.setupState.handleCopyConfirmed('CodeBuddy Copy')
    await wrapper.vm.$nextTick()

    expect(mockDuplicateAgent).toHaveBeenCalledWith('agent-1', 'CodeBuddy Copy')
    expect(mockToastShow).toHaveBeenCalledWith('Agent duplicated', expect.any(Object))
  })

  it('copy failed shows error toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    mockDuplicateAgent.mockRejectedValueOnce(new Error('fail'))

    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    vm.$.setupState.copyingAgent = { id: 'agent-1', name: 'CodeBuddy' }
    await vm.$.setupState.handleCopyConfirmed('CodeBuddy Copy')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Duplicate failed', expect.any(Object))
  })

  it('handleCopyConfirmed does nothing when copyingAgent is null', async () => {
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    vm.$.setupState.copyingAgent = null
    await vm.$.setupState.handleCopyConfirmed('Test')
    await wrapper.vm.$nextTick()

    expect(mockDuplicateAgent).not.toHaveBeenCalled()
  })

  it('does not render delete button in agent rows', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__icon-btn--danger').exists()).toBe(false)
  })

  it('handleDefaultAgentChange calls setDefaultAgent', async () => {
    mockSetDefaultAgent.mockResolvedValueOnce(undefined)
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    await vm.$.setupState.handleDefaultAgentChange('agent-2')
    expect(mockSetDefaultAgent).toHaveBeenCalledWith('agent-2')
  })

  it('handleDefaultAgentChange shows error toast on failure', async () => {
    mockSetDefaultAgent.mockRejectedValueOnce(new Error('fail'))
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    await vm.$.setupState.handleDefaultAgentChange('agent-2')
    await wrapper.vm.$nextTick()
    expect(mockToastShow).toHaveBeenCalledWith('Save failed', expect.any(Object))
  })

  it('handleEditToggle sets activeKey on open', async () => {
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    vm.$.setupState.handleEditToggle('default_agent', true)
    expect(vm.$.setupState.activeKey).toBe('default_agent')
  })

  it('handleEditToggle clears activeKey on close when key matches', async () => {
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    vm.$.setupState.activeKey = 'default_agent'
    vm.$.setupState.handleEditToggle('default_agent', false)
    expect(vm.$.setupState.activeKey).toBeNull()
  })

  it('renders default agent select item', () => {
    const wrapper = mountIndex()
    const item = wrapper.findComponent({ name: 'SettingsItem' })
    expect(item.exists()).toBe(true)
  })

  it('renders specialty text when agent has specialty', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: 'coding', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__specialty').exists()).toBe(true)
    expect(wrapper.find('.settings-agents-index__specialty').text()).toBe('coding')
  })

  it('does not render specialty element when agent has no specialty', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    // v-if="agent.specialty" should not render the span
    expect(wrapper.find('.settings-agents-index__specialty').exists()).toBe(false)
  })

  it('disables rescan row while scanning', async () => {
    let resolveRescan: () => void
    mockRescanAgents.mockReturnValueOnce(new Promise<void>(r => { resolveRescan = r }))
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('.settings-agents-index__rescan-row')
    await rescanRow.trigger('click')

    const vm = wrapper.vm as any
    expect(vm.$.setupState.rescanning).toBe(true)
    expect(rescanRow.classes()).toContain('settings-agents-index__rescan-row--disabled')

    resolveRescan!()
    await wrapper.vm.$nextTick()
    expect(vm.$.setupState.rescanning).toBe(false)
  })
})
