import { describe, expect, it, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import TeamPanel from '@/components/chat/TeamPanel.vue'
import { updateTeamState, clearTeamState, type TeamState } from '@/composables/useTeamState'

// BottomSheet Teleports to <body>, where the wrapper's DOM query cannot reach
// it. Render its slots inline so the detail sheet's content is assertable.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: {
      open: Boolean, title: String, auto: Boolean, instant: Boolean, maximized: Boolean,
      noHeader: Boolean, handleOnly: Boolean, transparentOverlay: Boolean, fullscreen: Boolean,
      closeGuard: Boolean, panelClass: [String, Array, Object],
    },
    emits: ['close'],
    template: `
      <div v-if="open" class="bottom-sheet-overlay">
        <div class="bs-header"><slot name="header" /></div>
        <div class="bs-body"><slot /></div>
      </div>
    `,
  }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        team: {
          title: 'Team',
          active: '{count} active',
          ended: 'ended',
          autoTeam: 'auto team',
          tokens: 'tokens',
          contextWindow: 'context',
          agentType: 'type',
          status: {
            pending: 'pending',
            running: 'running',
            completed: 'completed',
            failed: 'failed',
            terminated: 'ended',
          },
          detail: {
            open: 'View details for {name}',
            prompt: 'Full prompt',
            output: 'Result',
            description: 'Role',
            activity: 'Activity',
            lifecycle: 'Lifecycle',
            toolCalls: 'Tool calls',
            taskId: 'Task ID',
            sessionId: 'Session ID',
            loadingPrompt: 'Loading prompt…',
            noPrompt: 'no prompt',
          },
        },
      },
    },
  },
})

// The detail sheet resolves the member's Agent card through an injected lookup
// (ChatPanelContent owns the messages array). Tests supply one per case.
let _lookup: ((name: string) => { toolId: string; msgId: string | number } | null) | null = null
function provide(over: { teamMemberCardLookup?: typeof _lookup }) {
  _lookup = over.teamMemberCardLookup ?? null
}

function mountPanel() {
  return mount(TeamPanel, {
    global: {
      plugins: [i18n],
      provide: { teamMemberCardLookup: (n: string) => _lookup?.(n) ?? null },
    },
  })
}

function snapshot(over: Partial<TeamState> = {}): TeamState {
  return {
    type: 'member_status_change',
    teamName: 'clawbench-probe',
    hasLiveMembers: true,
    members: [
      { name: 'probe-alpha', color: 'blue', agentType: 'general-purpose', status: 'running', activity: 'working', lifecycle: 'alive', toolCallCount: 2, tokenUsage: { inputTokens: 56763, outputTokens: 229 } },
      { name: 'probe-beta', color: 'green', status: 'completed', activity: 'idle', lifecycle: 'terminated', toolCallCount: 1 },
    ],
    ...over,
  }
}

describe('TeamPanel', () => {
  beforeEach(() => {
    clearTeamState()
    _lookup = null
    vi.unstubAllGlobals()
  })

  it('renders nothing when there is no team', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.team-panel').exists()).toBe(false)
  })

  it('shows the collapsed chip with active/total count', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-chip').exists()).toBe(true)
    expect(wrapper.get('.team-chip__count').text()).toBe('1/2')
  })

  // ── Chip leading glyph: a team icon, not a bare status dot ──

  it('leads the collapsed chip with the team glyph', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    const icon = wrapper.get('.team-chip__icon')
    expect(icon.element.tagName.toLowerCase()).toBe('svg')
    // The dot is gone — the glyph carries the same "this is a team" meaning.
    expect(wrapper.find('.team-chip__dot').exists()).toBe(false)
  })

  it('pulses the chip glyph while a member is working', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.team-chip__icon').classes()).toContain('team-chip__icon--running')
  })

  it('keeps the chip glyph static once no member is working', async () => {
    updateTeamState(
      snapshot({
        type: 'team_idle',
        hasLiveMembers: false,
        members: [{ name: 'probe-alpha', status: 'completed', lifecycle: 'terminated' }],
      }),
    )
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.team-chip__icon').classes()).not.toContain('team-chip__icon--running')
  })

  it('expands to a roster with one row per member', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const rows = wrapper.findAll('.team-member')
    expect(rows).toHaveLength(2)
    expect(rows[0].get('.team-member__name').text()).toBe('probe-alpha')
    expect(rows[0].get('.team-member__status-icon').attributes('data-status')).toBe('running')
    expect(rows[1].get('.team-member__status-icon').attributes('data-status')).toBe('completed')
  })

  it('marks a terminated member row', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const rows = wrapper.findAll('.team-member')
    expect(rows[0].classes()).not.toContain('team-member--terminated')
    expect(rows[1].classes()).toContain('team-member--terminated')
  })

  it('renders the status as an icon, not a dot or a word', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const icons = wrapper.findAll('.team-member__status-icon')
    expect(icons).toHaveLength(2)
    expect(icons[0].classes()).toContain('team-member__status-icon--running')
    expect(icons[1].classes()).toContain('team-member__status-icon--completed')
    // Non-running statuses are lucide glyphs.
    expect(icons[1].element.tagName.toLowerCase()).toBe('svg')
    // The bare dot and the inline status word are both gone.
    expect(wrapper.find('.team-member__dot').exists()).toBe(false)
    expect(wrapper.find('.team-member__status').exists()).toBe(false)
    // The word still reaches assistive tech via the accessible name.
    expect(icons[0].attributes('aria-label')).toBe('running')
  })

  it('draws the running state with the app-wide loading ring', async () => {
    // There is exactly one loading indicator in this codebase
    // (css/components.css `.li-spinner`). A rotating lucide icon here would be a
    // second implementation that drifts in shape/speed.
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const running = wrapper.get('.team-member__status-icon--running')
    expect(running.classes()).toContain('li-spinner')
    // The ring owns its shape; this site may only size/tint it.
    expect(running.element.tagName.toLowerCase()).toBe('span')
    expect(running.find('svg').exists()).toBe(false)
  })

  it('gives every status kind a distinct glyph shape', async () => {
    // Colour cannot carry the distinction alone (3/36 themes make accent and
    // orange identical), so each kind must differ in shape. Comparing the SVG
    // bodies is what proves it — class names alone would pass even if all five
    // rendered the same circle.
    const kinds = ['running', 'completed', 'failed', 'killed', 'pending'] as const
    const shapes = new Set<string>()
    for (const status of kinds) {
      // clearTeamState also re-collapses the chip, so each iteration starts from
      // the same collapsed state (the composable is module-level).
      clearTeamState()
      updateTeamState({
        type: 'member_status_change',
        teamName: 't',
        members: [{ name: `m-${status}`, status }],
      })
      const wrapper = mountPanel()
      await wrapper.vm.$nextTick()
      await wrapper.get('.team-chip').trigger('click')
      // Compare the SVG BODY only: the outer HTML carries the per-kind class
      // name, so it would differ even if all five rendered the same glyph.
      const el = wrapper.get('.team-member__status-icon').element
      shapes.add(`${el.tagName}|${el.innerHTML}`)
    }
    expect(shapes.size).toBe(kinds.length)
  })

  it('maps each status kind to the glyph that MEANS that status', async () => {
    // The distinctness test above only proves the five differ from each other —
    // it passes if `completed` and `failed` are swapped, which would show a
    // checkmark for a failure. Pin the semantic mapping itself: lucide renders
    // a stable `lucide-<name>` class on each icon, which is the glyph's
    // identity without depending on its internal path data.
    const expected: Array<[string, string]> = [
      ['completed', 'lucide-circle-check'],
      ['failed', 'lucide-circle-x'],
      ['killed', 'lucide-circle-slash'],
      ['pending', 'lucide-circle-dashed'],
    ]
    for (const [status, iconClass] of expected) {
      clearTeamState()
      updateTeamState({
        type: 'member_status_change',
        teamName: 't',
        members: [{ name: `m-${status}`, status }],
      })
      const wrapper = mountPanel()
      await wrapper.vm.$nextTick()
      await wrapper.get('.team-chip').trigger('click')

      const icon = wrapper.get('.team-member__status-icon')
      expect(icon.classes(), `${status} must render ${iconClass}`).toContain(iconClass)
      // The row must also agree with the icon: a member shown as failed must
      // not be styled as completed.
      expect(icon.attributes('data-status')).toBe(status === 'killed' ? 'terminated' : status)
    }
  })

  it('marks a killed member as terminated', async () => {
    updateTeamState({
      type: 'member_status_change',
      teamName: 't',
      members: [{ name: 'probe-x', status: 'killed', lifecycle: 'terminated' }],
    })
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.get('.team-member__status-icon').attributes('data-status')).toBe('terminated')
  })

  it('formats token totals compactly', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    // (56763 + 229) / 1000 = 57.0k
    expect(wrapper.findAll('.team-member__tokens')[0].text()).toBe('57.0k')
  })

  // ── Tool count: wrench glyph + number, no word ──

  it('renders the tool count as a wrench glyph plus the number', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const cell = wrapper.findAll('.team-member__tools')[0]
    expect(cell.element.tagName.toLowerCase()).toBe('span')
    expect(cell.find('svg').exists()).toBe(true)
    // No unit word: the glyph already says "tool calls".
    expect(cell.text()).toBe('2')
  })

  it('keeps an empty tool cell so the column does not collapse', async () => {
    // The cell must still exist (and occupy its grid track) when a member has
    // no tool calls; dropping the element is what shifted every cell to its
    // left and made the numbers ragged.
    updateTeamState({
      type: 'member_status_change',
      teamName: 't',
      members: [{ name: 'idle-member', status: 'running', toolCallCount: 0 }],
    })
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const cell = wrapper.get('.team-member__tools')
    expect(cell.exists()).toBe(true)
    expect(cell.text()).toBe('')
    expect(cell.find('svg').exists()).toBe(false)
  })

  it('gives every member row the same four column slots', async () => {
    // Column alignment is a structure property: with a fixed grid template the
    // tool count and token total land on the same x in every row, regardless of
    // whether that member has tool calls or tokens. Asserting the child count
    // per row catches a reintroduced v-if on a cell.
    updateTeamState({
      type: 'member_status_change',
      teamName: 't',
      members: [
        { name: 'has-both', status: 'running', toolCallCount: 7, tokenUsage: { inputTokens: 900, outputTokens: 100 } },
        { name: 'has-none', status: 'pending' },
      ],
    })
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const rows = wrapper.findAll('.team-member')
    expect(rows).toHaveLength(2)
    for (const row of rows) {
      expect(row.element.children).toHaveLength(4)
      expect(row.find('.team-member__tools').exists()).toBe(true)
      expect(row.find('.team-member__tokens').exists()).toBe(true)
    }
  })

  it('collapses again when the expanded header is clicked', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.find('.team-expanded').exists()).toBe(true)
    await wrapper.get('.team-expanded__header').trigger('click')
    expect(wrapper.find('.team-chip').exists()).toBe(true)
  })

  it('disappears when the team is deleted', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-panel').exists()).toBe(true)
    updateTeamState({ type: 'team_deleted', teamName: 'clawbench-probe', members: [] })
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.team-panel').exists()).toBe(false)
  })

  // ── Idle / ended state (wire: hasLiveMembers:false, no member alive) ──

  it('shows an ended chip once the team has no live members', async () => {
    updateTeamState(
      snapshot({
        type: 'team_idle',
        hasLiveMembers: false,
        members: [{ name: 'probe-alpha', status: 'completed', activity: 'idle', lifecycle: 'terminated' }],
      }),
    )
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.team-chip__text').text()).toBe('clawbench-probe · ended')
  })

  it('does NOT treat a snapshot that omits hasLiveMembers as ended', async () => {
    // The flag is absent on some snapshots; a missing flag must not fake an
    // ended state (we only trust an explicit false).
    updateTeamState({
      type: 'member_status_change',
      teamName: 'clawbench-probe',
      members: [{ name: 'probe-alpha', status: 'completed', activity: 'idle' }],
    } as TeamState)
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    expect(wrapper.get('.team-chip__text').text()).toBe('clawbench-probe')
  })

  it('shows the completed tally in the header once ended', async () => {
    updateTeamState(
      snapshot({
        type: 'team_idle',
        hasLiveMembers: false,
        members: [
          { name: 'a', status: 'completed', lifecycle: 'terminated' },
          { name: 'b', status: 'completed', lifecycle: 'terminated' },
        ],
      }),
    )
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.get('.team-expanded__count').text()).toBe('2/2')
  })

  // ── Auto team label ──

  it('labels an auto team in the expanded header', async () => {
    updateTeamState(snapshot({ isAutoTeam: true }))
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.get('.team-expanded__auto').text()).toBe('auto team')
  })

  it('omits the auto label for an explicit team', async () => {
    updateTeamState(snapshot({ isAutoTeam: false }))
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.find('.team-expanded__auto').exists()).toBe(false)
  })

  // ── Member metadata: agentType is tooltip-only ──

  it('never renders the agent type inline, only in the tooltip', async () => {
    // Nearly every auto-team member is `general-purpose`, so the inline column
    // carried no information while eating the width the name needs. It must
    // stay out of the row and remain reachable via the tooltip.
    updateTeamState(
      snapshot({
        members: [{ name: 'probe-alpha', status: 'running', agentType: 'general-purpose' }],
      }),
    )
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.find('.team-member__agent-type').exists()).toBe(false)
    expect(wrapper.get('.team-member').text()).not.toContain('general-purpose')
    expect(wrapper.get('.team-member').attributes('title')).toContain('type: general-purpose')
  })

  it('builds the member tooltip from description, type, tokens and context', async () => {
    updateTeamState(
      snapshot({
        members: [
          {
            name: 'probe-alpha',
            status: 'running',
            description: 'runs the probe',
            agentType: 'general-purpose',
            tokenUsage: { inputTokens: 100, outputTokens: 20, lastContextWindow: 200000 },
          },
        ],
      }),
    )
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    const title = wrapper.get('.team-member').attributes('title')
    expect(title).toContain('runs the probe')
    expect(title).toContain('type: general-purpose')
    expect(title).toContain('tokens: 100 in / 20 out')
    expect(title).toContain('context: 200000')
  })

  it('omits the tooltip entirely when a member has no metadata', async () => {
    updateTeamState({
      type: 'member_status_change',
      teamName: 't',
      members: [{ name: 'bare', status: 'running' }],
    })
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('.team-chip').trigger('click')
    expect(wrapper.get('.team-member').attributes('title')).toBe('')
  })
})

// ── Member detail sheet ──
//
// Clicking a roster row opens a sheet with the member's facts and its FULL
// prompt. The prompt is not in the roster payload (teamUpdate carries only
// `description`), and the chat block for the Agent card is slim (empty input),
// so the sheet must fetch it from the tool-call endpoint using the Agent card's
// tool id. These tests pin the lookup, the fetch, and the Markdown rendering.
describe('TeamPanel — member detail', () => {
  it('opens the detail sheet when a member row is clicked', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('.team-member-detail__name').exists()).toBe(false)
    await wrapper.get('[data-action="team-member-detail"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('.team-member-detail__name').text()).toBe('probe-alpha')
  })

  it('renders the member prompt as Markdown, not escaped text', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        input: JSON.stringify({ name: 'probe-alpha', prompt: '# Heading\n\n- one\n- two\n\n`src/main.go`' }),
      }),
    })
    vi.stubGlobal('fetch', fetchMock)
    provide({ teamMemberCardLookup: () => ({ toolId: 'call_a', msgId: 7 }) })

    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-action="team-member-detail"]').trigger('click')
    await flushPromises()

    const prompt = wrapper.get('.team-member-detail__prompt')
    // Markdown structure survived — a <pre> dump would contain the literal '#'.
    expect(prompt.find('h1').exists()).toBe(true)
    expect(prompt.findAll('li').length).toBe(2)
    expect(prompt.text()).not.toContain('# Heading')
    // The path was annotated (chip class is applied by the annotator).
    expect(prompt.html()).toContain('src/main.go')
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/ai/chat/tool-call?tool_id=call_a&message_id=7',
    )
  })

  it('shows a notice instead of fetching when no Agent card is found', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    provide({ teamMemberCardLookup: () => null })

    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-action="team-member-detail"]').trigger('click')
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('no prompt')
  })

  it('lists the wire facts the roster cannot fit', async () => {
    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-action="team-member-detail"]').trigger('click')
    await flushPromises()

    const facts = wrapper.get('.team-member-detail__facts').text()
    // Wire fields the row cannot fit are all present.
    expect(facts).toContain('general-purpose')   // agentType
    expect(facts).toContain('working')           // activity
    expect(facts).toContain('alive')             // lifecycle
    expect(facts).toContain('2')                 // toolCallCount
    // Colour is a visual attribute (the header dot), not a fact row.
    expect(facts).not.toContain('blue')
  })
})

// The sheet teleports to <body>, so ChatMessageList's delegated click handler
// never sees its path chips. The drawer must route them itself, through the
// same resolver, or a path in a member's prompt is dead text.
describe('TeamMemberDetail — path chips', () => {
  it('opens the file when a path chip inside the prompt is clicked', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ input: JSON.stringify({ prompt: 'see `src/main.go`' }) }),
    })
    vi.stubGlobal('fetch', fetchMock)
    provide({ teamMemberCardLookup: () => ({ toolId: 'call_a', msgId: 9 }) })

    updateTeamState(snapshot())
    const wrapper = mountPanel()
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-action="team-member-detail"]').trigger('click')
    await flushPromises()

    // The prompt rendered a path chip plus its open button. Clicking the
    // BUTTON is the chat's own contract for FILE paths (directory paths open by
    // clicking the chip text) — the drawer mirrors it exactly.
    const btn = wrapper.get('.team-member-detail__prompt .chat-file-open-btn')
    fetchMock.mockClear()
    await btn.trigger('click')
    await flushPromises()

    // openFilePath's first observable step for an in-project path is the
    // directory probe. Without the drawer's own handler the click is inert and
    // this fetch never happens (ChatMessageList's delegated handler cannot see
    // a teleported sheet).
    const probed = fetchMock.mock.calls.some((c) => String(c[0]).includes('/api/dir?path=src%2Fmain.go'))
    expect(probed).toBe(true)
  })
})
