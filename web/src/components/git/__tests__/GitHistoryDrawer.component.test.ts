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

const { mockGitFetch, mockNavigateToCommit, mockHandleDrillBackToCommits, mockRevealInFileManager } = vi.hoisted(() => ({
  mockGitFetch: vi.fn(),
  mockNavigateToCommit: vi.fn(),
  mockHandleDrillBackToCommits: vi.fn(),
  mockRevealInFileManager: vi.fn(),
}))

// Spy on the origin-recording reveal helper: the host's job is to call it with
// the right source. The helper's own event contract is covered in
// useFilePathAnnotation.test.ts.
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  revealInFileManager: mockRevealInFileManager,
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
  splitGitFilePath: (path: string) => {
    const idx = path.lastIndexOf('/')
    return idx < 0 ? { name: path, dir: '' } : { name: path.slice(idx + 1), dir: path.slice(0, idx) }
  },
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
    props: ['commit', 'isWorkingTree', 'filePath'],
    emits: ['open-file', 'reveal-file'],
    template: '<div class="git-commit-meta-stub">'
      + '<button class="meta-open-file" @click="$emit(\'open-file\', filePath)" />'
      + '<button class="meta-reveal-file" @click="$emit(\'reveal-file\', filePath)" />'
      + '</div>',
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
  LoaderCircle: { template: '<svg />' },
  RefreshCw: { template: '<svg />' },
  RotateCw: { template: '<svg />' },
  RotateCcw: { template: '<svg />' },
  CheckCircle2: { template: '<svg />' },
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
          refresh: 'Refresh change list',
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

  it('forwards the meta panel open-file event and closes the sheet', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    // The file rows only render in the diff view, where the meta panel is
    // handed the selected file path.
    vm.selectedSHA = 'abc123'
    vm.drillToFile({ path: 'src/main.ts', type: 'M', staged: false })
    await flushPromises()

    const metaBtn = wrapper.find('.git-commit-meta-stub .meta-open-file')
    expect(metaBtn.exists()).toBe(true)
    await metaBtn.trigger('click')

    expect(wrapper.emitted('open-file')).toEqual([['src/main.ts']])
    // Opening a file from the sheet must dismiss it, otherwise the viewer is
    // pushed behind an open overlay.
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('reveals via the origin-recording jump with source=file and closes the sheet', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.drillToFile({ path: 'src/main.ts', type: 'M', staged: false })
    await flushPromises()

    const metaBtn = wrapper.find('.git-commit-meta-stub .meta-reveal-file')
    expect(metaBtn.exists()).toBe(true)
    await metaBtn.trigger('click')

    // This sheet lives inside the file view, so source 'file' makes the
    // coordinator suspend the file visit as a directory excursion: Back then
    // restores the viewed file instead of walking up the directory tree.
    expect(mockRevealInFileManager).toHaveBeenCalledTimes(1)
    expect(mockRevealInFileManager).toHaveBeenCalledWith('src/main.ts', 'file')
    // The sheet must not sit over the file manager it just revealed.
    expect(wrapper.emitted('close')).toBeTruthy()
  })

  it('closes the sheet synchronously, before the reveal awaits', async () => {
    // The reveal tears down the file overlay (making room for the manager),
    // which unmounts this sheet. If the close were deferred — e.g. left to the
    // sheet's own 250ms animation timer — Vue would drop the emit on the
    // unmounted instance and `open` would stay true, popping the sheet back open
    // over the restored file on Back. So the host close must precede the reveal.
    let releaseReveal: () => void = () => {}
    mockRevealInFileManager.mockImplementationOnce(
      () => new Promise<void>(resolve => { releaseReveal = resolve })
    )

    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.drillToFile({ path: 'src/main.ts', type: 'M', staged: false })
    await flushPromises()

    // Not awaited: the reveal promise is deliberately still pending here.
    void vm.onRevealFile('src/main.ts')
    await flushPromises()

    expect(mockRevealInFileManager).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeTruthy()

    releaseReveal()
    await flushPromises()
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

describe('GitHistoryDrawer — files view rendering', () => {
  /** Serve a commit-files response and open that commit's file list. */
  async function showCommitFiles(payload: unknown) {
    mockGitFetch.mockImplementation((url: string) => {
      if (url.startsWith('/api/git/commit-files')) return Promise.resolve(okJson(payload))
      return Promise.resolve(okJson({}))
    })
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    const commit = { sha: 'abc123', msg: 'm', date: '', author: '' }
    vm.commits = [commit]
    vm.onCommitSelect(commit)
    await flushPromises()
    return wrapper
  }

  it('renders files view when currentView=files and files present', async () => {
    const wrapper = await showCommitFiles([{ path: 'src/foo.ts', type: 'M', staged: false }])
    expect(wrapper.find('.drilldown-page').exists()).toBe(true)
    // Two-line item: bare name on top, parent directory (no file name) below.
    const item = wrapper.find('.drilldown-item')
    expect(item.find('.git-file-name').text()).toBe('foo.ts')
    expect(item.find('.git-file-dir').text()).toBe('src')
  })

  it('renders merge-group files as two-line items', async () => {
    const wrapper = await showCommitFiles({
      merge: true,
      groups: [{ label: 'main', files: [{ path: 'web/src/a.ts', type: 'M' }] }],
    })
    const item = wrapper.find('.merge-group .drilldown-item')
    expect(item.find('.git-file-name').text()).toBe('a.ts')
    expect(item.find('.git-file-dir').text()).toBe('web/src')
    expect(item.text()).not.toContain('web/src/a.ts')
  })

  it('shows empty state when no file changes', async () => {
    const wrapper = await showCommitFiles([])
    expect(wrapper.find('.git-history-empty').exists()).toBe(true)
  })

  it('shows loading indicator when filesLoading=true', async () => {
    const wrapper = mountDrawer()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.filesLoading = true
    await flushPromises()
    expect(wrapper.find('.git-history-loading').exists()).toBe(true)
  })

  it('shows merge groups when present', async () => {
    const wrapper = await showCommitFiles({
      merge: true,
      groups: [{ label: 'main', files: [{ path: 'src/a', type: 'M' }] }],
    })
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

// Regression: re-opening the drawer while drilled into the working-tree file
// list used to render an empty list. The open watcher reloads history, which
// clears files/wtFiles but leaves currentView on 'files'; the WT view was a
// pure cache read with no on-demand re-fetch.
//
// These assert on the rendered DOM (not on component state) because the state
// now lives in useGitHistoryView — the component-level contract is "the list
// the user sees is fresh".
describe('GitHistoryDrawer — working tree view stays fresh', () => {
  /** Route gitFetch so the workspace contents can change between calls. */
  function routeGitFetch(wt: { files: Array<Record<string, unknown>> }) {
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

  /**
   * The drawer is mounted closed in production (FileOverlay renders it with
   * :open="fileHistoryOpen"), and lastProjectRoot is only seeded inside the
   * open watcher — so the FIRST open always counts as an identity change and
   * resets state. Model the real cycle: mount closed, then open.
   */
  async function mountAndOpen(wt: { files: Array<Record<string, unknown>> }) {
    routeGitFetch(wt)
    const wrapper = mountDrawer({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()
    return wrapper
  }

  /** Open the synthetic working-tree row through the commit list. */
  async function selectWorkingTree(wrapper: ReturnType<typeof mountDrawer>) {
    const vm = wrapper.vm as any
    vm.commits = [{ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true }]
    vm.onCommitSelect({ sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true })
    await flushPromises()
  }

  it('re-fetches the working tree when the WT row is selected (not the stale snapshot)', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    const wrapper = await mountAndOpen(wt)

    // Workspace changes while the user is looking at something else.
    wt.files = [{ path: 'new.ts', type: 'A', staged: false }]
    await selectWorkingTree(wrapper)

    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('new.ts')
    wrapper.unmount()
  })

  it('re-opening on the working-tree view refreshes instead of rendering blank', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    const wrapper = await mountAndOpen(wt)
    await selectWorkingTree(wrapper)
    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('old.ts')

    // Close, change the workspace, re-open.
    await wrapper.setProps({ open: false })
    wt.files = [{ path: 'changed.ts', type: 'M', staged: false }]
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect(wrapper.find('.git-history-empty').exists()).toBe(false)
    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('changed.ts')
    wrapper.unmount()
  })

  it('falls back to the commit list when the workspace becomes clean', async () => {
    const wt = { files: [{ path: 'old.ts', type: 'M', staged: false }] }
    const wrapper = await mountAndOpen(wt)
    await selectWorkingTree(wrapper)
    expect(wrapper.find('.drilldown-page').exists()).toBe(true)

    // Everything got committed → no working-tree row any more.
    await wrapper.setProps({ open: false })
    wt.files = []
    await wrapper.setProps({ open: true })
    await flushPromises()

    expect((wrapper.vm as any).currentView).toBe('commits')
    expect((wrapper.vm as any).selectedSHA).toBeNull()
    wrapper.unmount()
  })
})

describe('GitHistoryDrawer — files view refresh button', () => {
  function routeGitFetch(wt: { files: Array<Record<string, unknown>> }) {
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

  async function mountAndOpen(wt: { files: Array<Record<string, unknown>> }) {
    routeGitFetch(wt)
    const wrapper = mountDrawer({ open: false })
    await flushPromises()
    await wrapper.setProps({ open: true })
    await flushPromises()
    return wrapper
  }

  async function selectWorkingTree(wrapper: ReturnType<typeof mountDrawer>) {
    const vm = wrapper.vm as any
    const row = { sha: 'HEAD', msg: 'wt', date: '', author: '', isWT: true }
    vm.commits = [row]
    vm.onCommitSelect(row)
    await flushPromises()
  }

  it('renders a refresh button in the files-view header', async () => {
    const wrapper = await mountAndOpen({ files: [{ path: 'a.ts', type: 'M', staged: false }] })
    await selectWorkingTree(wrapper)

    expect(wrapper.find('.drilldown-header .drilldown-refresh-btn').exists()).toBe(true)
    wrapper.unmount()
  })

  it('the refresh button re-fetches the working tree and picks up new changes', async () => {
    const wt = { files: [{ path: 'a.ts', type: 'M', staged: false }] }
    const wrapper = await mountAndOpen(wt)
    await selectWorkingTree(wrapper)

    wt.files = [{ path: 'b.ts', type: 'A', staged: false }]
    await wrapper.find('.drilldown-header .drilldown-refresh-btn').trigger('click')
    await flushPromises()

    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('b.ts')
    wrapper.unmount()
  })
})
