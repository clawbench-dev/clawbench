import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, ref } from 'vue'
import { readWebFile } from '@/testUtils/readWebFile'

// Mock lucide icons. ChevronRight / CircleAlert render a real root element so
// their class bindings survive (a `render: () => null` mock swallows the
// attribute, which would make any "chevron is present" assertion vacuous).
vi.mock('lucide-vue-next', () => ({
  Sparkles: { name: 'SparklesIcon', render: () => null },
  Plus: { name: 'PlusIcon', render: () => null },
  List: { name: 'ListIcon', render: () => null },
  Play: { name: 'PlayIcon', render: () => null },
  ChevronRight: { name: 'ChevronRightIcon', template: '<i />' },
  ChevronLeft: { name: 'ChevronLeftIcon', template: '<i />' },
  CircleAlert: { name: 'CircleAlertIcon', template: '<i />' },
  Layers: { name: 'LayersIcon', template: '<i />' },
  MessageSquareText: { name: 'MessageSquareTextIcon', template: '<i />' },
  Cpu: { name: 'CpuIcon', template: '<i />' },
  Clock: { name: 'ClockIcon', template: '<i />' },
  Star: { name: 'StarIcon', template: '<i />' },
  ListTree: { name: 'ListTreeIcon', template: '<i />' },
  RefreshCw: { name: 'RefreshCwIcon', render: () => null },
  RotateCw: { name: 'RotateCw', render: () => null },
  RotateCcw: { name: 'RotateCcw', render: () => null },
  LoaderCircle: { name: 'LoaderCircleIcon', render: () => null },
}))

// Mock BottomSheet
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div><slot name="header" /><slot /></div>',
    props: ['open', 'auto', 'title'],
    emits: ['close'],
  },
}))

// Mock useMessageClusters
const mockClusters = ref<any[]>([])
const mockLoaded = ref(false)
const mockLoading = ref(false)
const mockComputing = ref(false)
const mockProgress = ref<any>({ status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 })
const mockMode = ref('')
const mockUpdatedAt = ref('')
const mockFetchClusters = vi.fn()
const mockStartCompute = vi.fn()

vi.mock('@/composables/useMessageClusters', () => ({
  useMessageClusters: () => ({
    clusters: mockClusters,
    loaded: mockLoaded,
    loading: mockLoading,
    computing: mockComputing,
    progress: mockProgress,
    mode: mockMode,
    updatedAt: mockUpdatedAt,
    fetchClusters: mockFetchClusters,
    startCompute: mockStartCompute,
    syncProgressOnce: vi.fn(),
  }),
  MessageCluster: {},
}))

// Mock QuickSendEditModal
vi.mock('@/components/chat/QuickSendEditModal.vue', () => ({
  default: {
    name: 'QuickSendEditModal',
    template: '<div class="qs-edit-modal-mock"></div>',
    props: ['open', 'editingItem', 'initialValues'],
    emits: ['close', 'saved'],
  },
}))

// Mock useToast
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({
    show: vi.fn(),
  }),
}))

// Mock useTabDrawer
const mockDrawerOpen = vi.fn()
const mockDrawerClose = vi.fn()
vi.mock('@/composables/useTabDrawer', () => ({
  useTabDrawer: () => ({
    effectiveOpen: ref(true),
    isOpen: ref(false),
    open: mockDrawerOpen,
    close: mockDrawerClose,
    toggle: vi.fn(),
  }),
}))

import MessageClustersDrawer from '@/components/chat/MessageClustersDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      chat: {
        messageClusters: {
          title: 'Message Recommendations',
          loading: 'Loading...',
          computing: 'Analyzing messages...',
          noCache: 'No analysis results yet.',
          noCacheHint: 'Analysis groups the messages you have sent by wording.',
          firstAnalyze: 'Start Analysis',
          reanalyze: 'Re-analyze',
          add: 'Add',
          variantsTitle: 'Message Variants',
          mode_fts: 'Full-text Search',
          mode_vector: 'Semantic Vector',
          mode_exact: 'Exact Match',
          modeLabel: 'Mode',
          updatedLabel: 'Updated',
          statsGroups: 'groups',
          statsMessages: 'messages',
          statsPhrasings: 'phrasings',
          variantsCount: '{count} phrasings',
          variantsHint: 'Different wordings of the same request.',
          representative: 'representative',
          unitMessages: 'msgs',
          unitPhrasings: 'phr.',
          representativeShare: '{pct}% of group',
          otherVariants: 'Other phrasings',
          error: 'Analysis failed',
          retry: 'Retry',
          phase_extracting: 'Extracting messages ({msgCount} found)',
          phase_clustering: 'Clustering messages',
          phase_saving: 'Saving results',
          recommendations: 'Recommendations',
        },
        quickSend: {
          itemSaved: 'Item saved',
        },
      },
    },
  },
})

function mountDrawer() {
  return mount(MessageClustersDrawer, {
    global: { plugins: [i18n] },
  })
}

describe('MessageClustersDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClusters.value = []
    mockLoaded.value = false
    mockLoading.value = false
    mockComputing.value = false
    mockProgress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
    mockMode.value = ''
    mockUpdatedAt.value = ''
  })

  it('renders empty state when no cache', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-empty').exists()).toBe(true)
    expect(wrapper.find('.mc-empty').text()).toContain('No analysis results yet')
    expect(wrapper.find('.mc-btn.primary').exists()).toBe(true)
  })

  it('renders computing state with progress bar', () => {
    mockComputing.value = true
    mockProgress.value = { status: 'computing', phase: 'extracting', msg_count: 10, elapsed_ms: 500, mode: 'auto', progress_pct: 10 }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-computing').exists()).toBe(true)
    expect(wrapper.find('.mc-progress-bar').exists()).toBe(true)
  })

  it('renders error state', () => {
    mockProgress.value = { status: 'error', phase: '', msg_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-error').exists()).toBe(true)
    expect(wrapper.find('.mc-error').text()).toContain('Analysis failed')
  })

  it('renders loading state', () => {
    mockLoading.value = true
    const wrapper = mountDrawer()
    // First load previews the cluster rows; the ring only remains for in-place
    // actions, not for filling an empty drawer.
    expect(wrapper.find('.panel-skeleton').exists()).toBe(true)
    expect(wrapper.find('.li-spinner').exists()).toBe(false)
  })

  it('renders cached results with clusters and header reanalyze button', () => {
    mockLoaded.value = true
    mockClusters.value = [
      { id: 1, representative: 'Fix the bug', variants: ['fix bug', 'bug fix'], total_count: 5, representative_count: 3 },
    ]
    mockMode.value = 'auto'
    mockUpdatedAt.value = '2026-08-01'
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-results').exists()).toBe(true)
    expect(wrapper.find('.mc-cluster-item').exists()).toBe(true)
    expect(wrapper.find('.mc-cluster-representative').text()).toContain('Fix the bug')
    // Reanalyze button is in header, icon-only
    expect(wrapper.find('.mc-header-btn').exists()).toBe(true)
    // No reanalyze button in results area
    expect(wrapper.find('.mc-results-header .mc-btn').exists()).toBe(false)
  })

  it('header reanalyze button is hidden when computing', () => {
    mockComputing.value = true
    mockProgress.value = { status: 'computing', phase: 'extracting', msg_count: 10, elapsed_ms: 500, mode: 'auto', progress_pct: 10 }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-header-btn').exists()).toBe(false)
  })

  it('header reanalyze button is hidden in error state', () => {
    mockProgress.value = { status: 'error', phase: '', msg_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-header-btn').exists()).toBe(false)
  })

  it('header reanalyze button is hidden when no cache', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-header-btn').exists()).toBe(false)
  })

  it('clicking header reanalyze button calls startCompute', async () => {
    mockLoaded.value = true
    mockClusters.value = [
      { id: 1, representative: 'Fix the bug', variants: ['fix bug', 'bug fix'], total_count: 5, representative_count: 3 },
    ]
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
    const wrapper = mountDrawer()
    await wrapper.find('.mc-header-btn').trigger('click')
    expect(mockStartCompute).toHaveBeenCalledOnce()
  })

  it('calls startCompute when "Start Analysis" button is clicked', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-btn.primary').trigger('click')
    expect(mockStartCompute).toHaveBeenCalledOnce()
  })

  it('calls startCompute when "Retry" button is clicked', async () => {
    mockProgress.value = { status: 'error', phase: '', msg_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
    const wrapper = mountDrawer()
    await wrapper.find('.mc-btn').trigger('click')
    expect(mockStartCompute).toHaveBeenCalledOnce()
  })

  it('open() sets visible and calls fetchClusters', async () => {
    mockFetchClusters.mockResolvedValue(undefined)
    const wrapper = mountDrawer()
    await wrapper.vm.open()
    expect(mockFetchClusters).toHaveBeenCalledOnce()
  })
})

/**
 * Summary strip + row chrome.
 *
 * The list used to be a bare column of one-line rows under a single muted
 * status sentence, which is what made the panel read as "thin". The figures
 * below are the added substance — and each is derived, so a wrong computation
 * is invisible in a text-only assertion. These pin the derivation, not just the
 * presence of a class.
 */
describe('MessageClustersDrawer — summary strip', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockLoaded.value = true
    mockLoading.value = false
    mockComputing.value = false
    mockMode.value = 'auto'
    mockUpdatedAt.value = new Date().toISOString()
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
  })

  it('shows the number of clusters and the SUM of their message counts', () => {
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['a'], total_count: 5, representative_count: 3 },
      { id: 2, representative: 'B', variants: ['b', 'b2'], total_count: 7, representative_count: 4 },
      { id: 3, representative: 'C', variants: ['c'], total_count: 11, representative_count: 6 },
    ]
    const wrapper = mountDrawer()
    const values = wrapper.findAll('.mc-summary .mc-figure b').map((n) => n.text())
    // 3 groups, 5+7+11 = 23 messages. A per-row count alone never showed 23.
    expect(values).toEqual(['3', '23'])
  })

  it('omits the mode/freshness line when the server reports neither', () => {
    mockClusters.value = [{ id: 1, representative: 'A', variants: ['a'], total_count: 5, representative_count: 3 }]
    mockMode.value = ''
    mockUpdatedAt.value = ''
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-summary-meta').text()).toBe('')
  })

  it('renders the localized mode label, not the raw backend token', () => {
    mockClusters.value = [{ id: 1, representative: 'A', variants: ['a'], total_count: 5, representative_count: 3 }]
    mockMode.value = 'vector'
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-summary-meta').text()).toContain('Semantic Vector')
    expect(wrapper.find('.mc-summary-meta').text()).not.toContain('vector')
  })
})

describe('MessageClustersDrawer — cluster rows', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockLoaded.value = true
    mockLoading.value = false
    mockComputing.value = false
    mockMode.value = 'auto'
    mockUpdatedAt.value = ''
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
  })

  it('states how many phrasings collapsed into the cluster', () => {
    // The meta line always carries the cluster's MESSAGE count; the phrasing
    // count is the extra fact that tells the two numbers apart and hints that
    // the card expands.
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['a1', 'a2', 'a3'], total_count: 9, representative_count: 4 },
    ]
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-cluster-variants').text()).toContain('3')
    // The message count renders as number + tiny unit; assert the number.
    expect(wrapper.find('.mc-cluster-count').text()).toContain('9')
  })

  it('hides the phrasing count when there is only one', () => {
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['a'], total_count: 9, representative_count: 4 },
    ]
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-cluster-variants').exists()).toBe(false)
    // The message count is not part of the phrasing line, so it survives.
    expect(wrapper.find('.mc-cluster-count').text()).toContain('9')
  })

  it('puts the phrasing count BEFORE the message count', () => {
    // Reading order is "4 种 · 42 条": the variety is what the row is about,
    // the volume is the supporting figure.
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['A', 'a2', 'a3'], total_count: 9, representative_count: 4 },
    ]
    const wrapper = mountDrawer()
    const sub = wrapper.find('.mc-cluster-sub')
    const order = [...sub.element.children].map((el) => el.className)
    expect(order[0]).toContain('mc-cluster-variants')
    expect(order[1]).toContain('mc-cluster-count')
  })

  it('drops the separator when there is no phrasing count to separate', () => {
    // The divider is attached to the count's leading edge, so a single-phrasing
    // row must not render a dangling rule.
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['A'], total_count: 9, representative_count: 9 },
    ]
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-cluster-variants').exists()).toBe(false)
    // Class is still present (CSS decides the rule via the sibling selector),
    // so assert the structure that the selector depends on instead.
    const sub = wrapper.find('.mc-cluster-sub')
    expect([...sub.element.children].map((el) => el.className)).toEqual(['mc-cluster-count'])
  })

  it('renders each row as a real button so it is keyboard reachable', () => {
    // It was a <div> with @click: unreachable by Tab, and announced as nothing.
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['a1', 'a2'], total_count: 9, representative_count: 4 },
    ]
    const wrapper = mountDrawer()
    const row = wrapper.find('.mc-cluster-item')
    expect(row.element.tagName).toBe('BUTTON')
    expect(row.attributes('type')).toBe('button')
  })

  it('disables the row when there is nothing to expand', () => {
    mockClusters.value = [
      { id: 1, representative: 'A', variants: [], total_count: 9, representative_count: 4 },
    ]
    const wrapper = mountDrawer()
    const row = wrapper.find('.mc-cluster-item')
    expect(row.attributes('disabled')).toBeDefined()
    expect(row.classes()).toContain('is-static')
    // No chevron: it would promise an expansion that cannot happen.
    expect(wrapper.find('.mc-cluster-chevron').exists()).toBe(false)
  })

  it('shows the chevron only on expandable rows', () => {
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['a1', 'a2'], total_count: 9, representative_count: 4 },
      { id: 2, representative: 'B', variants: [], total_count: 4, representative_count: 2 },
    ]
    const wrapper = mountDrawer()
    expect(wrapper.findAll('.mc-cluster-chevron')).toHaveLength(1)
  })
})

/**
 * Drill-down page for one cluster's variants.
 *
 * The variants used to be a nested ModalDialog. That stacked a second sheet on
 * top of the list you were reading and gave the sheet two close affordances.
 * They are now a second PAGE of the same sheet (the pattern SessionSearchDrawer
 * and GitHistoryDrawer already use): the header swaps to a back button, the body
 * swaps wholesale, and Back pops one level before closing.
 */
describe('MessageClustersDrawer — variants drill-down', () => {
  const cluster = {
    id: 1,
    representative: 'Fix the bug',
    variants: ['Fix the bug', 'fix bug', 'bug fix'],
    total_count: 12,
    representative_count: 5,
  }

  beforeEach(() => {
    vi.clearAllMocks()
    mockLoaded.value = true
    mockLoading.value = false
    mockComputing.value = false
    mockMode.value = 'auto'
    mockUpdatedAt.value = ''
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
    mockClusters.value = [cluster]
  })

  it('opens the variants page instead of a nested dialog', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    // The list is REPLACED, not covered: no list content remains on screen.
    expect(wrapper.find('.mc-cluster-grid').exists()).toBe(false)
    expect(wrapper.find('.mc-variant-list').exists()).toBe(true)
    // No modal was mounted at all.
    expect(wrapper.find('.modal-dialog-mock').exists()).toBe(false)
  })

  it('swaps the header to a back button showing the cluster title', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    expect(wrapper.find('.mc-back-btn').exists()).toBe(true)
    // The list's own header action (re-analyze) belongs to the list page only.
    expect(wrapper.find('.mc-header-btn').exists()).toBe(false)
    expect(wrapper.find('.mc-detail-title').text()).toContain('Fix the bug')
  })

  it('lists every variant exactly once, numbered across both sections', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    // 3 variants: representative + 2 others, no duplication.
    expect(wrapper.findAll('.mc-variant-row')).toHaveLength(3)
    expect(wrapper.findAll('.mc-variant-text').map((n) => n.text())).toEqual([
      'Fix the bug', 'fix bug', 'bug fix',
    ])
    // Numbering continues across the section boundary.
    expect(wrapper.findAll('.mc-variant-index').map((n) => n.text())).toEqual(['01', '02', '03'])
  })

  it('splits the list into a representative section and an "others" section', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    const sections = wrapper.findAll('.mc-variant-section')
    expect(sections).toHaveLength(2)
    // The representative section holds exactly one row, flagged with the rail.
    expect(sections[0].findAll('.mc-variant-row')).toHaveLength(1)
    expect(sections[0].find('.mc-variant-row').classes()).toContain('is-representative')
    // The others section holds the rest and states how many.
    expect(sections[1].findAll('.mc-variant-row')).toHaveLength(2)
    // The count carries its unit — a bare "2" next to "占 50%" reads as a
    // mystery number rather than "2 phrasings".
    expect(sections[1].find('.mc-variant-section-meta').text()).toContain('2')
    expect(sections[1].find('.mc-variant-section-meta').text()).toContain('phrasings')
  })

  it('omits the "others" section when the cluster has one phrasing', async () => {
    mockClusters.value = [
      { id: 1, representative: 'Solo', variants: ['Solo'], total_count: 5, representative_count: 5 },
    ]
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    expect(wrapper.findAll('.mc-variant-section')).toHaveLength(1)
    expect(wrapper.findAll('.mc-variant-row')).toHaveLength(1)
  })

  it('reports the phrasing count in the detail bar', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    const figures = wrapper.findAll('.mc-detail-bar .mc-figure b').map((n) => n.text())
    // messages, phrasings
    expect(figures).toEqual(['12', '3'])
  })

  it('adds a variant to quick send using the cluster as the label', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    await wrapper.findAll('.mc-variant-row')[1].find('.mc-btn.add').trigger('click')
    expect(wrapper.vm.quickSendEditOpen).toBe(true)
    // Label comes from the cluster, not from the variant being added.
    expect(wrapper.vm.quickSendInitialValues).toEqual({ label: 'Fix the bug', command: 'fix bug' })
  })

  it('goes back to the list without closing the sheet', async () => {
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    await wrapper.find('.mc-back-btn').trigger('click')
    expect(wrapper.find('.mc-variant-list').exists()).toBe(false)
    expect(wrapper.find('.mc-cluster-grid').exists()).toBe(true)
    // The sheet itself was never asked to close.
    expect(wrapper.emitted('close')).toBeFalsy()
  })

  it('does not drill into a cluster with no variants', async () => {
    const empty = { id: 2, representative: 'B', variants: [], total_count: 4, representative_count: 2 }
    mockClusters.value = [empty]
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-cluster-item').attributes('disabled')).toBeDefined()

    // Call the guard directly. Triggering a click on a disabled <button> is
    // swallowed by the DOM, so a click-based assertion here would pass no
    // matter what openDrill() does — the guard is what needs pinning.
    wrapper.vm.openDrill(empty)
    // The assertion must come after the re-render, or it reads the list DOM
    // that was there before the call and passes either way.
    await nextTick()
    expect(wrapper.find('.mc-variant-list').exists()).toBe(false)
    expect(wrapper.find('.mc-cluster-grid').exists()).toBe(true)
  })
})

/**
 * Visual hierarchy: rank, frequency meter, per-fact glyphs.
 *
 * Every one of these encodes a NUMBER, so a decorative-looking assertion is not
 * enough — the meter's width and the top-three flag are both derived and both
 * silently wrong if the derivation is wrong.
 */
describe('MessageClustersDrawer — hierarchy and glyphs', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockLoaded.value = true
    mockLoading.value = false
    mockComputing.value = false
    mockMode.value = 'auto'
    mockUpdatedAt.value = ''
    mockProgress.value = { status: 'done', phase: '', msg_count: 0, elapsed_ms: 0, mode: 'auto', progress_pct: 100 }
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['A', 'a2'], total_count: 40, representative_count: 20 },
      { id: 2, representative: 'B', variants: ['B', 'b2'], total_count: 20, representative_count: 5 },
      { id: 3, representative: 'C', variants: ['C', 'c2'], total_count: 10, representative_count: 2 },
      { id: 4, representative: 'D', variants: ['D', 'd2'], total_count: 5, representative_count: 1 },
    ]
  })

  it('numbers the tiles in display order', () => {
    const wrapper = mountDrawer()
    expect(wrapper.findAll('.mc-cluster-rank').map((n) => n.text())).toEqual(['01', '02', '03', '04'])
  })

  it('sizes the frequency meter against the LARGEST cluster', () => {
    const wrapper = mountDrawer()
    const widths = wrapper.findAll('.mc-cluster-meter-fill').map((n) => (n.element as HTMLElement).style.width)
    // 40 is the max → 100%; 20 → 50%; 10 → 25%; 5 → 13% (rounded).
    expect(widths).toEqual(['100%', '50%', '25%', '13%'])
  })

  it('marks only the top three as top-ranked', () => {
    const wrapper = mountDrawer()
    const flagged = wrapper.findAll('.mc-cluster-item').filter((n) => n.classes().includes('is-top'))
    expect(flagged).toHaveLength(3)
    // The 4th tile is not flagged — the boundary is real, not "all of them".
    expect(wrapper.findAll('.mc-cluster-item')[3].classes()).not.toContain('is-top')
  })

  it('gives each summary figure its own glyph', () => {
    // Without distinct glyphs the strip is one undifferentiated run of numbers.
    const wrapper = mountDrawer()
    const icons = wrapper.findAll('.mc-summary .mc-figure-icon')
    expect(icons).toHaveLength(2)
    expect(icons[0].classes()).toContain('mc-figure-icon')
    // Mode and freshness are separately iconed too.
    expect(wrapper.findAll('.mc-summary-meta .mc-meta-icon').length).toBeGreaterThan(0)
  })

  it('separates the message count from its unit typographically', () => {
    const wrapper = mountDrawer()
    const count = wrapper.find('.mc-cluster-count')
    // number and unit are distinct nodes, so the unit can be small and muted.
    expect(count.find('i').exists()).toBe(true)
    expect(count.find('i').text().length).toBeGreaterThan(0)
  })

  it('shows the representative share only where a count exists', async () => {
    const wrapper = mountDrawer()
    // Cluster A: 20 of 40 → 50%. (First tile by display order.)
    await wrapper.find('.mc-cluster-item').trigger('click')
    // The FIRST section's meta is the representative's share; the second
    // section's meta is its own row count.
    const repSection = wrapper.findAll('.mc-variant-section')[0]
    expect(repSection.find('.mc-variant-section-meta').text()).toContain('50')
  })

  it('omits the share rather than showing 0% when the count is missing', async () => {
    mockClusters.value = [
      { id: 1, representative: 'A', variants: ['A', 'a2'], total_count: 10, representative_count: 0 },
    ]
    const wrapper = mountDrawer()
    await wrapper.find('.mc-cluster-item').trigger('click')
    // The section still renders; only the percentage is withheld.
    const repSection = wrapper.findAll('.mc-variant-section')[0]
    expect(repSection.find('.mc-variant-section-head').exists()).toBe(true)
    expect(repSection.find('.mc-variant-section-meta').exists()).toBe(false)
  })
})

/**
 * Type consistency of the two counts.
 *
 * jsdom has no CSS engine, so a rendered assertion cannot see a font-size. This
 * is a source contract instead: the phrasing count must declare the SAME
 * family / size / weight / colour as the message count, because a headline
 * number next to a footnote number makes the pair read as two different kinds
 * of fact when they are the same kind.
 */
describe('MessageClustersDrawer — count typography', () => {
  const SRC = readWebFile('src/components/chat/MessageClustersDrawer.vue')

  /** The declaration block of a single-class rule, comments stripped. */
  function rule(sel: string): string {
    const clean = SRC.replace(/\/\*[\s\S]*?\*\//g, '')
    const m = clean.match(new RegExp(`\\n${sel.replace('.', '\\.')} \\{([^}]*)\\}`))
    expect(m, `rule ${sel} must exist`).not.toBeNull()
    return m![1]
  }

  it.each([
    ['font-family', 'var(--font-mono)'],
    ['font-size', 'var(--font-size-sm)'],
    ['font-weight', 'var(--font-weight-bold)'],
    ['color', 'var(--text-primary)'],
  ])('phrasing count matches the message count on %s', (prop, value) => {
    expect(rule('.mc-cluster-count')).toContain(`${prop}: ${value}`)
    expect(rule('.mc-cluster-variants')).toContain(`${prop}: ${value}`)
  })

  it('keeps both units small and muted, so the numbers lead', () => {
    // Each unit is a nested <i>; the rule must not inherit the bold figure size.
    expect(rule('.mc-cluster-count i')).toContain('var(--font-size-2xs)')
    expect(rule('.mc-cluster-variants i')).toContain('var(--font-size-2xs)')
  })
})

describe('MessageClustersDrawer — empty state', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockClusters.value = []
    mockLoaded.value = false
    mockLoading.value = false
    mockComputing.value = false
    mockMode.value = ''
    mockUpdatedAt.value = ''
    mockProgress.value = { status: 'idle', phase: '', msg_count: 0, cluster_count: 0, elapsed_ms: 0, mode: '', progress_pct: 0 }
  })

  it('explains what analysis does instead of only stating that nothing happened', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.mc-empty-title').text()).toContain('No analysis results yet')
    expect(wrapper.find('.mc-empty-hint').text().length).toBeGreaterThan(0)
    expect(wrapper.find('.mc-empty-icon').exists()).toBe(true)
  })
})
