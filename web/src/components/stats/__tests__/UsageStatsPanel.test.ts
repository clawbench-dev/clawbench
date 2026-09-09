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

import UsageStatsPanel from '@/components/stats/UsageStatsPanel.vue'
import { useUsageStats } from '@/composables/useUsageStats'

const zhMessages = {
  nav: { stats: '数据统计', refresh: '刷新' },
  stats: {
    range24h: '近 24 小时',
    range7d: '近 7 天',
    range30d: '近 30 天',
    custom: '自定义',
    rangeTitle: '时间范围',
    filterTitle: '统计条件',
    summaryTitle: '用量总览',
    summaryInOut: '输入 vs 输出',
    summaryInputCache: '输入构成 — 缓存命中 / 未命中',
    back: '返回',
    onlyOneSide: '仅单侧有用量',
    cacheHitShort: '缓存命中',
    cacheMissShort: '缓存未命中',
    detailTitle: '维度明细',
    dimTitle: '分组方式',
    dimModel: '模型',
    dimBackend: 'AI 后端',
    dimAgent: '智能体',
    metricTitle: '数值列',
    colInput: '输入 Tokens',
    colOutput: '输出 Tokens',
    colTotal: '总 Tokens',
    colCacheHit: '缓存命中 Tokens',
    colHitRate: '缓存命中率',
    colCredit: 'Credit',
    colCost: '费用 (USD)',
    chartTitle: '图表类型',
    chartBar: '直方图',
    chartPie: '饼图',
    chartTrend: '时间趋势',
    emptyLabel: '未标注',
    unknown: '未知',
    noData: '所选时间段内暂无用量数据',
    loadFailed: '统计加载失败',
  },
  common: { loading: '加载中' },
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
    totals: { input: 0, output: 0, total: 0, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 0 },
    rows: [],
    ...partial,
  }
}

// Records every option prop pushed to a UsageChart stub so tests can assert a
// chart rebuild (e.g. on theme change) happened without needing echarts DOM.
const chartOptions: unknown[] = []
let currentChartProps: Record<string, unknown> | null = null

async function mountPanel() {
  chartOptions.length = 0
  currentChartProps = null
  const wrapper = shallowMount(UsageStatsPanel, {
    props: { active: true },
    global: {
      plugins: [makeI18n()],
      stubs: {
        RefreshButton: { template: '<button class="refresh-btn" @click="$emit(\'click\')" />' },
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
          setup(props: Record<string, unknown>) { currentChartProps = props },
        },
      },
    },
  })
  await nextTick()
  await flushPromises()
  await nextTick()
  return wrapper
}

describe('UsageStatsPanel', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    // Reset the shared composable state to defaults so stale raw/error from a
    // previous test never leaks into the next mount.
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    const stats = useUsageStats()
    await stats.resetStatsFilter()
    await flushPromises()
    mockApiGet.mockReset()
  })

  it('fetches on mount when active', async () => {
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    await mountPanel()
    expect(mockApiGet).toHaveBeenCalled()
  })

  it('shows noData when rows are empty', async () => {
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('所选时间段内暂无用量数据')
  })

  it('rebuilds the chart option when the theme changes', async () => {
    // Charts read their palette from CSS variables at option-build time, so a
    // theme switch must recompute and re-push the option (themeTick bump).
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 0, output: 0, total: 5, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [{ key: { model: 'glm' }, input: 0, output: 0, total: 5, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 }],
    }))
    await mountPanel()
    expect(chartOptions.length).toBeGreaterThan(0)
    const optionsBefore = chartOptions.length

    window.dispatchEvent(new CustomEvent('clawbench-theme-change', { detail: 'dark' }))
    await nextTick()

    // The option prop must be rebuilt (new object pushed to the chart) with the
    // new theme's palette.
    expect(chartOptions.length).toBeGreaterThan(optionsBefore)
  })

  it('renders overview cards: additive metrics + cache hit tokens + hit rate', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 100, output: 0, total: 100, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0.05, messageCnt: 1 },
      rows: [{ key: { model: 'glm' }, input: 100, output: 0, total: 100, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0.05, messageCnt: 1 }],
    }))
    const wrapper = await mountPanel()
    const cards = wrapper.findAll('.stats-total')
    const labels = cards.map(c => c.find('.stats-total-label')?.text() ?? '')
    const cardText = cards.map(c => c.text()).join(' | ')
    // Cache hit tokens are an overview card; the hit-rate percentage is a
    // derived card; cacheMiss itself is not a standalone card.
    expect(labels).toContain('缓存命中 Tokens')
    expect(labels).toContain('缓存命中率')
    expect(labels).not.toContain('缓存未命中')
    expect(cardText).toContain('输入 Tokens')
    expect(cardText).toContain('总 Tokens')
    expect(cardText).toContain('费用 (USD)')
    expect(cardText).toContain('80.0%') // cache hit rate
    expect(cardText).not.toContain('输出 Tokens') // zero → hidden
    expect(cardText).not.toContain('Credit') // zero → hidden
  })

  it('renders a data table with dim columns and selected metric columns', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 0, output: 0, total: 5, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [{ key: { model: 'glm-4.6', agent: 'CodeBuddy' }, input: 0, output: 0, total: 5, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 }],
    }))
    // Select both model + agent dims so both keys become table columns.
    const stats = useUsageStats()
    stats.setDims(['model', 'agent'])
    await flushPromises()
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('glm-4.6')
    expect(text).toContain('CodeBuddy')
    expect(text).toContain('5')
    // Flush the debounced reload scheduled by setDims so no timer leaks.
    await new Promise(r => setTimeout(r, 350))
  })

  it('defaults to a single selected metric column (total)', () => {
    // Default filter asserted through the composable singleton state.
    const stats = useUsageStats()
    expect(stats.filter.value.metrics).toEqual(['total'])
    expect(stats.filter.value.dims).toEqual(['model'])
    expect(stats.filter.value.chartType).toBe('bar')
  })

  it('displays the server message when api fails', async () => {
    mockApiGet.mockRejectedValue({ message: '服务器开小差了', status: 500 })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('服务器开小差了')
  })

  it('renders charts (not the empty state) for a trend response', async () => {
    // Trend requests return rows:[] plus trend:[day×dim series].
    const stats = useUsageStats()
    stats.setChartType('trend')
    await flushPromises()
    mockApiGet.mockResolvedValue(mockResponse({
      rows: [],
      trend: [
        { day: '2026-09-01', key: { model: 'glm' }, input: 0, output: 0, total: 10, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
        { day: '2026-09-02', key: { model: 'glm' }, input: 0, output: 0, total: 20, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
      ],
    }))
    const wrapper = await mountPanel()
    await new Promise(r => setTimeout(r, 400))
    const text = wrapper.text()
    expect(text).not.toContain('所选时间段内暂无用量数据')
    expect(wrapper.findAll('.usage-chart-stub').length).toBeGreaterThan(0)
  })

  it('drops zero-value rows from the table (no wasted rows)', async () => {
    // Two models: glm has usage, empty-model has all-zero for the selected
    // metric (total) → its row must be filtered out.
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 5, output: 0, total: 5, cacheHit: 3, cacheMiss: 2, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [
        { key: { model: 'glm' }, input: 5, output: 0, total: 5, cacheHit: 3, cacheMiss: 2, credit: 0, costUsd: 0, messageCnt: 1 },
        { key: { model: 'no-usage' }, input: 0, output: 0, total: 0, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
      ],
    }))
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('glm')
    expect(text).not.toContain('no-usage') // zero row dropped
  })

  it('renders the input/output overview donut from totals', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 300, output: 100, total: 400, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [{ key: { model: 'glm' }, input: 300, output: 100, total: 400, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0, messageCnt: 1 }],
    }))
    const wrapper = await mountPanel()
    // The overview donut is the pie option pushed with the totals slices.
    expect(wrapper.text()).toContain('输入 vs 输出')
    const donutOption = chartOptions.find(
      o => (o as { series?: { type?: string }[] }).series?.[0]?.type === 'pie',
    ) as { series?: { type?: string; data?: { name: string; value: number }[] }[] } | undefined
    const series = donutOption?.series?.[0]
    expect(series?.type).toBe('pie')
    const values = series?.data?.map(d => d.value) ?? []
    expect(values).toEqual(expect.arrayContaining([300, 100]))
  })

  it('places the totals overview above the dimension filter card', async () => {
    // The overview reflects time-range totals only and must not sit under the
    // dimension filter (which re-groups the table below).
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 300, output: 100, total: 400, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [{ key: { model: 'glm' }, input: 300, output: 100, total: 400, cacheHit: 80, cacheMiss: 20, credit: 0, costUsd: 0, messageCnt: 1 }],
    }))
    const wrapper = await mountPanel()
    const panels = wrapper.findAll('.stats-card-panel')
    const summaryIdx = panels.findIndex(p => p.text().includes('用量总览'))
    const filterIdx = panels.findIndex(p => p.text().includes('统计条件'))
    expect(summaryIdx).toBeGreaterThanOrEqual(0)
    expect(filterIdx).toBeGreaterThan(summaryIdx)
  })

  it('renders custom date inputs when the custom range chip is clicked', async () => {
    mockApiGet.mockResolvedValue(mockResponse({ rows: [] }))
    const wrapper = await mountPanel()
    const customBtn = wrapper.findAll('.stats-chip').find(b => b.text() === '自定义')
    expect(customBtn).toBeTruthy()
    await customBtn!.trigger('click')
    await nextTick()
    const dateInputs = wrapper.findAll('.stats-date-input')
    expect(dateInputs.length).toBe(2)
  })

  it('formats token values in M/K tiers on overview cards and table cells', async () => {
    mockApiGet.mockResolvedValue(mockResponse({
      totals: { input: 2_500_000, output: 800_000, total: 3_300_000, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 },
      rows: [{ key: { model: 'glm' }, input: 2_500_000, output: 800_000, total: 3_300_000, cacheHit: 0, cacheMiss: 0, credit: 0, costUsd: 0, messageCnt: 1 }],
    }))
    // Show the input/output/total columns so the table exercises every tier.
    const stats = useUsageStats()
    stats.setMetrics(['input', 'output', 'total'])
    await flushPromises()
    const wrapper = await mountPanel()
    const text = wrapper.text()
    // Overview cards: M for millions, K for hundreds of thousands.
    expect(text).toContain('2.5M')
    expect(text).toContain('800.0K')
    expect(text).toContain('3.3M')
    // Same tiers in the detail table cells.
    const cellTexts = wrapper.findAll('.stats-td-num').map(td => td.text())
    expect(cellTexts).toContain('2.5M')
    expect(cellTexts).toContain('800.0K')
    expect(cellTexts).toContain('3.3M')
    // Flush the debounced reload scheduled by setMetrics so no timer leaks.
    await new Promise(r => setTimeout(r, 350))
  })
})
