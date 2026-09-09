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

  it('lets the trend chart be scoped to selected authors (multi-select)', async () => {
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

  it('renders the code-inventory (cloc) card with per-language rows', async () => {
    // git-stats + cloc are fetched on mount; route by URL.
    mockApiGet.mockImplementation((url: string) => {
      if (String(url).includes('/cloc')) {
        return Promise.resolve({
          languages: [
            { name: 'Go', files: 2, code: 150, comment: 10, blank: 20 },
            { name: 'Vue', files: 1, code: 50, comment: 5, blank: 5 },
          ],
          total: { name: '', files: 3, code: 200, comment: 15, blank: 25 },
          scannedAt: '2026-09-09T00:00:00Z',
        })
      }
      return Promise.resolve(mockResponse({ rows: [] }))
    })
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('代码存量')
    expect(text).toContain('Go')
    expect(text).toContain('Vue')
    expect(text).toContain('200') // total code
    expect(text).toContain('合计')
    // Total code formatted in M/K tier — 200 stays raw.
    const codes = wrapper.findAll('.stats-td-num').map(td => td.text())
    expect(codes).toContain('150')
    expect(codes).toContain('50')
  })

  it('shows a hint when the workspace has no source files', async () => {
    mockApiGet.mockImplementation((url: string) => {
      if (String(url).includes('/cloc')) {
        return Promise.resolve({ languages: [], total: { files: 0, code: 0, comment: 0, blank: 0 }, scannedAt: '2026-09-09T00:00:00Z' })
      }
      return Promise.resolve(mockResponse({ rows: [] }))
    })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('当前工作区暂无源码文件')
  })
})
