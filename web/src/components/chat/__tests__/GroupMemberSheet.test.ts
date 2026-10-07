import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import GroupMemberSheet from '../GroupMemberSheet.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

const mockAdd = vi.fn().mockResolvedValue(undefined)
const mockRemove = vi.fn().mockResolvedValue(undefined)
const mockUpdateSettings = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useGroupChat', () => ({
  addGroupMembers: (...a: any[]) => mockAdd(...a),
  removeGroupMember: (...a: any[]) => mockRemove(...a),
  updateGroupSettings: (...a: any[]) => mockUpdateSettings(...a),
}))

// The group sheet's auto-approve switch delegates to the shared session toggle
// (it PATCHes the current session and the backend fans the flag out to every
// member row). Mock it so the test can assert the delegation.
const mockToggleAutoApprove = vi.fn()
vi.mock('@/composables/useSessionIdentity', () => ({
  toggleAutoApprove: (...a: any[]) => mockToggleAutoApprove(...a),
}))

vi.mock('@/composables/useAgents', () => ({ getAgentAvatar: () => '' }))
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/components/common/AgentSelectorDrawer.vue', () => ({
  default: { name: 'AgentSelectorDrawer', props: ['open'], template: '<div class="picker-stub" />' },
}))
// BottomSheet teleports to <body> and gates on everOpened; stub it with a
// pass-through so the sheet's own content is queryable in place.
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: { name: 'BottomSheet', template: '<div class="bs-stub"><slot /></div>' },
}))

function mountSheet(members: any[], props: Record<string, unknown> = {}) {
  return mount(GroupMemberSheet, {
    props: { groupId: 'g1', members, maxRounds: 10, autoApprove: false, ...props },
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

describe('GroupMemberSheet', () => {
  beforeEach(() => {
    mockRemove.mockClear()
    mockAdd.mockClear()
    mockUpdateSettings.mockClear()
    mockToggleAutoApprove.mockClear()
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
    // Host has no remove; left member has no remove; only Alice does.
    expect(w.findAll('.gm-remove')).toHaveLength(1)
  })

  it('removes a member through the API and emits changed', async () => {
    const w = mountSheet(MEMBERS)
    await w.find('.gm-remove').trigger('click')
    await flushPromises()
    expect(mockRemove).toHaveBeenCalledWith('g1', 'm2')
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('saves max rounds on change and renders the add button as a pill', async () => {
    const w = mountSheet(MEMBERS)
    await w.find('.gm-setting-input').setValue(5)
    await flushPromises()
    expect(mockUpdateSettings).toHaveBeenCalledWith('g1', 5)
    expect(w.find('.gm-add').classes()).toContain('fbtn')
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
})
