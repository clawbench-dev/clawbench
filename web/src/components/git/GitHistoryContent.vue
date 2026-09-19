<template>
  <div class="git-history-content">
    <!-- Loading (initial) -->
    <div v-if="loading" class="git-history-loading">
      <LoadingIndicator size="md" />
    </div>

    <!-- Error -->
    <div v-else-if="error" class="git-history-error">
      {{ error }}
    </div>

    <!-- View: commit list (shared by both modes) -->
    <GitCommitList
      v-else-if="currentView === 'commits'"
      ref="commitListRef"
      :commits="commits"
      :is-git="isGit"
      :has-more="hasMore"
      :loading-more="loadingMore"
      :search-loading="searchLoading"
      :loading="false"
      :error="''"
      :untracked="untracked"
      :count-label="mode === 'file' ? t('git.history.records') : t('git.history.commitRecords')"
      :selected-s-h-a="selectedSHA"
      :refresh-hint="refreshHint"
      :mode="mode"
      :wt-file-count="workingTreeFileCount"
      @select="onCommitSelect"
      @search="onSearch"
      @load-more="loadMoreCommits"
      @refresh="onRefresh"
      @manage="navigateToManage"
    />

    <!-- View: file list for selected commit (project mode only) -->
    <div v-else-if="currentView === 'files'" class="drilldown-page">
      <div class="drilldown-header">
        <GitBreadcrumb
          mode="project"
          :current-view="currentView"
          :selected-commit="selectedCommit"
          @navigate="drillBack"
        />
        <span class="drilldown-count count-badge">{{ t('git.history.fileCount', { count: totalFileCount }) }}</span>
        <RefreshButton
          class="drilldown-refresh-btn"
          :loading="filesLoading || filesRefreshing"
          :disabled="filesLoading || filesRefreshing"
          :title="t('git.history.refresh')"
          @click.stop="onFilesRefresh"
        />
      </div>
      <GitCommitMeta :commit="selectedCommit" :is-working-tree="isWorkingTree" @open-file="onOpenFile" @reveal-file="onRevealFile" />
      <div class="drilldown-body">
        <div v-if="filesLoading" class="git-history-loading">
          <LoadingIndicator size="md" />
        </div>
        <div v-else-if="totalFileCount === 0" class="git-history-empty">{{ t('git.history.noFileChanges') }}</div>
        <!-- Merge commit: grouped by parent branch -->
        <div v-else-if="mergeGroups.length > 0" class="drilldown-list">
          <div v-for="group in mergeGroups" :key="group.label" class="merge-group">
            <div class="file-group-label">{{ t('git.history.mergedFrom', { label: group.label }) }} ({{ group.files.length }})</div>
            <div
              v-for="f in group.files"
              :key="f.path + '-' + f.type"
              class="drilldown-item"
              @click="drillToFile(f)"
            >
              <span class="git-file-icon">
                <Plus v-if="f.type === 'A'" :size="14" :stroke-width="2.5" />
                <Minus v-else-if="f.type === 'D'" :size="14" :stroke-width="2.5" />
                <FileIcon v-else :path="f.path" :size="14" />
              </span>
              <span class="git-file-type-badge" :class="badgeClass(f)">{{ fileTypeLabel(f.type, false) }}</span>
              <span class="git-file-info" :title="f.path">
                <span class="git-file-name">{{ fileSplit(f).name }}</span>
                <span v-if="fileSplit(f).dir" class="git-file-dir">{{ fileSplit(f).dir }}</span>
              </span>
            </div>
          </div>
        </div>
        <!-- Regular commit or working tree -->
        <div v-else class="drilldown-list">
          <template v-if="hasStaged">
            <div class="file-group-label">{{ t('git.history.staged') }}</div>
            <div
              v-for="f in stagedFiles"
              :key="f.path + '-' + f.type + '-s'"
              class="drilldown-item"
              @click="drillToFile(f)"
            >
              <span class="git-file-icon">
                <Plus v-if="f.type === 'A'" :size="14" :stroke-width="2.5" />
                <Minus v-else-if="f.type === 'D'" :size="14" :stroke-width="2.5" />
                <FileIcon v-else :path="f.path" :size="14" />
              </span>
              <span class="git-file-type-badge" :class="badgeClass(f)">{{ fileTypeLabel(f.type, f.staged) }}</span>
              <span class="git-file-info" :title="f.path">
                <span class="git-file-name">{{ fileSplit(f).name }}</span>
                <span v-if="fileSplit(f).dir" class="git-file-dir">{{ fileSplit(f).dir }}</span>
              </span>
            </div>
          </template>
          <template v-if="hasUnstaged">
            <div v-if="hasStaged" class="file-group-label">{{ t('git.history.unstaged') }}</div>
            <div
              v-for="f in unstagedFiles"
              :key="f.path + '-' + f.type"
              class="drilldown-item"
              @click="drillToFile(f)"
            >
              <span class="git-file-icon">
                <Plus v-if="f.type === 'A'" :size="14" :stroke-width="2.5" />
                <Minus v-else-if="f.type === 'D'" :size="14" :stroke-width="2.5" />
                <FileIcon v-else :path="f.path" :size="14" />
              </span>
              <span class="git-file-type-badge" :class="badgeClass(f)">{{ fileTypeLabel(f.type, f.staged) }}</span>
              <span class="git-file-info" :title="f.path">
                <span class="git-file-name">{{ fileSplit(f).name }}</span>
                <span v-if="fileSplit(f).dir" class="git-file-dir">{{ fileSplit(f).dir }}</span>
              </span>
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- View: diff (shared by both modes) -->
    <div v-else-if="currentView === 'diff'" class="drilldown-page">
      <div class="drilldown-header">
        <GitBreadcrumb
          :mode="mode"
          :current-view="currentView"
          :selected-commit="selectedCommit"
          :selected-file-path="selectedFilePath"
          @navigate="drillBack"
          @open-file="onOpenFile"
        />
        <div v-if="mode === 'project' && diffNavTotal > 0" class="diff-nav">
          <button
            class="diff-nav-btn"
            :disabled="diffNavIndex <= 0"
            :title="t('git.history.prevFile')"
            @click="diffNav.prev"
          >
            <ChevronUp :size="14" />
          </button>
          <span class="diff-nav-count">{{ diffNavIndex + 1 }}/{{ diffNavTotal }}</span>
          <button
            class="diff-nav-btn"
            :disabled="diffNavIndex < 0 || diffNavIndex >= diffNavTotal - 1"
            :title="t('git.history.nextFile')"
            @click="diffNav.next"
          >
            <ChevronDown :size="14" />
          </button>
        </div>
      </div>
      <div class="drilldown-body">
        <GitCommitMeta :commit="selectedCommit" :is-working-tree="isWorkingTree" :file-path="mode === 'file' ? file?.path : selectedFilePath" @open-file="onOpenFile" @reveal-file="onRevealFile" />
        <GitDiffView
          :loading="diffState.loading"
          :empty="diffState.empty"
          :html="diffState.html"
          :no-wrap="mode === 'project'"
          :file-path="mode === 'project' ? selectedFilePath : file?.path"
        />
      </div>
    </div>

    <!-- View: worktree & branch management -->
    <div v-else-if="currentView === 'manage'" class="drilldown-page">
      <div class="drilldown-header">
        <GitBreadcrumb mode="project" current-view="manage" @navigate="drillBack" />
      </div>
      <GitManageContent />
    </div>
  </div>
</template>

<script setup>
import { Plus, Minus, ChevronUp, ChevronDown } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import { ref, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GitCommitList from './GitCommitList.vue'
import GitCommitMeta from './GitCommitMeta.vue'
import GitDiffView from './GitDiffView.vue'
import GitBreadcrumb from './GitBreadcrumb.vue'
import GitManageContent from './GitManageContent.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { store } from '@/stores/app.ts'
import { consumePendingCommitNavigation, pendingSha as pendingCommitSha, consumePendingManageNavigation, pendingManageView } from '@/composables/useCommitNavigation.ts'
import { useDiffNavigation } from '@/composables/useDiffNavigation.ts'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { shouldShowFullLoading } from '@/utils/gitFileHistory'
import { useGitHistoryView } from '@/composables/useGitHistoryView'
import { revealInFileManager } from '@/composables/useFilePathAnnotation.ts'
const { t } = useI18n()

const props = defineProps({
  mode: {
    type: String,
    default: 'project', // 'project' | 'file'
  },
  file: Object, // { path, name } — used when mode === 'file'
  active: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['open-file'])

function onOpenFile(path) {
  // App.vue's handleSelectFile switches to the file-view tab.
  emit('open-file', path)
}

/**
 * Reveal the file in the file manager. Routed through the shared directory-jump
 * event (not the navToFileInManager primitive) so the jump records a return
 * origin: this host is a jump-capable surface, and Back must come back here
 * rather than walking up the directory tree.
 */
function onRevealFile(path) {
  void revealInFileManager(path, 'history')
}

// ─── Shared git-history logic ───────────────────────────────────────────────
// State, loading and drill-down navigation are shared with GitHistoryDrawer —
// see useGitHistoryView. This host contributes only the tab-activation
// trigger, the manage view, diff prev/next, search, and its own chrome.

// Track git state so a tab re-entry can tell whether anything changed.
const lastGitState = ref({ branch: '', head: '', dirty: false, changeCount: 0 })
// Identity tracking for project hot-switches.
const lastProjectRoot = ref(null)
const lastFilePath = ref(null)

const {
  loading, error, commits, hasMore, searchLoading, loadingMore, isGit, untracked,
  currentView, selectedSHA, filesLoading, filesRefreshing, files, mergeGroups,
  selectedFilePath, diffState, hasLoadedMore, refreshHint,
  commitListRef,
  selectedCommit, isWorkingTree, mode, stagedFiles, unstagedFiles,
  hasStaged, hasUnstaged, totalFileCount, workingTreeFileCount,
  fileTypeLabel, fileSplit, badgeClass, resetListState,
  loadProjectHistory, loadFileHistory, loadMoreCommits, onSearch, onRefresh,
  loadDiff, onFilesRefresh,
  reloadPreservingDrillDown,
  onCommitSelect, drillBack, drillToFile, navigateToCommit,
} = useGitHistoryView({
  t,
  mode: () => props.mode,
  filePath: () => props.file?.path,
  useAbortSignal: true,
  onProjectHistoryLoaded: () => {
    // Record git state after a successful load so the active watcher can tell
    // whether a later re-entry needs a reload.
    lastGitState.value = {
      branch: store.state.gitBranch,
      head: store.state.gitHead,
      dirty: store.state.gitDirty,
      changeCount: store.state.gitWorkingTreeChangeCount,
    }
    refreshHint.value = false
  },
})

function resetState() {
  resetListState()
  lastProjectRoot.value = null
  lastFilePath.value = null
}

// Diff prev/next navigation: flat ordered list + prev/next helpers so the
// user can hop between file diffs without returning to the file list.
const diffNav = useDiffNavigation({
    files,
    mergeGroups,
    selectedFilePath,
    loadDiff,
})
const diffNavTotal = diffNav.total
const diffNavIndex = diffNav.index

// ─── Manage view (this host only) ───────────────────────────────────────────

function navigateToManage() {
  currentView.value = 'manage'
}

// Register back handler for git drill-down navigation
// canGoBack: active tab AND not on the commit list (root view)
// goBack: pop one level (diff→files in project mode, otherwise →commits)
useFeatureBackHandler(
    'git-history',
    () => props.active && currentView.value !== 'commits',
    () => {
        if (currentView.value === 'diff' && props.mode === 'project') {
            drillBack('files')
        } else {
            drillBack('commits')
        }
    },
    PRIORITY_PAGE,
)

// GitCommitList manages its own IntersectionObserver lifecycle (created in
// onMounted, disconnected in onUnmounted), so the parent does not need to
// call observeList() when switching back to the commits view.

// Watch for commit navigation requests from chat (handles the case where
// the history tab is already active and a commit hash link is clicked)
watch(pendingCommitSha, async (sha) => {
  if (!sha || !props.active) return
  const consumed = consumePendingCommitNavigation()
  if (consumed) {
    await navigateToCommit(consumed)
  }
})

// Watch for manage-view navigation requests from branch badge click
// (handles the case where the history tab is already active)
watch(pendingManageView, async (pending) => {
  if (!pending || !props.active) return
  const consumed = consumePendingManageNavigation()
  if (consumed) {
    currentView.value = 'manage'
  }
})

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// When tab becomes active, check if git state changed or pending navigation
watch(() => props.active, async (nowActive) => {
  if (!nowActive || props.mode !== 'project') return

  // Check for pending manage-view navigation (from branch badge click)
  if (consumePendingManageNavigation()) {
    currentView.value = 'manage'
    return
  }

  // Check for pending commit navigation (from chat hash links)
  const pendingSha = consumePendingCommitNavigation()
  if (pendingSha) {
    await navigateToCommit(pendingSha)
    return
  }

  await store.loadGitBranch()
  const cur = { branch: store.state.gitBranch, head: store.state.gitHead, dirty: store.state.gitDirty, changeCount: store.state.gitWorkingTreeChangeCount }
  const changed = lastGitState.value.branch &&
    (cur.branch !== lastGitState.value.branch ||
     cur.head !== lastGitState.value.head ||
     cur.dirty !== lastGitState.value.dirty ||
     cur.changeCount !== lastGitState.value.changeCount)
  if (changed) {
    if (hasLoadedMore.value) {
      // User has extra data loaded — don't auto-refresh, just hint
      refreshHint.value = true
    } else {
      // Only first page — safe to auto-refresh. reloadPreservingDrillDown
      // restores the drill-down that the reload clears, so re-entering the tab
      // while sitting on the working-tree file list no longer renders blank.
      await reloadPreservingDrillDown()
    }
  }
  lastGitState.value = { ...cur }
})

onMounted(async () => {
  // Refresh git working-tree state (dock badge) on mount. TabPanel conditionally
  // mounts this component the first time history is opened — at that point
  // props.active is already true, so the props.active watch never fires and the
  // dock badge would stay stale (e.g. a session finished while the user was
  // elsewhere, then they open history for the first time). Refreshing on mount
  // guarantees opening history is a reliable badge-refresh trigger.
  // Only refresh when actually active: during a project hot-switch the whole
  // component tree is rebuilt while the history tab is NOT the active tab, so
  // the mount-time refresh would be a redundant duplicate of App.vue's own
  // state sync (the props.active watch picks it up when the user later enters
  // the tab).
  if (props.active) {
    store.loadGitBranch().catch(() => {})
  }

  const currentProject = store.state.projectRoot
  const currentFile = props.file?.path
  const identityChanged =
    (lastProjectRoot.value !== currentProject) ||
    (props.mode === 'file' && lastFilePath.value !== currentFile)

  if (identityChanged) {
    resetState()
    lastProjectRoot.value = currentProject
    lastFilePath.value = currentFile
  }

  // If navigating from branch badge click, go directly to manage view
  if (consumePendingManageNavigation()) {
    currentView.value = 'manage'
    if (shouldShowFullLoading(commits.value, error.value)) {
      await loadProjectHistory()
    }
    return
  }

  // If navigating from a commit hash link, go directly to that commit
  const pendingSha = consumePendingCommitNavigation()
  if (pendingSha) {
    await navigateToCommit(pendingSha)
    return
  }

  if (shouldShowFullLoading(commits.value, error.value)) {
    if (props.mode === 'file' && props.file?.path) {
      await loadFileHistory(props.file.path)
    } else {
      await loadProjectHistory()
    }
  }
})
</script>

<style scoped>
.git-history-content {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.git-history-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.git-history-error {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

/* ─── Drill-down shared ────────────────────────────────────────────────── */

.drilldown-page {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.drilldown-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 14px;
  height: var(--header-height);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
  gap: var(--space-4);
}

.drilldown-count {
  font-weight: var(--font-weight-bold);
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
}

/* Matches GitCommitList's refresh button so both headers look identical.
   Kept local because that component's styles are scoped to itself. */
.drilldown-refresh-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  background: var(--bg-tertiary, #e9ecef);
  border-radius: 50%;
  cursor: pointer;
  color: var(--text-muted, #999);
  flex-shrink: 0;
  padding: 0;
  transition: background var(--duration-base), color var(--duration-base), transform 0.3s;
}

@media (hover: hover) {
  .drilldown-refresh-btn:hover:not(:disabled) {
    background: var(--accent-color, #4a90d9);
    color: #fff;
  }
}

.drilldown-refresh-btn:active:not(:disabled) {
  transform: scale(0.92);
}

.drilldown-refresh-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

.diff-nav {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  flex-shrink: 0;
  margin-left: var(--space-3);
}

.diff-nav-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-secondary, #555);
  cursor: pointer;
  padding: 0;
  line-height: 1;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .diff-nav-btn:hover:not(:disabled) {
    background: var(--bg-tertiary, #e9ecef);
    color: var(--accent-color, #4a90d9);
  }
}

.diff-nav-btn:disabled {
  opacity: var(--opacity-disabled);
  cursor: default;
}

.diff-nav-count {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
  padding:0 var(--space-2);
  white-space: nowrap;
}

.drilldown-body {
  flex: 1;
  overflow-y: auto;
}

.drilldown-list {
  padding: var(--space-3) 0;
}

.drilldown-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  padding: 11px 14px;
  cursor: pointer;
  transition: background var(--duration-base);
  border-bottom: 1px solid var(--border-color, #dee2e6);
}

@media (hover: hover) {
  .drilldown-item:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.drilldown-item:active {
  background: var(--bg-tertiary, #e9ecef);
}

.git-history-empty {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

/* ─── File list (project mode) ────────────────────────────────────────── */

.git-file-icon {
  flex-shrink: 0;
  color: var(--text-muted, #999);
  display: flex;
  align-items: center;
}

.git-file-type-badge {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
  padding: var(--space-1) 5px;
  border-radius: var(--radius-xs);
  flex-shrink: 0;
  letter-spacing: 0.02em;
}

.badge-A { background: color-mix(in srgb, var(--color-green, #16a34a) 15%, transparent); color: var(--color-green, #16a34a); }
.badge-M { background: color-mix(in srgb, var(--color-yellow, #a16207) 15%, transparent); color: var(--color-yellow, #a16207); }
.badge-D { background: color-mix(in srgb, var(--color-red, #dc2626) 15%, transparent); color: var(--color-red, #dc2626); }
.badge-R { background: color-mix(in srgb, var(--color-purple, #7c3aed) 15%, transparent); color: var(--color-purple, #7c3aed); }
.badge-U { background: var(--bg-tertiary, #f0f0f0); color: var(--text-muted, #999); }
.badge-staged { border: 1px solid var(--accent-color, #4a90d9); }

.git-file-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.git-file-name {
  color: var(--text-primary, #212529);
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.git-file-dir {
  color: var(--text-muted, #999);
  font-size: var(--font-size-xs);
  opacity: var(--opacity-hover);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-group-label {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted, #999);
  padding: var(--space-4) 14px var(--space-2);
  letter-spacing: 0.03em;
}

.merge-group + .merge-group {
  border-top: 1px solid var(--border-color, #dee2e6);
  margin-top: var(--space-2);
  padding-top: var(--space-2);
}
</style>
