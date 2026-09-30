import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import GitGraph from '@/components/git/GitGraph.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual: any = await importOriginal()
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

// Real lane geometry matters here: the viewport tests assert on the *column
// width*, which is derived from each node's lane, so the stub assigns lanes the
// way computeGraphData does — a straight first-parent chain on lane 0, with
// optional extra lanes supplied per commit.
vi.mock('@/utils/gitGraph', async (importOriginal) => {
  const actual: any = await importOriginal()
  return {
    ...actual,
    computeGraphData: (commits: any[] = [], rowHeight = 64) => {
      const nodes = commits.map((c, i) => ({
        row: i,
        lane: c.lane ?? 0,
        cx: (c.lane ?? 0) * 20 + 20,
        cy: i * rowHeight + rowHeight / 2,
        color: '#0066cc',
        refs: c.refs || [],
        branchNames: [],
        isWT: !!c.isWT,
      }))
      const maxLane = nodes.reduce((m, n) => Math.max(m, n.lane), 0)
      return {
        nodes,
        lines: [],
        laneCount: maxLane + 1,
        graphWidth: Math.max(40, (maxLane + 1) * 20 + 20),
        shaToLane: new Map(),
        laneBranchName: new Map(),
      }
    },
  }
})

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

/**
 * The graph column is sized from the lanes in the current scroll window, not
 * from the whole graph. jsdom does not lay out elements, so these tests stub the
 * shared scroll container's geometry and drive measureViewport() directly.
 */
describe('GitGraph viewport-driven column width', () => {
  const ROW_HEIGHT = 64

  // 500 commits so the list always overflows the viewport. Rows 0-14 are lane
  // 0; rows 15+ introduce a second lane. The switch sits below row 12 so a
  // window at the top (10 rows + the 2-row margin at each edge) stays lane-0
  // only, and scrolling a little brings the second lane into view.
  function makeCommits() {
    return Array.from({ length: 500 }, (_, i) => ({
      sha: `s${i}`,
      parents: i < 499 ? [`s${i + 1}`] : [],
      lane: i >= 15 ? 1 : 0,
    }))
  }

  /**
   * Stub the container geometry. `clientHeight` is what the window can show;
   * `scrollTop` is where it currently is.
   *
   * The graph is modelled as the first child at content offset 0, so its rect
   * top moves up as the container scrolls — that is what makes the component's
   * content-coordinate conversion produce row 0 at the top of the list.
   */
  function stubGeometry(wrapper: ReturnType<typeof mountInScroller>, opts: {
    scrollTop: number
    clientHeight: number
  }) {
    const container = wrapper.find('.drilldown-body').element as HTMLElement
    const graphEl = wrapper.find('.git-graph-scroll').element as HTMLElement
    Object.defineProperty(container, 'clientHeight', { value: opts.clientHeight, configurable: true })
    Object.defineProperty(container, 'scrollTop', { value: opts.scrollTop, configurable: true })
    container.getBoundingClientRect = () => ({
      top: 0, bottom: opts.clientHeight, left: 0, right: 800,
      width: 800, height: opts.clientHeight, x: 0, y: 0, toJSON: () => ({}),
    }) as DOMRect
    graphEl.getBoundingClientRect = () => ({
      top: -opts.scrollTop,
      bottom: -opts.scrollTop + 500 * ROW_HEIGHT,
      left: 0, right: 300,
      width: 300, height: 500 * ROW_HEIGHT, x: 0, y: -opts.scrollTop, toJSON: () => ({}),
    }) as DOMRect
    return container
  }

  /**
   * The graph must live inside a `.drilldown-body` scroller for the measurement
   * to find one — GitCommitList provides it in production.
   */
  function mountInScroller(props: Record<string, unknown> = {}) {
    return mount(
      {
        components: { GitGraph },
        props: ['commits', 'rowHeight', 'collapsed'],
        template: '<div class="drilldown-body"><GitGraph ref="g" :commits="commits" :row-height="rowHeight" :collapsed="collapsed" /></div>',
      },
      {
        props: {
          commits: makeCommits(),
          rowHeight: ROW_HEIGHT,
          collapsed: false,
          ...props,
        },
        attachTo: document.body,
      },
    )
  }

  function graphWidth(wrapper: ReturnType<typeof mountInScroller>): string {
    return (wrapper.findComponent(GitGraph).find('svg.git-graph-svg').attributes('width') || '')
  }

  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('takes only the visible lanes while scrolled to the top', async () => {
    const wrapper = mountInScroller()
    await nextTick()
    const graph = wrapper.findComponent(GitGraph)
    stubGeometry(wrapper, { scrollTop: 0, clientHeight: 640 }) // 10 rows

    ;(graph.vm as any).measureViewport()
    await nextTick()

    // Rows 0-9 are all lane 0 → one lane → 1 * 20 + 20 = 40.
    expect((graph.vm as any).visibleLaneCount).toBe(1)
    expect(graphWidth(wrapper)).toBe('40')
    wrapper.unmount()
  })

  it('widens when a second lane scrolls into the window', async () => {
    const wrapper = mountInScroller()
    await nextTick()
    const graph = wrapper.findComponent(GitGraph)
    // Window covering rows ~8-18, which includes the lane-1 rows.
    stubGeometry(wrapper, { scrollTop: 8 * ROW_HEIGHT, clientHeight: 640 })
    ;(graph.vm as any).measureViewport()
    await nextTick()

    expect((graph.vm as any).visibleLaneCount).toBe(2)
    expect(graphWidth(wrapper)).toBe('60')
    wrapper.unmount()
  })

  it('keeps the full width when every loaded commit fits on screen', async () => {
    // 8 commits in a tall viewport: nothing is hidden, so no lane is waiting to
    // scroll in and the column stays at the full graph width.
    const commits = Array.from({ length: 8 }, (_, i) => ({
      sha: `s${i}`, parents: [], lane: i >= 4 ? 1 : 0,
    }))
    const wrapper = mountInScroller({ commits })
    await nextTick()
    const graph = wrapper.findComponent(GitGraph)
    stubGeometry(wrapper, { scrollTop: 0, clientHeight: 640 })

    ;(graph.vm as any).measureViewport()
    await nextTick()

    // maxLane 1 → full width 2 * 20 + 20 = 60, not the 40 a window-based
    // measurement would have produced.
    expect(graphWidth(wrapper)).toBe('60')
    wrapper.unmount()
  })

  it('measures against the shared scroll container, not its own scroll position', async () => {
    // The graph has no scroll of its own (overflow-y: hidden); reading its own
    // scrollTop would always report row 0 and the column would never widen.
    const wrapper = mountInScroller()
    await nextTick()
    const graph = wrapper.findComponent(GitGraph)
    stubGeometry(wrapper, { scrollTop: 100 * ROW_HEIGHT, clientHeight: 640 })

    ;(graph.vm as any).measureViewport()
    await nextTick()

    expect((graph.vm as any).visibleLaneCount).toBe(2)
    wrapper.unmount()
  })

  it('falls back to the full width when there is no scroll container', async () => {
    // Mounted bare (no .drilldown-body ancestor) — nothing to measure.
    const wrapper = mount(GitGraph, {
      props: { commits: makeCommits(), rowHeight: ROW_HEIGHT, collapsed: false },
      attachTo: document.body,
    })
    const graph = wrapper.findComponent(GitGraph)
    ;(graph.vm as any).measureViewport()
    await nextTick()

    expect((graph.vm as any).visibleRowCount).toBe(0)
    expect(graphWidth(wrapper)).toBe('60') // full width for 2 lanes
    wrapper.unmount()
  })

  it('re-measures when the commit list changes', async () => {
    const wrapper = mountInScroller()
    await nextTick()
    const graph = wrapper.findComponent(GitGraph)
    stubGeometry(wrapper, { scrollTop: 0, clientHeight: 640 })
    ;(graph.vm as any).measureViewport()
    await nextTick()
    expect((graph.vm as any).visibleLaneCount).toBe(1)

    // Lazy load appends more commits; the window is unchanged but the graph is
    // recomputed, so the measurement must run again.
    const extended = makeCommits().concat([
      { sha: 'x1', parents: [], lane: 3 },
    ])
    await wrapper.setProps({ commits: extended })
    await nextTick()
    ;(graph.vm as any).measureViewport()
    await nextTick()

    // Still only lane 0 in the top window.
    expect((graph.vm as any).visibleLaneCount).toBe(1)
    wrapper.unmount()
  })
})
