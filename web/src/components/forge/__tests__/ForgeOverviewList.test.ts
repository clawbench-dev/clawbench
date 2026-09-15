import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const mockFetchForgeUnreadItems = vi.fn()
const mockMarkForgeRead = vi.fn()

vi.mock('@/utils/forgeApi', async () => {
  const actual = await vi.importActual<typeof import('@/utils/forgeApi')>('@/utils/forgeApi')
  return {
    ...actual,
    fetchForgeUnreadItems: (...a: unknown[]) => mockFetchForgeUnreadItems(...a),
    markForgeRead: (...a: unknown[]) => mockMarkForgeRead(...a),
  }
})

vi.mock('@/composables/useForgeUnread', () => ({
  useForgeUnread: () => ({
    forgeUnreadCount: { value: 0 },
    refresh: vi.fn(),
    onForgeEvent: vi.fn(),
    markAllRead: () => mockMarkForgeRead(),
  }),
}))

// vue-i18n resolves through the app singleton; echo the key so assertions read
// as mappings rather than translations.
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key: string, params?: Record<string, unknown>) =>
        params && 'runId' in params ? `${key}:${params.runId}` : key,
      locale: { value: 'en' },
    },
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params && 'runId' in params ? `${key}:${params.runId}` : key,
  }),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Must cover every icon the panel's transitive imports touch — RefreshButton
// pulls in the four rotate/check icons, and a missing export throws at mount.
vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, props: { size: Number }, template: '<svg />' })
  return {
    AlertCircle: stub('AlertCircle'),
    CheckCheck: stub('CheckCheck'),
    ChevronRight: stub('ChevronRight'),
    Inbox: stub('Inbox'),
    Rss: stub('Rss'),
    RefreshCw: stub('RefreshCw'),
    RotateCw: stub('RotateCw'),
    RotateCcw: stub('RotateCcw'),
    CheckCircle2: stub('CheckCircle2'),
  }
})

import ForgeOverviewList from '@/components/forge/ForgeOverviewList.vue'

function row(itemKey: string, overrides: Record<string, unknown> = {}) {
  return {
    itemKey,
    type: 'pr',
    number: 455,
    runId: 0,
    eventType: 'commented',
    eventCount: 1,
    url: 'https://example.com/x',
    slug: 'acme/widgets',
    updatedAt: '2026-09-14T12:00:00Z',
    ...overrides,
  }
}

function mountPanel(projectPath = '/proj') {
  return mount(ForgeOverviewList, {
    props: { active: true, projectPath },
  })
}

describe('ForgeOverviewList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockFetchForgeUnreadItems.mockResolvedValue({ count: 0, items: [] })
  })

  it('loads on activation and renders one row per unread item', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 2,
      items: [row('pr/455'), row('pr/456', { number: 456 })],
    })
    const w = mountPanel()
    await nextTick()
    await nextTick()

    expect(mockFetchForgeUnreadItems).toHaveBeenCalled()
    expect(w.findAll('.forge-overview-row')).toHaveLength(2)
  })

  it('does not load while inactive', async () => {
    mount(ForgeOverviewList, { props: { active: false, projectPath: '/proj' } })
    await nextTick()

    expect(mockFetchForgeUnreadItems).not.toHaveBeenCalled()
  })

  it('shows the empty state when nothing is unread', async () => {
    const w = mountPanel()
    await nextTick()
    await nextTick()

    expect(w.text()).toContain('forge.overview.empty')
    expect(w.findAll('.forge-overview-row')).toHaveLength(0)
  })

  it('uses the SAME glyph in its empty state as the tab shows', async () => {
    // The empty state must look like the tab it belongs to. The tab uses Rss
    // (see the forgeTabs registry); this asserts the list agrees, so the two
    // cannot drift apart.
    //
    // Asserted via the component name rather than a lucide CSS class: this file
    // stubs lucide, so the stub renders a bare <svg> with no class of its own.
    const w = mountPanel()
    await nextTick()
    await nextTick()

    const icon = w.findComponent({ name: 'Rss' })
    expect(icon.exists(), 'empty state must render the Rss glyph').toBe(true)
  })

  it('labels a pipeline row by run id, never #0', async () => {
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 1,
      items: [row('pipeline/run:555', { type: 'pipeline', number: 0, runId: 555, eventType: 'pipeline_done' })],
    })
    const w = mountPanel()
    await nextTick()
    await nextTick()

    const text = w.text()
    expect(text).toContain('555')
    expect(text).not.toContain('#0')
  })

  it('emits the item identity AND passes the itemKey through untouched', async () => {
    // The pipeline key is the mutation-sensitive part: a client that rebuilt the
    // key from type+number would send "pipeline/0" and mark nothing read.
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 1,
      items: [row('pipeline/run:555', { type: 'pipeline', number: 0, runId: 555, eventType: 'pipeline_done' })],
    })
    const w = mountPanel()
    await nextTick()
    await nextTick()

    await w.find('.forge-overview-row').trigger('click')

    const emitted = w.emitted('open-item')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toEqual({
      type: 'pipeline',
      number: 0,
      runId: 555,
      itemKey: 'pipeline/run:555',
    })
  })

  it('greys the clicked row out instead of removing it', async () => {
    // Removing it would shift every row below the cursor mid-click.
    mockFetchForgeUnreadItems.mockResolvedValue({
      count: 2,
      items: [row('pr/1'), row('pr/2', { number: 2 })],
    })
    const w = mountPanel()
    await nextTick()
    await nextTick()
    expect(w.findAll('.forge-overview-row')).toHaveLength(2)

    await w.findAll('.forge-overview-row')[0].trigger('click')
    await nextTick()

    const rows = w.findAll('.forge-overview-row')
    expect(rows).toHaveLength(2, 'the row must stay in the list')
    expect(rows[0].classes()).not.toContain('unread')
    expect(rows[1].classes()).toContain('unread')
  })

  it('clearLocal empties the rows without a request', async () => {
    // The host's "mark all read" already performed the repo-wide write through
    // the shared badge composable; this must not issue a second POST.
    mockFetchForgeUnreadItems.mockResolvedValue({ count: 1, items: [row('pr/1')] })
    const w = mountPanel()
    await nextTick()
    await nextTick()
    expect(w.findAll('.forge-overview-row')).toHaveLength(1)

    w.vm.clearLocal()
    await nextTick()

    expect(w.findAll('.forge-overview-row')).toHaveLength(0)
    expect(mockMarkForgeRead).not.toHaveBeenCalled()
  })

  it('shows the error card on a failed load rather than the empty state', async () => {
    const { ForgeApiError } = await import('@/utils/forgeApi')
    mockFetchForgeUnreadItems.mockRejectedValue(new ForgeApiError('nope', 'ForgeNetworkError'))
    const w = mountPanel()
    await nextTick()
    await nextTick()

    expect(w.find('.forge-error-card').exists()).toBe(true)
    expect(w.text()).not.toContain('forge.overview.empty')
  })
})
