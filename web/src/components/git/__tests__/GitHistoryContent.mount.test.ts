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

const { mockLoadGitBranch, mockGitFetch } = vi.hoisted(() => ({
  mockLoadGitBranch: vi.fn().mockResolvedValue(undefined),
  mockGitFetch: vi.fn(),
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
    state: {
      projectRoot: '/project',
      gitBranch: 'main',
      gitHead: 'abc',
      gitDirty: false,
      gitWorkingTreeChangeCount: 0,
    },
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
  default: { template: '<div class="commit-meta-stub" />' },
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
})

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
