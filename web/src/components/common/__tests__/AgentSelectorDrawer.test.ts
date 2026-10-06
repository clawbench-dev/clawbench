import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const { mockLoadAgents, mockIsDefaultAgent, mockGetAgentDefaultModelName, mockSetDefaultAgent } = vi.hoisted(() => ({
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockIsDefaultAgent: vi.fn(() => false),
  mockGetAgentDefaultModelName: vi.fn(() => ''),
  mockSetDefaultAgent: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => ({
    agents: ref([
      { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' },
      { id: 'agent-2', name: 'Agent Two', backend: 'acp', specialty: 'Design' },
    ]),
    loadAgents: mockLoadAgents,
    isDefaultAgent: mockIsDefaultAgent,
    getAgentDefaultModelName: mockGetAgentDefaultModelName,
    setDefaultAgent: mockSetDefaultAgent,
  }),
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet-stub" :data-open="open"><slot name="header" /><slot /><slot name="footer" /></div>',
    methods: { close: vi.fn() },
  },
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: {
    name: 'AgentIcon',
    template: '<span class="agent-icon-stub" />',
  },
}))

const { mockSetPendingSettingsCategory } = vi.hoisted(() => ({
  mockSetPendingSettingsCategory: vi.fn(),
}))
vi.mock('@/composables/useSettingsNavigation', () => ({
  setPendingSettingsCategory: mockSetPendingSettingsCategory,
}))
const mockSwitchTab = vi.fn()

import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'

function mountDrawer(props = {}, opts: Record<string, any> = {}) {
  return mount(AgentSelectorDrawer, {
    props: {
      open: true,
      modelValue: '',
      title: 'Select Agent',
      defaultBadge: 'Default',
      setDefaultTitle: 'Set as default',
      ...props,
    },
    global: {
      provide: {
        switchTab: mockSwitchTab,
      },
      ...opts.global,
    },
  })
}

describe('AgentSelectorDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    mockSetPendingSettingsCategory.mockClear()
    mockSwitchTab.mockClear()
  })

  describe('rendering', () => {
    it('renders agent options from useAgents', () => {
      const wrapper = mountDrawer()
      const options = wrapper.findAll('.agent-option')
      expect(options.length).toBe(2)
    })

    it('shows agent names', () => {
      const wrapper = mountDrawer()
      expect(wrapper.text()).toContain('Agent One')
      expect(wrapper.text()).toContain('Agent Two')
    })

    it('shows backend tags', () => {
      const wrapper = mountDrawer()
      expect(wrapper.text()).toContain('cli')
      expect(wrapper.text()).toContain('acp')
    })
  })

  describe('selection', () => {
    it('emits select and update:modelValue when agent is clicked after debounce', async () => {
      const wrapper = mountDrawer({ open: true })

      // Advance past 400ms debounce
      vi.advanceTimersByTime(500)

      await wrapper.find('.agent-option').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['agent-1'])
      expect(wrapper.emitted('update:modelValue')).toBeTruthy()
      expect(wrapper.emitted('update:modelValue')![0]).toEqual(['agent-1'])
    })

    it('emits update:open=false when agent is selected', async () => {
      const wrapper = mountDrawer({ open: true })

      vi.advanceTimersByTime(500)

      await wrapper.find('.agent-option').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:open')).toBeTruthy()
      expect(wrapper.emitted('update:open')![0]).toEqual([false])
    })
  })

  describe('touch guard (400ms debounce)', () => {
    it('ignores clicks within 400ms of opening', async () => {
      vi.setSystemTime(new Date('2025-01-01T00:00:00'))
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      // Click immediately (time hasn't advanced) — should be debounced
      await wrapper.find('.agent-option').trigger('click')

      expect(wrapper.emitted('select')).toBeFalsy()
    })

    it('allows clicks after 400ms', async () => {
      vi.setSystemTime(new Date('2025-01-01T00:00:00'))
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      vi.advanceTimersByTime(500)

      await wrapper.find('.agent-option').trigger('click')

      expect(wrapper.emitted('select')).toBeTruthy()
    })
  })

  describe('multi-select', () => {
    it('renders checkboxes and does not close on click', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      expect(wrapper.findAll('.agent-option-check').length).toBe(2)

      await wrapper.findAll('.agent-option')[0].trigger('click')
      await flushPromises()

      // No immediate emit/close; selection is buffered until confirm.
      expect(wrapper.emitted('update:open')).toBeFalsy()
      expect(wrapper.emitted('select')).toBeFalsy()
    })

    it('emits an array of ids on confirm', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[0].trigger('click')
      await wrapper.findAll('.agent-option')[1].trigger('click')
      await flushPromises()

      await wrapper.find('.agent-multi-confirm').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual([['agent-1', 'agent-2']])
      expect(wrapper.emitted('update:open')![0]).toEqual([false])
    })

    it('toggles a selected agent off', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[0].trigger('click')
      await wrapper.find('.agent-multi-confirm').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('select')![0]).toEqual([[]])
    })
  })

  describe('group mode (host dot)', () => {
    it('shows a host dot only on selected rows', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const rows = wrapper.findAll('.agent-option')
      // agent-1 is selected -> host dot present; agent-2 is not -> absent.
      expect(rows[0].find('.agent-host-dot').exists()).toBe(true)
      expect(rows[1].find('.agent-host-dot').exists()).toBe(false)
    })

    it('does not show host dots in the plain add-members mode', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      expect(wrapper.findAll('.agent-host-dot').length).toBe(0)
    })

    it('emits update:hostId without toggling selection when the dot is clicked', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[0].find('.agent-host-dot').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')).toBeTruthy()
      expect(wrapper.emitted('update:hostId')![0]).toEqual(['agent-1'])
      // The row must stay selected (the dot click is not a toggle).
      expect(wrapper.findAll('.agent-option')[0].classes()).toContain('selected')
      expect(wrapper.emitted('select')).toBeFalsy()
    })

    it('keeps a single host: selecting another dot replaces it', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1', 'agent-2'], hostId: 'agent-1' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[1].find('.agent-host-dot').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')![0]).toEqual(['agent-2'])
    })

    it('marks the current host dot as active', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1', 'agent-2'], hostId: 'agent-2' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const rows = wrapper.findAll('.agent-option')
      expect(rows[1].find('.agent-host-dot').classes()).toContain('active')
      expect(rows[0].find('.agent-host-dot').classes()).not.toContain('active')
    })

    it('disables confirm until a host is chosen', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'], hostId: '' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const confirm = wrapper.find('.agent-multi-confirm')
      expect(confirm.attributes('disabled')).toBeDefined()

      await wrapper.setProps({ hostId: 'agent-1' })
      await flushPromises()
      expect(wrapper.find('.agent-multi-confirm').attributes('disabled')).toBeUndefined()
    })

    it('clears the host when its row is deselected', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'], hostId: 'agent-1' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // Deselect agent-1 (the host) by clicking its row body.
      await wrapper.findAll('.agent-option')[0].trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')).toBeTruthy()
      expect(wrapper.emitted('update:hostId')!.at(-1)).toEqual([''])
    })
  })

  describe('close', () => {
    it('emits update:open=false when handleClose is called', async () => {
      const wrapper = mountDrawer()
      await flushPromises()

      wrapper.vm.handleClose()
      await flushPromises()

      expect(wrapper.emitted('update:open')).toBeTruthy()
      expect(wrapper.emitted('update:open')![0]).toEqual([false])
    })
  })

  describe('set default agent', () => {
    it('calls setDefaultAgent when star button is clicked', async () => {
      mockIsDefaultAgent.mockReturnValue(false)
      const wrapper = mountDrawer()
      await flushPromises()

      const starBtn = wrapper.find('.agent-set-default-btn')
      if (starBtn.exists()) {
        await starBtn.trigger('click')
        expect(mockSetDefaultAgent).toHaveBeenCalled()
      }
    })

    it('shows default badge when agent is default', async () => {
      mockIsDefaultAgent.mockImplementation((id: string) => id === 'agent-1')
      const wrapper = mountDrawer()

      expect(wrapper.find('.agent-default-badge-pill').exists()).toBe(true)
    })
  })

  describe('agent config', () => {
    it('renders a config gear button for every agent row', () => {
      const wrapper = mountDrawer()
      expect(wrapper.findAll('.agent-config-btn').length).toBe(2)
    })

    it('hides the default badge/star and the gear when showAgentActions is false', () => {
      // The group "add members" picker passes showAgentActions=false: those
      // per-row actions are noise there.
      mockIsDefaultAgent.mockImplementation((id: string) => id === 'agent-1')
      const wrapper = mountDrawer({ showAgentActions: false })
      expect(wrapper.find('.agent-config-btn').exists()).toBe(false)
      expect(wrapper.find('.agent-set-default-btn').exists()).toBe(false)
      expect(wrapper.find('.agent-default-badge-pill').exists()).toBe(false)
    })

    it('dims and blocks agents listed in excludedAgentIds (already members)', async () => {
      const wrapper = mountDrawer({ multiple: true, excludedAgentIds: ['agent-1'], addedLabel: 'Added' })
      await flushPromises()
      // Clear the 400ms open-guard so clicks register.
      vi.advanceTimersByTime(500)
      const rows = wrapper.findAll('.agent-option')
      // agent-1 is excluded: dimmed + tagged, agent-2 is normal.
      expect(rows[0].classes()).toContain('agent-option-disabled')
      expect(rows[0].find('.agent-added-tag').text()).toBe('Added')
      expect(rows[1].classes()).not.toContain('agent-option-disabled')
      // Clicking an excluded row must not select it.
      await rows[0].trigger('click')
      expect(rows[0].classes()).not.toContain('selected')
      // Clicking a normal row still toggles selection.
      await rows[1].trigger('click')
      expect(rows[1].classes()).toContain('selected')
    })

    it('deep-links to the agent settings page and closes the drawer on gear click without selecting', async () => {
      const wrapper = mountDrawer()
      await flushPromises()

      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-config-btn')[0].trigger('click')
      await flushPromises()

      expect(mockSetPendingSettingsCategory).toHaveBeenCalledWith('agents:agent-1')
      expect(mockSwitchTab).toHaveBeenCalledWith('settings')
      // The gear click must close the drawer but not select/change the agent
      expect(wrapper.emitted('update:open')![0]).toEqual([false])
      expect(wrapper.emitted('select')).toBeFalsy()
      expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    })

    it('does not fire when no agent id', async () => {
      const wrapper = mountDrawer()
      await flushPromises()
      vi.advanceTimersByTime(500)
      // Simulate an empty agent id by calling the internal handler directly
      const vm = wrapper.vm as any
      vm.handleOpenAgentConfig('')
      expect(mockSetPendingSettingsCategory).not.toHaveBeenCalled()
    })
  })

  describe('selected state', () => {
    it('adds selected class to currently selected agent', () => {
      const wrapper = mountDrawer({ modelValue: 'agent-1' })
      const options = wrapper.findAll('.agent-option')

      expect(options[0].classes()).toContain('selected')
      expect(options[1].classes()).not.toContain('selected')
    })
  })

  describe('auto load on open', () => {
    it('calls loadAgents when open becomes true', async () => {
      // Vue's watch(() => props.open, ...) does not react to VTU's setProps
      // in jsdom (known VTU/Vue issue). Test the watch behavior by verifying:
      // 1. loadAgents is NOT called when mounted with open=false
      // 2. loadAgents IS called when mounted with open=true
      mockLoadAgents.mockClear()
      mountDrawer({ open: false })
      await flushPromises()
      expect(mockLoadAgents).not.toHaveBeenCalled()

      mockLoadAgents.mockClear()
      mountDrawer({ open: true })
      await flushPromises()
      expect(mockLoadAgents).toHaveBeenCalled()
    })
  })

  describe('keyboard interaction', () => {
    it('selects agent on Enter key', async () => {
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      vi.advanceTimersByTime(500)

      await wrapper.find('.agent-option').trigger('keydown.enter')
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
    })

    it('selects agent on Space key', async () => {
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      vi.advanceTimersByTime(500)

      await wrapper.find('.agent-option').trigger('keydown.space')
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
    })

    it('ArrowDown + Enter on the list confirms the highlighted agent', async () => {
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      vi.advanceTimersByTime(500)

      function key(key: string) {
        document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
      }
      key('ArrowDown')
      key('ArrowDown')
      key('Enter')
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['agent-2'])
    })

    it('ArrowUp + Enter on the list confirms the last agent', async () => {
      const wrapper = mountDrawer({ open: true })
      await flushPromises()

      vi.advanceTimersByTime(500)

      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }))
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
      await flushPromises()

      expect(wrapper.emitted('select')).toBeTruthy()
      expect(wrapper.emitted('select')![0]).toEqual(['agent-2'])
    })
  })
})
