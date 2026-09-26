import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SessionListHeader from '@/components/session/SessionListHeader.vue'
import { readWebFile } from '@/testUtils/readWebFile'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
}))

function mountHeader(props = {}) {
  return mount(SessionListHeader, {
    props: { sessionCount: 0, sessionMaxCount: 10, ...props },
  })
}

describe('SessionListHeader', () => {
  it('renders counter when maxCount > 0', () => {
    const wrapper = mountHeader({ sessionCount: 5, sessionMaxCount: 10 })
    expect(wrapper.find('.session-counter').exists()).toBe(true)
  })

  it('does not render counter when maxCount is 0', () => {
    const wrapper = mountHeader({ sessionMaxCount: 0 })
    expect(wrapper.find('.session-counter').exists()).toBe(false)
  })

  it('renders default action buttons (search + create)', async () => {
    const wrapper = mountHeader()
    const search = wrapper.find('.header-action-btn[data-action="search"]')
    const create = wrapper.find('.header-action-btn[data-action="create"]')
    expect(search.exists()).toBe(true)
    expect(create.exists()).toBe(true)
  })

  it('emits open-search when search clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="search"]').trigger('click')
    expect(wrapper.emitted('open-search')).toBeTruthy()
  })

  it('emits create when create clicked', async () => {
    const wrapper = mountHeader()
    await wrapper.find('.header-action-btn[data-action="create"]').trigger('click')
    expect(wrapper.emitted('create')).toBeTruthy()
  })

  it('renders a leading extra button passed via slot', () => {
    const wrapper = mount(SessionListHeader, {
      props: { sessionCount: 0, sessionMaxCount: 0 },
      slots: { actions: '<button class="pin-stub" />' },
    })
    expect(wrapper.find('.pin-stub').exists()).toBe(true)
  })

  it('renders the trailing actions-end slot AFTER the built-in buttons', () => {
    // The sidebar's pin/unpin toggle goes here so it is the right-most control.
    // Order is the whole point: a button that renders first is not "right-most".
    const wrapper = mount(SessionListHeader, {
      props: { sessionCount: 0, sessionMaxCount: 0, pinned: true },
      slots: {
        actions: '<button class="leading-stub" />',
        'actions-end': '<button class="trailing-stub" />',
      },
    })
    const buttons = wrapper.findAll('.session-header-actions button')
    const classes = buttons.map((b) => b.classes())
    const trailingIdx = classes.findIndex((c) => c.includes('trailing-stub'))
    const searchIdx = classes.findIndex((c) => c.includes('header-action-btn'))
    expect(trailingIdx).toBeGreaterThan(-1)
    expect(searchIdx).toBeGreaterThan(-1)
    // Trailing slot is the last button; every built-in (search/create/refresh)
    // comes before it.
    expect(trailingIdx).toBe(buttons.length - 1)
    expect(trailingIdx).toBeGreaterThan(searchIdx)
  })

  it('centers the counter between two equal-flex groups, without overlapping the actions', () => {
    // jsdom does not lay out, so the centering is pinned at the source. The
    // meter is centered by giving the leading and trailing groups equal share
    // of the free space (`flex: 1 1 0` on both), NOT by absolute positioning:
    // absolute centering was measured to sit on top of the action buttons on a
    // narrow sidebar (220–280px), where 4–5 buttons need more than half the
    // header. The trailing group's `min-width: min-content` is what turns the
    // impossible case into "slide left" instead of "overlap".
    const src = readWebFile('src/components/session/SessionListHeader.vue')

    const ruleFor = (selector: string) => {
      const start = src.indexOf(selector + ' {')
      expect(start, `${selector} rule must exist`).toBeGreaterThan(-1)
      return src.slice(start, src.indexOf('}', start))
    }

    // The meter must NOT be absolutely positioned any more.
    expect(ruleFor('.session-counter')).not.toMatch(/position:\s*absolute/)

    // Both side groups share the free space equally — that is the centering.
    const leadingRule = ruleFor('.session-header-leading')
    expect(leadingRule).toMatch(/flex:\s*1 1 0/)
    // …and the leading group clips rather than spilling under the meter when
    // the header is too narrow for everything (220px + 5 buttons).
    expect(leadingRule).toMatch(/overflow:\s*hidden/)
    const actionsRule = ruleFor('.session-header-actions')
    expect(actionsRule).toMatch(/flex:\s*1 1 0/)
    // …and the actions floor their shrink at their own content width so the
    // meter slides left rather than overlapping them.
    expect(actionsRule).toMatch(/min-width:\s*min-content/)
  })

  it('when pinned, keeps search/create and also shows a refresh button', () => {
    const wrapper = mountHeader({ pinned: true })
    expect(wrapper.find('.header-action-btn[data-action="search"]').exists()).toBe(true)
    expect(wrapper.find('.header-action-btn[data-action="create"]').exists()).toBe(true)
    expect(wrapper.find('.header-action-btn[data-action="refresh"]').exists()).toBe(true)
  })

  it('emits refresh when the pinned refresh button is clicked', async () => {
    const wrapper = mountHeader({ pinned: true })
    await wrapper.find('.header-action-btn[data-action="refresh"]').trigger('click')
    expect(wrapper.emitted('refresh')).toBeTruthy()
  })

  it('shows the spinning feedback driven by the refreshing prop', async () => {
    const wrapper = mountHeader({ pinned: true })
    const btn = wrapper.find('.header-action-btn[data-action="refresh"]')
    expect(btn.classes()).not.toContain('refresh-spin--active')

    await btn.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    // Parent sets refreshing=true while its loadSessions() is in flight
    await wrapper.setProps({ refreshing: true })
    expect(btn.classes()).toContain('refresh-spin--active')

    // Double-click while refreshing is ignored
    await btn.trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)

    // Refresh completes → spin ends
    await wrapper.setProps({ refreshing: false })
    expect(btn.classes()).not.toContain('refresh-spin--active')
  })
})
