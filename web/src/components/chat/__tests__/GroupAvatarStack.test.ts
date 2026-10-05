import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupAvatarStack from '../GroupAvatarStack.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k }),
}))

// AgentIcon is a heavy SVG component; stub it with a hookable element.
vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" />' },
}))
vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: () => '',
}))
const sheetOpen = vi.fn()
vi.mock('../GroupMemberSheet.vue', () => ({
  default: { name: 'GroupMemberSheet', template: '<div class="sheet-stub" />', methods: { open: () => sheetOpen() } },
}))

function mountStack(members: any[]) {
  return mount(GroupAvatarStack, {
    props: {
      sessionId: 'g1',
      members,
      hostMemberId: members.find(m => m.isHost)?.id || '',
      isGroup: true,
    },
  })
}

describe('GroupAvatarStack', () => {
  it('renders one overlapping disc per member and NO add button', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'B', backend: 'claude', agentId: 'a3', isHost: false },
    ])
    expect(w.findAll('.stack-item')).toHaveLength(3)
    expect(w.findAll('.stack-item .agent-icon-stub')).toHaveLength(3)
    expect(w.find('.stack-add').exists()).toBe(false)
  })

  it('marks the host disc so it gets the accent ring', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
    ])
    const discs = w.findAll('.stack-item')
    expect(discs[0].classes()).toContain('is-host')
    expect(discs[1].classes()).not.toContain('is-host')
  })

  it('stacks with the FIRST member (host) on top, decreasing by DOM order', () => {
    // The host is first and must sit ON TOP; each later disc tucks UNDER the
    // previous one. So z-index decreases with DOM order (first = highest).
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'B', backend: 'claude', agentId: 'a3', isHost: false },
    ])
    const z = w.findAll('.stack-item').map(el => Number(el.element.style.zIndex))
    expect(z).toEqual([3, 2, 1])
    expect(z[0]).toBeGreaterThan(z[1])
    expect(z[1]).toBeGreaterThan(z[2])
  })

  it('does not dim members (no half-transparent icons)', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'Gone', backend: 'claude', agentId: 'a2', isHost: false, left: true },
    ])
    // Left members are NOT greyed any more; every disc renders at full opacity.
    expect(w.findAll('.stack-item')[1].classes()).not.toContain('is-left')
  })

  it('opens the member sheet when the stack is clicked anywhere', async () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
    ])
    sheetOpen.mockClear()
    await w.find('.agent-stack').trigger('click')
    expect(sheetOpen).toHaveBeenCalled()
  })

  it('does not render when the session is not a group', () => {
    const w = mount(GroupAvatarStack, {
      props: { sessionId: 's1', members: [], hostMemberId: '', isGroup: false },
    })
    expect(w.find('.agent-stack').exists()).toBe(false)
  })
})
