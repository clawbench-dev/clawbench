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
          agentDelete: 'Delete',
          agentDeleteConfirm: 'Delete agent "{name}"?',
          agentDeleteDefault: 'Cannot delete default agent',
          agentDeleted: 'Agent deleted',
          agentDeleteFailed: 'Delete failed',
        },
      },
      common: {
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
const mockDeleteAgent = vi.fn()
const mockRescanAgents = vi.fn()
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: mockAgents,
    defaultAgentId: mockDefaultAgentId,
    loadAgents: (...args: unknown[]) => mockLoadAgents(...args),
    duplicateAgent: (...args: unknown[]) => mockDuplicateAgent(...args),
    deleteAgent: (...args: unknown[]) => mockDeleteAgent(...args),
    rescanAgents: (...args: unknown[]) => mockRescanAgents(...args),
  }),
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: (...args: any[]) => mockToastShow(...args) }),
}))

// Mock useDialog
const mockConfirm = vi.fn().mockResolvedValue(false)
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: (...args: any[]) => mockConfirm(...args) }),
}))

// Mock lucide-vue-next
vi.mock('lucide-vue-next', () => ({
  ChevronRight: { name: 'ChevronRight', template: '<span class="icon-chevron" />' },
  Copy: { name: 'Copy', template: '<span class="icon-copy" />' },
  Trash2: { name: 'Trash2', template: '<span class="icon-trash" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
}))

// Mock CopyAgentDialog
vi.mock('@/components/settings/CopyAgentDialog.vue', () => ({
  default: { name: 'CopyAgentDialog', template: '<div class="mock-copy-agent-dialog" />' },
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
    mockDeleteAgent.mockReset()
    mockRescanAgents.mockReset()
    mockToastShow.mockReset()
    mockConfirm.mockReset().mockResolvedValue(false)
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

  it('renders delete button for each agent', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const deleteButtons = wrapper.findAll('.settings-agents-index__icon-btn--danger')
    expect(deleteButtons.length).toBe(1)
  })

  it('renders rescan button', () => {
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__rescan-btn').exists()).toBe(true)
  })

  it('clicking rescan button calls rescanAgents', async () => {
    mockRescanAgents.mockResolvedValue(undefined)
    const wrapper = mountIndex()
    const rescanBtn = wrapper.find('.settings-agents-index__rescan-btn')
    await rescanBtn.trigger('click')
    expect(mockRescanAgents).toHaveBeenCalled()
  })

  it('clicking copy button does not emit navigate', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    // First icon-btn is copy
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

    // CopyAgentDialog should be shown (copyingAgent is set)
    const vm = wrapper.vm as any
    expect(vm.$.setupState.copyingAgent).toEqual({ id: 'agent-1', name: 'CodeBuddy' })
  })

  it('clicking delete on default agent shows error toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    mockDefaultAgentId.value = 'agent-1'
    const wrapper = mountIndex()
    const deleteBtn = wrapper.find('.settings-agents-index__icon-btn--danger')
    await deleteBtn.trigger('click')

    expect(mockToastShow).toHaveBeenCalledWith('Cannot delete default agent', expect.any(Object))
    expect(mockDeleteAgent).not.toHaveBeenCalled()
  })

  it('clicking delete on non-default agent shows confirm dialog', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: '', sortOrder: 1 },
    ]
    mockDefaultAgentId.value = 'agent-1'
    const wrapper = mountIndex()
    // Second row's delete button
    const deleteButtons = wrapper.findAll('.settings-agents-index__icon-btn--danger')
    await deleteButtons[1].trigger('click')

    expect(mockConfirm).toHaveBeenCalled()
  })

  it('confirmed delete calls deleteAgent and shows success toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: '', sortOrder: 1 },
    ]
    mockDefaultAgentId.value = 'agent-1'
    mockConfirm.mockResolvedValueOnce(true)
    mockDeleteAgent.mockResolvedValueOnce(undefined)

    const wrapper = mountIndex()
    const deleteButtons = wrapper.findAll('.settings-agents-index__icon-btn--danger')
    await deleteButtons[1].trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockDeleteAgent).toHaveBeenCalledWith('agent-2')
    expect(mockToastShow).toHaveBeenCalledWith('Agent deleted', expect.any(Object))
  })

  it('failed delete shows error toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: '', sortOrder: 1 },
    ]
    mockDefaultAgentId.value = 'agent-1'
    mockConfirm.mockResolvedValueOnce(true)
    mockDeleteAgent.mockRejectedValueOnce(new Error('fail'))

    const wrapper = mountIndex()
    const deleteButtons = wrapper.findAll('.settings-agents-index__icon-btn--danger')
    await deleteButtons[1].trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Delete failed', expect.any(Object))
  })

  it('rescan failure shows error toast', async () => {
    mockRescanAgents.mockRejectedValueOnce(new Error('fail'))
    const wrapper = mountIndex()
    const rescanBtn = wrapper.find('.settings-agents-index__rescan-btn')
    await rescanBtn.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Rescan failed', expect.any(Object))
  })

  it('rescan success shows success toast', async () => {
    mockRescanAgents.mockResolvedValueOnce(undefined)
    const wrapper = mountIndex()
    const rescanBtn = wrapper.find('.settings-agents-index__rescan-btn')
    await rescanBtn.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Rescan complete', expect.any(Object))
  })

  it('rescan sets rescanning state while scanning', async () => {
    const wrapper = mountIndex()
    const vm = wrapper.vm as any

    // Verify rescanning starts as false
    expect(vm.$.setupState.rescanning).toBe(false)

    mockRescanAgents.mockResolvedValueOnce(undefined)
    await vm.$.setupState.handleRescan()
    await wrapper.vm.$nextTick()

    // After completion, rescanning is false again
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

  it('clicking delete button does not emit navigate', async () => {
    mockAgents.value = [
      { id: 'agent-2', name: 'Claude', icon: '🧠', specialty: '', sortOrder: 0 },
    ]
    mockDefaultAgentId.value = 'agent-1' // different, so not default
    const wrapper = mountIndex()
    const deleteBtn = wrapper.find('.settings-agents-index__icon-btn--danger')
    await deleteBtn.trigger('click')
    expect(wrapper.emitted('navigate')).toBeFalsy()
  })

  it('copy confirmed calls duplicateAgent and shows success toast', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', icon: '🤖', specialty: '', sortOrder: 0 },
    ]
    mockDuplicateAgent.mockResolvedValueOnce(undefined)

    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    // Simulate: open copy dialog, then confirm
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
})
