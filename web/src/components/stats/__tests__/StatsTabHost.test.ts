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

import StatsTabHost from '@/components/stats/StatsTabHost.vue'
import { resetUsageStats } from '@/composables/useUsageStats'
import { resetGitStats } from '@/composables/useGitCodeStats'

const zhMessages = {
  common: { loading: '加载中' },
  gitStats: { tabUsage: '用量统计', tabCode: '代码统计' },
  stats: { rangeTitle: 'x' },
}

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    messages: { zh: zhMessages },
  })
}

function stubChild(name: string) {
  return {
    name,
    template: `<div class="${name}-stub" />`,
    props: ['active'],
  }
}

async function mountHost() {
  const wrapper = shallowMount(StatsTabHost, {
    props: { active: true },
    global: {
      plugins: [makeI18n()],
      stubs: {
        UsageStatsPanel: stubChild('usage-panel'),
        GitCodeStatsPanel: stubChild('git-panel'),
        AsyncComponentLoader: { template: '<span />' },
      },
    },
  })
  await nextTick()
  await flushPromises()
  return wrapper
}

describe('StatsTabHost', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetUsageStats()
    resetGitStats()
    mockApiGet.mockResolvedValue({ totals: {}, rows: [] })
  })

  it('renders both sub-tabs and defaults to the usage panel active', async () => {
    const wrapper = await mountHost()
    const btns = wrapper.findAll('.stats-subtab').map(b => b.text())
    expect(btns).toEqual(['用量统计', '代码统计'])
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(true)
    expect(git.props('active')).toBe(false)
  })

  it('switches to the code panel when its tab is clicked', async () => {
    const wrapper = await mountHost()
    const codeTab = wrapper.findAll('.stats-subtab').find(b => b.text() === '代码统计')
    expect(codeTab).toBeTruthy()
    await codeTab!.trigger('click')
    await nextTick()
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(false)
    expect(git.props('active')).toBe(true)
  })

  it('passes active=false to both children when the host is inactive', async () => {
    const wrapper = await mountHost()
    await wrapper.setProps({ active: false })
    await nextTick()
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(false)
    expect(git.props('active')).toBe(false)
  })
})
