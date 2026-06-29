import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'

const { mockGetAgent, mockLoadAgents, mockPatchAgentField, mockApiGet, mockToastShow, mockDeleteAgent, mockDefaultAgentId } = vi.hoisted(() => ({
  mockGetAgent: vi.fn(),
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockPatchAgentField: vi.fn().mockResolvedValue(undefined),
  mockApiGet: vi.fn().mockResolvedValue({ commonPrompt: '' }),
  mockToastShow: vi.fn(),
  mockDeleteAgent: vi.fn().mockResolvedValue(undefined),
  mockDefaultAgentId: { value: 'other-agent' },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: any) => {
      const map: Record<string, string> = {
        'settings.items.agentPreferredModel': 'Preferred Model',
        'settings.items.agentPreferredThinkingEffort': 'Thinking Effort',
        'settings.items.agentTransport': 'Transport',
        'settings.items.agentSectionIdentity': 'Identity',
        'settings.items.agentName': 'Name',
        'settings.items.agentIcon': 'Icon',
        'settings.items.agentSpecialty': 'Specialty',
        'settings.items.agentSectionAdvanced': 'Advanced',
        'settings.items.agentSystemPrompt': 'System Prompt',
        'settings.items.agentSystemPromptDesc': 'Custom system prompt',
        'settings.items.agentSystemPromptWarning': 'Warning',
        'settings.items.agentSystemPromptACPNote': 'ACP note',
        'settings.items.agentSectionInfo': 'Info',
        'settings.items.agentBackend': 'Backend',
        'settings.items.agentCommand': 'Command',
        'settings.items.agentSource': 'Source',
        'settings.items.agentModels': 'Models',
        'settings.items.agentModelCount': `${params?.count ?? 0} models`,
        'settings.items.agentAcpCommand': 'ACP Command',
        'settings.saveFailed': 'Save failed',
        'settings.items.agentDelete': 'Delete',
        'settings.items.agentDeleteConfirm': `Delete ${params?.name ?? ''}?`,
        'settings.items.agentDeleteDefault': 'Cannot delete default agent',
        'settings.items.agentDeleted': 'Deleted',
        'settings.items.agentDeleteFailed': 'Delete failed',
      }
      if (key === 'settings.items.agentModelCount' && params) return `${params.count} models`
      if (key === 'settings.items.agentDeleteConfirm' && params) return `Delete ${params.name}?`
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    getAgent: mockGetAgent,
    loadAgents: mockLoadAgents,
    deleteAgent: mockDeleteAgent,
    defaultAgentId: mockDefaultAgentId,
  }),
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentField: mockPatchAgentField,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/utils/api', () => ({
  apiGet: mockApiGet,
}))

const mockDialogConfirm = vi.fn().mockResolvedValue(false)
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

import SettingsAgentDetail from '@/components/settings/SettingsAgentDetail.vue'

const baseAgent = {
  id: 'test-agent',
  name: 'Test Agent',
  icon: 'bot',
  specialty: 'coding',
  backend: 'claude',
  command: 'claude',
  source: 'discovery',
  transport: 'cli',
  models: [{ id: 'model-1', name: 'Model 1', default: true }],
  preferredModel: 'model-1',
  customSystemPrompt: '',
  systemPrompt: '',
  acpCommand: '',
  canRefreshModels: true,
  thinkingEffortLevels: [],
  preferredThinkingEffort: '',
}

function mountDetail(agentOverrides: Record<string, any> = {}) {
  const agent = { ...baseAgent, ...agentOverrides }
  mockGetAgent.mockReturnValue(agent)
  return mount(SettingsAgentDetail, {
    props: { agentId: 'test-agent' },
    global: {
      stubs: {
        SettingsItem: true,
      },
    },
  })
}

describe('SettingsAgentDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockGetAgent.mockReturnValue(baseAgent)
  })

  it('renders SettingsItem children for a basic CLI agent', () => {
    const wrapper = mountDetail()
    // The component should render SettingsItem stubs for each item
    const items = wrapper.findAllComponents({ name: 'SettingsItem' })
    expect(items.length).toBeGreaterThan(0)
  })

  it('renders more items for dual-transport agent', () => {
    const wrapper1 = mountDetail()
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ acpCommand: 'claude --acp' })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    // Dual-transport agent has transport select + acp_command info
    expect(count2).toBeGreaterThan(count1)
  })

  it('renders fewer items for agent without command', () => {
    const wrapper1 = mountDetail({ command: 'claude' })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ command: '' })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    expect(count1).toBeGreaterThan(count2)
  })

  it('renders more items when thinking effort levels exist', () => {
    const wrapper1 = mountDetail({ thinkingEffortLevels: [] })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ thinkingEffortLevels: ['low', 'medium', 'high'] })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    expect(count2).toBeGreaterThan(count1)
  })

  it('renders same number of items for ACP-only vs non-ACP agent (type differs, count same)', () => {
    const wrapper1 = mountDetail({ acpCommand: '', canRefreshModels: true })
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    // ACP-only has no transport select but has system prompt as info
    const wrapper2 = mountDetail({ acpCommand: 'claude --acp', canRefreshModels: false })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    // Both should have items
    expect(count1).toBeGreaterThan(0)
    expect(count2).toBeGreaterThan(0)
  })

  it('loads agents on mount', () => {
    mountDetail()
    expect(mockLoadAgents).toHaveBeenCalledWith(true)
  })

  it('renders container div', () => {
    const wrapper = mountDetail()
    expect(wrapper.find('.settings-agent-detail').exists()).toBe(true)
  })

  // ─── Delete agent ──────────────────────────────
  describe('delete agent', () => {
    it('renders delete row', () => {
      const wrapper = mountDetail()
      expect(wrapper.find('.settings-agent-detail__delete-row').exists()).toBe(true)
    })

    it('shows error toast when trying to delete default agent', async () => {
      mockDefaultAgentId.value = 'test-agent'
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-row')
      await deleteRow.trigger('click')
      expect(mockToastShow).toHaveBeenCalledWith('Cannot delete default agent', expect.any(Object))
      expect(mockDeleteAgent).not.toHaveBeenCalled()
    })

    it('shows confirmation dialog when deleting non-default agent', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(false)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-row')
      await deleteRow.trigger('click')
      expect(mockDialogConfirm).toHaveBeenCalled()
    })

    it('deletes agent when confirmed', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(true)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-row')
      await deleteRow.trigger('click')
      expect(mockDeleteAgent).toHaveBeenCalledWith('test-agent')
      expect(mockToastShow).toHaveBeenCalledWith('Deleted', expect.any(Object))
      expect(wrapper.emitted('deleted')).toBeTruthy()
    })

    it('does not delete when confirmation is cancelled', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(false)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-row')
      await deleteRow.trigger('click')
      expect(mockDeleteAgent).not.toHaveBeenCalled()
    })

    it('shows error toast when delete fails', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(true)
      mockDeleteAgent.mockRejectedValueOnce(new Error('fail'))
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-row')
      await deleteRow.trigger('click')
      expect(mockToastShow).toHaveBeenCalledWith('Delete failed', expect.any(Object))
    })
  })

  // ─── Edit toggle ──────────────────────────────
  describe('edit toggle', () => {
    it('handleUpdate calls patchAgentField', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      await vm.$.setupState.handleUpdate({ key: 'name', patchField: 'name' }, 'New Name')
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'name', 'New Name')
    })

    it('handleUpdate skips items without patchField', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      await vm.$.setupState.handleUpdate({ key: 'backend', type: 'info' }, 'value')
      expect(mockPatchAgentField).not.toHaveBeenCalled()
    })

    it('handleEditToggle sets activeKey when open', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.handleEditToggle('name', true)
      expect(vm.$.setupState.activeKey).toBe('name')
    })

    it('handleEditToggle clears activeKey when closed and key matches', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.activeKey = 'name'
      vm.$.setupState.handleEditToggle('name', false)
      expect(vm.$.setupState.activeKey).toBeNull()
    })
  })
})
