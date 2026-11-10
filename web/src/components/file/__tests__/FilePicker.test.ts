import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { computed, h, reactive, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'

// ── Icon stubs (the real set is large and irrelevant here) ──
vi.mock('lucide-vue-next', () => ({
  ArrowLeft: { name: 'ArrowLeft', render: () => h('span', { class: 'i-arrow-left' }) },
  Check: { name: 'Check', render: () => h('span', { class: 'i-check' }) },
  FileX: { name: 'FileX', render: () => h('span', { class: 'i-file-x' }) },
  FolderTree: { name: 'FolderTree', render: () => h('span', { class: 'i-folder-tree' }) },
  Globe: { name: 'Globe', render: () => h('span', { class: 'i-globe' }) },
  Plus: { name: 'Plus', render: () => h('span', { class: 'i-plus' }) },
  WholeWord: { name: 'WholeWord', render: () => h('span', { class: 'i-whole-word' }) },
  Search: { name: 'Search', render: () => h('span', { class: 'i-search' }) },
  X: { name: 'X', render: () => h('span', { class: 'i-x' }) },
}))

const apiGet = vi.fn()
vi.mock('@/utils/api', () => ({ apiGet: (...args: unknown[]) => apiGet(...args) }))

// The picker must never write the file manager's location; the store is read
// only for `projectRoot`.
const storeState = reactive({ projectRoot: '/proj', currentDir: 'untouched', dirEntries: [] as unknown[] })
vi.mock('@/stores/app', () => ({
  store: {
    // Getter, not a snapshot: the factory is hoisted above `storeState`'s
    // initializer, and this is only read once a component mounts.
    get state() { return storeState },
    loadFiles: vi.fn(),
  },
}))

vi.mock('@/components/file/DirBreadcrumb.vue', () => ({
  default: {
    name: 'DirBreadcrumb',
    props: { path: String },
    emits: ['navigate'],
    template: '<div class="breadcrumb-stub" :data-path="path" />',
  },
}))

vi.mock('@/components/common/SearchInput.vue', () => ({
  default: {
    name: 'SearchInput',
    props: { modelValue: String, placeholder: String },
    emits: ['update:modelValue', 'enter', 'down', 'up'],
    template: '<input class="search-stub" :value="modelValue" :placeholder="placeholder" @input="$emit(\'update:modelValue\', $event.target.value)" @keydown.enter="$emit(\'enter\')" @keydown.down="$emit(\'down\')" @keydown.up="$emit(\'up\')" />',
  },
}))

vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { name: 'FileIcon', props: ['path', 'isDir', 'size'], template: '<span class="icon-stub" />' },
}))

vi.mock('@/components/common/PanelSkeleton.vue', () => ({
  default: { name: 'PanelSkeleton', template: '<div class="skeleton-stub" />' },
}))

vi.mock('@/utils/fileIcon', () => ({
  buildPathThumbUrl: (path: string) => `/thumb?p=${encodeURIComponent(path)}`,
}))
vi.mock('@/utils/fileManager', () => ({ isThumbableExt: () => false }))

// ── Search: a controllable stand-in for the SSE-backed composable ──
const searchState = reactive({
  query: '',
  recursive: false,
  scope: 'current' as 'current' | 'global',
  exact: false,
  results: [] as Array<{ name: string; path: string; type: 'dir' | 'file'; matchedIndices: number[] }>,
  searching: false,
  total: 0,
  truncated: false,
  searchBasePath: '',
})
const startSearch = vi.fn()
const cancelSearch = vi.fn()
vi.mock('@/composables/useFileSearch', () => ({
  useFileSearch: () => ({
    state: searchState,
    effectiveDir: computed(() => searchState.searchBasePath),
    effectiveRecursive: computed(() => searchState.scope === 'global' || searchState.recursive),
    startSearch,
    cancelSearch,
    reset: () => {
      searchState.query = ''
      searchState.results = []
      searchState.total = 0
      searchState.truncated = false
      searchState.searchBasePath = ''
    },
    getDisplayLimit: () => 100,
  }),
}))

import FilePicker from '../FilePicker.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  missingWarn: false,
  fallbackWarn: false,
  messages: {
    en: {
      common: { loading: 'Loading' },
      file: {
        nav: { parentDir: 'Up one level' },
        emptyDir: 'This folder is empty',
        search: {
          noResults: 'No files found',
          resultCount: '{count} files',
          resultCountPlus: '{limit}+ files',
          exact: 'Exact',
          recursive: 'Recursive',
          scopeGlobal: 'Global search',
          wordExact: 'exact',
          wordRecursive: 'recursive',
          wordCurrent: 'in this folder',
          wordGlobal: 'in this project',
          wordVerb: 'Search ',
        },
      },
      chat: {
        attach: {
          currentFile: 'File',
          currentDir: 'Dir',
          pickDir: 'Attach folder',
          unpickDir: 'Deselect folder',
        },
      },
    },
  },
})

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(FilePicker, {
    props: { open: true, selected: [], ...props },
    global: { plugins: [i18n] },
  })
}

function rows(wrapper: ReturnType<typeof mountPicker>) {
  return wrapper.findAll('.fp-row[data-path]')
}

beforeEach(() => {
  apiGet.mockReset()
  startSearch.mockReset()
  cancelSearch.mockReset()
  storeState.projectRoot = '/proj'
  storeState.currentDir = 'untouched'
  searchState.query = ''
  searchState.results = []
  searchState.searching = false
  searchState.total = 0
  searchState.scope = 'current'
  searchState.recursive = false
  apiGet.mockImplementation(async (url: string) => {
    if (url.includes('path=src')) {
      return {
        items: [
          { name: 'main.ts', type: 'file' },
          { name: 'utils', type: 'dir' },
          { name: 'App.vue', type: 'file' },
        ],
      }
    }
    return { items: [{ name: 'src', type: 'dir' }, { name: 'README.md', type: 'file' }] }
  })
})

describe('FilePicker — listing', () => {
  it('loads the starting directory and lists folders first, then names', async () => {
    const wrapper = mountPicker({ startDir: '' })
    await flushPromises()
    expect(apiGet).toHaveBeenCalledWith('/api/dir?path=')
    expect(rows(wrapper).map(r => r.attributes('data-path'))).toEqual(['src', 'README.md'])
  })

  it('starts in the directory it was given', async () => {
    const wrapper = mountPicker({ startDir: 'src' })
    await flushPromises()
    expect(apiGet).toHaveBeenCalledWith('/api/dir?path=src')
    expect(rows(wrapper).map(r => r.attributes('data-path'))).toEqual(['src/utils', 'src/App.vue', 'src/main.ts'])
  })

  it('browses an absolute path outside the project through /api/projects', async () => {
    mountPicker({ startDir: '/var/log' })
    await flushPromises()
    expect(apiGet).toHaveBeenCalledWith('/api/projects?path=%2Fvar%2Flog')
  })

  it('never moves the file manager: the global store is left alone', async () => {
    // The whole reason this component owns its own directory state.
    const wrapper = mountPicker({ startDir: 'src' })
    await flushPromises()
    await rows(wrapper)[0].trigger('click')
    await flushPromises()
    expect(storeState.currentDir).toBe('untouched')
  })

  it('shows the empty state for an empty directory', async () => {
    apiGet.mockResolvedValue({ items: [] })
    const wrapper = mountPicker()
    await flushPromises()
    expect(wrapper.find('[data-role="fp-empty"]').text()).toContain('This folder is empty')
  })

  it('keeps the previous listing when a request fails', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    const before = rows(wrapper).length
    apiGet.mockRejectedValueOnce(new Error('boom'))
    await rows(wrapper)[0].trigger('click')
    await flushPromises()
    expect(rows(wrapper)).toHaveLength(before)
  })
})

describe('FilePicker — navigation and selection', () => {
  it('enters a folder on row click instead of selecting it', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('[data-role="fp-dir"]').trigger('click')
    await flushPromises()
    expect(apiGet).toHaveBeenLastCalledWith('/api/dir?path=src')
    expect(wrapper.emitted('update:selected')).toBeFalsy()
  })

  it('selects a file on row click', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('[data-role="fp-file"][data-path="README.md"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([{ path: 'README.md', isDir: false }])
  })

  it('deselects a file that is already selected', async () => {
    const wrapper = mountPicker({ selected: [{ path: 'README.md' }] })
    await flushPromises()
    await wrapper.find('[data-role="fp-file"][data-path="README.md"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([])
  })

  it('selects a folder from its trailing pick button', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('[data-action="fp-pick-dir"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([{ path: 'src', isDir: true }])
    // Selecting a folder must not also navigate.
    expect(apiGet).toHaveBeenCalledTimes(1)
  })

  it('refuses to select a folder when folders are not allowed', async () => {
    const wrapper = mountPicker({ allowDir: false })
    await flushPromises()
    await wrapper.find('[data-action="fp-pick-dir"]').trigger('click')
    expect(wrapper.emitted('update:selected')).toBeFalsy()
  })

  it('preserves entries it does not own (quotes) when toggling', async () => {
    const quote = { path: '', kind: 'quote', text: 'x', note: '' }
    const wrapper = mountPicker({ selected: [quote] })
    await flushPromises()
    await wrapper.find('[data-role="fp-file"][data-path="README.md"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([quote, { path: 'README.md', isDir: false }])
  })

  it('offers the current file and directory as one-tap shortcuts', async () => {
    const wrapper = mountPicker({ startDir: 'src', currentFile: 'src/main.ts' })
    await flushPromises()
    expect(wrapper.find('[data-role="fp-current-file"]').text()).toContain('main.ts')
    expect(wrapper.find('[data-role="fp-current-dir"]').text()).toContain('src')
    await wrapper.find('[data-role="fp-current-file"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([{ path: 'src/main.ts', isDir: false }])
  })

  it('goes up one level, and cannot go above the project root', async () => {
    const wrapper = mountPicker({ startDir: 'src' })
    await flushPromises()
    expect(wrapper.find('.fp-up').attributes('disabled')).toBeUndefined()
    await wrapper.find('.fp-up').trigger('click')
    await flushPromises()
    expect(apiGet).toHaveBeenLastCalledWith('/api/dir?path=')
    expect(wrapper.find('.fp-up').attributes('disabled')).toBeDefined()
  })
})

describe('FilePicker — search', () => {
  it('searches the directory being browsed when the query changes', async () => {
    const wrapper = mountPicker({ startDir: 'src' })
    await flushPromises()
    await wrapper.find('.search-stub').setValue('main')
    expect(startSearch).toHaveBeenLastCalledWith('src')
  })

  it('replaces the listing with hits and shows where each came from', async () => {
    searchState.recursive = true
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('.search-stub').setValue('main')
    searchState.results = [{ name: 'main.ts', path: 'src/main.ts', type: 'file', matchedIndices: [] }]
    searchState.total = 1
    await flushPromises()
    const hit = wrapper.find('[data-path="src/main.ts"]')
    expect(hit.exists()).toBe(true)
    expect(hit.text()).toContain('src')
    expect(wrapper.text()).toContain('1 files')
  })

  it('selects a search hit file', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('.search-stub').setValue('main')
    searchState.results = [{ name: 'main.ts', path: 'src/main.ts', type: 'file', matchedIndices: [] }]
    await flushPromises()
    await wrapper.find('[data-path="src/main.ts"]').trigger('click')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([{ path: 'src/main.ts', isDir: false }])
  })

  it('entering a hit folder clears the query so the folder is browsable', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('.search-stub').setValue('uti')
    searchState.results = [{ name: 'utils', path: 'src/utils', type: 'dir', matchedIndices: [] }]
    await flushPromises()
    await wrapper.find('[data-path="src/utils"]').trigger('click')
    await flushPromises()
    expect(searchState.query).toBe('')
    expect(apiGet).toHaveBeenLastCalledWith('/api/dir?path=src%2Futils')
  })

  it('Enter acts on the highlighted hit', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.find('.search-stub').setValue('main')
    searchState.results = [
      { name: 'main.ts', path: 'src/main.ts', type: 'file', matchedIndices: [] },
      { name: 'main.go', path: 'src/main.go', type: 'file', matchedIndices: [] },
    ]
    await flushPromises()
    const input = wrapper.findComponent({ name: 'SearchInput' })
    await input.vm.$emit('down')
    await input.vm.$emit('down')
    await input.vm.$emit('enter')
    expect(wrapper.emitted('update:selected')![0][0]).toEqual([{ path: 'src/main.go', isDir: false }])
  })

  it('global scope is reported in the placeholder and forces recursion', async () => {
    const wrapper = mountPicker()
    await flushPromises()
    await wrapper.findAll('.fs-toggle-btn')[2].trigger('click')
    expect(searchState.scope).toBe('global')
    expect(wrapper.find('.search-stub').attributes('placeholder')).toContain('in this project')
  })
})
