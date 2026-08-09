import { describe, expect, it, vi, beforeEach } from 'vitest'
import { computed, nextTick } from 'vue'
import { _resetForTesting, openRecentFile, recordRecentFile, removeRecentFile, useRecentFiles } from '@/composables/useRecentFiles'
import { store } from '@/stores/app.ts'

// Mock store — reactive so the useRecentFiles projectRoot watcher fires on switch.
vi.mock('@/stores/app.ts', async () => {
  const { reactive } = await import('vue')
  return {
    store: {
      state: reactive({ projectRoot: '/test/project' }),
    },
  }
})

// Mock localStorage
const localStorageStore: Record<string, string> = {}
const mockLocalStorage = {
  getItem: vi.fn((key: string) => localStorageStore[key] ?? null),
  setItem: vi.fn((key: string, val: string) => { localStorageStore[key] = val }),
  removeItem: vi.fn((key: string) => { delete localStorageStore[key] }),
  clear: vi.fn(() => Object.keys(localStorageStore).forEach(k => delete localStorageStore[k])),
}
vi.stubGlobal('localStorage', mockLocalStorage)

beforeEach(() => {
  _resetForTesting()
  Object.keys(localStorageStore).forEach(k => delete localStorageStore[k])
  mockLocalStorage.getItem.mockClear()
  mockLocalStorage.setItem.mockClear()
})

describe('useRecentFiles', () => {
  it('recordRecentFile adds entry', () => {
    recordRecentFile('foo/bar.ts')
    const { entries } = useRecentFiles()
    expect(entries.value).toHaveLength(1)
    expect(entries.value[0].path).toBe('foo/bar.ts')
  })

  it('recordRecentFile deduplicates and moves to front', () => {
    recordRecentFile('a.ts')
    recordRecentFile('b.ts')
    recordRecentFile('a.ts') // re-open, should move to front
    const { entries } = useRecentFiles()
    expect(entries.value).toHaveLength(2)
    expect(entries.value[0].path).toBe('a.ts')
    expect(entries.value[1].path).toBe('b.ts')
  })

  it('recordRecentFile caps at 10 entries', () => {
    for (let i = 0; i < 15; i++) {
      recordRecentFile(`file${i}.ts`)
    }
    const { entries } = useRecentFiles()
    expect(entries.value).toHaveLength(10)
    // Most recent should be first
    expect(entries.value[0].path).toBe('file14.ts')
    expect(entries.value[9].path).toBe('file5.ts')
  })

  it('removeRecentFile removes entry', () => {
    recordRecentFile('a.ts')
    recordRecentFile('b.ts')
    removeRecentFile('a.ts')
    const { entries } = useRecentFiles()
    expect(entries.value).toHaveLength(1)
    expect(entries.value[0].path).toBe('b.ts')
  })

  it('openRecentFile removes the entry when the file fails to open', async () => {
    recordRecentFile('gone.ts')
    recordRecentFile('ok.ts')
    const load = vi.fn(async (p: string) => p !== 'gone.ts')
    const ok = await openRecentFile('gone.ts', load)
    expect(ok).toBe(false)
    const { entries } = useRecentFiles()
    expect(entries.value.map(e => e.path)).toEqual(['ok.ts'])
  })

  it('openRecentFile keeps the entry when the file opens successfully', async () => {
    recordRecentFile('a.ts')
    recordRecentFile('b.ts')
    const load = vi.fn(async () => true)
    const ok = await openRecentFile('b.ts', load)
    expect(ok).toBe(true)
    expect(load).toHaveBeenCalledWith('b.ts')
    const { entries } = useRecentFiles()
    expect(entries.value.map(e => e.path)).toEqual(['b.ts', 'a.ts'])
  })

  it('recentFilesExcluding filters out current file', () => {
    recordRecentFile('a.ts')
    recordRecentFile('b.ts')
    recordRecentFile('c.ts')
    const { recentFilesExcluding } = useRecentFiles()
    const filtered = recentFilesExcluding(computed(() => 'b.ts'))
    expect(filtered.value).toHaveLength(2)
    expect(filtered.value.map(e => e.path)).toEqual(['c.ts', 'a.ts'])
  })

  it('recentFilesExcluding returns all when no current path', () => {
    recordRecentFile('a.ts')
    recordRecentFile('b.ts')
    const { recentFilesExcluding } = useRecentFiles()
    const filtered = recentFilesExcluding(computed(() => null))
    expect(filtered.value).toHaveLength(2)
  })

  it('persists to localStorage', () => {
    recordRecentFile('a.ts')
    expect(mockLocalStorage.setItem).toHaveBeenCalled()
    const key = mockLocalStorage.setItem.mock.calls[0][0]
    expect(key).toContain('clawbench-recent-files:')
    const val = JSON.parse(mockLocalStorage.setItem.mock.calls[0][1])
    expect(val[0].path).toBe('a.ts')
  })

  it('recordRecentFile ignores empty path', () => {
    recordRecentFile('')
    const { entries } = useRecentFiles()
    expect(entries.value).toHaveLength(0)
  })

  it('clears entries when switching to a project with no recent files', async () => {
    store.state.projectRoot = '/proj-a'
    recordRecentFile('a.ts')
    expect(useRecentFiles().entries.value).toHaveLength(1)

    // Switch to a project that has no stored recent files — the previous
    // project's entries must not leak through.
    store.state.projectRoot = '/proj-b'
    await nextTick()
    expect(useRecentFiles().entries.value).toHaveLength(0)

    store.state.projectRoot = '/test/project'
  })
})
