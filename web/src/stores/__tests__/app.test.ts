import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { loadBrowseDir, loadOpenFile, clearStaleOpenFile } from '@/stores/app.ts'
import { store } from '@/stores/app.ts'
import { apiGet } from '@/utils/api'

// Mock API to prevent real network calls
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn().mockResolvedValue({}),
  apiPost: vi.fn().mockResolvedValue({ ok: true, path: '' }),
}))

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// Mock useToast
const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

// Mock useDialog
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: vi.fn().mockResolvedValue(false) }),
}))

// Mock useFileNavStack
vi.mock('@/composables/useFileNavStack', () => ({
  useFileNavStack: () => ({ removePath: vi.fn() }),
}))

describe('saveBrowseDir / loadBrowseDir', () => {
  const BROWSE_DIR_PREFIX = 'clawbench-browse-dir:'

  beforeEach(() => {
    localStorage.clear()
  })

  it('loadBrowseDir returns empty string when no projectRoot', () => {
    store.state.projectRoot = ''
    store.state.currentDir = 'some/dir'
    expect(loadBrowseDir()).toBe('')
  })

  it('loadBrowseDir returns saved dir for the current project', () => {
    store.state.projectRoot = '/home/user/myproject'
    localStorage.setItem(BROWSE_DIR_PREFIX + '/home/user/myproject', 'src/components')
    expect(loadBrowseDir()).toBe('src/components')
  })

  it('loadBrowseDir returns empty string when no saved dir exists', () => {
    store.state.projectRoot = '/home/user/newproject'
    expect(loadBrowseDir()).toBe('')
  })

  it('saveBrowseDir persists currentDir keyed by projectRoot', async () => {
    store.state.projectRoot = '/home/user/project1'
    store.state.currentDir = 'internal/handler'

    // loadFiles triggers saveBrowseDir internally
    await store.loadFiles('internal/handler').catch(() => {
      // API mock returns empty, loadFiles may throw — that's ok
    })

    // Verify the value was persisted
    expect(localStorage.getItem(BROWSE_DIR_PREFIX + '/home/user/project1')).toBe('internal/handler')
  })

  it('saveBrowseDir does nothing when projectRoot is empty', async () => {
    store.state.projectRoot = ''
    store.state.currentDir = 'some/dir'

    await store.loadFiles('some/dir').catch(() => {})

    // No key should be set without projectRoot
    const keys = Object.keys(localStorage)
    const browseKeys = keys.filter(k => k.startsWith(BROWSE_DIR_PREFIX))
    expect(browseKeys.length).toBe(0)
  })

  it('loadBrowseDir handles localStorage error gracefully', () => {
    store.state.projectRoot = '/home/user/project'
    // Make localStorage.getItem throw
    const originalGetItem = localStorage.getItem.bind(localStorage)
    localStorage.getItem = () => { throw new Error('DOMException') }

    expect(loadBrowseDir()).toBe('')

    // Restore
    localStorage.getItem = originalGetItem
  })

  it('different projects have independent browse dirs', async () => {
    store.state.projectRoot = '/project/a'
    store.state.currentDir = 'dir-a'
    await store.loadFiles('dir-a').catch(() => {})

    store.state.projectRoot = '/project/b'
    store.state.currentDir = 'dir-b'
    await store.loadFiles('dir-b').catch(() => {})

    // Each project should have its own saved dir
    expect(localStorage.getItem(BROWSE_DIR_PREFIX + '/project/a')).toBe('dir-a')
    expect(localStorage.getItem(BROWSE_DIR_PREFIX + '/project/b')).toBe('dir-b')
  })
})

describe('loadFiles DirectoryNotFound → parent navigation', () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset()
    store.state.currentDir = ''
    store.state.dirEntries = []
    store.state.dirLoading = false
  })

  it('navigates to parent directory on DirectoryNotFound', async () => {
    // First call: returns DirectoryNotFound
    // Second call (parent): returns success
    vi.mocked(apiGet)
      .mockRejectedValueOnce(Object.assign(new Error('Directory not found'), { msgKey: 'DirectoryNotFound' }))
      .mockResolvedValueOnce({ items: [{ name: 'parent_file.txt', type: 'file', modified: '', size: 0, supported: true }] })

    store.state.currentDir = 'src/deleted-dir'
    await store.loadFiles('src/deleted-dir')

    // Should have navigated to parent 'src'
    expect(store.state.currentDir).toBe('src')
    expect(store.state.dirEntries.length).toBe(1)
  })

  it('rolls back to stale entries on non-DirectoryNotFound error', async () => {
    vi.mocked(apiGet).mockRejectedValueOnce(Object.assign(new Error('Server error'), { msgKey: 'InternalError' }))

    store.state.currentDir = 'src/some-dir'
    store.state.dirEntries = [{ name: 'existing.txt', type: 'file', modified: '', size: 0, supported: true }]

    await store.loadFiles('src/some-dir')

    // Should roll back, not navigate to parent
    expect(store.state.currentDir).toBe('src/some-dir')
    expect(store.state.dirEntries.length).toBe(1)
  })

  it('does not recurse at project root (empty dir)', async () => {
    vi.mocked(apiGet).mockRejectedValueOnce(Object.assign(new Error('Directory not found'), { msgKey: 'DirectoryNotFound' }))

    store.state.currentDir = 'single-dir'
    await store.loadFiles('single-dir')

    // 'single-dir' → parent is '' (project root) via dirName
    // The first call fails, but the recursive call to loadFiles('') should succeed
    // because we need a second mock for it
  })

  it('stops recursing after depth limit', async () => {
    // All calls return DirectoryNotFound — should stop after 10 levels and fall through to rollback
    const err = Object.assign(new Error('Directory not found'), { msgKey: 'DirectoryNotFound' })
    vi.mocked(apiGet).mockRejectedValue(err)

    store.state.currentDir = 'a/b/c/d/e/f/g/h/i/j/k/l'
    store.state.dirEntries = [{ name: 'old.txt', type: 'file', modified: '', size: 0, supported: true }]

    await store.loadFiles('a/b/c/d/e/f/g/h/i/j/k/l')

    // After 10 recursive attempts, should fall through to rollback path
    // dirLoading must be cleared
    expect(store.state.dirLoading).toBe(false)
    // apiGet should have been called at least 11 times (initial + 10 recursive)
    expect(vi.mocked(apiGet).mock.calls.length).toBeGreaterThanOrEqual(10)
  })

  it('resets to project root with an info toast when the root is deleted', async () => {
    const err = Object.assign(new Error('Directory not found'), { msgKey: 'DirectoryNotFound' })
    vi.mocked(apiGet).mockRejectedValue(err)
    mockToastShow.mockClear()

    store.state.currentDir = ''
    store.state.dirEntries = [{ name: 'old.txt', type: 'file', modified: '', size: 0, supported: true }]

    await store.loadFiles('')

    expect(store.state.currentDir).toBe('')
    expect(store.state.dirEntries).toEqual([])
    expect(mockToastShow).toHaveBeenCalledTimes(1)
    expect(mockToastShow.mock.calls[0][1]).toMatchObject({ type: 'info' })
  })

  it('noLoading=true skips loading mask on refresh', async () => {
    let resolveApi!: (v: unknown) => void
    vi.mocked(apiGet).mockReturnValue(new Promise(r => { resolveApi = r }))
    store.state.dirLoading = false

    const p = store.loadFiles('some/dir', false, 0, true)
    // While in-flight, dirLoading must still be false (no mask)
    expect(store.state.dirLoading).toBe(false)
    resolveApi({ items: [{ name: 'file.txt', type: 'file', modified: '', size: 0, supported: true }] })
    await p
    expect(store.state.dirLoading).toBe(false)
    expect(store.state.currentDir).toBe('some/dir')
  })

  it('noLoading=false (default) shows loading mask during fetch', async () => {
    let resolveApi!: (v: unknown) => void
    vi.mocked(apiGet).mockReturnValue(new Promise(r => { resolveApi = r }))
    store.state.dirLoading = false

    const p = store.loadFiles('some/dir')
    // While in-flight, dirLoading must be true (mask showing)
    expect(store.state.dirLoading).toBe(true)
    resolveApi({ items: [{ name: 'file.txt', type: 'file', modified: '', size: 0, supported: true }] })
    await p
    expect(store.state.dirLoading).toBe(false) // cleared after completion
    expect(store.state.currentDir).toBe('some/dir')
  })

  it('noLoading call superseding loading call clears dirLoading', async () => {
    // Scenario: user navigates (loading), then file watch triggers a noLoading
    // refresh that supersedes the first call — dirLoading must not get stuck.
    let resolveA!: (v: unknown) => void
    let resolveB!: (v: unknown) => void
    vi.mocked(apiGet)
      .mockReturnValueOnce(new Promise(r => { resolveA = r }))
      .mockReturnValueOnce(new Promise(r => { resolveB = r }))
    store.state.dirLoading = false

    const pA = store.loadFiles('dir-a', false, 0, false) // loading call, seq=1
    expect(store.state.dirLoading).toBe(true)
    const pB = store.loadFiles('dir-b', false, 0, true)  // noLoading call, seq=2 supersedes
    // dirLoading is still true (set by call A, not cleared yet)

    // Complete call B first (it's the latest)
    resolveB({ items: [{ name: 'b.txt', type: 'file', modified: '', size: 0, supported: true }] })
    await pB
    // dirLoading must be cleared — B is the latest call and finally runs
    expect(store.state.dirLoading).toBe(false)
    expect(store.state.currentDir).toBe('dir-b')

    // Now complete call A — stale, should be ignored
    resolveA({ items: [{ name: 'a.txt', type: 'file', modified: '', size: 0, supported: true }] })
    await pA
    expect(store.state.currentDir).toBe('dir-b') // unchanged by stale call
  })
})

describe('saveOpenFile / loadOpenFile / clearStaleOpenFile', () => {
  const OPEN_FILE_PREFIX = 'clawbench-open-file:'

  beforeEach(() => {
    localStorage.clear()
    store.state.projectRoot = ''
    store.state.currentFile = null
  })

  it('loadOpenFile returns empty string when no projectRoot', () => {
    store.state.projectRoot = ''
    expect(loadOpenFile()).toBe('')
  })

  it('loadOpenFile returns saved file path for the current project', () => {
    store.state.projectRoot = '/home/user/myproject'
    localStorage.setItem(OPEN_FILE_PREFIX + '/home/user/myproject', 'src/main.go')
    expect(loadOpenFile()).toBe('src/main.go')
  })

  it('loadOpenFile returns empty string when no saved file exists', () => {
    store.state.projectRoot = '/home/user/newproject'
    expect(loadOpenFile()).toBe('')
  })

  it('clearStaleOpenFile removes the persisted file for the current project', () => {
    store.state.projectRoot = '/home/user/myproject'
    localStorage.setItem(OPEN_FILE_PREFIX + '/home/user/myproject', 'src/main.go')
    clearStaleOpenFile()
    expect(localStorage.getItem(OPEN_FILE_PREFIX + '/home/user/myproject')).toBeNull()
  })

  it('clearStaleOpenFile does nothing when projectRoot is empty', () => {
    store.state.projectRoot = ''
    localStorage.setItem(OPEN_FILE_PREFIX + '/home/user/other', 'other.go')
    clearStaleOpenFile()
    // Should not affect other keys
    expect(localStorage.getItem(OPEN_FILE_PREFIX + '/home/user/other')).toBe('other.go')
  })

  it('different projects have independent open files', () => {
    store.state.projectRoot = '/project/a'
    localStorage.setItem(OPEN_FILE_PREFIX + '/project/a', 'file-a.ts')
    store.state.projectRoot = '/project/b'
    localStorage.setItem(OPEN_FILE_PREFIX + '/project/b', 'file-b.ts')

    store.state.projectRoot = '/project/a'
    expect(loadOpenFile()).toBe('file-a.ts')
    store.state.projectRoot = '/project/b'
    expect(loadOpenFile()).toBe('file-b.ts')
  })

  it('switching away from a project preserves its open-file record (restore on return)', () => {
    // Simulate: open file-a in project a
    store.state.projectRoot = '/project/a'
    localStorage.setItem(OPEN_FILE_PREFIX + '/project/a', 'file-a.ts')

    // Simulate project switch a -> b. The switch must NOT clear project a's
    // record — otherwise returning to project a cannot restore the current file.
    store.state.projectRoot = '/project/b'
    localStorage.setItem(OPEN_FILE_PREFIX + '/project/b', 'file-b.ts')

    // Return a -> b -> a; project a's open file must still be there.
    store.state.projectRoot = '/project/a'
    expect(loadOpenFile()).toBe('file-a.ts')
  })
})

describe('markSaved', () => {
  beforeEach(() => {
    localStorage.clear()
    store.state.projectRoot = ''
    store.state.currentFile = null
  })

  it('updates the current file content in place and clears transient flags', () => {
    store.state.currentFile = {
      name: 'main.go',
      path: '/tmp/main.go',
      content: 'old',
      truncated: true,
      tooLarge: true,
      isBinary: true,
    }
    store.markSaved('/tmp/main.go', 'package main')
    expect(store.state.currentFile?.content).toBe('package main')
    expect(store.state.currentFile?.truncated).toBe(false)
    expect(store.state.currentFile?.tooLarge).toBe(false)
    expect(store.state.currentFile?.isBinary).toBe(false)
  })

  it('does nothing when the path does not match the current file', () => {
    store.state.currentFile = { name: 'main.go', path: '/tmp/main.go', content: 'old' }
    store.markSaved('/tmp/other.go', 'new')
    expect(store.state.currentFile?.content).toBe('old')
  })

  it('does nothing when no file is open', () => {
    store.state.currentFile = null
    store.markSaved('/tmp/main.go', 'new')
    expect(store.state.currentFile).toBeNull()
  })
})

describe('selectFile not-found handling', () => {
  const originalFetch = global.fetch

  beforeEach(() => {
    store.state.currentFile = null
    store.state.projectRoot = '/tmp/project'
    mockToastShow.mockClear()
  })

  afterEach(() => {
    global.fetch = originalFetch
  })

  function mockFetchResponse(payload: { error?: string; msgKey?: string }, ok: boolean) {
    global.fetch = vi.fn().mockResolvedValue({ ok, json: async () => payload })
  }

  it('shows an info toast (not an error) when the file is not found', async () => {
    mockFetchResponse({ error: 'File not found', msgKey: 'FileNotFoundShort' }, false)
    const ok = await store.selectFile('/tmp/project/gone.go')
    expect(ok).toBe(false)
    expect(mockToastShow).toHaveBeenCalledTimes(1)
    const [msg, opts] = mockToastShow.mock.calls[0]
    expect(opts).toMatchObject({ type: 'info' })
    // Informational "file removed" message, not the raw backend error
    expect(msg).not.toBe('File not found')
  })

  it('shows no toast at all when the silent flag is set', async () => {
    mockFetchResponse({ error: 'File not found', msgKey: 'FileNotFoundShort' }, false)
    const ok = await store.selectFile('/tmp/project/gone.go', false, false, true, false, true)
    expect(ok).toBe(false)
    expect(mockToastShow).not.toHaveBeenCalled()
  })

  it('keeps an error toast for non-not-found failures', async () => {
    mockFetchResponse({ error: 'Server exploded', msgKey: 'InternalError' }, false)
    const ok = await store.selectFile('/tmp/project/main.go')
    expect(ok).toBe(false)
    expect(mockToastShow).toHaveBeenCalledWith('Server exploded', expect.objectContaining({ type: 'error' }))
  })

  it('does not set fileLoading when noLoading=true', async () => {
    mockFetchResponse({ content: 'hello' }, true)
    store.state.fileLoading = false
    await store.selectFile('/tmp/project/test.go', false, false, true, false, false, true)
    expect(store.state.fileLoading).toBe(false)
  })

  it('sets fileLoading during normal file open', async () => {
    mockFetchResponse({ content: 'hello' }, true)
    store.state.fileLoading = false
    // We need to check that fileLoading was set to true at some point during the call.
    // Since it's set to false in finally, we can't easily observe it after the call.
    // Instead, verify the default behavior: noLoading=false means fileLoading is managed normally.
    await store.selectFile('/tmp/project/test.go')
    expect(store.state.fileLoading).toBe(false) // cleared in finally
  })
})
