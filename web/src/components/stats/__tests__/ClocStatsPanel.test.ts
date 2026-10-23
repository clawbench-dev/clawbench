import { describe, expect, it, vi, beforeEach } from 'vitest'
import { shallowMount, mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'

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
vi.mock('@/stores/app', () => ({
  store: { state: { projectRoot: '/p' } },
}))

import ClocStatsPanel from '@/components/stats/ClocStatsPanel.vue'
import { resetGitStats } from '@/composables/useGitCodeStats'

const zhMessages = {
  common: { loading: '加载中' },
  gitStats: {
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

const clocMock = {
  languages: [
    { name: 'Go', files: 2, code: 150, comment: 10, blank: 20 },
    { name: 'Vue', files: 1, code: 50, comment: 5, blank: 5 },
    { name: 'TypeScript', files: 10, code: 300, comment: 40, blank: 60 },
  ],
  total: { name: '', files: 13, code: 500, comment: 55, blank: 85 },
  scannedAt: '2026-09-09T00:00:00Z',
}

const chartOptions: unknown[] = []

async function mountPanel() {
  chartOptions.length = 0
  const wrapper = shallowMount(ClocStatsPanel, {
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

/** Poll until `cond` holds — the skeleton gate is time-based by design. */
async function waitFor(cond: () => boolean, timeoutMs = 3000): Promise<void> {
  const start = Date.now()
  while (!cond()) {
    if (Date.now() - start > timeoutMs) throw new Error('waitFor timed out')
    await new Promise(r => setTimeout(r, 20))
  }
}

describe('ClocStatsPanel', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    resetGitStats()
    mockApiGet.mockResolvedValue(clocMock)
  })

  it('fetches on mount when active', async () => {
    await mountPanel()
    expect(mockApiGet).toHaveBeenCalled()
    expect(String(mockApiGet.mock.calls[0][0])).toContain('/api/git/cloc')
  })

  it('renders totals, chart and per-language rows with a total row', async () => {
    const wrapper = await mountPanel()
    const text = wrapper.text()
    expect(text).toContain('代码存量')
    expect(text).toContain('Go')
    expect(text).toContain('Vue')
    expect(text).toContain('TypeScript')
    expect(text).toContain('合计')
    // Chart got the cloc bar option.
    expect(chartOptions.length).toBeGreaterThan(0)
    // The bar is a hairline over a full-width track — the same geometry as the
    // usage bar chart, so both panels read as one system. Asserted against a
    // literal (not BAR_MAX_WIDTH) so a drifted constant fails here too.
    const series = (chartOptions.at(-1) as { series: { barMaxWidth?: number; showBackground?: boolean }[] }).series[0]
    expect(series.barMaxWidth).toBeLessThanOrEqual(6)
    expect(series.showBackground).toBe(true)
    // Total row values.
    const rows = wrapper.findAll('.stats-table tbody tr')
    expect(rows.length).toBe(4) // 3 languages + total
  })

  it('gives every totals card its own icon', async () => {
    // Full mount: the cards render lucide icons directly, which shallowMount
    // turns into indistinguishable stubs.
    const wrapper = mount(ClocStatsPanel, {
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
    expect(cards.length).toBe(4) // code / comment / blank / files
    const icons = cards.map(c => c.find('.stats-total-icon'))
    expect(icons.every(i => i.exists())).toBe(true)
    const glyphs = icons.map(i => i.classes().find(cls => cls.startsWith('lucide-') && cls.endsWith('-icon')))
    expect(glyphs.every(Boolean)).toBe(true)
    expect(new Set(glyphs).size).toBe(glyphs.length)
    expect(icons[0].attributes('aria-hidden')).toBe('true')
    expect(cards[0].find('.stats-total-label-text').text()).toBe('代码行')
    wrapper.unmount()
  })

  it('sorts by code desc by default', async () => {
    const wrapper = await mountPanel()
    const firstLang = wrapper.findAll('.stats-td-dim')[0]
    expect(firstLang.text()).toBe('TypeScript') // highest code 300
  })

  it('sorts by a clicked column and toggles direction', async () => {
    const wrapper = await mountPanel()
    // Click "文件数" header → sort by files desc (TS 10 first again). Click
    // again → files asc (Vue 1 first).
    const headers = wrapper.findAll('.stats-table th')
    const filesHeader = headers.find(h => h.text().includes('文件数'))
    await filesHeader!.trigger('click')
    await nextTick()
    let firstLang = wrapper.findAll('.stats-td-dim')[0]
    expect(firstLang.text()).toBe('TypeScript')

    await filesHeader!.trigger('click')
    await nextTick()
    firstLang = wrapper.findAll('.stats-td-dim')[0]
    expect(firstLang.text()).toBe('Vue')

    // Click language header → asc alphabetical (Go first).
    const langHeader = headers.find(h => h.text().includes('语言'))
    await langHeader!.trigger('click')
    await nextTick()
    firstLang = wrapper.findAll('.stats-td-dim')[0]
    expect(firstLang.text()).toBe('Go')
  })

  it('shows a hint when the workspace has no source files', async () => {
    mockApiGet.mockResolvedValue({ languages: [], total: { files: 0, code: 0, comment: 0, blank: 0 }, scannedAt: '2026-09-09T00:00:00Z' })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('当前工作区暂无源码文件')
  })

  it('shows the error message when the request fails', async () => {
    mockApiGet.mockRejectedValue({ message: '存量失败', status: 500 })
    const wrapper = await mountPanel()
    expect(wrapper.text()).toContain('存量失败')
  })

  it('shows a skeleton while the first scan is in flight', async () => {
    // The cloc scan walks the whole tree, so it is the stats load most likely
    // to be visibly slow — and it previously showed only a bare spinner.
    const pending: ((v: unknown) => void)[] = []
    mockApiGet.mockImplementation(() => new Promise(r => { pending.push(r as (v: unknown) => void) }))

    const wrapper = await mountPanel()
    expect(wrapper.find('.skeleton-stub').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('当前工作区暂无源码文件')

    for (const release of pending) {
      release({ languages: [{ name: 'Go', files: 1, code: 10, comment: 0, blank: 0 }], total: { name: 'Total', files: 1, code: 10, comment: 0, blank: 0 }, scannedAt: '2026-09-09T00:00:00Z' })
    }
    await flushPromises()
    await nextTick()
    expect(wrapper.find('.skeleton-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('Go')
  })
})
