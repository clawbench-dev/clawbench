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
      'sessionSearch.deleted': 'Deleted',
      'sessionSearch.chunks': `${params?.count ?? 0} chunks`,
      'sessionSearch.roleUser': 'User',
      'sessionSearch.roleAssistant': 'Assistant',
      'sessionSearch.resume': 'Resume Session',
      'sessionSearch.openSession': 'Open',
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
const mockSearchState = vi.fn()

vi.mock('@/composables/useSessionSearch', () => ({
  useSessionSearch: () => ({
    state: mockSearchState(),
    setQuery: mockSetQuery,
    clear: mockClear,
  }),
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

function createState(overrides = {}) {
  return {
    query: '',
    results: [],
    total: 0,
    loading: false,
    error: null as string | null,
    searchMode: '',
    ragAvailable: null as boolean | null,
    ...overrides,
  }
}

const sampleResult = {
  session_id: 's1',
  session_title: 'My Session',
  score: 0.9,
  backend: 'cli',
  project_path: '/tmp',
  deleted: false,
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

  it('shows no query message when query is empty', () => {
    mockSearchState.mockReturnValue(createState({ query: '' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-empty').text()).toContain('Enter a query')
  })

  it('shows searching state', () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', loading: true }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-empty').text()).toContain('Searching...')
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
        deleted: false,
        created_at: '2025-01-01',
        match_count: 1,
        chunks: [],
      }],
    }))

    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-item-title').text()).toBe('Untitled')
  })

  it('shows deleted badge', () => {
    mockSearchState.mockReturnValue(createState({
      query: 'test',
      results: [{
        session_id: 's3',
        session_title: 'Deleted Session',
        score: 0.5,
        backend: 'cli',
        project_path: '',
        deleted: true,
        created_at: '2025-01-01',
        match_count: 1,
        chunks: [],
      }],
    }))

    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-item-deleted').exists()).toBe(true)
    expect(wrapper.find('.session-search-item-deleted').text()).toBe('Deleted')
  })

  it('drills down to detail view when clicking a result', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    // Click on a search result item
    await wrapper.find('.session-search-item').trigger('click')
    await flushPromises()

    // Should show detail view
    expect(wrapper.find('.detail-page').exists()).toBe(true)
    expect(wrapper.find('.detail-chunk').exists()).toBe(true)
    expect(wrapper.find('.detail-resume-btn').exists()).toBe(true)
    // Search results list should be hidden
    expect(wrapper.find('.session-search-body').exists()).toBe(false)
  })

  it('returns to search list from detail view via back button', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    // Drill down
    await wrapper.find('.session-search-item').trigger('click')
    await flushPromises()
    expect(wrapper.find('.detail-page').exists()).toBe(true)

    // Click back button
    await wrapper.find('.detail-back-btn').trigger('click')
    await flushPromises()

    // Should return to search results list
    expect(wrapper.find('.session-search-body').exists()).toBe(true)
    expect(wrapper.find('.detail-page').exists()).toBe(false)
  })

  it('emits open for non-deleted session from detail view', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult] }))

    const wrapper = mountDrawer()
    // Drill down
    await wrapper.find('.session-search-item').trigger('click')
    await flushPromises()

    // Click open button (non-deleted session)
    await wrapper.find('.detail-resume-btn').trigger('click')
    expect(wrapper.emitted('open')).toBeTruthy()
    expect(wrapper.emitted('resume')).toBeFalsy()
  })

  it('emits resume for deleted session from detail view', async () => {
    const deletedResult = { ...sampleResult, deleted: true }
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [deletedResult] }))

    const wrapper = mountDrawer()
    await wrapper.find('.session-search-item').trigger('click')
    await flushPromises()

    // Click resume button (deleted session)
    await wrapper.find('.detail-resume-btn').trigger('click')
    expect(wrapper.emitted('resume')).toBeTruthy()
    expect(wrapper.emitted('open')).toBeFalsy()
  })

  it('emits close when handleClose is triggered', async () => {
    const wrapper = mountDrawer()
    const bs = wrapper.findComponent({ name: 'BottomSheet' })
    await bs.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })
})
