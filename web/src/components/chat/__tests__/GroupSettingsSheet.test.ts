import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import GroupSettingsSheet from '../GroupSettingsSheet.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
  // The component now imports the shared store, which transitively imports the
  // i18n singleton; that module calls createI18n at load time.
  createI18n: () => ({ global: { t: (key: string) => key, locale: { value: 'en' } } }),
}))

const mockAdd = vi.fn().mockResolvedValue(undefined)
const mockSetDisabled = vi.fn().mockResolvedValue(undefined)
const mockSetHost = vi.fn().mockResolvedValue(undefined)
const mockSetParallel = vi.fn().mockResolvedValue(undefined)
const mockSetCruise = vi.fn().mockResolvedValue(undefined)
const mockGenerateAvatars = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useGroupChat', () => ({
  addGroupMembers: (...a: any[]) => mockAdd(...a),
  setGroupMemberDisabled: (...a: any[]) => mockSetDisabled(...a),
  setGroupHost: (...a: any[]) => mockSetHost(...a),
  setGroupParallelDefault: (...a: any[]) => mockSetParallel(...a),
  setGroupCruise: (...a: any[]) => mockSetCruise(...a),
  generateMemberAvatars: (...a: any[]) => mockGenerateAvatars(...a),
}))

// The group sheet's auto-approve switch delegates to the shared session toggle
// (it PATCHes the current session and the backend fans the flag out to every
// member row). Mock it so the test can assert the delegation.
const mockToggleAutoApprove = vi.fn()
const mockApplyCruiseDefault = vi.fn()
vi.mock('@/composables/useSessionIdentity', () => ({
  toggleAutoApprove: (...a: any[]) => mockToggleAutoApprove(...a),
  applyCruiseAutoApproveDefault: (...a: any[]) => mockApplyCruiseDefault(...a),
}))

// The roster's model line resolves a member's model id against the agent's
// list (name lookup + the agent's own default). Both live on the shared
// useAgents store; stub them here so a row's label is deterministic.
const AGENT_MODELS: Record<string, Array<{ id: string; name: string; default: boolean }>> = {
  a1: [
    { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', default: true },
    { id: 'claude-opus-4-5', name: 'Claude Opus 4.5', default: false },
  ],
}
const AGENT_DEFAULT_MODEL: Record<string, string> = { a1: 'claude-sonnet-4-6' }
vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: (agentId: string) => (agentId ? `<svg>agent-${agentId}</svg>` : ''),
  useAgents: () => ({
    getAgentModel: (agentId: string, modelId: string) => AGENT_MODELS[agentId]?.find(m => m.id === modelId),
    getDefaultModelId: (agentId: string) => AGENT_DEFAULT_MODEL[agentId] || '',
  }),
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/AgentSelectorDrawer.vue', () => ({
  default: { name: 'AgentSelectorDrawer', props: ['open'], template: '<div class="picker-stub" />' },
}))
// Clicking a member opens the shared SessionDrawer (the SAME component single
// chat uses) addressed at that member's session row. It has its own heavy
// dependencies (session identity, api, the compact endpoint) covered by its own
// test file; here only the props the roster passes need to be assertable.
vi.mock('../SessionDrawer.vue', () => ({
  default: {
    name: 'SessionDrawer',
    props: ['open', 'agentId', 'initialTab', 'targetSessionId'],
    emits: ['close', 'changed'],
    template: '<div class="mcs-stub" :data-open="String(!!open)" :data-target="targetSessionId" :data-tab="initialTab" />',
  },
}))
// BottomSheet teleports to <body> and gates on everOpened; stub it with a
// pass-through so the sheet's own content is queryable in place. The header
// slot must be rendered too — the add-members action lives there.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bs-stub"><div class="bs-header-stub"><slot name="header" /></div><slot /></div>',
  },
}))

function mountSheet(members: any[], props: Record<string, unknown> = {}) {
  return mount(GroupSettingsSheet, {
    props: { groupId: 'g1', members, autoApprove: false, ...props },
  })
}

// The host is deliberately NOT first here: the server orders members by
// created_at, so a host that joined later (or was re-added) lands mid-list. The
// sheet must pin it to the top regardless of the incoming order.
const MEMBERS = [
  { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false },
  { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, disabled: true },
  { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
]

describe('GroupSettingsSheet', () => {
  beforeEach(() => {
    mockSetDisabled.mockClear()
    mockAdd.mockClear()
    mockGenerateAvatars.mockClear()
    mockToggleAutoApprove.mockClear()
    mockSetCruise.mockClear()
  })

  // The panel is the group's settings surface (members + concurrency +
  // auto-approve), so its header reads "群聊设置" — not "成员", which described
  // only the roster and undersold the settings rows below it.
  it('titles the header with the group-settings key, not the members key', () => {
    const w = mountSheet(MEMBERS)
    const title = w.find('.bs-header-title')
    expect(title.exists()).toBe(true)
    expect(title.text()).toBe('group.settings')
  })

  it('renders one row per member with an avatar and name, host pinned first', () => {
    const w = mountSheet(MEMBERS)
    expect(w.findAll('.gm-row')).toHaveLength(3)
    expect(w.findAll('.gm-avatar .agent-icon-stub')).toHaveLength(3)
    // Host leads even though it arrived last in the roster.
    expect(w.findAll('.gm-name').map(n => n.text())).toEqual(['Host', 'Alice', 'Gone'])
  })

  it('keeps the non-host order stable when the host is already first', () => {
    const w = mountSheet([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'Bob', backend: 'claude', agentId: 'a3', isHost: false },
    ])
    expect(w.findAll('.gm-name').map(n => n.text())).toEqual(['Host', 'Alice', 'Bob'])
  })

  it('tags the host and a disabled member, and offers a disable toggle on every row', () => {
    const w = mountSheet(MEMBERS)
    expect(w.find('.gm-tag--host').exists()).toBe(true)
    expect(w.find('.gm-tag--disabled').exists()).toBe(true)
    // The host tag carries a crown icon alongside the label, the disabled tag a
    // BAN glyph — the same "disabled" icon the agent settings use, so the state
    // reads the same way in both places (and is legible without the tooltip).
    expect(w.find('.gm-tag--host .gm-tag-crown').exists()).toBe(true)
    expect(w.find('.gm-tag--host .lucide-crown').exists()).toBe(true)
    const tagIcon = w.find('.gm-tag--disabled .gm-tag-disabled-icon')
    expect(tagIcon.exists()).toBe(true)
    expect(tagIcon.classes()).toContain('lucide-ban')
    expect(tagIcon.classes()).not.toContain('lucide-mic-off')
    // EVERY row keeps the trailing control (the host's is disabled), so the
    // usage chip holds the same x-position across rows.
    expect(w.findAll('.gm-disable-toggle')).toHaveLength(3)
  })

  it('shows a Ban glyph on an enabled member and a Play glyph on a disabled one', () => {
    // The toggle's ICON carries the action: Ban = "disable this member",
    // Play = "turn it back on". Same pair the agent settings use, so "disabled"
    // is the same visual vocabulary everywhere.
    const w = mountSheet(MEMBERS)
    const enabledRow = w.findAll('.gm-row').find(r => !r.classes().includes('is-disabled'))!
    const disabledRow = w.findAll('.gm-row').find(r => r.classes().includes('is-disabled'))!
    const enabledIcon = enabledRow.find('.gm-disable-toggle svg')
    const disabledIcon = disabledRow.find('.gm-disable-toggle svg')
    expect(enabledIcon.classes()).toContain('lucide-ban')
    expect(disabledIcon.classes()).toContain('lucide-play')
  })

  it('puts the host pill with the name, not in a trailing column', () => {
    // The pill describes WHO the member is, so it rides inside the identity
    // group right after the name / real name. Pinned to the far right it read
    // like a row action and broke the alignment across rows.
    const w = mountSheet(MEMBERS)
    const hostRow = w.findAll('.gm-row')[0]
    expect(hostRow.find('.gm-identity .gm-tag--host').exists()).toBe(true)
    // And the identity group comes BEFORE the usage chip / disable control.
    const identity = hostRow.find('.gm-identity').element
    const usage = hostRow.find('.gm-usage').element
    const disable = hostRow.find('.gm-disable-toggle').element
    expect(identity.compareDocumentPosition(usage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(usage.compareDocumentPosition(disable) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('keeps the host disable button disabled and inert', async () => {
    const w = mountSheet(MEMBERS)
    const hostRow = w.findAll('.gm-row')[0]
    const btn = hostRow.find('.gm-disable-toggle')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.classes()).toContain('gm-disable-toggle--inert')
    expect(btn.attributes('title')).toBe('group.hostCannotDisable')
    await btn.trigger('click')
    expect(mockSetDisabled).not.toHaveBeenCalled()
  })

  // ── 更换主持人 (issue #608) ──

  // Every host-mode row keeps the crown slot (the host's and a disabled member's
  // are inert) so the trailing column does not shift — the same rule the disable
  // toggle follows.
  it('offers make-host on enabled non-host rows only, keeping the column aligned', () => {
    const w = mountSheet(MEMBERS)
    const rows = w.findAll('.gm-row')
    expect(rows).toHaveLength(3)

    const hostRow = rows.find(r => r.text().includes('Host'))!
    const aliceRow = rows.find(r => r.text().includes('Alice'))!
    const goneRow = rows.find(r => r.text().includes('Gone'))!

    const hostBtn = hostRow.find('.gm-host-toggle')
    expect(hostBtn.attributes('disabled')).toBeDefined()
    expect(hostBtn.classes()).toContain('gm-host-toggle--inert')
    expect(hostBtn.attributes('title')).toBe('group.alreadyHost')
    expect(hostBtn.attributes('data-action')).toBeUndefined()

    const goneBtn = goneRow.find('.gm-host-toggle')
    expect(goneBtn.attributes('disabled')).toBeDefined()
    expect(goneBtn.attributes('title')).toBe('group.hostDisabledCannotSet')

    const aliceBtn = aliceRow.find('.gm-host-toggle')
    expect(aliceBtn.attributes('disabled')).toBeUndefined()
    expect(aliceBtn.attributes('title')).toBe('group.makeHost')
    expect(aliceBtn.attributes('data-action')).toBe('make-host')
    // Same glyph as the host pill, so "crown = host" needs no label.
    expect(aliceBtn.find('svg').classes()).toContain('lucide-crown')
  })

  it('promotes the member and refreshes the roster from the server', async () => {
    const w = mountSheet(MEMBERS)
    const aliceRow = w.findAll('.gm-row').find(r => r.text().includes('Alice'))!
    await aliceRow.find('.gm-host-toggle').trigger('click')
    expect(mockSetHost).toHaveBeenCalledWith('g1', 'm2')
    // The roster is server-authoritative: the isHost flags must be re-fetched
    // rather than patched locally (a failed PATCH then leaves the old host).
    expect(w.emitted('changed')).toBeTruthy()
  })

  // A free group has no host, so the action is not offered at all — the server
  // would answer 409.
  it('hides the make-host action in free mode', () => {
    const w = mountSheet(MEMBERS, { mode: 'free' })
    expect(w.findAll('.gm-host-toggle')).toHaveLength(0)
  })

  // The last member still able to speak cannot be turned off: the group would
  // have nobody to answer and no turn could ever run again. Mirrors the server
  // guard (ErrCannotDisableLastMember) so the click never comes back as a 409.
  //
  // A free group (no host) is used because in host mode the host itself speaks,
  // so "host + 1 member" still has a speaker left — that shape legitimately
  // degrades to a single chat and is NOT blocked.
  it('keeps the last speaking member’s disable button inert', async () => {
    const w = mountSheet([
      { id: 'm2', name: 'Solo', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, disabled: true },
    ])
    const soloRow = w.findAll('.gm-row').find(r => r.text().includes('Solo'))!
    const btn = soloRow.find('.gm-disable-toggle')
    expect(btn.attributes('disabled')).toBeDefined()
    expect(btn.classes()).toContain('gm-disable-toggle--inert')
    expect(btn.attributes('title')).toBe('group.lastSpeakerCannotDisable')
    await btn.trigger('click')
    expect(mockSetDisabled).not.toHaveBeenCalled()
  })

  it('allows disabling a member while another speaker remains', async () => {
    const w = mountSheet([
      { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'Bob', backend: 'claude', agentId: 'a3', isHost: false },
    ])
    const aliceRow = w.findAll('.gm-row').find(r => r.text().includes('Alice'))!
    const btn = aliceRow.find('.gm-disable-toggle')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.attributes('title')).toBe('group.disable')
    await btn.trigger('click')
    expect(mockSetDisabled).toHaveBeenCalledWith('g1', 'm2', true)
  })

  // Enabling is never blocked by the last-speaker rule: a disabled member is the
  // one that makes the group non-empty again, so the guard must not leak onto it.
  it('still allows re-enabling a disabled member when nobody else speaks', async () => {
    const w = mountSheet([
      { id: 'm2', name: 'Solo', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, disabled: true },
    ])
    const goneRow = w.findAll('.gm-row').find(r => r.text().includes('Gone'))!
    const btn = goneRow.find('.gm-disable-toggle')
    expect(btn.attributes('disabled')).toBeUndefined()
    expect(btn.attributes('title')).toBe('group.enable')
  })

  // In host mode the host itself speaks, so "host + one member" still has a
  // speaker left — that group legitimately degrades to a single chat, so the
  // member's toggle stays usable.
  it('still allows disabling the only non-host member when a host speaks', async () => {
    const w = mountSheet([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'Solo', backend: 'claude', agentId: 'a2', isHost: false },
    ])
    const soloRow = w.findAll('.gm-row').find(r => r.text().includes('Solo'))!
    expect(soloRow.find('.gm-disable-toggle').attributes('disabled')).toBeUndefined()
  })

  it('renders each member own avatar, falling back to the agent avatar', () => {
    const w = mountSheet([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true, avatar: '<svg>member-1</svg>' },
      { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false },
    ])
    const avatars = w.findAllComponents({ name: 'AgentIcon' }).map(c => c.props('avatar'))
    expect(avatars[0]).toBe('<svg>member-1</svg>')
    expect(avatars[1]).toBe('<svg>agent-a2</svg>')
  })

  // A pooled member (nickname ≠ real agent name) discloses its real agent name
  // next to the nickname; a member whose nickname IS the agent name shows none.
  it('shows the real agent name label only when the nickname differs', () => {
    const w = mountSheet([
      { id: 'm1', name: 'Alice', backend: 'claude', agentId: 'a1', isHost: false, agentName: 'Claude' },
      { id: 'm2', name: 'Claude', backend: 'claude', agentId: 'a2', isHost: true, agentName: 'Claude' },
    ])
    const labels = w.findAll('.gm-real-name')
    expect(labels).toHaveLength(1)
    expect(labels[0].text()).toBe('Claude')
    // It sits right after the nickname inside the identity wrapper.
    expect(w.find('.gm-identity .gm-real-name').exists()).toBe(true)
  })

  it('disables an enabled member through the API and emits changed', async () => {
    const w = mountSheet(MEMBERS)
    // The host also renders a (disabled) disable button, so target the one that is
    // actually enabled rather than relying on position.
    // `attributes('disabled')` is "" (not undefined) on a disabled button, so
    // a truthiness check would pick the HOST's inert button first.
    // Both non-host members have an enabled toggle (Alice enabled, Gone disabled);
    // the action differs, so select by data-action rather than position.
    const disableButtons = w.findAll('.gm-disable-toggle[data-action="disable-member"]')
    expect(disableButtons).toHaveLength(1)
    await disableButtons[0].trigger('click')
    await flushPromises()
    expect(mockSetDisabled).toHaveBeenCalledWith('g1', 'm2', true)
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('re-enables a disabled member (the toggle is the same control)', async () => {
    const w = mountSheet(MEMBERS)
    const disabledRow = w.findAll('.gm-row').find(r => r.classes().includes('is-disabled'))!
    expect(disabledRow.find('.gm-disable-toggle').attributes('data-action')).toBe('enable-member')
    await disabledRow.find('.gm-disable-toggle').trigger('click')
    await flushPromises()
    expect(mockSetDisabled).toHaveBeenCalledWith('g1', 'm3', false)
  })

  // Adding is the sheet's primary action: it lives in the drawer header (right
  // of the title), not as a full-width pill in the body. A regression would
  // put it back in the body where it scrolls away with a long roster.
  it('renders the add-members action in the header, right of the title', () => {
    const w = mountSheet(MEMBERS)
    const header = w.find('.bs-header-stub')
    expect(header.exists()).toBe(true)
    const add = header.find('[data-action="add-members"]')
    expect(add.exists()).toBe(true)
    expect(add.text()).toContain('group.addMembers')
    // It must NOT also linger in the body.
    expect(w.find('.group-settings-sheet').find('[data-action="add-members"]').exists()).toBe(false)
    expect(w.find('.gm-add').exists()).toBe(false)
  })

  // Avatars are generated AFTER the members are added, so the roster must be
  // refreshed a SECOND time once generation resolves — otherwise the new
  // members keep their fallback icons until a session switch reloads them.
  it('refreshes the roster after avatar generation, not just on add', async () => {
    let resolveGen: () => void = () => {}
    mockGenerateAvatars.mockImplementationOnce(() => new Promise<void>(res => { resolveGen = res }))
    mockAdd.mockResolvedValueOnce(['m9'])

    const w = mountSheet(MEMBERS)
    w.findComponent({ name: 'AgentSelectorDrawer' }).vm.$emit('select', ['a4'])
    await flushPromises()

    // Members added → immediate refresh; generation still in flight.
    expect(mockAdd).toHaveBeenCalledWith('g1', ['a4'])
    expect(mockGenerateAvatars).toHaveBeenCalledWith('g1', ['m9'])
    expect(w.emitted('changed')?.length).toBe(1)

    // Generation resolves → second refresh so the avatars replace the fallbacks.
    resolveGen()
    await flushPromises()
    expect(w.emitted('changed')?.length).toBe(2)
  })

  it('opens the member picker from the header button', async () => {
    const w = mountSheet(MEMBERS)
    const picker = () => w.findComponent({ name: 'AgentSelectorDrawer' })
    // The picker is closed until the header action is clicked.
    expect(picker().props('open')).toBe(false)
    await w.find('[data-action="add-members"]').trigger('click')
    expect(picker().props('open')).toBe(true)
  })

  // Group sessions hide the per-agent model/mode chrome (ChatInputBar), which
  // also hides the SessionDrawer — the single-agent home of the auto-approve
  // switch. This sheet is the group's entry point, so the switch must exist
  // here and delegate to the shared toggle (which fans out to every member).
  it('renders an auto-approve switch reflecting the server value', () => {
    const off = mountSheet(MEMBERS)
    const offInput = off.find('.settings-item__switch-input')
    expect(offInput.exists()).toBe(true)
    expect((offInput.element as HTMLInputElement).checked).toBe(false)

    const on = mountSheet(MEMBERS, { autoApprove: true })
    expect((on.find('.settings-item__switch-input').element as HTMLInputElement).checked).toBe(true)
  })

  it('delegates the auto-approve toggle to the shared session toggle', async () => {
    const w = mountSheet(MEMBERS, { autoApprove: false })
    await w.find('.settings-item__switch-input').setValue(true)
    expect(mockToggleAutoApprove).toHaveBeenCalledWith(true)

    mockToggleAutoApprove.mockClear()
    const on = mountSheet(MEMBERS, { autoApprove: true })
    await on.find('.settings-item__switch-input').setValue(false)
    expect(mockToggleAutoApprove).toHaveBeenCalledWith(false)
  })

  it('never renders a per-group maxRounds control (the cap is global now)', () => {
    // The member-speech cap moved to Settings → 聊天 → 群聊
    // (chat.group_max_speeches), so the sheet must not carry a per-group input
    // for it in either mode.
    expect(mountSheet(MEMBERS, { mode: 'host' }).find('#group-max-rounds').exists()).toBe(false)
    expect(mountSheet(MEMBERS, { mode: 'free' }).find('#group-max-rounds').exists()).toBe(false)
    expect(mountSheet(MEMBERS).find('.gm-setting-input').exists()).toBe(false)
  })

  // The auto-cruise switch is available in BOTH modes (cruise only changes WHEN
  // the turn ends, not who routes), and persists via setGroupCruise.
  describe('auto-cruise switch', () => {
    it('renders in both modes and reflects the server value', () => {
      const off = mountSheet(MEMBERS, { mode: 'host', cruise: false })
      const input = off.find('#group-cruise')
      expect(input.exists()).toBe(true)
      expect((input.element as HTMLInputElement).checked).toBe(false)

      const on = mountSheet(MEMBERS, { mode: 'free', cruise: true })
      expect((on.find('#group-cruise').element as HTMLInputElement).checked).toBe(true)
    })

    it('persists the toggle via setGroupCruise', async () => {
      const w = mountSheet(MEMBERS, { mode: 'host', cruise: false })
      await w.find('#group-cruise').setValue(true)
      expect(mockSetCruise).toHaveBeenCalledWith('g1', true)

      mockSetCruise.mockClear()
      const on = mountSheet(MEMBERS, { mode: 'free', cruise: true })
      await on.find('#group-cruise').setValue(false)
      expect(mockSetCruise).toHaveBeenCalledWith('g1', false)
    })

    it('enabling cruise applies the auto-approve hard default', async () => {
      mockApplyCruiseDefault.mockClear()
      const w = mountSheet(MEMBERS, { mode: 'host', cruise: false })
      await w.find('#group-cruise').setValue(true)
      expect(mockApplyCruiseDefault).toHaveBeenCalledWith(true)
    })

    it('disabling cruise does not touch auto-approve', async () => {
      mockApplyCruiseDefault.mockClear()
      const w = mountSheet(MEMBERS, { mode: 'host', cruise: true })
      await w.find('#group-cruise').setValue(false)
      // The helper is still CALLED (with false) but must be a no-op for false;
      // what matters is it is never called with `true` when disabling.
      expect(mockApplyCruiseDefault).not.toHaveBeenCalledWith(true)
    })

    it('shows a mode-specific hint', () => {
      // The i18n stub returns the key, so the mode picks WHICH key is shown.
      expect(mountSheet(MEMBERS, { mode: 'free' }).find('#group-cruise').element.parentElement?.parentElement?.textContent).toContain('group.cruiseHintFree')
      expect(mountSheet(MEMBERS, { mode: 'host' }).find('#group-cruise').element.parentElement?.parentElement?.textContent).toContain('group.cruiseHintHost')
    })
  })

  // The concurrency switch lives HERE, not in the chat
  // action bar. It is shown only in free mode
  // (in host mode the host routes, so the user never @-names a speaker).
  describe('free-mode concurrency switch', () => {
    it('shows only in free mode and reflects the server value', () => {
      const off = mountSheet(MEMBERS, { mode: 'free', parallelDefault: false })
      const offInput = off.find('#group-parallel')
      expect(offInput.exists()).toBe(true)
      expect((offInput.element as HTMLInputElement).checked).toBe(false)

      const on = mountSheet(MEMBERS, { mode: 'free', parallelDefault: true })
      expect((on.find('#group-parallel').element as HTMLInputElement).checked).toBe(true)

      // Host mode: no switch (it would be a dead setting).
      expect(mountSheet(MEMBERS, { mode: 'host' }).find('#group-parallel').exists()).toBe(false)
    })

    it('persists the toggle and emits changed', async () => {
      const w = mountSheet(MEMBERS, { mode: 'free', parallelDefault: false })
      await w.find('#group-parallel').setValue(true)
      await flushPromises()
      expect(mockSetParallel).toHaveBeenCalledWith('g1', true)
      expect(w.emitted('changed')).toBeTruthy()
    })
  })

  describe('member context chip and detail panel', () => {
    // Usage lives on the MEMBER row: each member has its own context window, so
    // the roster shows one chip per member rather than a group-level number.
    const WITH_USAGE = [
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true, usage: { used: 50000, size: 200000 } },
      { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false, usage: null },
      { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, disabled: true, usage: { used: 90000, size: 100000 } },
    ]

    it('renders a bar + percentage for a member that reported usage', () => {
      const w = mountSheet(WITH_USAGE)
      const chip = w.findAll('.gm-usage')[0]
      expect(chip.classes()).not.toContain('gm-usage--empty')
      expect(chip.find('.gm-usage-bar-fill').exists()).toBe(true)
      expect(chip.find('.gm-usage-pct').text()).toBe('25%')
    })

    it('shows an em dash (not 0%) for a member that never reported usage', () => {
      const w = mountSheet(WITH_USAGE)
      const chip = w.findAll('.gm-usage')[1]
      expect(chip.classes()).toContain('gm-usage--empty')
      expect(chip.find('.gm-usage-bar').exists()).toBe(false)
      expect(chip.find('.gm-usage-pct').text()).toBe('—')
    })

    it('keeps the chip for a disabled member (the number is still real)', () => {
      // Muting only takes the member off the schedule; its context window is
      // untouched, and that is exactly what may need compacting while it is off
      // the mic. Hiding the chip would make the row look empty for no reason.
      const w = mountSheet(WITH_USAGE)
      expect(w.findAll('.gm-usage')).toHaveLength(3)
      const disabledRow = w.findAll('.gm-row').find(r => r.classes().includes('is-disabled'))!
      expect(disabledRow.find('.gm-usage-pct').text()).toBe('90%')
    })

    it('opens the member panel when the row is clicked', async () => {
      const w = mountSheet(WITH_USAGE)
      const row = w.findAll('.gm-row-main')[0]
      expect(row.element.tagName).toBe('BUTTON')
      await row.trigger('click')
      const stub = w.find('.mcs-stub')
      expect(stub.attributes('data-open')).toBe('true')
      expect(stub.attributes('data-target')).toBe('m1')
    })

    it('keeps a disabled member row interactive', async () => {
      // A disabled member is still a member: its context window is inspectable and
      // compactable, so the row must stay a button.
      const w = mountSheet(WITH_USAGE)
      const rows = w.findAll('.gm-row-main')
      expect(rows).toHaveLength(3)
      for (const r of rows) expect(r.element.tagName).toBe('BUTTON')
      await rows[2].trigger('click')
      expect(w.find('.mcs-stub').attributes('data-target')).toBe('m3')
    })

    it('keeps the disable control outside the row button (no nested buttons)', () => {
      const w = mountSheet(WITH_USAGE)
      const rowButton = w.findAll('.gm-row-main')[1]
      expect(rowButton.find('.gm-disable-toggle').exists()).toBe(false)
      // Every row has one (the host's is disabled).
      expect(w.findAll('.gm-disable-toggle')).toHaveLength(3)
    })

    it('does not open the panel when the disable control is clicked', async () => {
      const w = mountSheet(WITH_USAGE)
      await w.findAll('.gm-disable-toggle').find(b => b.attributes('disabled') === undefined)!.trigger('click')
      expect(w.find('.mcs-stub').attributes('data-open')).toBe('false')
    })

    it('opens the shared settings drawer on the member row, on the drawer default tab', async () => {
      // The whole point of the change: a member is a session, so clicking one
      // opens the SAME drawer a single chat opens — addressed at the member row
      // (which owns the agent binding and that member's own context window).
      //
      // `initialTab` must NOT be overridden: the drawer's own default (model) is
      // what the single-chat session info dialog opens on, and the roster used
      // to force 'usage', making the member path the one place that landed
      // somewhere else. `undefined` is the assertion — passing 'model'
      // explicitly would keep the two paths coupled to a value that could drift
      // from SessionDrawer's default.
      const w = mountSheet(WITH_USAGE)
      await w.findAll('.gm-row-main')[0].trigger('click')
      const drawer = w.findComponent({ name: 'SessionDrawer' })
      expect(drawer.props('open')).toBe(true)
      expect(drawer.props('targetSessionId')).toBe('m1')
      expect(drawer.props('agentId')).toBe('a1')
      expect(drawer.props('initialTab')).toBeUndefined()
    })

    it('shows each member’s model on a second line of the row', () => {
      // The roster must answer "who runs what" without opening every drawer.
      // A member with an override names that model; one without names the
      // agent's default (a real answer, not "unknown").
      const w = mountSheet([
        { id: 'm1', name: 'Host', backend: 'claude', agentId: 'a1', isHost: true, model: 'claude-opus-4-5' },
        { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a1', isHost: false },
        { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a1', isHost: false, disabled: true },
      ])
      const rows = w.findAll('.gm-row')
      // Overridden: the member's own model.
      expect(rows[0].find('.gm-model').text()).toContain('Claude Opus 4.5')
      expect(rows[0].find('.gm-model').classes()).not.toContain('gm-model--default')
      // Inherited: the agent's default, marked as such.
      expect(rows[1].find('.gm-model').text()).toContain('Claude Sonnet 4.6')
      expect(rows[1].find('.gm-model').classes()).toContain('gm-model--default')
      // A disabled member still reports the model it will use when enabled.
      expect(rows[2].find('.gm-model').exists()).toBe(true)
    })

    it('keeps the model line out of the usage chip’s trailing column', () => {
      // The row has room for ONE trailing column on a phone. The model must
      // live inside the identity column (stacked under the name), or a long id
      // would squeeze the usage chip off the right edge.
      const w = mountSheet(WITH_USAGE)
      const row = w.findAll('.gm-row')[0]
      expect(row.find('.gm-identity .gm-model').exists()).toBe(true)
      const identity = row.find('.gm-identity').element
      const usage = row.find('.gm-usage').element
      // Model line precedes the chip in document order (i.e. is not after it).
      expect(identity.compareDocumentPosition(usage) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    })

    it('refreshes the roster when the member drawer writes', async () => {
      const w = mountSheet(WITH_USAGE)
      await w.findAll('.gm-row-main')[0].trigger('click')
      w.findComponent({ name: 'SessionDrawer' }).vm.$emit('changed')
      expect(w.emitted('changed')).toBeTruthy()
    })
  })
})
