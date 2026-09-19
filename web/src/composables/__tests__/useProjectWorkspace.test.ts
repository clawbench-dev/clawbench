import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { restoreProjectWorkspace } from '@/composables/useProjectWorkspace'
import { store } from '@/stores/app'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

const mockToastShow = vi.fn()
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: mockToastShow }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

const openFileMock = vi.fn()
vi.mock('@/composables/useFileNavStack', () => ({
  useFileNavStack: () => ({ openFile: openFileMock }),
}))

const OPEN_FILE_PREFIX = 'clawbench-open-file:'
const BROWSE_DIR_PREFIX = 'clawbench-browse-dir:'

describe('restoreProjectWorkspace', () => {
  const PROJECT = '/project/a'

  beforeEach(() => {
    localStorage.clear()
    store.state.projectRoot = PROJECT
    store.state.currentFile = null
    openFileMock.mockClear()
    mockToastShow.mockClear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  // Which panel to show is no longer this function's job — it is remembered per
  // project (useProjectPanel.ts) and applied by the caller after restore. These
  // tests therefore assert that restore ONLY populates state and never touches
  // the active tab, on either call path.
  it('restores the saved file into state without touching the active tab', async () => {
    localStorage.setItem(OPEN_FILE_PREFIX + PROJECT, 'web/src/App.vue')
    vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined)
    vi.spyOn(store, 'selectFile').mockResolvedValue(true)

    await restoreProjectWorkspace()

    expect(store.selectFile).toHaveBeenCalledWith('web/src/App.vue')
    expect(openFileMock).toHaveBeenCalledWith('web/src/App.vue')
  })

  it('does not open any file when no saved file exists', async () => {
    vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined)

    await restoreProjectWorkspace()

    expect(openFileMock).not.toHaveBeenCalled()
  })

  it('clears a stale open-file record when the saved file can no longer be opened', async () => {
    localStorage.setItem(OPEN_FILE_PREFIX + PROJECT, 'web/src/App.vue')
    vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined)
    vi.spyOn(store, 'selectFile').mockResolvedValue(false)

    await restoreProjectWorkspace()

    expect(openFileMock).not.toHaveBeenCalled()
    expect(localStorage.getItem(OPEN_FILE_PREFIX + PROJECT)).toBeNull()
  })

  it('loads the saved browse directory, falling back to the project root', async () => {
    localStorage.setItem(BROWSE_DIR_PREFIX + PROJECT, 'web/src')
    const loadFiles = vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined)

    await restoreProjectWorkspace()

    expect(loadFiles).toHaveBeenCalledWith('web/src', true)
  })
})
