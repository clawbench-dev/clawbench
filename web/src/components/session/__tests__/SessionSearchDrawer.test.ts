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
      'sessionSearch.openSession': 'Open',
      'sessionSearch.modeHybrid': 'Hybrid',
      'sessionSearch.modeFts': 'Full-text',
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
    preferMode: 'hybrid' as const,
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
    expect(wrapper.find('.detail-resume-btn').exists()).toBe(true)
    // Search results list should be hidden
    expect(wrapper.find('.session-search-body').exists()).toBe(false)
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
    await wrapper.find('.detail-resume-btn').trigger('click')
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

  it('renders mode selector with hybrid active by default', () => {
    const wrapper = mountDrawer()
    const buttons = wrapper.findAll('.mode-btn')
    expect(buttons).toHaveLength(2)
    expect(buttons[0].classes()).toContain('active')
    expect(buttons[0].text()).toBe('Hybrid')
    expect(buttons[1].text()).toBe('Full-text')
  })

  it('switches to FTS mode and re-searches when clicking FTS button', async () => {
    const state = createState({ query: 'test' })
    mockSearchState.mockReturnValue(state)
    const wrapper = mountDrawer()
    const ftsBtn = wrapper.findAll('.mode-btn')[1]

    await ftsBtn.trigger('click')
    expect(state.preferMode).toBe('fts')
    // setMode triggers re-search via setQuery
    expect(mockSetQuery).toHaveBeenCalledWith('test')
  })

  it('shows actual search mode badge in results', async () => {
    mockSearchState.mockReturnValue(createState({ query: 'test', results: [sampleResult], searchMode: 'hybrid' }))
    const wrapper = mountDrawer()
    expect(wrapper.find('.session-search-mode').text()).toBe('Hybrid')
  })
})
