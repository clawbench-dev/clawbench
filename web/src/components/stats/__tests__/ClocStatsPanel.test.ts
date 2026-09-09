import { describe, expect, it, vi, beforeEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
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
    // Total row values.
    const rows = wrapper.findAll('.stats-table tbody tr')
    expect(rows.length).toBe(4) // 3 languages + total
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
})
