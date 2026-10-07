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

function mountStack(members: any[], props: Record<string, unknown> = {}) {
  return mount(GroupAvatarStack, {
    props: {
      sessionId: 'g1',
      members,
      hostMemberId: members.find(m => m.isHost)?.id || '',
      maxRounds: 10,
      isGroup: true,
      ...props,
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
    expect(w.findAll('.avatar-disc')).toHaveLength(3)
    expect(w.findAll('.avatar-disc .agent-icon-stub')).toHaveLength(3)
    expect(w.find('.stack-add').exists()).toBe(false)
  })

  it('caps the stack at 4 discs: exactly 4 members shows 4 avatars, no +N', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'B', backend: 'claude', agentId: 'a3', isHost: false },
      { id: 'm4', name: 'C', backend: 'claude', agentId: 'a4', isHost: false },
    ])
    expect(w.findAll('.avatar-disc')).toHaveLength(4)
    expect(w.find('.avatar-more').exists()).toBe(false)
  })

  it('shows 4 avatars + a "+N" text label (not a disc) when there are more than 4 members', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'B', backend: 'claude', agentId: 'a3', isHost: false },
      { id: 'm4', name: 'C', backend: 'claude', agentId: 'a4', isHost: false },
      { id: 'm5', name: 'D', backend: 'claude', agentId: 'a5', isHost: false },
      { id: 'm6', name: 'E', backend: 'claude', agentId: 'a6', isHost: false },
    ])
    // 4 avatar discs, no 5th disc.
    expect(w.findAll('.avatar-disc')).toHaveLength(4)
    expect(w.findAll('.avatar-disc .agent-icon-stub')).toHaveLength(4)
    // The overflow count is a plain text label beside the stack.
    const more = w.find('.avatar-more')
    expect(more.exists()).toBe(true)
    expect(more.text()).toBe('+2')
    expect(more.classes()).not.toContain('avatar-disc')
  })

  it('marks the host disc so it gets the accent ring', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
    ])
    const discs = w.findAll('.avatar-disc')
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
    const z = w.findAll('.avatar-disc').map(el => Number(el.element.style.zIndex))
    expect(z).toEqual([3, 2, 1])
    expect(z[0]).toBeGreaterThan(z[1])
    expect(z[1]).toBeGreaterThan(z[2])
  })

  it('opens the member sheet when the stack is clicked anywhere', async () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
    ])
    sheetOpen.mockClear()
    await w.find('.group-avatar-stack').trigger('click')
    expect(sheetOpen).toHaveBeenCalled()
  })

  it('does not render when the session is not a group', () => {
    const w = mount(GroupAvatarStack, {
      props: { sessionId: 's1', members: [], hostMemberId: '', maxRounds: 10, isGroup: false },
    })
    expect(w.find('.group-avatar-stack').exists()).toBe(false)
  })

  it('never highlights a member (the active-speaker animation was removed)', () => {
    const w = mountStack([
      { id: 'm1', name: 'Host', backend: 'codebuddy', agentId: 'a1', isHost: true },
      { id: 'm2', name: 'A', backend: 'claude', agentId: 'a2', isHost: false },
      { id: 'm3', name: 'B', backend: 'claude', agentId: 'a3', isHost: false },
    ])

    expect(w.findAll('.avatar-disc.is-speaking')).toHaveLength(0)
  })
})
