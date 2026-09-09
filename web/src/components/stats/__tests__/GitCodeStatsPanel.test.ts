import { describe, expect, it, vi, beforeEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
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
vi.mock('echarts/components', () => ({ GridComponent: {}, TooltipComponent: {}, LegendComponent: {}, TitleComponent: {}, DataZoomComponent: {} }))
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
    notGitRepo: '当前项目不是 Git 仓库，无法统计代码量',
    noData: '所选时间段内暂无代码提交',
    loadFailed: '代码统计加载失败',
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

const chartOptions: unknown[] = []

async function mountPanel() {
  chartOptions.length = 0
  const wrapper = shallowMount(GitCodeStatsPanel, {
    props: { active: true },
    global: {
      plugins: [makeI18n()],
      stubs: {
        LoadingIndicator: { template: '<span class="loading-stub" />' },
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
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    const stats = useGitCodeStats()
    await stats.loadGitStats()
    await flushPromises()
    mockApiGet.mockReset()
  })

  it('fetches on mount when active', async () => {
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    await mountPanel()
    expect(mockApiGet).toHaveBeenCalled()
  })

  it('shows the not-a-git-repo hint when isGit is false', async () => {
    mockApiGet.mockResolvedValue({ isGit: false, rows: [], trend: [] })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('当前项目不是 Git 仓库')
  })

  it('shows noData when in a git repo but no commits in range', async () => {
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('所选时间段内暂无代码提交')
  })

  it('renders totals cards with M/K formatting and colored net', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
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

  it('renders a per-author table with commit counts', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
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

  it('renders the trend chart from per-day rows folded across authors', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
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

  it('lets the trend chart be scoped to a single author', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
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
    // Two authors → the author-scope chips (plus the "all authors" default) render.
    const chips = wrapper.findAll('.stats-chip').map(c => c.text())
    expect(chips).toEqual(expect.arrayContaining(['全部作者', 'Ann', 'Bob']))

    // Default "all authors": per-day sums across Ann+Bob.
    const lastOption = (): { series?: { data: number[] }[]; xAxis?: { data: string[] } } =>
      chartOptions[chartOptions.length - 1] as never
    expect(lastOption().series?.[0]?.data).toEqual([22, 8])

    // Click "Bob" → the trend lines now only carry Bob's own daily values.
    // (Days come from the filtered rows, so only days where Bob committed stay.)
    const bobChip = wrapper.findAll('.stats-chip').find(c => c.text() === 'Bob')
    await bobChip!.trigger('click')
    await nextTick()
    await flushPromises()
    expect(lastOption().series?.[0]?.data).toEqual([10])
    expect(lastOption().xAxis?.data).toEqual(['2026-09-01'])
  })

  it('shows the server error message on failure', async () => {
    mockApiGet.mockRejectedValue({ message: 'git 统计失败', status: 500 })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('git 统计失败')
  })
})
