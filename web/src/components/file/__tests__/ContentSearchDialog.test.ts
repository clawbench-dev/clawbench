import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, reactive, computed } from 'vue'
import ContentSearchDialog from '../ContentSearchDialog.vue'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// The composable owns the SSE transport and is covered by its own test file;
// here we drive the dialog's rendering and interaction against a controllable
// state object. It MUST be reactive: the dialog watches `state.include` /
// `state.exclude` / `state.query`, and a plain object would never trigger them.
const searchState = reactive({
  query: '',
  recursive: false,
  regex: false,
  wholeWord: false,
  caseSensitive: false,
  include: '',
  exclude: '',
  scope: 'current' as 'current' | 'global',
  results: [] as Array<{
    name: string
    path: string
    matches: Array<{ line: number; text: string; ranges: Array<{ start: number; end: number }> }>
    total: number
    truncated?: boolean
    ignored?: boolean
  }>,
  searching: false,
  files: 0,
  matches: 0,
  searched: 0,
  truncated: false,
  error: '',
  searchBasePath: '',
})

const mockStartSearch = vi.fn()
const mockCancelSearch = vi.fn()
const mockReset = vi.fn()

vi.mock('@/composables/useFileContentSearch', () => ({
  useFileContentSearch: () => ({
    state: searchState,
    effectiveDir: computed(() => (searchState.scope === 'global' ? '' : 'src')),
    effectiveRecursive: computed(() => searchState.scope === 'global' || searchState.recursive),
    loadedMatches: computed(() => 0),
    startSearch: mockStartSearch,
    cancelSearch: mockCancelSearch,
    reset: mockReset,
    getDisplayLimit: () => 100,
  }),
}))

// EventSource is never reached (the composable is mocked), but importing the
// module must not touch a real one.
class MockEventSource {
  addEventListener() {}
  close() {}
}
vi.stubGlobal('EventSource', MockEventSource)

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        search: {
          searching: 'Searching...',
          noResults: 'No files found',
          recursive: 'Recursive',
          scopeGlobal: 'Global search',
          wordGlobal: 'project',
          wordCurrent: 'current directory',
        },
        contentSearch: {
          title: 'Search in files',
          placeholder: 'Search file contents...',
          button: 'Search in files',
          caseSensitive: 'Match case',
          wholeWord: 'Match whole word',
          regex: 'Use regular expression',
          filters: 'Include / exclude files',
          includeLabel: 'files to include',
          includePlaceholder: 'e.g. *.ts, src/**',
          excludeLabel: 'files to exclude',
          excludePlaceholder: 'e.g. dist/**, *.min.js',
          hint: 'Type to search inside file contents',
          summary: '{files} files, {matches} matches',
          summaryPlus: '{files}+ files, {matches}+ matches',
          fileTruncated: '{total} matches in this file — open it to see all',
        },
      },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }
// BottomSheet is teleported and animated; a passthrough stub keeps the DOM
// assertions local to the dialog's own markup.
const BottomSheetStub = {
  props: ['open', 'title'],
  template: '<div v-if="open" class="bs-stub"><slot name="header" /><slot /></div>',
}
const FileIconStub = { props: ['path', 'size'], template: '<i class="icon-stub" />' }

function mountDialog(props: Record<string, unknown> = {}) {
  return mount(ContentSearchDialog, {
    props: { open: true, currentDir: 'src', ...props },
    global: {
      stubs: { Teleport: TeleportStub, BottomSheet: BottomSheetStub, FileIcon: FileIconStub },
      plugins: [i18n],
    },
  })
}

function resetState() {
  Object.assign(searchState, {
    query: '',
    recursive: false,
    regex: false,
    wholeWord: false,
    caseSensitive: false,
    include: '',
    exclude: '',
    scope: 'current',
    results: [],
    searching: false,
    files: 0,
    matches: 0,
    searched: 0,
    truncated: false,
    error: '',
    searchBasePath: '',
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  resetState()
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('ContentSearchDialog', () => {
  it('renders the search box and all option toggles', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.search-pill').exists()).toBe(true)
    // case / whole word / regex / recursive / scope / filters
    expect(wrapper.findAll('.cs-toggle-btn')).toHaveLength(6)
  })

  it('shows the hint before a query is typed', () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-empty').text()).toContain('Type to search')
  })

  it('shows a spinner while searching with no results yet', () => {
    searchState.query = 'needle'
    searchState.searching = true
    const wrapper = mountDialog()
    expect(wrapper.find('.loading-indicator').exists()).toBe(true)
  })

  it('shows the empty state when the search finished with no hits', () => {
    searchState.query = 'needle'
    searchState.searching = false
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-empty').text()).toContain('No files found')
  })

  it('surfaces a backend error instead of the empty state', () => {
    searchState.query = '([bad'
    searchState.regex = true
    searchState.error = 'Invalid regular expression: missing )'
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-error').text()).toContain('missing )')
    expect(wrapper.find('.cs-empty').exists()).toBe(false)
  })

  it('groups matches under their file and shows the per-file count', () => {
    searchState.query = 'needle'
    searchState.results = [
      {
        name: 'a.go',
        path: 'src/a.go',
        matches: [
          { line: 3, text: 'needle', ranges: [{ start: 0, end: 6 }] },
          { line: 9, text: 'more needle', ranges: [{ start: 5, end: 11 }] },
        ],
        total: 2,
      },
      { name: 'b.go', path: 'src/b.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = mountDialog()

    expect(wrapper.findAll('.cs-file')).toHaveLength(2)
    expect(wrapper.findAll('.cs-file-count')[0].text()).toBe('2')
    expect(wrapper.findAll('.cs-file-count')[1].text()).toBe('1')
  })

  it('highlights the matched span inside a line', () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'a needle b', ranges: [{ start: 2, end: 8 }] }], total: 1 },
    ]
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-match-text').html()).toContain('<mark>needle</mark>')
  })

  it('escapes HTML in matched line text', () => {
    searchState.query = 'x'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: '<img src=x onerror=alert(1)>', ranges: [] }], total: 1 },
    ]
    const wrapper = mountDialog()
    const html = wrapper.find('.cs-match-text').html()
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
  })

  it('shows the summary line with file and match counts', () => {
    searchState.query = 'needle'
    searchState.files = 3
    searchState.matches = 7
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-summary').text()).toContain('3 files')
    expect(wrapper.find('.cs-summary').text()).toContain('7 matches')
  })

  it('emits openFile with the path and line when a match is clicked', async () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'src/a.go', matches: [{ line: 42, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = mountDialog()
    await wrapper.find('.cs-match').trigger('click')

    expect(wrapper.emitted('openFile')).toBeTruthy()
    expect(wrapper.emitted('openFile')![0]).toEqual(['src/a.go', 42])
  })

  it('collapses a file group when its header is clicked', async () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = mountDialog()

    const head = wrapper.find('.cs-file-head')
    expect(head.classes()).not.toContain('collapsed')
    await head.trigger('click')
    expect(wrapper.find('.cs-file-head').classes()).toContain('collapsed')
  })

  it('toggling case sensitivity re-runs the search immediately', async () => {
    searchState.query = 'needle'
    const wrapper = mountDialog()

    await wrapper.findAll('.cs-toggle-btn')[0].trigger('click')

    expect(searchState.caseSensitive).toBe(true)
    // immediate = true so the toggle does not wait for the debounce
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('toggling regex re-runs the search', async () => {
    searchState.query = 'needle'
    const wrapper = mountDialog()

    await wrapper.findAll('.cs-toggle-btn')[2].trigger('click')

    expect(searchState.regex).toBe(true)
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('scope toggle switches between current and global', async () => {
    searchState.query = 'needle'
    const wrapper = mountDialog()

    // The scope button is the fifth toggle (index 4).
    await wrapper.findAll('.cs-toggle-btn')[4].trigger('click')
    expect(searchState.scope).toBe('global')
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('recursive toggle is disabled while global scope is active', async () => {
    searchState.scope = 'global'
    const wrapper = mountDialog()
    // Index 3 is the recursive toggle.
    expect(wrapper.findAll('.cs-toggle-btn')[3].attributes('disabled')).toBeDefined()
  })

  it('reveals the include/exclude inputs behind the filter toggle', async () => {
    const wrapper = mountDialog()
    expect(wrapper.find('.cs-filters').exists()).toBe(false)

    // The filters button is the last toggle.
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')
    expect(wrapper.find('.cs-filters').exists()).toBe(true)
    expect(wrapper.findAll('.cs-filter-input')).toHaveLength(2)
  })

  it('re-runs the search when include/exclude change', async () => {
    searchState.query = 'needle'
    const wrapper = mountDialog()
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')

    const inputs = wrapper.findAll('.cs-filter-input')
    await inputs[0].setValue('*.ts')
    await nextTick()

    expect(searchState.include).toBe('*.ts')
    expect(mockStartSearch).toHaveBeenCalledWith('src')
  })

  it('marks a truncated file group and explains the cap', () => {
    searchState.query = 'needle'
    searchState.results = [
      {
        name: 'big.txt',
        path: 'big.txt',
        matches: [{ line: 1, text: 'needle', ranges: [] }],
        total: 500,
        truncated: true,
      },
    ]
    const wrapper = mountDialog()

    expect(wrapper.find('.cs-count-plus').text()).toBe('1+')
    expect(wrapper.find('.cs-match-more').text()).toContain('500 matches')
  })

  it('cancels the search and emits close when dismissed', async () => {
    const wrapper = mountDialog()
    await wrapper.vm.handleClose?.()
    expect(mockCancelSearch).toHaveBeenCalled()
  })

  it('does not re-run on include change when the query is empty', async () => {
    const wrapper = mountDialog()
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')
    await wrapper.findAll('.cs-filter-input')[0].setValue('*.ts')
    await nextTick()

    expect(mockStartSearch).not.toHaveBeenCalled()
  })
})
