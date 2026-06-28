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
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock useDialog
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(false) }),
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
})
