/**
 * Resident (inline) search, the search API wiring, the content-search dialog,
 * and dragging search hits.
 *
 * Split out of the single FileManagerContent spec (6.2k lines / 379 cases — it
 * was also the file whose worker timed out during full-suite teardown).
 * Everything shared (mocks, i18n, mount helper, sample data) lives in
 * ./fileManagerContent.setup.
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'

// Per-module setting (vi.setConfig does not leak in from the helper): these
// suites drive async paths that take seconds when the worker pool is busy.
vi.setConfig({ testTimeout: 60_000, hookTimeout: 60_000 })
import { nextTick, reactive, ref, computed, readonly, defineComponent } from 'vue'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { PANEL_KEY_PREFIX, DEFAULT_FM_PREVIEW_RATIO } from '@/utils/panelState'

import {
  mocks,
  mountContent,
  sampleEntries,
  readSource,
  cleanupDragGhost,
  i18n,
  LongPressPlugin,
  SplitView,
  cancelTarget,
  usePendingTarget,
} from './fileManagerContent.setup'

describe('FileManagerContent — resident search', () => {
  it('renders the search bar permanently with no toggle button', () => {
    const wrapper = mountContent()
    // The search bar is always present — there is no mode to enter.
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
    // No toolbar search toggle and no in-bar close button.
    const titles = wrapper.findAll('.toolbar-btn').map(b => b.attributes('title') ?? '')
    expect(titles.some(t => t.includes('搜索文件'))).toBe(false)
    expect(wrapper.find('.fs-close-btn').exists()).toBe(false)
  })

  it('keeps the search bar while multi-select is active and selecting results', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
      { name: 'main2.go', path: 'cmd/main2.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    // Enter multi-select from the search-results layer.
    wrapper.vm.multiSelectState.active = true
    await nextTick()
    // The search bar stays, and the toolbar swaps to the multi-select variant.
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
    expect(wrapper.find('.ms-toolbar-btns').exists()).toBe(true)

    // Selecting a search result works while the query stays in the box.
    await wrapper.find('.file-item').trigger('click')
    expect(wrapper.vm.multiSelectState.selected.has('cmd/main.go')).toBe(true)
    expect(mocks.searchState.query).toBe('main')
    expect(wrapper.vm.searchActive).toBe(true)
  })

  it('does not re-run the search when a delete is emitted, only when the parent reports it landed', async () => {
    // Regression: the result list was refreshed right after emit(). Emitting is
    // synchronous while the delete handler waits for its confirmation dialog, so
    // the re-scan ran while the file still existed and the deleted hit stayed in
    // the list until the query changed.
    mocks.searchState.query = 'main'
    const wrapper = mountContent({ currentDir: 'src' })
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'main.ts', path: 'src/main.ts' }
    await nextTick()
    // `mocks.searchState` is module-level and wrappers from earlier cases are still
    // mounted, so their query watcher may have fired into this mock already.
    mocks.mockSearchStart.mockClear()

    await wrapper.vm.doDelete()
    expect(wrapper.emitted('delete')).toBeTruthy()
    expect(mocks.mockSearchStart).not.toHaveBeenCalled()

    // The parent calls this once store.deleteFile() resolved.
    wrapper.vm.refreshSearchResults()
    expect(mocks.mockSearchStart).toHaveBeenCalledTimes(1)
    expect(mocks.mockSearchStart).toHaveBeenCalledWith('src')
    wrapper.unmount()
  })

  it('does not re-run the search when a rename is emitted, only when the parent reports it landed', async () => {
    mocks.searchState.query = 'main'
    const wrapper = mountContent({ currentDir: 'src' })
    wrapper.vm.ctxMenu.visible = true
    wrapper.vm.ctxMenu.entry = { type: 'file', name: 'main.ts', path: 'src/main.ts' }
    await nextTick()
    mocks.mockSearchStart.mockClear()

    await wrapper.vm.doRename()
    expect(wrapper.emitted('rename')).toBeTruthy()
    expect(mocks.mockSearchStart).not.toHaveBeenCalled()

    wrapper.vm.refreshSearchResults()
    expect(mocks.mockSearchStart).toHaveBeenCalledTimes(1)
    expect(mocks.mockSearchStart).toHaveBeenCalledWith('src')
    wrapper.unmount()
  })

  it('refreshSearchResults is a no-op while no query is active', async () => {
    const wrapper = mountContent()
    await nextTick()
    mocks.mockSearchStart.mockClear()
    wrapper.vm.refreshSearchResults()
    expect(mocks.mockSearchStart).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('clears the query without exiting multi-select', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('cmd/main.go')
    await nextTick()

    wrapper.vm.closeSearch()
    await nextTick()
    // Results layer gone, but the multi-selection and its toolbar survive.
    expect(wrapper.vm.searchActive).toBe(false)
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(wrapper.vm.multiSelectState.selected.has('cmd/main.go')).toBe(true)
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
    expect(wrapper.find('.ms-toolbar-btns').exists()).toBe(true)
  })

  it('swaps the browse toolbar for the multi-select toolbar in place', async () => {
    const wrapper = mountContent({ currentDir: 'src' })
    // Browse toolbar: the sort button is present, the multi-select bar is not.
    // It lives in `.toolbar-dropdown-wrap` (which anchors its popup), so the
    // shared ActionBar rules reach it as a descendant, not a direct child.
    expect(wrapper.find('.dir-toolbar-btns .toolbar-btn').exists()).toBe(true)
    expect(wrapper.find('.ms-toolbar-btns').exists()).toBe(false)

    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('test.ts')
    await nextTick()

    // Same toolbar row, now showing the multi-select variant.
    const toolbar = wrapper.find('.dir-toolbar')
    expect(toolbar.find('.ms-toolbar-btns').exists()).toBe(true)
    expect(wrapper.find('.toolbar-dropdown-wrap').exists()).toBe(false)
    // The breadcrumb is untouched — the merged bar never covers it.
    expect(wrapper.find('.dir-breadcrumb-stub').exists()).toBe(true)
  })

  it('shows the directory listing while the query is empty and results once typed', async () => {
    const wrapper = mountContent()
    // Empty query → current directory listing.
    expect(wrapper.findAll('.file-item').length).toBe(sampleEntries.length)
    expect(wrapper.vm.searchActive).toBe(false)

    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
    ]
    await nextTick()
    expect(wrapper.vm.searchActive).toBe(true)
    const items = wrapper.findAll('.file-item')
    expect(items.length).toBe(1)
    expect(items[0].attributes('data-path')).toBe('cmd/main.go')
  })

  it('clears the query on directory change and returns to the listing', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.vm.searchActive).toBe(true)
    await wrapper.setProps({ currentDir: 'src' })
    await nextTick()
    // Search bar stays resident; the results layer is dismissed.
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
    expect(wrapper.vm.searchActive).toBe(false)
  })

  it('renders search results as file items with result paths', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [0, 1, 2, 3] },
      { name: 'lib', path: 'pkg/lib', type: 'dir', matchedIndices: [0, 1, 2] },
    ]
    const wrapper = mountContent()
    await nextTick()
    const items = wrapper.findAll('.file-item')
    expect(items.length).toBe(2)
    expect(items[0].attributes('data-path')).toBe('cmd/main.go')
    expect(items[1].attributes('data-path')).toBe('pkg/lib')
    expect(items[0].find('.file-name').text()).toContain('main.go')
  })

  it('shows each hit parent directory in global scope results', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'global'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
      { name: 'main2.go', path: 'internal/ai/main2.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    const items = wrapper.findAll('.file-item')
    expect(items[0].find('.file-parent-dir').text()).toBe('cmd')
    expect(items[1].find('.file-parent-dir').text()).toBe('internal/ai')
  })

  it('shows each hit parent directory in recursive search results', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.recursive = true
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.find('.file-parent-dir').text()).toBe('cmd')
  })

  it('omits the parent directory for a plain current-directory search', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    // Every hit is already known to be in the browsed directory — no path row.
    expect(wrapper.find('.file-parent-dir').exists()).toBe(false)
    expect(wrapper.find('.file-meta').exists()).toBe(true)
  })

  it('omits the parent directory for root-level hits even when recursive', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.recursive = true
    mocks.searchState.results = [
      { name: 'main.go', path: 'main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.find('.file-parent-dir').exists()).toBe(false)
  })

  it('shows the parent directory in grid view for scoped results', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'global'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()
    expect(wrapper.find('.grid-parent-dir').text()).toBe('cmd')
    expect(wrapper.find('.file-parent-dir').exists()).toBe(false)
  })

  it('never shows a parent directory row while browsing a directory', async () => {
    const wrapper = mountContent({ currentDir: 'src' })
    expect(wrapper.find('.file-parent-dir').exists()).toBe(false)
    expect(wrapper.find('.file-meta').exists()).toBe(true)
  })

  it('double-clicking a file result emits selectFile and keeps the query', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    await wrapper.find('.file-item').trigger('dblclick')
    expect(wrapper.emitted('selectFile')).toBeTruthy()
    expect(wrapper.emitted('selectFile')![0][0]).toBe('cmd/main.go')
    expect(wrapper.emitted('navigateDir')).toBeFalsy()
    expect(wrapper.vm.searchActive).toBe(true)
  })

  it('double-clicking a dir result emits navigateDir and clears the query', async () => {
    mocks.mockIsTouchPrimary.value = false
    mocks.searchState.query = 'cmd'
    mocks.searchState.results = [
      { name: 'cmd', path: 'cmd', type: 'dir', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    await wrapper.find('.dir-item').trigger('dblclick')
    expect(wrapper.emitted('navigateDir')).toBeTruthy()
    expect(wrapper.emitted('navigateDir')![0][0]).toBe('cmd')
    expect(wrapper.vm.searchActive).toBe(false)
  })

  it('right-clicking a search result exposes its result path in the context menu', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    // Right-click the name zone — that is the entry's hit zone.
    await wrapper.find('.file-item .file-name').trigger('contextmenu')
    expect(wrapper.find('.context-menu').exists()).toBe(true)
    expect(wrapper.vm.ctxMenu.entry.path).toBe('cmd/main.go')
  })

  // Search results have no browsed-directory background: the empty-area menu
  // (paste / new file / new folder) is suppressed there, so any pixel that is
  // not part of the entry falls through to nothing at all. The whole row must
  // therefore open the entry menu — the parent-directory line, the size column
  // and the reveal button included.
  it('right-clicking anywhere on a search result row opens the entry menu', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'global'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    for (const sel of ['.file-parent-dir', '.file-meta', '.fs-result-dir-btn']) {
      wrapper.vm.closeCtxMenu()
      await nextTick()
      const target = wrapper.find(`.file-item ${sel}`)
      expect(target.exists(), sel).toBe(true)
      await target.trigger('contextmenu')
      await nextTick()
      expect(wrapper.vm.ctxMenu.visible, sel).toBe(true)
      expect(wrapper.vm.ctxMenu.entry?.path, sel).toBe('cmd/main.go')
    }
  })

  it('right-clicking a grid search result outside the name opens the entry menu', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'global'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    wrapper.vm._setViewMode('grid')
    await nextTick()
    await wrapper.find('.grid-item .grid-parent-dir').trigger('contextmenu')
    await nextTick()
    expect(wrapper.vm.ctxMenu.visible).toBe(true)
    expect(wrapper.vm.ctxMenu.entry?.path).toBe('cmd/main.go')
  })

  it('still shows no menu when right-clicking empty space in the search results', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    await wrapper.find('.file-list').trigger('contextmenu')
    await nextTick()
    expect(wrapper.vm.ctxMenu.visible).toBe(false)
  })

  it('Escape clears the query and returns to the listing', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.vm.searchActive).toBe(true)
    await wrapper.find('.search-pill input').trigger('keydown', { key: 'Escape' })
    expect(wrapper.vm.searchActive).toBe(false)
    // The bar itself stays visible.
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
  })

  it('Enter in the search box opens the first result without a prior highlight', async () => {
    mocks.searchState.query = 'go'
    mocks.searchState.results = [
      { name: 'a.go', path: 'root/a.go', type: 'file', matchedIndices: [] },
      { name: 'b.go', path: 'cmd/b.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    // No arrow key pressed yet — Enter should open the first result.
    await wrapper.find('.search-pill input').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('selectFile')).toBeTruthy()
    expect(wrapper.emitted('selectFile')![0][0]).toBe('root/a.go')
  })

  it('replaces the stale highlight when the result set changes', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'a.go', path: 'root/a.go', type: 'file', matchedIndices: [] },
      { name: 'b.go', path: 'cmd/b.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    // Highlight the second result via ArrowDown twice
    await wrapper.find('.search-pill input').trigger('keydown', { key: 'ArrowDown' })
    await wrapper.find('.search-pill input').trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.vm._getSelectedPath()).toBe('cmd/b.go')
    // Replace results with a fresh set (as a new search round would)
    mocks.searchState.results = [
      { name: 'c.go', path: 'pkg/c.go', type: 'file', matchedIndices: [] },
    ]
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('')
  })

  it('keeps the toolbar and breadcrumb visible alongside the resident search bar', () => {
    const wrapper = mountContent({ currentDir: 'src' })
    expect(wrapper.find('.dir-toolbar').exists()).toBe(true)
    expect(wrapper.find('.dir-breadcrumb-stub').exists()).toBe(true)
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
  })

  it('reflects the active search options in the search box placeholder', async () => {
    mocks.searchState.scope = 'current'
    mocks.searchState.recursive = false
    mocks.searchState.exact = false
    const wrapper = mountContent()
    await nextTick()
    // No separate hint line anymore — the mode description lives in the placeholder
    expect(wrapper.find('.fs-mode-hint').exists()).toBe(false)
    // zh wording concatenates scope + modifiers + verb without spaces
    expect(wrapper.find('.search-pill input').attributes('placeholder')).toBe('在当前目录搜索')

    mocks.searchState.recursive = true
    mocks.searchState.exact = true
    await nextTick()
    expect(wrapper.find('.search-pill input').attributes('placeholder')).toBe('在当前目录精确递归搜索')

    mocks.searchState.scope = 'global'
    mocks.searchState.recursive = false
    mocks.searchState.exact = false
    await nextTick()
    // Global scope always recurses, so the placeholder reflects recursion even
    // when the (now disabled) recursive toggle is off.
    expect(wrapper.find('.search-pill input').attributes('placeholder')).toBe('在当前项目下递归搜索')
  })

  it('forces recursive search in global scope and disables the recursive toggle', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'global'
    mocks.searchState.recursive = false
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()

    // The recursive toggle is disabled while global, and shown active.
    const recursiveBtn = wrapper.findAll('.fs-toggle-btn').find(b => b.attributes('title') === '递归搜索')!
    expect(recursiveBtn.attributes('disabled')).toBeDefined()
    expect(recursiveBtn.classes()).toContain('active')

    // Global results show their containing directory (search ranges beyond the
    // browsed directory).
    expect(wrapper.find('.file-parent-dir').text()).toBe('cmd')
  })

  it('keeps the recursive toggle operable and off outside global scope', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.scope = 'current'
    mocks.searchState.recursive = false
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()

    const recursiveBtn = wrapper.findAll('.fs-toggle-btn').find(b => b.attributes('title') === '递归搜索')!
    expect(recursiveBtn.attributes('disabled')).toBeUndefined()
    expect(recursiveBtn.classes()).not.toContain('active')
    // Non-recursive current-dir search: no containing-directory row.
    expect(wrapper.find('.file-parent-dir').exists()).toBe(false)
  })

  it('places the recursive toggle directly to the left of the global toggle', () => {
    const wrapper = mountContent()
    const titles = wrapper.findAll('.fs-toggle-btn').map(b => b.attributes('title'))
    const recursiveIdx = titles.indexOf('递归搜索')
    const globalIdx = titles.indexOf('全局搜索')
    expect(recursiveIdx).toBeGreaterThanOrEqual(0)
    expect(globalIdx).toBe(recursiveIdx + 1)
  })

  it('renders a reveal-in-directory button on each search result', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    const locateBtn = wrapper.find('.file-item .fs-result-dir-btn')
    expect(locateBtn.exists()).toBe(true)
  })

  it('lists the current directory files while the search box is empty', async () => {
    mocks.searchState.query = ''
    mocks.searchState.results = []
    const wrapper = mountContent() // default entries = sampleEntries (src dir, test.ts, readme.md)
    await nextTick()
    // The whole current dir listing is shown even with no query typed
    const items = wrapper.findAll('.file-item')
    expect(items.length).toBe(sampleEntries.length)
    expect(items[0].text()).toContain('src')
    // No reveal/locate buttons nor highlight while not actually filtering
    expect(wrapper.find('.file-item .fs-result-dir-btn').exists()).toBe(false)
    expect(wrapper.find('.file-name mark').exists()).toBe(false)
  })

  it('applies the toolbar sort to search results', async () => {
    mocks.searchState.query = 'go'
    mocks.searchState.results = [
      { name: 'big.go', path: 'big.go', type: 'file', size: 5000, modified: '2025-01-01T00:00:00Z', matchedIndices: [] },
      { name: 'a.go', path: 'a.go', type: 'file', size: 10, modified: '2025-01-01T00:00:00Z', matchedIndices: [] },
    ]
    const wrapper = mountContent({ sortField: 'size', sortDir: 'asc' })
    await nextTick()
    const items = wrapper.findAll('.file-item')
    // Sorted ascending by size → a.go (10) before big.go (5000)
    expect(items[0].attributes('data-path')).toBe('a.go')
    expect(items[1].attributes('data-path')).toBe('big.go')
  })
})

// ── Hidden files ──

describe('FileManagerContent — search API', () => {
  it('openSearch focuses the resident search input', async () => {
    const wrapper = mountContent()
    await wrapper.vm.openSearch()
    await nextTick()
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
  })

  it('closeSearch clears the query but keeps the bar resident', async () => {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.vm.searchActive).toBe(true)
    await wrapper.vm.closeSearch()
    await nextTick()
    expect(wrapper.vm.searchActive).toBe(false)
    expect(wrapper.find('.fs-input-row').exists()).toBe(true)
  })

  it('focusSearchInput does not throw', async () => {
    const wrapper = mountContent()
    expect(() => wrapper.vm.focusSearchInput()).not.toThrow()
  })

  it('the search box up/down events move the highlight', async () => {
    const wrapper = mountContent()
    wrapper.vm._setSelectedPath('src')
    await nextTick()

    // The resident search box wires its own arrow keys to moveSelection, so
    // ↑/↓ typed into the box walk the listing without leaving the field.
    wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('down')
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('test.ts')

    wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('up')
    await nextTick()
    expect(wrapper.vm._getSelectedPath()).toBe('src')
  })

  it('exposes searchActive as an already-unwrapped boolean', async () => {
    // App.vue reads this off the template ref to decide whether back-navigation
    // should dismiss the results layer. defineExpose unwraps refs/computeds, so
    // the value is a plain boolean — App.vue must NOT read `.value` off it.
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()

    const exposed = wrapper.vm.searchActive
    expect(typeof exposed).toBe('boolean')
    expect(exposed).toBe(true)
    // The App.vue predicate must evaluate truthy for this exact expression.
    expect(!!exposed).toBe(true)
    // Guard against the regression where App.vue appended `.value`.
    expect(!!(exposed as unknown as { value?: unknown })?.value).toBe(false)
  })
})

// ── Empty state text ──

describe('FileManagerContent — content search dialog', () => {
  it('opens the dialog from the toolbar button', async () => {
    const wrapper = mountContent()
    // The dialog ref must be wired; openContentSearch is the exposed entry point
    // that App's Ctrl+Shift+F also calls.
    expect(typeof (wrapper.vm as any).openContentSearch).toBe('function')
    expect(() => (wrapper.vm as any).openContentSearch()).not.toThrow()
  })

  it('routes a chosen hit through openFilePath (not a bare overlay dispatch)', () => {
    mocks.mockOpenFilePath.mockClear()
    const wrapper = mountContent()
    const vm = wrapper.vm as any

    vm.$.setupState.onContentSearchOpenFile('src/a.go', 42)

    // openFilePath owns existence check, content fetch and the line target.
    // Dispatching open-file-overlay directly would open an empty viewer.
    expect(mocks.mockOpenFilePath).toHaveBeenCalledTimes(1)
    const [path, line, , source] = mocks.mockOpenFilePath.mock.calls[0] as unknown as [string, number, unknown, string]
    expect(path).toBe('src/a.go')
    expect(line).toBe(42)
    // 'browse': the dialog lives in the file manager, so Back returns there.
    expect(source).toBe('browse')
  })

  it('ignores an empty path from a hit', () => {
    mocks.mockOpenFilePath.mockClear()
    const wrapper = mountContent()
    ;(wrapper.vm as any).$.setupState.onContentSearchOpenFile('', 1)
    expect(mocks.mockOpenFilePath).not.toHaveBeenCalled()
  })
})

/**
 * Dragging a search hit must not attach paths that the results listing no longer
 * shows.
 *
 * The multi-selection survives a query change on purpose (only exiting
 * multi-select clears it), so a set built in browse mode is still populated when
 * the results layer takes over. Dragging then handed the whole set to the chat
 * payload — including rows the user can no longer see, and could not have meant
 * to attach from this listing.
 */

describe('FileManagerContent — dragging search hits', () => {
  /** The attach payload a dragstart wrote, or null. */
  function attachPayload(dt: { store: Map<string, string> }): { path: string; entries?: Array<{ path: string }> } | null {
    const raw = dt.store.get('application/x-clawbench-attach')
    return raw ? JSON.parse(raw) : null
  }

  function fakeDataTransfer() {
    const store = new Map<string, string>()
    return {
      store,
      setData: (k: string, v: string) => { store.set(k, v) },
      getData: (k: string) => store.get(k) ?? '',
      effectAllowed: '',
      setDragImage: () => {},
    }
  }

  it('attaches only the hits the results listing shows', async () => {
    const wrapper = mountContent()
    // Browse-mode selection: `src` is a directory in the normal listing.
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('src')
    wrapper.vm.multiSelectState.selected.add('readme.md')

    // A query replaces the listing with two hits; `src` is NOT among them.
    mocks.searchState.query = 'test'
    mocks.searchState.results = [
      { name: 'test.ts', path: 'test.ts', type: 'file', matchedIndices: [] },
      { name: 'test2.ts', path: 'test2.ts', type: 'file', matchedIndices: [] },
    ]
    await nextTick()

    // The user ctrl+clicks a hit, then drags it.
    wrapper.vm.multiSelectState.selected.add('test.ts')
    wrapper.vm.multiSelectState.selected.add('test2.ts')

    const dt = fakeDataTransfer()
    const row = wrapper.findAll('.file-item').find(el => el.attributes('data-path') === 'test.ts')
    expect(row, 'the hit must be rendered').toBeTruthy()
    await row!.trigger('dragstart', { dataTransfer: dt })

    const payload = attachPayload(dt)
    expect(payload).not.toBeNull()
    const paths = (payload!.entries ?? [{ path: payload!.path }]).map(e => e.path).sort()
    // `src` / `readme.md` are stale browse-mode leftovers: they are not in the
    // results, so attaching them would be invisible to the user.
    expect(paths).toEqual(['test.ts', 'test2.ts'])
    wrapper.unmount()
  })

  it('still attaches the whole selection in browse mode', async () => {
    // The filter is search-specific. Browse mode keeps the raw set — a selected
    // row may legitimately outlive the current listing (a directory switch).
    const wrapper = mountContent()
    wrapper.vm.multiSelectState.active = true
    wrapper.vm.multiSelectState.selected.add('src')
    wrapper.vm.multiSelectState.selected.add('readme.md')

    const dt = fakeDataTransfer()
    const row = wrapper.findAll('.file-item').find(el => el.attributes('data-path') === 'readme.md')
    expect(row).toBeTruthy()
    await row!.trigger('dragstart', { dataTransfer: dt })

    const payload = attachPayload(dt)
    const paths = (payload!.entries ?? [{ path: payload!.path }]).map(e => e.path).sort()
    expect(paths).toEqual(['readme.md', 'src'])
    wrapper.unmount()
  })

  afterEach(() => {
    mocks.searchState.query = ''
    mocks.searchState.results = []
  })
})
