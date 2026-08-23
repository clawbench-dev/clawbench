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
        <span class="drilldown-count">{{ t('git.history.fileCount', { count: totalFileCount }) }}</span>
      </div>
      <GitCommitMeta :commit="selectedCommit" :is-working-tree="isWorkingTree" />
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
              <span class="git-file-path" :title="f.path">{{ f.path }}</span>
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
              <span class="git-file-path" :title="f.path">{{ f.path }}</span>
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
              <span class="git-file-path" :title="f.path">{{ f.path }}</span>
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
        <GitCommitMeta :commit="selectedCommit" :is-working-tree="isWorkingTree" />
        <GitDiffView
          :loading="diffState.loading"
          :empty="diffState.empty"
          :html="diffState.html"
          :no-wrap="mode === 'project'"
          :file-path="mode === 'project' ? selectedFilePath : file?.path"
        />
      </div>
    </div>
  </BottomSheet>
</template>

<script setup>
import { GitBranch, Plus, Minus } from 'lucide-vue-next'
import FileIcon from '@/components/common/FileIcon.vue'
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BottomSheet from '@/components/common/BottomSheet.vue'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import GitCommitList from './GitCommitList.vue'
import GitCommitMeta from './GitCommitMeta.vue'
import GitDiffView from './GitDiffView.vue'
import GitBreadcrumb from './GitBreadcrumb.vue'
import { renderDiff } from '@/utils/diff.ts'
import { buildFileHistoryCommits, shouldShowFullLoading } from '@/utils/gitFileHistory.ts'
import { store } from '@/stores/app.ts'
import { useCommitNavigation, consumePendingCommitNavigation } from '@/composables/useCommitNavigation.ts'
import { useFeatureBackHandler, PRIORITY_OVERLAY } from '@/composables/useEdgeSwipeBack'
import { gitFetch, GitTimeoutError, createSeqGuard } from '@/utils/gitApi'
import { appLog } from '@/utils/appLog'
const { t } = useI18n()

// Sequence guard to suppress stale concurrent loads — a refresh, re-open, or
// load-more can overlap an in-flight request; only the latest call may write
// data and reset the loading flag.
const historySeq = createSeqGuard()

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

// ─── Unified state ─────────────────────────────────────────────────────────

const loading = ref(false)
// True while a full reload is in flight WITHOUT the full-screen spinner (i.e.
// a background refresh that keeps the existing list). loadMore must not run
// concurrently with it — it would paginate the old commits with stale counts.
const fullReloading = ref(false)
const error = ref('')
const commits = ref([])
const hasMore = ref(false)
const searchLoading = ref(false)
const loadingMore = ref(false)
const isGit = ref(false)
const untracked = ref(false)

const currentView = ref('commits') // 'commits' | 'files' | 'diff'
const selectedSHA = ref(null)

// Files view (project mode only)
const filesLoading = ref(false)
const files = ref([])
const mergeGroups = ref([])
const selectedFilePath = ref(null)

// Unified diff state
const diffState = ref({ loading: false, empty: false, html: '' })

// Working tree
const wtFiles = ref([])

const commitListRef = ref(null)

const selectedCommit = computed(() => {
  return commits.value.find(c => c.sha === selectedSHA.value) || null
})
const isWorkingTree = computed(() => selectedSHA.value === 'HEAD')

const mode = computed(() => props.mode)

const sortedFiles = computed(() => {
  const order = { M: 0, A: 1, D: 2, R: 3, '?': 4 }
  return [...files.value].sort((a, b) => (order[a.type] ?? 5) - (order[b.type] ?? 5))
})
const stagedFiles = computed(() => sortedFiles.value.filter(f => f.staged))
const unstagedFiles = computed(() => sortedFiles.value.filter(f => !f.staged))
const hasStaged = computed(() => stagedFiles.value.length > 0)
const hasUnstaged = computed(() => unstagedFiles.value.length > 0)

const totalFileCount = computed(() => {
  if (mergeGroups.value.length > 0) {
    return mergeGroups.value.reduce((sum, g) => sum + g.files.length, 0)
  }
  return files.value.length
})

// ─── Helpers ────────────────────────────────────────────────────────────────

function fileTypeLabel(type, staged) {
  const keys = { A: 'git.fileType.added', M: 'git.fileType.modified', D: 'git.fileType.deleted', R: 'git.fileType.renamed', '?': 'git.fileType.untracked' }
  const base = t(keys[type] || type)
  return staged ? t('git.fileType.stagedPrefix') + base : base
}

function badgeClass(f) {
  const typeMap = { A: 'A', M: 'M', D: 'D', R: 'R', '?': 'U' }
  const cls = typeMap[f.type] || 'M'
  return 'badge-' + cls + (f.staged ? ' badge-staged' : '')
}

function resetState() {
  commits.value = []
  files.value = []
  mergeGroups.value = []
  hasMore.value = false
  selectedSHA.value = null
  selectedFilePath.value = null
  diffState.value = { loading: false, empty: false, html: '' }
  currentView.value = 'commits'
  error.value = ''
  commitSearch.value = ''
  isGit.value = false
  untracked.value = false
  wtFiles.value = []
  lastProjectRoot.value = null
  lastFilePath.value = null
}

// Expose commitSearch for the search watcher
const commitSearch = ref('')

// ─── Data loading ───────────────────────────────────────────────────────────

async function loadProjectHistory() {
  const seq = historySeq.token()
  // Keep the existing list visible during background refreshes — only show the
  // full-screen spinner when there is nothing to render yet (first load/empty),
  // so the refresh button stays mounted and its spin feedback is visible.
  const isFirstLoad = shouldShowFullLoading(commits.value, error.value)
  loading.value = isFirstLoad
  fullReloading.value = !isFirstLoad
  error.value = ''
  if (isFirstLoad) commits.value = []
  hasMore.value = false
  selectedSHA.value = null
  files.value = []
  mergeGroups.value = []
  selectedFilePath.value = null
  wtFiles.value = []
  isGit.value = true

  try {
    const resp = await gitFetch('/api/git/project-history')
    if (!historySeq.isCurrent(seq)) return // superseded by a newer load
    if (!resp.ok) {
      const data = await resp.json()
      commits.value = []
      error.value = data.error || t('git.history.loadError')
      return
    }
    const data = await resp.json()

    if (!data.isGit) {
      commits.value = []
      isGit.value = false
      return
    }

    isGit.value = true

    // Check working tree changes
    const wtResp = await gitFetch('/api/git/working-tree')
    let loadedWtFiles = []
    if (wtResp.ok) {
      const wt = await wtResp.json()
      loadedWtFiles = wt.files || []
      wtFiles.value = loadedWtFiles
    }

    if (!historySeq.isCurrent(seq)) return // superseded while working-tree was in flight

    const histCommits = data.commits || []

    // Prepend working tree entry if there are uncommitted changes
    if (loadedWtFiles.length > 0) {
      commits.value = [{ sha: 'HEAD', msg: t('git.history.workingTreeChanges'), date: '', author: '', isWT: true }, ...histCommits]
    } else {
      commits.value = histCommits
    }
    hasMore.value = data.hasMore
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') return
    if (!historySeq.isCurrent(seq)) return
    commits.value = []
    if (err instanceof GitTimeoutError) {
      appLog.w('GitHistory', err.message)
      error.value = t('git.history.loadTimeout')
      return
    }
    error.value = t('git.history.loadError')
  } finally {
    if (historySeq.isCurrent(seq)) {
      loading.value = false
      fullReloading.value = false
    }
  }
}

async function loadFileHistory(filePath) {
  const seq = historySeq.token()
  // Keep the existing list visible during background refreshes (see
  // loadProjectHistory) so the refresh button's spin stays visible.
  const isFirstLoad = shouldShowFullLoading(commits.value, error.value)
  loading.value = isFirstLoad
  fullReloading.value = !isFirstLoad
  error.value = ''
  if (isFirstLoad) commits.value = []
  selectedSHA.value = null
  isGit.value = true
  untracked.value = false

  try {
    const resp = await gitFetch(`/api/git/history?path=${encodeURIComponent(filePath)}`)
    if (!historySeq.isCurrent(seq)) return
    if (!resp.ok) {
      const data = await resp.json()
      commits.value = []
      error.value = data.error || t('git.history.loadError')
      return
    }
    const hist = await resp.json()
    if (!historySeq.isCurrent(seq)) return
    if (!hist.isGit) {
      commits.value = []
      isGit.value = false
      return
    }
    isGit.value = true
    untracked.value = !!hist.untracked

    // Prepend a working-tree entry only when this specific file has
    // uncommitted changes. Otherwise file history shows commits only.
    let hasUncommitted = false
    const wtResp = await gitFetch(`/api/git/working-tree?path=${encodeURIComponent(filePath)}`)
    if (wtResp.ok) {
      const wt = await wtResp.json()
      hasUncommitted = !!wt.hasUncommitted
    }

    if (!historySeq.isCurrent(seq)) return // superseded while working-tree was in flight

    const histCommits = hist.commits || []
    commits.value = buildFileHistoryCommits(histCommits, hasUncommitted, t('git.history.workingTreeChanges'))
  } catch (err) {
    if (err instanceof Error && err.name === 'AbortError') return
    if (!historySeq.isCurrent(seq)) return
    commits.value = []
    if (err instanceof GitTimeoutError) {
      appLog.w('GitHistory', err.message)
      error.value = t('git.history.loadTimeout')
      return
    }
    error.value = t('git.history.loadError')
  } finally {
    if (historySeq.isCurrent(seq)) {
      loading.value = false
      fullReloading.value = false
    }
  }
}

async function loadMoreCommits() {
  // Skip while a full reload is in flight: loading replaces the commit list
  // and loadMore would paginate the OLD commits with stale skip counts.
  if (loading.value || fullReloading.value || loadingMore.value || !hasMore.value || !isGit.value) return
  loadingMore.value = true
  try {
    // Count only git commits (exclude WT node) for the skip parameter,
    // since WT is a frontend-only entry not present in git log output.
    const gitCount = commits.value.filter(c => !c.isWT).length
    const resp = await gitFetch(`/api/git/project-history?skip=${gitCount}`)
    if (!resp.ok) return
    const data = await resp.json()
    commits.value.push(...(data.commits || []))
    hasMore.value = data.hasMore
  } catch {
    // ignore
  } finally {
    loadingMore.value = false
  }
}

// When searching, auto-load all commits so filtering covers the full history
async function onSearch(q) {
  if (!q.trim() || !isGit.value || props.mode === 'file') return
  const seq = historySeq.token()
  searchLoading.value = true
  try {
    while (hasMore.value) {
      if (!historySeq.isCurrent(seq)) return // superseded by a refresh/load
      const gitCount = commits.value.filter(c => !c.isWT).length
      const resp = await gitFetch(`/api/git/project-history?skip=${gitCount}`)
      if (!resp.ok) break
      const data = await resp.json()
      commits.value.push(...(data.commits || []))
      hasMore.value = data.hasMore
    }
  } catch (err) {
    if (err instanceof GitTimeoutError) {
      appLog.w('GitHistory', err.message)
    }
    // Search is best-effort: ignore failures, the already-loaded commits remain visible.
  } finally {
    searchLoading.value = false
  }
}

async function onRefresh() {
  commitSearch.value = ''
  if (commitListRef.value) commitListRef.value.commitSearch = ''
  if (props.mode === 'file' && props.file?.path) {
    await loadFileHistory(props.file.path)
  } else {
    await loadProjectHistory()
  }
  setTimeout(() => commitListRef.value?.observeList(), 100)
}

// ─── Shared commit navigation composable ─────────────────────────────────

const { navigateToCommit, handleDrillBackToCommits } = useCommitNavigation({
    commits,
    selectedSHA,
    currentView,
    loadCommitFiles,
    loadProjectHistory,
})

// ─── Drill-down navigation ──────────────────────────────────────────────────

function onCommitSelect(c) {
  selectedSHA.value = c.sha

  if (props.mode === 'project') {
    // Project mode: commit → files list
    currentView.value = 'files'
    if (c.sha === 'HEAD') {
      filesLoading.value = true
      files.value = wtFiles.value
      mergeGroups.value = []
      filesLoading.value = false
    } else {
      loadCommitFiles(c.sha).catch(() => {})
    }
  } else {
    // File mode: commit → diff
    currentView.value = 'diff'
    loadDiff()
  }
}

function drillBack(view) {
  if (view === 'commits') {
    selectedSHA.value = null
    files.value = []
    mergeGroups.value = []
    selectedFilePath.value = null
    diffState.value = { loading: false, empty: false, html: '' }
    handleDrillBackToCommits()
  } else if (view === 'files') {
    selectedFilePath.value = null
    diffState.value = { loading: false, empty: false, html: '' }
  }
  currentView.value = view
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

function drillToFile(f) {
  selectedFilePath.value = f.path
  currentView.value = 'diff'
  loadDiff()
}

// ─── Diff loading ───────────────────────────────────────────────────────────

async function loadCommitFiles(sha) {
  filesLoading.value = true
  files.value = []
  mergeGroups.value = []
  try {
    const resp = await gitFetch(`/api/git/commit-files?sha=${encodeURIComponent(sha)}`)
    if (!resp.ok) { files.value = []; return }
    const data = await resp.json()
    if (data && data.merge === true && Array.isArray(data.groups)) {
      mergeGroups.value = data.groups
      files.value = []
    } else if (Array.isArray(data)) {
      files.value = data
      mergeGroups.value = []
    } else {
      files.value = []
      mergeGroups.value = []
    }
  } catch {
    files.value = []
    mergeGroups.value = []
  } finally {
    filesLoading.value = false
  }
}

async function loadDiff() {
  diffState.value = { loading: true, empty: false, html: '' }

  try {
    let resp
    if (props.mode === 'project') {
      resp = await gitFetch(
        `/api/git/file-diff?sha=${encodeURIComponent(selectedSHA.value)}&path=${encodeURIComponent(selectedFilePath.value)}`
      )
    } else {
      resp = await gitFetch(
        `/api/git/diff?path=${encodeURIComponent(props.file.path)}&commit=${encodeURIComponent(selectedSHA.value)}`
      )
    }
    if (!resp.ok) {
      diffState.value = { loading: false, empty: true, html: '' }
      return
    }
    const data = await resp.json()
    if (data.empty) {
      diffState.value = { loading: false, empty: true, html: '' }
    } else {
      const filePath = props.mode === 'project' ? selectedFilePath.value : props.file.path
      diffState.value = { loading: false, empty: false, html: renderDiff(data.diff || '', filePath) }
    }
  } catch {
    diffState.value = { loading: false, empty: true, html: '' }
  }
}

// ─── Lifecycle ──────────────────────────────────────────────────────────────

function handleClose() {
  emit('close')
}

// Track previous identity to detect actual changes
const lastProjectRoot = ref(null)
const lastFilePath = ref(null)

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

  // Only load data if we have no commits loaded
  if (shouldShowFullLoading(commits.value, error.value)) {
    if (props.mode === 'file' && props.file?.path) {
      await loadFileHistory(props.file.path)
    } else {
      await loadProjectHistory()
    }
  }

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
  font-size: 14px;
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
  height: var(--header-height, 44px);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-secondary, #f8f9fa);
  flex-shrink: 0;
  gap: 8px;
}

.drilldown-count {
  font-size: 10px;
  font-weight: 700;
  background: var(--bg-tertiary, #e9ecef);
  color: var(--text-muted, #999);
  padding: 1px 6px;
  border-radius: 10px;
  flex-shrink: 0;
}

.drilldown-body {
  flex: 1;
  overflow-y: auto;
}

.drilldown-list {
  padding: 6px 0;
}

.drilldown-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 11px 14px;
  cursor: pointer;
  transition: background 0.15s;
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
  font-size: 14px;
}

/* ─── File list (project mode) ────────────────────────────────────────── */

.git-file-icon {
  flex-shrink: 0;
  color: var(--text-muted, #999);
  display: flex;
  align-items: center;
}

.git-file-type-badge {
  font-size: 10px;
  font-weight: 700;
  padding: 2px 5px;
  border-radius: 4px;
  flex-shrink: 0;
  letter-spacing: 0.02em;
}

.badge-A { background: color-mix(in srgb, var(--color-green, #16a34a) 15%, transparent); color: var(--color-green, #16a34a); }
.badge-M { background: color-mix(in srgb, var(--color-yellow, #a16207) 15%, transparent); color: var(--color-yellow, #a16207); }
.badge-D { background: color-mix(in srgb, var(--color-red, #dc2626) 15%, transparent); color: var(--color-red, #dc2626); }
.badge-R { background: color-mix(in srgb, var(--color-purple, #7c3aed) 15%, transparent); color: var(--color-purple, #7c3aed); }
.badge-U { background: var(--bg-tertiary, #f0f0f0); color: var(--text-muted, #999); }
.badge-staged { border: 1px solid var(--accent-color, #4a90d9); }

.git-file-path {
  color: var(--text-primary, #212529);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-group-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted, #999);
  padding: 8px 14px 4px;
  letter-spacing: 0.03em;
}

.merge-group + .merge-group {
  border-top: 1px solid var(--border-color, #dee2e6);
  margin-top: 4px;
  padding-top: 4px;
}
</style>
