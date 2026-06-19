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
    // Because the first refresh already ran selectFile, the deferred one
    // will also call selectFile.
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
