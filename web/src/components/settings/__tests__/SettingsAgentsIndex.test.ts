import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { ref, defineComponent } from 'vue'
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
          agentReorderFailed: 'Reorder failed',
          agentDragHandle: 'Drag to reorder',
          agentCreateAcp: 'Add ACP Agent',
          agentCreateAcpSuccess: 'ACP agent added',
          agentCreateAcpFailed: 'Failed to add agent',
          agentCreateAcpCommandRequired: 'The ACP command cannot be empty',
          agentCreateAcpInvalidCommand: 'Invalid ACP command',
          agentCreateAcpInvalidName: 'Invalid agent name',
          agentCopyNameTaken: 'An agent with this name already exists',
          agentNameReserved: 'This name conflicts with the group-chat user nickname',
          agentSelfTestAll: 'Test All Agents',
          agentSelfTestAllRunning: 'Testing…',
          agentSelfTestStatusOk: 'OK',
          agentSelfTestStatusWarn: 'WARN',
          agentSelfTestStatusFail: 'FAIL',
          agentSelfTestAllFailed: 'Test run failed',
          agentSelfTestNoResult: 'No result returned',
          // vue-i18n interpolates these for real here (this file uses a real
          // i18n instance, not a t() stub), so the placeholders must exist or
          // the rendered text keeps the raw {params}.
          agentSelfTestAllSummary: '{ok} OK, {warn} warning(s), {fail} failed',
          agentSelfTestPassedCount: 'All {passed} checks passed',
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
const mockReorderAgents = vi.fn().mockResolvedValue(undefined)
const mockCreateAcpAgent = vi.fn()
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: mockAgents,
    defaultAgentId: mockDefaultAgentId,
    loadAgents: (...args: unknown[]) => mockLoadAgents(...args),
    duplicateAgent: (...args: unknown[]) => mockDuplicateAgent(...args),
    rescanAgents: (...args: unknown[]) => mockRescanAgents(...args),
    setDefaultAgent: (...args: unknown[]) => mockSetDefaultAgent(...args),
    reorderAgents: (...args: unknown[]) => mockReorderAgents(...args),
    createAcpAgent: (...args: unknown[]) => mockCreateAcpAgent(...args),
  }),
}))

// Mock vue-draggable-plus (same stub shape as QuickSendDrawer.test.ts). The
// real component reorders the bound array; tests set localAgents and call
// onDragEnd directly instead of simulating a drag.
vi.mock('vue-draggable-plus', () => ({
  VueDraggable: defineComponent({
    name: 'VueDraggable',
    props: ['modelValue', 'handle'],
    emits: ['update:modelValue', 'end'],
    template: `<div class="vdp-stub"><slot /></div>`,
  }),
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: (...args: any[]) => mockToastShow(...args) }),
}))

// Mock lucide-vue-next
vi.mock('lucide-vue-next', () => ({
  Activity: { name: 'Activity', template: '<span class="icon-activity" />' },
  ChevronRight: { name: 'ChevronRight', template: '<span class="icon-chevron" />' },
  Copy: { name: 'Copy', template: '<span class="icon-copy" />' },
  Plus: { name: 'Plus', template: '<span class="icon-plus" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
}))

// Mock CreateAcpAgentDialog — the flow under test is the index page's
// open/create/navigate handling, not the dialog's own form.
// Test-all drives one composable instance per agent; the mock hands out a fresh
// controllable instance each call so the test can script per-agent outcomes.
const mockSelfTestRuns = vi.fn()
vi.mock('@/composables/useAgentSelfTest', () => ({
  useAgentSelfTest: () => {
    const state = { testing: { value: false }, result: { value: null as unknown }, error: { value: '' } }
    const run = async (id: string) => {
      const scripted = mockSelfTestRuns(id)
      state.result.value = scripted.result ?? null
      state.error.value = scripted.error ?? ''
    }
    return { ...state, run, clear: vi.fn() }
  },
}))

vi.mock('@/components/settings/CreateAcpAgentDialog.vue', () => ({
  default: {
    name: 'CreateAcpAgentDialog',
    props: ['open', 'errorMessage'],
    emits: ['close', 'confirmed'],
    template: '<div class="mock-create-acp-dialog" :data-open="String(open)" />',
  },
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
    mockReorderAgents.mockReset().mockResolvedValue(undefined)
    mockCreateAcpAgent.mockReset()
    mockSelfTestRuns.mockReset()
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
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: 'coding', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: 'analysis', sortOrder: 1 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.text()).toContain('CodeBuddy')
    expect(wrapper.text()).toContain('Claude')
  })

  it('renders default badge for default agent', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 1 },
    ]
    mockDefaultAgentId.value = 'agent-1'
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__default-badge').exists()).toBe(true)
  })

  it('does not render default badge for non-default agent', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    mockDefaultAgentId.value = 'agent-2'
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__default-badge').exists()).toBe(false)
  })

  it('emits navigate with agent ID on row click', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const row = wrapper.find('.settings-agents-index__row')
    await row.trigger('click')
    expect(wrapper.emitted('navigate')).toBeTruthy()
    expect(wrapper.emitted('navigate')![0]).toEqual(['agents:agent-1'])
  })

  it('renders rescan row', () => {
    const wrapper = mountIndex()
    expect(wrapper.find('[data-action="agent-rescan"]').exists()).toBe(true)
  })

  it('clicking rescan row calls rescanAgents', async () => {
    mockRescanAgents.mockResolvedValue(undefined)
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('[data-action="agent-rescan"]')
    await rescanRow.trigger('click')
    expect(mockRescanAgents).toHaveBeenCalled()
  })

  it('rescan failure shows error toast', async () => {
    mockRescanAgents.mockRejectedValueOnce(new Error('fail'))
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('[data-action="agent-rescan"]')
    await rescanRow.trigger('click')
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Rescan failed', expect.any(Object))
  })

  it('rescan success shows success toast', async () => {
    mockRescanAgents.mockResolvedValueOnce(undefined)
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('[data-action="agent-rescan"]')
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
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 1 },
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const names = wrapper.findAll('.settings-agents-index__name')
    expect(names[0].text()).toBe('CodeBuddy')
    expect(names[1].text()).toBe('Claude')
  })

  it('does not render delete button in agent rows', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__icon-btn--danger').exists()).toBe(false)
  })

  it('renders specialty text when agent has specialty', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: 'coding', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.find('.settings-agents-index__specialty').exists()).toBe(true)
    expect(wrapper.find('.settings-agents-index__specialty').text()).toBe('coding')
  })

  it('does not render specialty element when agent has no specialty', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    // v-if="agent.specialty" should not render the span
    expect(wrapper.find('.settings-agents-index__specialty').exists()).toBe(false)
  })

  it('disables rescan row while scanning', async () => {
    let resolveRescan: () => void
    mockRescanAgents.mockReturnValueOnce(new Promise<void>(r => { resolveRescan = r }))
    const wrapper = mountIndex()

    // Call handleRescan directly and let it run until the first await
    const vm = wrapper.vm as any
    const rescanPromise = vm.$.setupState.handleRescan()
    // The async function sets rescanning=true synchronously before the first await
    // but Vue needs to process the reactive update and re-render
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    // Verify rescanning is true (the async function is awaiting the promise)
    expect(vm.$.setupState.rescanning).toBe(true)

    resolveRescan!()
    await rescanPromise
    await wrapper.vm.$nextTick()
    expect(vm.$.setupState.rescanning).toBe(false)
  })

  // --- create custom ACP agent (issue #557) ---

  it('renders a create-ACP row that opens the dialog', async () => {
    const wrapper = mountIndex()
    const dialog = wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
    expect(dialog.props('open')).toBe(false)

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    expect(wrapper.findComponent({ name: 'CreateAcpAgentDialog' }).props('open')).toBe(true)
  })

  it('creates the agent and navigates to its detail page', async () => {
    mockCreateAcpAgent.mockResolvedValueOnce('custom-acp-abc123')
    const wrapper = mountIndex()

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
      .vm.$emit('confirmed', { name: 'Trae', command: 'trae acp' })
    await wrapper.vm.$nextTick()

    expect(mockCreateAcpAgent).toHaveBeenCalledWith('Trae', 'trae acp')
    expect(wrapper.emitted('navigate')![0]).toEqual(['agents:custom-acp-abc123'])
    expect(mockToastShow).toHaveBeenCalledWith('ACP agent added', expect.any(Object))
  })

  it('keeps the dialog open with an inline message on a name collision', async () => {
    const err = Object.assign(new Error('taken'), { msgKey: 'AgentNameTaken' })
    mockCreateAcpAgent.mockRejectedValueOnce(err)
    const wrapper = mountIndex()

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
      .vm.$emit('confirmed', { name: 'Trae', command: 'trae acp' })
    await wrapper.vm.$nextTick()

    const dialog = wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
    expect(dialog.props('open')).toBe(true)
    expect(dialog.props('errorMessage')).toBe('An agent with this name already exists')
    expect(wrapper.emitted('navigate')).toBeUndefined()
  })

  it('clears a previous error when the dialog is reopened', async () => {
    const err = Object.assign(new Error('taken'), { msgKey: 'AgentNameTaken' })
    mockCreateAcpAgent.mockRejectedValueOnce(err)
    const wrapper = mountIndex()

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
      .vm.$emit('confirmed', { name: 'Trae', command: 'trae acp' })
    await wrapper.vm.$nextTick()
    expect(wrapper.findComponent({ name: 'CreateAcpAgentDialog' }).props('errorMessage')).toBeTruthy()

    // Close, reopen: the stale banner must not reappear over empty fields.
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' }).vm.$emit('close')
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-action="agent-create"]').trigger('click')

    expect(wrapper.findComponent({ name: 'CreateAcpAgentDialog' }).props('errorMessage')).toBe('')
  })

  it('keeps the dialog open with a specific message for an invalid command', async () => {
    const err = Object.assign(new Error('bad'), { msgKey: 'InvalidAcpCommand' })
    mockCreateAcpAgent.mockRejectedValueOnce(err)
    const wrapper = mountIndex()

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
      .vm.$emit('confirmed', { name: 'Trae', command: 'x' })
    await wrapper.vm.$nextTick()

    const dialog = wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
    expect(dialog.props('open')).toBe(true)
    expect(dialog.props('errorMessage')).toBe('Invalid ACP command')
  })

  it('closes the dialog and toasts on an unrecoverable failure', async () => {
    mockCreateAcpAgent.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountIndex()

    await wrapper.find('[data-action="agent-create"]').trigger('click')
    await wrapper.findComponent({ name: 'CreateAcpAgentDialog' })
      .vm.$emit('confirmed', { name: 'Trae', command: 'trae acp' })
    await wrapper.vm.$nextTick()

    expect(wrapper.findComponent({ name: 'CreateAcpAgentDialog' }).props('open')).toBe(false)
    expect(mockToastShow).toHaveBeenCalledWith('Failed to add agent', expect.any(Object))
  })

  // --- test all agents ---

  it('runs the self-test for every loaded agent and lists the outcomes worst first', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'Good', backend: 'claude', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Broken', backend: 'custom-acp', specialty: '', sortOrder: 1 },
      { id: 'agent-3', name: 'Shaky', backend: 'cursor', specialty: '', sortOrder: 2 },
    ]
    mockSelfTestRuns.mockImplementation((id: string) => {
      if (id === 'agent-1') {
        return { result: { agentId: id, ok: true, passed: 9, failed: 0, warned: 0, checks: [{ key: 'enabled', status: 'ok' }] } }
      }
      if (id === 'agent-2') {
        return { result: { agentId: id, ok: false, passed: 5, failed: 2, warned: 0,
          checks: [{ key: 'acp_command', status: 'fail', detail: 'Executable "nope" was not found' }] } }
      }
      return { result: { agentId: id, ok: true, passed: 8, failed: 0, warned: 1,
        checks: [{ key: 'models', status: 'warn', detail: 'No models reported yet' }] } }
    })

    const wrapper = mountIndex()
    await wrapper.find('[data-action="agent-selftest-all"]').trigger('click')
    // Promise.all + reactive render need a couple of ticks.
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    expect(mockSelfTestRuns).toHaveBeenCalledTimes(3)

    const rows = wrapper.findAll('.settings-agents-index__selftest-row')
    expect(rows).toHaveLength(3)

    // Worst first: the failing agent must lead, then the warning, then the pass.
    expect(rows[0].classes()).toContain('settings-agents-index__selftest-row--fail')
    expect(rows[0].text()).toContain('Broken')
    expect(rows[1].classes()).toContain('settings-agents-index__selftest-row--warn')
    expect(rows[1].text()).toContain('Shaky')
    expect(rows[2].classes()).toContain('settings-agents-index__selftest-row--ok')
    expect(rows[2].text()).toContain('Good')

    // The first problem's detail is what the user needs to act on.
    expect(rows[0].text()).toContain('Executable "nope" was not found')
    expect(rows[1].text()).toContain('No models reported yet')

    // Summary counts must match the rows.
    expect(wrapper.text()).toContain('1 OK, 1 warning(s), 1 failed')
  })

  it('reports a request error for the specific agent', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'Gone', backend: 'claude', specialty: '', sortOrder: 0 },
    ]
    mockSelfTestRuns.mockImplementation(() => ({ error: 'Agent not found' }))

    const wrapper = mountIndex()
    await wrapper.find('[data-action="agent-selftest-all"]').trigger('click')
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()
    await wrapper.vm.$nextTick()

    const rows = wrapper.findAll('.settings-agents-index__selftest-row')
    expect(rows).toHaveLength(1)
    expect(rows[0].classes()).toContain('settings-agents-index__selftest-row--fail')
    expect(rows[0].text()).toContain('Agent not found')
  })

  // --- drag-to-reorder ---

  it('renders a drag handle for each agent row', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.findAll('.settings-agents-index__drag-handle')).toHaveLength(1)
  })

  it('onDragEnd persists the local order via reorderAgents', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 1 },
    ]
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    // Simulate the drag: Sortable mutates the bound array in place.
    vm.$.setupState.localAgents = [
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 0 },
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 1 },
    ]
    await vm.$.setupState.onDragEnd()
    expect(mockReorderAgents).toHaveBeenCalledWith(['agent-2', 'agent-1'])
  })

  it('onDragEnd rolls back and toasts on failure', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 1 },
    ]
    const wrapper = mountIndex()
    const vm = wrapper.vm as any
    mockReorderAgents.mockRejectedValueOnce(new Error('fail'))
    vm.$.setupState.localAgents = [
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 0 },
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 1 },
    ]
    await vm.$.setupState.onDragEnd()
    await wrapper.vm.$nextTick()

    expect(mockToastShow).toHaveBeenCalledWith('Reorder failed', expect.any(Object))
    // Rolled back to the server order.
    const ids = (vm.$.setupState.localAgents as any[]).map(a => a.id)
    expect(ids).toEqual(['agent-1', 'agent-2'])
  })
})
