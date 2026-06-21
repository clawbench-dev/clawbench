import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'

// --- Mock apiDelete ---
const mockApiDelete = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: vi.fn().mockResolvedValue({ isGit: true, worktrees: [], branches: [], tags: [], stashCount: 0 }),
  apiPost: vi.fn().mockResolvedValue({ success: true }),
  apiDelete: (...args: unknown[]) => mockApiDelete(...args),
}))

// --- Mock useDialog ---
const dialogConfirmFn = vi.fn()
const dialogAlertFn = vi.fn()
vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({
    state: { value: { visible: false, type: 'confirm', title: '', message: '', value: '', placeholder: '', confirmText: '', cancelText: '', dangerous: false, resolve: null } },
    confirm: (...args: unknown[]) => dialogConfirmFn(...args),
    alert: (...args: unknown[]) => dialogAlertFn(...args),
    resolve: vi.fn(),
  }),
}))

// --- Mock store ---
vi.mock('@/stores/app.ts', () => ({
  store: {
    setProject: vi.fn().mockResolvedValue(undefined),
    loadGitBranch: vi.fn().mockResolvedValue(undefined),
  },
}))

// --- i18n ---
const i18n = createI18n({
  legacy: false,
  locale: 'zh',
  messages: {
    zh: {
      common: { cancel: '取消', ok: '确定', confirm: '确认' },
      git: {
        manage: {
          tabWorktrees: '工作树',
          tabBranches: '分支',
          tabTags: '标签',
          deleteWorktree: '删除工作树',
          deleteWorktreeConfirm: '确定删除工作树「{name}」？',
          deleteWorktreeDirty: '工作树包含未提交的修改或未跟踪的文件，\n强制删除将丢失这些更改。',
          deleteWorktreeForce: '强制删除',
          cannotDeleteCurrentWorktree: '不能删除当前工作树',
          deleteFailed: '删除失败',
          dirty: '{count} 个未提交修改',
          stashSwitch: '暂存并切换',
          forceSwitch: '强制切换（丢弃更改）',
          switchBranch: '切换分支',
        },
      },
    },
  },
})

// --- Stub child components ---
const StubComponent = {
  template: '<div class="stub"><slot /></div>',
  props: ['worktrees', 'branches', 'tags', 'stashCount', 'loading', 'error', 'checkoutInProgress', 'initialCollapsed', 'hideHeader'],
  emits: ['switch-worktree', 'delete-worktree', 'switch-branch', 'delete-branch', 'switch-tag', 'delete-tag', 'retry'],
}

// --- Import after mocks ---
import GitManageContent from '@/components/git/GitManageContent.vue'

function mountContent() {
  return mount(GitManageContent, {
    global: {
      plugins: [i18n],
      stubs: {
        GitWorktreeList: StubComponent,
        GitBranchList: StubComponent,
        GitTagList: StubComponent,
        FolderTree: { template: '<span />' },
        GitBranch: { template: '<span />' },
        Tag: { template: '<span />' },
        Teleport: { template: '<div><slot /></div>' },
      },
    },
  })
}

// Helper: flush all pending microtasks + Vue ticks
async function flush(ms = 0) {
  await new Promise(r => setTimeout(r, ms))
  await nextTick()
}

describe('GitManageContent - onDeleteWorktree dirty_worktree handling', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: first confirm = true (user confirms initial delete)
    dialogConfirmFn.mockResolvedValue(true)
  })

  it('shows force-delete confirmation when dirty_worktree error is returned', async () => {
    // First call returns dirty_worktree error
    mockApiDelete.mockResolvedValueOnce({ success: false, error: 'dirty_worktree' })
    // Second confirm (force) = true — set up as second resolved value
    dialogConfirmFn
      .mockResolvedValueOnce(true)   // initial delete confirm
      .mockResolvedValueOnce(true)   // force delete confirm
    // Force delete succeeds
    mockApiDelete.mockResolvedValueOnce({ success: true })

    const wrapper = mountContent()
    await flush()

    // Trigger delete-worktree event
    const wt = { path: '/repo/.worktrees/fix-lint', branch: 'fix-lint' }
    wrapper.findComponent('.stub').vm.$emit('delete-worktree', wt)
    await flush(10)

    // First apiDelete call (without force)
    expect(mockApiDelete).toHaveBeenCalledWith('/api/git/worktrees', { body: { path: wt.path } })

    // Confirm dialog called twice: initial + force confirmation
    expect(dialogConfirmFn).toHaveBeenCalledTimes(2)
    // Second confirm should include force-delete text
    expect(dialogConfirmFn).toHaveBeenNthCalledWith(2,
      '工作树包含未提交的修改或未跟踪的文件，\n强制删除将丢失这些更改。',
      expect.objectContaining({
        title: '删除工作树',
        confirmText: '强制删除',
        cancelText: '取消',
        dangerous: true,
      }),
    )

    // Second apiDelete call (with force)
    expect(mockApiDelete).toHaveBeenNthCalledWith(2, '/api/git/worktrees', { body: { path: wt.path, force: true } })
  })

  it('does not force-delete when user cancels dirty confirmation', async () => {
    // First call returns dirty_worktree error
    mockApiDelete.mockResolvedValueOnce({ success: false, error: 'dirty_worktree' })
    // User confirms initial delete, cancels force delete
    dialogConfirmFn
      .mockResolvedValueOnce(true)   // initial delete confirm
      .mockResolvedValueOnce(false)  // force delete confirm — cancelled

    const wrapper = mountContent()
    await flush()

    const wt = { path: '/repo/.worktrees/fix-lint', branch: 'fix-lint' }
    wrapper.findComponent('.stub').vm.$emit('delete-worktree', wt)
    await flush(10)

    // Only one apiDelete call (without force)
    expect(mockApiDelete).toHaveBeenCalledTimes(1)
    expect(mockApiDelete).toHaveBeenCalledWith('/api/git/worktrees', { body: { path: wt.path } })
  })

  it('deletes clean worktree without force confirmation', async () => {
    mockApiDelete.mockResolvedValueOnce({ success: true })
    dialogConfirmFn.mockResolvedValueOnce(true)

    const wrapper = mountContent()
    await flush()

    const wt = { path: '/repo/.worktrees/clean', branch: 'clean' }
    wrapper.findComponent('.stub').vm.$emit('delete-worktree', wt)
    await flush(10)

    // Only one confirm (the initial one) and one apiDelete
    expect(dialogConfirmFn).toHaveBeenCalledTimes(1)
    expect(mockApiDelete).toHaveBeenCalledTimes(1)
  })

  it('does not proceed when user cancels initial delete confirmation', async () => {
    dialogConfirmFn.mockResolvedValue(false)

    const wrapper = mountContent()
    await flush()

    const wt = { path: '/repo/.worktrees/clean', branch: 'clean' }
    wrapper.findComponent('.stub').vm.$emit('delete-worktree', wt)
    await flush(10)

    expect(mockApiDelete).not.toHaveBeenCalled()
  })
})
