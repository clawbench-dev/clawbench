import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
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
          agentCreateAcp: 'Add Agent',
          agentCreateAcpSuccess: 'Agent added',
          agentCreateAcpFailed: 'Failed to add agent',
          agentInstallable: 'Available to install',
          agentInstalled: 'Installed',
          agentInstallNone: 'All known agents are already installed',
          agentInstallHint: 'Install commands run on the server',
          agentCreateAcpCommandRequired: 'The ACP command cannot be empty',
          agentCreateAcpInvalidCommand: 'Invalid ACP command',
          agentCreateAcpInvalidName: 'Invalid agent name',
          agentCopyNameTaken: 'An agent with this name already exists',
          agentNameReserved: 'This name conflicts with the group-chat user nickname',
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
  ChevronRight: { name: 'ChevronRight', template: '<span class="icon-chevron" />' },
  Copy: { name: 'Copy', template: '<span class="icon-copy" />' },
  GripVertical: { name: 'GripVertical', template: '<span class="icon-grip" />' },
  Plus: { name: 'Plus', template: '<span class="icon-plus" />' },
  RefreshCw: { name: 'RefreshCw', template: '<span class="icon-refresh" />' },
}))

// Mock CreateAcpAgentDialog — the flow under test is the index page's
// open/create/navigate handling, not the dialog's own form.
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

// Mock AgentInstallDialog — the flow under test is the index page's
// "available → installed" wiring, not the dialog's own command/progress UI.
vi.mock('@/components/AgentInstallDialog.vue', () => ({
  default: {
    name: 'AgentInstallDialog',
    props: ['backendId', 'backendName', 'installCmd'],
    emits: ['close', 'installed'],
    template: '<div class="mock-agent-install-dialog" :data-backend="backendId" />',
  },
}))

/** The catalogue the component fetches on mount. */
const CATALOG = [
  { id: 'claude', name: 'Claude', specialty: 'Coding', default_cmd: 'claude', install_cmd: 'npm install -g claude' },
  { id: 'codebuddy', name: 'CodeBuddy', specialty: 'Coding', default_cmd: 'codebuddy', install_cmd: 'npm install -g codebuddy' },
  { id: 'builtin', name: 'ClawBench Agent', specialty: 'Built-in', default_cmd: '' },
]

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
    mockToastShow.mockReset()
    mockAgents.value = []
    mockDefaultAgentId.value = 'agent-1'
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ backends: CATALOG }),
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
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
    expect(mockToastShow).toHaveBeenCalledWith('Agent added', expect.any(Object))
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

  // --- drag-to-reorder ---

  it('renders a drag handle for each agent row', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    expect(wrapper.findAll('.settings-agents-index__drag-handle')).toHaveLength(1)
  })

  // Style alignment with the other settings pages: rows are transparent rows
  // INSIDE the shared SettingsCard, not a full-bleed slab of --bg-secondary.
  // Actions and installed agents live in their own cards so the installable
  // section can sit above a list that can be dozens of rows long.
  it('renders actions and agent rows inside shared settings cards', () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    const cards = wrapper.findAll('.settings-card__body')
    expect(cards.length).toBeGreaterThan(0)
    expect(cards[0].find('[data-action="agent-create"]').exists()).toBe(true)
    expect(cards[0].find('[data-action="agent-rescan"]').exists()).toBe(true)
    expect(wrapper.find('.settings-card__body .settings-agents-index__row').exists()).toBe(true)
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

describe('SettingsAgentsIndex — installable backends', () => {
  beforeEach(() => {
    mockAgents.value = []
    mockLoadAgents.mockReset()
    mockToastShow.mockReset()
    mockDefaultAgentId.value = 'agent-1'
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ backends: CATALOG }),
    })))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists catalogue backends that have no agent yet', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
    ]
    const wrapper = mountIndex()
    await flushPromises()

    expect(wrapper.find('[data-backend="claude"]').exists()).toBe(true)
    // Already installed — must not be offered again.
    expect(wrapper.find('[data-backend="codebuddy"]').exists()).toBe(false)
    // The built-in backend has no install command.
    expect(wrapper.find('[data-backend="builtin"]').exists()).toBe(false)
    expect(wrapper.find('.settings-agents-index__install-hint').text()).toBe('Install commands run on the server')
    expect(wrapper.text()).toContain('Available to install · 1')

    wrapper.unmount()
  })

  it('shows an empty state instead of disappearing when nothing is installable', async () => {
    mockAgents.value = [
      { id: 'agent-1', name: 'CodeBuddy', backend: 'codebuddy', specialty: '', sortOrder: 0 },
      { id: 'agent-2', name: 'Claude', backend: 'claude', specialty: '', sortOrder: 1 },
    ]
    const wrapper = mountIndex()
    await flushPromises()

    expect(wrapper.find('.settings-agents-index__install-row').exists()).toBe(false)
    expect(wrapper.find('.settings-agents-index__install-hint').exists()).toBe(false)
    expect(wrapper.find('.settings-agents-index__install-empty').text()).toBe('All known agents are already installed')
    expect(wrapper.text()).toContain('Available to install · 0')

    wrapper.unmount()
  })

  it('opens the install dialog for the chosen backend', async () => {
    const wrapper = mountIndex()
    await flushPromises()

    await wrapper.find('[data-backend="claude"] .fbtn').trigger('click')

    const dialog = wrapper.findComponent({ name: 'AgentInstallDialog' })
    expect(dialog.exists()).toBe(true)
    expect(dialog.props('backendId')).toBe('claude')

    wrapper.unmount()
  })

  it('reloads the agent list after a successful install', async () => {
    const wrapper = mountIndex()
    await flushPromises()

    await wrapper.find('[data-backend="claude"] .fbtn').trigger('click')
    mockLoadAgents.mockClear()

    await wrapper.findComponent({ name: 'AgentInstallDialog' }).vm.$emit('installed', { ok: true })
    await flushPromises()

    expect(mockLoadAgents).toHaveBeenCalledWith(true)

    wrapper.unmount()
  })
})
