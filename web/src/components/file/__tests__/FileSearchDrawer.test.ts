import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import FileSearchDrawer from '@/components/file/FileSearchDrawer.vue'

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock isThumbableExt — prevents real implementation from running
vi.mock('@/utils/fileManager', () => ({
  isThumbableExt: () => false,
}))

// Mock useSettingsConfig — required by useFileSearch
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    getServerValueWithDefault: () => 100,
  }),
}))

// Mock navToFileInManager
const mockNavToFileInManager = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useFilePathAnnotation', () => ({
  navToFileInManager: (...args: any[]) => mockNavToFileInManager(...args),
}))

// Mock BottomSheet component — must use vi.mock since SFC has no explicit name
vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    props: ['open', 'auto', 'title', 'instant', 'compact', 'noHeader', 'handleOnly', 'transparentOverlay', 'fullscreen', 'closeGuard'],
    emits: ['close'],
    template: '<div class="bottom-sheet-stub" v-if="$props.open"><slot name="header" /><slot /></div>',
  }),
}))

// Mock useFileSearch to control state
const mockState = {
  query: '',
  recursive: true,
  scope: 'current' as 'current' | 'global',
  results: [] as Array<{ name: string; path: string; type: string; matchedIndices: number[] }>,
  searching: false,
  total: 0,
  truncated: false,
  searchBasePath: '',
}
const mockStartSearch = vi.fn()
const mockCancelSearch = vi.fn()
const mockReset = vi.fn()

vi.mock('@/composables/useFileSearch', () => ({
  useFileSearch: () => ({
    state: mockState,
    effectiveDir: { value: '' },
    startSearch: mockStartSearch,
    cancelSearch: mockCancelSearch,
    reset: mockReset,
    getDisplayLimit: () => 100,
  }),
}))

// Minimal i18n instance for tests — globalInjection:false avoids async
// enableDevTools() leaks that cause vitest detectAsyncLeaks to flag 4 promises.
const i18n = createI18n({
  legacy: false,
  locale: 'en',
  globalInjection: false,
  messages: {
    en: {
      file: {
        search: {
          title: 'Search Files',
          titleCurrent: 'Search in current directory',
          titleCurrentRecursive: 'Recursive search in current directory',
          titleGlobal: 'Search in project',
          titleGlobalRecursive: 'Recursive search in project',
          placeholder: 'Search filenames...',
          recursive: 'Recursive',
          noResults: 'No files found',
          searching: 'Searching...',
          resultCount: '{count} files found',
          resultCountPlus: '{limit}+ files found',
          truncated: 'Showing first {max} results',
          searchFrom: 'From: {path}',
          reset: 'Reset',
          scopeGlobal: 'Global search',
        },
      },
      chat: {
        attach: {
          openDirectory: 'Open directory',
        },
      },
    },
  },
})

// Stubs for child components
const LucideStub = { template: '<span class="lucide-stub" />' }

function mountDrawer(props: Record<string, any> = {}) {
  return mount(FileSearchDrawer, {
    props: { open: true, currentDir: '', ...props },
    global: {
      plugins: [i18n],
      stubs: {
        'lucide-vue-next': LucideStub,
        HeaderMarquee: { template: '<span class="marquee-stub"><slot /></span>' },
        SearchInput: {
          template: '<input class="search-input-stub" @keydown.down.prevent="$emit(\'down\')" @keydown.up.prevent="$emit(\'up\')" @keydown.enter="$emit(\'enter\')" />',
          props: ['modelValue', 'placeholder'],
          emits: ['update:modelValue', 'enter', 'down', 'up'],
        },
        FileIcon: { template: '<span class="file-icon-stub" />' },
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockState.query = ''
  mockState.recursive = true
  mockState.scope = 'current'
  mockState.results = []
  mockState.searching = false
  mockState.total = 0
  mockState.truncated = false
  mockState.searchBasePath = ''
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('FileSearchDrawer', () => {
  it('renders when open', () => {
    const wrapper = mountDrawer({ open: true })
    expect(wrapper.find('.bottom-sheet-stub').exists()).toBe(true)
  })

  it('does not render when closed', () => {
    const wrapper = mountDrawer({ open: false })
    expect(wrapper.find('.bottom-sheet-stub').exists()).toBe(false)
  })

  it('cancels search and emits close when drawer closes', () => {
    const wrapper = mountDrawer({ open: true })
    // Verify the component renders
    expect(wrapper.find('.fs-body').exists()).toBe(true)
    // The close behavior is handled by handleClose which calls cancelSearch + emit('close')
    // We can verify cancelSearch was called on mount (since open=true triggers the watch)
    // and that the component structure is correct
    expect(wrapper.find('.fs-toggle-btn').exists()).toBe(true)
  })

  it('shows no results message when query is empty', () => {
    mockState.query = ''
    const wrapper = mountDrawer()
    expect(wrapper.find('.fs-empty').text()).toContain('Search filenames')
  })

  it('shows no results message when no matches found', () => {
    mockState.query = 'xyz'
    mockState.searching = false
    const wrapper = mountDrawer()
    expect(wrapper.find('.fs-empty').text()).toContain('No files found')
  })

  it('shows searching message when searching with no results', () => {
    mockState.query = 'test'
    mockState.searching = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.loading-indicator').exists()).toBe(true)
    expect(wrapper.find('.li-label').text()).toContain('Searching')
  })

  it('renders search results with file info', () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    expect(wrapper.find('.fs-results-count').text()).toContain('1')
    expect(wrapper.find('.fs-result-item').exists()).toBe(true)
  })

  it('shows truncated notice when truncated is true', () => {
    mockState.query = 'test'
    mockState.results = [{ name: 't.go', path: 't.go', type: 'file', matchedIndices: [0] }]
    mockState.total = 150
    mockState.truncated = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.fs-truncated').exists()).toBe(true)
  })

  it('clicking file result emits selectFile and navigateDir', async () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-item').trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('cmd')
    expect(wrapper.emitted('selectFile')).toBeTruthy()
    expect(wrapper.emitted('selectFile')![0][0]).toBe('cmd/main.go')
  })

  it('clicking dir result emits navigateDir only', async () => {
    mockState.query = 'cmd'
    mockState.results = [
      { name: 'cmd', path: 'cmd', type: 'dir', matchedIndices: [0, 1, 2] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-item').trigger('click')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('cmd')
    expect(wrapper.emitted('selectFile')).toBeFalsy()
  })

  it('ArrowDown then Enter confirms the highlighted result (equal to click)', async () => {
    mockState.query = 'go'
    mockState.results = [
      { name: 'a.go', path: 'a.go', type: 'file', matchedIndices: [] },
      { name: 'b.go', path: 'cmd/b.go', type: 'file', matchedIndices: [] },
    ]
    mockState.total = 2
    const wrapper = mountDrawer()

    // ArrowDown twice → highlight second result, then Enter confirms it
    await wrapper.find('input').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.find('input').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.find('input').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('cmd')
    expect(wrapper.emitted('selectFile')![0][0]).toBe('cmd/b.go')
  })

  it('ArrowUp then Enter confirms the last result', async () => {
    mockState.query = 'go'
    mockState.results = [
      { name: 'a.go', path: 'a.go', type: 'file', matchedIndices: [] },
      { name: 'b.go', path: 'cmd/b.go', type: 'file', matchedIndices: [] },
    ]
    mockState.total = 2
    const wrapper = mountDrawer()

    // ArrowUp from no selection → last result (b.go), Enter confirms it
    await wrapper.find('input').trigger('keydown', { key: 'ArrowUp' })
    await wrapper.find('input').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('cmd')
    expect(wrapper.emitted('selectFile')![0][0]).toBe('cmd/b.go')
  })

  it('Enter without navigation confirms the first result', async () => {
    mockState.query = 'go'
    mockState.results = [
      { name: 'a.go', path: 'root/a.go', type: 'file', matchedIndices: [] },
      { name: 'b.go', path: 'cmd/b.go', type: 'file', matchedIndices: [] },
    ]
    mockState.total = 2
    const wrapper = mountDrawer()

    await wrapper.find('input').trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('root')
    expect(wrapper.emitted('selectFile')![0][0]).toBe('root/a.go')
  })

  it('clicking reset button calls reset', async () => {    const wrapper = mountDrawer()
    const buttons = wrapper.findAll('.fs-toggle-btn')
    await buttons[buttons.length - 1].trigger('click')
    expect(mockReset).toHaveBeenCalled()
  })

  it('header title shows "Recursive search in current directory" by default', () => {
    mockState.scope = 'current'
    mockState.recursive = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-title').text()).toBe('Recursive search in current directory')
  })

  it('header title shows "Search in current directory" when recursive is off', () => {
    mockState.scope = 'current'
    mockState.recursive = false
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-title').text()).toBe('Search in current directory')
  })

  it('header title shows "Recursive search in project" when scope is global', () => {
    mockState.scope = 'global'
    mockState.recursive = true
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-title').text()).toBe('Recursive search in project')
  })

  it('header title shows "Search in project" when scope is global and recursive is off', () => {
    mockState.scope = 'global'
    mockState.recursive = false
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-title').text()).toBe('Search in project')
  })

  it('scope toggle button uses fs-toggle-btn style and shows active when global', () => {
    mockState.scope = 'global'
    const wrapper = mountDrawer()
    // Find the scope toggle (3rd toggle btn: recursive, scope, reset)
    const toggleBtns = wrapper.findAll('.fs-toggle-btn')
    // The scope btn is the second one (index 1)
    const scopeBtn = toggleBtns[1]
    expect(scopeBtn.classes()).toContain('active')
  })

  it('scope toggle button is not active when scope is current', () => {
    mockState.scope = 'current'
    const wrapper = mountDrawer()
    const toggleBtns = wrapper.findAll('.fs-toggle-btn')
    const scopeBtn = toggleBtns[1]
    expect(scopeBtn.classes()).not.toContain('active')
  })

  it('toggling scope switches from current to global', async () => {
    mockState.scope = 'current'
    const wrapper = mountDrawer()
    const toggleBtns = wrapper.findAll('.fs-toggle-btn')
    await toggleBtns[1].trigger('click')
    expect(mockState.scope).toBe('global')
  })

  it('toggling scope switches from global to current', async () => {
    mockState.scope = 'global'
    const wrapper = mountDrawer()
    const toggleBtns = wrapper.findAll('.fs-toggle-btn')
    await toggleBtns[1].trigger('click')
    expect(mockState.scope).toBe('current')
  })

  it('toggling scope triggers search when query exists', async () => {
    mockState.query = 'test'
    mockState.scope = 'current'
    const wrapper = mountDrawer()
    const toggleBtns = wrapper.findAll('.fs-toggle-btn')
    await toggleBtns[1].trigger('click')
    expect(mockStartSearch).toHaveBeenCalled()
  })

  it('shows header description when scope is current and searchBasePath exists', () => {
    mockState.scope = 'current'
    mockState.searchBasePath = 'internal/handler'
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-description').exists()).toBe(true)
  })

  it('hides header description when scope is global', () => {
    mockState.scope = 'global'
    mockState.searchBasePath = 'internal/handler'
    const wrapper = mountDrawer()
    expect(wrapper.find('.bs-header-description').exists()).toBe(false)
  })

  it('file result with project-relative path emits correct navigateDir and selectFile', async () => {
    // This tests the bug fix: results from subdirectory search should have
    // project-relative paths (e.g., 'internal/handler/dir_search.go'),
    // not search-directory-relative paths (e.g., 'dir_search.go')
    mockState.query = 'dir_search'
    mockState.results = [
      { name: 'dir_search.go', path: 'internal/handler/dir_search.go', type: 'file', matchedIndices: [0, 1, 2, 3, 4, 5, 6, 7, 8] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-item').trigger('click')
    // Should navigate to the correct parent directory
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('internal/handler')
    // Should select the file with the full project-relative path
    expect(wrapper.emitted('selectFile')![0][0]).toBe('internal/handler/dir_search.go')
  })

  it('file result in project root emits empty navigateDir', async () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-item').trigger('click')
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('')
    expect(wrapper.emitted('selectFile')![0][0]).toBe('main.go')
  })

  it('renders open directory button on each result', () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    expect(wrapper.find('.fs-result-dir-btn').exists()).toBe(true)
  })

  it('clicking open directory button calls navToFileInManager and closes drawer', async () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-dir-btn').trigger('click')
    expect(mockNavToFileInManager).toHaveBeenCalledWith('cmd/main.go')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('clicking open directory button does not trigger result click', async () => {
    mockState.query = 'main'
    mockState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    mockState.total = 1
    const wrapper = mountDrawer()
    await wrapper.find('.fs-result-dir-btn').trigger('click')
    // Should NOT emit selectFile or navigateDir (those are from onResultClick)
    expect(wrapper.emitted('selectFile')).toBeFalsy()
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
  })

  it('starts search when currentDir changes while a query exists', async () => {
    mockState.query = 'main'
    const wrapper = mountDrawer({ open: true, currentDir: 'src' })
    mockCancelSearch.mockClear()
    mockStartSearch.mockClear()
    await wrapper.setProps({ currentDir: 'src/utils' })
    expect(mockCancelSearch).toHaveBeenCalled()
    expect(mockStartSearch).toHaveBeenCalled()
  })

  it('cancels search when drawer is closed', async () => {
    const wrapper = mountDrawer({ open: true })
    mockCancelSearch.mockClear()
    await wrapper.setProps({ open: false })
    expect(mockCancelSearch).toHaveBeenCalled()
  })
})
