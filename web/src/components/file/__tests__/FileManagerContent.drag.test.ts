/**
 * Drag-and-drop upload / move and the dragstart attach payload.
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

describe('FileManagerContent — drag-and-drop move (PC)', () => {
  const movedCalls: { path: string; dest: string }[] = []

  // One mount for the block: each case here starts from a fresh selection and
  // only needs the SAME empty fixture. Mounting per case cost ~140ms a time and
  // this was the slowest block in the old file. The selection is cleared in
  // beforeEach, so no case inherits another's multi-select.
  let wrapper: ReturnType<typeof mountContent>

  beforeEach(() => {
    movedCalls.length = 0
    vi.stubGlobal('fetch', vi.fn(async (url: any, opts: any) => {
      if (String(url).endsWith('/api/file/move')) {
        movedCalls.push(JSON.parse(opts.body))
      }
      return { ok: true, status: 200, text: async () => '' }
    }))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('moves a dragged file into the directory it is dropped on', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()

    const dt = { setData: vi.fn(), setDragImage: vi.fn() }
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })
    await wrapper.find('.dir-item').trigger('drop', { dataTransfer: { files: [] } })
    await nextTick()

    expect(movedCalls).toHaveLength(1)
    expect(movedCalls[0]).toEqual({ path: 'test.ts', dest: 'src/test.ts' })
    expect(wrapper.emitted('refresh')).toBeTruthy()
  })

  it('moves all selected items when dragging from a Ctrl multi-selection', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()

    // Build a Ctrl multi-selection of two files
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await nextTick()

    const dt = { setData: vi.fn(), setDragImage: vi.fn() }
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })
    await wrapper.find('.dir-item').trigger('drop', { dataTransfer: { files: [] } })
    await nextTick()

    const moved = movedCalls.map(c => c.path).sort()
    expect(moved).toEqual(['readme.md', 'test.ts'])
  })

  it('skips moving a directory into itself (self-nesting guard)', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()

    const dt = { setData: vi.fn(), setDragImage: vi.fn() }
    // Drag the "src" directory and drop it onto itself
    await wrapper.find('.dir-item').trigger('dragstart', { dataTransfer: dt })
    await wrapper.find('.dir-item').trigger('drop', { dataTransfer: { files: [] } })
    await nextTick()

    expect(movedCalls).toHaveLength(0)
  })
})

// ── Clipboard paste upload ──

describe('FileManagerContent — dragstart attach payload', () => {
  // Each dragstart installs a 5s ghost safety timer; clear it so the test file
  // does not report leaked timers.
  afterEach(() => { cleanupDragGhost() })

  /** dataTransfer stand-in that records what the dragstart handler writes. */
  function makeDT() {
    const store: Record<string, string> = {}
    return {
      setData: vi.fn((type: string, value: string) => { store[type] = value }),
      setDragImage: vi.fn(),
      effectAllowed: '',
      getData: (type: string) => store[type] ?? '',
      _store: store,
    }
  }

  it('writes only the single item when nothing is multi-selected', async () => {
    const wrapper = mountContent()
    await nextTick()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })

    const payload = JSON.parse(dt.getData('application/x-clawbench-attach'))
    expect(payload).toEqual({ path: 'test.ts', isDir: false })
    // No `entries` key at all — the single-item shape must stay unchanged.
    expect('entries' in payload).toBe(false)
  })

  it('writes the whole selection, tagging directories via metaForPath', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()
    // Ctrl+click builds a multi-selection that includes the "src" directory.
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.dir-item').trigger('click', { ctrlKey: true })
    await nextTick()

    const dt = makeDT()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })

    const payload = JSON.parse(dt.getData('application/x-clawbench-attach'))
    expect(payload.path).toBe('test.ts')
    expect(payload.entries).toHaveLength(3)
    expect(payload.entries).toEqual(expect.arrayContaining([
      { path: 'test.ts', isDir: false },
      { path: 'readme.md', isDir: false },
      { path: 'src', isDir: true },
    ]))
  })

  it('keeps the multi-selection active after dragging to chat', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await nextTick()

    const dt = makeDT()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })
    await nextTick()

    // The user can still run batch delete/archive right after the drop.
    expect(wrapper.vm.multiSelectState.active).toBe(true)
    expect(wrapper.vm.multiSelectState.selected.size).toBe(2)
  })

  it('labels the drag ghost with the selection count', async () => {
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent()
    await nextTick()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="readme.md"]').trigger('click', { ctrlKey: true })
    await nextTick()

    const dt = makeDT()
    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })

    const ghost = document.querySelector('[data-attach-ghost]')
    expect(ghost?.textContent).toContain('已选 2 项')
  })

  it('labels the drag ghost with the file name for a single item', async () => {
    const wrapper = mountContent()
    await nextTick()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })

    const ghost = document.querySelector('[data-attach-ghost]')
    expect(ghost?.textContent).toContain('test.ts')
  })

  // ── Search results ──
  // Search hits are ordinary rows in the same listing, so they must be
  // draggable to chat too. Only the internal MOVE affordance is withheld (the
  // listing no longer represents the browsed directory).

  /** Put the manager into search mode with two hits, one of them a directory. */
  async function mountSearchResults() {
    mocks.searchState.query = 'main'
    mocks.searchState.results = [
      { name: 'main.go', path: 'cmd/main.go', type: 'file', matchedIndices: [] },
      { name: 'lib', path: 'pkg/lib', type: 'dir', matchedIndices: [] },
    ]
    const wrapper = mountContent()
    await nextTick()
    return wrapper
  }

  it('keeps search-result rows draggable', async () => {
    const wrapper = await mountSearchResults()
    const rows = wrapper.findAll('.file-item')
    expect(rows.length).toBe(2)
    for (const row of rows) expect(row.attributes('draggable')).toBe('true')
  })

  it('writes the attach payload for a search-result file', async () => {
    const wrapper = await mountSearchResults()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="cmd/main.go"]').trigger('dragstart', { dataTransfer: dt })

    const payload = JSON.parse(dt.getData('application/x-clawbench-attach'))
    expect(payload).toEqual({ path: 'cmd/main.go', isDir: false })
    // Attaching is a copy — no internal move is armed while searching.
    expect(dt.effectAllowed).toBe('copy')
    expect(wrapper.vm.dragSourcePaths).toBeNull()
  })

  it('tags a search-result directory from the hit, not the browsed listing', async () => {
    // The browsed directory also holds a "lib" FILE; the hit's own type must
    // win, otherwise the chat gets a file reference for a folder. Only the
    // multi-selection path resolves types through metaForPath, so this needs
    // two selected hits.
    mocks.mockIsTouchPrimary.value = false
    const wrapper = mountContent({ entries: [{ name: 'lib', type: 'file', size: 1 }] })
    mocks.searchState.query = 'lib'
    mocks.searchState.results = [
      { name: 'lib', path: 'pkg/lib', type: 'dir', matchedIndices: [] },
      { name: 'lib.go', path: 'pkg/lib.go', type: 'file', matchedIndices: [] },
    ]
    await nextTick()
    await wrapper.find('.file-item[data-path="pkg/lib"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="pkg/lib.go"]').trigger('click', { ctrlKey: true })
    await nextTick()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="pkg/lib"]').trigger('dragstart', { dataTransfer: dt })

    const payload = JSON.parse(dt.getData('application/x-clawbench-attach'))
    expect(payload.entries).toEqual(expect.arrayContaining([
      { path: 'pkg/lib', isDir: true },
      { path: 'pkg/lib.go', isDir: false },
    ]))
  })

  it('carries the whole multi-selection when dragging a selected search hit', async () => {
    // Ctrl+click is the desktop multi-select gesture; touch taps would open the
    // hit (a directory tap clears the query) instead of accumulating.
    mocks.mockIsTouchPrimary.value = false
    const wrapper = await mountSearchResults()
    await wrapper.find('.file-item[data-path="cmd/main.go"]').trigger('click', { ctrlKey: true })
    await wrapper.find('.file-item[data-path="pkg/lib"]').trigger('click', { ctrlKey: true })
    await nextTick()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="cmd/main.go"]').trigger('dragstart', { dataTransfer: dt })

    const payload = JSON.parse(dt.getData('application/x-clawbench-attach'))
    expect(payload.entries).toEqual(expect.arrayContaining([
      { path: 'cmd/main.go', isDir: false },
      { path: 'pkg/lib', isDir: true },
    ]))
    expect(wrapper.vm.dragSourcePaths).toBeNull()
  })

  it('still arms an internal move drag for browsed rows', async () => {
    const wrapper = mountContent()
    await nextTick()
    const dt = makeDT()

    await wrapper.find('.file-item[data-path="test.ts"]').trigger('dragstart', { dataTransfer: dt })

    expect(wrapper.vm.dragSourcePaths).toEqual(['test.ts'])
    expect(dt.effectAllowed).toBe('move')
  })
})

// ── Dropdown positioning & close ──
