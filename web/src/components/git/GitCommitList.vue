<template>
  <div class="drilldown-page">
    <div v-if="!(commits.length === 0 && untracked) && isGit" class="drilldown-header">
      <div class="drilldown-title">
        <span v-if="commits.length > 0" class="drilldown-count">
          <template v-if="searchLoading">
            <span class="spinner" style="width:10px;height:10px;border-width:1.5px;margin-right:4px;display:inline-block;vertical-align:middle;" />
            {{ t('git.commitList.loadingAll') }}
          </template>
          <template v-else>
            {{ filteredCommits.filter(c => !c.isWT).length + (hasMore && !commitSearch ? '+' : '') + t('git.commitList.countUnit') + countLabel }}
          </template>
        </span>
        <span v-else-if="!isGit" class="drilldown-count">{{ t('git.commitList.notInitialized') }}</span>
        <span v-else-if="!untracked" class="drilldown-count">{{ t('git.commitList.loading') }}</span>
      </div>
      <SearchInput v-if="commits.length > 0" v-model="commitSearch" :placeholder="searchPlaceholder" class="commit-search-input" />
      <button
        v-if="commits.length > 0"
        class="drilldown-refresh-btn"
        :class="{ spinning: loading, 'refresh-pulse': refreshHint }"
        :disabled="loading"
        :title="t('git.commitList.refresh')"
        @click.stop="$emit('refresh')"
      >
        <RefreshCw :size="14" />
      </button>
      <button
        v-if="isGit && mode !== 'file'"
        class="drilldown-refresh-btn"
        :title="t('git.manage.title')"
        @click.stop="$emit('manage')"
      >
        <GitBranch :size="14" />
      </button>
    </div>
    <div class="drilldown-body" ref="bodyRef">
      <div v-if="loading" class="git-history-loading">
        <div class="spinner" style="width:24px;height:24px;border-width:2px;" />
      </div>
      <div v-else-if="error" class="git-history-error">{{ error }}</div>
      <div v-else-if="!isGit" class="git-history-empty">
        <div class="empty-state-card">
          <GitBranch :size="36" :stroke-width="1.5" style="color:var(--text-muted);" />
          <div class="empty-state-title">{{ t('git.commitList.notGitRepo') }}</div>
          <div class="empty-state-desc">{{ t('git.commitList.notGitRepoDesc') }}</div>
          <div class="empty-state-hint">
            <code>git init</code>
          </div>
        </div>
      </div>
      <div v-else-if="commits.length === 0 && untracked" class="git-history-empty">
        <div class="empty-state-card">
          <FileText :size="36" :stroke-width="1.5" style="color:var(--text-muted);" />
          <div class="empty-state-title">{{ t('git.commitList.untrackedFile') }}</div>
          <div class="empty-state-desc">{{ t('git.commitList.untrackedDesc') }}</div>
          <div class="empty-state-hint">
            <code>git add &lt;filename&gt;</code> {{ t('git.commitList.untrackedHint') }}
          </div>
        </div>
      </div>
      <div v-else-if="commits.length === 0" class="git-history-empty">{{ t('git.commitList.noCommits') }}</div>
      <div v-else class="commit-list-container">
        <!-- Graph SVG - hidden during search or file mode (no multi-branch graph for single file) -->
        <GitGraph
          v-if="!isSearching && mode !== 'file'"
          class="commit-list-graph"
          :commits="filteredCommits"
          :row-height="64"
          :collapsed="graphCollapsed"
          @update:collapsed="graphCollapsed = $event"
        />
        <div v-else-if="isSearching" class="commit-list-graph-hint">
          <Info :size="14" />
        </div>
        <!-- Commit rows -->
        <div class="commit-list-content" ref="contentRef" @touchstart="onTouchStart" @touchend="onTouchEnd">
          <div
            v-for="c in filteredCommits"
            :key="c.sha"
            class="drilldown-item"
            :class="{ 'drilldown-item-selected': c.sha === selectedSHA }"
            @click="$emit('select', c)"
          >
            <div class="git-commit-info">
              <div class="git-commit-msg">{{ c.msg }}</div>
              <div class="git-commit-meta">
                <span v-if="!c.isWT" class="git-commit-sha">{{ c.sha.slice(0, 7) }}</span>
                <span v-if="c.fileCount > 0" class="git-commit-file-count">{{ c.fileCount }}</span>
                <span v-if="c.refs && c.refs.length" class="git-commit-refs">
                  <span v-for="ref in c.refs" :key="ref" class="git-ref-tag" :class="refTagClass(ref)">{{ refLabelText(ref) }}</span>
                </span>
                <span>{{ formatDate(c.date) }}</span>
                <span v-if="c.author"> · {{ c.author }}</span>
              </div>
            </div>
          </div>
          <div ref="listRef" class="git-load-more-sentinel">
            <div v-if="hasMore && loadingMore" class="git-load-more">
              <div class="spinner" style="width:20px;height:20px;border-width:2px;" />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { FileText, Info, RefreshCw, GitBranch } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import GitGraph from './GitGraph.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import { refLabelText } from '@/utils/gitGraph'
import { formatRelativeTime, formatDateTime } from '@/utils/format'
const { t } = useI18n()

const props = defineProps({
  commits: { type: Array, default: () => [] },
  isGit: { type: Boolean, default: true },
  hasMore: { type: Boolean, default: false },
  loadingMore: { type: Boolean, default: false },
  searchLoading: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  error: { type: String, default: '' },
  untracked: { type: Boolean, default: false },
  countLabel: { type: String, default: '' },
  searchPlaceholder: { type: String, default: '' },
  selectedSHA: { type: String, default: null },
  refreshHint: { type: Boolean, default: false },
  mode: { type: String, default: 'project' }, // 'project' | 'file'
})

const emit = defineEmits(['select', 'search', 'load-more', 'refresh', 'manage'])

const commitSearch = ref('')
const listRef = ref(null)
const contentRef = ref(null)
const bodyRef = ref(null)
const observer = ref(null)
const graphCollapsed = ref(false)

// Touch swipe handling for graph toggle
const SWIPE_THRESHOLD = 50
let touchStartX = 0
let touchStartY = 0
let touchStartTime = 0

function onTouchStart(e) {
  if (props.mode === 'file') return // No graph to toggle in file mode
  touchStartX = e.touches[0].clientX
  touchStartY = e.touches[0].clientY
  touchStartTime = Date.now()
}

function onTouchEnd(e) {
  if (props.mode === 'file') return // No graph to toggle in file mode
  const dx = e.changedTouches[0].clientX - touchStartX
  const dy = e.changedTouches[0].clientY - touchStartY
  const dt = Date.now() - touchStartTime
  // Only trigger on primarily horizontal swipes, fast enough
  if (dt > 500 || Math.abs(dy) > Math.abs(dx) || Math.abs(dx) < SWIPE_THRESHOLD) return
  if (dx < 0) {
    // Swipe left: hide graph
    graphCollapsed.value = true
  } else {
    // Swipe right: show graph
    graphCollapsed.value = false
  }
}

const isSearching = computed(() => commitSearch.value.trim().length > 0)

const filteredCommits = computed(() => {
  const q = commitSearch.value.trim().toLowerCase()
  if (!q) return props.commits
  return props.commits.filter(c => c.msg.toLowerCase().includes(q))
})

function formatDate(dateStr) {
  if (!dateStr) return ''
  const diffDays = (Date.now() - new Date(dateStr).getTime()) / 86400000
  return diffDays < 7 ? formatRelativeTime(dateStr) : formatDateTime(dateStr)
}

function refTagClass(ref) {
  if (ref === 'HEAD') return 'ref-head'
  if (ref.startsWith('tag: ')) return 'ref-tag'
  return 'ref-branch'
}

let searchTimer = null

watch(commitSearch, (q) => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => emit('search', q), 300)
})

function setupObserver() {
  // No-op: observer is now created lazily in observeList() so we can pass
  // bodyRef as the root at construction time (root cannot be changed later).
}

function observeList() {
  if (!listRef.value) return
  // Disconnect any previous observer
  if (observer.value) observer.value.disconnect()
  // Create observer with the actual scroll container as root
  // so that scrolling inside .drilldown-body triggers intersection.
  observer.value = new IntersectionObserver((entries) => {
    if (entries[0].isIntersecting && props.hasMore && !props.loadingMore) {
      emit('load-more')
    }
  }, { threshold: 0.1, rootMargin: '100px', root: bodyRef.value || undefined })
  observer.value.observe(listRef.value)
}

function unobserveList() {
  if (observer.value) {
    observer.value.disconnect()
  }
}

onMounted(() => {
  setupObserver()
})

onUnmounted(() => {
  unobserveList()
  clearTimeout(searchTimer)
})

defineExpose({ observeList, unobserveList, commitSearch })
</script>

<style scoped>
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

.drilldown-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary, #212529);
  overflow: hidden;
  flex: 1;
  min-width: 0;
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

.commit-search-input {
  flex: 0 1 auto;
  max-width: 160px;
  min-width: 80px;
}

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
  transition: background 0.15s, color 0.15s, transform 0.3s;
}

.drilldown-refresh-btn:hover:not(:disabled) {
  background: var(--accent-color, #4a90d9);
  color: #fff;
}

.drilldown-refresh-btn:active:not(:disabled) {
  transform: scale(0.92);
}

.drilldown-refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.drilldown-refresh-btn.spinning svg {
  animation: spin 0.8s linear infinite;
}

/* Stale data indicator — pulsing glow on refresh button */
.drilldown-refresh-btn.refresh-pulse {
  animation: refresh-pulse-glow 1.5s ease-in-out infinite;
  color: var(--accent-color, #4a90d9);
}

@keyframes refresh-pulse-glow {
  0%, 100% { box-shadow: 0 0 0 0 rgba(74, 144, 217, 0); }
  50% { box-shadow: 0 0 6px 2px rgba(74, 144, 217, 0.4); }
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.drilldown-body {
  flex: 1;
  overflow-y: auto;
}

/* ─── Commit list: Graph + Info ─────────────────────────────────────────── */

.commit-list-container {
  position: relative;
  display: flex;
}

.commit-list-graph {
  position: sticky;
  left: 0;
  z-index: 1;
  flex-shrink: 0;
}

.commit-list-content {
  flex: 1;
  min-width: 0;
}

.drilldown-item {
  display: flex;
  align-items: center;
  padding: 10px 14px;
  cursor: pointer;
  transition: background 0.15s;
  border-bottom: 1px solid var(--border-color, #dee2e6);
  height: 64px;
  box-sizing: border-box;
  overflow: hidden;
}

.drilldown-item:hover {
  background: var(--bg-secondary, #f8f9fa);
}

.drilldown-item:active {
  background: var(--bg-tertiary, #e9ecef);
}

.git-commit-info {
  flex: 1;
  min-width: 0;
}

.git-commit-msg {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  color: inherit;
}

.git-commit-meta {
  font-size: 11px;
  color: var(--text-muted, #999);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.git-load-more-sentinel {
  min-height: 1px;
}

.git-load-more {
  padding: 20px 14px;
  display: flex;
  justify-content: center;
  min-height: 60px;
}

.git-history-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

.git-history-error,
.git-history-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-muted, #999);
    font-size: 14px;
}

.empty-state-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 32px 24px;
    gap: 8px;
}

.empty-state-title {
    font-size: 14px;
    font-weight: 500;
    color: var(--text-primary);
}

.empty-state-desc {
    font-size: 13px;
    color: var(--text-muted);
}

.empty-state-hint {
    font-size: 12px;
    color: var(--text-muted);
    margin-top: 4px;
}

.empty-state-hint code {
    background: var(--bg-tertiary);
    padding: 2px 6px;
    border-radius: 4px;
    font-family: monospace;
    font-size: 11px;
}

/* Selected commit highlight */
.drilldown-item-selected {
  background: rgba(74, 144, 217, 0.08);
  border-left: 3px solid var(--accent-color, #4a90d9);
  padding-left: 11px;
}

/* Search graph hint */
.commit-list-graph-hint {
  width: 24px;
  flex-shrink: 0;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: 16px;
  color: var(--text-muted, #ccc);
}

/* Short SHA tag */
.git-commit-sha {
  font-family: 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 10px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 1px 4px;
  border-radius: 3px;
  margin-right: 4px;
}

/* File change count badge */
.git-commit-file-count {
  font-size: 10px;
  font-weight: 600;
  color: var(--accent-color, #4a90d9);
  background: rgba(74, 144, 217, 0.12);
  padding: 1px 5px;
  border-radius: 3px;
  margin-right: 4px;
}

/* Ref tags */
.git-commit-refs {
  display: inline-flex;
  gap: 3px;
  margin-right: 4px;
}
.git-ref-tag {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 3px;
  white-space: nowrap;
}
.ref-head { background: #1a1a2e; color: #fff; }
.ref-branch { background: rgba(74, 144, 217, 0.15); color: #4a90d9; }
.ref-tag { background: rgba(85, 85, 85, 0.15); color: #666; }

</style>
