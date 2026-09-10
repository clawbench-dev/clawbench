import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import SessionSearchDrawer from '@/components/session/SessionSearchDrawer.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => {
    const map: Record<string, string> = {
      'sessionSearch.title': 'Search Sessions',
      'sessionSearch.placeholder': 'Search...',
      'sessionSearch.noQuery': 'Enter a query',
      'sessionSearch.searching': 'Searching...',
      'sessionSearch.noResults': 'No results',
      'sessionSearch.resultCount': `${params?.count ?? 0} results`,
      'sessionSearch.untitledSession': 'Untitled',
      'sessionSearch.archived': 'Archived',
      'sessionSearch.chunks': `${params?.count ?? 0} chunks`,
      'sessionSearch.roleUser': 'User',
      'sessionSearch.roleAssistant': 'Assistant',
      'sessionSearch.resume': 'Resume Session',
      'sessionSearch.destroy': 'Remove',
      'sessionSearch.openSession': 'Open',
      'sessionSearch.modeHybrid': 'Hybrid',
      'sessionSearch.modeFts': 'Full-text',
      'sessionSearch.modeLabel': 'Search Mode',
      'sessionSearch.filterArchive': 'Status',
      'sessionSearch.archiveAll': 'All',
      'sessionSearch.archiveActive': 'Active',
      'sessionSearch.archiveArchived': 'Archived',
      'sessionSearch.sortLabel': 'Sort',
      'sessionSearch.sortRelevance': 'Relevance',
      'sessionSearch.sortNewest': 'Newest',
      'sessionSearch.sortOldest': 'Oldest',
      'sessionSearch.timeAll': 'Any time',
      'sessionSearch.timeToday': 'Today',
      'sessionSearch.time7d': 'Last 7 days',
      'sessionSearch.time30d': 'Last 30 days',
      'sessionSearch.timeCustom': 'Custom',
      'sessionSearch.noPreview': 'No messages in this session',
      'sessionSearch.loadingPreview': 'Loading preview...',
      'sessionSearch.loadingMore': 'Loading more...',
      'sessionSearch.noMore': 'No more sessions',
    }
    return map[key] ?? key
  }}),
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/utils/format', () => ({
  formatRelativeTime: (d: string) => d || 'now',
}))

vi.mock('@/utils/searchUtils', () => ({
  highlightTextByPositions: (text: string, positions: { start: number; end: number }[]) => {
    if (!positions || positions.length === 0) return text
    return text + '<mark>highlighted</mark>'
  },
}))

vi.mock('@/utils/html.ts', () => ({
  escapeHtml: (text: string) => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'),
}))

vi.mock('@/composables/useMarkdownRenderer.ts', () => ({
  renderMarkdownHtml: (text: string) => `<p>${text}</p>`,
}))

vi.mock('@/composables/useBackHandler', () => ({
  registerBackHandler: () => vi.fn(),
  PRIORITY_OVERLAY: 1000,
}))

const mockClear = vi.fn()
const mockSetQuery = vi.fn()
const mockBrowse = vi.fn()
const mockSetFilters = vi.fn()
const mockLoadMore = vi.fn()
const mockSearchState = vi.fn()
const mockFetchFirstMessage = vi.fn()

vi.mock('@/composables/useSessionSearch', () => ({
  useSessionSearch: () => ({
    state: mockSearchState(),
    setQuery: mockSetQuery,
    browse: mockBrowse,
    clear: mockClear,
    setFilters: mockSetFilters,
    loadMore: mockLoadMore,
  }),
  fetchSessionFirstMessage: (...args: unknown[]) => mockFetchFirstMessage(...args),
}))

// Stub child components
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: {
    name: 'BottomSheet',
    template: '<div class="bottom-sheet-stub"><slot name="header" /><slot /><slot name="footer" /></div>',
  },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: {
    name: 'SearchInput',
    template: '<div class="search-input-stub" />',
    methods: { focus: vi.fn() },
  },
}))

// PopupMenu teleports to body; stub it to render its slot inline so menu items
// are queryable within the wrapper.
vi.mock('@/components/common/PopupMenu.vue', () => ({
  default: {
    name: 'PopupMenu',
    props: ['show', 'targetElement', 'maxWidth', 'menuItemsCount', 'anchor'],
    template: '<div v-if="show" class="popup-menu-stub"><slot /></div>',
  },
}))

// jsdom does not implement IntersectionObserver; the browse list uses one for
// infinite scroll. Stub it so mounting does not throw.
class MockIntersectionObserver {
  callback: any
  constructor(cb: any) { this.callback = cb }
  observe() {}
  disconnect() {}
  unobserve() {}
}
vi.stubGlobal('IntersectionObserver', MockIntersectionObserver)

function createState(overrides = {}) {
  return {
    query: '',
    results: [],
    total: 0,
    loading: false,
    error: null as string | null,
    searchMode: '',
    preferMode: 'hybrid' as const,
    archivedFilter: 'all' as const,
    sortOrder: 'relevance' as const,
    timeRange: 'all' as const,
    customFrom: '',
    customTo: '',
    hasMore: false,
    loadingMore: false,
    ...overrides,
  }
}

const sampleResult = {
  session_id: 's1',
  session_title: 'My Session',
  score: 0.9,
  backend: 'cli',
  project_path: '/tmp',
  archived: false,
  created_at: '2025-01-01',
  match_count: 3,
  chunks: [{
    chunk_id: 1,
    chunk_text: 'some matching text here',
    match_positions: [{ start: 5, end: 13 }],
    score: 0.9,
    role: 'user',
    message_id: 1,
    created_at: '2025-01-01',
  }],
}

function mountDrawer(props = {}) {
  return mount(SessionSearchDrawer, {
    props: {
      open: true,
      ...props,
    },
  })
}

describe('SessionSearchDrawer', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSearchState.mockReturnValue(createState())
  })

  it('renders when open', () => {
    const wrapper = mountDrawer()
    expect(wrapper.find('.bottom-sheet-stub').exists()).toBe(true)
    expect(wrapper.find('.session-search-body').exists()).toBe(true)
  })

  it('emits open-acp-sessions when the ACP resume button is clicked', async () => {
    const wrapper = mountDrawer()
    const btn = wrapper.find('.acp-resume-header-btn')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')
    expect(wrapper.emitted('open-acp-sessions')).toBeTruthy()
  })

  it('lists browsed sessions when query is empty', () => {
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], total: 1 }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-item').exists()).toBe(true)
    expect(wrapper.find('.session-search-item-title').text()).toBe('My Session')
    // No "enter a query" placeholder blocks the browse list.
    expect(wrapper.find('.session-search-empty').exists()).toBe(false)
  })

  it('shows searching state', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', loading: true }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.loading-indicator').exists()).toBe(true)
    expect(wrapper.find('.li-spinner').exists()).toBe(true)
    expect(wrapper.find('.loading-indicator').text()).toContain('Searching...')
  })

  it('shows error state', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', error: 'Something went wrong' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-error').text()).toContain('Something went wrong')
  })

  it('shows no results message', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [] }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-empty').text()).toContain('No results')
  })

  it('shows search results', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-results').exists()).toBe(true)
    expect(wrapper.find('.session-search-item').exists()).toBe(true)
    expect(wrapper.find('.session-search-item-title').text()).toBe('My Session')
    expect(wrapper.find('.session-search-item-chunks').text()).toContain('3 chunks')
  })

  it('shows untitled session fallback', () => {
    mockSearchState.mockReturnValue(createState({
      query: 'test',
      results: [{
        session_id: 's2',
        session_title: '',
        score: 0.5,
        backend: '',
        project_path: '',
        archived: false,
        created_at: '2025-01-01',
        match_count: 1,
        chunks: [],
      }],
    }))

    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-item-title').text()).toBe('Untitled')
  })

  it('shows archived badge', () => {
    mockSearchState.mockReturnValue(createState({
      query: 'test',
      results: [{
        session_id: 's3',
        session_title: 'Archived Session',
        score: 0.5,
        backend: 'cli',
        project_path: '',
        archived: true,
        created_at: '2025-01-01',
        match_count: 1,
        chunks: [],
      }],
    }))

    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-item-archived').exists()).toBe(true)
    expect(wrapper.find('.session-search-item-archived').text()).toBe('Archived')
  })

  it('drills down to detail view when clicking a result', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()

    // Set selectedSession via internal ref and force re-render.
    // Due to monorepo dual-reactivity-module issue, Composition API ref changes
    // don't trigger Vue's scheduler in this test environment. We access the raw
    // ref through setupState and call update() manually.
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = sampleResult
    instance.update()
    await flushPromises()

    // Should show detail view
    expect(wrapper.find('.detail-page').exists()).toBe(true)
    expect(wrapper.find('.detail-chunk').exists()).toBe(true)
    expect(wrapper.find('.fbtn-primary').exists()).toBe(true)
    // Search results list should be hidden
    expect(wrapper.find('.session-search-body').exists()).toBe(false)
  })

  it('lazily fetches the first message when drilling into a browse result', async () => {
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], searchMode: 'recent' }))
    mockFetchFirstMessage.mockResolvedValue({
      chunk_id: 42,
      chunk_text: 'first message body',
      match_positions: [],
      score: 0,
      role: 'user',
      message_id: 42,
      created_at: '2025-01-01',
    })

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectSession(sampleResult)
    await flushPromises()
    instance.update()

    expect(mockFetchFirstMessage).toHaveBeenCalledWith('s1')
    expect(wrapper.find('.detail-page').exists()).toBe(true)
    expect(wrapper.find('.detail-chunk').exists()).toBe(true)
  })

  it('shows an empty preview when a browse session has no first message', async () => {
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], searchMode: 'recent' }))
    mockFetchFirstMessage.mockResolvedValue(null)

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectSession(sampleResult)
    await flushPromises()
    instance.update()

    expect(wrapper.find('.detail-empty').exists()).toBe(true)
    expect(wrapper.find('.detail-chunk').exists()).toBe(false)
  })

  it('does not lazily fetch for search-mode results (they already carry chunks)', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: 'hybrid' }))

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectSession(sampleResult)
    await flushPromises()
    instance.update()

    expect(mockFetchFirstMessage).not.toHaveBeenCalled()
    expect(wrapper.find('.detail-chunk').exists()).toBe(true)
  })

  it('returns to search list from detail view via back button', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    // Drill down via internal ref
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = sampleResult
    instance.update()
    await flushPromises()
    expect(wrapper.find('.detail-page').exists()).toBe(true)

    // Click back button — same reactivity workaround: set via internal ref + update()
    instance.setupState.selectedSession = null
    instance.update()
    await flushPromises()

    // Should return to search results list
    expect(wrapper.find('.session-search-body').exists()).toBe(true)
    expect(wrapper.find('.detail-page').exists()).toBe(false)
  })

  it('emits open for non-archived session from detail view', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    // Drill down via internal ref
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = sampleResult
    instance.update()
    await flushPromises()

    // Click open button (non-archived session)
    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('open')).toBeTruthy()
    expect(wrapper.emitted('resume')).toBeFalsy()
  })

  it('emits resume for archived session from detail view', async () => {
    const archivedResult = { ...sampleResult, archived: true }
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [archivedResult] }))

    const wrapper = mountDrawer()
    // Drill down via internal ref
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = archivedResult
    instance.update()
    await flushPromises()

    // Click resume button (archived session)
    await wrapper.find('.fbtn-primary').trigger('click')
    expect(wrapper.emitted('resume')).toBeTruthy()
    expect(wrapper.emitted('open')).toBeFalsy()
  })

  it('shows destroy button only for archived session in detail view', async () => {
    const archivedResult = { ...sampleResult, archived: true }
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [archivedResult] }))

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = archivedResult
    instance.update()
    await flushPromises()

    expect(wrapper.find('.fbtn-danger').exists()).toBe(true)
    expect(wrapper.find('.fbtn-danger').text()).toBe('Remove')
  })

  it('does not show destroy button for non-archived session in detail view', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = sampleResult
    instance.update()
    await flushPromises()

    expect(wrapper.find('.fbtn-danger').exists()).toBe(false)
  })

  it('emits destroy when destroy button is clicked on archived session', async () => {
    const archivedResult = { ...sampleResult, archived: true }
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [archivedResult] }))

    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.setupState.selectedSession = archivedResult
    instance.update()
    await flushPromises()

    await wrapper.find('.fbtn-danger').trigger('click')
    expect(wrapper.emitted('destroy')).toBeTruthy()
    expect(wrapper.emitted('destroy')![0][0]).toStrictEqual(archivedResult)
  })

  it('emits close when handleClose is triggered', async () => {
    const wrapper = mountDrawer()
    const bs = wrapper.findComponent({ name: 'BottomSheet' })
    await bs.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('renders mode dropdown showing the current mode', () => {
    const wrapper = mountDrawer()
    // First trigger on the row is the search-mode dropdown.
    const trigger = wrapper.findAll('.filter-dropdown-btn')[0]
    expect(trigger.text()).toContain('Hybrid')
  })

  it('opens the mode dropdown and lists both options', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.filter-dropdown-btn')[0].trigger('click')
    const items = wrapper.findAll('.filter-menu-item')
    expect(items.map(i => i.text())).toEqual(['Hybrid', 'Full-text'])
  })

  it('switches to FTS mode and re-searches via the mode dropdown', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()

    await wrapper.findAll('.filter-dropdown-btn')[0].trigger('click')
    await wrapper.findAll('.filter-menu-item')[1].trigger('click')
    expect(state.preferMode).toBe('fts')
    // setMode triggers re-search via setQuery
    expect(mockSetQuery).toHaveBeenCalledWith('test')
  })

  it('shows actual search mode badge in results', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: 'hybrid' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-mode').text()).toBe('Hybrid')
  })

  it('shows FTS mode badge label for fts search mode', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: 'fts' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-mode').text()).toBe('Full-text')
  })

  it('does not show a mode badge when searchMode is empty', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: '' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-mode').exists()).toBe(false)
  })

  it('does not show a mode badge in browse mode even when mode is set', () => {
    // Browse mode reports mode "recent"; the badge must stay hidden when no
    // query has been entered.
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], searchMode: 'recent' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-mode').exists()).toBe(false)
  })

  it('renders the infinite-scroll sentinel in browse mode', () => {
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], searchMode: 'recent', hasMore: true }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-sentinel').exists()).toBe(true)
  })

  it('does not render the sentinel in search mode', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: 'hybrid', hasMore: true }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-sentinel').exists()).toBe(false)
  })

  it('shows the end marker in browse mode when there are no more pages', () => {
    mockSearchState.mockReturnValue(createState({ query: '', results: [sampleResult], searchMode: 'recent', hasMore: false }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-end').exists()).toBe(true)
  })

  it('shows an escaped preview when a chunk has no match positions', () => {
    mockSearchState.mockReturnValue(createState({
      query: 'test',
      results: [{
        ...sampleResult,
        chunks: [{ ...sampleResult.chunks[0], match_positions: [] }],
      }],
    }))
    const wrapper = mountDrawer()
    // Preview renders via getPreviewHtml → escapeHtml when no match positions
    expect(wrapper.find('.session-search-item-preview').exists()).toBe(true)
  })

  it('browses sessions when opened and clears when closed', async () => {
    vi.useFakeTimers()
    const wrapper = mountDrawer({ open: false })
    await wrapper.setProps({ open: true })
    await vi.advanceTimersByTimeAsync(300)
    expect(mockBrowse).toHaveBeenCalled()

    await wrapper.setProps({ open: false })
    expect(mockClear).toHaveBeenCalled()
    vi.useRealTimers()
  })

  it('focusSearchInput is callable without throwing', () => {
    const wrapper = mountDrawer()
    expect(() => wrapper.vm.focusSearchInput()).not.toThrow()
  })

  it('setMode re-searches when there is an active query', async () => {
    const state = createState({ query: 'active' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()
    await wrapper.findAll('.filter-dropdown-btn')[0].trigger('click')
    await wrapper.findAll('.filter-menu-item')[1].trigger('click')
    expect(mockSetQuery).toHaveBeenCalledWith('active')
  })

  it('setMode does not re-search when query is blank', async () => {
    mockSetQuery.mockClear()
    const state = createState({ query: '' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()
    await wrapper.findAll('.filter-dropdown-btn')[0].trigger('click')
    await wrapper.findAll('.filter-menu-item')[1].trigger('click')
    expect(mockSetQuery).not.toHaveBeenCalled()
  })

  it('renders archive and sort dropdown triggers on the search row', () => {
    const wrapper = mountDrawer()
    const triggers = wrapper.findAll('.filter-dropdown-btn')
    expect(triggers).toHaveLength(3)
    // Triggers: mode / archive / sort. Defaults: Hybrid / All / Relevance.
    expect(triggers[0].text()).toContain('Hybrid')
    expect(triggers[1].text()).toContain('All')
    expect(triggers[2].text()).toContain('Relevance')
    // Mode trigger is never highlighted; archive/sort are when non-default.
    expect(triggers[0].classes()).not.toContain('filter-active')
    expect(triggers[1].classes()).not.toContain('filter-active')
    expect(triggers[2].classes()).not.toContain('filter-active')
  })

  it('opens the archive dropdown and lists the three options', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.filter-dropdown-btn')[1].trigger('click')
    const items = wrapper.findAll('.filter-menu-item')
    expect(items.map(i => i.text())).toEqual(['All', 'Active', 'Archived'])
  })

  it('applies archive filter via the dropdown', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()

    await wrapper.findAll('.filter-dropdown-btn')[1].trigger('click')
    await wrapper.findAll('.filter-menu-item')[2].trigger('click')
    expect(mockSetFilters).toHaveBeenCalledWith({ archived: 'archived' })
  })

  it('does not re-filter when choosing the already-active archive option', async () => {
    mockSearchState.mockReturnValue(createState({ archivedFilter: 'all' }))
    const wrapper = mountDrawer()

    await wrapper.findAll('.filter-dropdown-btn')[1].trigger('click')
    await wrapper.findAll('.filter-menu-item')[0].trigger('click')
    expect(mockSetFilters).not.toHaveBeenCalled()
  })

  it('highlights the archive trigger when a non-default filter is active', () => {
    mockSearchState.mockReturnValue(createState({ archivedFilter: 'archived' }))
    const wrapper = mountDrawer()
    const trigger = wrapper.findAll('.filter-dropdown-btn')[1]
    expect(trigger.classes()).toContain('filter-active')
    expect(trigger.text()).toContain('Archived')
  })

  it('opens the sort dropdown and lists the three options', async () => {
    const wrapper = mountDrawer()
    await wrapper.findAll('.filter-dropdown-btn')[2].trigger('click')
    const items = wrapper.findAll('.filter-menu-item')
    expect(items.map(i => i.text())).toEqual(['Relevance', 'Newest', 'Oldest'])
  })

  it('applies sort order via the dropdown', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()

    await wrapper.findAll('.filter-dropdown-btn')[2].trigger('click')
    await wrapper.findAll('.filter-menu-item')[2].trigger('click')
    expect(mockSetFilters).toHaveBeenCalledWith({ sort: 'oldest' })
  })

  it('does not re-sort when choosing the already-active sort option', async () => {
    mockSearchState.mockReturnValue(createState({ sortOrder: 'relevance' }))
    const wrapper = mountDrawer()

    await wrapper.findAll('.filter-dropdown-btn')[2].trigger('click')
    await wrapper.findAll('.filter-menu-item')[0].trigger('click')
    expect(mockSetFilters).not.toHaveBeenCalled()
  })

  // ── Time range ──
  it('renders the time-range chips with "Any time" active by default', () => {
    const wrapper = mountDrawer()
    const chips = wrapper.findAll('.time-chip')
    expect(chips.map(c => c.text())).toEqual([
      'Any time',
      'Today',
      'Last 7 days',
      'Last 30 days',
      'Custom',
    ])
    expect(chips[0].classes()).toContain('active')
    // Custom date inputs stay hidden until the "Custom" chip is chosen.
    expect(wrapper.findAll('.time-date-input')).toHaveLength(0)
  })

  it('applies a time preset when a chip is clicked', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()

    await wrapper.findAll('.time-chip')[2].trigger('click')
    expect(mockSetFilters).toHaveBeenCalledWith({ timeRange: '7d' })
  })

  it('does not re-filter when the already-active time chip is clicked', async () => {
    mockSearchState.mockReturnValue(createState({ timeRange: 'all' }))
    const wrapper = mountDrawer()

    await wrapper.findAll('.time-chip')[0].trigger('click')
    expect(mockSetFilters).not.toHaveBeenCalled()
  })

  it('highlights the active time chip', () => {
    mockSearchState.mockReturnValue(createState({ timeRange: '30d' }))
    const wrapper = mountDrawer()
    const chips = wrapper.findAll('.time-chip')
    expect(chips[3].classes()).toContain('active')
    expect(chips[0].classes()).not.toContain('active')
  })

  it('reveals date inputs and seeds them when switching to custom', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()

    await wrapper.findAll('.time-chip')[4].trigger('click')

    // The composable state is seeded so the fields are not blank, then the
    // selection is applied.
    expect(state.customFrom).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(state.customTo).toMatch(/^\d{4}-\d{2}-\d{2}$/)
    expect(mockSetFilters).toHaveBeenCalledWith({ timeRange: 'custom' })

    // setFilters is mocked here, so apply the resulting selection manually to
    // assert the custom date inputs render.
    state.timeRange = 'custom'
    const instance = (wrapper.vm as any).$
    instance.update()
    expect(wrapper.findAll('.time-date-input')).toHaveLength(2)
  })

  it('re-runs the search when a custom date bound changes', async () => {
    mockSearchState.mockReturnValue(createState({
      query: 'test',
      timeRange: 'custom',
      customFrom: '2024-01-01',
      customTo: '2024-02-01',
    }))
    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.update()
    await flushPromises()

    await wrapper.findAll('.time-date-input')[0].trigger('change')
    expect(mockSetFilters).toHaveBeenCalledWith({})
  })

  it('does not re-run the search when a custom date change leaves both bounds empty', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', timeRange: 'custom' }))
    const wrapper = mountDrawer()
    const instance = (wrapper.vm as any).$
    instance.update()
    await flushPromises()

    await wrapper.findAll('.time-date-input')[0].trigger('change')
    expect(mockSetFilters).not.toHaveBeenCalled()
  })
})
