import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed, nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

vi.mock('vue-i18n', () => ({
  // Return the key, but interpolate named params so assertions on counts
  // (e.g. "已添加 {count}/{max} 个") can check real values. Param names are
  // preserved in the output via `key(param=value)`.
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (!params) return key
      const parts = Object.entries(params).map(([k, v]) => `${k}=${v}`).join(',')
      return `${key}(${parts})`
    },
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const { mockLoadAgents, mockIsDefaultAgent, mockGetAgentDefaultModelName, mockSetDefaultAgent, mockAgents } = vi.hoisted(() => ({
  mockLoadAgents: vi.fn().mockResolvedValue(undefined),
  mockIsDefaultAgent: vi.fn(() => false),
  mockGetAgentDefaultModelName: vi.fn(() => ''),
  mockSetDefaultAgent: vi.fn().mockResolvedValue(undefined),
  mockAgents: [
    { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' },
    { id: 'agent-2', name: 'Agent Two', backend: 'acp', specialty: 'Design' },
  ],
}))

vi.mock('@/composables/useAgents', () => ({
  useAgents: () => {
    const agents = ref(mockAgents)
    // Mirror the real composable: the picker renders selectableAgents, which
    // filters out disabled agents. Derived from the same ref so tests can flip
    // a `disabled` flag and observe the row disappear.
    const selectableAgents = computed(() => agents.value.filter((a: { disabled?: boolean }) => !a.disabled))
    return {
      agents,
      selectableAgents,
      loadAgents: mockLoadAgents,
      isDefaultAgent: mockIsDefaultAgent,
      getAgentDefaultModelName: mockGetAgentDefaultModelName,
      setDefaultAgent: mockSetDefaultAgent,
    }
  },
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

// PopupMenu uses <Teleport> + <Transition>, which break reactivity in
// test-utils Vue 3.5. Stub it so GroupModeHint's chip still renders while the
// popover content is skipped (show is false until hover/tap).
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: {
    name: 'PopupMenu',
    props: ['show', 'targetElement', 'maxWidth', 'maxHeight', 'menuItemsCount', 'anchor'],
    template: '<div v-if="show" class="popup-menu-stub"><slot /></div>',
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

  describe('group mode (host button)', () => {
    it('shows a host button only on selected rows', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const rows = wrapper.findAll('.agent-option')
      // agent-1 is selected -> host button present; agent-2 is not -> absent.
      expect(rows[0].find('.agent-host-btn').exists()).toBe(true)
      expect(rows[1].find('.agent-host-btn').exists()).toBe(false)
    })

    it('does not show host buttons in the plain add-members mode', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      expect(wrapper.findAll('.agent-host-btn').length).toBe(0)
    })

    it('does NOT auto-assign a host when the first member is selected (host is optional)', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[0].trigger('click')
      await flushPromises()

      // Selecting a member must not silently make it the host: leaving the host
      // empty is a valid choice (it means a free group).
      expect(wrapper.emitted('update:hostId')).toBeFalsy()
    })

    it('does not touch the host when more members are selected', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: [], hostId: 'agent-1' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[1].trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')).toBeFalsy()
    })

    it('emits update:hostId without toggling selection when the button is clicked', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[0].find('.agent-host-btn').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')).toBeTruthy()
      expect(wrapper.emitted('update:hostId')![0]).toEqual(['agent-1'])
      // The row must stay selected (the host button click is not a toggle).
      expect(wrapper.findAll('.agent-option')[0].classes()).toContain('selected')
      expect(wrapper.emitted('select')).toBeFalsy()
    })

    it('keeps a single host: selecting another button replaces it', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1', 'agent-2'], hostId: 'agent-1' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.findAll('.agent-option')[1].find('.agent-host-btn').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')![0]).toEqual(['agent-2'])
    })

    it('marks the current host button as active', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1', 'agent-2'], hostId: 'agent-2' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const rows = wrapper.findAll('.agent-option')
      expect(rows[1].find('.agent-host-btn').classes()).toContain('active')
      expect(rows[0].find('.agent-host-btn').classes()).not.toContain('active')
    })

    it('labels a non-host row with the action, and the host row with the badge', async () => {
      const wrapper = mountDrawer({
        multiple: true, groupMode: true,
        modelValue: ['agent-1', 'agent-2'], hostId: 'agent-1',
        hostLabel: 'Set as host', hostBadge: 'Host',
      })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const rows = wrapper.findAll('.agent-option')
      // The host row shows the resulting state badge…
      expect(rows[0].find('.agent-host-btn-label').text()).toBe('Host')
      // …while a merely-selected row shows the ACTION, not the state.
      expect(rows[1].find('.agent-host-btn-label').text()).toBe('Set as host')
    })

    it('uses the group glyph in the header only in group mode', async () => {
      const group = mountDrawer({ multiple: true, groupMode: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)
      // Group creation reads as a group: the Users mark replaces the Bot mark.
      expect(group.find('.bs-header-icon.lucide-users').exists()).toBe(true)
      expect(group.find('.bs-header-icon.lucide-bot').exists()).toBe(false)

      const single = mountDrawer({ multiple: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)
      expect(single.find('.bs-header-icon.lucide-bot').exists()).toBe(true)
      expect(single.find('.bs-header-icon.lucide-users').exists()).toBe(false)
    })

    it('renders the confirm as a plain pill, not a full-width bar', () => {
      const src = readFileSync(
        join(__dirname, '..', 'AgentSelectorDrawer.vue'),
        'utf8',
      )
      // The shared pill language supplies the shape; a scoped geometry
      // declaration (width:100%) would silently re-diverge from .fbtn.
      const confirmRule = src.match(/\.agent-multi-confirm\s*\{[^}]*\}/)
      expect(confirmRule).toBeNull()
      expect(src).toMatch(/class="fbtn fbtn-primary agent-multi-confirm"/)
    })

    it('hides disabled agents (renders selectableAgents, not the raw list)', async () => {
      mockAgents.push({ id: 'agent-3', name: 'Agent Three', backend: 'cli', specialty: '', disabled: true } as any)
      try {
        const wrapper = mountDrawer()
        await flushPromises()
        vi.advanceTimersByTime(500)
        const names = wrapper.findAll('.agent-option-name').map(n => n.text())
        expect(names).toEqual(['Agent One', 'Agent Two'])
        expect(names).not.toContain('Agent Three')
      } finally {
        mockAgents.pop()
      }
    })

    it('source guard: the list is rendered from selectableAgents', () => {
      // The component test above proves behavior with the mock; this pins the
      // actual render source so a future edit cannot silently swap back to the
      // unfiltered `agents` list (which would show disabled agents again).
      const src = readFileSync(
        join(__dirname, '..', 'AgentSelectorDrawer.vue'),
        'utf8',
      )
      expect(src).toMatch(/v-for="\(agent, idx\) in selectableAgents"/)
      expect(src).not.toMatch(/v-for="\(agent, idx\) in agents"/)
    })

    it('enables confirm once a valid group is formed (host mode: host chosen)', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'], hostId: '' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // One member, no host -> free mode needs 2 members, so still disabled.
      expect(wrapper.find('.agent-multi-confirm').attributes('disabled')).toBeDefined()

      await wrapper.setProps({ hostId: 'agent-1' })
      await flushPromises()
      expect(wrapper.find('.agent-multi-confirm').attributes('disabled')).toBeUndefined()
    })

    it('enables confirm for a free group with two members and no host', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'], hostId: '' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // One member and no host: not yet a valid free group.
      expect(wrapper.find('.agent-multi-confirm').attributes('disabled')).toBeDefined()

      // Select a second member by clicking its row (the live selection buffer is
      // what confirm reads).
      await wrapper.findAll('.agent-option')[1].trigger('click')
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

    it('clears the host when the active host button is clicked again (toggle off)', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'], hostId: 'agent-1' })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // The row stays selected; clicking the ACTIVE host button clears the host.
      await wrapper.findAll('.agent-option')[0].find('.agent-host-btn').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('update:hostId')!.at(-1)).toEqual([''])
      expect(wrapper.findAll('.agent-option')[0].classes()).toContain('selected')
      expect(wrapper.emitted('select')).toBeFalsy()
    })

    it('shows a host/free mode hint in the footer that flips with the host', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // No host yet -> free mode. The i18n mock returns the key verbatim.
      const free = wrapper.find('.agent-mode-hint')
      expect(free.exists()).toBe(true)
      expect(free.text()).toContain('group.freeMode')
      // Free mode carries the @ glyph (the relay is @-driven).
      expect(free.find('.lucide-at-sign').exists()).toBe(true)
      expect(free.find('.lucide-crown').exists()).toBe(false)

      await wrapper.setProps({ hostId: 'agent-1' })
      await flushPromises()
      const host = wrapper.find('.agent-mode-hint')
      expect(host.text()).toContain('group.hostMode')
      // Host mode carries the crown; the glyph pairing mirrors the session chip.
      expect(host.find('.lucide-crown').exists()).toBe(true)
      expect(host.find('.lucide-at-sign').exists()).toBe(false)
    })

    it('does not render the mode hint outside group mode', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: ['agent-1'] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      expect(wrapper.find('.agent-mode-hint').exists()).toBe(false)
    })
  })

  // Single vs group creation share one page; the segmented switch in the header
  // flips between them. Only the create page opts in (showModeSwitch).
  describe('mode switch (single | group)', () => {
    it('does not render the switch unless showModeSwitch is set', async () => {
      const wrapper = mountDrawer({ multiple: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      expect(wrapper.find('.agent-mode-switch').exists()).toBe(false)
    })

    it('renders the switch when showModeSwitch is set and at least one agent is selectable', async () => {
      const wrapper = mountDrawer({ showModeSwitch: true })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const sw = wrapper.find('.agent-mode-switch')
      expect(sw.exists()).toBe(true)
      expect(sw.findAll('.agent-mode-tab').length).toBe(2)
      // Single mode -> the single tab is active.
      expect(sw.find('[data-mode="single"]').classes()).toContain('active')
      expect(sw.find('[data-mode="group"]').classes()).not.toContain('active')
    })

    it('marks the group tab active in group mode', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: true, showModeSwitch: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      const sw = wrapper.find('.agent-mode-switch')
      expect(sw.find('[data-mode="group"]').classes()).toContain('active')
      expect(sw.find('[data-mode="single"]').classes()).not.toContain('active')
    })

    it('emits switch-mode with the target when a tab is clicked, without closing', async () => {
      const wrapper = mountDrawer({ showModeSwitch: true })
      await flushPromises()
      vi.advanceTimersByTime(500)

      await wrapper.find('[data-mode="group"]').trigger('click')
      await flushPromises()

      expect(wrapper.emitted('switch-mode')).toBeTruthy()
      expect(wrapper.emitted('switch-mode')![0]).toEqual(['group'])
      // The mode switch sits in the header; interacting with it must not close
      // the drawer (header click is no longer a close target either).
      expect(wrapper.emitted('update:open')).toBeFalsy()
    })

    it('still renders the switch with a SINGLE selectable agent (repeat instances make a group)', async () => {
      // The group needs two MEMBERS, not two distinct agents: one installed
      // agent can be added twice via the row's "+" stepper. Gating the switch
      // on agent count would make group creation unreachable for a
      // single-agent install, so the gate must be "any agent at all".
      const original = [...mockAgents]
      mockAgents.splice(0, mockAgents.length, { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' } as any)
      try {
        const wrapper = mountDrawer({ showModeSwitch: true })
        await flushPromises()
        vi.advanceTimersByTime(500)

        const sw = wrapper.find('.agent-mode-switch')
        expect(sw.exists()).toBe(true)
        expect(sw.findAll('.agent-mode-tab').length).toBe(2)
      } finally {
        mockAgents.splice(0, mockAgents.length, ...original)
      }
    })

    it('hides the switch when no agent is selectable (all disabled)', async () => {
      // Nothing to build a group from — the segmented control would be dead.
      const original = [...mockAgents]
      mockAgents.splice(0, mockAgents.length, { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: '', disabled: true } as any)
      try {
        const wrapper = mountDrawer({ showModeSwitch: true })
        await flushPromises()
        vi.advanceTimersByTime(500)
        expect(wrapper.find('.agent-mode-switch').exists()).toBe(false)
        expect(wrapper.find('.agent-list-empty').exists()).toBe(true)
      } finally {
        mockAgents.splice(0, mockAgents.length, ...original)
      }
    })

    it('hides the switch when the agent list is empty (no install at all)', async () => {
      // The OTHER way to have zero selectable agents: nothing installed. This
      // is a distinct input from "all disabled" — a gate written as
      // `selectableAgents.length >= 1` passes one and fails the other only if
      // it reads the same list, so both must be pinned.
      const original = [...mockAgents]
      mockAgents.splice(0, mockAgents.length)
      try {
        const wrapper = mountDrawer({ showModeSwitch: true })
        await flushPromises()
        vi.advanceTimersByTime(500)
        expect(wrapper.find('.agent-mode-switch').exists()).toBe(false)
        expect(wrapper.find('.agent-list-empty').exists()).toBe(true)
      } finally {
        mockAgents.splice(0, mockAgents.length, ...original)
      }
    })

    it('builds a HOST group from two instances of a single installed agent', async () => {
      // Host mode has its own validity rule (the host must be among the
      // members) and does NOT need two members. With one installed agent the
      // only way to get a valid host group is still the stepper, so the host
      // path must not be blocked by the single-agent install either.
      const original = [...mockAgents]
      mockAgents.splice(0, mockAgents.length, { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' } as any)
      try {
        const wrapper = mountDrawer({
          multiple: true,
          groupMode: true,
          allowInstances: true,
          maxInstances: 12,
          showModeSwitch: true,
          hostId: 'agent-1',
          modelValue: [],
        })
        await flushPromises()
        vi.advanceTimersByTime(500)

        const row = wrapper.find('.agent-option')
        await row.trigger('click')
        await flushPromises()
        // Host is among the members already ⇒ confirm enabled with ONE instance.
        expect((wrapper.find('.agent-multi-confirm').element as HTMLButtonElement).disabled).toBe(false)

        // A second instance is allowed too (and does not disturb the host).
        await row.find('.agent-stepper .agent-stepper-btn:last-child').trigger('click')
        await flushPromises()
        await wrapper.find('.agent-multi-confirm').trigger('click')
        await flushPromises()
        expect(wrapper.emitted('select')![0]).toEqual([['agent-1', 'agent-1']])
      } finally {
        mockAgents.splice(0, mockAgents.length, ...original)
      }
    })

    it('builds a two-instance group from a single installed agent', async () => {
      // End-to-end of the scenario the gate change unblocks: one installed
      // agent, flip to the group tab, "+" once, confirm — the payload repeats
      // the same agent id twice (two independent instances).
      const original = [...mockAgents]
      mockAgents.splice(0, mockAgents.length, { id: 'agent-1', name: 'Agent One', backend: 'cli', specialty: 'Coding' } as any)
      try {
        const wrapper = mountDrawer({
          multiple: false,
          groupMode: false,
          allowInstances: false,
          maxInstances: 12,
          showModeSwitch: true,
          modelValue: '',
        })
        await flushPromises()
        vi.advanceTimersByTime(500)

        // The group tab must be reachable at all with a single agent.
        await wrapper.find('[data-mode="group"]').trigger('click')
        await flushPromises()
        expect(wrapper.emitted('switch-mode')![0]).toEqual(['group'])

        // The parent owns groupMode; mirror what SessionDrawer does on flip.
        await wrapper.setProps({ multiple: true, groupMode: true, allowInstances: true })
        await flushPromises()
        vi.advanceTimersByTime(500)

        const row = wrapper.find('.agent-option')
        await row.trigger('click')
        await flushPromises()
        // One instance is not enough for a free group.
        expect((wrapper.find('.agent-multi-confirm').element as HTMLButtonElement).disabled).toBe(true)

        await row.find('.agent-stepper .agent-stepper-btn:last-child').trigger('click')
        await flushPromises()
        expect(row.find('.agent-stepper-count').text()).toBe('2')
        expect((wrapper.find('.agent-multi-confirm').element as HTMLButtonElement).disabled).toBe(false)

        await wrapper.find('.agent-multi-confirm').trigger('click')
        await flushPromises()
        expect(wrapper.emitted('select')![0]).toEqual([['agent-1', 'agent-1']])
      } finally {
        mockAgents.splice(0, mockAgents.length, ...original)
      }
    })

    it('clears the member buffer when the mode flips', async () => {
      const wrapper = mountDrawer({ multiple: true, groupMode: false, showModeSwitch: true, modelValue: [] })
      await flushPromises()
      vi.advanceTimersByTime(500)

      // Buffer a member in the current mode.
      await wrapper.findAll('.agent-option')[0].trigger('click')
      await flushPromises()
      expect(wrapper.findAll('.agent-option')[0].classes()).toContain('selected')

      // Flipping modes must drop the stale buffer.
      await wrapper.setProps({ groupMode: true })
      await flushPromises()
      expect(wrapper.findAll('.agent-option')[0].classes()).not.toContain('selected')
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

// Instance mode (allowInstances): the same agent can be added several times.
// A selected row grows a − count + stepper; confirming repeats the agent id
// once per instance.
describe('AgentSelectorDrawer instance mode', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('shows a stepper only on a selected row when allowInstances is on', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    // Nothing selected yet: no stepper anywhere.
    expect(wrapper.findAll('.agent-stepper').length).toBe(0)

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    // The selected row shows exactly one stepper with count 1.
    expect(wrapper.findAll('.agent-stepper').length).toBe(1)
    expect(wrapper.find('.agent-stepper-count').text()).toBe('1')
  })

  it('increments the count with the + button and confirms repeated ids', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    // + twice → 3 instances.
    const plus = wrapper.find('.agent-stepper .agent-stepper-btn:last-child')
    await plus.trigger('click')
    await plus.trigger('click')
    await flushPromises()
    expect(wrapper.find('.agent-stepper-count').text()).toBe('3')

    await wrapper.find('.agent-multi-confirm').trigger('click')
    await flushPromises()
    expect(wrapper.emitted('select')![0]).toEqual([['agent-1', 'agent-1', 'agent-1']])
  })

  it('disables − at one instance and deselects via the row click', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    // At 1 the − is disabled (the stepper never goes below one instance).
    expect((wrapper.find('.agent-stepper .agent-stepper-btn:first-child').element as HTMLButtonElement).disabled).toBe(true)

    const plus = wrapper.find('.agent-stepper .agent-stepper-btn:last-child')
    await plus.trigger('click')
    await flushPromises()
    // At 2 the − is enabled and steps back to 1.
    await wrapper.find('.agent-stepper .agent-stepper-btn:first-child').trigger('click')
    await flushPromises()
    expect(wrapper.find('.agent-stepper-count').text()).toBe('1')

    // Deselecting entirely is a row click (clears all instances).
    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.agent-stepper').length).toBe(0)
  })

  it('clicking an already-selected row clears all its instances', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    const plus = wrapper.find('.agent-stepper .agent-stepper-btn:last-child')
    await plus.trigger('click')
    await flushPromises()
    expect(wrapper.find('.agent-stepper-count').text()).toBe('2')

    // Clicking the row clears the whole selection for that agent.
    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.agent-stepper').length).toBe(0)
  })

  it('free-mode confirm stays disabled with a single instance', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, groupMode: true, hostId: '', modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    // One instance is not enough for a free group.
    expect((wrapper.find('.agent-multi-confirm').element as HTMLButtonElement).disabled).toBe(true)

    // Two instances of the SAME agent satisfy the ≥2 rule.
    await wrapper.find('.agent-stepper .agent-stepper-btn:last-child').trigger('click')
    await flushPromises()
    expect((wrapper.find('.agent-multi-confirm').element as HTMLButtonElement).disabled).toBe(false)
  })
})

// Instance cap: the group member limit counts every instance across ALL agents.
// At the cap the "+" buttons disable and the footer warns.
describe('AgentSelectorDrawer instance limit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
  })
  afterEach(() => vi.useRealTimers())

  it('disables + at the total-instance limit (across all agents)', async () => {
    // Cap 2: select agent-1 once, agent-2 once → both rows at the cap.
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, maxInstances: 2, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    const rows = wrapper.findAll('.agent-option')
    await rows[0].trigger('click')
    await flushPromises()
    // 1/2: still room.
    expect((rows[0].find('.agent-stepper .agent-stepper-btn:last-child').element as HTMLButtonElement).disabled).toBe(false)

    await rows[1].trigger('click')
    await flushPromises()
    // 2/2: BOTH rows' "+" are now disabled — the cap is group-wide.
    expect((rows[0].find('.agent-stepper .agent-stepper-btn:last-child').element as HTMLButtonElement).disabled).toBe(true)
    expect((rows[1].find('.agent-stepper .agent-stepper-btn:last-child').element as HTMLButtonElement).disabled).toBe(true)

    // Clicking "+" at the cap must not add anything.
    await rows[0].find('.agent-stepper .agent-stepper-btn:last-child').trigger('click')
    await flushPromises()
    expect(rows[0].find('.agent-stepper-count').text()).toBe('1')
  })

  it('does not select a fresh row at the cap', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, maxInstances: 1, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    const rows = wrapper.findAll('.agent-option')
    await rows[0].trigger('click')
    await flushPromises()
    // At 1/1, selecting a different agent must be refused.
    await rows[1].trigger('click')
    await flushPromises()
    expect(rows[1].classes()).not.toContain('selected')
    expect(wrapper.findAll('.agent-stepper').length).toBe(1)
  })

  it('shows the running total and a warning at the limit', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, maxInstances: 3, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    // Empty: 0/3.
    expect(wrapper.find('.agent-count-hint').text()).toContain('count=0')
    expect(wrapper.find('.agent-count-hint').text()).toContain('max=3')
    expect(wrapper.find('.agent-count-hint').classes()).not.toContain('at-limit')

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    expect(wrapper.find('.agent-count-hint').text()).toContain('count=1')

    const plus = wrapper.find('.agent-stepper .agent-stepper-btn:last-child')
    await plus.trigger('click')
    await plus.trigger('click')
    await flushPromises()
    // 3/3: warning state.
    expect(wrapper.find('.agent-count-hint').classes()).toContain('at-limit')
  })

  it('counts instances already in the group (usedInstances) toward the cap', async () => {
    // Add-members flow: 11 already in the group, cap 12 → only 1 slot left.
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, maxInstances: 12, usedInstances: 11, modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    // Footer total includes the 11 existing.
    expect(wrapper.find('.agent-count-hint').text()).toContain('count=11')
    expect(wrapper.find('.agent-count-hint').text()).toContain('max=12')

    const row = wrapper.find('.agent-option')
    await row.trigger('click')
    await flushPromises()
    // 12/12 → "+" disabled, cannot add a second instance.
    expect((row.find('.agent-stepper .agent-stepper-btn:last-child').element as HTMLButtonElement).disabled).toBe(true)
    await row.find('.agent-stepper .agent-stepper-btn:last-child').trigger('click')
    await flushPromises()
    expect(row.find('.agent-stepper-count').text()).toBe('1')
  })

  it('lays the stepper above the host action in instance mode', async () => {
    const wrapper = mountDrawer({ multiple: true, allowInstances: true, groupMode: true, hostId: '', modelValue: [] })
    vi.advanceTimersByTime(500)
    await flushPromises()

    await wrapper.find('.agent-option').trigger('click')
    await flushPromises()
    const actions = wrapper.find('.agent-row-actions')
    expect(actions.exists()).toBe(true)
    // Stepper first (top, name/description line), host action second (bottom,
    // model-tag line).
    const children = actions.element.children
    expect(children[0].classList.contains('agent-stepper')).toBe(true)
    expect(children[1].classList.contains('agent-host-btn')).toBe(true)
  })
})
