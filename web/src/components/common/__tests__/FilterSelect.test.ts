import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import FilterSelect from '../FilterSelect.vue'

// PopupMenu teleports to <body>; the stub renders in place so menu rows are
// queryable from the wrapper. Positioning/outside-click belong to PopupMenu.
const global = {
  stubs: { Teleport: { template: '<div><slot/></div>' } },
}

const options = [
  { value: 'open', label: 'Open' },
  { value: 'closed', label: 'Closed' },
]

// Defaults live here rather than in the props object below: the component
// forwards a STATIC `class="filter-select is-active` onto MenuSelect's root,
// so a test that passes `class` through `mount()` would replace it instead of
// merging, and the tint assertions would read a class list Vue never produced.
// Passing the data the tint is derived from keeps the assertions honest.
type Props = {
  modelValue?: string | number | (string | number)[]
  allValue?: string | number
  options?: { value: string | number, label: string }[]
  caption?: string
  allLabel?: string
  multiple?: boolean
  minSelected?: number
}

function mountFilter(props: Props = {}) {
  return mount(FilterSelect, {
    props: {
      modelValue: '',
      options,
      caption: 'State',
      allLabel: 'All states',
      ...props,
    },
    global,
    attachTo: document.body,
  })
}

/** Menu row (only present while the popup is open). */
function rowLabels(wrapper: ReturnType<typeof mountFilter>): string[] {
  return wrapper.findAll('.menu-select-item-label').map(r => r.text())
}

describe('FilterSelect', () => {
  it('shows the caption plus the current value', () => {
    const wrapper = mountFilter({ modelValue: 'closed' })
    expect(wrapper.find('.filter-select-caption').text()).toBe('State')
    expect(wrapper.find('.filter-select-value').text()).toBe('Closed')
  })

  it('shows the all-label when nothing is filtered', () => {
    const wrapper = mountFilter({ modelValue: '' })
    expect(wrapper.find('.filter-select-value').text()).toBe('All states')
  })

  it('prepends the "all" row from allLabel instead of asking the caller for it', async () => {
    // The whole point of the extraction: a caller supplies real choices only.
    // Every caller used to hand-build a `__all__` sentinel row with its own
    // caption baked into the label.
    const wrapper = mountFilter()
    await wrapper.find('.menu-select').trigger('click')
    expect(rowLabels(wrapper)).toEqual(['All states', 'Open', 'Closed'])
  })

  it('does not duplicate the "all" row when the caller already includes it', async () => {
    const wrapper = mountFilter({
      options: [{ value: '', label: 'Everything' }, ...options],
    })
    await wrapper.find('.menu-select').trigger('click')
    expect(rowLabels(wrapper)).toEqual(['All states', 'Open', 'Closed'])
  })

  it('honours a custom all-value (the project filter resets to a path sentinel)', async () => {
    // Notifications reset the project scope to ALL_PROJECTS (''), but a filter
    // like the usage panel's may use a real value as "all". The component must
    // not assume ''.
    const wrapper = mountFilter({ allValue: 'any', modelValue: 'any' })
    await wrapper.find('.menu-select').trigger('click')
    const rows = wrapper.findAll('.menu-select-item-label').map(r => r.text())
    expect(rows[0]).toBe('All states')

    await wrapper.findAll('.menu-select-item')[2].trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['closed'])
  })

  describe('effective state', () => {
    it('is quiet when unfiltered', () => {
      const wrapper = mountFilter({ modelValue: '' })
      expect(wrapper.find('.filter-select').classes()).not.toContain('is-active')
    })

    it('is tinted as soon as the value stops meaning "all"', () => {
      const wrapper = mountFilter({ modelValue: 'open' })
      expect(wrapper.find('.filter-select').classes()).toContain('is-active')
    })

    it('treats a non-empty allValue as unfiltered', () => {
      // Regression guard for a caller that filters on a real value: comparing
      // against '' instead of allValue would tint an untouched filter.
      const wrapper = mountFilter({ allValue: 'any', modelValue: 'any' })
      expect(wrapper.find('.filter-select').classes()).not.toContain('is-active')
    })
  })

  it('emits the chosen value and never an array', async () => {
    const wrapper = mountFilter()
    await wrapper.find('.menu-select').trigger('click')
    await wrapper.findAll('.menu-select-item')[1].trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['open']])
  })

  describe('multiple mode', () => {
    const multiOptions = [{ value: '', label: 'All agents' }, ...options]

    function mountMulti(modelValue: (string | number)[]) {
      return mountFilter({
        multiple: true,
        minSelected: 1,
        modelValue,
        options: multiOptions,
        allLabel: 'All agents',
      })
    }

    it('is quiet when only the sentinel is selected', () => {
      const wrapper = mountMulti([''])
      expect(wrapper.find('.filter-select').classes()).not.toContain('is-active')
      expect(wrapper.find('.filter-select-value').text()).toBe('All agents')
    })

    it('tints when a real value narrows the set', () => {
      const wrapper = mountMulti(['open'])
      expect(wrapper.find('.filter-select').classes()).toContain('is-active')
      expect(wrapper.find('.filter-select-value').text()).toBe('Open')
    })

    it('ignores the sentinel in the collapse count', () => {
      // The sentinel is a state marker, not a choice: it must not turn a
      // one-agent selection into "All agents +1".
      const wrapper = mountMulti(['', 'open'])
      expect(wrapper.find('.filter-select-value').text()).toBe('Open')
    })

    it('is quiet when every real value is selected', () => {
      const wrapper = mountMulti(['open', 'closed'])
      expect(wrapper.find('.filter-select').classes()).not.toContain('is-active')
    })
  })

  it('puts caption and value in the tooltip', () => {
    // The pill is narrow on a phone and the value can be ellipsised; the
    // tooltip is the fallback that always spells it out.
    const wrapper = mountFilter({ modelValue: 'closed' })
    expect(wrapper.find('.menu-select').attributes('title')).toBe('State: Closed')
  })

  it('drops the caption from the tooltip when there is none', () => {
    const wrapper = mountFilter({ caption: '', modelValue: 'closed' })
    expect(wrapper.find('.menu-select').attributes('title')).toBe('Closed')
  })
})
