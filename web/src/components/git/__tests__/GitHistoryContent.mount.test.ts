import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import GitHistoryContent from '@/components/git/GitHistoryContent.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
  createI18n: (options?: Record<string, unknown>) => ({
    global: { locale: 'en' },
    install: () => {},
  }),
}))

const { mockLoadGitBranch, mockGitFetch, mockGitState, mockRevealInFileManager } = vi.hoisted(() => ({
  mockLoadGitBranch: vi.fn().mockResolvedValue(undefined),
  mockGitFetch: vi.fn(),
  mockRevealInFileManager: vi.fn(),
  // Shared mutable store state so tests can simulate the workspace changing
  // between tab activations (loadGitBranch updates these in production).
  mockGitState: {
    projectRoot: '/project',
    gitBranch: 'main',
    gitHead: 'abc',
    gitDirty: false,
    gitWorkingTreeChangeCount: 0,
  },
}))
// Spy on the origin-recording reveal helper. The point of the host wiring is
// *which* helper it calls and with what source — the helper's own event
// contract is covered in useFilePathAnnotation.test.ts.
vi.mock('@/composables/useFilePathAnnotation.ts', () => ({
  revealInFileManager: mockRevealInFileManager,
}))
// gitFetch is what GitHistoryContent actually uses for its git API calls
// (project-history, working-tree, commit-files, …). Mock it so mounting does
// not fire real network requests in jsdom; ok:false short-circuits
// loadProjectHistory into its error branch.
vi.mock('@/utils/gitApi', () => ({
  gitFetch: mockGitFetch,
  GitTimeoutError: class GitTimeoutError extends Error {},
  createSeqGuard: () => {
    let current: { __seqToken: true; signal: AbortSignal } | null = null
    let controller: AbortController | null = null
    return {
      token: () => {
        controller?.abort()
        controller = new AbortController()
        current = { __seqToken: true, signal: controller.signal }
        return current
      },
      isCurrent: (t: { __seqToken: true }) => t === current,
    }
  },
}))
vi.mock('@/stores/app', () => ({
  store: {
    state: mockGitState,
    loadGitBranch: mockLoadGitBranch,
    loadFiles: vi.fn().mockResolvedValue(undefined),
  },
}))

// Child components
vi.mock('@/components/git/GitCommitList.vue', () => ({
  default: { template: '<div class="commit-list-stub"><slot /></div>' },
}))
vi.mock('@/components/git/GitManageContent.vue', () => ({
  default: { template: '<div class="manage-stub"><slot /></div>' },
}))
vi.mock('@/components/git/GitCommitMeta.vue', () => ({
  default: {
    props: ['commit', 'isWorkingTree', 'filePath'],
    emits: ['open-file', 'reveal-file'],
    template: '<div class="commit-meta-stub">'
      + '<button class="meta-open-file" @click="$emit(\'open-file\', filePath)" />'
      + '<button class="meta-reveal-file" @click="$emit(\'reveal-file\', filePath)" />'
      + '</div>',
  },
}))
vi.mock('@/components/git/GitDiffView.vue', () => ({
  default: { template: '<div class="diff-view-stub" />' },
}))
vi.mock('@/components/git/GitBreadcrumb.vue', () => ({
  default: { template: '<div class="breadcrumb-stub" />' },
}))
vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { template: '<div class="loading-stub" />' },
}))
vi.mock('@/components/common/FileIcon.vue', () => ({
  default: { template: '<div class="file-icon-stub" />' },
}))
vi.mock('lucide-vue-next', () => ({
  Plus: { template: '<div />' },
  Minus: { template: '<div />' },
  ChevronUp: { template: '<div />' },
  ChevronDown: { template: '<div />' },
  LoaderCircle: { template: '<div />' },
  RefreshCw: { template: '<svg />' },
  RotateCw: { template: '<svg />' },
  RotateCcw: { template: '<svg />' },
  CheckCircle2: { template: '<svg />' },
}))
vi.mock('@/composables/useEdgeSwipeBack', () => ({
  useFeatureBackHandler: vi.fn(),
  PRIORITY_PAGE: 2,
}))
vi.mock('@/composables/useCommitNavigation', () => ({
  useCommitNavigation: () => ({
    navigateToCommit: vi.fn(),
    handleDrillBackToCommits: vi.fn(),
    fetchCommitInfo: vi.fn(),
  }),
  consumePendingCommitNavigation: vi.fn().mockReturnValue(null),
  pendingSha: ref(null),
  consumePendingManageNavigation: vi.fn().mockReturnValue(null),
  pendingManageView: ref(false),
}))
vi.mock('@/composables/useDiffNavigation', () => ({
  useDiffNavigation: () => ({
    navigableFiles: { value: [] },
    total: { value: 0 },
    index: { value: -1 },
    goToFile: vi.fn(),
    prev: vi.fn(),
    next: vi.fn(),
  }),
}))

// gitFetch default: all git API calls fail fast so loadProjectHistory and
// friends short-circuit — this test only exercises the git-badge refresh.
beforeEach(() => {
  vi.clearAllMocks()
  mockGitFetch.mockResolvedValue({ ok: false })
  mockLoadGitBranch.mockClear()
  mockLoadGitBranch.mockResolvedValue(undefined)
  Object.assign(mockGitState, {
    projectRoot: '/project',
    gitBranch: 'main',
    gitHead: 'abc',
    gitDirty: false,
    gitWorkingTreeChangeCount: 0,
  })
})

/**
 * Route gitFetch by endpoint so a test can serve a realistic history + working
 * tree. Returns a mutable `wt` holder so the workspace contents can change
 * between calls (simulating edits while the user is on another tab).
 */
function routeGitFetch(init: {
  commits?: Array<Record<string, unknown>>
  wt?: Array<Record<string, unknown>>
}) {
  const wt = { files: init.wt ?? [] }
  const commits = init.commits ?? [
    { sha: 'c0ffee1', msg: 'first', date: '2026-01-01T00:00:00Z', author: 'a' },
  ]
  mockGitFetch.mockImplementation((url: string) => {
    if (url.startsWith('/api/git/project-history')) {
      return Promise.resolve({ ok: true, json: async () => ({ isGit: true, commits, hasMore: false }) })
    }
    if (url.startsWith('/api/git/working-tree')) {
      return Promise.resolve({ ok: true, json: async () => ({ files: wt.files }) })
    }
    return Promise.resolve({ ok: false, json: async () => ({}) })
  })
  return { wt, commits }
}

/** Make loadGitBranch mirror the real store: it writes the fetched values into state. */
function stubLoadGitBranch(overrides: Partial<typeof mockGitState>) {
  mockLoadGitBranch.mockImplementation(async () => {
    Object.assign(mockGitState, overrides)
    return { isGit: true, ...mockGitState }
  })
}

describe('GitHistoryContent dock badge refresh', () => {
  it('refreshes git branch/change state on mount (first open of history tab)', async () => {
    // TabPanel mounts this component with active already true — the
    // props.active watch never fires, so onMounted must refresh git state.
    mockLoadGitBranch.mockResolvedValue({
      isGit: true, branch: 'main', head: 'abc', dirty: true, changeCount: 5,
    })
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: {
        stubs: { Teleport: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()
    expect(mockLoadGitBranch).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('skips the mount refresh when mounted inactive (project hot-switch rebuild)', async () => {
    // hotSwitchProject rebuilds the component tree while history is not the
    // active tab — the mount refresh would duplicate App.vue's own sync.
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: false },
      global: {
        stubs: { Teleport: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()
    expect(mockLoadGitBranch).not.toHaveBeenCalled()

    // Activating the tab picks it up via the props.active watch.
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(mockLoadGitBranch).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})

describe('GitHistoryContent — files view rendering', () => {
  async function showFilesView(files: Array<{ path: string; type: string; staged?: boolean }>) {
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: {
        stubs: { Teleport: { template: '<div><slot /></div>' } },
      },
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.error = ''
    vm.loading = false
    vm.selectedSHA = 'abc123'
    vm.currentView = 'files'
    vm.files = files
    vm.mergeGroups = []
    await flushPromises()
    return wrapper
  }

  it('renders each change file as a two-line item: bare name + parent directory', async () => {
    const wrapper = await showFilesView([{ path: 'web/src/foo.ts', type: 'M', staged: false }])
    const item = wrapper.find('.drilldown-item')
    expect(item.find('.git-file-name').text()).toBe('foo.ts')
    expect(item.find('.git-file-dir').text()).toBe('web/src')
    expect(item.text()).not.toContain('web/src/foo.ts')
    wrapper.unmount()
  })

  it('omits the directory line for a root-level file', async () => {
    const wrapper = await showFilesView([{ path: 'README.md', type: 'A', staged: false }])
    const item = wrapper.find('.drilldown-item')
    expect(item.find('.git-file-name').text()).toBe('README.md')
    expect(item.find('.git-file-dir').exists()).toBe(false)
    wrapper.unmount()
  })
})

// Regression: entering the working-tree view after the workspace changed used
// to render an empty list. `loadProjectHistory()` clears files/wtFiles but
// leaves `currentView` on 'files', and the WT view was a pure cache read.
describe('GitHistoryContent — working tree view stays fresh', () => {
  function mountActive() {
    return mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: { stubs: { Teleport: { template: '<div><slot /></div>' } } },
    })
  }

  it('re-fetches the working tree when the WT row is selected (not the stale snapshot)', async () => {
    const { wt } = routeGitFetch({ wt: [{ path: 'old.ts', type: 'M', staged: false }] })
    const wrapper = mountActive()
    await flushPromises()
    const vm = wrapper.vm as any

    // Workspace changes while the user is elsewhere.
    wt.files = [{ path: 'new.ts', type: 'A', staged: false }]

    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()

    expect(vm.currentView).toBe('files')
    expect(vm.files.map((f: any) => f.path)).toEqual(['new.ts'])
    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('new.ts')
    wrapper.unmount()
  })

  it('re-entering the tab on the working-tree view refreshes instead of rendering blank', async () => {
    const { wt } = routeGitFetch({ wt: [{ path: 'old.ts', type: 'M', staged: false }] })
    const wrapper = mountActive()
    await flushPromises()
    const vm = wrapper.vm as any
    stubLoadGitBranch({ gitDirty: true, gitWorkingTreeChangeCount: 1 })

    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()
    expect(vm.files.map((f: any) => f.path)).toEqual(['old.ts'])

    // Leave the tab, change the workspace, come back.
    await wrapper.setProps({ active: false })
    wt.files = [{ path: 'changed.ts', type: 'M', staged: false }]
    stubLoadGitBranch({ gitDirty: true, gitWorkingTreeChangeCount: 2 })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(vm.currentView).toBe('files')
    expect(vm.files.map((f: any) => f.path)).toEqual(['changed.ts'])
    // The list is rendered, not the empty state.
    expect(wrapper.find('.git-history-empty').exists()).toBe(false)
    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('changed.ts')
    wrapper.unmount()
  })

  it('falls back to the commit list when the workspace becomes clean', async () => {
    const { wt } = routeGitFetch({ wt: [{ path: 'old.ts', type: 'M', staged: false }] })
    const wrapper = mountActive()
    await flushPromises()
    const vm = wrapper.vm as any

    // 1st re-entry: workspace is dirty → the working-tree row exists and the
    // refresh is recorded, so lastGitState now holds dirty/1.
    stubLoadGitBranch({ gitDirty: true, gitWorkingTreeChangeCount: 1 })
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()
    expect(vm.currentView).toBe('files')

    // 2nd re-entry: everything got committed → no working-tree row any more.
    wt.files = []
    stubLoadGitBranch({ gitDirty: false, gitWorkingTreeChangeCount: 0 })
    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()

    expect(vm.currentView).toBe('commits')
    expect(vm.selectedSHA).toBeNull()
    wrapper.unmount()
  })
})

describe('GitHistoryContent — files view refresh button', () => {
  it('renders a refresh button in the files-view header', async () => {
    routeGitFetch({ wt: [{ path: 'a.ts', type: 'M', staged: false }] })
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: { stubs: { Teleport: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()

    const btn = wrapper.find('.drilldown-header .drilldown-refresh-btn')
    expect(btn.exists()).toBe(true)
    wrapper.unmount()
  })

  it('the refresh button re-fetches the working tree and picks up new changes', async () => {
    const { wt } = routeGitFetch({ wt: [{ path: 'a.ts', type: 'M', staged: false }] })
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: { stubs: { Teleport: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()

    wt.files = [{ path: 'b.ts', type: 'A', staged: false }]
    await wrapper.find('.drilldown-header .drilldown-refresh-btn').trigger('click')
    await flushPromises()

    expect(vm.files.map((f: any) => f.path)).toEqual(['b.ts'])
    expect(wrapper.find('.drilldown-item .git-file-name').text()).toBe('b.ts')
    wrapper.unmount()
  })

  it('spins (and disables) while the in-place working-tree refresh is in flight', async () => {
    const { wt } = routeGitFetch({ wt: [{ path: 'a.ts', type: 'M', staged: false }] })
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: { stubs: { Teleport: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const vm = wrapper.vm as any
    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()

    // Hold the working-tree response open so the in-flight state is observable.
    let release: (v: unknown) => void = () => {}
    mockGitFetch.mockImplementation((url: string) => {
      if (url.startsWith('/api/git/working-tree')) {
        return new Promise(resolve => {
          release = () => resolve({ ok: true, json: async () => ({ files: wt.files }) })
        })
      }
      return Promise.resolve({ ok: false, json: async () => ({}) })
    })

    vm.onFilesRefresh()
    await flushPromises()

    const btn = wrapper.find('.drilldown-header .drilldown-refresh-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    // The existing list stays on screen during the refresh (no empty state).
    expect(wrapper.find('.drilldown-item').exists()).toBe(true)

    release(null)
    await flushPromises()
    expect(wrapper.find('.drilldown-header .drilldown-refresh-btn').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})

// The commit-meta panel delegates both row actions to its host, because the
// same panel also renders inside the mobile file-history sheet (where the
// origin is the file-viewer stack, not the history tab). The host must forward
// them rather than letting the panel navigate on its own.
describe('GitHistoryContent — commit meta file annotation', () => {
  async function mountInDiffView() {
    routeGitFetch({ wt: [{ path: 'a.ts', type: 'M', staged: false }] })
    const wrapper = mount(GitHistoryContent, {
      props: { mode: 'project', active: true },
      global: { stubs: { Teleport: { template: '<div><slot /></div>' } } },
    })
    await flushPromises()
    const vm = wrapper.vm as any
    // The file rows only render in the diff view, where the meta panel is
    // handed the selected file path.
    vm.onCommitSelect({ sha: 'HEAD', isWT: true })
    await flushPromises()
    vm.drillToFile({ path: 'a.ts', type: 'M', staged: false })
    await flushPromises()
    return wrapper
  }

  it('forwards the meta panel open-file event to the host open-file emit', async () => {
    const wrapper = await mountInDiffView()
    const metaBtn = wrapper.find('.commit-meta-stub .meta-open-file')
    expect(metaBtn.exists()).toBe(true)
    await metaBtn.trigger('click')

    expect(wrapper.emitted('open-file')).toEqual([['a.ts']])
    wrapper.unmount()
  })

  it('routes reveal through the origin-recording directory jump, not the bare primitive', async () => {
    const wrapper = await mountInDiffView()
    const metaBtn = wrapper.find('.commit-meta-stub .meta-reveal-file')
    expect(metaBtn.exists()).toBe(true)
    await metaBtn.trigger('click')

    // `source: 'history'` is what makes the jump record a return origin; without
    // it Back would walk up the directory tree instead of returning here.
    expect(mockRevealInFileManager).toHaveBeenCalledTimes(1)
    expect(mockRevealInFileManager).toHaveBeenCalledWith('a.ts', 'history')
    wrapper.unmount()
  })
})
