import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { loadBrowseDir } from '@/stores/app.ts'
import { store } from '@/stores/app.ts'

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
