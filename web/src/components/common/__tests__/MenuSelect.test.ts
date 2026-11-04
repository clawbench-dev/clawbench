import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MenuSelect from '../MenuSelect.vue'

// PopupMenu teleports to <body>; the stub renders in place so options are
// queryable from the wrapper. Its own positioning/outside-click behaviour is
// covered by PopupMenu.test.ts.
const global = {
  stubs: { Teleport: { template: '<div><slot/></div>' } },
}

// Stand-in for PopupMenu that exposes the width props it would receive. Used by
// the sizing tests: the real component writes those onto the popup ROOT (the
// scroll container) inside a requestAnimationFrame, which jsdom cannot measure.
const PopupMenuStub = {
  name: 'PopupMenu',
  props: ['show', 'minWidth', 'maxWidth'],
  template: '<div v-if="show"><slot/></div>',
}

const options = [
  { value: '', label: 'No repository' },
  { value: 'github|github.com|acme/widgets', label: 'acme/widgets' },
]

function mountSelect(props: Record<string, unknown> = {}) {
  return mount(MenuSelect, {
    props: { modelValue: '', options, ...props },
    global,
  })
}

function mountWithPopupStub(props: Record<string, unknown> = {}) {
  return mount(MenuSelect, {
    props: { modelValue: '', options, ...props },
    global: { stubs: { Teleport: { template: '<div><slot/></div>' }, PopupMenu: PopupMenuStub } },
  })
}

describe('MenuSelect', () => {
  it('shows the label of the matching option', () => {
    const wrapper = mountSelect({ modelValue: 'github|github.com|acme/widgets' })
    expect(wrapper.find('.menu-select-label').text()).toBe('acme/widgets')
  })

  // A value with no matching option (e.g. a scope pointing at a repo the
  // project is no longer bound to) must not render blank.
  it('falls back to the placeholder when nothing matches', () => {
    const wrapper = mountSelect({ modelValue: 'github|github.com|gone/repo', placeholder: 'Unlinked' })
    expect(wrapper.find('.menu-select-label').text()).toBe('Unlinked')
  })

  it('starts closed and opens on click', async () => {
    const wrapper = mountSelect()
    expect(wrapper.find('.menu-select-list').exists()).toBe(false)
    await wrapper.find('.menu-select').trigger('click')
    expect(wrapper.find('.menu-select-list').exists()).toBe(true)
  })

  it('emits the chosen value and closes', async () => {
    const wrapper = mountSelect()
    await wrapper.find('.menu-select').trigger('click')
    const items = wrapper.findAll('.menu-select-item')
    await items[1].trigger('click')

    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['github|github.com|acme/widgets'])
    expect(wrapper.emitted('change')?.[0]).toEqual(['github|github.com|acme/widgets'])
    expect(wrapper.find('.menu-select-list').exists()).toBe(false)
  })

  // Re-picking the current value is a no-op: emitting would mark the form dirty
  // and could trigger an unnecessary save.
  it('emits nothing when the current value is re-picked', async () => {
    const wrapper = mountSelect({ modelValue: 'github|github.com|acme/widgets' })
    await wrapper.find('.menu-select').trigger('click')
    const active = wrapper.findAll('.menu-select-item').find(i => i.classes().includes('active'))!
    await active.trigger('click')

    expect(wrapper.emitted('update:modelValue')).toBeFalsy()
    expect(wrapper.find('.menu-select-list').exists()).toBe(false)
  })

  it('marks the selected option active', async () => {
    const wrapper = mountSelect({ modelValue: 'github|github.com|acme/widgets' })
    await wrapper.find('.menu-select').trigger('click')
    const items = wrapper.findAll('.menu-select-item')
    expect(items[0].classes()).not.toContain('active')
    expect(items[1].classes()).toContain('active')
  })

  it('does not open or emit while disabled', async () => {
    const wrapper = mountSelect({ disabled: true })
    const trigger = wrapper.find('.menu-select')
    expect(trigger.attributes('disabled')).toBeDefined()
    await trigger.trigger('click')
    expect(wrapper.find('.menu-select-list').exists()).toBe(false)
  })

  // The whole point of the component: it must never be a native <select>, whose
  // OS picker is what this replaces.
  it('renders as a button trigger, not a native select', () => {
    const wrapper = mountSelect()
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.find('button.menu-select').exists()).toBe(true)
  })

  // The menu is measured against the trigger so it cannot open narrower than
  // the control it drops out of. The width goes on the popup ROOT (the scroll
  // container), not on the list inside it — a child wider than the root's
  // content box painted a horizontal scrollbar under the menu.
  it('sizes the menu to the trigger width', async () => {
    const wrapper = mountWithPopupStub()
    const trigger = wrapper.find('.menu-select')
    Object.defineProperty(trigger.element, 'offsetWidth', { value: 180, configurable: true })
    await trigger.trigger('click')

    expect(wrapper.findComponent(PopupMenuStub).props('minWidth')).toBe(180)
    expect(wrapper.find('.menu-select-list').attributes('style')).toBeUndefined()
  })

  // A `block` field stretches to the form width (600px), which is wider than the
  // popup's max width; the popup clamps the floor to that max width itself.
  it('passes the raw trigger width and leaves the clamping to the popup', async () => {
    const wrapper = mountWithPopupStub({ block: true, menuWidth: 400 })
    const trigger = wrapper.find('.menu-select')
    Object.defineProperty(trigger.element, 'offsetWidth', { value: 600, configurable: true })
    await trigger.trigger('click')

    const popup = wrapper.findComponent(PopupMenuStub)
    expect(popup.props('minWidth')).toBe(600)
    expect(popup.props('maxWidth')).toBe(400)
  })
})
