import { describe, expect, it, vi, beforeEach } from 'vitest'
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
    template: '<div class="bottom-sheet-stub" :data-open="open"><slot name="header" /><slot /></div>',
    methods: { close: vi.fn() },
  },
}))

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: {
    name: 'AgentIcon',
    template: '<span class="agent-icon-stub" />',
  },
}))

import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'

function mountDrawer(props = {}) {
  return mount(AgentSelectorDrawer, {
    props: {
      open: true,
      modelValue: '',
      title: 'Select Agent',
      defaultBadge: 'Default',
      setDefaultTitle: 'Set as default',
      ...props,
    },
  })
}

describe('AgentSelectorDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
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
      mockLoadAgents.mockClear()
      const wrapper = mountDrawer({ open: false })

      await wrapper.setProps({ open: true })
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
  })
})
