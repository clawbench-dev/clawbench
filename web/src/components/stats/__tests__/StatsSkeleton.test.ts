import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import StatsSkeleton from '@/components/stats/StatsSkeleton.vue'

function mountSkeleton(props: Record<string, unknown> = {}) {
  return mount(StatsSkeleton, { props })
}

describe('StatsSkeleton', () => {
  it('previews the panel shape: overview cards, table rows, chart', () => {
    const wrapper = mountSkeleton({ cards: 3, tableRows: 4, chart: true, label: '加载中' })

    expect(wrapper.findAll('.stats-skeleton-card').length).toBe(3)
    expect(wrapper.findAll('.stats-skeleton-row').length).toBe(4)
    expect(wrapper.find('.stats-skeleton-chart').exists()).toBe(true)
    // Three sections: cards, table, chart.
    expect(wrapper.findAll('.stats-skeleton-section').length).toBe(3)
  })

  it('hides a block entirely when its count is zero', () => {
    const wrapper = mountSkeleton({ cards: 0, tableRows: 0, chart: false })
    expect(wrapper.findAll('.stats-skeleton-section').length).toBe(0)
    expect(wrapper.find('.stats-skeleton-card').exists()).toBe(false)
    expect(wrapper.find('.stats-skeleton-row').exists()).toBe(false)
    expect(wrapper.find('.stats-skeleton-chart').exists()).toBe(false)
  })

  it('keeps the decoration out of the a11y tree but announces the load', () => {
    const wrapper = mountSkeleton({ cards: 2, label: '加载中' })

    // Every visible block is decorative — a screen reader walking them would
    // hear a stream of nothing.
    for (const section of wrapper.findAll('.stats-skeleton-section')) {
      expect(section.attributes('aria-hidden')).toBe('true')
    }
    // The one live region carries the status text.
    const status = wrapper.find('[role="status"]')
    expect(status.exists()).toBe(true)
    expect(status.text()).toBe('加载中')
  })

  it('cycles fixed row widths rather than randomizing them', async () => {
    const wrapper = mountSkeleton({ cards: 0, tableRows: 7, chart: false })
    const widths = () => wrapper.findAll('.stats-skeleton-row').map(r => (r.element as HTMLElement).style.width)
    const first = widths()
    // A random placeholder would shift on every re-render and read as content
    // arriving then changing; the same row count must always yield the same
    // widths.
    expect(first.length).toBe(7)
    expect(first[0]).toBe(first[5])
    await wrapper.setProps({ cards: 1 })
    expect(widths()).toEqual(first)
  })
})
