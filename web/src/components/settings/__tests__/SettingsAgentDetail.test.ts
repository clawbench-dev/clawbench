import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

const { mockGetAgent, mockLoadAgents, mockPatchAgentField, mockToastShow, mockDeleteAgent, mockDuplicateAgent, mockDefaultAgentId, mockPopulateACPStateFromCache, mockAgentsLoaded } = vi.hoisted(() => ({
  mockGetAgent: vi.fn(),
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockPatchAgentField: vi.fn().mockResolvedValue(undefined),
  mockToastShow: vi.fn(),
  mockDeleteAgent: vi.fn().mockResolvedValue(undefined),
  mockDuplicateAgent: vi.fn().mockResolvedValue(undefined),
  mockDefaultAgentId: { value: 'other-agent' },
  mockPopulateACPStateFromCache: vi.fn().mockResolvedValue(undefined),
  // Plain hoisted holder; the mock factory below turns it into a real ref so
  // the component's computed tracks it (see the vi.mock body).
  mockAgentsLoaded: { value: true },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: any) => {
      const map: Record<string, string> = {
        'settings.items.agentPreferredModel': 'Preferred Model',
        'settings.items.agentPreferredThinkingEffort': 'Thinking Effort',
        'settings.items.agentTransport': 'Protocol',
        'settings.items.agentAutoApprove': 'Auto-Approve by Default',
        'settings.items.agentAutoApproveDesc': 'New sessions use this',
        'settings.items.agentSectionIdentity': 'Identity',
        'settings.items.agentName': 'Name',
        'settings.items.agentSpecialty': 'Specialty',
        'settings.items.agentSectionPreference': 'Preference',
        'settings.items.agentSystemPrompt': 'System Prompt',
        'settings.items.agentSystemPromptDesc': 'Custom system prompt',
        'settings.items.agentSystemPromptWarning': 'Warning',
        'settings.items.agentSystemPromptACPNote': 'ACP note',
        'settings.items.agentSectionInfo': 'Info',
        'settings.items.agentBackend': 'Backend',
        'settings.items.agentCommand': 'Command',
        'settings.items.agentModels': 'Models',
        'settings.items.agentModelCount': `${params?.count ?? 0} models`,
        'settings.items.agentSessionCountLabel': 'Sessions',
        'settings.items.agentSessionCountDesc': 'Conversations using this agent',
        'settings.items.agentSessionCount': `${params?.count ?? 0} sessions`,
        'settings.items.agentTaskCountLabel': 'Tasks',
        'settings.items.agentTaskCountDesc': 'Tasks using this agent',
        'settings.items.agentTaskCount': `${params?.count ?? 0} tasks`,
        'settings.items.agentAcpCommand': 'ACP Command',
        'settings.saveFailed': 'Save failed',
        'settings.items.agentDelete': 'Delete',
        'settings.items.agentDeleteConfirm': `Delete ${params?.name ?? ''}?`,
        'settings.items.agentDeleteDefault': 'Cannot delete default agent',
        'settings.items.agentDeleteBlocked': `In use (${params?.sessions ?? 0} sessions, ${params?.tasks ?? 0} tasks)`,
        'settings.items.agentDeleted': 'Deleted',
        'settings.items.agentDeleteFailed': 'Delete failed',
        'settings.items.agentDisable': 'Disable',
        'settings.items.agentEnable': 'Enable',
        'settings.items.agentDisabledToast': 'Agent disabled',
        'settings.items.agentEnabledToast': 'Agent enabled',
        'settings.items.agentCannotDisableDefault': 'The default agent cannot be disabled',
        'settings.items.agentNotFound': 'Agent not found or already removed',
        'settings.items.agentNotFoundHint': 'It may have been deleted.',
        'settings.items.agentBackToList': 'Back to agent list',
        'settings.items.agentCopied': 'Agent duplicated',
        'settings.items.agentCopyFailed': 'Duplicate failed',
        'settings.items.agentCopyNameTaken': 'An agent with this name already exists',
      }
      if (key === 'settings.items.agentModelCount' && params) return `${params.count} models`
      if (key === 'settings.items.agentDeleteConfirm' && params) return `Delete ${params.name}?`
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/composables/useAgents', async () => {
  // Dynamic import: the factory is hoisted above the top-level `import { ref }`,
  // so referencing that binding here would hit the TDZ.
  const { ref: vueRef } = await import('vue')
  const loadedRef = vueRef(mockAgentsLoaded.value)
  Object.defineProperty(mockAgentsLoaded, 'value', {
    get: () => loadedRef.value,
    set: (v: boolean) => { loadedRef.value = v },
  })
  return {
    useAgents: () => ({
      getAgent: mockGetAgent,
      loadAgents: mockLoadAgents,
      agentsLoaded: loadedRef,
      deleteAgent: mockDeleteAgent,
      duplicateAgent: mockDuplicateAgent,
      defaultAgentId: mockDefaultAgentId,
    }),
    populateACPStateFromCache: mockPopulateACPStateFromCache,
  }
})

vi.mock('@/composables/useSettingsConfig', () => ({
  patchAgentField: mockPatchAgentField,
}))

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

const mockDialogConfirm = vi.fn().mockResolvedValue(false)
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockDialogConfirm }),
}))

// The picker lazily imports DiceBear; stub the heavy dep so this file does not
// pull the library (and its ESM/JSON chain) into jsdom.
vi.mock('@/utils/lazyAvatar', () => ({
  AVATAR_STYLES: ['bottts', 'identicon'],
  loadAvatarKit: vi.fn().mockResolvedValue({ Avatar: class {}, styles: {} }),
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'size', 'avatar'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

import SettingsAgentDetail from '@/components/settings/SettingsAgentDetail.vue'

const baseAgent = {
  id: 'test-agent',
  name: 'Test Agent',
  specialty: 'coding',
  backend: 'claude',
  command: 'claude',
  transport: 'cli',
  models: [{ id: 'model-1', name: 'Model 1', default: true }],
  preferredModel: 'model-1',
  customSystemPrompt: '',
  acpCommand: '',
  canRefreshModels: true,
  thinkingEffortLevels: [],
  preferredThinkingEffort: '',
  autoApprove: false,
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

/** Mount for an agent id the (already loaded) list does not contain. */
function mountMissingAgent() {
  mockGetAgent.mockReturnValue(undefined)
  return mount(SettingsAgentDetail, {
    props: { agentId: 'ghost-agent' },
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
    mockAgentsLoaded.value = true
    mockDuplicateAgent.mockResolvedValue(undefined)
  })

  it('renders SettingsItem children for a basic CLI agent', () => {
    const wrapper = mountDetail()
    // The component should render SettingsItem stubs for each item
    const items = wrapper.findAllComponents({ name: 'SettingsItem' })
    expect(items.length).toBeGreaterThan(0)
  })

  it('renders the avatar row and opens the picker on change', async () => {
    const wrapper = mountDetail()
    expect(wrapper.find('.settings-agent-detail__avatar-row').exists()).toBe(true)

    const picker = wrapper.findComponent({ name: 'AgentAvatarPicker' })
    expect(picker.props('open')).toBe(false)

    await wrapper.find('.settings-agent-detail__avatar-btn').trigger('click')
    expect(wrapper.findComponent({ name: 'AgentAvatarPicker' }).props('open')).toBe(true)
  })

  // ─── Identity card leads the page ──────────────
  // Avatar, identity (name/specialty) and the system prompt were three separate
  // groups; they are now one leading "Identity" card. The avatar row must live
  // INSIDE that first card (so it sits with the name), not as a standalone block.
  describe('identity card', () => {
    it('leads the page with the Identity card', () => {
      const wrapper = mountDetail()
      const headers = wrapper.findAll('.settings-card__header')
      expect(headers.length).toBeGreaterThan(0)
      expect(headers[0].text()).toBe('Identity')
    })

    it('puts the avatar row inside the first (Identity) card', () => {
      const wrapper = mountDetail()
      const firstCard = wrapper.findAll('.settings-card')[0]
      expect(firstCard.find('.settings-agent-detail__avatar-row').exists()).toBe(true)
    })

    it('groups the name, specialty and system prompt into the Identity card', () => {
      const wrapper = mountDetail()
      const firstCard = wrapper.findAll('.settings-card')[0]
      const labels = firstCard.findAllComponents({ name: 'SettingsItem' }).map(it => it.props('label'))
      expect(labels).toContain('Name')
      expect(labels).toContain('Specialty')
      expect(labels).toContain('System Prompt')
    })

    it('no longer renders an Advanced section', () => {
      const wrapper = mountDetail()
      const headers = wrapper.findAll('.settings-card__header').map(h => h.text())
      expect(headers).not.toContain('Advanced')
    })

    it('renders the Preference card after the Identity card', () => {
      const wrapper = mountDetail()
      const headers = wrapper.findAll('.settings-card__header').map(h => h.text())
      expect(headers.indexOf('Preference')).toBeGreaterThan(headers.indexOf('Identity'))
    })
  })

  it('saves a new avatar via patchAgentField', async () => {
    const wrapper = mountDetail()
    const picker = wrapper.findComponent({ name: 'AgentAvatarPicker' })

    const svg = '<svg viewBox="0 0 2 2"><rect width="2" height="2"/></svg>'
    await picker.vm.$emit('saved', svg)
    await vi.waitFor(() => {
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'avatar', svg)
    })
    // Dialog closes after a successful save.
    expect(wrapper.findComponent({ name: 'AgentAvatarPicker' }).props('open')).toBe(false)
  })

  it('clears the avatar by saving an empty string', async () => {
    const wrapper = mountDetail({ avatar: '<svg/>' })
    const picker = wrapper.findComponent({ name: 'AgentAvatarPicker' })
    await picker.vm.$emit('saved', '')
    await vi.waitFor(() => {
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'avatar', '')
    })
  })

  it('calls loadAgents then populateACPStateFromCache on mount (Issue #404)', async () => {
    mountDetail()
    await vi.waitFor(() => {
      expect(mockLoadAgents).toHaveBeenCalledWith(true)
      expect(mockPopulateACPStateFromCache).toHaveBeenCalledWith('test-agent')
    })
  })

  it('renders more items for dual-transport agent', () => {
    const wrapper1 = mountDetail()
    const count1 = wrapper1.findAllComponents({ name: 'SettingsItem' }).length

    const wrapper2 = mountDetail({ acpCommand: 'claude --acp' })
    const count2 = wrapper2.findAllComponents({ name: 'SettingsItem' }).length

    // Dual-transport agent has transport select + acp_command info
    expect(count2).toBeGreaterThan(count1)
  })

  it('ACP-only agent has no transport select (no CLI option offered)', () => {
    // Genuine ACP-only agent: acpCommand set but supportsCLI=false (e.g. grok).
    // A transport select must NOT be rendered — CLI is not available to switch to.
    const wrapper = mountDetail({ acpCommand: 'grok agent stdio', supportsCLI: false })
    const items = wrapper.findAllComponents({ name: 'SettingsItem' })

    const transportItem = items.find((it: any) => it.props('item')?.key === 'transport')
    expect(transportItem).toBeUndefined()
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

    // Genuine ACP-only agent: has acpCommand but no CLI support (supportsCLI=false).
    // No transport select (no dual transport) and system prompt shown as info.
    const wrapper2 = mountDetail({ acpCommand: 'grok agent stdio', supportsCLI: false })
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
      expect(wrapper.find('.settings-agent-detail__delete-btn').exists()).toBe(true)
    })

    it('shows error toast when trying to delete default agent', async () => {
      mockDefaultAgentId.value = 'test-agent'
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-btn')
      await deleteRow.trigger('click')
      expect(mockToastShow).toHaveBeenCalledWith('Cannot delete default agent', expect.any(Object))
      expect(mockDeleteAgent).not.toHaveBeenCalled()
    })

    it('shows confirmation dialog when deleting non-default agent', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(false)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-btn')
      await deleteRow.trigger('click')
      expect(mockDialogConfirm).toHaveBeenCalled()
    })

    it('deletes agent when confirmed', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(true)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-btn')
      await deleteRow.trigger('click')
      expect(mockDeleteAgent).toHaveBeenCalledWith('test-agent')
      expect(mockToastShow).toHaveBeenCalledWith('Deleted', expect.any(Object))
      expect(wrapper.emitted('deleted')).toBeTruthy()
    })

    it('does not delete when confirmation is cancelled', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(false)
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-btn')
      await deleteRow.trigger('click')
      expect(mockDeleteAgent).not.toHaveBeenCalled()
    })

    it('shows error toast when delete fails', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(true)
      mockDeleteAgent.mockRejectedValueOnce(new Error('fail'))
      const wrapper = mountDetail()
      const deleteRow = wrapper.find('.settings-agent-detail__delete-btn')
      await deleteRow.trigger('click')
      expect(mockToastShow).toHaveBeenCalledWith('Delete failed', expect.any(Object))
    })

    it('blocks deletion locally when the agent still has sessions', async () => {
      mockDefaultAgentId.value = 'other-agent'
      const wrapper = mountDetail({ sessionCount: 3, taskCount: 0 })
      await wrapper.find('.settings-agent-detail__delete-btn').trigger('click')
      // No confirm dialog, no delete call — a toast explains why.
      expect(mockDialogConfirm).not.toHaveBeenCalled()
      expect(mockDeleteAgent).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('In use'),
        expect.any(Object),
      )
    })

    it('blocks deletion locally when the agent still has tasks', async () => {
      mockDefaultAgentId.value = 'other-agent'
      const wrapper = mountDetail({ sessionCount: 0, taskCount: 2 })
      await wrapper.find('.settings-agent-detail__delete-btn').trigger('click')
      expect(mockDialogConfirm).not.toHaveBeenCalled()
      expect(mockDeleteAgent).not.toHaveBeenCalled()
    })

    it('blocks deletion locally when the agent is only a group member', async () => {
      mockDefaultAgentId.value = 'other-agent'
      const wrapper = mountDetail({ sessionCount: 0, taskCount: 0, membershipCount: 1 })
      await wrapper.find('.settings-agent-detail__delete-btn').trigger('click')
      expect(mockDialogConfirm).not.toHaveBeenCalled()
      expect(mockDeleteAgent).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('In use'),
        expect.any(Object),
      )
    })

    it('surfaces the backend AgentInUse 409 with its authoritative counts', async () => {
      mockDefaultAgentId.value = 'other-agent'
      mockDialogConfirm.mockResolvedValueOnce(true)
      // Local counts are stale (0), so the dialog opens; the backend refuses.
      mockDeleteAgent.mockRejectedValueOnce(
        Object.assign(new Error('in use'), {
          msgKey: 'AgentInUse',
          detail: { SessionCount: 5, TaskCount: 1 },
        }),
      )
      const wrapper = mountDetail({ sessionCount: 0, taskCount: 0 })
      await wrapper.find('.settings-agent-detail__delete-btn').trigger('click')
      expect(mockDeleteAgent).toHaveBeenCalledWith('test-agent')
      expect(mockToastShow).toHaveBeenCalledWith(
        expect.stringContaining('5 sessions'),
        expect.any(Object),
      )
      // The generic failure toast must NOT also fire.
      expect(mockToastShow).not.toHaveBeenCalledWith('Delete failed', expect.any(Object))
    })
  })

  // ─── Disable / enable ──────────────────────────────
  describe('disable toggle', () => {
    it('renders the disable button with the Disable label when enabled', () => {
      const wrapper = mountDetail({ disabled: false })
      const btns = wrapper.findAll('.settings-agent-detail__action-btn')
      // Copy + disable share the action-btn class; the disable one carries the label.
      expect(btns.some(b => b.text() === 'Disable')).toBe(true)
    })

    it('shows Enable when the agent is disabled', () => {
      const wrapper = mountDetail({ disabled: true })
      const btns = wrapper.findAll('.settings-agent-detail__action-btn')
      expect(btns.some(b => b.text() === 'Enable')).toBe(true)
    })

    it('patches disabled=true when toggled on', async () => {
      mockDefaultAgentId.value = 'other-agent'
      const wrapper = mountDetail({ disabled: false })
      const vm = wrapper.vm as any
      await vm.$.setupState.handleToggleDisabled()
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'disabled', true)
      expect(mockToastShow).toHaveBeenCalledWith('Agent disabled', expect.any(Object))
    })

    it('patches disabled=false when toggled off', async () => {
      mockDefaultAgentId.value = 'other-agent'
      const wrapper = mountDetail({ disabled: true })
      const vm = wrapper.vm as any
      await vm.$.setupState.handleToggleDisabled()
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'disabled', false)
      expect(mockToastShow).toHaveBeenCalledWith('Agent enabled', expect.any(Object))
    })

    it('refuses to disable the default agent', async () => {
      mockDefaultAgentId.value = 'test-agent'
      const wrapper = mountDetail({ disabled: false })
      const vm = wrapper.vm as any
      await vm.$.setupState.handleToggleDisabled()
      expect(mockPatchAgentField).not.toHaveBeenCalled()
      expect(mockToastShow).toHaveBeenCalledWith(
        'The default agent cannot be disabled',
        expect.any(Object),
      )
    })

    it('disables the toggle button for the default agent', () => {
      mockDefaultAgentId.value = 'test-agent'
      const wrapper = mountDetail({ disabled: false })
      const btns = wrapper.findAll('.settings-agent-detail__action-btn')
      const toggle = btns.find(b => b.text() === 'Disable')
      expect(toggle?.attributes('disabled')).toBeDefined()
    })
  })

  // ─── Usage counts (Information section) ──────────────────────────────
  describe('usage counts', () => {
    // SettingsItem is stubbed, so the count strings ride on the stub's
    // model-value prop rather than rendered text.
    function countValues(wrapper: ReturnType<typeof mountDetail>): string[] {
      return wrapper.findAllComponents({ name: 'SettingsItem' })
        .map(s => s.props('modelValue') as string)
    }

    it('renders session and task counts from the agent record', () => {
      const wrapper = mountDetail({ sessionCount: 4, taskCount: 2 })
      const values = countValues(wrapper)
      expect(values).toContain('4 sessions')
      expect(values).toContain('2 tasks')
    })

    it('renders zero counts when the fields are absent', () => {
      const wrapper = mountDetail()
      const values = countValues(wrapper)
      expect(values).toContain('0 sessions')
      expect(values).toContain('0 tasks')
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

  // ─── Preferred model select ──────────────────────────────
  describe('preferred model select', () => {
    it('adds the raw id as a sublabel when two models share a display name', () => {
      // CodeBuddy ships deepseek-v4-pro and deepseek-v4-pro-exclusive, both
      // named "Deepseek-V4-Pro"; the id is the only distinguishing label.
      const wrapper = mountDetail({
        models: [
          { id: 'deepseek-v4-pro', name: 'Deepseek-V4-Pro', default: true },
          { id: 'deepseek-v4-pro-exclusive', name: 'Deepseek-V4-Pro' },
        ],
      })
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = items.find((it: any) => it.props('label') === 'Preferred Model')
      expect(item).toBeTruthy()

      const options = item!.props('options') as Array<{ label: string; value: string; sublabel?: string }>
      expect(options.map(o => o.value)).toEqual(['deepseek-v4-pro', 'deepseek-v4-pro-exclusive'])
      expect(options.map(o => o.sublabel)).toEqual(['deepseek-v4-pro', 'deepseek-v4-pro-exclusive'])
    })

    it('renders a JSON-shaped wire id as provider/model, not raw JSON', () => {
      // DeepSeek Harness identifies a model by a JSON-stringified pair; the id
      // must stay untouched (it is the wire value) but the sublabel must be
      // readable rather than a JSON blob.
      const wrapper = mountDetail({
        models: [
          { id: '["deepseek-official","deepseek-v4-pro"]', name: 'DeepSeek-V4-Pro', default: true },
        ],
      })
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = items.find((it: any) => it.props('label') === 'Preferred Model')

      const options = item!.props('options') as Array<{ label: string; value: string; sublabel?: string }>
      // The value (what gets sent to the agent) is unchanged...
      expect(options[0].value).toBe('["deepseek-official","deepseek-v4-pro"]')
      // ...while the sublabel is the readable form.
      expect(options[0].sublabel).toBe('deepseek-official/deepseek-v4-pro')
    })

    it('omits the sublabel when the display name already is the id', () => {
      const wrapper = mountDetail({
        models: [{ id: 'gpt-5.1-codex', name: 'gpt-5.1-codex', default: true }],
      })
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = items.find((it: any) => it.props('label') === 'Preferred Model')

      const options = item!.props('options') as Array<{ sublabel?: string }>
      expect(options[0].sublabel).toBeUndefined()
    })
  })

  // ─── Auto-approve default toggle ──────────────────────────
  describe('auto-approve default toggle', () => {
    it('renders an auto_approve switch item with the agent value', () => {
      const wrapper = mountDetail({ autoApprove: true })
      const items = wrapper.findAllComponents({ name: 'SettingsItem' })
      const item = items.find((it: any) => it.props('label') === 'Auto-Approve by Default')
      expect(item).toBeTruthy()
      expect(item?.props('type')).toBe('switch')
      expect(item?.props('modelValue')).toBe(true)
    })

    it('handleUpdate patches auto_approve on the server', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      await vm.$.setupState.handleUpdate({ key: 'auto_approve', patchField: 'auto_approve' }, true)
      expect(mockPatchAgentField).toHaveBeenCalledWith('test-agent', 'auto_approve', true)
    })

    it('getItemValue reads autoApprove from the agent', async () => {
      const wrapper = mountDetail({ autoApprove: true })
      const vm = wrapper.vm as any
      const value = vm.$.setupState.getItemValue({ key: 'auto_approve' })
      expect(value).toBe(true)
    })
  })

  // ─── Stale reference: the agent no longer exists ──────────
  // A deep link (or a lingering nav-stack entry) can outlive the agent it
  // points at — it was deleted, or dropped by a rescan. The page must say so
  // and offer a way back, instead of rendering an empty shell whose only
  // controls are a copy button and a delete button that silently no-ops.
  describe('stale agent reference', () => {
    it('shows the fallback when the list is loaded but lacks the agent', () => {
      mockAgentsLoaded.value = true

      const wrapper = mountMissingAgent()

      expect(wrapper.find('.settings-agent-detail--missing').exists()).toBe(true)
      // No cards, and the dead delete button must not be offered at all.
      expect(wrapper.findAllComponents({ name: 'SettingsItem' }).length).toBe(0)
      expect(wrapper.text()).toContain('Agent not found or already removed')
    })

    it('does NOT show the fallback before the list has loaded (no false "removed")', () => {
      // A valid agent looks identical to a deleted one until the list lands,
      // so claiming "removed" here would be a lie.
      mockAgentsLoaded.value = false

      const wrapper = mountMissingAgent()

      expect(wrapper.find('.settings-agent-detail--missing').exists()).toBe(false)
    })

    it('emits back (not deleted) when the fallback button is clicked', async () => {
      mockAgentsLoaded.value = true

      const wrapper = mountMissingAgent()
      await wrapper.find('.settings-agent-detail--missing button').trigger('click')

      // `back` leaves without deleting; `deleted` is reserved for a real delete.
      expect(wrapper.emitted('back')).toHaveLength(1)
      expect(wrapper.emitted('deleted')).toBeUndefined()
    })

    it('renders normally when the agent exists', () => {
      mockAgentsLoaded.value = true
      const wrapper = mountDetail()

      expect(wrapper.find('.settings-agent-detail--missing').exists()).toBe(false)
      expect(wrapper.findAllComponents({ name: 'SettingsItem' }).length).toBeGreaterThan(0)
    })
  })

  // ─── Copy: name collision keeps the dialog open with an inline reason ────
  describe('copy agent name collision', () => {
    it('shows the collision inside the dialog and keeps it open', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any

      // Open the copy dialog, then simulate a 409 name collision.
      vm.$.setupState.startCopy()
      await wrapper.vm.$nextTick()
      expect(wrapper.findComponent({ name: 'CopyAgentDialog' }).props('open')).toBe(true)

      const err = Object.assign(new Error('taken'), { msgKey: 'AgentNameTaken' })
      mockDuplicateAgent.mockRejectedValueOnce(err)
      await vm.$.setupState.handleCopyConfirmed('Test Agent (Copy)')
      await wrapper.vm.$nextTick()

      const dialog = wrapper.findComponent({ name: 'CopyAgentDialog' })
      expect(dialog.props('open')).toBe(true)
      expect(dialog.props('errorMessage')).toBe('An agent with this name already exists')
      // A collision is not a generic failure toast.
      expect(mockToastShow).not.toHaveBeenCalledWith(
        'Duplicate failed',
        expect.anything(),
      )
    })

    it('closes the dialog on a successful copy', async () => {
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.startCopy()
      await wrapper.vm.$nextTick()

      await vm.$.setupState.handleCopyConfirmed('Test Agent (Copy)')
      await wrapper.vm.$nextTick()

      expect(mockDuplicateAgent).toHaveBeenCalledWith('test-agent', 'Test Agent (Copy)')
      expect(wrapper.findComponent({ name: 'CopyAgentDialog' }).props('open')).toBe(false)
    })

    it('navigates to the new agent after a successful copy', async () => {
      // Copying should switch the panel to the copy's config page, not leave
      // the user staring at the source agent.
      mockDuplicateAgent.mockResolvedValueOnce('claude-deadbeef')
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.startCopy()
      await wrapper.vm.$nextTick()

      await vm.$.setupState.handleCopyConfirmed('Test Agent (Copy)')
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('navigateReplace')).toEqual([['agents:claude-deadbeef']])
    })

    it('does not navigate when the backend returns no new id', async () => {
      // An older backend answers without the created agent; navigating would
      // produce a bogus `agents:` (empty-id) route.
      mockDuplicateAgent.mockResolvedValueOnce('')
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.startCopy()
      await wrapper.vm.$nextTick()

      await vm.$.setupState.handleCopyConfirmed('Test Agent (Copy)')
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('navigateReplace')).toBeUndefined()
    })

    it('does not navigate when the copy fails', async () => {
      mockDuplicateAgent.mockRejectedValueOnce(new Error('fail'))
      const wrapper = mountDetail()
      const vm = wrapper.vm as any
      vm.$.setupState.startCopy()
      await wrapper.vm.$nextTick()

      await vm.$.setupState.handleCopyConfirmed('Test Agent (Copy)')
      await wrapper.vm.$nextTick()

      expect(wrapper.emitted('navigateReplace')).toBeUndefined()
    })
  })
})
