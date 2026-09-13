import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MenuSelect from '../MenuSelect.vue'

// PopupMenu teleports to <body>; the stub renders in place so options are
// queryable from the wrapper. Its own positioning/outside-click behaviour is
// covered by PopupMenu.test.ts.
const global = {
  stubs: { Teleport: { template: '<div><slot/></div>' } },
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
  // the control it drops out of.
  it('sizes the menu to the trigger width', async () => {
    const wrapper = mountSelect()
    const trigger = wrapper.find('.menu-select')
    Object.defineProperty(trigger.element, 'offsetWidth', { value: 180, configurable: true })
    await trigger.trigger('click')

    const list = wrapper.find('.menu-select-list')
    expect(list.attributes('style')).toContain('min-width: 180px')
  })
})
