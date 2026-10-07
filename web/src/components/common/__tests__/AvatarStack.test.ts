import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AvatarStack from '../AvatarStack.vue'

vi.mock('@/components/common/AgentIcon.vue', () => ({
  default: { name: 'AgentIcon', props: ['backend', 'name', 'avatar', 'size'], template: '<span class="agent-icon-stub" :data-size="size" />' },
}))
vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: () => '',
}))

function member(i: number, extra: Record<string, unknown> = {}) {
  return { id: `m${i}`, agentId: `a${i}`, name: `M${i}`, backend: 'cli', ...extra }
}

describe('AvatarStack', () => {
  it('renders nothing for an empty roster', () => {
    const w = mount(AvatarStack, { props: { members: [] } })
    expect(w.find('.avatar-stack').exists()).toBe(false)
  })

  it('renders one disc per member and passes the size to AgentIcon', () => {
    const w = mount(AvatarStack, { props: { members: [member(1), member(2)], size: 'sm' } })
    expect(w.findAll('.avatar-disc')).toHaveLength(2)
    expect(w.findAll('.agent-icon-stub')[0].attributes('data-size')).toBe('sm')
    expect(w.find('.avatar-disc').classes()).toContain('avatar-disc--sm')
  })

  it('caps at max and summarises the rest as a "+N" text label (not a disc)', () => {
    const w = mount(AvatarStack, { props: { members: [member(1), member(2), member(3), member(4), member(5)] } })
    expect(w.findAll('.avatar-disc')).toHaveLength(4)
    const more = w.find('.avatar-more')
    expect(more.text()).toBe('+1')
    expect(more.classes()).not.toContain('avatar-disc')
  })

  it('honours a custom max', () => {
    const w = mount(AvatarStack, { props: { members: [member(1), member(2), member(3)], max: 2 } })
    expect(w.findAll('.avatar-disc')).toHaveLength(2)
    expect(w.find('.avatar-more').text()).toBe('+1')
  })

  it('puts the FIRST member on top (z-index decreases with DOM order)', () => {
    const w = mount(AvatarStack, { props: { members: [member(1), member(2), member(3)] } })
    const z = w.findAll('.avatar-disc').map(el => Number(el.element.style.zIndex))
    expect(z).toEqual([3, 2, 1])
  })

  it('marks the host disc and titles it accordingly', () => {
    const w = mount(AvatarStack, { props: { members: [member(1, { isHost: true }), member(2)] } })
    const discs = w.findAll('.avatar-disc')
    expect(discs[0].classes()).toContain('is-host')
    expect(discs[0].attributes('title')).toContain('Host')
    expect(discs[1].classes()).not.toContain('is-host')
  })

  it('moves the host to the front (top) and keeps the rest in order', () => {
    const w = mount(AvatarStack, {
      props: { members: [member(1), member(2, { isHost: true }), member(3)] },
    })
    const discs = w.findAll('.avatar-disc')
    // Host is first in the DOM, so it is fully visible (highest z-index)…
    expect(discs[0].classes()).toContain('is-host')
    // …and the others keep their original relative order.
    expect(discs.map(d => d.attributes('title'))).toEqual(['M2 (Host)', 'M1', 'M3'])
    const z = discs.map(d => Number(d.element.style.zIndex))
    expect(z[0]).toBeGreaterThan(z[1])
  })

  it('leaves the order untouched when no member is the host', () => {
    const w = mount(AvatarStack, { props: { members: [member(1), member(2), member(3)] } })
    expect(w.findAll('.avatar-disc').map(d => d.attributes('title'))).toEqual(['M1', 'M2', 'M3'])
  })

  it('gives EVERY disc the accent ring (host is distinguished by position)', async () => {
    // jsdom does not apply stylesheet rules, so assert on the source: the disc
    // rule must ring with --accent-color, and there must be no neutral
    // (--border-color) ring left on the base disc.
    const mod = await import('@/components/common/AvatarStack.vue?raw')
    const src = typeof mod.default === 'string' ? mod.default : ''
    const discRule = src.match(/\.avatar-disc\s*\{[^}]*\}/)?.[0] ?? ''
    expect(discRule).toContain('var(--accent-color')
    expect(discRule).not.toContain('var(--border-color)')
  })

  it('uses the provided tooltip when given, else the joined names', () => {
    const withTip = mount(AvatarStack, { props: { members: [member(1)], tooltip: 'custom tip' } })
    expect(withTip.find('.avatar-stack').attributes('title')).toBe('custom tip')
    const noTip = mount(AvatarStack, { props: { members: [member(1), member(2)] } })
    expect(noTip.find('.avatar-stack').attributes('title')).toBe('M1, M2')
  })

  it('never renders a speaking/highlight class (the ring animation was removed)', () => {
    // The active-speaker ring + pulse on the header stack was removed as
    // confusing. Guard against it creeping back: no prop and no class.
    const w = mount(AvatarStack, { props: { members: [member(1), member(2), member(3)] } })
    expect(w.findAll('.avatar-disc.is-speaking')).toHaveLength(0)
    expect(w.find('.avatar-disc').classes()).not.toContain('is-speaking')
    expect(AvatarStack.props).not.toHaveProperty('activeSpeakerId')
  })
})
