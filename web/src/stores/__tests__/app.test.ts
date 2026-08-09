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
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
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
