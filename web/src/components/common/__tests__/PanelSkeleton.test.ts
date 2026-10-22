import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'

function mountSkeleton(props: Record<string, unknown> = {}) {
  return mount(PanelSkeleton, { props })
}

describe('PanelSkeleton', () => {
  it('previews list rows with leading icon and trailing meta by default', () => {
    const wrapper = mountSkeleton({ rows: 5 })

    expect(wrapper.findAll('.panel-skeleton-row').length).toBe(5)
    // Files / sessions / forge rows all lead with an icon and trail with a size
    // or timestamp; the default must reserve both or the real rows shift.
    expect(wrapper.findAll('.panel-skeleton-leading').length).toBe(5)
    expect(wrapper.findAll('.panel-skeleton-meta').length).toBe(5)
  })

  it('drops the optional parts when asked', () => {
    const wrapper = mountSkeleton({ rows: 3, leading: false, meta: false })

    expect(wrapper.findAll('.panel-skeleton-row').length).toBe(3)
    expect(wrapper.find('.panel-skeleton-leading').exists()).toBe(false)
    expect(wrapper.find('.panel-skeleton-meta').exists()).toBe(false)
  })

  it('draws square tiles for the grid variant', () => {
    const wrapper = mountSkeleton({ variant: 'grid', rows: 12 })

    expect(wrapper.findAll('.panel-skeleton-tile').length).toBe(12)
    expect(wrapper.find('.panel-skeleton-thumb').exists()).toBe(true)
    expect(wrapper.find('.panel-skeleton-caption').exists()).toBe(true)
    // A grid panel must not also emit list rows.
    expect(wrapper.find('.panel-skeleton-row').exists()).toBe(false)
  })

  it('draws paragraph lines for the text variant', () => {
    const wrapper = mountSkeleton({ variant: 'text', rows: 7 })

    expect(wrapper.findAll('.panel-skeleton-line').length).toBe(7)
    expect(wrapper.find('.panel-skeleton-row').exists()).toBe(false)
    expect(wrapper.find('.panel-skeleton-tile').exists()).toBe(false)
  })

  it('keeps the decoration out of the a11y tree but announces the load', () => {
    const wrapper = mountSkeleton({ rows: 4, label: '加载中' })

    // Every visible block is decorative — a screen reader walking them would
    // hear a stream of nothing.
    for (const row of wrapper.findAll('.panel-skeleton-row')) {
      expect(row.attributes('aria-hidden')).toBe('true')
    }
    // The one live region carries the status text.
    const status = wrapper.find('[role="status"]')
    expect(status.exists()).toBe(true)
    expect(status.text()).toBe('加载中')
  })

  it('cycles fixed widths rather than randomizing them', async () => {
    const wrapper = mountSkeleton({ rows: 7 })
    const widths = () =>
      wrapper.findAll('.panel-skeleton-row').map(r =>
        (r.find('.panel-skeleton-line').element as HTMLElement).style.width)

    const first = widths()
    expect(first.length).toBe(7)
    // The cycle repeats, so row 6 repeats row 0 — a random placeholder would
    // shift on every re-render and read as content arriving then changing.
    expect(first[6]).toBe(first[0])

    await wrapper.setProps({ meta: false })
    expect(widths()).toEqual(first)
  })
})
