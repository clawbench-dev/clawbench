import { describe, expect, it, vi, beforeEach } from 'vitest'
import { shallowMount, mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'

// --- Mocks ---
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))
vi.mock('echarts/core', () => ({
  use: vi.fn(),
  init: vi.fn(() => ({
    setOption: vi.fn(),
    resize: vi.fn(),
    dispose: vi.fn(),
  })),
}))
vi.mock('echarts/charts', () => ({ BarChart: {}, PieChart: {}, LineChart: {} }))
vi.mock('echarts/components', () => ({ GridComponent: {}, TooltipComponent: {}, LegendComponent: {}, TitleComponent: {}, DataZoomComponent: {}, GraphicComponent: {} }))
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }))

import GitCodeStatsPanel from '@/components/stats/GitCodeStatsPanel.vue'
import { useGitCodeStats, resetGitStats } from '@/composables/useGitCodeStats'

// Minimal i18n dictionary — keyed strings so assertions target text.
const zhMessages = {
  common: { loading: '加载中' },
  nav: { refresh: '刷新' },
  gitStats: {
    range24h: '近 24 小时',
    range7d: '近 7 天',
    range30d: '近 30 天',
    custom: '自定义',
    rangeTitle: '时间范围',
    panelTitle: '代码量统计',
    summaryTitle: '代码量总览',
    authorTitle: '按作者明细',
    trendTitle: '每日代码量趋势',
    colAuthor: '作者',
    colAdded: '新增行数',
    colDeleted: '删除行数',
    colNet: '净增行数',
    colCommits: '提交数',
    allAuthors: '全部作者',
    efficiencyTitle: '生产效率',
    efficiencyHint: '每净增 1K 行代码的消耗（当前净增 {net} 行）',
    effTokens: 'Token / 1K 行',
    effCredit: 'Credit / 1K 行',
    effCost: '费用 / 1K 行',
    effNoNet: '当前区间净增行数为 0 或为负，无法计算每千行消耗',
    effNoUsage: '当前区间没有用量记录，无法计算每千行消耗',
    effLoadFailed: '用量数据加载失败',
    notGitRepo: '当前项目不是 Git 仓库，无法统计代码量',
    noData: '所选时间段内暂无代码提交',
    loadFailed: '代码统计加载失败',
    clocTitle: '代码存量',
    clocColLang: '语言',
    clocColFiles: '文件数',
    clocColCode: '代码行',
    clocColComment: '注释行',
    clocColBlank: '空白行',
    clocTotalLabel: '合计',
    clocNoData: '当前工作区暂无源码文件',
    clocLoadFailed: '代码存量加载失败',
  },
}

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    messages: { zh: zhMessages },
  })
}

function mockResponse(partial: Record<string, unknown>) {
  return {
    isGit: true,
    totals: { added: 0, deleted: 0, net: 0, commitCnt: 0 },
    rows: [],
    trend: [],
    ...partial,
  }
}

/** Usage totals returned for the efficiency group's /api/usage/stats call. */
const EMPTY_USAGE = { totals: { total: 0, credit: 0, costUsd: 0 }, rows: [] }

/**
 * Route the two requests the panel issues: the git stats payload and the
 * usage totals used for the per-1K ratios. Both are keyed off the URL so a
 * test that only cares about git data does not accidentally feed the git
 * payload to the usage endpoint (which would silently disable the ratios).
 */
function routeMock(gitResp: unknown, usageResp: unknown = EMPTY_USAGE) {
  mockApiGet.mockImplementation((url: string) =>
    Promise.resolve(String(url).startsWith('/api/usage/stats') ? usageResp : gitResp))
}

const chartOptions: unknown[] = []

/** Poll until `cond` holds — the skeleton gate is time-based by design. */
async function waitFor(cond: () => boolean, timeoutMs = 3000): Promise<void> {
  const start = Date.now()
  while (!cond()) {
    if (Date.now() - start > timeoutMs) throw new Error('waitFor timed out')
    await new Promise(r => setTimeout(r, 20))
  }
}

async function mountPanel() {
  chartOptions.length = 0
  const wrapper = shallowMount(GitCodeStatsPanel, {
    props: { active: true },
    global: {
      plugins: [makeI18n()],
      stubs: {
        StatsSkeleton: { name: 'StatsSkeleton', template: '<div class="skeleton-stub" />' },
        UsageChart: {
          template: '<div class="usage-chart-stub" />',
          props: ['option'],
          watch: {
            option: {
              handler(v: unknown) { chartOptions.push(v) },
              immediate: true,
            },
          },
        },
      },
    },
  })
  await nextTick()
  await flushPromises()
  await nextTick()
  return wrapper
}

describe('GitCodeStatsPanel', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    resetGitStats()
    routeMock(mockResponse({ rows: [] }))
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    mockApiGet.mockReset()
  })

  it('fetches on mount when active', async () => {
    routeMock(mockResponse({ rows: [] }))
    await mountPanel()
    expect(mockApiGet).toHaveBeenCalled()
  })

  it('shows the not-a-git-repo hint when isGit is false', async () => {
    routeMock({ isGit: false, rows: [], trend: [] })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('当前项目不是 Git 仓库')
  })

  it('shows noData when in a git repo but no commits in range', async () => {
    routeMock(mockResponse({ rows: [] }))
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('所选时间段内暂无代码提交')
  })

  it('renders totals cards with M/K formatting and colored net', async () => {
    routeMock(mockResponse({
      totals: { added: 2500, deleted: 300, net: 2200, commitCnt: 12 },
      rows: [],
    }))
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('新增行数')
    expect(text).toContain('2.5K')
    expect(text).toContain('删除行数')
    expect(text).toContain('300')
    expect(text).toContain('净增行数')
    expect(text).toContain('+2.2K')
    expect(text).toContain('提交数')
    expect(text).toContain('12')
    const netCard = wrapper.findAll('.stats-total').find(c => c.text().includes('净增行数'))
    expect(netCard!.find('.git-val-pos').exists()).toBe(true)
  })

  it('gives every totals card its own icon', async () => {
    routeMock(mockResponse({
      totals: { added: 2500, deleted: 300, net: 2200, commitCnt: 12 },
      rows: [],
    }))
    // Full mount: the cards render `<component :is>` lucide icons, which
    // shallowMount turns into indistinguishable stubs.
    const wrapper = mount(GitCodeStatsPanel, {
      props: { active: true },
      global: {
        plugins: [makeI18n()],
        stubs: { UsageChart: { template: '<div class="usage-chart-stub" />' }, LoadingIndicator: true },
      },
    })
    await nextTick()
    await flushPromises()
    await nextTick()

    const cards = wrapper.findAll('.stats-total')
    expect(cards.length).toBe(4)
    const icons = cards.map(c => c.find('.stats-total-icon'))
    expect(icons.every(i => i.exists())).toBe(true)
    const glyphs = icons.map(i => i.classes().find(cls => cls.startsWith('lucide-') && cls.endsWith('-icon')))
    expect(glyphs.every(Boolean)).toBe(true)
    expect(new Set(glyphs).size).toBe(glyphs.length)
    expect(icons[0].attributes('aria-hidden')).toBe('true')
    // Icon is decorative and must not leak into the label text.
    expect(cards[0].find('.stats-total-label-text').text()).toBe('新增行数')
    wrapper.unmount()
  })

  it('renders a per-author table with commit counts', async () => {
    routeMock(mockResponse({
      totals: { added: 10, deleted: 2, net: 8, commitCnt: 3 },
      rows: [
        { author: 'A', added: 6, deleted: 1, net: 5, commitCnt: 2 },
        { author: 'B', added: 4, deleted: 1, net: 3, commitCnt: 1 },
      ],
    }))
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('A')
    expect(text).toContain('B')
    expect(wrapper.findAll('.stats-td-dim').length).toBe(2)
  })

  it('sorts the per-author table by each column', async () => {
    routeMock(mockResponse({
      totals: { added: 30, deleted: 5, net: 25, commitCnt: 3 },
      rows: [
        { author: 'A', added: 6, deleted: 1, net: 5, commitCnt: 2 },
        { author: 'B', added: 10, deleted: 2, net: 8, commitCnt: 1 },
        { author: 'C', added: 14, deleted: 2, net: 12, commitCnt: 0 },
      ],
    }))
    const wrapper = await mountPanel()
    const firstAuthor = () => wrapper.findAll('.stats-td-dim')[0].text()
    const headerText = (name: string) => wrapper.findAll('.stats-table th').find(h => h.text().includes(name))!

    // Default: added desc → C (14) first.
    expect(firstAuthor()).toBe('C')

    // Click commits header → commitCnt desc → A (2) first.
    await headerText('提交数').trigger('click')
    await nextTick()
    expect(firstAuthor()).toBe('A')

    // Click commits again → asc → C (0) first.
    await headerText('提交数').trigger('click')
    await nextTick()
    expect(firstAuthor()).toBe('C')

    // Click net header → net desc → C (12).
    await headerText('净增行数').trigger('click')
    await nextTick()
    expect(firstAuthor()).toBe('C')

    // Click author header → name asc → A.
    await headerText('作者').trigger('click')
    await nextTick()
    expect(firstAuthor()).toBe('A')
  })

  it('renders the trend chart from per-day rows folded across authors', async () => {
    routeMock(mockResponse({
      totals: { added: 10, deleted: 2, net: 8, commitCnt: 2 },
      rows: [{ author: 'A', added: 10, deleted: 2, net: 8, commitCnt: 2 }],
      trend: [
        { day: '2026-09-01', author: 'A', added: 6, deleted: 1, net: 5, commitCnt: 1 },
        { day: '2026-09-02', author: 'A', added: 4, deleted: 1, net: 3, commitCnt: 1 },
      ],
    }))
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('每日代码量趋势')
    expect(chartOptions.length).toBeGreaterThan(0)
    const option = chartOptions[0] as { xAxis?: { data: string[] }; series?: { name: string; data: number[] }[] }
    expect(option.xAxis?.data).toEqual(['2026-09-01', '2026-09-02'])
    expect(option.series?.[0]?.data).toEqual([6, 4])
    expect(option.series?.[1]?.data).toEqual([1, 1])
    // Net line (series index 2) = added − deleted per day.
    expect(option.series?.map(s => s.name)).toEqual(['新增行数', '删除行数', '净增行数'])
    expect(option.series?.[2]?.data).toEqual([5, 3])
  })

  it('lets the trend chart be scoped to selected authors (multi-select)', async () => {
    routeMock(mockResponse({
      totals: { added: 30, deleted: 5, net: 25, commitCnt: 3 },
      rows: [
        { author: 'Ann', added: 20, deleted: 3, net: 17, commitCnt: 2 },
        { author: 'Bob', added: 10, deleted: 2, net: 8, commitCnt: 1 },
      ],
      trend: [
        { day: '2026-09-01', author: 'Ann', added: 12, deleted: 2, net: 10, commitCnt: 1 },
        { day: '2026-09-01', author: 'Bob', added: 10, deleted: 2, net: 8, commitCnt: 1 },
        { day: '2026-09-02', author: 'Ann', added: 8, deleted: 1, net: 7, commitCnt: 1 },
      ],
    }))
    const wrapper = await mountPanel()
    // Two authors → the author-scope chips (plus the "all authors" toggle) render.
    const chips = wrapper.findAll('.stats-chip').map(c => c.text())
    expect(chips).toEqual(expect.arrayContaining(['全部作者', 'Ann', 'Bob']))

    const lastOption = (): { series?: { data: number[] }[]; xAxis?: { data: string[] } } =>
      chartOptions[chartOptions.length - 1] as never

    // Default: no selection → per-day sums across Ann+Bob.
    expect(lastOption().series?.[0]?.data).toEqual([22, 8])

    // Select Bob → lines narrow to Bob's own daily values.
    const bobChip = wrapper.findAll('.stats-chip').find(c => c.text() === 'Bob')
    await bobChip!.trigger('click')
    await nextTick()
    await flushPromises()
    expect(lastOption().series?.[0]?.data).toEqual([10])
    expect(lastOption().xAxis?.data).toEqual(['2026-09-01'])
    expect(bobChip!.classes()).toContain('active')

    // Multi-select: also select Ann → lines show their per-day sum.
    const annChip = wrapper.findAll('.stats-chip').find(c => c.text() === 'Ann')
    await annChip!.trigger('click')
    await nextTick()
    await flushPromises()
    expect(annChip!.classes()).toContain('active')
    expect(lastOption().series?.[0]?.data).toEqual([22, 8])
    expect(lastOption().xAxis?.data).toEqual(['2026-09-01', '2026-09-02'])

    // Deselect Bob → only Ann remains.
    await bobChip!.trigger('click')
    await nextTick()
    await flushPromises()
    expect(lastOption().series?.[0]?.data).toEqual([12, 8])

    // "全部作者" clears every selection back to the summed view.
    const allChip = wrapper.findAll('.stats-chip').find(c => c.text() === '全部作者')
    await allChip!.trigger('click')
    await nextTick()
    await flushPromises()
    expect(allChip!.classes()).toContain('active')
    const authorChips = wrapper.findAll('.stats-chip').filter(c => c.text() === 'Ann' || c.text() === 'Bob')
    expect(authorChips.every(c => !c.classes().includes('active'))).toBe(true)
    expect(lastOption().series?.[0]?.data).toEqual([22, 8])
  })

  it('shows the server error message on failure', async () => {
    mockApiGet.mockRejectedValue({ message: 'git 统计失败', status: 500 })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('git 统计失败')
  })

  // --- Skeleton on range switch ---

  it('shows a skeleton instead of stale numbers while a new range loads', async () => {
    routeMock(mockResponse({ totals: { added: 2500, deleted: 300, net: 2200, commitCnt: 12 }, rows: [] }))
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('+2.2K')
    expect(wrapper.find('.skeleton-stub').exists()).toBe(false)

    // Stall both requests of the next refresh (git stats + usage totals). Each
    // call gets its own resolver, so both must be settled — releasing only the
    // last one leaves the other promise pending and leaks into the next test.
    const pending: ((v: unknown) => void)[] = []
    mockApiGet.mockImplementation(() => new Promise(r => { pending.push(r as (v: unknown) => void) }))
    const stats = useGitCodeStats()
    stats.setRange({ rangeKey: '7d' })
    await nextTick()

    // A fast local query must not flash: the previous numbers stay briefly.
    expect(wrapper.find('.skeleton-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('+2.2K')

    await waitFor(() => wrapper.find('.skeleton-stub').exists())
    // Once the skeleton is up the old range's numbers are gone — leaving them
    // under the new chip is what read as a frozen panel.
    expect(wrapper.text()).not.toContain('+2.2K')
    expect(wrapper.text()).not.toContain('提交数')

    // Settle the pending requests so the module-level composable state is not
    // left mid-flight for the next test.
    expect(pending.length).toBeGreaterThan(0)
    for (const release of pending) release(mockResponse({ rows: [] }))
    await flushPromises()
  })

  it('shows loading (not a ratio) while the two halves of the window disagree', async () => {
    // Git answers instantly with the new range while the usage query is still
    // in flight. Rendering a ratio now would divide the NEW net lines by the
    // PREVIOUS window's spend — a plausible number describing no window.
    routeMock(
      mockResponse({ totals: { added: 2000, deleted: 0, net: 2000, commitCnt: 1 }, rows: [] }),
      { totals: { total: 200_000, credit: 20, costUsd: 2 }, rows: [] },
    )
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('Token / 1K 行')

    const pending: ((v: unknown) => void)[] = []
    mockApiGet.mockImplementation((url: string) => {
      if (String(url).startsWith('/api/usage/stats')) {
        return new Promise(r => { pending.push(r as (v: unknown) => void) })
      }
      return Promise.resolve(mockResponse({ totals: { added: 4000, deleted: 0, net: 4000, commitCnt: 2 }, rows: [] }))
    })
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await nextTick()

    // New net lines are in, spend is not — no ratio, and the card says loading.
    expect(wrapper.text()).toContain('+4.0K')
    expect(wrapper.text()).not.toContain('Token / 1K 行')
    expect(wrapper.text()).toContain('加载中')

    for (const release of pending) release({ totals: { total: 400_000, credit: 40, costUsd: 4 }, rows: [] })
    await flushPromises()
    await nextTick()
    // 4000 net lines = 4 units → 100K tokens per unit.
    expect(wrapper.text()).toContain('100.0K')
  })

  it('shows loading (not a ratio) when the usage half lands first', async () => {
    // Mirror of the case above: spend is in, net lines are not. Dividing the
    // NEW spend by the PREVIOUS range's net lines is just as wrong.
    routeMock(
      mockResponse({ totals: { added: 2000, deleted: 0, net: 2000, commitCnt: 1 }, rows: [] }),
      { totals: { total: 200_000, credit: 20, costUsd: 2 }, rows: [] },
    )
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('100.0K')

    const pending: ((v: unknown) => void)[] = []
    mockApiGet.mockImplementation((url: string) => {
      if (String(url).startsWith('/api/usage/stats')) {
        return Promise.resolve({ totals: { total: 800_000, credit: 80, costUsd: 8 }, rows: [] })
      }
      return new Promise(r => { pending.push(r as (v: unknown) => void) })
    })
    const stats = useGitCodeStats()
    const load = stats.loadGitStats()
    await flushPromises()
    await nextTick()

    // Fresh spend, stale net lines → no ratio at all.
    expect(wrapper.text()).not.toContain('Token / 1K 行')
    expect(wrapper.text()).toContain('加载中')

    for (const release of pending) release(mockResponse({ totals: { added: 4000, deleted: 0, net: 4000, commitCnt: 2 }, rows: [] }))
    await load
    await flushPromises()
    await nextTick()
    // 4000 net lines = 4 units → 200K tokens per unit.
    expect(wrapper.text()).toContain('200.0K')
  })

  // --- Efficiency group (spend per 1K net added lines) ---

  it('renders per-1K tokens / credit / cost from the same-window usage totals', async () => {
    routeMock(
      mockResponse({ totals: { added: 2500, deleted: 500, net: 2000, commitCnt: 4 }, rows: [] }),
      { totals: { total: 1_000_000, credit: 200, costUsd: 4 }, rows: [] },
    )
    const wrapper = await mountPanel()
    const text = wrapper.text()

    expect(text).toContain('生产效率')
    // 2000 net lines = 2 units → 500K tokens, 100 credit, $2.00 per unit.
    expect(text).toContain('500.0K')
    expect(text).toContain('100')
    expect(text).toContain('$2.00')
    // The hint names the denominator so the ratio is never read as absolute spend.
    expect(text).toContain('当前净增 +2.0K 行')
  })

  it('asks the usage endpoint for the same window as the git stats', async () => {
    routeMock(
      mockResponse({ totals: { added: 1000, deleted: 0, net: 1000, commitCnt: 1 }, rows: [] }),
      { totals: { total: 1000, credit: 1, costUsd: 1 }, rows: [] },
    )
    await mountPanel()

    const urls = mockApiGet.mock.calls.map(c => String(c[0]))
    const gitURL = urls.find(u => u.startsWith('/api/git/stats'))!
    const usageURL = urls.find(u => u.startsWith('/api/usage/stats'))!
    expect(gitURL).toBeTruthy()
    expect(usageURL).toBeTruthy()

    const gitParams = new URLSearchParams(gitURL.split('?')[1])
    const usageParams = new URLSearchParams(usageURL.split('?')[1])
    expect(usageParams.get('start')).toBe(gitParams.get('start'))
    expect(usageParams.get('end')).toBe(gitParams.get('end'))
    expect(usageParams.get('scope')).toBe('project')
  })

  it('explains a non-positive net instead of rendering a bogus ratio', async () => {
    routeMock(
      mockResponse({ totals: { added: 100, deleted: 400, net: -300, commitCnt: 2 }, rows: [] }),
      { totals: { total: 500_000, credit: 50, costUsd: 1 }, rows: [] },
    )
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('净增行数为 0 或为负')
    // No ratio cards at all — a negative denominator must not produce numbers.
    expect(text).not.toContain('Token / 1K 行')
  })

  it('explains an empty usage window instead of claiming zero spend', async () => {
    routeMock(mockResponse({ totals: { added: 2000, deleted: 0, net: 2000, commitCnt: 1 }, rows: [] }))
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('没有用量记录')
    expect(text).not.toContain('Token / 1K 行')
  })
})
