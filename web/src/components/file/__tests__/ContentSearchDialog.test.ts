import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, reactive, computed } from 'vue'
import { onTabSwitch } from '@/composables/useTabDrawer'
import { _setWideScreenForTest, switchLeftTab } from '@/composables/useWideScreenLayout'
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
  stopped: false,
  error: '',
  searchBasePath: '',
})

const mockStartSearch = vi.fn()
const mockCancelSearch = vi.fn()
// The real stopSearch flips `searching` off and marks `stopped`; the dialog
// reads both, so the mock must do the same or the stopped UI is untestable.
const mockStopSearch = vi.fn(() => {
  searchState.searching = false
  searchState.stopped = true
})
const mockReset = vi.fn()

vi.mock('@/composables/useFileContentSearch', () => ({
  useFileContentSearch: () => ({
    state: searchState,
    effectiveDir: computed(() => (searchState.scope === 'global' ? '' : 'src')),
    effectiveRecursive: computed(() => searchState.scope === 'global' || searchState.recursive),
    loadedMatches: computed(() =>
      searchState.results.reduce((sum, f) => sum + f.matches.length, 0),
    ),
    startSearch: mockStartSearch,
    cancelSearch: mockCancelSearch,
    stopSearch: mockStopSearch,
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
          title: 'File content search',
          scopeCurrent: 'only this directory',
          scopeRecursive: 'this directory and below',
          scopeProject: 'the whole project',
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
          noResultsHint: 'Try a shorter term, or adjust the include / exclude and scope',
          summary: '{files} files, {matches} matches',
          summaryPlus: '{files}+ files, {matches}+ matches',
          summaryStopped: 'Stopped — found {files} files, {matches} matches',
          stop: 'Stop',
          stoppedEmpty: 'Stopped before anything matched',
          fileTruncated: '{total} matches in this file — open it to see all',
        },
      },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }
// BottomSheet is teleported and animated; a passthrough stub keeps the DOM
// assertions local to the dialog's own markup. `maximized` is forwarded as a
// class so tests can assert the dialog opens maximized.
//
// Props MUST be declared with their types (object form, not `props: [...]`).
// A bare `maximized` attribute arrives as the empty string "", and with the
// array form Vue does not coerce it — the value stays "" (falsy) and the class
// is never applied, so a "starts maximized" assertion would pass against a
// stub that never saw `true`.
const BottomSheetStub = {
  props: { open: Boolean, title: String, maximized: Boolean },
  template: '<div v-if="open" class="bs-stub" :class="{ \'bs-maximized\': maximized }"><slot name="header" /><slot /></div>',
}
const FileIconStub = { props: ['path', 'size'], template: '<i class="icon-stub" />' }

/**
 * Simulate the user switching to `tab`, using whichever lever production uses.
 *
 * jsdom reports a viewport that makes `isWideScreen` true, so the two layouts
 * take different paths (narrow: `onTabSwitch` sets currentTab; wide: the left
 * column's tab is `leftTab`, set via `switchLeftTab`). Driving only one of them
 * would make the tab-binding tests silently vacuous in the other layout.
 */
function goToTab(tab: string) {
  onTabSwitch(tab)
  if (tab !== 'chat') switchLeftTab(tab)
}

/**
 * Mount and OPEN the dialog the way production does: through its exposed
 * `open()` (which flips the useTabDrawer state the template actually binds to).
 *
 * Do NOT pass an `open` prop — the component no longer has one, and a leftover
 * prop would silently fall through as a DOM attribute. The stub's
 * `v-if="open"` would then see the truthy string "true" and every test would
 * pass while the real binding stayed false.
 *
 * Async because flipping the drawer state needs a tick to reach the template.
 */
async function mountDialog(props: Record<string, unknown> = {}) {
  goToTab('browse')
  const wrapper = mount(ContentSearchDialog, {
    props: { currentDir: 'src', ...props },
    global: {
      stubs: { Teleport: TeleportStub, BottomSheet: BottomSheetStub, FileIcon: FileIconStub },
      plugins: [i18n],
    },
  })
  ;(wrapper.vm as unknown as { open: () => void }).open()
  await nextTick()
  return wrapper
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
  it('renders the search box and all option toggles', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.find('.search-pill').exists()).toBe(true)
    // case / whole word / regex / recursive / scope / filters
    expect(wrapper.findAll('.cs-toggle-btn')).toHaveLength(6)
  })

  it('shows the hint before a query is typed', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-empty').text()).toContain('Type to search')
  })

  // ── Header title: fixed prefix + muted scope suffix ──

  it('shows the fixed prefix followed by a muted scope suffix', async () => {
    const wrapper = await mountDialog()
    const title = wrapper.find('.bs-header-title')

    expect(title.text()).toContain('File content search')
    // The suffix is a separate element so it can be styled down; the old header
    // had an unstyled chip and no recursion in the title at all.
    const suffix = title.find('.cs-header-scope')
    expect(suffix.exists()).toBe(true)
    expect(suffix.text()).toBe('only this directory')
  })

  it('suffix names recursion when the current directory is searched recursively', async () => {
    searchState.recursive = true
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-header-scope').text()).toBe('this directory and below')
  })

  it('suffix collapses to the project label under global scope', async () => {
    // Global always recurses, so scope+recursion collapses to ONE label:
    // naming recursion here would imply a state the user cannot turn off (the
    // recursive toggle is disabled and pinned active).
    searchState.scope = 'global'
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-header-scope').text()).toBe('the whole project')
  })

  it('global scope does not also claim recursion in the suffix', async () => {
    searchState.scope = 'global'
    searchState.recursive = true
    const wrapper = await mountDialog()

    const suffix = wrapper.find('.cs-header-scope').text()
    expect(suffix).toBe('the whole project')
    expect(suffix).not.toContain('below')
  })

  it('suffix tracks a live toggle without remounting', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-header-scope').text()).toBe('only this directory')

    // The recursive toggle is the 4th cs-toggle-btn (case, whole word, regex,
    // recursive, scope, filters).
    await wrapper.findAll('.cs-toggle-btn')[3].trigger('click')
    await nextTick()

    expect(wrapper.find('.cs-header-scope').text()).toBe('this directory and below')
  })

  it('renders a large icon in the pre-query empty state', async () => {
    const wrapper = await mountDialog()
    const icon = wrapper.find('.cs-empty-icon')

    // A large muted glyph anchors the empty state; without it the panel is a
    // lone line of grey text. 56px matches the filename search's empty state
    // in FileManagerContent so the two surfaces look alike.
    expect(icon.exists()).toBe(true)
    expect(icon.attributes('width')).toBe('56')
    expect(icon.attributes('height')).toBe('56')
    // The message must sit alongside the icon, not replace it.
    expect(wrapper.find('.cs-empty-text').text()).toContain('Type to search')
  })

  it('renders a large icon plus a suggestion when nothing matched', async () => {
    searchState.query = 'needle'
    searchState.searching = false
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-empty-icon').exists()).toBe(true)
    expect(wrapper.find('.cs-empty-icon').attributes('width')).toBe('56')
    expect(wrapper.find('.cs-empty-text').text()).toContain('No files found')
    // A dead-end "no results" is less useful than one that suggests widening
    // the search.
    expect(wrapper.find('.cs-empty-hint').text()).toContain('shorter term')
  })

  it('does not show the no-results icon while still searching', async () => {
    searchState.query = 'needle'
    searchState.searching = true
    const wrapper = await mountDialog()

    // The spinner owns this state; an empty-state icon here would claim the
    // search already finished.
    expect(wrapper.find('.cs-empty').exists()).toBe(false)
    expect(wrapper.find('.cs-empty-icon').exists()).toBe(false)
  })

  it('shows a spinner while searching with no results yet', async () => {
    searchState.query = 'needle'
    searchState.searching = true
    const wrapper = await mountDialog()
    expect(wrapper.find('.loading-indicator').exists()).toBe(true)
  })

  it('shows the empty state when the search finished with no hits', async () => {
    searchState.query = 'needle'
    searchState.searching = false
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-empty').text()).toContain('No files found')
  })

  it('surfaces a backend error instead of the empty state', async () => {
    searchState.query = '([bad'
    searchState.regex = true
    searchState.error = 'Invalid regular expression: missing )'
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-error').text()).toContain('missing )')
    expect(wrapper.find('.cs-empty').exists()).toBe(false)
  })

  it('groups matches under their file and shows the per-file count', async () => {
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
    const wrapper = await mountDialog()

    expect(wrapper.findAll('.cs-file')).toHaveLength(2)
    expect(wrapper.findAll('.cs-file-count')[0].text()).toBe('2')
    expect(wrapper.findAll('.cs-file-count')[1].text()).toBe('1')
  })

  it('highlights the matched span inside a line', async () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'a needle b', ranges: [{ start: 2, end: 8 }] }], total: 1 },
    ]
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-match-text').html()).toContain('<mark>needle</mark>')
  })

  it('escapes HTML in matched line text', async () => {
    searchState.query = 'x'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: '<img src=x onerror=alert(1)>', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()
    const html = wrapper.find('.cs-match-text').html()
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
  })

  it('shows the summary line with file and match counts', async () => {
    searchState.query = 'needle'
    searchState.files = 3
    searchState.matches = 7
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-summary').text()).toContain('3 files')
    expect(wrapper.find('.cs-summary').text()).toContain('7 matches')
  })

  // ── Streaming progress + stop ──

  it('counts what has arrived while still searching, not the final totals', async () => {
    // state.files / state.matches are only finalised by the `done` event, so
    // they are still 0 mid-flight. Reading them here would render
    // "0 files, 0 matches" beside files whose matches are visibly on screen.
    searchState.query = 'needle'
    searchState.searching = true
    searchState.files = 0
    searchState.matches = 0
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
      { name: 'b.go', path: 'b.go', matches: [
        { line: 2, text: 'needle', ranges: [] },
        { line: 9, text: 'needle', ranges: [] },
      ], total: 2 },
    ]
    const wrapper = await mountDialog()

    const summary = wrapper.find('.cs-summary').text()
    expect(summary).toContain('2 files')
    expect(summary).toContain('3 matches')
  })

  it('shows a spinner in the summary while searching with results present', async () => {
    // The old header only rendered progress when there were ZERO results, so
    // once the first file arrived the in-flight state became invisible.
    searchState.query = 'needle'
    searchState.searching = true
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-summary .loading-indicator').exists()).toBe(true)
  })

  it('offers a stop button only while searching', async () => {
    searchState.query = 'needle'
    searchState.searching = true
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-summary .cs-stop-btn').exists()).toBe(true)

    searchState.searching = false
    await nextTick()
    expect(wrapper.find('.cs-summary .cs-stop-btn').exists()).toBe(false)
  })

  it('stops the search from the summary button', async () => {
    searchState.query = 'needle'
    searchState.searching = true
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()

    await wrapper.find('.cs-summary .cs-stop-btn').trigger('click')
    await nextTick()

    expect(mockStopSearch).toHaveBeenCalledTimes(1)
  })

  it('offers stop before the first result arrives', async () => {
    // A slow first hit is exactly when the user wants out; hiding the button
    // until something matches would trap them.
    searchState.query = 'needle'
    searchState.searching = true
    searchState.results = []
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-loading .cs-stop-btn').exists()).toBe(true)
    await wrapper.find('.cs-loading .cs-stop-btn').trigger('click')
    expect(mockStopSearch).toHaveBeenCalledTimes(1)
  })

  it('labels a stopped search as partial instead of showing final counts', async () => {
    searchState.query = 'needle'
    searchState.searching = false
    searchState.stopped = true
    // Finalised totals never arrive (the stop aborts the request), so they are
    // still zero while results are on screen.
    searchState.files = 0
    searchState.matches = 0
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [
        { line: 1, text: 'needle', ranges: [] },
        { line: 4, text: 'needle', ranges: [] },
      ], total: 2 },
    ]
    const wrapper = await mountDialog()

    const summary = wrapper.find('.cs-summary').text()
    expect(summary).toContain('Stopped')
    expect(summary).toContain('1 files')
    expect(summary).toContain('2 matches')
  })

  it('does not claim "no files found" when stopped before any match', async () => {
    // The walk never finished, so the absence of hits proves nothing.
    searchState.query = 'needle'
    searchState.searching = false
    searchState.stopped = true
    searchState.results = []
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-empty').text()).toContain('Stopped before anything matched')
    expect(wrapper.find('.cs-empty').text()).not.toContain('No files found')
  })

  it('hides the stop button once stopped', async () => {
    searchState.query = 'needle'
    searchState.searching = false
    searchState.stopped = true
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-stop-btn').exists()).toBe(false)
  })

  it('emits openFile with the path and line when a match is clicked', async () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'src/a.go', matches: [{ line: 42, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()
    await wrapper.find('.cs-match').trigger('click')

    expect(wrapper.emitted('openFile')).toBeTruthy()
    expect(wrapper.emitted('openFile')![0]).toEqual(['src/a.go', 42])
  })

  it('collapses a file group when its header is clicked', async () => {
    searchState.query = 'needle'
    searchState.results = [
      { name: 'a.go', path: 'a.go', matches: [{ line: 1, text: 'needle', ranges: [] }], total: 1 },
    ]
    const wrapper = await mountDialog()

    const head = wrapper.find('.cs-file-head')
    expect(head.classes()).not.toContain('collapsed')
    await head.trigger('click')
    expect(wrapper.find('.cs-file-head').classes()).toContain('collapsed')
  })

  it('toggling case sensitivity re-runs the search immediately', async () => {
    searchState.query = 'needle'
    const wrapper = await mountDialog()

    await wrapper.findAll('.cs-toggle-btn')[0].trigger('click')

    expect(searchState.caseSensitive).toBe(true)
    // immediate = true so the toggle does not wait for the debounce
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('toggling regex re-runs the search', async () => {
    searchState.query = 'needle'
    const wrapper = await mountDialog()

    await wrapper.findAll('.cs-toggle-btn')[2].trigger('click')

    expect(searchState.regex).toBe(true)
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('scope toggle switches between current and global', async () => {
    searchState.query = 'needle'
    const wrapper = await mountDialog()

    // The scope button is the fifth toggle (index 4).
    await wrapper.findAll('.cs-toggle-btn')[4].trigger('click')
    expect(searchState.scope).toBe('global')
    expect(mockStartSearch).toHaveBeenCalledWith('src', true)
  })

  it('recursive toggle is disabled while global scope is active', async () => {
    searchState.scope = 'global'
    const wrapper = await mountDialog()
    // Index 3 is the recursive toggle.
    expect(wrapper.findAll('.cs-toggle-btn')[3].attributes('disabled')).toBeDefined()
  })

  it('reveals the include/exclude inputs behind the filter toggle', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.find('.cs-filters').exists()).toBe(false)

    // The filters button is the last toggle.
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')
    expect(wrapper.find('.cs-filters').exists()).toBe(true)
    expect(wrapper.findAll('.cs-filter-input')).toHaveLength(2)
  })

  it('re-runs the search when include/exclude change', async () => {
    searchState.query = 'needle'
    const wrapper = await mountDialog()
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')

    const inputs = wrapper.findAll('.cs-filter-input')
    await inputs[0].setValue('*.ts')
    await nextTick()

    expect(searchState.include).toBe('*.ts')
    expect(mockStartSearch).toHaveBeenCalledWith('src')
  })

  it('marks a truncated file group and explains the cap', async () => {
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
    const wrapper = await mountDialog()

    expect(wrapper.find('.cs-count-plus').text()).toBe('1+')
    expect(wrapper.find('.cs-match-more').text()).toContain('500 matches')
  })

  it('cancels the search and closes when dismissed', async () => {
    const wrapper = await mountDialog()
    await wrapper.vm.handleClose?.()
    await nextTick()

    expect(mockCancelSearch).toHaveBeenCalled()
    expect(wrapper.find('.bs-stub').exists()).toBe(false)
  })

  it('does not re-run on include change when the query is empty', async () => {
    const wrapper = await mountDialog()
    await wrapper.findAll('.cs-toggle-btn')[5].trigger('click')
    await wrapper.findAll('.cs-filter-input')[0].setValue('*.ts')
    await nextTick()

    expect(mockStartSearch).not.toHaveBeenCalled()
  })

  // ── Tab binding (the dialog is bound to the file-manager tab) ──

  it('hides when the user leaves the browse tab', async () => {
    const wrapper = await mountDialog()
    expect(wrapper.find('.bs-stub').exists()).toBe(true)

    goToTab('view')
    await nextTick()

    expect(wrapper.find('.bs-stub').exists()).toBe(false)
  })

  it('restores when the user returns to the browse tab', async () => {
    const wrapper = await mountDialog()

    // 'view' (not 'chat'): chat is a separate pane on wide screens, so leaving
    // the file manager means switching the LEFT column away from browse.
    goToTab('view')
    await nextTick()
    expect(wrapper.find('.bs-stub').exists()).toBe(false)

    goToTab('browse')
    await nextTick()
    expect(wrapper.find('.bs-stub').exists()).toBe(true)
  })

  it('stays closed on tab return if the user had closed it', async () => {
    const wrapper = await mountDialog()
    ;(wrapper.vm as unknown as { close: () => void }).close()
    await nextTick()
    expect(wrapper.find('.bs-stub').exists()).toBe(false)

    goToTab('view')
    await nextTick()
    goToTab('browse')
    await nextTick()

    expect(wrapper.find('.bs-stub').exists()).toBe(false)
  })

  it('opens maximized', async () => {
    const wrapper = await mountDialog()
    // The panel must fill the available height rather than hug its content —
    // otherwise the sheet grows and shrinks on every result update.
    expect(wrapper.find('.bs-stub').classes()).toContain('bs-maximized')
  })

  it('cancels the in-flight search when hidden by a tab switch', async () => {
    await mountDialog()
    mockCancelSearch.mockClear()

    goToTab('view')
    await nextTick()

    expect(mockCancelSearch).toHaveBeenCalled()
  })
})
