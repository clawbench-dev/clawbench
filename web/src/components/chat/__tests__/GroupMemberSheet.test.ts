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

function mountSheet(members: any[]) {
  return mount(GroupMemberSheet, {
    props: { groupId: 'g1', members, hostMemberId: 'm1' },
  })
}

const MEMBERS = [
  { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
  { id: 'm2', name: 'Alice', backend: 'claude', agentId: 'a2', isHost: false },
  { id: 'm3', name: 'Gone', backend: 'claude', agentId: 'a3', isHost: false, left: true },
]

describe('GroupMemberSheet', () => {
  beforeEach(() => {
    mockRemove.mockClear()
    mockAdd.mockClear()
    mockUpdateSettings.mockClear()
  })

  it('renders one row per member with an avatar and name', () => {
    const w = mountSheet(MEMBERS)
    expect(w.findAll('.gm-row')).toHaveLength(3)
    expect(w.findAll('.gm-avatar .agent-icon-stub')).toHaveLength(3)
    expect(w.findAll('.gm-name').map(n => n.text())).toEqual(['Host', 'Alice', 'Gone'])
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
})
