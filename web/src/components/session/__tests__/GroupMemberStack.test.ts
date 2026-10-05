import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupMemberStack from '../GroupMemberStack.vue'

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

function member(i: number) {
  return { id: `m${i}`, agentId: `a${i}`, name: `M${i}`, backend: 'cli' }
}

describe('GroupMemberStack', () => {
  it('renders one disc per member when under the cap', () => {
    const w = mount(GroupMemberStack, { props: { members: [member(1), member(2)] } })
    expect(w.findAll('.stack-disc')).toHaveLength(2)
    expect(w.find('.stack-more').exists()).toBe(false)
  })

  it('caps the discs and folds the rest into a +N counter', () => {
    const w = mount(GroupMemberStack, { props: { members: [member(1), member(2), member(3), member(4), member(5)] } })
    // 3 capped discs + 1 overflow disc.
    expect(w.findAll('.stack-disc')).toHaveLength(4)
    expect(w.find('.stack-more').text()).toBe('+2')
  })

  it('honours a custom max', () => {
    const w = mount(GroupMemberStack, { props: { members: [member(1), member(2), member(3)], max: 1 } })
    expect(w.findAll('.stack-disc')).toHaveLength(2)
    expect(w.find('.stack-more').text()).toBe('+2')
  })

  it('renders nothing for an empty roster', () => {
    const w = mount(GroupMemberStack, { props: { members: [] } })
    expect(w.find('.group-member-stack').exists()).toBe(false)
  })
})
