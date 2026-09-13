import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionGroupHeader from '@/components/session/SessionGroupHeader.vue'

/**
 * SessionGroupHeader is the single collapsible header shared by the project
 * pane (Pinned / Recent) and the cross-project pane (one per other project).
 * These tests pin the props contract both callers rely on:
 * - count renders as a badge, and 0 is a real value (not treated as "no count")
 * - subtitle renders the project path with a tooltip
 * - collapsed drives the chevron rotation
 * - clicking anywhere on the header emits `toggle` (the parent owns the state)
 */

function mountHeader(props = {}) {
  return mount(SessionGroupHeader, {
    props: { title: 'Recent', ...props },
  })
}

describe('SessionGroupHeader', () => {
  it('renders the title', () => {
    const wrapper = mountHeader({ title: 'Pinned' })
    expect(wrapper.find('.session-group-title').text()).toBe('Pinned')
  })

  it('renders the count badge', () => {
    const wrapper = mountHeader({ count: 3 })
    expect(wrapper.find('.session-group-count').text()).toBe('3')
  })

  it('renders a count of 0 rather than hiding the badge', () => {
    // 0 is a legitimate count (an empty Recent section); only `null` hides it.
    const wrapper = mountHeader({ count: 0 })
    expect(wrapper.find('.session-group-count').exists()).toBe(true)
    expect(wrapper.find('.session-group-count').text()).toBe('0')
  })

  it('hides the count badge when count is null', () => {
    const wrapper = mountHeader({ count: null })
    expect(wrapper.find('.session-group-count').exists()).toBe(false)
  })

  it('renders the subtitle with the subtitleTitle as its tooltip', () => {
    const wrapper = mountHeader({ subtitle: '~/proj/other', subtitleTitle: '/proj/other' })
    const sub = wrapper.find('.session-group-subtitle')
    expect(sub.text()).toBe('~/proj/other')
    expect(sub.attributes('title')).toBe('/proj/other')
  })

  it('falls back to the subtitle text as the tooltip when subtitleTitle is absent', () => {
    const wrapper = mountHeader({ subtitle: '~/proj/other' })
    expect(wrapper.find('.session-group-subtitle').attributes('title')).toBe('~/proj/other')
  })

  it('omits the subtitle element when no subtitle is given', () => {
    const wrapper = mountHeader()
    expect(wrapper.find('.session-group-subtitle').exists()).toBe(false)
  })

  it('rotates the chevron only when collapsed', () => {
    const expanded = mountHeader({ collapsed: false })
    expect(expanded.find('.session-group-chevron').classes()).not.toContain('collapsed')

    const collapsed = mountHeader({ collapsed: true })
    expect(collapsed.find('.session-group-chevron').classes()).toContain('collapsed')
  })

  it('emits toggle when the header is clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.session-group-header').trigger('click')
    expect(wrapper.emitted('toggle')).toHaveLength(1)
  })

  it('renders slotted icon content (e.g. the Pinned pin)', () => {
    const wrapper = mount(SessionGroupHeader, {
      props: { title: 'Pinned' },
      slots: { icon: '<span class="my-icon" />' },
    })
    expect(wrapper.find('.my-icon').exists()).toBe(true)
  })
})
