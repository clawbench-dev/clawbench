<template>
  <div class="drilldown-page">
    <!-- Toolbar. ONE row: where you are on the left, what you can do on the
         right. It used to be two — a title/count/search bar plus a separate
         branch bar whose only other content was a sentence, so the second row
         was 80% empty and every control sat in a different shape.

         The branch picker is the page's primary control, so it lives outside
         the ActionBar: ActionBar folds what does not fit, and folding the
         control that says "which history am I reading" would be worse than
         folding a button. Everything else is a plain <button> inside it. -->
    <div v-if="searchOpen && !(commits.length === 0 && untracked) && isGit" class="drilldown-header git-search-row">
      <div class="commit-search">
        <SearchInput
          ref="searchRef"
          v-model="commitSearch"
          :placeholder="searchPlaceholder || t('git.commitList.searchPlaceholder')"
          @enter="listNav.confirm"
          @down="listNav.down"
          @up="listNav.up"
        />
      </div>
      <button class="search-cancel" @click.stop="closeSearch">{{ t('common.cancel') }}</button>
    </div>
    <div v-else-if="!(commits.length === 0 && untracked) && isGit" class="drilldown-header git-toolbar">
      <div v-if="showBranchBar" class="branch-picker">
        <button
          class="branch-chip"
          :class="{ 'is-other': isOtherBranch, 'is-open': branchMenuOpen }"
          :title="t('git.branchView.switchHint')"
          @click.stop="toggleBranchMenu"
        >
          <GitBranch :size="12" />
          <span class="branch-chip-name">{{ shownBranch }}</span>
          <ChevronDown :size="10" />
        </button>

        <div v-if="branchMenuOpen" class="branch-menu" @click.stop>
          <!-- The old bar spent a whole row saying "本地 1 个分支". It belongs
               here: the moment you open the picker is the moment it is useful. -->
          <div class="branch-menu-label">{{ t('git.branchView.localCount', { count: branches.length }) }}</div>
          <button
            v-for="b in branches"
            :key="b.name"
            class="branch-menu-item"
            :class="{ active: b.name === shownBranch }"
            @click="pickBranch(b.name)"
          >
            <GitBranch :size="12" />
            <span class="branch-menu-name">{{ b.name }}</span>
            <span v-if="b.name === currentBranch" class="branch-menu-tag">{{ t('git.branchView.current') }}</span>
            <Check v-if="b.name === shownBranch" :size="12" class="branch-menu-check" />
          </button>
          <div class="branch-menu-sep"></div>
          <button class="branch-menu-item" @click="openManage">
            <Settings :size="12" />
            <span class="branch-menu-name">{{ t('git.branchView.manage') }}</span>
          </button>
        </div>
      </div>

      <!-- Divergence from the upstream, right next to the branch it belongs to.
           Only rendered when there IS a difference: "↑0 ↓0" on every clean
           branch is noise, and it would read as "in sync" for a branch that
           has no upstream at all.

           Outside the branch picker on purpose: the picker is desktop-only
           chrome, while the numbers also matter to hosts that show no picker.

           The numbers are buttons only when the detail behind them was actually
           loaded. With counts but no detail (an older server, or a failed
           request) they stay plain text — a button that silently does nothing
           is worse than no button. -->
      <span v-if="trackInfo" class="branch-track">
        <template v-if="hasDetail">
          <button
            v-if="trackInfo.behind > 0"
            class="branch-track-btn branch-track-behind"
            :title="trackTitle"
            :aria-label="t('git.divergence.revealHint', { count: trackInfo.behind })"
            data-action="track-behind"
            @click.stop="revealRemoteCommits"
          >↓{{ trackInfo.behind }}</button>
          <button
            v-if="trackInfo.ahead > 0"
            class="branch-track-btn branch-track-ahead"
            :title="trackTitle"
            :aria-label="t('git.divergence.jumpHint', { count: trackInfo.ahead })"
            data-action="track-ahead"
            @click.stop="jumpToFirstUnpushed"
          >↑{{ trackInfo.ahead }}</button>
        </template>
        <template v-else>
          <span v-if="trackInfo.behind > 0" class="branch-track-behind" :title="trackTitle">↓{{ trackInfo.behind }}</span>
          <span v-if="trackInfo.ahead > 0" class="branch-track-ahead" :title="trackTitle">↑{{ trackInfo.ahead }}</span>
        </template>
      </span>

      <!-- How many commits are on screen. A badge, not a sentence: the full
           wording ("30+ 条提交记录") is one hover away, and it was eating the
           width the branch picker and the buttons needed. -->
      <span
        v-if="commits.length > 0"
        class="drilldown-count count-badge git-count"
        :title="countTitle"
      >
        <LoadingIndicator v-if="searchLoading" size="sm" inline />
        <template v-else>{{ countText }}</template>
      </span>
      <span v-else-if="!isGit" class="drilldown-count count-badge">{{ t('git.commitList.notInitialized') }}</span>
      <span v-else-if="!untracked" class="drilldown-count count-badge">{{ t('git.commitList.loading') }}</span>

      <ActionBar class="git-actions" mode="menu" align="end" size="header">
        <button
          v-if="commits.length > 0"
          class="drilldown-refresh-btn"
          data-action="search"
          :title="t('git.commitList.search')"
          :aria-label="t('git.commitList.search')"
          @click.stop="openSearch"
        >
          <Search :size="14" />
          <span class="action-label">{{ t('git.commitList.search') }}</span>
        </button>
        <!-- Fetch from every remote. Only the remote-tracking refs move, so this
             is safe to press at any time — the working tree and the current
             branch stay exactly as they are; the numbers next to the branch are
             what change. -->
        <button
          v-if="isGit && mode !== 'file' && commits.length > 0"
          class="drilldown-refresh-btn"
          :title="t('git.fetch.title')"
          :aria-label="t('git.fetch.title')"
          :disabled="fetching"
          data-action="fetch"
          @click.stop="$emit('fetch')"
        >
          <LoadingIndicator v-if="fetching" size="sm" inline />
          <CloudDownload v-else :size="14" />
          <span class="action-label">{{ t('git.fetch.title') }}</span>
        </button>
        <RefreshButton
          v-if="commits.length > 0"
          class="drilldown-refresh-btn"
          data-action="refresh"
          :class="{ 'refresh-pulse': refreshHint }"
          :loading="refreshing"
          :disabled="refreshing"
          :title="t('git.commitList.refresh')"
          @click.stop="handleRefresh"
        >
          <span class="action-label">{{ t('git.commitList.refresh') }}</span>
        </RefreshButton>
        <!-- Merge-commit filter. A merge row lists every file both sides touched,
             so in a history full of branch merges the interesting commits drown.
             Off by default: hiding commits is a view choice, not a correction. -->
        <button
          v-if="isGit && mode !== 'file' && commits.length > 0"
          class="drilldown-refresh-btn"
          :class="{ 'is-on': hideMerges }"
          :title="hideMerges ? t('git.commitList.showMerges') : t('git.commitList.hideMerges')"
          :aria-label="hideMerges ? t('git.commitList.showMerges') : t('git.commitList.hideMerges')"
          :aria-pressed="hideMerges"
          data-action="toggle-merge-commits"
          @click.stop="hideMerges = !hideMerges"
        >
          <GitMerge :size="14" />
          <span class="action-label">{{ t('git.commitList.mergeFilter') }}</span>
        </button>
        <!-- Branch / tag / worktree management. NOT a "more" glyph: the bar has
             its own overflow button, and two ⋮ sitting a few pixels apart for
             different things is unreadable. -->
        <button
          v-if="isGit && mode !== 'file'"
          class="drilldown-refresh-btn"
          :title="t('git.manage.title')"
          data-action="manage"
          @click.stop="$emit('manage')"
        >
          <ListTree :size="14" />
          <span class="action-label">{{ t('git.manage.title') }}</span>
        </button>
      </ActionBar>
    </div>

    <!-- Reading another branch is a read-only view: nothing is checked out, so
         the bar has to keep saying where the workspace actually is. This is a
         strip rather than part of the toolbar because it is a warning, not a
         control — and it only exists in that one state. -->
    <div v-if="isOtherBranch && showBranchBar" class="branch-alert">
      <Eye :size="11" class="branch-alert-icon" />
      <span class="branch-note branch-note--warn">{{ t('git.branchView.readOnly', { branch: currentBranch }) }}</span>
      <button class="branch-back-btn" @click.stop="emit('change-branch', '')">
        {{ t('git.branchView.backTo', { branch: currentBranch }) }}
      </button>
    </div>

    <!-- Commits the upstream has and this branch does not. They are NOT part of
         the history below: that list is walked from the branch's own commit, and
         the graph's lanes are drawn from parent links inside it — splicing
         foreign rows in would draw a graph that contradicts the list.

         Collapsed by default and only present when there is something to show,
         so a branch that is up to date looks exactly as it did before. -->
    <div v-if="behindCommits.length" class="remote-commits" :class="{ 'is-open': remoteOpen }">
      <button
        class="remote-commits-head"
        :aria-expanded="remoteOpen"
        data-action="toggle-remote-commits"
        @click.stop="toggleRemoteCommits"
      >
        <CloudDownload :size="12" class="remote-commits-icon" />
        <span class="remote-commits-title">
          {{ t('git.divergence.remoteTitle', { upstream: divergence.upstream, count: behindCount }) }}
        </span>
        <ChevronDown :size="12" class="remote-commits-chevron" />
      </button>
      <div v-if="remoteOpen" class="remote-commits-list">
        <div
          v-for="c in behindCommits"
          :key="c.sha"
          class="remote-commit"
          @click="$emit('select', c)"
        >
          <!-- Two lines: the subject needs the width, and cramming the hash,
               date and author onto the same line left it clipped to a stub. -->
          <div class="remote-commit-head">
            <span class="git-ref-tag git-ref-tag--remote">{{ t('git.divergence.remoteTag') }}</span>
            <span class="remote-commit-msg">{{ c.msg }}</span>
          </div>
          <div class="remote-commit-meta">
            <span class="remote-commit-sha">{{ c.sha.slice(0, 7) }}</span>
            <span>{{ formatDate(c.date) }}</span>
            <span v-if="c.author">· {{ c.author }}</span>
          </div>
        </div>
        <div v-if="behindCount > behindCommits.length" class="remote-commits-more">
          {{ t('git.divergence.moreRemote', { count: behindCount - behindCommits.length }) }}
        </div>
      </div>
    </div>
    <div class="drilldown-body" ref="bodyRef">
      <div v-if="loading" class="git-history-loading git-history-loading--skeleton">
        <PanelSkeleton variant="list" :rows="10" :meta="true" :label="t('common.loading')" />
      </div>
      <div v-else-if="error" class="git-history-error">{{ error }}</div>
      <div v-else-if="!isGit" class="git-history-empty">
        <div class="empty-state-card">
          <GitBranch class="empty-state__icon" />
          <div class="empty-state-title">{{ t('git.commitList.notGitRepo') }}</div>
          <div class="empty-state-desc">{{ t('git.commitList.notGitRepoDesc') }}</div>
          <div class="empty-state-hint">
            <code>git init</code>
          </div>
        </div>
      </div>
      <div v-else-if="commits.length === 0 && untracked" class="git-history-empty">
        <div class="empty-state-card">
          <FileText class="empty-state__icon" />
          <div class="empty-state-title">{{ t('git.commitList.untrackedFile') }}</div>
          <div class="empty-state-desc">{{ t('git.commitList.untrackedDesc') }}</div>
          <div class="empty-state-hint">
            <code>git add &lt;filename&gt;</code> {{ t('git.commitList.untrackedHint') }}
          </div>
        </div>
      </div>
      <div v-else-if="commits.length === 0" class="git-history-empty">
        <div class="empty-state-card">
          <GitCommitHorizontal class="empty-state__icon" />
          <div class="empty-state-title">{{ t('git.commitList.noCommits') }}</div>
        </div>
      </div>
      <div v-else class="commit-list-container">
        <!-- Graph SVG - hidden during search or file mode (no multi-branch graph for single file).
             Also hidden while merge commits are filtered out: the lanes are drawn
             from parent links, so removing the merge rows would leave a graph that
             silently contradicts the list next to it. -->
        <GitGraph
          v-if="!isSearching && !hideMerges && mode !== 'file'"
          class="commit-list-graph"
          :commits="filteredCommits"
          :row-height="graphRowHeight"
          :collapsed="graphCollapsed"
          @update:collapsed="graphCollapsed = $event"
        />
        <div v-else-if="isSearching" class="commit-list-graph-hint">
          <Info :size="14" />
        </div>
        <!-- Commit rows -->
        <div class="commit-list-content" ref="contentRef" @touchstart.passive="onTouchStart" @touchend="onTouchEnd">
          <div
            v-for="(c, idx) in filteredCommits"
            :key="c.sha"
            class="drilldown-item"
            :class="{ 'drilldown-item-selected': c.sha === selectedSHA, 'drilldown-item-active': listNav.activeIndex.value === idx, 'is-unpushed': aheadSet.has(c.sha) }"
            :draggable="isWideScreen && canDragCommit(c)"
            @dragstart="onCommitDragStart(c, $event)"
            @dragend="cleanupDragGhost()"
            @click="$emit('select', c)"
          >
            <div class="git-commit-info">
              <div class="git-commit-msg">
                <span class="git-commit-msg-text">{{ c.msg }}</span>
                <span
                  v-if="c.isWT && wtFileCount > 0"
                  class="git-commit-file-count count-badge"
                  :title="t('git.history.fileCount', { count: wtFileCount })"
                >{{ wtFileCount }}</span>
              </div>
              <div class="git-commit-meta">
                <span v-if="!c.isWT" class="git-commit-sha">{{ c.sha.slice(0, 7) }}</span>
                <!-- Same visual language as a ref tag: this row is on the branch
                     but not on the upstream, so it has not been pushed yet. -->
                <span v-if="aheadSet.has(c.sha)" class="git-ref-tag git-ref-tag--unpushed" :title="t('git.divergence.unpushedHint')">
                  {{ t('git.divergence.unpushed') }}
                </span>
                <span v-if="c.refs && c.refs.length" class="git-commit-refs">
                  <span v-for="ref in c.refs" :key="ref" class="git-ref-tag" :class="refTagClass(ref)">{{ refLabelText(ref) }}</span>
                </span>
                <span>{{ formatDate(c.date) }}</span>
                <span v-if="c.author"> · {{ c.author }}</span>
              </div>
            </div>
            <!-- Quote this commit into a conversation. Dragging the row does the
                 same thing, but a drag is invisible: every other quotable object
                 (issue/PR, meeting, file) also has a button, and a commit row
                 was the one place the action could not be discovered. -->
            <button
              v-if="!c.isWT && c.sha"
              class="commit-quote-btn"
              :title="t('file.header.quoteInChat')"
              :aria-label="t('file.header.quoteInChat')"
              @click.stop="quoteCommit(c)"
            >
              <MessageSquare :size="14" />
            </button>
          </div>
          <div ref="listRef" class="git-load-more-sentinel">
            <div v-if="hasMore && loadingMore" class="git-load-more">
              <LoadingIndicator size="sm" inline />
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { FileText, GitBranch, GitCommitHorizontal, GitMerge, CloudDownload, Info, ListTree, MessageSquare, Search, ChevronDown, Check, Eye, Settings } from 'lucide-vue-next'
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import GitGraph from './GitGraph.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ActionBar from '@/components/common/ActionBar.vue'
import { useListNav } from '@/composables/useListNav'
import { useListKeys } from '@/composables/useListKeys'
import { refLabelText } from '@/utils/gitGraph'
import { formatRelativeTime, formatDateTime } from '@/utils/format'
import { startQuoteDrag, commitDragPayload } from '@/utils/quoteDrag'
import { useWideScreenLayout } from '@/composables/useWideScreenLayout'
import { useQuoteQuestion } from '@/composables/useQuoteQuestion.ts'
import { useToast } from '@/composables/useToast'
import { cleanupDragGhost } from '@/utils/attachDrag'
import { useUiScale } from '@/composables/useUiScale'
const { t } = useI18n()
const toast = useToast()

const { isWideScreen } = useWideScreenLayout()

/**
 * Commit row height, in CSS px, for the graph to lay its lanes out against.
 *
 * `.drilldown-item` is `calc(64 * var(--dp))` in the stylesheet, so the SVG's
 * row grid has to grow with the same factor — a fixed 64 would draw each commit
 * node progressively higher than its row as the scale rises (off by 32px by the
 * second row at 150%). Kept reactive because the rows resize without a
 * `resize` event.
 */
const DESIGN_ROW_HEIGHT = 64
// Shared composable rather than a local ref + listener: this had the exact
// module-level copy of the subscription that leaked scale between instances.
const uiScale = useUiScale()
const graphRowHeight = computed(() => DESIGN_ROW_HEIGHT * uiScale.value)

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
  /** Changed-file count badge on the working-tree row (0 hides it). */
  wtFileCount: { type: Number, default: 0 },
  /**
   * Local branches offered by the picker. Empty hides the branch bar — hosts
   * that cannot browse other branches (the mobile file-history sheet, file
   * mode) simply pass nothing.
   */
  branches: { type: Array, default: () => [] },
  /** The checked-out branch, as reported by the server. */
  currentBranch: { type: String, default: '' },
  /** Branch whose history is on screen ('' = the checked-out branch). */
  viewBranch: { type: String, default: '' },
  /** True while a fetch is in flight (drives the button's spinner + disabled state). */
  fetching: { type: Boolean, default: false },
  /**
   * Which commits differ from the upstream (see /api/git/divergence).
   *
   * Empty by default: hosts that never load it (file mode) simply get no
   * "unpushed" badges and no remote strip.
   */
  divergence: {
    type: Object,
    default: () => ({ hasUpstream: false, upstream: '', ahead: [], behind: [], aheadCount: 0, behindCount: 0 }),
  },
})

const emit = defineEmits(['select', 'search', 'load-more', 'refresh', 'manage', 'change-branch', 'fetch'])

const isOtherBranch = computed(() => !!props.viewBranch && props.viewBranch !== props.currentBranch)
const shownBranch = computed(() => props.viewBranch || props.currentBranch)

/**
 * How far the branch on screen is from its upstream, or null when it matches.
 *
 * The divergence endpoint is preferred because it is the same source as the
 * detail lists below (and it is the only one the mobile sheet loads). The
 * branch listing is the fallback for hosts/servers that only have counts.
 *
 * A branch with no upstream reports nothing at all: "↑0 ↓0" would read as
 * "in sync", when in fact there is nothing to be in sync with.
 */
const trackInfo = computed(() => {
  const d = props.divergence
  if (d && d.hasUpstream) {
    const ahead = d.aheadCount || 0
    const behind = d.behindCount || 0
    if (ahead === 0 && behind === 0) return null
    return { ahead, behind, upstream: d.upstream || shownBranch.value }
  }
  const b = props.branches.find(x => x.name === shownBranch.value)
  if (!b) return null
  const ahead = b.ahead || 0
  const behind = b.behind || 0
  if (ahead === 0 && behind === 0) return null
  return { ahead, behind, upstream: b.remoteTracking || shownBranch.value }
})

const trackTitle = computed(() => t('git.branchView.trackTitle', {
  upstream: trackInfo.value?.upstream || '',
  ahead: trackInfo.value?.ahead || 0,
  behind: trackInfo.value?.behind || 0,
}))

/** SHAs already on this branch but not on the upstream — the "unpushed" set. */
const aheadSet = computed(() => new Set(props.divergence?.ahead || []))
/** Commits only the upstream has, newest first. */
const behindCommits = computed(() => props.divergence?.behind || [])
const behindCount = computed(() => props.divergence?.behindCount ?? behindCommits.value.length)
/**
 * Whether the commit-level detail behind the numbers is available. The counts
 * also exist in the branch listing, but that carries no SHAs — the numbers are
 * then shown as plain text instead of dead buttons.
 */
const hasDetail = computed(() => !!props.divergence?.hasUpstream)
/** The remote strip starts collapsed: it is a notice, not the main list. */
const remoteOpen = ref(false)

function toggleRemoteCommits() {
  remoteOpen.value = !remoteOpen.value
}

/**
 * "↓3" click: open the strip.
 *
 * The strip is pinned above the scrolling body, so opening it is enough to see
 * the commits — no scrolling, which would also move the history under the user
 * for no reason.
 */
function revealRemoteCommits() {
  if (!behindCommits.value.length) {
    // The count is known but the list is not (git log failed, or the payload
    // was cut). Say so rather than opening an empty strip.
    toast.show(t('git.divergence.detailUnavailable'), { type: 'info', duration: 4000 })
    return
  }
  remoteOpen.value = true
}

/**
 * "↑2" click: jump to the newest commit that has not been pushed.
 *
 * The unpushed commits are the newest ones on the branch, so the first match is
 * also the most relevant. When the current list holds none of them (a search
 * filter, or a page that ends before them) there is nothing to scroll to — say
 * so instead of doing nothing.
 */
function jumpToFirstUnpushed() {
  const el = contentRef.value?.querySelector('.drilldown-item.is-unpushed')
  if (!el) {
    toast.show(t('git.divergence.notInPage'), { type: 'info', duration: 4000 })
    return
  }
  el.scrollIntoView?.({ block: 'center', behavior: 'smooth' })
  el.classList.add('is-flash')
  flashTimer = window.setTimeout(() => {
    el.classList.remove('is-flash')
    flashTimer = null
  }, 1400)
}
const showBranchBar = computed(() => props.mode !== 'file' && props.branches.length > 0)

// ── Branch picker ──
const branchMenuOpen = ref(false)

function toggleBranchMenu() {
  branchMenuOpen.value = !branchMenuOpen.value
}

function closeBranchMenu() {
  branchMenuOpen.value = false
}

function pickBranch(name) {
  branchMenuOpen.value = false
  // Selecting the checked-out branch means "stop browsing elsewhere"; the host
  // normalises that back to '' so the bar loses its read-only state.
  emit('change-branch', name === props.currentBranch ? '' : name)
}

function openManage() {
  branchMenuOpen.value = false
  emit('manage')
}

function onDocumentClick() {
  closeBranchMenu()
}

watch(branchMenuOpen, (open) => {
  if (open) document.addEventListener('click', onDocumentClick)
  else document.removeEventListener('click', onDocumentClick)
})

// A project switch replaces the branch list; a menu left open would show the
// previous project's branches.
watch(() => props.branches, closeBranchMenu)

/**
 * Whether a commit row can be dragged into the chat as a quote.
 *
 * The working-tree row ("uncommitted changes") has no commit to reference, so it
 * is not draggable — dragging it would produce a card that cannot be jumped to
 * and that names nothing.
 */
function canDragCommit(c) {
  return !c.isWT && !!c.sha
}

/**
 * Start dragging a commit into the chat: dropping it stages a quote card for
 * that commit (no annotation). The commit's sha is the locator, which is what
 * lets the card jump back to the commit in git history.
 */
function onCommitDragStart(c, e) {
  const payload = commitDragPayload(c)
  if (!startQuoteDrag(e, payload)) {
    // Nothing usable to drag — cancel so the browser does not start an empty
    // drag that would silently drop nothing.
    e.preventDefault()
  }
}

/**
 * Quote a commit from the row's button.
 *
 * Same payload as the drag (`commitDragPayload`), but through the shared quote
 * bar so the user can write an annotation and, for a cross-conversation quote,
 * pick a destination. The bar is a global singleton, so no event has to be
 * plumbed up to App.vue.
 */
const quoteQuestion = useQuoteQuestion()

function quoteCommit(c) {
  const payload = commitDragPayload(c)
  if (!payload) return
  quoteQuestion.openComposer({ quote: payload, label: payload.filePath })
}

const commitSearch = ref('')
const listRef = ref(null)
const contentRef = ref(null)
const bodyRef = ref(null)
const observer = ref(null)
const graphCollapsed = ref(false)

/**
 * The search field is not a permanent resident of the toolbar.
 *
 * It used to take about half the row, which is why the branch picker had to
 * live on a second row of its own. Collapsing it to a button buys back that
 * width; opening it replaces the toolbar with the field, so the row count and
 * the bar height never change.
 */
const searchOpen = ref(false)
const searchRef = ref(null)

async function openSearch() {
  searchOpen.value = true
  await nextTick()
  searchRef.value?.focus?.()
}

function closeSearch() {
  searchOpen.value = false
  // A hidden filter is a lie: the list would stay filtered with nothing on
  // screen saying why. Closing the field therefore clears the query.
  commitSearch.value = ''
}

// Refresh-button spin feedback. The actual load is delegated to the parent
// (onRefresh → loadProjectHistory/loadFileHistory) and the `loading` prop is
// hard-coded false there, so drive the spin locally for a consistent 600ms
// minimum-visible window and dedup clicks.
const refreshing = ref(false)
let refreshTimer = null
// The highlight on the row an "↑N" jump landed on. Cleared on unmount so a
// pending timer cannot touch a detached element (and so tests do not leak it).
let flashTimer = null
function handleRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => { refreshing.value = false }, 600)
  emit('refresh')
}

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

/**
 * Hide merge commits from the list.
 *
 * A merge row expands into "everything both sides did" (the server groups those
 * files per parent), which in a branch-merge workflow means one row can carry a
 * hundred files and bury the actual work. This is display-only: pagination and
 * the `hasMore` flag still count every commit git returned.
 */
const hideMerges = ref(false)

/** A commit with two or more parents. `parents` is absent on older hosts. */
function isMergeCommit(c) {
  return !c.isWT && Array.isArray(c.parents) && c.parents.length > 1
}

// ── Keyboard ↑/↓ + Enter navigation over commits ──
const listNav = useListNav({
  getCount: () => filteredCommits.value.length,
  onConfirm: (idx) => emit('select', filteredCommits.value[idx]),
  onActiveChange: scrollActiveIntoView,
})
// Document-level keys so navigation works regardless of where focus is inside the list.
// The component is only mounted while the commit list is the active view.
useListKeys({ isOpen: () => true, nav: listNav })

function scrollActiveIntoView(index) {
  const items = document.querySelectorAll('.commit-list-content .drilldown-item')
  const el = items[index]
  if (el && typeof el.scrollIntoView === 'function') {
    el.scrollIntoView({ behavior: 'auto', block: 'nearest' })
  }
}

/**
 * Shortest commit-id prefix that is treated as an id search.
 *
 * 4 hex chars is the shortest abbreviation git itself accepts; below that a
 * query like "a" or "ab" would match a large share of any history and read as
 * "search is broken". Shorter hex queries still search commit messages, so
 * nothing is lost — and a word that happens to be hex ("dead", "added") is
 * matched on both channels, so message search is never shadowed by the id path.
 */
const SHA_MIN_PREFIX = 4

/**
 * Whether a commit row matches the query. Matches the commit message (substring,
 * as before) or, when the query looks like a commit id, the sha prefix — so
 * pasting an id from a chat link finds the commit without needing the full 40
 * characters.
 */
function matchesQuery(c, q) {
  if (c.msg.toLowerCase().includes(q)) return true
  if (q.length >= SHA_MIN_PREFIX && c.sha && /^[0-9a-f]+$/.test(q)) {
    return c.sha.toLowerCase().startsWith(q)
  }
  return false
}

const filteredCommits = computed(() => {
  const q = commitSearch.value.trim().toLowerCase()
  const visible = hideMerges.value ? props.commits.filter(c => !isMergeCommit(c)) : props.commits
  if (!q) return visible
  return visible.filter(c => matchesQuery(c, q))
})

watch(filteredCommits, () => listNav.reset())

/**
 * The badge reads "30+", not "30+ 条提交记录".
 *
 * The sentence was ~110px of the header on a 393px phone — more than the branch
 * picker — to say something a badge already says. The full wording survives as
 * the tooltip, so nothing is lost for anyone who needs it spelled out.
 */
const countText = computed(() => {
  const n = filteredCommits.value.filter(c => !c.isWT).length
  return `${n}${props.hasMore && !commitSearch.value ? '+' : ''}`
})

const countTitle = computed(() => `${countText.value}${t('git.commitList.countUnit')}${props.countLabel}`)

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
  nextTick(() => observeList())
})

onUnmounted(() => {
  unobserveList()
  document.removeEventListener('click', onDocumentClick)
  clearTimeout(searchTimer)
  if (refreshTimer) { clearTimeout(refreshTimer); refreshTimer = null }
  if (flashTimer) { clearTimeout(flashTimer); flashTimer = null }
})

defineExpose({ observeList, unobserveList, commitSearch })
</script>

<style scoped>
/* The drill-down shell, header bar, its title, the count badge's colour and the
   header icon buttons are declared GLOBALLY (web/css/components.css, "Git
   history panel chrome") because three components render them — see that
   section's header for why. Only what is specific to this list lives here. */

/* The field takes the width left over between the row's edges and the cancel
   button. No max-width: filling the remaining space is the point. */
.commit-search {
  flex: 1;
  min-width: 0;
}

.git-search-row {
  gap: var(--space-3);
}

/* Closing the field is a text action, not another square icon: it belongs to
   the field it dismisses. */
.search-cancel {
  flex: none;
  height: calc(28 * var(--dp));
  padding: 0 var(--space-4);
  border: none;
  border-radius: var(--radius-lg);
  background: transparent;
  color: var(--accent-color);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
}

.search-cancel:hover {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

/* ─── Toolbar ────────────────────────────────────────────────────────────
   One row: branch picker (fixed) · count (fixed) · ActionBar (absorbs the
   rest, hugging the trailing edge). The picker and the badge never fold —
   ActionBar only owns the buttons. */
.git-toolbar {
  /* .drilldown-header already supplies height/padding/gap. */
  position: relative;
}

.branch-picker {
  position: relative;
  flex: none;
  display: flex;
  align-items: center;
}

.git-count {
  flex: none;
  font-variant-numeric: tabular-nums;
}

.git-actions {
  flex: 1 1 0;
  gap: var(--space-2);
}



/* ActionBar's own overflow button, matched to the bar's buttons — a bare
   transparent glyph next to three filled squares reads as a different kind of
   control rather than the same row's overflow. */
.git-actions :deep(.ab-more) {
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
}

.git-actions :deep(.ab-more:hover) {
  background: var(--bg-tertiary);
}

/* ─── Branch picker ──────────────────────────────────────────────────────
   Amber once the view no longer matches the workspace, because that mismatch
   is the one thing a user must not lose track of. */
.branch-chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  height: calc(26 * var(--dp));
  padding: 0 var(--space-3);
  max-width: calc(190 * var(--dp));
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
  flex: none;
}

.branch-chip:hover {
  background: var(--bg-tertiary);
}

.branch-chip.is-open {
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
}

.branch-chip.is-other {
  background: color-mix(in srgb, var(--color-yellow, #f59e0b) 16%, transparent);
  border-color: color-mix(in srgb, var(--color-yellow, #f59e0b) 30%, transparent);
}

.branch-chip-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Divergence from the upstream (↑ahead ↓behind), right after the branch chip.
   The numbers carry the meaning; the colour only marks the actionable
   direction — "behind" means there is something to fetch. */
.branch-track {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-xs);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  flex: none;
}

.branch-track-behind {
  color: var(--accent-color);
}

.branch-track-ahead {
  color: var(--text-muted);
}

/* The numbers are buttons now (jump to the commits they count), so strip the
   native button chrome and keep the text exactly as it was. */
.branch-track-btn {
  background: none;
  border: 0;
  padding: 0;
  margin: 0;
  font: inherit;
  color: inherit;
  cursor: pointer;
  border-radius: var(--radius-xs);
}

.branch-track-btn:hover {
  text-decoration: underline;
}

.branch-track-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 2px;
}

/* Commits the upstream has and this branch does not. A notice strip above the
   history, not part of it: the list below is walked from this branch's own
   commit and its graph would contradict foreign rows. */
.remote-commits {
  flex: none;
  border-bottom: 1px solid var(--border-color);
  background: var(--bg-secondary);
}

.remote-commits-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: calc(6 * var(--dp)) calc(14 * var(--dp));
  background: none;
  border: 0;
  color: var(--text-secondary);
  font: inherit;
  font-size: var(--font-size-xs);
  text-align: left;
  cursor: pointer;
}

.remote-commits-icon {
  flex: none;
  color: var(--accent-color);
}

.remote-commits-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.remote-commits-chevron {
  flex: none;
  transition: transform var(--duration-fast, 0.15s) ease;
}

.remote-commits.is-open .remote-commits-chevron {
  transform: rotate(180deg);
}

.remote-commits-list {
  /* Taller than before: each entry is now two lines. Roughly five entries stay
     visible before the list scrolls. */
  max-height: calc(260 * var(--dp));
  overflow-y: auto;
  border-top: 1px solid var(--border-color);
}

.remote-commit {
  display: flex;
  flex-direction: column;
  gap: calc(3 * var(--dp));
  padding: calc(7 * var(--dp)) calc(14 * var(--dp));
  font-size: var(--font-size-xs);
  cursor: pointer;
}

.remote-commit:hover {
  background: var(--bg-tertiary);
}

.remote-commit-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
}

/* The tag is fixed-width furniture; only the subject may shrink. */
.remote-commit-head .git-ref-tag {
  flex: none;
}

/* Up to two lines: commit subjects are prose, and a single clipped line made
   every entry look identical. */
.remote-commit-msg {
  flex: 1;
  min-width: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
  line-height: 1.45;
}

.remote-commit-sha {
  flex: none;
  font-family: var(--font-mono, monospace);
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
}

.remote-commit-meta {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex: none;
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
}

.remote-commits-more {
  padding: calc(5 * var(--dp)) calc(14 * var(--dp));
  font-size: var(--font-size-2xs);
  color: var(--text-muted);
}

/* Flash the row we just jumped to. A background tint rather than a border:
   the row already has hover/selected backgrounds, and a border would shift
   the text by a pixel. */
.drilldown-item.is-flash {
  animation: unpushed-flash 1.4s ease-out;
}

@keyframes unpushed-flash {
  0%,
  35% {
    background: color-mix(in srgb, var(--accent-color) 16%, transparent);
  }
  100% {
    background: transparent;
  }
}

/* The flash only reinforces where the jump landed — the scroll position already
   carries that information, so dropping the animation loses nothing. */
@media (prefers-reduced-motion: reduce) {
  .drilldown-item.is-flash {
    animation: none;
  }
}

/* Read-only strip: a warning about where the workspace is, not a toolbar.
   Only rendered while viewing another branch. */
.branch-alert {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  height: calc(28 * var(--dp));
  padding: 0 var(--space-5) 0 var(--space-6);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  /* Mixed from the warning hue rather than a fixed background so it works in
     all 36 themes (design-guide red line 4). */
  background: color-mix(in srgb, var(--color-yellow, #f59e0b) 10%, var(--bg-primary));
  box-shadow: inset calc(2 * var(--dp)) 0 0 var(--color-yellow, #f59e0b);
}

.branch-alert-icon {
  flex: none;
  color: var(--color-yellow, #a16207);
}

.branch-note {
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.branch-note--warn {
  /* The eye glyph is a sibling, not a child: the note itself must stay a
     plain text block so its ellipsis works (a flex container ignores
     text-overflow on its own text). */
  color: var(--color-yellow, #a16207);
}

.branch-back-btn {
  flex: none;
  height: calc(22 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid color-mix(in srgb, var(--color-yellow, #f59e0b) 40%, transparent);
  border-radius: var(--radius-full);
  background: var(--bg-primary);
  color: var(--color-yellow, #a16207);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
}

.branch-back-btn:hover {
  background: var(--bg-secondary);
}

.branch-menu {
  position: absolute;
  /* Anchored to the picker (the chip), not to the bar: the chip is now one of
     several items in the row, so a bar-relative offset would drift with it. */
  top: calc(100% + 6 * var(--dp));
  left: 0;
  z-index: 20;
  width: calc(258 * var(--dp));
  padding: var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  box-shadow: 0 calc(12 * var(--dp)) calc(28 * var(--dp)) rgba(0, 0, 0, 0.16);
}

.branch-menu-label {
  padding: var(--space-3) var(--space-4) var(--space-1);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
  letter-spacing: 0.04em;
  color: var(--text-muted);
}

.branch-menu-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  text-align: left;
  cursor: pointer;
}

.branch-menu-item:hover {
  background: var(--bg-secondary);
}

.branch-menu-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.branch-menu-tag {
  flex: none;
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  background: var(--bg-tertiary);
  color: var(--text-muted);
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-bold);
}

.branch-menu-check {
  flex: none;
  color: var(--accent-color);
}

.branch-menu-sep {
  height: 1px;
  margin: var(--space-2) var(--space-3);
  background: var(--border-color);
}

/* Borderless field: SearchInput's shared defaults (--bg-primary fill + 1px
   --border-color outline) read as a nested box on the header bar. The fill is
   mixed from --text-primary instead of jumping to --bg-tertiary: the header is
   transparent on the page's --bg-primary, and across the 36 themes
   secondary↔tertiary contrast can be as low as 1.06, which would make the
   field disappear (design-guide red line 4). */
.commit-search :deep(.search-pill) {
  border: none;
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
}

/* Focus is carried by the ring alone — the shared rule would also tint the
   border, which no longer exists here. */
.commit-search :deep(.search-pill.focused) {
  border-color: transparent;
  box-shadow: 0 0 0 calc(2 * var(--dp)) color-mix(in srgb, var(--accent-color) 35%, transparent);
}

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
  padding: var(--space-5) calc(14 * var(--dp));
  cursor: pointer;
  transition: background var(--duration-base);
  border-bottom: 1px solid var(--border-color, #dee2e6);
  height: calc(64 * var(--dp));
  box-sizing: border-box;
  overflow: hidden;
}

@media (hover: hover) {
  .drilldown-item:hover {
    background: var(--bg-secondary, #f8f9fa);
  }
}

.drilldown-item:active {
  background: var(--bg-tertiary, #e9ecef);
}

.git-commit-info {
  flex: 1;
  min-width: 0;
}

/* Row-level "quote into a conversation" action. Hidden until the row is hovered
   so the list keeps its density; on touch there is no hover, so it is always
   visible — an invisible button simply would not exist there. */
.commit-quote-btn {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: calc(24 * var(--dp));
  height: calc(24 * var(--dp));
  margin-left: var(--space-3);
  padding: 0;
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  opacity: 0;
  transition: opacity var(--duration-base), color var(--duration-base), background var(--duration-base);
}

.drilldown-item:hover .commit-quote-btn,
.commit-quote-btn:focus-visible {
  opacity: 1;
}

@media (hover: none) {
  .commit-quote-btn { opacity: 1; }
}

@media (hover: hover) {
  .commit-quote-btn:hover {
    color: var(--accent-color, #4a90d9);
    background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
  }
}

.git-commit-msg {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  color: inherit;
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

/* The clamp lives on the text span, not the flex row, so the working-tree
   file-count badge stays a sibling instead of being swallowed by the clamp. */
.git-commit-msg-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

/* Working-tree row: how many files the workspace has changed. Shape comes from
   the shared .count-badge — only colour and weight belong here. */
.git-commit-file-count {
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 15%, transparent);
  color: var(--accent-color, #4a90d9);
  font-weight: var(--font-weight-bold);
}

.git-commit-meta {
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  margin-top: var(--space-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.git-load-more-sentinel {
  min-height: 1px;
}

.git-load-more {
  padding: var(--space-8) calc(14 * var(--dp));
  display: flex;
  justify-content: center;
  min-height: calc(60 * var(--dp));
}

.git-history-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* A skeleton must fill the body and align with the real rows; centring it
   would shrink it to its content and float a narrow column in the middle. */
.git-history-loading--skeleton {
  align-items: stretch;
  justify-content: flex-start;
  min-width: 0;
  /* 14dp is `.drilldown-item`'s inset — the rows this placeholder previews. */
  --panel-skeleton-inset: calc(14 * var(--dp));
}

.git-history-error,
.git-history-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-muted, #999);
    font-size: var(--font-size-lg);
}

.empty-state-card {
    display: flex;
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: calc(32 * var(--dp)) calc(24 * var(--dp));
    gap: var(--space-4);
}

.empty-state-title {
    font-size: var(--font-size-lg);
    font-weight: var(--font-weight-medium);
    color: var(--text-primary);
}

.empty-state-desc {
    font-size: var(--font-size-md);
    color: var(--text-muted);
}

.empty-state-hint {
    font-size: var(--font-size-sm);
    color: var(--text-muted);
    margin-top: var(--space-2);
}

.empty-state-hint code {
    background: var(--bg-tertiary);
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-xs);
    font-family: var(--font-mono);
    font-size: var(--font-size-xs);
}

/* Selected commit highlight */
.drilldown-item-selected {
  background: rgba(74, 144, 217, 0.08);
  border-left: 3px solid var(--accent-color, #4a90d9);
  padding-left: calc(11 * var(--dp));
}

/* Keyboard-navigation highlight */
.drilldown-item-active {
  background: var(--bg-secondary, #f8f9fa);
  border-radius: 0;
}

/* Search graph hint */
.commit-list-graph-hint {
  width: calc(24 * var(--dp));
  flex-shrink: 0;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding-top: var(--space-7);
  color: var(--text-muted, #ccc);
}

/* Short SHA tag */
.git-commit-sha {
  font-family: var(--font-mono);
  font-size: var(--font-size-2xs);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 1px var(--space-2);
  border-radius: var(--radius-xs);
  margin-right: var(--space-2);
}

/* Ref tags */
.git-commit-refs {
  display: inline-flex;
  gap: calc(3 * var(--dp));
  margin-right: var(--space-2);
}
.git-ref-tag {
  font-size: var(--font-size-2xs);
  font-weight: var(--font-weight-semibold);
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
  white-space: nowrap;
}
.ref-head { background: #1a1a2e; color: #fff; }
.ref-branch { background: rgba(74, 144, 217, 0.15); color: #4a90d9; }
.ref-tag { background: rgba(85, 85, 85, 0.15); color: #666; }

/* Outlined rather than filled, so "unpushed" reads as a state of the commit
   instead of another ref pointing at it. */
.git-ref-tag--unpushed {
  background: transparent;
  color: var(--text-secondary);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--text-muted) 45%, transparent);
}

.git-ref-tag--remote {
  background: color-mix(in srgb, var(--accent-color) 15%, transparent);
  color: var(--accent-color);
}

</style>
