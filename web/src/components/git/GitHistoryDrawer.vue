<template>
  <BottomSheet ref="bottomSheetRef" :open="open" @close="handleClose">
    <template #header>
      <GitBranch :size="16" class="bs-header-icon" />
      <span class="bs-header-title">{{ mode === 'file' ? t('git.history.fileHistory') : t('git.history.projectHistory') }}</span>
      <div v-if="mode === 'file' && file?.path" class="bs-header-description">
        <HeaderMarquee :text="file.path">{{ file.path }}</HeaderMarquee>
      </div>
    </template>

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
      :mode="mode"
      :wt-file-count="workingTreeFileCount"
      @select="onCommitSelect"
      @search="onSearch"
      @load-more="loadMoreCommits"
      @refresh="onRefresh"
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
      </div>
      <div class="drilldown-body">
        <GitCommitMeta :commit="selectedCommit" :is-working-tree="isWorkingTree" :file-path="mode === 'file' ? file?.path : selectedFilePath" @open-file="onOpenFile" @reveal-file="onRevealFile" />
        <GitDiffView
          :loading="diffState.loading"
          :empty="diffState.empty"
          :html="diffState.html"
          :no-wrap="mode === 'project'"
          :file-path="mode === 'project' ? selectedFilePath : file?.path"
          :commit-sha="selectedSHA || ''"
        />
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { GitBranch, Plus, Minus } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import GitCommitList from './GitCommitList.vue'
import GitCommitMeta from './GitCommitMeta.vue'
import GitDiffView from './GitDiffView.vue'
import GitBreadcrumb from './GitBreadcrumb.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import { store } from '@/stores/app.ts'
import { consumePendingCommitNavigation } from '@/composables/useCommitNavigation.ts'
import { useFeatureBackHandler, PRIORITY_OVERLAY } from '@/composables/useEdgeSwipeBack'
import { useGitHistoryView } from '@/composables/useGitHistoryView'
import { revealInFileManager } from '@/composables/useFilePathAnnotation.ts'
const { t } = useI18n()

const props = defineProps({
  open: Boolean,
  mode: {
    type: String,
    default: 'project', // 'project' | 'file'
  },
  file: Object, // { path, name } — used when mode === 'file'
})

const emit = defineEmits(['close', 'open-file'])

const bottomSheetRef = ref(null)

function onOpenFile(path) {
  emit('open-file', path)
  bottomSheetRef.value?.close()
}

/**
 * Reveal the file in the file manager. The sheet lives inside the file view, so
 * `source: 'file'` makes the coordinator suspend this file visit as a directory
 * excursion — Back then restores the file the user was viewing instead of
 * walking up the directory tree.
 *
 * The host close is emitted synchronously, before the reveal. The reveal closes
 * the file overlay to make room for the manager, which unmounts this sheet — so
 * a deferred close (the sheet's own 250ms animation timer) is dropped before it
 * fires and `open` stays true, making the sheet pop back open over the restored
 * file when the user presses Back. Closing first makes `open` false while the
 * component is still mounted, whatever the reveal goes on to do.
 */
async function onRevealFile(path) {
  emit('close')
  await revealInFileManager(path, 'file')
}

// ─── Shared git-history logic ───────────────────────────────────────────────
// State, loading and drill-down navigation are shared with GitHistoryContent —
// see useGitHistoryView. This host contributes only the open trigger, the
// identity reset, the BottomSheet chrome and its back handler.

// Track previous identity to detect actual changes
const lastProjectRoot = ref(null)
const lastFilePath = ref(null)

const {
  loading, error, commits, hasMore, searchLoading, loadingMore, isGit, untracked,
  currentView, selectedSHA, filesLoading, filesRefreshing, mergeGroups,
  selectedFilePath, diffState,
  commitListRef,
  selectedCommit, isWorkingTree, mode, stagedFiles, unstagedFiles,
  hasStaged, hasUnstaged, totalFileCount, workingTreeFileCount,
  fileTypeLabel, fileSplit, badgeClass, resetListState,
  loadMoreCommits, onSearch, onRefresh, onFilesRefresh,
  reloadPreservingDrillDown,
  onCommitSelect, drillBack, drillToFile, navigateToCommit,
} = useGitHistoryView({
  t,
  mode: () => props.mode,
  filePath: () => props.file?.path,
})

function resetState() {
  resetListState()
  lastProjectRoot.value = null
  lastFilePath.value = null
}

function handleClose() {
  emit('close')
}

// Register back handler for drill-down navigation inside the drawer.
// Priority: PRIORITY_OVERLAY + 1 so it wins over BottomSheet's own close handler,
// allowing us to pop one view level before the sheet closes on the final back.
useFeatureBackHandler(
    'git-history-drawer',
    () => props.open && currentView.value !== 'commits',
    () => {
        if (currentView.value === 'diff' && props.mode === 'project') {
            drillBack('files')
        } else {
            drillBack('commits')
        }
    },
    PRIORITY_OVERLAY + 1,
)

watch(() => props.open, async (val) => {
  if (!val) {
    // Stop observing but keep state so reopening resumes where we left off
    commitListRef.value?.unobserveList()
    return
  }

  // Check if identity changed (different project or file)
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

  // Check for pending commit navigation (from chat hash links)
  const pendingSha = consumePendingCommitNavigation()
  if (pendingSha) {
    await navigateToCommit(pendingSha)
    setTimeout(() => commitListRef.value?.observeList(), 100)
    return
  }

  // Refresh on every open, not only when empty: the workspace/file may have
  // changed while the drawer was closed, and re-opening is the user's signal
  // that they want the current state. The existing list stays visible during
  // the background refresh (shouldShowFullLoading keeps the spinner off), and
  // reloadPreservingDrillDown restores the drill-down the reload clears — so
  // re-opening while drilled into the working-tree list no longer renders blank.
  await reloadPreservingDrillDown()

  // Start observing after content loads
  setTimeout(() => commitListRef.value?.observeList(), 100)
})
</script>

<style scoped>
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
