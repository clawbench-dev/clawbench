import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GitGraph from '@/components/git/GitGraph.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual: any = await importOriginal()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/utils/gitGraph', () => ({
  computeGraphData: (commits: any[] = [], rowHeight = 64) => ({
    nodes: commits.map((c, i) => ({
      row: i,
      cx: 20,
      cy: i * rowHeight + rowHeight / 2,
      color: '#0066cc',
      refs: c.refs || [],
      branchNames: [],
      isWT: false,
    })),
    lines: [],
    laneCount: 1,
    graphWidth: 40,
    shaToLane: new Map(),
    laneBranchName: new Map(),
  }),
  refLabelText: (ref: string) => ref,
}))

vi.mock('@/composables/useSettingsConfig', () => ({
  getZoomedViewport: () => ({ width: 800, height: 600 }),
  toFixedCSS: (n: number) => String(n),
}))

describe('GitGraph', () => {
  function mountGraph(props: Record<string, unknown> = {}) {
    return mount(GitGraph, {
      props: {
        commits: [],
        rowHeight: 64,
        collapsed: false,
        ...props,
      },
      attachTo: document.body,
    })
  }

  it('renders scroll container', () => {
    const wrapper = mountGraph()
    expect(wrapper.find('.git-graph-scroll').exists()).toBe(true)
  })

  it('renders svg element with correct dimensions', () => {
    const wrapper = mountGraph({ commits: [{ sha: 'a', parents: [] }, { sha: 'b', parents: ['a'] }] })
    const svg = wrapper.find('svg.git-graph-svg')
    expect(svg.exists()).toBe(true)
    expect(svg.attributes('width')).toBe('40')
    expect(svg.attributes('height')).toBe('132')
  })

  it('applies collapsed-mode class when collapsed', () => {
    const wrapper = mountGraph({ collapsed: true })
    expect(wrapper.find('.git-graph-scroll').classes()).toContain('collapsed-mode')
  })

  it('uses collapsed svg width when collapsed', () => {
    const wrapper = mountGraph({ collapsed: true })
    const svg = wrapper.find('svg.git-graph-svg')
    expect(svg.attributes('width')).toBe('20')
  })

  it('renders one node group per commit', () => {
    const wrapper = mountGraph({
      commits: [{ sha: 'a', parents: [] }, { sha: 'b', parents: ['a'] }, { sha: 'c', parents: ['b'] }],
    })
    const nodes = wrapper.findAll('g.git-graph-nodes > g')
    expect(nodes.length).toBe(3)
  })

  it('does not render line connections when collapsed', () => {
    const wrapper = mountGraph({ collapsed: true, commits: [{ sha: 'a', parents: [] }] })
    expect(wrapper.find('g.git-graph-lines').exists()).toBe(false)
  })

  it('declares update:collapsed emit', () => {
    expect((GitGraph as any).emits || []).toContain('update:collapsed')
  })

  it('dismissTooltip sets tooltip to null on scroll', async () => {
    const wrapper = mountGraph({ commits: [{ sha: 'a', parents: [], refs: ['HEAD'] }] })
    ;(wrapper.vm as any).tooltip = { x: 10, y: 10, items: ['HEAD'], color: '#000' }
    await wrapper.find('.git-graph-scroll').trigger('scroll')
    expect((wrapper.vm as any).tooltip).toBeNull()
  })
})
