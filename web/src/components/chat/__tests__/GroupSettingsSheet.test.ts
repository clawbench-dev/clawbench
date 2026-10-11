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
const mockRemove = vi.fn().mockResolvedValue(undefined)
const mockSetParallel = vi.fn().mockResolvedValue(undefined)
const mockGenerateAvatars = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useGroupChat', () => ({
  addGroupMembers: (...a: any[]) => mockAdd(...a),
  removeGroupMember: (...a: any[]) => mockRemove(...a),
  setGroupParallelDefault: (...a: any[]) => mockSetParallel(...a),
  generateMemberAvatars: (...a: any[]) => mockGenerateAvatars(...a),
}))

// The group sheet's auto-approve switch delegates to the shared session toggle
// (it PATCHes the current session and the backend fans the flag out to every
// member row). Mock it so the test can assert the delegation.
const mockToggleAutoApprove = vi.fn()
vi.mock('@/composables/useSessionIdentity', () => ({
  toggleAutoApprove: (...a: any[]) => mockToggleAutoApprove(...a),
}))

vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: (agentId: string) => (agentId ? `<svg>agent-${agentId}</svg>` : ''),
}))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/AgentSelectorDrawer.vue', () => ({
  default: { name: 'AgentSelectorDrawer', props: ['open'], template: '<div class="picker-stub" />' },
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
  { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, left: true },
  { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
]

describe('GroupSettingsSheet', () => {
  beforeEach(() => {
    mockRemove.mockClear()
    mockAdd.mockClear()
    mockGenerateAvatars.mockClear()
    mockToggleAutoApprove.mockClear()
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

  it('tags the host and a left member, and only offers remove on active non-hosts', () => {
    const w = mountSheet(MEMBERS)
    expect(w.find('.gm-tag--host').exists()).toBe(true)
    expect(w.find('.gm-tag--left').exists()).toBe(true)
    // The host tag carries a crown icon alongside the label.
    expect(w.find('.gm-tag--host .gm-tag-crown').exists()).toBe(true)
    expect(w.find('.gm-tag--host .lucide-crown').exists()).toBe(true)
    // Host has no remove; left member has no remove; only Alice does.
    expect(w.findAll('.gm-remove')).toHaveLength(1)
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

  it('removes a member through the API and emits changed', async () => {
    const w = mountSheet(MEMBERS)
    await w.find('.gm-remove').trigger('click')
    await flushPromises()
    expect(mockRemove).toHaveBeenCalledWith('g1', 'm2')
    expect(w.emitted('changed')).toBeTruthy()
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
})
