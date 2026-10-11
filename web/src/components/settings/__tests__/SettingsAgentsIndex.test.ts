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
vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: mockAgents,
    defaultAgentId: mockDefaultAgentId,
    loadAgents: (...args: unknown[]) => mockLoadAgents(...args),
    duplicateAgent: (...args: unknown[]) => mockDuplicateAgent(...args),
    rescanAgents: (...args: unknown[]) => mockRescanAgents(...args),
    setDefaultAgent: (...args: unknown[]) => mockSetDefaultAgent(...args),
    reorderAgents: (...args: unknown[]) => mockReorderAgents(...args),
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
    mockReorderAgents.mockReset().mockResolvedValue(undefined)
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
    expect(wrapper.find('.settings-agents-index__rescan-row').exists()).toBe(true)
  })

  it('clicking rescan row calls rescanAgents', async () => {
    mockRescanAgents.mockResolvedValue(undefined)
    const wrapper = mountIndex()
    const rescanRow = wrapper.find('.settings-agents-index__rescan-row')
    await rescanRow.trigger('click')
    expect(mockRescanAgents).toHaveBeenCalled()
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
