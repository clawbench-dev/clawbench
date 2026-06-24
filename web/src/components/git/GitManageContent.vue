<template>
  <div class="git-manage-content">
    <!-- Tab bar -->
    <div class="manage-tabs">
      <button
        v-for="tab in tabs"
        :key="tab.key"
        class="manage-tab"
        :class="{ active: activeTab === tab.key }"
        @click="activeTab = tab.key"
      >
        <component :is="tab.icon" :size="14" />
        <span>{{ tab.label }}</span>
        <span v-if="tab.count > 0" class="tab-count">{{ tab.count }}</span>
      </button>
    </div>

    <!-- Tab content -->
    <div class="manage-tab-body">
      <!-- Worktree tab -->
      <div v-if="activeTab === 'worktrees'" class="tab-pane">
        <GitWorktreeList
          :worktrees="worktrees"
          :loading="worktreesLoading"
          :error="worktreesError"
          :initial-collapsed="false"
          hide-header
          @switch-worktree="onSwitchWorktree"
          @delete-worktree="onDeleteWorktree"
          @retry="loadWorktrees"
        />
      </div>

      <!-- Branches tab -->
      <div v-if="activeTab === 'branches'" class="tab-pane">
        <GitBranchList
          :branches="branches"
          :stash-count="stashCount"
          :loading="branchesLoading"
          :error="branchesError"
          :checkout-in-progress="checkoutInProgress"
          :initial-collapsed="false"
          hide-header
          @switch-branch="onSwitchBranch"
          @delete-branch="onDeleteBranch"
          @retry="loadBranches"
        />
      </div>

      <!-- Tags tab -->
      <div v-if="activeTab === 'tags'" class="tab-pane">
        <GitTagList
          :tags="tags"
          :loading="tagsLoading"
          :error="tagsError"
          @retry="loadTags"
          @switch-tag="onSwitchTag"
          @delete-tag="onDeleteTag"
        />
      </div>
    </div>

    <!-- Dirty worktree modal -->
    <Teleport to="body">
      <div v-if="showDirtyModal" class="modal-overlay" @click.self="showDirtyModal = false">
        <div class="modal-dialog">
          <div class="modal-title">{{ t('git.manage.switchBranch') }}</div>
          <p class="modal-msg">{{ t('git.manage.dirty', { count: dirtyCount }) }}</p>
          <div class="modal-actions">
            <button class="modal-btn modal-stash-btn" @click="doDirtyCheckout('stash')">{{ t('git.manage.stashSwitch') }}</button>
            <button class="modal-btn modal-force-btn" @click="doDirtyCheckout('force')">{{ t('git.manage.forceSwitch') }}</button>
            <button class="modal-btn modal-cancel-btn" @click="showDirtyModal = false">{{ t('common.cancel') }}</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, inject, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { GitBranch, FolderTree, Tag } from 'lucide-vue-next'
import { store } from '@/stores/app.ts'
import { apiGet, apiPost, apiDelete } from '@/utils/api'
import { useDialog } from '@/composables/useDialog.ts'
import { refreshCurrentFile } from '@/composables/useFileRefresh.ts'
import GitWorktreeList from './GitWorktreeList.vue'
import GitBranchList from './GitBranchList.vue'
import GitTagList from './GitTagList.vue'

const { t } = useI18n()
const dialog = useDialog()
const hotSwitchProject = inject('hotSwitchProject', null) as ((path: string, pendingSessionId?: string) => Promise<void>) | null

const worktrees = ref<any[]>([])
const branches = ref<any[]>([])
const tags = ref<any[]>([])
const stashCount = ref(0)

const worktreesLoading = ref(false)
const worktreesError = ref(false)
const branchesLoading = ref(false)
const branchesError = ref(false)
const tagsLoading = ref(false)
const tagsError = ref(false)

const checkoutInProgress = ref(false)

// Dirty checkout modal state
const showDirtyModal = ref(false)
const pendingRef = ref('')
const dirtyCount = ref(0)
const pendingReload = ref<(() => Promise<void>) | null>(null)

const TAB_STORAGE_KEY = 'git-manage-active-tab'
const activeTab = ref<'worktrees' | 'branches' | 'tags'>('worktrees')

// Restore persisted tab
onMounted(() => {
  const stored = localStorage.getItem(TAB_STORAGE_KEY)
  if (stored === 'branches' || stored === 'tags' || stored === 'worktrees') {
    activeTab.value = stored
  }
})

// Watch tab changes to persist
watch(activeTab, (val) => {
  localStorage.setItem(TAB_STORAGE_KEY, val)
})

const tabs = computed(() => [
  {
    key: 'worktrees' as const,
    label: t('git.manage.tabWorktrees'),
    icon: FolderTree,
    count: worktrees.value.length,
  },
  {
    key: 'branches' as const,
    label: t('git.manage.tabBranches'),
    icon: GitBranch,
    count: branches.value.length,
  },
  {
    key: 'tags' as const,
    label: t('git.manage.tabTags'),
    icon: Tag,
    count: tags.value.length,
  },
])

async function loadWorktrees() {
  worktreesLoading.value = true
  worktreesError.value = false
  try {
    const data = await apiGet<{ isGit: boolean; worktrees: any[] }>('/api/git/worktrees')
    worktrees.value = data.worktrees || []
  } catch {
    worktreesError.value = true
  } finally {
    worktreesLoading.value = false
  }
}

async function loadBranches() {
  branchesLoading.value = true
  branchesError.value = false
  try {
    const data = await apiGet<{ isGit: boolean; branches: any[]; stashCount?: number }>('/api/git/branches')
    branches.value = data.branches || []
    stashCount.value = data.stashCount || 0
  } catch {
    branchesError.value = true
  } finally {
    branchesLoading.value = false
  }
}

async function loadTags() {
  tagsLoading.value = true
  tagsError.value = false
  try {
    const data = await apiGet<{ isGit: boolean; tags: any[] }>('/api/git/tags')
    tags.value = data.tags || []
  } catch {
    tagsError.value = true
  } finally {
    tagsLoading.value = false
  }
}

onMounted(() => {
  Promise.all([loadWorktrees(), loadBranches(), loadTags()])
})

async function onSwitchWorktree(wt: any) {
  if (hotSwitchProject) {
    await hotSwitchProject(wt.path)
  } else {
    await store.setProject(wt.path)
  }
}

function showDirtyCheckoutModal(name: string, count: number, reload: () => Promise<void>) {
  pendingRef.value = name
  dirtyCount.value = count
  pendingReload.value = reload
  showDirtyModal.value = true
}

async function doDirtyCheckout(mode: 'stash' | 'force') {
  showDirtyModal.value = false
  checkoutInProgress.value = true
  try {
    await apiPost('/api/git/checkout', {
      branch: pendingRef.value,
      stash: mode === 'stash',
      force: mode === 'force',
    })
    await store.loadGitBranch()
    await store.loadFiles(store.state.currentDir)
    refreshCurrentFile()
    if (pendingReload.value) await pendingReload.value()
  } finally {
    checkoutInProgress.value = false
    pendingRef.value = ''
    pendingReload.value = null
  }
}

async function onSwitchBranch(branch: any) {
  checkoutInProgress.value = true
  try {
    const result = await apiPost<{ success: boolean; error?: string; untrackedCount?: number; errorDetail?: string }>('/api/git/checkout', { branch: branch.name })
    if (result.success) {
      await store.loadGitBranch()
      await store.loadFiles(store.state.currentDir)
      refreshCurrentFile()
      await Promise.all([loadBranches(), loadWorktrees()])
    } else if (result.error === 'dirty_worktree') {
      checkoutInProgress.value = false
      showDirtyCheckoutModal(branch.name, result.untrackedCount || 0, async () => { await Promise.all([loadBranches(), loadWorktrees()]) })
    } else if (result.error) {
      const errorMessages: Record<string, string> = {
        checkout_conflict: t('git.manage.checkoutConflict'),
        hook_rejected: t('git.manage.hookRejected'),
        branch_not_found: t('git.manage.branchNotFound'),
        checkout_in_progress: t('git.manage.checkoutInProgress'),
        checkout_failed: result.errorDetail || t('git.manage.checkoutFailed'),
      }
      await dialog.alert(errorMessages[result.error] || t('git.manage.checkoutFailed'))
      if (result.error === 'branch_not_found') {
        await loadBranches()
      }
    }
  } finally {
    checkoutInProgress.value = false
  }
}

async function onSwitchTag(tag: any) {
  checkoutInProgress.value = true
  try {
    const result = await apiPost<{ success: boolean; error?: string; untrackedCount?: number; errorDetail?: string }>('/api/git/checkout', { branch: tag.name })
    if (result.success) {
      await store.loadGitBranch()
      await store.loadFiles(store.state.currentDir)
      refreshCurrentFile()
      await Promise.all([loadBranches(), loadWorktrees(), loadTags()])
    } else if (result.error === 'dirty_worktree') {
      checkoutInProgress.value = false
      showDirtyCheckoutModal(tag.name, result.untrackedCount || 0, async () => { await Promise.all([loadBranches(), loadWorktrees(), loadTags()]) })
    } else if (result.error) {
      const errorMessages: Record<string, string> = {
        checkout_conflict: t('git.manage.checkoutConflict'),
        hook_rejected: t('git.manage.hookRejected'),
        branch_not_found: t('git.manage.branchNotFound'),
        checkout_in_progress: t('git.manage.checkoutInProgress'),
        checkout_failed: result.errorDetail || t('git.manage.checkoutFailed'),
      }
      await dialog.alert(errorMessages[result.error] || t('git.manage.checkoutFailed'))
      if (result.error === 'branch_not_found') {
        await loadTags()
      }
    }
  } finally {
    checkoutInProgress.value = false
  }
}

async function onDeleteBranch(branch: any) {
  const confirmed = await dialog.confirm(
    t('git.manage.deleteBranchConfirm', { name: branch.name }),
    { title: t('git.manage.deleteBranch'), dangerous: true },
  )
  if (!confirmed) return
  try {
    const result = await apiDelete<{ success: boolean; error?: string; errorDetail?: string }>('/api/git/branch', { body: { name: branch.name } })
    if (result.success) {
      await Promise.all([loadBranches(), loadWorktrees()])
    } else if (result.error) {
      const errorMessages: Record<string, string> = {
        cannot_delete_current: t('git.manage.cannotDeleteCurrent'),
        cannot_delete_default: t('git.manage.cannotDeleteDefault'),
        branch_not_found: t('git.manage.branchNotFound'),
      }
      await dialog.alert(errorMessages[result.error] || result.errorDetail || t('git.manage.deleteFailed'))
      await loadBranches()
    }
  } catch {
    await dialog.alert(t('git.manage.deleteFailed'))
  }
}

async function onDeleteWorktree(wt: any) {
  const confirmed = await dialog.confirm(
    t('git.manage.deleteWorktreeConfirm', { name: wt.branch || wt.path }),
    { title: t('git.manage.deleteWorktree'), dangerous: true },
  )
  if (!confirmed) return
  try {
    const result = await apiDelete<{ success: boolean; error?: string; errorDetail?: string }>('/api/git/worktrees', { body: { path: wt.path } })
    if (result.success) {
      await Promise.all([loadWorktrees(), loadBranches()])
    } else if (result.error === 'dirty_worktree') {
      const forceConfirmed = await dialog.confirm(
        t('git.manage.deleteWorktreeDirty'),
        {
          title: t('git.manage.deleteWorktree'),
          confirmText: t('git.manage.deleteWorktreeForce'),
          cancelText: t('common.cancel'),
          dangerous: true,
        },
      )
      if (!forceConfirmed) return
      const forceResult = await apiDelete<{ success: boolean; error?: string; errorDetail?: string }>('/api/git/worktrees', { body: { path: wt.path, force: true } })
      if (forceResult.success) {
        await Promise.all([loadWorktrees(), loadBranches()])
      } else if (forceResult.error) {
        const errorMessages: Record<string, string> = {
          cannot_delete_current: t('git.manage.cannotDeleteCurrentWorktree'),
        }
        await dialog.alert(errorMessages[forceResult.error] || forceResult.errorDetail || t('git.manage.deleteFailed'))
        await loadWorktrees()
      }
    } else if (result.error) {
      const errorMessages: Record<string, string> = {
        cannot_delete_current: t('git.manage.cannotDeleteCurrentWorktree'),
      }
      await dialog.alert(errorMessages[result.error] || result.errorDetail || t('git.manage.deleteFailed'))
      await loadWorktrees()
    }
  } catch {
    await dialog.alert(t('git.manage.deleteFailed'))
  }
}

async function onDeleteTag(tag: any) {
  const confirmed = await dialog.confirm(
    t('git.manage.deleteTagConfirm', { name: tag.name }),
    { title: t('git.manage.deleteTag'), dangerous: true },
  )
  if (!confirmed) return
  try {
    const result = await apiDelete<{ success: boolean; error?: string; errorDetail?: string }>('/api/git/tags', { body: { name: tag.name } })
    if (result.success) {
      await loadTags()
    } else if (result.error) {
      await dialog.alert(result.errorDetail || t('git.manage.deleteFailed'))
      await loadTags()
    }
  } catch {
    await dialog.alert(t('git.manage.deleteFailed'))
  }
}

defineExpose({
  activeTab,
  showDirtyModal,
  dirtyCount,
  checkoutInProgress,
  worktrees,
  branches,
  tags,
  doDirtyCheckout,
  _setActiveTab(val: 'branches' | 'worktrees' | 'tags') { activeTab.value = val },
  _setShowDirtyModal(val: boolean) { showDirtyModal.value = val },
  _setDirtyCount(val: number) { dirtyCount.value = val },
  _getActiveTab() { return activeTab.value },
  _getShowDirtyModal() { return showDirtyModal.value },
})
</script>

<style scoped>
.git-manage-content {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

/* ─── Tab bar ─────────────────────────────────────────────────────── */

.manage-tabs {
  display: flex;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
}

.manage-tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 10px 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--text-secondary, #666);
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  cursor: pointer;
  transition: color 0.15s, border-color 0.15s;
  position: relative;
}

@media (hover: hover) {
  .manage-tab:hover {
    color: var(--text-primary, #1a1a1a);
  }
}

.manage-tab.active {
  color: var(--accent-color, #4a90d9);
  border-bottom-color: var(--accent-color, #4a90d9);
  font-weight: 600;
}

.tab-count {
  font-size: 10px;
  font-weight: 700;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
  padding: 1px 5px;
  border-radius: 10px;
}

.manage-tab.active .tab-count {
  background: color-mix(in srgb, var(--accent-color) 18%, transparent);
  color: var(--accent-color, #4a90d9);
}

/* ─── Tab content ─────────────────────────────────────────────────── */

.manage-tab-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.tab-pane {
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

/* ─── Dirty worktree modal ────────────────────────────────────────── */

.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.4);
}

.modal-dialog {
  background: var(--bg-primary, #fff);
  border-radius: 12px;
  padding: 20px;
  width: min(320px, 85vw);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.2);
}

.modal-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary, #1a1a1a);
  margin-bottom: 8px;
}

.modal-msg {
  font-size: 13px;
  color: var(--text-secondary, #666);
  margin: 0 0 16px;
  line-height: 1.5;
  white-space: pre-line;
  word-break: break-word;
  overflow-wrap: break-word;
  max-height: 40vh;
  overflow-y: auto;
}

.modal-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.modal-btn {
  width: 100%;
  padding: 10px;
  border-radius: 8px;
  border: 1px solid;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  text-align: center;
  background: transparent;
  transition: opacity 0.15s;
}

.modal-btn:active {
  opacity: 0.7;
}

.modal-stash-btn {
  border-color: var(--accent-color, #4a90d9);
  color: var(--accent-color, #4a90d9);
}

.modal-force-btn {
  border-color: var(--danger-color, #dc3545);
  color: var(--danger-color, #dc3545);
}

.modal-cancel-btn {
  border-color: var(--border-color, #dee2e6);
  color: var(--text-secondary, #666);
}
</style>
