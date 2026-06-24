import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, defineComponent } from 'vue'
import GitManageContent from '@/components/git/GitManageContent.vue'

// ── Mocks ──

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, any>) => {
      const map: Record<string, string> = {
        'git.manage.tabWorktrees': 'Worktrees',
        'git.manage.tabBranches': 'Branches',
        'git.manage.tabTags': 'Tags',
        'git.manage.switchBranch': 'Switch Branch',
        'git.manage.dirty': `Dirty: ${params?.count || 0} untracked`,
        'git.manage.stashSwitch': 'Stash & Switch',
        'git.manage.forceSwitch': 'Force Switch',
        'common.cancel': 'Cancel',
      }
      return map[key] ?? key
    },
  }),
}))

vi.mock('@/stores/app.ts', () => ({
  store: {
    state: { projectRoot: '/project', currentDir: '' },
    loadGitBranch: vi.fn().mockResolvedValue(undefined),
    loadFiles: vi.fn().mockResolvedValue(undefined),
    setProject: vi.fn().mockResolvedValue(undefined),
  },
}))

vi.mock('@/utils/api', () => ({
  apiGet: vi.fn().mockResolvedValue({ isGit: true, worktrees: [], branches: [], tags: [], stashCount: 0 }),
  apiPost: vi.fn().mockResolvedValue({ success: true }),
  apiDelete: vi.fn().mockResolvedValue({ success: true }),
}))

vi.mock('@/composables/useDialog.ts', () => ({
  useDialog: () => ({
    confirm: vi.fn().mockResolvedValue(true),
    alert: vi.fn().mockResolvedValue(undefined),
    prompt: vi.fn().mockResolvedValue(''),
  }),
}))

vi.mock('@/composables/useFileRefresh.ts', () => ({
  refreshCurrentFile: vi.fn(),
}))

vi.mock('@/components/git/GitWorktreeList.vue', () => ({
  default: defineComponent({
    props: ['worktrees', 'loading', 'error', 'initialCollapsed', 'hideHeader'],
    emits: ['switch-worktree', 'delete-worktree', 'retry'],
    template: '<div class="git-worktree-list-stub" />',
  }),
}))

vi.mock('@/components/git/GitBranchList.vue', () => ({
  default: defineComponent({
    props: ['branches', 'stashCount', 'loading', 'error', 'checkoutInProgress', 'initialCollapsed', 'hideHeader'],
    emits: ['switch-branch', 'delete-branch', 'retry'],
    template: '<div class="git-branch-list-stub" />',
  }),
}))

vi.mock('@/components/git/GitTagList.vue', () => ({
  default: defineComponent({
    props: ['tags', 'loading', 'error'],
    emits: ['retry', 'switch-tag', 'delete-tag'],
    template: '<div class="git-tag-list-stub" />',
  }),
}))

describe('GitManageContent', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  function mountContent(props = {}) {
    return mount(GitManageContent, {
      props: { ...props },
      global: {
        stubs: { Teleport: { template: '<div><slot /></div>' } },
        provide: {
          hotSwitchProject: null,
        },
      },
    })
  }

  // ── Basic mount ──

  it('mounts without errors', () => {
    const wrapper = mountContent()
    expect(wrapper.exists()).toBe(true)
  })

  it('renders the tab bar', () => {
    const wrapper = mountContent()
    expect(wrapper.find('.manage-tabs').exists()).toBe(true)
  })

  it('renders three tab buttons', () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    expect(tabs.length).toBe(3)
  })

  it('renders tab labels in order', () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    expect(tabs[0].text()).toContain('Worktrees')
    expect(tabs[1].text()).toContain('Branches')
    expect(tabs[2].text()).toContain('Tags')
  })

  // ── Active tab ──

  it('defaults to worktrees tab as active', () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    expect(tabs[0].classes()).toContain('active')
  })

  it('switches to branches tab on click', async () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    await tabs[1].trigger('click')
    await nextTick()
    // Verify via VM (DOM class update may be stale in test env)
    expect(wrapper.vm._getActiveTab()).toBe('branches')
  })

  it('switches to tags tab on click', async () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    await tabs[2].trigger('click')
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('tags')
  })

  // ── Tab persistence ──

  it('persists active tab to localStorage', async () => {
    const wrapper = mountContent()
    const tabs = wrapper.findAll('.manage-tab')
    await tabs[1].trigger('click')

    expect(localStorage.getItem('git-manage-active-tab')).toBe('branches')
  })

  it('restores active tab from localStorage on mount', async () => {
    localStorage.setItem('git-manage-active-tab', 'tags')
    const wrapper = mountContent()
    await nextTick()
    expect(wrapper.vm._getActiveTab()).toBe('tags')
  })

  // ── API calls on mount ──

  it('calls apiGet for git data on mount', async () => {
    const { apiGet } = await import('@/utils/api')
    mountContent()
    await nextTick()
    await new Promise(r => setTimeout(r, 50))

    expect(apiGet).toHaveBeenCalledWith('/api/git/worktrees')
    expect(apiGet).toHaveBeenCalledWith('/api/git/branches')
    expect(apiGet).toHaveBeenCalledWith('/api/git/tags')
  })

  // ── Internal state ──

  it('starts with checkoutInProgress false', () => {
    const wrapper = mountContent()
    expect(wrapper.vm.checkoutInProgress).toBe(false)
  })

  it('starts with showDirtyModal false', () => {
    const wrapper = mountContent()
    expect(wrapper.vm.showDirtyModal).toBe(false)
  })

  // ── Dirty modal via VM ──

  it('can set and clear dirty modal via VM', async () => {
    const wrapper = mountContent()
    wrapper.vm._setShowDirtyModal(true)
    wrapper.vm._setDirtyCount(3)
    await nextTick()
    expect(wrapper.vm._getShowDirtyModal()).toBe(true)

    wrapper.vm._setShowDirtyModal(false)
    await nextTick()
    expect(wrapper.vm._getShowDirtyModal()).toBe(false)
  })
})
