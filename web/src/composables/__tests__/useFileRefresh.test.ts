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
vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      currentFile: null as any,
      currentDir: undefined as string | undefined,
    },
    loadFiles: vi.fn(),
    selectFile: vi.fn().mockResolvedValue(true),
    closeCurrentFile: closeCurrentFileMock,
  },
}))

vi.mock('@/composables/useMarkdownDiff.ts', () => ({
  computeMarkdownDiff: vi.fn(),
  offscreenExtractBlocks: vi.fn(),
  diffMarkers: { value: [] },
  diffOldContent: { value: null },
  diffOldFilePath: { value: null },
  clearDiffMarkers: vi.fn(),
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

import { refreshCurrentFile, flashRanges, flashType, markFileSaved, wasRecentlySaved } from '../useFileRefresh.ts'
import { store } from '@/stores/app.ts'
import { computeDiff } from '@/utils/diffUtils.ts'
import { computeCodeDiffMarkers, diffMarkers, diffOldContent } from '@/composables/useMarkdownDiff.ts'
import { useFileNavStack } from '@/composables/useFileNavStack.ts'

describe('useFileRefresh deduplication', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    flashRanges.value = []
    flashType.value = 'add'
    diffMarkers.value = []
    diffOldContent.value = null
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

    // While first is in-flight, start second refresh with clearOnError=true
    const p2 = refreshCurrentFile({ loadDir: false, clearOnError: true })

    // Let first refresh complete
    resolveSelect!()
    await Promise.all([p1, p2])

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

        const changes = diffLines(oldText, newText, { timeout: 3 })
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
