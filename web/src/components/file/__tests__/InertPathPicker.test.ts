import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick, reactive } from 'vue'
import InertPathPicker from '../InertPathPicker.vue'
import {
  openInertPathPicker,
  closeInertPathPicker,
  inertPathPickerState,
  _resetInertPathPickerForTesting,
} from '@/composables/useInertPathPicker'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockOpenFilePath = vi.fn().mockResolvedValue(true)
vi.mock('@/composables/useFilePathAnnotation', () => ({
  openFilePath: (...args: unknown[]) => mockOpenFilePath(...args),
}))

// Drive the panel against a controllable search state. It must be reactive:
// the component watches `search.state.results`.
const searchState = reactive({
  query: '',
  recursive: false,
  scope: 'current' as 'current' | 'global',
  exact: false,
  results: [] as Array<{ name: string; path: string; type: 'dir' | 'file' | 'image'; matchedIndices: number[] }>,
  searching: false,
  total: 0,
  truncated: false,
  searchBasePath: '',
})

const mockStartSearch = vi.fn()
const mockCancelSearch = vi.fn()
const mockReset = vi.fn()

vi.mock('@/composables/useFileSearch', () => ({
  useFileSearch: () => ({
    state: searchState,
    effectiveDir: { value: '' },
    effectiveRecursive: { value: true },
    startSearch: mockStartSearch,
    cancelSearch: mockCancelSearch,
    reset: mockReset,
    getDisplayLimit: () => 100,
  }),
}))

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
          resultCountPlus: '{limit}+ files found',
          inertHeading: 'Find “{name}”',
          inertHint: 'This path could not be resolved. Pick the matching file:',
          inertNoResults: 'No file named “{name}” in the project',
          inertNoResultsHint: 'It may have been renamed or moved.',
        },
      },
    },
  },
})

const TeleportStub = { template: '<div><slot /></div>' }
const BottomSheetStub = {
  props: { open: Boolean, panelClass: String },
  template: '<div v-if="open" class="bs-stub"><slot name="header" /><slot /></div>',
}
const FileIconStub = { props: ['path', 'size', 'isDir'], template: '<i class="icon-stub" />' }
const LoadingIndicatorStub = { props: ['label', 'size'], template: '<div class="loading-stub" />' }

function mountPanel() {
  return mount(InertPathPicker, {
    global: {
      stubs: {
        Teleport: TeleportStub,
        BottomSheet: BottomSheetStub,
        FileIcon: FileIconStub,
        LoadingIndicator: LoadingIndicatorStub,
      },
      plugins: [i18n],
    },
  })
}

function resetSearch() {
  Object.assign(searchState, {
    query: '',
    recursive: false,
    scope: 'current',
    exact: false,
    results: [],
    searching: false,
    total: 0,
    truncated: false,
    searchBasePath: '',
  })
}

beforeEach(() => {
  _resetInertPathPickerForTesting()
  resetSearch()
  vi.clearAllMocks()
})

afterEach(() => {
  closeInertPathPicker()
})

describe('InertPathPicker', () => {
  it('stays hidden until the click layer opens it', () => {
    const wrapper = mountPanel()
    expect(wrapper.find('.bs-stub').exists()).toBe(false)
  })

  it('is not gated on the chat tab (chips are cross-surface)', async () => {
    // An inert chip can be clicked in the tasks or forge tab. The panel must NOT
    // go through useTabDrawer: that gates effectiveOpen on
    // `currentTab === tabId` on narrow screens, so a hard-coded tab id would
    // silently swallow the panel everywhere else.
    const { _setWideScreenForTest } = await import('@/composables/useWideScreenLayout')
    const { onTabSwitch } = await import('@/composables/useTabDrawer')
    _setWideScreenForTest(false)
    onTabSwitch('tasks')

    const wrapper = mountPanel()
    openInertPathPicker({ path: 'src/main.go' })
    await nextTick()

    expect(wrapper.find('.bs-stub').exists()).toBe(true)
  })

  it('searches globally with exact match, bypassing the debounce', async () => {
    mountPanel()
    openInertPathPicker({ path: 'internal/service/chat_history.go' })
    await nextTick()

    // The whole point: a wrong directory prefix must not matter, so the search
    // is project-wide and matches on the basename.
    expect(searchState.scope).toBe('global')
    expect(searchState.exact).toBe(true)
    expect(searchState.query).toBe('chat_history.go')
    // `true` = immediate: the query came from a click, not from typing.
    expect(mockStartSearch).toHaveBeenCalledWith('', true)
  })

  it('renders the candidate list with name and directory', async () => {
    const wrapper = mountPanel()
    searchState.results = [
      { name: 'chat_history.go', path: 'internal/service/history/chat_history.go', type: 'file', matchedIndices: [] },
      { name: 'chat_history.go', path: 'internal/legacy/chat_history.go', type: 'file', matchedIndices: [] },
    ]
    openInertPathPicker({ path: 'internal/service/chat_history.go' })
    await nextTick()

    const items = wrapper.findAll('.ip-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('chat_history.go')
    expect(items[0].text()).toContain('internal/service/history')
  })

  it('opens the chosen candidate at the stashed line target', async () => {
    const wrapper = mountPanel()
    searchState.results = [
      { name: 'chat_history.go', path: 'internal/service/history/chat_history.go', type: 'file', matchedIndices: [] },
    ]
    openInertPathPicker({
      path: 'internal/service/chat_history.go',
      lineStart: 42,
      lineEnd: 48,
      source: 'chat',
    })
    await nextTick()

    await wrapper.find('.ip-item').trigger('click')

    // The original chip pointed at :42 — the picker must carry that through so
    // the user lands on the intended line, not the top of the file.
    expect(mockOpenFilePath).toHaveBeenCalledWith(
      'internal/service/history/chat_history.go',
      42,
      48,
      'chat',
      undefined,
    )
  })

  it('carries a multi-range target through as the serialized list', async () => {
    const wrapper = mountPanel()
    searchState.results = [
      { name: 'main.go', path: 'src/main.go', type: 'file', matchedIndices: [] },
    ]
    openInertPathPicker({ path: 'src/main.go', lineStart: 90, lineEnd: 91, lineRanges: '90-91,309' })
    await nextTick()

    await wrapper.find('.ip-item').trigger('click')

    expect(mockOpenFilePath).toHaveBeenCalledWith('src/main.go', 90, 91, undefined, '90-91,309')
  })

  it('shows an empty state when nothing matches the filename', async () => {
    const wrapper = mountPanel()
    openInertPathPicker({ path: 'src/renamed_away.go' })
    await nextTick()

    expect(wrapper.find('.ip-item').exists()).toBe(false)
    expect(wrapper.find('.ip-empty').text()).toContain('renamed_away.go')
  })

  it('shows a loading state while the walk is in flight', async () => {
    const wrapper = mountPanel()
    searchState.searching = true
    openInertPathPicker({ path: 'src/main.go' })
    await nextTick()

    expect(wrapper.find('.ip-loading').exists()).toBe(true)
  })

  it('shows the truncated hint when the result set is capped', async () => {
    const wrapper = mountPanel()
    searchState.results = [
      { name: 'main.go', path: 'a/main.go', type: 'file', matchedIndices: [] },
    ]
    searchState.truncated = true
    openInertPathPicker({ path: 'src/main.go' })
    await nextTick()

    expect(wrapper.find('.ip-more').text()).toContain('100+')
  })

  it('cancels the search and clears state on close', async () => {
    const wrapper = mountPanel()
    openInertPathPicker({ path: 'src/main.go' })
    await nextTick()

    closeInertPathPicker()
    await nextTick()

    expect(mockCancelSearch).toHaveBeenCalled()
    expect(mockReset).toHaveBeenCalled()
    expect(wrapper.find('.bs-stub').exists()).toBe(false)
  })

  it('keeps the stashed target until the candidate is picked', async () => {
    // openCandidate reads the target BEFORE closing; if close ran first the
    // line number would be lost and every jump would land on line 1.
    const wrapper = mountPanel()
    searchState.results = [
      { name: 'main.go', path: 'src/main.go', type: 'file', matchedIndices: [] },
    ]
    openInertPathPicker({ path: 'src/main.go', lineStart: 7 })
    await nextTick()

    await wrapper.find('.ip-item').trigger('click')

    expect(mockOpenFilePath).toHaveBeenCalledWith('src/main.go', 7, undefined, undefined, undefined)
    // And the panel is dismissed as part of the jump.
    expect(inertPathPickerState.open).toBe(false)
  })
})
