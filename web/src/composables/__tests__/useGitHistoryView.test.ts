import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useGitHistoryView } from '@/composables/useGitHistoryView'

// ── Mocks ────────────────────────────────────────────────────

const { mockGitFetch, mockHandleDrillBackToCommits, mockNavigateToCommit } = vi.hoisted(() => ({
  mockGitFetch: vi.fn(),
  mockHandleDrillBackToCommits: vi.fn(),
  mockNavigateToCommit: vi.fn(),
}))

vi.mock('@/utils/gitApi', () => ({
  gitFetch: mockGitFetch,
  GitTimeoutError: class GitTimeoutError extends Error {
    constructor(url: string) { super(`git timeout ${url}`); this.name = 'GitTimeoutError' }
  },
  createSeqGuard: () => {
    let current: any = null
    let controller: AbortController | null = null
    return {
      token: () => {
        controller?.abort()
        controller = new AbortController()
        current = { __seqToken: true, signal: controller.signal }
        return current
      },
      isCurrent: (t: any) => t === current,
    }
  },
}))

vi.mock('@/utils/diff.ts', () => ({
  renderDiff: (s: string) => `<div>${s}</div>`,
}))

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

vi.mock('@/composables/useCommitNavigation.ts', () => ({
  useCommitNavigation: () => ({
    navigateToCommit: mockNavigateToCommit,
    handleDrillBackToCommits: mockHandleDrillBackToCommits,
  }),
  consumePendingCommitNavigation: () => null,
  consumePendingManageNavigation: () => false,
}))

/** Translation stub that returns the key, so assertions read as i18n keys. */
const t = (key: string) => key

function okJson(data: unknown) {
  return { ok: true, json: () => Promise.resolve(data) }
}

/** Build the composable with the drawer's call shape (no AbortSignal). */
function setup(opts: { mode?: string; filePath?: string } = {}) {
  return useGitHistoryView({
    t,
    mode: () => opts.mode ?? 'project',
    filePath: () => opts.filePath,
  })
}

/** Build the composable with GitHistoryContent's call shape (AbortSignal). */
function setupWithSignal(opts: { mode?: string } = {}) {
  return useGitHistoryView({
    t,
    mode: () => opts.mode ?? 'project',
    useAbortSignal: true,
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGitFetch.mockReset()
  mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [], hasMore: false }))
})

// ── Helpers ──────────────────────────────────────────────────

describe('useGitHistoryView — helpers', () => {
  it('fileTypeLabel returns i18n key for known types', () => {
    const v = setup()
    expect(v.fileTypeLabel('A', false)).toBe('git.fileType.added')
    expect(v.fileTypeLabel('M', true)).toContain('git.fileType.stagedPrefix')
    // Unknown types fall through to the raw type letter.
    expect(v.fileTypeLabel('Q', false)).toBe('Q')
  })

  it('badgeClass maps type to class name', () => {
    const v = setup()
    expect(v.badgeClass({ path: 'a', type: 'A' })).toBe('badge-A')
    expect(v.badgeClass({ path: 'a', type: 'M' })).toBe('badge-M')
    expect(v.badgeClass({ path: 'a', type: 'D' })).toBe('badge-D')
    expect(v.badgeClass({ path: 'a', type: 'R' })).toBe('badge-R')
    expect(v.badgeClass({ path: 'a', type: '?' })).toBe('badge-U')
    // Unknown type degrades to modified.
    expect(v.badgeClass({ path: 'a', type: 'Q' })).toBe('badge-M')
    expect(v.badgeClass({ path: 'a', type: 'M', staged: true })).toBe('badge-M badge-staged')
  })

  it('fileSplit separates the bare name from its parent directory', () => {
    const v = setup()
    expect(v.fileSplit({ path: 'web/src/foo.ts', type: 'M' })).toEqual({ name: 'foo.ts', dir: 'web/src' })
    expect(v.fileSplit({ path: 'README.md', type: 'A' })).toEqual({ name: 'README.md', dir: '' })
    // Untracked directories arrive with a trailing slash.
    expect(v.fileSplit({ path: 'newdir/', type: '?' })).toEqual({ name: 'newdir', dir: '' })
    expect(v.fileSplit(undefined)).toEqual({ name: '', dir: '' })
  })

  it('resetListState clears all list state', () => {
    const v = setup()
    v.commits.value = [{ sha: 'a', msg: 'm', date: '', author: '' }]
    v.error.value = 'oops'
    v.currentView.value = 'files'
    v.files.value = [{ path: 'x', type: 'M' }]
    v.hasLoadedMore.value = true

    v.resetListState()

    expect(v.commits.value).toEqual([])
    expect(v.error.value).toBe('')
    expect(v.currentView.value).toBe('commits')
    expect(v.files.value).toEqual([])
    expect(v.selectedSHA.value).toBeNull()
    expect(v.isGit.value).toBe(false)
    expect(v.hasLoadedMore.value).toBe(false)
  })
})

// ── Computed ─────────────────────────────────────────────────

describe('useGitHistoryView — computed', () => {
  it('selectedCommit returns the matching commit or null', () => {
    const v = setup()
    v.commits.value = [
      { sha: 'a', msg: 'A', date: '', author: '' },
      { sha: 'b', msg: 'B', date: '', author: '' },
    ]
    v.selectedSHA.value = 'b'
    expect(v.selectedCommit.value).toEqual({ sha: 'b', msg: 'B', date: '', author: '' })
    v.selectedSHA.value = 'missing'
    expect(v.selectedCommit.value).toBeNull()
  })

  it('isWorkingTree is true only for HEAD', () => {
    const v = setup()
    v.selectedSHA.value = 'HEAD'
    expect(v.isWorkingTree.value).toBe(true)
    v.selectedSHA.value = 'abc'
    expect(v.isWorkingTree.value).toBe(false)
  })

  it('mode reflects the host mode reader', () => {
    expect(setup({ mode: 'file' }).mode.value).toBe('file')
    expect(setup({ mode: 'project' }).mode.value).toBe('project')
  })

  it('totalFileCount sums merge groups, else counts files', () => {
    const v = setup()
    v.mergeGroups.value = [
      { label: 'A', files: [{ path: 'x', type: 'M' }, { path: 'y', type: 'M' }] },
    ]
    expect(v.totalFileCount.value).toBe(2)
    v.mergeGroups.value = []
    v.files.value = [{ path: 'z', type: 'M' }]
    expect(v.totalFileCount.value).toBe(1)
  })

  it('hasStaged/hasUnstaged reflect the staged flag', () => {
    const v = setup()
    v.files.value = [{ path: 'a', type: 'M', staged: true }]
    expect(v.hasStaged.value).toBe(true)
    expect(v.hasUnstaged.value).toBe(false)
    v.files.value = [{ path: 'a', type: 'M', staged: false }]
    expect(v.hasStaged.value).toBe(false)
    expect(v.hasUnstaged.value).toBe(true)
  })

  it('sortedFiles orders by type (M, A, D, R, ?)', () => {
    const v = setup()
    v.files.value = [
      { path: 'a', type: 'D' },
      { path: 'b', type: 'A' },
      { path: 'c', type: 'M' },
    ]
    expect(v.sortedFiles.value.map(f => f.path)).toEqual(['c', 'b', 'a'])
  })
})

// ── Navigation ───────────────────────────────────────────────

describe('useGitHistoryView — navigation', () => {
  it('drillBack("commits") resets selection state and reloads the list', () => {
    const v = setup()
    v.selectedSHA.value = 'abc123'
    v.currentView.value = 'files'

    v.drillBack('commits')

    expect(v.selectedSHA.value).toBeNull()
    expect(v.currentView.value).toBe('commits')
    expect(mockHandleDrillBackToCommits).toHaveBeenCalled()
  })

  it('drillBack("files") resets the file path and clears the diff', () => {
    const v = setup()
    v.selectedFilePath.value = 'src/foo.ts'
    v.currentView.value = 'diff'
    v.diffState.value = { loading: false, empty: false, html: '<div/>' }

    v.drillBack('files')

    expect(v.selectedFilePath.value).toBeNull()
    expect(v.currentView.value).toBe('files')
    expect(v.diffState.value.html).toBe('')
  })

  it('drillToFile sets the path and switches to the diff view', async () => {
    mockGitFetch.mockResolvedValue(okJson({ diff: 'sample diff' }))
    const v = setup()

    v.drillToFile({ path: 'src/foo.ts', type: 'M' })

    expect(v.selectedFilePath.value).toBe('src/foo.ts')
    expect(v.currentView.value).toBe('diff')
  })

  it('onCommitSelect for a non-HEAD commit loads that commit\'s files', async () => {
    mockGitFetch.mockResolvedValue(okJson([{ path: 'src/x', type: 'M' }]))
    const v = setup()

    v.onCommitSelect({ sha: 'abc123', msg: 'm', date: '', author: '' })
    await vi.waitFor(() => expect(v.files.value).toHaveLength(1))

    expect(v.currentView.value).toBe('files')
    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/commit-files?sha=abc123')
  })

  it('onCommitSelect for file mode switches to the diff view', async () => {
    mockGitFetch.mockResolvedValue(okJson({ diff: 'data', empty: false }))
    const v = setup({ mode: 'file', filePath: 'src/x.ts' })

    v.onCommitSelect({ sha: 'abc123', msg: 'm', date: '', author: '' })

    expect(v.currentView.value).toBe('diff')
  })
})

// ── loadProjectHistory ───────────────────────────────────────

describe('useGitHistoryView — loadProjectHistory', () => {
  it('prepends a working-tree entry when the working tree has changes', async () => {
    let call = 0
    mockGitFetch.mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a', msg: 'A', date: '', author: '' }], hasMore: false }))
      if (call === 2) return Promise.resolve(okJson({ files: [{ path: 'x', type: 'M' }] }))
      return Promise.resolve(okJson({}))
    })
    const v = setup()

    await v.loadProjectHistory()

    expect(v.commits.value[0].sha).toBe('HEAD')
    expect(v.commits.value[0].isWT).toBe(true)
    expect(v.commits.value).toHaveLength(2)
    // wtFiles is cached alongside the commits.
    expect(v.wtFiles.value).toEqual([{ path: 'x', type: 'M' }])
  })

  it('omits the working-tree entry when the workspace is clean', async () => {
    let call = 0
    mockGitFetch.mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a', msg: 'A', date: '', author: '' }], hasMore: false }))
      return Promise.resolve(okJson({ files: [] }))
    })
    const v = setup()

    await v.loadProjectHistory()

    expect(v.commits.value.map(c => c.sha)).toEqual(['a'])
  })

  it('handles an isGit=false response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ isGit: false }))
    const v = setup()

    await v.loadProjectHistory()

    expect(v.isGit.value).toBe(false)
    expect(v.commits.value).toEqual([])
  })

  it('surfaces the server error message', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'failed' }) })
    const v = setup()

    await v.loadProjectHistory()

    expect(v.error.value).toBe('failed')
  })

  it('surfaces a distinct message on timeout', async () => {
    const v = setup()
    const { GitTimeoutError } = await import('@/utils/gitApi')
    mockGitFetch.mockImplementationOnce(() => { throw new GitTimeoutError('url') })

    await v.loadProjectHistory()

    expect(v.error.value).toBe('git.history.loadTimeout')
  })

  it('surfaces a generic message on other failures', async () => {
    mockGitFetch.mockImplementationOnce(() => { throw new Error('net') })
    const v = setup()

    await v.loadProjectHistory()

    expect(v.error.value).toBe('git.history.loadError')
  })

  it('passes an AbortSignal when the host opts in, and omits it otherwise', async () => {
    const withSignal = setupWithSignal()
    await withSignal.loadProjectHistory()
    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/project-history', expect.objectContaining({ signal: expect.anything() }))

    mockGitFetch.mockClear()
    mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [], hasMore: false }))
    const without = setup()
    await without.loadProjectHistory()
    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/project-history')
  })

  it('notifies the host after a successful load (git-state snapshot hook)', async () => {
    const onProjectHistoryLoaded = vi.fn()
    const v = useGitHistoryView({ t, mode: () => 'project', onProjectHistoryLoaded })
    mockGitFetch.mockImplementation((url: string) =>
      url.startsWith('/api/git/working-tree')
        ? Promise.resolve(okJson({ files: [] }))
        : Promise.resolve(okJson({ isGit: true, commits: [], hasMore: false }))
    )

    await v.loadProjectHistory()

    expect(onProjectHistoryLoaded).toHaveBeenCalledTimes(1)
  })
})

// ── loadFileHistory ──────────────────────────────────────────

describe('useGitHistoryView — loadFileHistory', () => {
  it('prepends a working-tree entry when the file has uncommitted changes', async () => {
    let call = 0
    mockGitFetch.mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a', msg: 'A', date: '', author: '' }] }))
      if (call === 2) return Promise.resolve(okJson({ hasUncommitted: true }))
      return Promise.resolve(okJson({}))
    })
    const v = setup({ mode: 'file', filePath: 'src/foo.ts' })

    await v.loadFileHistory('src/foo.ts')

    expect(v.commits.value[0].sha).toBe('HEAD')
    expect(v.isGit.value).toBe(true)
  })

  it('does not prepend a working-tree entry for a clean file', async () => {
    let call = 0
    mockGitFetch.mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a', msg: 'A', date: '', author: '' }] }))
      return Promise.resolve(okJson({ hasUncommitted: false }))
    })
    const v = setup({ mode: 'file', filePath: 'src/foo.ts' })

    await v.loadFileHistory('src/foo.ts')

    expect(v.commits.value.map(c => c.sha)).toEqual(['a'])
  })

  it('handles a non-git repository', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ isGit: false }))
    const v = setup({ mode: 'file', filePath: 'src/foo.ts' })

    await v.loadFileHistory('src/foo.ts')

    expect(v.isGit.value).toBe(false)
  })
})

// ── loadCommitFiles / loadDiff ───────────────────────────────

describe('useGitHistoryView — loadCommitFiles', () => {
  it('sets files from an array response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson([{ path: 'a', type: 'M' }]))
    const v = setup()

    await v.loadCommitFiles('abc')

    expect(v.files.value).toHaveLength(1)
    expect(v.mergeGroups.value).toEqual([])
  })

  it('sets mergeGroups from a merge response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ merge: true, groups: [{ label: 'main', files: [{ path: 'a', type: 'M' }] }] }))
    const v = setup()

    await v.loadCommitFiles('abc')

    expect(v.mergeGroups.value).toHaveLength(1)
    expect(v.files.value).toEqual([])
  })

  it('clears the list on a failed response', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    const v = setup()
    v.files.value = [{ path: 'stale', type: 'M' }]

    await v.loadCommitFiles('abc')

    expect(v.files.value).toEqual([])
  })
})

describe('useGitHistoryView — loadDiff', () => {
  it('project mode renders the diff html', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ diff: '+added', empty: false }))
    const v = setup()
    v.selectedSHA.value = 'abc'
    v.selectedFilePath.value = 'src/foo.ts'

    await v.loadDiff()

    expect(v.diffState.value.html).toContain('added')
    expect(v.diffState.value.empty).toBe(false)
    expect(mockGitFetch).toHaveBeenCalledWith(expect.stringContaining('/api/git/file-diff'))
  })

  it('marks the diff empty when the server says so', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ empty: true }))
    const v = setup()
    v.selectedSHA.value = 'abc'
    v.selectedFilePath.value = 'src/foo.ts'

    await v.loadDiff()

    expect(v.diffState.value.empty).toBe(true)
  })

  it('marks the diff empty on a failed response', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    const v = setup()
    v.selectedSHA.value = 'abc'
    v.selectedFilePath.value = 'src/foo.ts'

    await v.loadDiff()

    expect(v.diffState.value.empty).toBe(true)
  })

  it('file mode uses the per-file diff endpoint', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ diff: 'data', empty: false }))
    const v = setup({ mode: 'file', filePath: 'src/main.ts' })
    v.selectedSHA.value = 'abc'

    await v.loadDiff()

    expect(mockGitFetch).toHaveBeenCalledWith(expect.stringContaining('/api/git/diff?path='))
  })
})

// ── loadMoreCommits / onSearch / onRefresh ───────────────────

describe('useGitHistoryView — loadMoreCommits', () => {
  it('skips when there is nothing more to load', async () => {
    const v = setup()
    v.hasMore.value = false

    await v.loadMoreCommits()

    expect(mockGitFetch).not.toHaveBeenCalledWith(expect.stringContaining('skip='))
  })

  it('fetches and appends commits, excluding the WT row from the offset', async () => {
    mockGitFetch.mockResolvedValue(okJson({ commits: [{ sha: 'new1', msg: 'n', date: '', author: '' }], hasMore: false }))
    const v = setup()
    v.commits.value = [
      { sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true },
      { sha: 'a', msg: 'A', date: '', author: '' },
    ]
    v.hasMore.value = true
    v.isGit.value = true

    await v.loadMoreCommits()

    expect(v.commits.value).toHaveLength(3)
    expect(v.hasMore.value).toBe(false)
    // Only the one real git commit counts toward the skip offset.
    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/project-history?skip=1')
    expect(v.hasLoadedMore.value).toBe(true)
  })
})

describe('useGitHistoryView — onSearch', () => {
  it('does nothing for a blank query', async () => {
    const v = setup()

    await v.onSearch('   ')

    expect(mockGitFetch).not.toHaveBeenCalled()
  })

  it('does nothing in file mode', async () => {
    const v = setup({ mode: 'file', filePath: 'src/foo.ts' })
    v.isGit.value = true

    await v.onSearch('foo')

    expect(mockGitFetch).not.toHaveBeenCalled()
  })

  it('pages in the remaining commits so the filter covers full history', async () => {
    let call = 0
    mockGitFetch.mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve(okJson({ commits: [{ sha: 'b', msg: 'B', date: '', author: '' }], hasMore: true }))
      return Promise.resolve(okJson({ commits: [{ sha: 'c', msg: 'C', date: '', author: '' }], hasMore: false }))
    })
    const v = setup()
    v.isGit.value = true
    v.commits.value = [{ sha: 'a', msg: 'A', date: '', author: '' }]
    v.hasMore.value = true

    await v.onSearch('b')

    expect(v.commits.value.map(c => c.sha)).toEqual(['a', 'b', 'c'])
    expect(v.hasMore.value).toBe(false)
    expect(v.searchLoading.value).toBe(false)
  })
})

describe('useGitHistoryView — onRefresh', () => {
  it('refetches project history and resets the search/pagination state', async () => {
    const v = setup()
    v.commitSearch.value = 'query'
    v.hasLoadedMore.value = true
    mockGitFetch.mockClear()
    mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [], hasMore: false }))

    await v.onRefresh()

    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/project-history')
    expect(v.commitSearch.value).toBe('')
    expect(v.hasLoadedMore.value).toBe(false)
  })

  it('refetches file history when in file mode', async () => {
    const v = setup({ mode: 'file', filePath: 'src/foo.ts' })
    mockGitFetch.mockClear()
    mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [] }))

    await v.onRefresh()

    expect(mockGitFetch).toHaveBeenCalledWith(expect.stringContaining('/api/git/history'))
  })
})

// ── Working-tree freshness (the regression this composable was extracted for) ──

describe('useGitHistoryView — working tree view stays fresh', () => {
  /** Route gitFetch so the workspace contents can change between calls. */
  function route(wt: { files: Array<Record<string, unknown>> }) {
    mockGitFetch.mockImplementation((url: string) => {
      if (url.startsWith('/api/git/project-history')) {
        return Promise.resolve(okJson({ isGit: true, commits: [], hasMore: false }))
      }
      if (url.startsWith('/api/git/working-tree')) {
        return Promise.resolve(okJson({ files: wt.files }))
      }
      return Promise.resolve(okJson([]))
    })
  }

  it('re-fetches the working tree when the WT row is selected (not the stale snapshot)', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    route(wt)
    const v = setup()
    await v.loadProjectHistory()

    // Workspace changes while the user is elsewhere.
    wt.files = [{ path: 'new.ts', type: 'A', staged: false }]

    v.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['new.ts']))

    expect(v.currentView.value).toBe('files')
  })

  it('reloadPreservingDrillDown refreshes the WT list instead of rendering blank', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    route(wt)
    const v = setup()
    await v.loadProjectHistory()

    v.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['old.ts']))

    // Workspace changes, then the host reloads on re-entry.
    wt.files = [{ path: 'changed.ts', type: 'M', staged: false }]
    await v.reloadPreservingDrillDown()

    expect(v.currentView.value).toBe('files')
    expect(v.files.value.map(f => f.path)).toEqual(['changed.ts'])
  })

  it('reloadPreservingDrillDown falls back to the commit list when the workspace is clean', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    route(wt)
    const v = setup()
    await v.loadProjectHistory()

    v.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['old.ts']))

    // Everything got committed → no working-tree row any more.
    wt.files = []
    await v.reloadPreservingDrillDown()

    expect(v.currentView.value).toBe('commits')
    expect(v.selectedSHA.value).toBeNull()
  })

  it('reloadPreservingDrillDown leaves the commit-list view alone', async () => {
    route({ files: [] })
    const v = setup()
    await v.loadProjectHistory()
    expect(v.currentView.value).toBe('commits')

    await v.reloadPreservingDrillDown()

    expect(v.currentView.value).toBe('commits')
  })
})

describe('useGitHistoryView — files view refresh button', () => {
  function route(wt: { files: Array<Record<string, unknown>> }) {
    mockGitFetch.mockImplementation((url: string) => {
      if (url.startsWith('/api/git/project-history')) {
        return Promise.resolve(okJson({ isGit: true, commits: [], hasMore: false }))
      }
      if (url.startsWith('/api/git/working-tree')) {
        return Promise.resolve(okJson({ files: wt.files }))
      }
      return Promise.resolve(okJson([]))
    })
  }

  it('onFilesRefresh re-fetches the working tree and picks up new changes', async () => {
    const wt = { files: [{ path: 'a.ts', type: 'M', staged: false }] }
    route(wt)
    const v = setup()
    await v.loadProjectHistory()
    v.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['a.ts']))

    wt.files = [{ path: 'b.ts', type: 'A', staged: false }]
    v.onFilesRefresh()
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['b.ts']))
  })

  it('onFilesRefresh re-runs the commit-files load for an immutable commit', async () => {
    mockGitFetch.mockResolvedValue(okJson([{ path: 'c.ts', type: 'M' }]))
    const v = setup()
    v.selectedSHA.value = 'abc123'

    v.onFilesRefresh()

    await vi.waitFor(() => expect(mockGitFetch).toHaveBeenCalledWith('/api/git/commit-files?sha=abc123'))
  })

  it('keeps the existing list on screen while an in-place refresh is in flight', async () => {
    const wt = { files: [{ path: 'a.ts', type: 'M', staged: false }] }
    route(wt)
    const v = setup()
    await v.loadProjectHistory()
    v.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await vi.waitFor(() => expect(v.files.value.map(f => f.path)).toEqual(['a.ts']))

    // Hold the next working-tree response open.
    let release: (value: unknown) => void = () => {}
    mockGitFetch.mockImplementation((url: string) => {
      if (url.startsWith('/api/git/working-tree')) {
        return new Promise(resolve => {
          release = () => resolve(okJson({ files: wt.files }))
        })
      }
      return Promise.resolve(okJson([]))
    })

    v.onFilesRefresh()
    await vi.waitFor(() => expect(v.filesRefreshing.value).toBe(true))

    // The list is still rendered (no spinner, no empty state).
    expect(v.files.value.map(f => f.path)).toEqual(['a.ts'])
    expect(v.filesLoading.value).toBe(false)

    release(null)
    await vi.waitFor(() => expect(v.filesRefreshing.value).toBe(false))
  })
})
