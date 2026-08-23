import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string, params?: Record<string, any>) => {
    if (params) return `${k}:${JSON.stringify(params)}`
    return k
  }, locale: { value: 'en' } }),
  createI18n: (opts: any) => ({
    global: {
      t: (k: string) => k,
      locale: { value: opts?.locale ?? 'en' },
    },
    install() {},
  }),
}))

const { mockGitFetch, mockNavigateToCommit, mockHandleDrillBackToCommits } = vi.hoisted(() => ({
  mockGitFetch: vi.fn(),
  mockNavigateToCommit: vi.fn(),
  mockHandleDrillBackToCommits: vi.fn(),
}))

vi.mock('@/utils/gitApi', () => ({
  gitFetch: mockGitFetch,
  GitTimeoutError: class GitTimeoutError extends Error {
    constructor(url: string) { super(`git timeout ${url}`); this.name = 'GitTimeoutError' }
  },
  createSeqGuard: () => {
    let current: any = null
    return {
      token: () => {
        current = { __seqToken: true, signal: new AbortController().signal }
        return current
      },
      isCurrent: (t: any) => t === current,
    }
  },
}))

vi.mock('@/stores/app.ts', () => ({
  store: { state: { projectRoot: '/proj', currentDir: '', dirEntries: [] } },
}))

vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_OVERLAY: 50,
}))

vi.mock('@/composables/useCommitNavigation.ts', () => ({
  useCommitNavigation: () => ({
    navigateToCommit: mockNavigateToCommit,
    handleDrillBackToCommits: mockHandleDrillBackToCommits,
  }),
  consumePendingCommitNavigation: () => null,
  consumePendingManageNavigation: () => false,
}))

vi.mock('@/utils/diff.ts', () => ({
  renderDiff: (s: string) => `<div>${s}</div>`,
}))

vi.mock('@/utils/gitFileHistory.ts', () => ({
  buildFileHistoryCommits: (commits: any[], has: boolean, msg: string) =>
    has ? [{ sha: 'HEAD', msg, isWT: true }, ...commits] : commits,
  shouldShowFullLoading: (commits: any[], err: string) => commits.length === 0 && !err,
}))

vi.mock('@/components/common/BottomSheet.vue', () => ({
  default: defineComponent({
    name: 'BottomSheet',
    props: ['open', 'title'],
    emits: ['close'],
    template: `<div class="bs-stub" :data-open="String(open)">
      <div class="bs-stub-header"><slot name="header" /></div>
      <div class="bs-stub-body"><slot /></div>
      <div class="bs-stub-footer"><slot name="footer" /></div>
    </div>`,
    methods: { close() { this.$emit('close') } },
  }),
}))

vi.mock('@/components/common/HeaderMarquee.vue', () => ({
  default: defineComponent({
    name: 'HeaderMarquee',
    props: ['text'],
    template: '<span class="header-marquee-stub">{{ text }}</span>',
  }),
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: defineComponent({
    name: 'LoadingIndicator',
    props: ['size'],
    template: '<div class="loading-stub" />',
  }),
}))

vi.mock('@/components/common/FileIcon.vue', () => ({
  default: defineComponent({
    name: 'FileIcon',
    props: ['path', 'isDir', 'size'],
    template: '<span class="file-icon-stub" />',
  }),
}))

vi.mock('@/components/git/GitCommitList.vue', () => ({
  default: defineComponent({
    name: 'GitCommitList',
    props: ['commits', 'isGit', 'hasMore', 'loadingMore', 'searchLoading', 'loading', 'error', 'untracked', 'countLabel', 'selectedSHA', 'mode'],
    emits: ['select', 'search', 'load-more', 'refresh'],
    template: '<div class="git-commit-list-stub" @click="$emit(\'select\', commits && commits[0])" />',
    methods: {
      observeList() {},
      unobserveList() {},
      get commitSearch() { return '' },
      set commitSearch(v: string) {},
    },
  }),
}))

vi.mock('@/components/git/GitCommitMeta.vue', () => ({
  default: defineComponent({
    name: 'GitCommitMeta',
    props: ['commit', 'isWorkingTree'],
    template: '<div class="git-commit-meta-stub" />',
  }),
}))

vi.mock('@/components/git/GitDiffView.vue', () => ({
  default: defineComponent({
    name: 'GitDiffView',
    props: ['loading', 'empty', 'html', 'noWrap', 'filePath'],
    template: '<div class="git-diff-view-stub" />',
  }),
}))

vi.mock('@/components/git/GitBreadcrumb.vue', () => ({
  default: defineComponent({
    name: 'GitBreadcrumb',
    props: ['mode', 'currentView', 'selectedCommit', 'selectedFilePath'],
    emits: ['navigate', 'open-file'],
    template: '<div class="git-breadcrumb-stub" @click="$emit(\'navigate\', \'commits\')" />',
  }),
}))

vi.mock('lucide-vue-next', () => ({
  GitBranch: { template: '<svg />' },
  Plus: { template: '<svg />' },
  Minus: { template: '<svg />' },
}))

import { createI18n } from 'vue-i18n'
import GitHistoryDrawer from '@/components/git/GitHistoryDrawer.vue'

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      git: {
        history: {
          fileHistory: 'File history',
          projectHistory: 'Project history',
          records: 'records',
          commitRecords: 'commit records',
          loadError: 'Load error',
          loadTimeout: 'Timeout',
          noFileChanges: 'No changes',
          mergedFrom: 'Merged from {label}',
          staged: 'Staged',
          unstaged: 'Unstaged',
          fileCount: '{count} files',
          workingTreeChanges: 'Working tree',
        },
        fileType: {
          added: 'added', modified: 'modified', deleted: 'deleted',
          renamed: 'renamed', untracked: 'untracked', stagedPrefix: 'staged: ',
        },
      },
    },
  },
})

function mountDrawer(props: Record<string, unknown> = {}) {
  return mount(GitHistoryDrawer, {
    props: { open: true, mode: 'project', ...props },
    global: {
      stubs: { Teleport: true },
      plugins: [i18n],
    },
  })
}

function okJson(data: any) {
  return { ok: true, json: () => Promise.resolve(data) }
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGitFetch.mockReset()
  mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [], hasMore: false }))
})

describe('GitHistoryDrawer — mount and close', () => {
  it('mounts without errors in project mode', () => {
    const wrapper = mountDrawer()
    expect(wrapper.exists()).toBe(true)
  })

  it('mounts without errors in file mode', () => {
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/main.ts' } })
    expect(wrapper.exists()).toBe(true)
  })

  it('emits close when bottom sheet closes', async () => {
    const wrapper = mountDrawer()
    const bs = wrapper.findComponent({ name: 'BottomSheet' })
    await bs.vm.$emit('close')
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('renders commit list view by default', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    expect(wrapper.find('.git-commit-list-stub').exists()).toBe(true)
  })

  it('handles open-file event and emits open-file', async () => {
    const wrapper = mountDrawer()
    const vm = wrapper.vm as any
    vm.onOpenFile('src/main.ts')
    expect(wrapper.emitted('open-file')?.[0]).toEqual(['src/main.ts'])
  })
})

describe('GitHistoryDrawer — loading states', () => {
  it('shows loading indicator when loading=true', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.loading = true
    await flushPromises()
    expect(wrapper.find('.git-history-loading').exists()).toBe(true)
  })

  it('shows error message when error is set', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.loading = false
    vm.error = 'boom'
    await flushPromises()
    expect(wrapper.find('.git-history-error').exists()).toBe(true)
  })
})

describe('GitHistoryDrawer — navigation', () => {
  it('drillBack("commits") resets selection state', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.drillBack('commits')
    expect(vm.selectedSHA).toBe(null)
    expect(vm.currentView).toBe('commits')
    expect(mockHandleDrillBackToCommits).toHaveBeenCalled()
  })

  it('drillBack("files") resets file path', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedFilePath = 'src/foo.ts'
    vm.currentView = 'diff'
    vm.drillBack('files')
    expect(vm.selectedFilePath).toBe(null)
    expect(vm.currentView).toBe('files')
  })

  it('drillToFile sets file path and switches to diff view', async () => {
    mockGitFetch.mockResolvedValue(okJson({ diff: 'sample diff' }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.drillToFile({ path: 'src/foo.ts', type: 'M' })
    expect(vm.selectedFilePath).toBe('src/foo.ts')
    expect(vm.currentView).toBe('diff')
  })

  it('onCommitSelect for project HEAD commits uses wtFiles', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.wtFiles = [{ path: 'foo', type: 'M' }]
    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    expect(vm.currentView).toBe('files')
    expect(vm.files).toEqual([{ path: 'foo', type: 'M' }])
  })

  it('onCommitSelect for project non-HEAD calls loadCommitFiles', async () => {
    mockGitFetch.mockResolvedValue(okJson([{ path: 'src/x', type: 'M' }]))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onCommitSelect({ sha: 'abc123' })
    expect(vm.currentView).toBe('files')
  })

  it('onCommitSelect for file mode switches to diff', async () => {
    mockGitFetch.mockResolvedValue(okJson({ diff: 'data' }))
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/x.ts' } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onCommitSelect({ sha: 'abc123' })
    expect(vm.currentView).toBe('diff')
  })
})

describe('GitHistoryDrawer — helpers', () => {
  it('fileTypeLabel returns i18n key for known types', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.fileTypeLabel('A', false)).toBe('git.fileType.added')
    expect(vm.fileTypeLabel('M', true)).toContain('git.fileType.stagedPrefix')
    expect(vm.fileTypeLabel('Q', false)).toBe('Q')
  })

  it('badgeClass maps type to class name', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.badgeClass({ type: 'A' })).toBe('badge-A')
    expect(vm.badgeClass({ type: 'M' })).toBe('badge-M')
    expect(vm.badgeClass({ type: 'D' })).toBe('badge-D')
    expect(vm.badgeClass({ type: 'R' })).toBe('badge-R')
    expect(vm.badgeClass({ type: '?' })).toBe('badge-U')
    expect(vm.badgeClass({ type: 'Q' })).toBe('badge-M')
    expect(vm.badgeClass({ type: 'M', staged: true })).toBe('badge-M badge-staged')
  })

  it('resetState clears all state', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.commits = [{ sha: 'a' }]
    vm.error = 'oops'
    vm.resetState()
    expect(vm.commits).toEqual([])
    expect(vm.error).toBe('')
    expect(vm.currentView).toBe('commits')
  })
})

describe('GitHistoryDrawer — loadMoreCommits', () => {
  it('loadMoreCommits skips when not hasMore', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.hasMore = false
    await vm.loadMoreCommits()
    expect(mockGitFetch).not.toHaveBeenCalledWith(expect.stringContaining('skip='), expect.anything())
  })

  it('loadMoreCommits fetches and appends commits', async () => {
    mockGitFetch.mockResolvedValue(okJson({ commits: [{ sha: 'new1' }], hasMore: false }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.commits = [{ sha: 'a' }]
    vm.hasMore = true
    vm.isGit = true
    await vm.loadMoreCommits()
    expect(vm.commits.length).toBe(2)
    expect(vm.hasMore).toBe(false)
  })
})

describe('GitHistoryDrawer — computed properties', () => {
  it('selectedCommit returns matching commit or null', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.commits = [{ sha: 'a' }, { sha: 'b' }]
    vm.selectedSHA = 'b'
    expect(vm.selectedCommit).toEqual({ sha: 'b' })
    vm.selectedSHA = 'missing'
    expect(vm.selectedCommit).toBe(null)
  })

  it('isWorkingTree is true only for HEAD', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'HEAD'
    expect(vm.isWorkingTree).toBe(true)
    vm.selectedSHA = 'abc'
    expect(vm.isWorkingTree).toBe(false)
  })

  it('totalFileCount sums merge groups', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.mergeGroups = [{ label: 'A', files: [{ path: 'x' }, { path: 'y' }] }]
    expect(vm.totalFileCount).toBe(2)
    vm.mergeGroups = []
    vm.files = [{ path: 'z' }]
    expect(vm.totalFileCount).toBe(1)
  })

  it('hasStaged/hasUnstaged reflect stagedFiles/unstagedFiles', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.files = [{ path: 'a', type: 'M', staged: true }]
    expect(vm.hasStaged).toBe(true)
    expect(vm.hasUnstaged).toBe(false)
    vm.files = [{ path: 'a', type: 'M', staged: false }]
    expect(vm.hasStaged).toBe(false)
    expect(vm.hasUnstaged).toBe(true)
  })

  it('sortedFiles sorts by type order', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.files = [{ path: 'a', type: 'D' }, { path: 'b', type: 'A' }, { path: 'c', type: 'M' }]
    expect(vm.sortedFiles.map((f: any) => f.path)).toEqual(['c', 'b', 'a'])
  })

  it('mode computed reflects props.mode', async () => {
    const wrapper = mountDrawer({ mode: 'file' })
    expect((wrapper.vm as any).mode).toBe('file')
  })
})

describe('GitHistoryDrawer — loadProjectHistory', () => {
  it('loads commits and prepends WT entry when working tree exists', async () => {
    let callIdx = 0
    mockGitFetch.mockImplementation(() => {
      callIdx++
      if (callIdx === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a' }], hasMore: false }))
      if (callIdx === 2) return Promise.resolve(okJson({ files: [{ path: 'x', type: 'M' }] }))
      return Promise.resolve(okJson({}))
    })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadProjectHistory()
    await flushPromises()
    expect(vm.commits[0].sha).toBe('HEAD')
    expect(vm.commits[0].isWT).toBe(true)
    expect(vm.commits.length).toBe(2)
  })

  it('handles isGit=false response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ isGit: false }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadProjectHistory()
    expect(vm.isGit).toBe(false)
    expect(vm.commits).toEqual([])
  })

  it('handles error response', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'failed' }) })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadProjectHistory()
    expect(vm.error).toBe('failed')
  })

  it('handles GitTimeoutError', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    const { GitTimeoutError } = await import('@/utils/gitApi')
    mockGitFetch.mockImplementationOnce(() => { throw new GitTimeoutError('url') })
    await vm.loadProjectHistory()
    expect(vm.error).toBe('git.history.loadTimeout')
  })

  it('handles generic error', async () => {
    mockGitFetch.mockImplementationOnce(() => { throw new Error('net') })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadProjectHistory()
    expect(vm.error).toBe('git.history.loadError')
  })
})

describe('GitHistoryDrawer — loadFileHistory', () => {
  it('loads commits for file with uncommitted changes', async () => {
    let callIdx = 0
    mockGitFetch.mockImplementation(() => {
      callIdx++
      if (callIdx === 1) return Promise.resolve(okJson({ isGit: true, commits: [{ sha: 'a' }] }))
      if (callIdx === 2) return Promise.resolve(okJson({ hasUncommitted: true }))
      return Promise.resolve(okJson({}))
    })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadFileHistory('src/foo.ts')
    expect(vm.commits[0].sha).toBe('HEAD')
    expect(vm.isGit).toBe(true)
  })

  it('handles not a git repo', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ isGit: false }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadFileHistory('src/foo.ts')
    expect(vm.isGit).toBe(false)
  })
})

describe('GitHistoryDrawer — loadCommitFiles', () => {
  it('sets files when array response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson([{ path: 'a', type: 'M' }]))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadCommitFiles('abc')
    expect(vm.files.length).toBe(1)
  })

  it('sets mergeGroups when merge response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ merge: true, groups: [{ label: 'main', files: [{ path: 'a' }] }] }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadCommitFiles('abc')
    expect(vm.mergeGroups.length).toBe(1)
  })

  it('handles error response', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    await vm.loadCommitFiles('abc')
    expect(vm.files).toEqual([])
  })
})

describe('GitHistoryDrawer — loadDiff', () => {
  it('project mode: renders html from diff response', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ diff: '+added', empty: false }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc'
    vm.selectedFilePath = 'src/foo.ts'
    await vm.loadDiff()
    expect(vm.diffState.html).toContain('added')
    expect(vm.diffState.empty).toBe(false)
  })

  it('sets empty when diff is empty', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ empty: true }))
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc'
    vm.selectedFilePath = 'src/foo.ts'
    await vm.loadDiff()
    expect(vm.diffState.empty).toBe(true)
  })

  it('sets empty when response is not ok', async () => {
    mockGitFetch.mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({}) })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc'
    vm.selectedFilePath = 'src/foo.ts'
    await vm.loadDiff()
    expect(vm.diffState.empty).toBe(true)
  })

  it('file mode uses different endpoint', async () => {
    mockGitFetch.mockResolvedValueOnce(okJson({ diff: 'data', empty: false }))
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/main.ts' } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc'
    await vm.loadDiff()
    expect(mockGitFetch).toHaveBeenCalledWith(expect.stringContaining('/api/git/diff'))
  })
})

describe('GitHistoryDrawer — onRefresh', () => {
  it('refetches project history', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.commits = [{ sha: 'a' }]
    mockGitFetch.mockClear()
    mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [], hasMore: false }))
    await vm.onRefresh()
    await flushPromises()
    expect(mockGitFetch).toHaveBeenCalledWith('/api/git/project-history')
  })

  it('refetches file history when in file mode', async () => {
    mockGitFetch.mockResolvedValue(okJson({ isGit: true, commits: [] }))
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/foo.ts' } })
    await flushPromises()
    const vm = wrapper.vm as any
    mockGitFetch.mockClear()
    await vm.onRefresh()
    await flushPromises()
    expect(mockGitFetch).toHaveBeenCalledWith(expect.stringContaining('/api/git/history'))
  })
})

describe('GitHistoryDrawer — onSearch', () => {
  it('does nothing for empty query', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    mockGitFetch.mockClear()
    await vm.onSearch('   ')
    expect(mockGitFetch).not.toHaveBeenCalled()
  })

  it('does nothing in file mode', async () => {
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/foo.ts' } })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.isGit = true
    mockGitFetch.mockClear()
    await vm.onSearch('foo')
    expect(mockGitFetch).not.toHaveBeenCalled()
  })
})

describe('GitHistoryDrawer — files view rendering', () => {
  it('renders files view when currentView=files and files present', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.files = [{ path: 'src/foo.ts', type: 'M', staged: false }]
    vm.selectedCommit = { sha: 'abc123' }
    vm.totalFileCount = 1
    await flushPromises()
    expect(wrapper.find('.drilldown-page').exists()).toBe(true)
  })

  it('shows empty state when no file changes', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.files = []
    vm.mergeGroups = []
    vm.selectedCommit = { sha: 'abc123' }
    vm.totalFileCount = 0
    await flushPromises()
    expect(wrapper.find('.git-history-empty').exists()).toBe(true)
  })

  it('shows loading indicator when filesLoading=true', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.filesLoading = true
    vm.selectedCommit = { sha: 'abc123' }
    vm.totalFileCount = 0
    await flushPromises()
    expect(wrapper.find('.git-history-loading').exists()).toBe(true)
  })

  it('shows merge groups when present', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.mergeGroups = [{ label: 'main', files: [{ path: 'src/a', type: 'M' }] }]
    vm.selectedCommit = { sha: 'abc123' }
    vm.totalFileCount = 1
    await flushPromises()
    expect(wrapper.find('.merge-group').exists()).toBe(true)
  })
})

describe('GitHistoryDrawer — diff view rendering', () => {
  it('renders diff view when currentView=diff', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.selectedCommit = { sha: 'abc123' }
    vm.currentView = 'diff'
    await flushPromises()
    expect(wrapper.find('.git-diff-view-stub').exists()).toBe(true)
  })
})

describe('GitHistoryDrawer — file mode rendering', () => {
  it('shows file header when file prop has path', async () => {
    const wrapper = mountDrawer({ mode: 'file', file: { path: 'src/foo.ts' } })
    await flushPromises()
    expect(wrapper.find('.bs-header-description').exists()).toBe(true)
  })
})