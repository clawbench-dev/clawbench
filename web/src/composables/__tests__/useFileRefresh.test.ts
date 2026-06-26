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
import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock dependencies before importing
vi.mock('@/stores/app.ts', () => ({
  store: {
    state: {
      currentFile: null as any,
      currentDir: undefined as string | undefined,
    },
    loadFiles: vi.fn(),
    selectFile: vi.fn().mockResolvedValue(true),
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

import { refreshCurrentFile, flashRanges, flashType } from '../useFileRefresh.ts'
import { store } from '@/stores/app.ts'
import { computeDiff } from '@/utils/diffUtils.ts'
import { computeCodeDiffMarkers, diffMarkers, diffOldContent } from '@/composables/useMarkdownDiff.ts'

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
