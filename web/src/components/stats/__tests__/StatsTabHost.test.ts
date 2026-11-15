import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

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

import StatsTabHost from '@/components/stats/StatsTabHost.vue'
import { resetUsageStats, useUsageStats } from '@/composables/useUsageStats'
import { resetGitStats, useGitCodeStats } from '@/composables/useGitCodeStats'
import { localConfig } from '@/composables/useSettingsConfig'
import { readPanelState, writePanelState } from '@/utils/panelState'

const zhMessages = {
  common: { loading: '加载中' },
  nav: { refresh: '刷新' },
  gitStats: { tabUsage: '用量统计', tabCloc: '代码存量', tabDelta: '代码增量' },
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
        ClocStatsPanel: stubChild('cloc-panel'),
        GitCodeStatsPanel: stubChild('git-panel'),
        AsyncComponentLoader: { template: '<span />' },
        RefreshButton: { template: '<button class="refresh-stub" @click="$emit(\'click\')" />' },
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
    mockApiGet.mockResolvedValue({ totals: {}, rows: [], languages: [] })
  })

  it('renders three page tabs and defaults to the usage panel active', async () => {
    const wrapper = await mountHost()
    const btns = wrapper.findAll('.stats-tab').map(b => b.text())
    expect(btns).toEqual(['用量统计', '代码存量', '代码增量'])
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const cloc = wrapper.findComponent({ name: 'cloc-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(true)
    expect(cloc.props('active')).toBe(false)
    expect(git.props('active')).toBe(false)
  })

  it('switches to the cloc panel when its tab is clicked', async () => {
    const wrapper = await mountHost()
    const clocTab = wrapper.findAll('.stats-tab').find(b => b.text() === '代码存量')
    expect(clocTab).toBeTruthy()
    await clocTab!.trigger('click')
    await nextTick()
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const cloc = wrapper.findComponent({ name: 'cloc-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(false)
    expect(cloc.props('active')).toBe(true)
    expect(git.props('active')).toBe(false)
  })

  it('switches to the git delta panel when its tab is clicked', async () => {
    const wrapper = await mountHost()
    const gitTab = wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')
    expect(gitTab).toBeTruthy()
    await gitTab!.trigger('click')
    await nextTick()
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const cloc = wrapper.findComponent({ name: 'cloc-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(false)
    expect(cloc.props('active')).toBe(false)
    expect(git.props('active')).toBe(true)
  })

  it('marks the clicked tab as active (accent indicator)', async () => {
    const wrapper = await mountHost()
    let activeTabs = wrapper.findAll('.stats-tab.active')
    expect(activeTabs.map(b => b.text())).toEqual(['用量统计'])
    const gitTab = wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')
    await gitTab!.trigger('click')
    await nextTick()
    activeTabs = wrapper.findAll('.stats-tab.active')
    expect(activeTabs.map(b => b.text())).toEqual(['代码增量'])
  })

  it('passes active=false to all children when the host is inactive', async () => {
    const wrapper = await mountHost()
    await wrapper.setProps({ active: false })
    await nextTick()
    const usage = wrapper.findComponent({ name: 'usage-panel' })
    const cloc = wrapper.findComponent({ name: 'cloc-panel' })
    const git = wrapper.findComponent({ name: 'git-panel' })
    expect(usage.props('active')).toBe(false)
    expect(cloc.props('active')).toBe(false)
    expect(git.props('active')).toBe(false)
  })

  it('refresh button triggers a reload of the active panel', async () => {
    const wrapper = await mountHost()
    const countUsage = () => mockApiGet.mock.calls.filter(c => String(c[0]).includes('/api/usage/stats')).length
    const countCloc = () => mockApiGet.mock.calls.filter(c => String(c[0]).includes('/api/git/cloc')).length
    const countGit = () => mockApiGet.mock.calls.filter(c => String(c[0]).includes('/api/git/stats')).length

    const usageBefore = countUsage()
    // Default tab = usage → refresh issues another usage-stats request.
    await wrapper.find('.refresh-stub').trigger('click')
    await flushPromises()
    expect(countUsage()).toBeGreaterThan(usageBefore)

    // Switch to cloc tab → refresh issues a /api/git/cloc request.
    const clocTab = wrapper.findAll('.stats-tab').find(b => b.text() === '代码存量')
    await clocTab!.trigger('click')
    await nextTick()
    const clocBefore = countCloc()
    await wrapper.find('.refresh-stub').trigger('click')
    await flushPromises()
    expect(countCloc()).toBeGreaterThan(clocBefore)

    // Switch to git delta tab → refresh issues a /api/git/stats request.
    const gitTab = wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')
    await gitTab!.trigger('click')
    await nextTick()
    const gitBefore = countGit()
    await wrapper.find('.refresh-stub').trigger('click')
    await flushPromises()
    expect(countGit()).toBeGreaterThan(gitBefore)
  })
})

describe('StatsTabHost tab strip scrolling', () => {
  /**
   * A narrow panel cannot fit all three labels. The strip must clip its own
   * overflow (so the last tab can never paint over the refresh button or the
   * window controls) and scroll sideways. jsdom has no layout and no
   * scrollWidth, so the overflow branch is driven through a fake
   * ResizeObserver with the measured sizes stubbed on the element.
   */
  function stubStripSize(el: Element, scrollWidth: number, clientWidth: number) {
    Object.defineProperty(el, 'scrollWidth', { value: scrollWidth, configurable: true })
    Object.defineProperty(el, 'clientWidth', { value: clientWidth, configurable: true })
  }

  function stubResizeObserver() {
    const callbacks: (() => void)[] = []
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          callbacks.push(cb)
        }
        observe() {}
        disconnect() {}
      },
    )
    return { fire: () => callbacks.forEach((cb) => cb()) }
  }

  it('routes the wheel to horizontal scrolling', () => {
    // Vue compiles @wheel into an attached listener, not an onwheel attribute,
    // so this is asserted at the source; the behaviour lives in the shared
    // onHorizontalWheel util, which has its own tests.
    const src = readFileSync(
      join(__dirname, '..', 'StatsTabHost.vue'),
      'utf8',
    )
    expect(src).toContain('@wheel="onHorizontalWheel"')
    expect(src).toContain("from '@/utils/horizontalWheelScroll'")
    expect(src).toContain("from '@/utils/dragScroll'")
  })

  it('stays inert while the tabs fit (no grab cursor, no drag handler)', async () => {
    const wrapper = await mountHost()
    const strip = wrapper.find('.stats-tab-list')
    expect(strip.classes()).not.toContain('is-overflowing')
  })

  it('turns on drag scrolling only once the strip really overflows', async () => {
    const ro = stubResizeObserver()
    const wrapper = await mountHost()
    const strip = wrapper.find('.stats-tab-list')

    // Fits → inert.
    stubStripSize(strip.element, 300, 300)
    ro.fire()
    await nextTick()
    expect(wrapper.find('.stats-tab-list').classes()).not.toContain('is-overflowing')

    // Narrowed panel → overflowing → grab affordance appears.
    stubStripSize(strip.element, 420, 300)
    ro.fire()
    await nextTick()
    expect(wrapper.find('.stats-tab-list').classes()).toContain('is-overflowing')

    wrapper.unmount()
    vi.unstubAllGlobals()
  })
})

describe('StatsTabHost lazy panels', () => {
  /**
   * The three panels used to be built with a bare `defineAsyncComponent`
   * carrying only a `loadingComponent`. When the chunk 404s — the normal case
   * for an app left open across a rebuild, whose old hashed filenames are gone
   * — Vue renders the loading component FOREVER (it has no errorComponent), so
   * the stats tab spun until the app was restarted. These are source
   * assertions because the failure only exists in a real chunk-loading
   * environment: jsdom resolves the imports statically.
   */
  function readHost(): string {
    for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
      try {
        return readFileSync(join(base, 'src/components/stats/StatsTabHost.vue'), 'utf8')
      } catch {
        // try the next candidate root
      }
    }
    throw new Error('StatsTabHost.vue not found from cwd: ' + process.cwd())
  }

  it('builds every panel through buildAsyncComponentOptions (error UI + stale reload)', () => {
    const src = readHost()
    const loaders = src.match(/loader: \(\) => import\('@\/components\/stats\/[A-Za-z]+\.vue'\)/g) ?? []
    expect(loaders).toHaveLength(3)
    // Each loader must be wrapped, not handed straight to defineAsyncComponent.
    expect((src.match(/buildAsyncComponentOptions\(\{/g) ?? [])).toHaveLength(3)
    expect(src).not.toMatch(/defineAsyncComponent\(\{\s*loader:/)
  })
})

// ── 设置 → 统计: default tab / range, and 记住上次选择 ──
describe('StatsTabHost — configured defaults', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetUsageStats()
    resetGitStats()
    mockApiGet.mockResolvedValue({ totals: {}, rows: [], languages: [] })
    localConfig.statsDefaultTab = 'usage'
    localConfig.statsDefaultRange = '24h'
    localConfig.statsRememberLast = false
  })

  afterEach(() => {
    localConfig.statsDefaultTab = 'usage'
    localConfig.statsDefaultRange = '24h'
    localConfig.statsRememberLast = false
  })

  function activePanel(wrapper: Awaited<ReturnType<typeof mountHost>>): string {
    if (wrapper.findComponent({ name: 'usage-panel' }).props('active')) return 'usage'
    if (wrapper.findComponent({ name: 'cloc-panel' }).props('active')) return 'cloc'
    return 'git'
  }

  it('opens on the configured default tab', async () => {
    localConfig.statsDefaultTab = 'git'
    const wrapper = await mountHost()
    expect(activePanel(wrapper)).toBe('git')
  })

  it('returns to the default tab when the tab is reopened with 记住上次选择 off', async () => {
    localConfig.statsDefaultTab = 'usage'
    const wrapper = await mountHost()

    await wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')!.trigger('click')
    await nextTick()
    expect(activePanel(wrapper)).toBe('git')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await nextTick()
    expect(activePanel(wrapper)).toBe('usage')
  })

  it('keeps the last tab when 记住上次选择 is on', async () => {
    localConfig.statsRememberLast = true
    const wrapper = await mountHost()

    await wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')!.trigger('click')
    await nextTick()

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await nextTick()
    expect(activePanel(wrapper)).toBe('git')
  })

  it('resets both panels to the configured range on reopen when not remembering', async () => {
    localConfig.statsDefaultRange = '7d'
    const wrapper = await mountHost()
    const usage = useUsageStats()
    const git = useGitCodeStats()

    usage.setRange({ rangeKey: '30d' })
    git.setRange({ rangeKey: '30d' })
    expect(usage.filter.value.range.rangeKey).toBe('30d')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await nextTick()

    expect(usage.filter.value.range.rangeKey).toBe('7d')
    expect(git.range.value.rangeKey).toBe('7d')
  })

  it('leaves a chosen range alone when 记住上次选择 is on', async () => {
    localConfig.statsRememberLast = true
    localConfig.statsDefaultRange = '7d'
    const wrapper = await mountHost()
    const usage = useUsageStats()

    usage.setRange({ rangeKey: '30d' })
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await nextTick()

    expect(usage.filter.value.range.rangeKey).toBe('30d')
  })
})

// ── 记住上次选择: survives a reload ──
//
// The refs alone only survive the SPA session; a reload recreates the component
// and reloads the module, so "remember" has to come from storage. These tests
// write/read the same panelState entry the component uses, which is exactly
// what a reload does (fresh mount, storage intact).
describe('StatsTabHost — 记住上次选择 persistence', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetUsageStats()
    resetGitStats()
    mockApiGet.mockResolvedValue({ totals: {}, rows: [], languages: [] })
    localStorage.clear()
    localConfig.statsDefaultTab = 'usage'
    localConfig.statsRememberLast = false
  })

  afterEach(() => {
    localStorage.clear()
    localConfig.statsRememberLast = false
  })

  function activePanel(wrapper: Awaited<ReturnType<typeof mountHost>>): string {
    if (wrapper.findComponent({ name: 'usage-panel' }).props('active')) return 'usage'
    if (wrapper.findComponent({ name: 'cloc-panel' }).props('active')) return 'cloc'
    return 'git'
  }

  it('restores the stored tab on a fresh mount', async () => {
    writePanelState('statsTab', 'git')
    localConfig.statsRememberLast = true

    const wrapper = await mountHost()
    expect(activePanel(wrapper)).toBe('git')
  })

  it('persists the chosen tab', async () => {
    localConfig.statsRememberLast = true
    const wrapper = await mountHost()

    await wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')!.trigger('click')
    await nextTick()

    expect(readPanelState<string | null>('statsTab')).toBe('git')
  })

  it('ignores the stored tab and never writes when 记住上次选择 is off', async () => {
    writePanelState('statsTab', 'git')
    localConfig.statsRememberLast = false

    const wrapper = await mountHost()
    // The configured default wins over a leftover stored value.
    expect(activePanel(wrapper)).toBe('usage')

    await wrapper.findAll('.stats-tab').find(b => b.text() === '代码增量')!.trigger('click')
    await nextTick()
    // Nothing new was written: the stale 'git' is still the only record.
    expect(readPanelState<string | null>('statsTab')).toBe('git')
    // …and the user's click still works for this view.
    expect(activePanel(wrapper)).toBe('git')
  })
})
