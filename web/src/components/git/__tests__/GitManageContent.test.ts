import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, nextTick } from 'vue'
import GitManageContent from '@/components/git/GitManageContent.vue'

// ── Mocks ────────────────────────────────────────────────────
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const mockApiGet = vi.fn()
const mockApiPost = vi.fn()
const mockApiDelete = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
  apiPost: (...args: any[]) => mockApiPost(...args),
  apiDelete: (...args: any[]) => mockApiDelete(...args),
}))

const mockDialogConfirm = vi.fn().mockResolvedValue(true)
const mockDialogAlert = vi.fn().mockResolvedValue(undefined)
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    confirm: mockDialogConfirm,
    alert: mockDialogAlert,
  }),
}))

vi.mock('@/composables/useFileRefresh', () => ({
  refreshCurrentFile: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  store: {
    state: { currentDir: '', projectRoot: '/project' },
    loadGitBranch: vi.fn().mockResolvedValue(undefined),
    loadFiles: vi.fn().mockResolvedValue(undefined),
    setProject: vi.fn().mockResolvedValue(undefined),
  },
}))

// Stub child components
vi.mock('@/components/git/GitWorktreeList.vue', () => ({
  default: { template: '<div class="worktree-list-stub"><slot /></div>' },
}))
vi.mock('@/components/git/GitBranchList.vue', () => ({
  default: { template: '<div class="branch-list-stub"><slot /></div>' },
}))
vi.mock('@/components/git/GitTagList.vue', () => ({
  default: { template: '<div class="tag-list-stub"><slot /></div>' },
}))

function mountContent(props = {}) {
  return mount(GitManageContent, {
    props,
    global: {
      stubs: { Teleport: { template: '<div><slot /></div>' } },
      provide: {
        hotSwitchProject: null,
      },
    },
  })
}

describe('GitManageContent', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockApiGet.mockReset()
    mockApiPost.mockReset()
    mockApiDelete.mockReset()
    // Default: all load endpoints succeed
    mockApiGet.mockImplementation((url: string) => {
      if (url.includes('worktrees')) return Promise.resolve({ isGit: true, worktrees: [] })
      if (url.includes('branches')) return Promise.resolve({ isGit: true, branches: [], stashCount: 0 })
      if (url.includes('tags')) return Promise.resolve({ isGit: true, tags: [] })
      return Promise.resolve({})
    })
  })

  describe('tab switching', () => {
    it('renders worktree tab by default', async () => {
      const wrapper = mountContent()
      await flushPromises()
      expect(wrapper.vm.activeTab).toBe('worktrees')
    })

    it('switches to branches tab', async () => {
      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setActiveTab('branches')
      await nextTick()
      expect(wrapper.vm._getActiveTab()).toBe('branches')
    })

    it('switches to tags tab', async () => {
      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setActiveTab('tags')
      await nextTick()
      expect(wrapper.vm._getActiveTab()).toBe('tags')
    })

    it('persists active tab to localStorage', async () => {
      const setItemSpy = vi.spyOn(Storage.prototype, 'setItem')
      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setActiveTab('branches')
      await nextTick()
      expect(setItemSpy).toHaveBeenCalledWith('git-manage-active-tab', 'branches')
      setItemSpy.mockRestore()
    })

    it('restores tab from localStorage on mount', async () => {
      vi.spyOn(Storage.prototype, 'getItem').mockReturnValue('tags')
      const wrapper = mountContent()
      await flushPromises()
      expect(wrapper.vm.activeTab).toBe('tags')
      vi.restoreAllMocks()
    })
  })

  describe('tab counts', () => {
    it('populates worktrees, branches, tags from API', async () => {
      mockApiGet.mockImplementation((url: string) => {
        if (url.includes('worktrees')) return Promise.resolve({ isGit: true, worktrees: [{ path: '/w1' }] })
        if (url.includes('branches')) return Promise.resolve({ isGit: true, branches: [{ name: 'main' }, { name: 'dev' }], stashCount: 0 })
        if (url.includes('tags')) return Promise.resolve({ isGit: true, tags: [{ name: 'v1' }] })
        return Promise.resolve({})
      })

      const wrapper = mountContent()
      await flushPromises()
      await nextTick()

      // Verify data loaded via VM (DOM may not render due to stub issues)
      expect(wrapper.vm.worktrees.length).toBe(1)
      expect(wrapper.vm.branches.length).toBe(2)
      expect(wrapper.vm.tags.length).toBe(1)
    })
  })

  describe('onSwitchWorktree', () => {
    it('calls store.setProject when no hotSwitchProject', async () => {
      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchWorktree({ path: '/worktree/path' })

      const { store } = await import('@/stores/app')
      expect(store.setProject).toHaveBeenCalledWith('/worktree/path')
    })

    it('calls hotSwitchProject when injected', async () => {
      const mockHotSwitch = vi.fn().mockResolvedValue(undefined)
      const wrapper = mount(GitManageContent, {
        global: {
          stubs: { Teleport: { template: '<div><slot /></div>' } },
          provide: {
            hotSwitchProject: mockHotSwitch,
          },
        },
      })
      await flushPromises()

      await wrapper.vm.onSwitchWorktree({ path: '/worktree/path' })

      expect(mockHotSwitch).toHaveBeenCalledWith('/worktree/path')
    })
  })

  describe('onSwitchBranch', () => {
    it('calls checkout API and refreshes on success', async () => {
      mockApiPost.mockResolvedValue({ success: true })
      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchBranch({ name: 'feature' })

      expect(mockApiPost).toHaveBeenCalledWith('/api/git/checkout', { branch: 'feature' })
      const { store } = await import('@/stores/app')
      expect(store.loadGitBranch).toHaveBeenCalled()
    })

    it('shows dirty checkout modal on dirty_worktree error', async () => {
      mockApiPost.mockResolvedValue({ success: false, error: 'dirty_worktree', untrackedCount: 3 })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchBranch({ name: 'feature' })

      expect(wrapper.vm.showDirtyModal).toBe(true)
      expect(wrapper.vm.dirtyCount).toBe(3)
    })

    it('shows alert on checkout error', async () => {
      mockApiPost.mockResolvedValue({ success: false, error: 'checkout_conflict' })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchBranch({ name: 'feature' })

      expect(mockDialogAlert).toHaveBeenCalled()
    })
  })

  describe('onSwitchTag', () => {
    it('calls checkout API and refreshes on success', async () => {
      mockApiPost.mockResolvedValue({ success: true })
      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchTag({ name: 'v1.0' })

      expect(mockApiPost).toHaveBeenCalledWith('/api/git/checkout', { branch: 'v1.0' })
    })

    it('shows dirty checkout modal on dirty_worktree error', async () => {
      mockApiPost.mockResolvedValue({ success: false, error: 'dirty_worktree', untrackedCount: 2 })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onSwitchTag({ name: 'v1.0' })

      expect(wrapper.vm.showDirtyModal).toBe(true)
    })
  })

  describe('onDeleteBranch', () => {
    it('deletes branch after confirmation', async () => {
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({ success: true })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onDeleteBranch({ name: 'old-branch' })

      expect(mockApiDelete).toHaveBeenCalledWith('/api/git/branch', expect.objectContaining({
        body: { name: 'old-branch' },
      }))
    })

    it('cancels deletion when not confirmed', async () => {
      mockDialogConfirm.mockResolvedValue(false)

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onDeleteBranch({ name: 'old-branch' })

      expect(mockApiDelete).not.toHaveBeenCalled()
    })

    it('shows alert on delete error', async () => {
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({ success: false, error: 'cannot_delete_current' })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onDeleteBranch({ name: 'main' })

      expect(mockDialogAlert).toHaveBeenCalled()
    })
  })

  describe('onDeleteTag', () => {
    it('deletes tag after confirmation', async () => {
      mockDialogConfirm.mockResolvedValue(true)
      mockApiDelete.mockResolvedValue({ success: true })

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onDeleteTag({ name: 'v0.1' })

      expect(mockApiDelete).toHaveBeenCalledWith('/api/git/tags', expect.objectContaining({
        body: { name: 'v0.1' },
      }))
    })

    it('cancels tag deletion when not confirmed', async () => {
      mockDialogConfirm.mockResolvedValue(false)

      const wrapper = mountContent()
      await flushPromises()

      await wrapper.vm.onDeleteTag({ name: 'v0.1' })

      expect(mockApiDelete).not.toHaveBeenCalled()
    })
  })

  describe('doDirtyCheckout', () => {
    it('calls checkout with stash flag', async () => {
      mockApiPost.mockResolvedValue({ success: true })

      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setShowDirtyModal(true)
      wrapper.vm._setDirtyCount(2)
      // Set pending ref
      wrapper.vm.pendingRef = 'feature'
      await nextTick()

      await wrapper.vm.doDirtyCheckout('stash')

      expect(mockApiPost).toHaveBeenCalledWith('/api/git/checkout', expect.objectContaining({
        stash: true,
        force: false,
      }))
    })

    it('calls checkout with force flag', async () => {
      mockApiPost.mockResolvedValue({ success: true })

      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm.pendingRef = 'feature'

      await wrapper.vm.doDirtyCheckout('force')

      expect(mockApiPost).toHaveBeenCalledWith('/api/git/checkout', expect.objectContaining({
        stash: false,
        force: true,
      }))
    })
  })

  describe('load errors', () => {
    it('sets error flags on load failure', async () => {
      mockApiGet.mockRejectedValue(new Error('fail'))

      const wrapper = mountContent()
      await flushPromises()

      expect(wrapper.vm.worktreesError).toBe(true)
      expect(wrapper.vm.branchesError).toBe(true)
      expect(wrapper.vm.tagsError).toBe(true)
    })
  })

  describe('dirty modal', () => {
    it('sets showDirtyModal to true', async () => {
      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setShowDirtyModal(true)
      await nextTick()

      expect(wrapper.vm._getShowDirtyModal()).toBe(true)
    })

    it('closes modal by setting showDirtyModal to false', async () => {
      const wrapper = mountContent()
      await flushPromises()
      wrapper.vm._setShowDirtyModal(true)
      await nextTick()

      wrapper.vm._setShowDirtyModal(false)
      await nextTick()

      expect(wrapper.vm._getShowDirtyModal()).toBe(false)
    })
  })
})
