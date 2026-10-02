/**
 * Tests for useFileRefresh deduplication logic.
 *
 * The key bug: when onFileModified and file_change SSE event both trigger
 * refreshCurrentFile concurrently, the second call would increment
 * refreshGeneration, causing the first call's stale-generation check to
 * clearFlash() — which wiped the second call's flash state.
 *
 * Fix: refreshCurrentFile now deduplicates — if a refresh is already
 * in-flight, new calls are deferred until the current one completes.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'

// ── Timer leak prevention ──

const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

const pendingIntervals: ReturnType<typeof setInterval>[] = []
const _origSetInterval = setInterval
globalThis.setInterval = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetInterval(fn, ms, ...args)
  pendingIntervals.push(id)
  return id
}) as typeof setInterval

afterEach(() => {
  for (const id of pendingTimers) {
    clearTimeout(id)
  }
  pendingTimers.length = 0
  for (const id of pendingIntervals) {
    clearInterval(id)
  }
  pendingIntervals.length = 0
})

// Mock dependencies before importing
const closeCurrentFileMock = vi.hoisted(() => vi.fn())
// The store mock mirrors the real selectFile contract: it replaces the open
// file's content with what it fetched. Tests drive content through
// globalThis.fetch, so selectFile reads the same source. Without this, the
// refresh path would observe stale content and derive no markers.
const storeMock = vi.hoisted(() => ({
  state: null as any,
  loadFiles: vi.fn(),
  selectFile: vi.fn(),
  closeCurrentFile: closeCurrentFileMock,
}))
// The real store is reactive, and useFileRefresh registers module-level watchers
// on `store.state.currentFile.path`. A plain-object mock would make those
// watchers dead, so the file-navigation tests must drive a reactive state.
vi.mock('@/stores/app.ts', async () => {
  const { reactive } = await import('vue')
  storeMock.state = reactive({ currentFile: null, currentDir: undefined })
  return { store: storeMock }
})

/** Faithful stand-in for the real store.selectFile: fetch + apply content. */
function faithfulSelectFile(path: string): Promise<boolean> {
  return (globalThis.fetch as typeof fetch)(`/api/fs/file/${encodeURIComponent(path)}`)
    .then(async (resp) => {
      if (!resp || !resp.ok) return false
      const data = await resp.json()
      if (storeMock.state.currentFile?.path === path) Object.assign(storeMock.state.currentFile, data)
      else storeMock.state.currentFile = data
      return true
    })
}

/** Restore the faithful selectFile behavior (overrides leak across tests). */
function resetStoreMock(): void {
  storeMock.selectFile.mockImplementation(faithfulSelectFile as any)
}

// Mock implementations set via mockResolvedValue/mockImplementation survive
// clearAllMocks, so restore the faithful default before every test.
beforeEach(() => {
  resetStoreMock()
})

// Shared markdown-diff state. `clearDiffMarkers` mirrors the real one (which
// empties the published refs) rather than being a no-op: the drop-to-baseline
// path relies on it actually clearing `diffMarkers`, so a no-op mock would make
// that assertion vacuous.
const mdDiffMock = vi.hoisted(() => {
  const diffMarkers = { value: [] as any[] }
  const diffOldContent = { value: null as string | null }
  const diffOldFilePath = { value: null as string | null }
  const clearDiffMarkers = vi.fn(() => {
    diffMarkers.value = []
    diffOldContent.value = null
    diffOldFilePath.value = null
  })
  return { diffMarkers, diffOldContent, diffOldFilePath, clearDiffMarkers }
})

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  computeMarkdownDiff: vi.fn(),
  offscreenExtractBlocks: vi.fn(),
  diffMarkers: mdDiffMock.diffMarkers,
  diffOldContent: mdDiffMock.diffOldContent,
  diffOldFilePath: mdDiffMock.diffOldFilePath,
  clearDiffMarkers: mdDiffMock.clearDiffMarkers,
  extractBlocks: vi.fn(),
  computeCodeDiffMarkers: vi.fn().mockReturnValue([]),
}))

vi.mock('@/utils/diffUtils.ts', () => ({
  computeDiff: vi.fn().mockReturnValue({
    deletedInOld: [],
    addedInNew: [],
    deletedChars: new Map(),
    addedChars: new Map(),
    modifiedPairs: [],
  }),
  wholeLineRanges: vi.fn((nums: number[]) => nums.map(n => ({ line: n, start: 0, end: Infinity }))),
}))

vi.mock('@/utils/fileType.ts', () => ({
  getFileType: vi.fn(),
}))

vi.mock('@/composables/useFileNavStack.ts', () => ({
  useFileNavStack: vi.fn(() => ({ removePath: vi.fn() })),
}))

// Mock editing + dialog so the external-change confirmation can be controlled.
const isEditingMock = vi.hoisted(() => vi.fn(() => false))
const isEditorDirtyMock = vi.hoisted(() => vi.fn(() => false))
vi.mock('@/composables/useFileEditor.ts', () => ({
  useFileEditor: () => ({
    editing: { value: false },
    isEditing: isEditingMock,
    isEditorDirty: isEditorDirtyMock,
    setEditing: vi.fn(),
    registerExitEditHandler: vi.fn(),
    exitEdit: vi.fn(),
    registerDirtyGetter: vi.fn(),
  }),
}))
const confirmDialogMock = vi.hoisted(() => vi.fn(() => Promise.resolve(true)))
vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    confirm: confirmDialogMock,
    prompt: vi.fn(),
    alert: vi.fn(),
    state: { value: { visible: false } },
  }),
}))

import {
  refreshCurrentFile,
  isRefreshing,
  flashRanges,
  flashType,
  markFileSaved,
  wasRecentlySaved,
  syncMarkersFor,
} from '../useFileRefresh.ts'
import { clearAllBaselines, getBaseline, recordBaseline } from '@/composables/useFileChangeBaseline.ts'
import { store } from '@/stores/app.ts'
import { computeDiff } from '@/utils/diffUtils.ts'
import { computeCodeDiffMarkers, computeMarkdownDiff, offscreenExtractBlocks, diffMarkers, diffOldContent } from '@/composables/useMarkdownDiff.ts'
import { useFileNavStack } from '@/composables/useFileNavStack.ts'

describe('useFileRefresh deduplication', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Baselines are module-level state; reset them here so no test can inherit a
    // baseline seeded by an earlier describe (a marker assertion could otherwise
    // pass for the wrong reason).
    clearAllBaselines()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
    isRefreshing.value = false
  })

  it('should set diffMarkers and diffOldContent when refresh completes', async () => {
    store.state.currentFile = {
      name: 'test.go',
      path: 'test.go',
      content: 'old content\n',
    }

    const markerData = [{
      id: 'code-modified-1-1',
      type: 'modified' as const,
      label: 'M',
      blockSelector: '',
      lineNumbers: [1],
      charDiff: null,
      ariaLabel: 'modified line 1',
    }]

    vi.mocked(computeDiff).mockReturnValue({
      deletedInOld: [],
      addedInNew: [],
      deletedChars: new Map([[1, [{ start: 0, end: 3 }]]]),
      addedChars: new Map([[1, [{ start: 0, end: 3 }]]]),
      modifiedPairs: [[1, 1]],
    })
    vi.mocked(computeCodeDiffMarkers).mockReturnValue(markerData)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'new content\n' }),
    })

    await refreshCurrentFile()

    expect(diffMarkers.value).toEqual(markerData)
    expect(diffOldContent.value).toBe('old content\n')

    globalThis.fetch = originalFetch
  })

  it('should defer concurrent refresh until current one finishes', async () => {
    store.state.currentFile = {
      name: 'test.go',
      path: 'test.go',
      content: 'v1\n',
    }
    store.state.currentDir = '.'

    // Make selectFile slow so a concurrent refresh can arrive
    let resolveSelect: () => void
    const selectPromise = new Promise<void>(r => { resolveSelect = r })
    vi.mocked(store.selectFile).mockReturnValue(selectPromise.then(() => true))

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'v2\n' }),
    })

    // Start first refresh (will block on selectFile)
    const p1 = refreshCurrentFile({ loadDir: true })
    expect(isRefreshing.value).toBe(true)

    // While first is in-flight, start second refresh with clearOnError=true
    const p2 = refreshCurrentFile({ loadDir: false, clearOnError: true })

    // Let first refresh complete
    resolveSelect!()
    await Promise.all([p1, p2])
    expect(isRefreshing.value).toBe(false)

    // Both should complete without error.
    // The deferred refresh should run after the first one.
    expect(store.selectFile.mock.calls.length).toBeGreaterThanOrEqual(1)

    globalThis.fetch = originalFetch
  })

  it('should clear flash when no diff changes exist', async () => {
    store.state.currentFile = {
      name: 'test.go',
      path: 'test.go',
      content: 'same content\n',
    }

    // newContent === oldContent, so diff is null
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'same content\n' }),
    })

    await refreshCurrentFile()

    // No diff → flash should be cleared
    expect(flashRanges.value).toEqual([])
    expect(flashType.value).toBe('add')

    globalThis.fetch = originalFetch
  })

  it('should not call selectFile when no file is open', async () => {
    store.state.currentFile = null

    await refreshCurrentFile()

    expect(store.selectFile).not.toHaveBeenCalled()
  })

  describe('syncMarkersFor (accumulated baseline)', () => {
    beforeEach(() => {
      // clearAllBaselines() now runs in the outer beforeEach (baselines are
      // module-level and must not leak across describes).
      vi.clearAllMocks()
      diffMarkers.value = []
    })

    it('derives code markers from the baseline, not the last refresh', () => {
      recordBaseline('a.go', 'line1\nline2\n')
      // computeCodeDiffMarkers is mocked to return [] by the module mock; assert
      // it was called with the BASELINE as oldContent.
      syncMarkersFor('a.go', 'code', 'line1\nCHANGED\n')
      expect(computeCodeDiffMarkers).toHaveBeenCalledWith(
        expect.anything(),
        'line1\nline2\n',
        'line1\nCHANGED\n',
      )
    })

    it('produces no markers and records no baseline when none exists', () => {
      // syncMarkersFor only DERIVES. Recording the baseline is the refresh path's
      // job (it alone knows the pre-change content); a bare sync must not seed a
      // baseline, or merely opening a file would create one.
      syncMarkersFor('b.go', 'code', 'fresh\n')
      expect(getBaseline('b.go')).toBeNull()
      expect(diffMarkers.value).toEqual([])
    })

    it('restores markers when re-syncing the same path later', () => {
      recordBaseline('c.go', 'v1\n')
      // The module mock defaults computeCodeDiffMarkers to [], which would make
      // this assertion vacuous ([] === []). Return real markers so a broken
      // restore path actually fails.
      const markers = [{
        id: 'code-modified-1-1',
        type: 'modified' as const,
        label: 'M',
        blockSelector: '',
        lineNumbers: [1],
        charDiff: null,
        ariaLabel: 'modified line 1',
      }]
      ;(computeCodeDiffMarkers as any).mockReturnValueOnce(markers)
      syncMarkersFor('c.go', 'code', 'v2\n')
      expect(diffMarkers.value).toEqual(markers)
      // Simulate navigating away then back: markers cleared, then re-synced.
      diffMarkers.value = []
      ;(computeCodeDiffMarkers as any).mockReturnValueOnce(markers)
      syncMarkersFor('c.go', 'code', 'v2\n')
      expect(diffMarkers.value).toEqual(markers)
    })

    it('publishes markdown markers from the baseline on the markdown surface', () => {
      recordBaseline('doc.md', '# old\n')
      const oldBlocks = [{ id: 'b0', tag: 'h1', text: '# old' }]
      const newBlocks = [{ id: 'b0', tag: 'h1', text: '# new' }]
      const markers = [{
        id: 'md-modified-0',
        type: 'modified' as const,
        label: 'M',
        blockSelector: '#block-0',
        charDiff: null,
        ariaLabel: 'modified block 0',
      }]
      ;(offscreenExtractBlocks as any)
        .mockReturnValueOnce(oldBlocks)
        .mockReturnValueOnce(newBlocks)
      ;(computeMarkdownDiff as any).mockReturnValueOnce({ markers, hasChanges: true })

      syncMarkersFor('doc.md', 'markdown', '# new\n')

      // Real markers must be published — the module mock returns undefined by
      // default, so comparing against [] here would be vacuous.
      expect(diffMarkers.value).toEqual(markers)
      expect(diffOldContent.value).toBe('# old\n')
      // Extracted from the BASELINE (not the current content) and the current
      // content, in that order.
      expect(offscreenExtractBlocks).toHaveBeenNthCalledWith(1, '# old\n')
      expect(offscreenExtractBlocks).toHaveBeenNthCalledWith(2, '# new\n')
    })

    it('drops markers when the content returns to the baseline', () => {
      recordBaseline('d.go', 'v1\n')
      ;(computeCodeDiffMarkers as any).mockReturnValueOnce([{
        id: 'code-modified-1-1',
        type: 'modified' as const,
        label: 'M',
        blockSelector: '',
        lineNumbers: [1],
        charDiff: null,
        ariaLabel: 'modified line 1',
      }])
      syncMarkersFor('d.go', 'code', 'v2\n')
      expect(diffMarkers.value).not.toEqual([])

      // Content back at the baseline → the marker must be cleared.
      syncMarkersFor('d.go', 'code', 'v1\n')
      expect(diffMarkers.value).toEqual([])
    })
  })
})

describe('useFileRefresh file-navigation watch', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    clearAllBaselines()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
    mdDiffMock.diffOldFilePath.value = null
    isRefreshing.value = false
    resetStoreMock()
  })

  it('clears the diff-drawer side effects (not just the markers) on file switch', async () => {
    // Regression guard: replacing clearDiffMarkers() with `diffMarkers.value = []`
    // here left diffOldContent / diffOldFilePath set and the drawer open on the
    // previous file, with a dead Undo. Assert the full side-effect set.
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'a\n' }
    diffMarkers.value = [{ id: 'x', type: 'modified', label: 'M', blockSelector: '', lineNumbers: [1], charDiff: null, ariaLabel: 'm' }]
    diffOldContent.value = 'old\n'
    mdDiffMock.diffOldFilePath.value = 'a.go'

    store.state.currentFile = { name: 'b.go', path: 'b.go', content: 'b\n' }
    await nextTick()

    expect(diffMarkers.value).toEqual([])
    expect(diffOldContent.value).toBeNull()
    expect(mdDiffMock.diffOldFilePath.value).toBeNull()
  })

  it('keeps the accumulated baseline across a file switch (markers must survive)', async () => {
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'a\n' }
    recordBaseline('a.go', 'v1\n')
    store.state.currentFile = { name: 'b.go', path: 'b.go', content: 'b\n' }
    await nextTick()
    // The baseline is what restores the markers on return; navigation must not
    // drop it.
    expect(getBaseline('a.go')).toBe('v1\n')
  })
})

describe('useFileRefresh modified-line flash', () => {
  // Use the REAL computeDiff to verify end-to-end behavior for modified lines
  // (character-level changes, not whole-line add/delete)

  beforeEach(() => {
    vi.clearAllMocks()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
    // Use real computeDiff for these tests
    vi.mocked(computeDiff).mockImplementation(
      (oldText: string, newText: string) => {
        // eslint-disable-next-line @typescript-eslint/no-require-imports
        const { diffLines, diffChars } = require('diff')
        const result = {
          deletedInOld: [] as number[],
          addedInNew: [] as number[],
          deletedChars: new Map<number, { start: number; end: number }[]>(),
          addedChars: new Map<number, { start: number; end: number }[]>(),
          modifiedPairs: [] as [number, number][],
        }

        const changes = diffLines(oldText, newText) ?? []
        let oldLine = 1, newLine = 1
        const deleteGroups: Array<{ startOld: number; lines: string[] }> = []
        const addGroups: Array<{ startNew: number; lines: string[] }> = []

        for (const change of changes) {
          const lineCount = change.count || 0
          if (change.removed) {
            deleteGroups.push({ startOld: oldLine, lines: change.value.replace(/\n$/, '').split('\n') })
            oldLine += lineCount
          } else if (change.added) {
            addGroups.push({ startNew: newLine, lines: change.value.replace(/\n$/, '').split('\n') })
            newLine += lineCount
          } else {
            oldLine += lineCount
            newLine += lineCount
          }
        }

        let di = 0, ai = 0
        while (di < deleteGroups.length && ai < addGroups.length) {
          const delG = deleteGroups[di], addG = addGroups[ai]
          const pairCount = Math.min(delG.lines.length, addG.lines.length)
          for (let i = 0; i < pairCount; i++) {
            const ol = delG.startOld + i, nl = addG.startNew + i
            if (delG.lines[i] !== addG.lines[i]) {
              result.modifiedPairs.push([ol, nl])
              result.deletedChars.set(ol, [{ start: 0, end: delG.lines[i].length }])
              result.addedChars.set(nl, [{ start: 0, end: addG.lines[i].length }])
            }
          }
          for (let i = pairCount; i < delG.lines.length; i++) result.deletedInOld.push(delG.startOld + i)
          for (let i = pairCount; i < addG.lines.length; i++) result.addedInNew.push(addG.startNew + i)
          di++; ai++
        }
        while (di < deleteGroups.length) {
          for (let i = 0; i < deleteGroups[di].lines.length; i++) result.deletedInOld.push(deleteGroups[di].startOld + i)
          di++
        }
        while (ai < addGroups.length) {
          for (let i = 0; i < addGroups[ai].lines.length; i++) result.addedInNew.push(addGroups[ai].startNew + i)
          ai++
        }
        return result
      }
    )
  })

  it('should produce deletion flash for modified lines (char-level change)', async () => {
    store.state.currentFile = {
      name: 'test.go',
      path: 'test.go',
      content: 'line1\nold line2\nline3\n',
    }

    const markerData = [{
      id: 'code-modified-2-2',
      type: 'modified' as const,
      label: 'M',
      blockSelector: '',
      lineNumbers: [2],
      charDiff: null,
      ariaLabel: 'modified line 2',
    }]
    vi.mocked(computeCodeDiffMarkers).mockReturnValue(markerData)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'line1\nnew line2\nline3\n' }),
    })

    await refreshCurrentFile()

    // After full refresh, flashType should end as 'add' (Phase 3)
    expect(flashType.value).toBe('add')
    // flashRanges should have entry for line 2 (new line number)
    expect(flashRanges.value.some(r => r.line === 2)).toBe(true)
    // diffMarkers should be set
    expect(diffMarkers.value).toEqual(markerData)

    globalThis.fetch = originalFetch
  })

  it('should produce both deletion and addition flash ranges for modified lines', async () => {
    store.state.currentFile = {
      name: 'main.go',
      path: 'main.go',
      content: 'package main\n\nfunc hello() {\n\tfmt.Println("old")\n}\n',
    }

    vi.mocked(computeCodeDiffMarkers).mockReturnValue([{
      id: 'code-modified-4-4',
      type: 'modified' as const,
      label: 'M',
      blockSelector: '',
      lineNumbers: [4],
      charDiff: null,
      ariaLabel: 'modified line 4',
    }])

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'package main\n\nfunc hello() {\n\tfmt.Println("new")\n}\n' }),
    })

    await refreshCurrentFile()

    // The diff should detect char-level change on line 4
    // After refresh, Phase 3 should set add flash on line 4
    expect(flashType.value).toBe('add')
    expect(flashRanges.value.some(r => r.line === 4)).toBe(true)
    expect(diffMarkers.value.length).toBeGreaterThan(0)

    globalThis.fetch = originalFetch
  })
})

describe('useFileRefresh clearOnError', () => {
  let removePathMock: ReturnType<typeof vi.fn>

  beforeEach(() => {
    vi.clearAllMocks()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
    removePathMock = vi.fn()
    vi.mocked(useFileNavStack).mockReturnValue({ removePath: removePathMock })
    // closeCurrentFile should mirror the real store logic for the assertion below
    closeCurrentFileMock.mockImplementation((path?: string) => {
      if (path && store.state.currentFile?.path !== path) return
      if (store.state.currentFile) removePathMock(store.state.currentFile.path)
      store.state.currentFile = null
    })
  })

  it('clears currentFile and removes path from nav stack when selectFile fails with clearOnError', async () => {
    store.state.currentFile = {
      name: 'deleted.go',
      path: 'src/deleted.go',
      content: 'old content\n',
    }
    store.state.currentDir = 'src'

    // selectFile returns false (file not found)
    vi.mocked(store.selectFile).mockResolvedValue(false)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'File not found', msgKey: 'FileNotFoundShort' }),
    })

    await refreshCurrentFile({ clearOnError: true })

    // currentFile should be cleared via the unified closeCurrentFile
    expect(store.state.currentFile).toBeNull()
    expect(closeCurrentFileMock).toHaveBeenCalledWith('src/deleted.go')

    globalThis.fetch = originalFetch
  })

  it('does not clear currentFile when selectFile succeeds with clearOnError', async () => {
    store.state.currentFile = {
      name: 'exists.go',
      path: 'src/exists.go',
      content: 'old content\n',
    }
    store.state.currentDir = 'src'

    // selectFile returns true (file exists)
    vi.mocked(store.selectFile).mockResolvedValue(true)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'new content\n' }),
    })

    await refreshCurrentFile({ clearOnError: true })

    // currentFile should NOT be cleared
    expect(store.state.currentFile).not.toBeNull()
    // removePath should NOT be called
    expect(removePathMock).not.toHaveBeenCalled()

    globalThis.fetch = originalFetch
  })

  it('does not clear currentFile when selectFile fails without clearOnError', async () => {
    store.state.currentFile = {
      name: 'error.go',
      path: 'src/error.go',
      content: 'old content\n',
    }
    store.state.currentDir = 'src'

    // selectFile returns false (network error)
    vi.mocked(store.selectFile).mockResolvedValue(false)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Network error' }),
    })

    await refreshCurrentFile({ clearOnError: false })

    // Without clearOnError, currentFile should remain even on failure
    expect(store.state.currentFile).not.toBeNull()
    expect(removePathMock).not.toHaveBeenCalled()

    globalThis.fetch = originalFetch
  })

  it('passes noLoading=true to selectFile during refresh (avoids loading mask flicker)', async () => {
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'old\n' }
    store.state.currentDir = ''

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'new\n' }),
    })

    await refreshCurrentFile()

    const args = store.selectFile.mock.calls[0]
    expect(args[6]).toBe(true) // noLoading — suppress loading mask on auto-refresh

    globalThis.fetch = originalFetch
  })

  it('passes silent=true to selectFile when clearOnError is set (no toast on deletion)', async () => {
    store.state.currentFile = { name: 'gone.go', path: 'src/gone.go', content: 'x\n' }
    store.state.currentDir = 'src'
    vi.mocked(store.selectFile).mockResolvedValue(false)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'File not found', msgKey: 'FileNotFoundShort' }),
    })

    await refreshCurrentFile({ clearOnError: true })

    const args = store.selectFile.mock.calls[0]
    expect(args[5]).toBe(true)

    globalThis.fetch = originalFetch
  })

  it('passes silent=false to selectFile without clearOnError', async () => {
    store.state.currentFile = { name: 'gone.go', path: 'src/gone.go', content: 'x\n' }
    store.state.currentDir = 'src'
    vi.mocked(store.selectFile).mockResolvedValue(false)

    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'Network error' }),
    })

    await refreshCurrentFile({ clearOnError: false })

    const args = store.selectFile.mock.calls[0]
    expect(args[5]).toBe(false)

    globalThis.fetch = originalFetch
  })
})

describe('useFileRefresh external-change confirmation while editing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
    isEditingMock.mockReturnValue(false)
    isEditorDirtyMock.mockReturnValue(false)
  })

  const setFetch = (content: string, ok = true) => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok,
      json: () => Promise.resolve(ok ? { content } : { error: 'File not found' }),
    })
  }

  it('does not prompt when not editing', async () => {
    isEditingMock.mockReturnValue(false)
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'old\n' }
    setFetch('new\n')
    await refreshCurrentFile()
    expect(confirmDialogMock).not.toHaveBeenCalled()
    expect(store.selectFile).toHaveBeenCalled()
  })

  it('does not prompt when editing but content is unchanged on disk', async () => {
    isEditingMock.mockReturnValue(true)
    isEditorDirtyMock.mockReturnValue(true)
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'same\n' }
    setFetch('same\n')
    await refreshCurrentFile()
    expect(confirmDialogMock).not.toHaveBeenCalled()
    expect(store.selectFile).toHaveBeenCalled()
  })

  it('does not prompt when editing but there are no unsaved changes (refreshes directly)', async () => {
    isEditingMock.mockReturnValue(true)
    isEditorDirtyMock.mockReturnValue(false)
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'old\n' }
    setFetch('new\n')
    await refreshCurrentFile()
    expect(confirmDialogMock).not.toHaveBeenCalled()
    expect(store.selectFile).toHaveBeenCalled()
  })

  it('aborts the refresh (no selectFile, flash cleared) when editing, dirty, external change, and user keeps current', async () => {
    isEditingMock.mockReturnValue(true)
    isEditorDirtyMock.mockReturnValue(true)
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'old\n' }
    setFetch('new\n')
    confirmDialogMock.mockResolvedValue(false)
    flashRanges.value = [{ line: 1, start: 0, end: 3 }]
    await refreshCurrentFile()
    expect(confirmDialogMock).toHaveBeenCalled()
    expect(store.selectFile).not.toHaveBeenCalled()
    expect(flashRanges.value).toEqual([])
  })

  it('proceeds with reload when editing, dirty, external change, and user confirms', async () => {
    isEditingMock.mockReturnValue(true)
    isEditorDirtyMock.mockReturnValue(true)
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'old\n' }
    setFetch('new\n')
    confirmDialogMock.mockResolvedValue(true)
    await refreshCurrentFile()
    expect(confirmDialogMock).toHaveBeenCalled()
    expect(store.selectFile).toHaveBeenCalled()
  })
})

describe('useFileRefresh self-save suppression', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
  })

  it('wasRecentlySaved returns false for an unmarked path', () => {
    expect(wasRecentlySaved('/tmp/a.go')).toBe(false)
  })

  it('wasRecentlySaved returns true for a recently-saved path, then false after expiry', async () => {
    markFileSaved('/tmp/a.go', 50)
    expect(wasRecentlySaved('/tmp/a.go')).toBe(true)
    // After the window expires the marker is dropped.
    await new Promise(r => setTimeout(r, 80))
    expect(wasRecentlySaved('/tmp/a.go')).toBe(false)
  })

  it('wasRecentlySaved is scoped per path', () => {
    markFileSaved('/tmp/a.go', 2000)
    expect(wasRecentlySaved('/tmp/a.go')).toBe(true)
    expect(wasRecentlySaved('/tmp/b.go')).toBe(false)
  })

  it('marks file saved so the watcher skips its own save-triggered refresh', async () => {
    store.state.currentFile = { name: 'a.go', path: 'a.go', content: 'new content\n' }
    const fetchSpy = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ content: 'new content\n' }),
    })
    globalThis.fetch = fetchSpy

    // The save flow marks the path; a file_change handler would check this and skip.
    markFileSaved('a.go', 2000)
    expect(wasRecentlySaved('a.go')).toBe(true)

    // Simulate the watcher guard: skip refresh while recently saved.
    if (!wasRecentlySaved('a.go')) {
      await refreshCurrentFile()
    }
    // refreshCurrentFile should NOT have run (no prefetch fetch).
    expect(fetchSpy).not.toHaveBeenCalled()
    expect(store.selectFile).not.toHaveBeenCalled()
  })
})
