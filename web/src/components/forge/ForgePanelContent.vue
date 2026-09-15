<template>
  <div class="forge-panel">
    <!-- Unbound / no-project states use a question-card layout (same visual
         language as the AskUserQuestion card): one prompt plus actionable
         options. -->
    <div v-if="!projectPath" class="forge-state">
      <div class="forge-card">
        <div class="forge-card-icon">
          <Github :size="30" />
        </div>
        <div class="forge-card-header">{{ t('forge.empty.noProjectHeader') }}</div>
        <div class="forge-card-body">{{ t('forge.empty.noProjectBody') }}</div>
        <div class="forge-card-options">
          <button class="fbtn fbtn-primary" @click="emit('request-project')">
            {{ t('forge.empty.chooseProject') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Fallback only. The backend auto-binds the highest-priority git remote
         when it points at github.com / gitlab.com, so this card appears only
         when nothing could be bound automatically: no usable remote, a
         self-hosted host that needs explicit confirmation, or the user having
         unbound the repository earlier. -->
    <div v-else-if="!items.isBound.value && !items.loading.value" class="forge-state">
      <div class="forge-card">
        <div class="forge-card-icon">
          <Github :size="30" />
        </div>
        <div class="forge-card-header">{{ t('forge.empty.noBindingHeader') }}</div>
        <div class="forge-card-body">{{ t('forge.empty.noBindingBody') }}</div>
        <div class="forge-card-options">
          <button class="fbtn fbtn-primary" @click="openBindDialog">
            {{ t('forge.empty.bindRepo') }}
          </button>
        </div>
      </div>
    </div>

    <template v-else>
      <!-- Detail view replaces the list in place (currentView pattern).
           A pipeline gets its own component: the issue/PR detail is
           title + markdown body + comment thread, none of which a CI run has —
           its shape is metadata + a job table. -->
      <ForgePipelineDetail
        v-if="detailOpen && activeTab === 'pipeline'"
        :run-id="pipelineDetailId"
        @back="closeDetail"
        @quote="onPipelineQuote"
        @open-pr="onOpenLinkedPr"
      />

      <ForgeDetail
        v-else-if="detailOpen"
        :type="items.type.value"
        :number="detailNumber"
        @back="closeDetail"
        @quote="onQuote"
        @open-pipeline="onOpenItemPipeline"
      />

      <template v-else>
        <!-- Standard panel header: matches every other list page
             (var(--header-height), bg-primary, bottom border). The repository
             badge is clickable — same "context badge + dropdown" affordance as
             the app header's project/branch switchers — and is the only place
             to change or clear the binding once one exists. -->
        <div class="forge-header">
          <span class="forge-header-title">
            <Github :size="14" />
            <button
              v-if="items.binding.value"
              ref="repoBadgeRef"
              class="forge-repo-badge"
              :title="items.binding.value.slug"
              @click.stop="repoMenuOpen = !repoMenuOpen"
            >
              <span class="forge-repo-name">{{ items.binding.value.slug }}</span>
              <ChevronDown :size="12" />
            </button>
            <span v-else>{{ t('nav.forge') }}</span>
          </span>
          <button
            class="forge-header-btn clear-unread-btn"
            :class="{ active: forgeUnreadCount > 0 }"
            :disabled="forgeUnreadCount === 0"
            :title="t('forge.markAllRead')"
            :aria-label="t('forge.markAllRead')"
            @click="markAllRead"
          >
            <CheckCheck :size="14" />
          </button>
          <RefreshButton
            class="forge-header-btn"
            :loading="items.loading.value"
            :title="t('nav.refresh')"
            @click="onRefreshClick"
          />
        </div>

        <PopupMenu v-model:show="repoMenuOpen" :target-element="repoBadgeRef" :menu-items-count="2">
          <button class="forge-repo-menu-item" @click="openRebindDialog">
            <Github :size="14" />
            <span>{{ t('forge.bind.change') }}</span>
          </button>
          <button class="forge-repo-menu-item danger" @click="unbindRepo">
            <Unlink :size="14" />
            <span>{{ t('forge.bind.unbind') }}</span>
          </button>
        </PopupMenu>

        <!-- Type switch: page tabs, matching the stats panel tab bar
             (connected rectangular tabs with a bottom accent underline).
             State/mine below are independent filters, so they stay as chips.
             Pipelines is a peer tab: CI runs are neither issues nor PRs, and
             they need their own filters (a run has no open/closed state and no
             assignee). -->
        <div class="forge-tabs">
          <button
            v-for="tab in forgeTabs"
            :key="tab.key"
            class="forge-tab"
            :class="{ active: activeTab === tab.key }"
            @click="setActiveTab(tab.key)"
          >
            <component :is="tab.icon" :size="13" />
            <span class="forge-tab-label">{{ t(tab.labelKey) }}</span>
          </button>
        </div>

        <!-- Unread: every item with new activity, across all three categories.
             `active` is a composite: the dock tab must be showing AND this
             internal tab selected, or switching to Issues would leave it
             fetching in the background. -->
        <template v-if="activeTab === 'overview'">
          <ForgeOverviewList
            ref="overviewListRef"
            :active="active && activeTab === 'overview'"
            :project-path="projectPath"
            @open-item="onOverviewOpenItem"
          />
        </template>

        <!-- Pipelines: a repository-level view with its own filters and list. -->
        <template v-else-if="activeTab === 'pipeline'">
          <div class="forge-toolbar">
            <div class="forge-chips forge-chips-scroll">
              <button
                v-for="f in FORGE_PIPELINE_FILTERS"
                :key="f"
                class="forge-chip"
                :class="{ active: pipelines.filter.value === f }"
                @click="pipelines.setFilter(f)"
              >{{ t(`forge.pipeline.filter.${f}`) }}</button>
            </div>
          </div>

          <div v-if="pipelines.error.value" class="forge-error-card">
            <AlertCircle :size="18" class="forge-error-icon" />
            <div class="forge-error-text">
              <div class="forge-error-title">{{ pipelineErrorTitle(pipelines.error.value.code) }}</div>
              <div class="forge-error-body">{{ pipelines.error.value.message }}</div>
            </div>
            <button class="fbtn" @click="onRefreshClick">{{ t('forge.retry') }}</button>
          </div>

          <div v-else-if="pipelines.loading.value" class="forge-loading">
            <LoadingIndicator size="md" :label="t('forge.loading')" />
          </div>

          <div v-else-if="pipelines.pipelines.value.length === 0" class="forge-state">
            <div class="forge-empty-card">
              <component
                :is="tabIcon('pipeline')"
                :size="34"
                :stroke-width="1.5"
                class="forge-empty-icon"
              />
              <div class="forge-empty-title">{{ t('forge.pipeline.emptyList') }}</div>
            </div>
          </div>

          <div v-else class="forge-list" @scroll="onPipelineScroll">
            <div
              v-for="run in pipelines.pipelines.value"
              :key="run.id"
              class="forge-row forge-pipeline-row"
              :class="{ unread: run.unread }"
              @click="openPipelineDetail(run)"
            >
              <span class="forge-state-dot" :class="`pipeline-${run.status}`"></span>
              <div class="forge-row-main">
                <div class="forge-row-title">
                  <span class="forge-row-number">#{{ run.number }}</span>
                  <span class="forge-row-text">{{ run.name }}</span>
                  <span
                    v-if="run.unread"
                    class="forge-unread-dot"
                    :title="t('forge.unreadItem')"
                    :aria-label="t('forge.unreadItem')"
                  ></span>
                </div>
                <div class="forge-row-meta">
                  <span class="forge-pipeline-ref">{{ run.ref }}</span>
                  <span v-if="run.sha" class="forge-pipeline-sha">{{ shortSha(run.sha) }}</span>
                  <span v-if="run.event" class="forge-pipeline-event">{{ run.event }}</span>
                  <span v-if="run.actor" class="forge-row-author">{{ run.actor }}</span>
                  <span class="forge-row-time">{{ formatTime(run.updatedAt) }}</span>
                </div>
              </div>
              <ChevronRight :size="16" class="forge-row-chevron" />
            </div>
            <div v-if="pipelines.loadingMore.value" class="forge-loading-more">
              <LoadingIndicator size="sm" />
            </div>
          </div>
        </template>

        <template v-else>
        <div class="forge-toolbar">
          <div class="forge-chips forge-chips-scroll">
            <button
              v-for="s in stateOptions"
              :key="s"
              class="forge-chip"
              :class="{ active: items.state.value === s }"
              @click="items.setState(s)"
            >{{ t(`forge.state.${s}`) }}</button>
            <span class="forge-chips-divider"></span>
            <button
              v-for="f in mineOptions"
              :key="f"
              class="forge-chip"
              :class="{ active: items.mineFilter.value === f }"
              @click="items.setMineFilter(f)"
            >{{ t(`forge.mine.${f}`) }}</button>
          </div>
        </div>

        <!-- Search: uses the shared search-pill affordance (icon + clear). -->
        <div class="forge-search">
          <SearchInput
            v-model="searchInput"
            :placeholder="t('forge.searchPlaceholder')"
            @enter="items.setQuery(searchInput)"
          />
        </div>

        <!-- Error card: distinguishes auth / rate-limit / network so the user
             knows what to do. -->
        <div v-if="items.error.value" class="forge-error-card">
          <AlertCircle :size="18" class="forge-error-icon" />
          <div class="forge-error-text">
            <div class="forge-error-title">{{ errorTitle(items.error.value.code) }}</div>
            <div class="forge-error-body">{{ items.error.value.message }}</div>
          </div>
          <button class="fbtn" @click="onRefreshClick">{{ t('forge.retry') }}</button>
        </div>

        <div v-else-if="items.loading.value" class="forge-loading">
          <LoadingIndicator size="md" :label="t('forge.loading')" />
        </div>

        <div v-else-if="items.items.value.length === 0" class="forge-state">
          <div class="forge-empty-card">
            <component
              :is="tabIcon(items.type.value)"
              :size="34"
              :stroke-width="1.5"
              class="forge-empty-icon"
            />
            <div class="forge-empty-title">{{ t('forge.emptyList') }}</div>
          </div>
        </div>

        <div v-else class="forge-list" @scroll="onListScroll">
          <div
            v-for="it in items.items.value"
            :key="`${it.type}-${it.number}`"
            class="forge-row"
            :class="{ unread: it.unread }"
            @click="openDetail(it)"
          >
            <span class="forge-state-dot" :class="`state-${it.state}`"></span>
            <div class="forge-row-main">
              <div class="forge-row-title">
                <span class="forge-row-number">#{{ it.number }}</span>
                <span class="forge-row-text">{{ it.title }}</span>
                <span
                  v-if="it.unread"
                  class="forge-unread-dot"
                  :title="t('forge.unreadItem')"
                  :aria-label="t('forge.unreadItem')"
                ></span>
              </div>
              <div class="forge-row-meta">
                <span class="forge-row-author">{{ it.author }}</span>
                <span class="forge-row-time">{{ formatTime(it.updatedAt) }}</span>
                <span v-if="it.commentCount" class="forge-row-comments">
                  <MessageSquare :size="11" />
                  {{ it.commentCount }}
                </span>
              </div>
            </div>
            <ChevronRight :size="16" class="forge-row-chevron" />
          </div>
          <div v-if="items.loadingMore.value" class="forge-loading-more">
            <LoadingIndicator size="sm" />
          </div>
        </div>
        </template>
      </template>
    </template>

    <!-- Binding dialog. ModalDialog renders on its own `everOpened` latch, which
         only flips inside its props.open watcher — so `open` MUST be passed. A
         bare v-if would leave the dialog permanently unrendered. -->
    <ModalDialog :open="bindDialogOpen" :title="t('forge.bind.title')" @close="bindDialogOpen = false">
      <div class="forge-bind-form">
        <div v-if="remotes.length" class="forge-bind-section">
          <div class="forge-bind-label">{{ t('forge.bind.fromRemote') }}</div>
          <div class="forge-bind-remotes">
            <button
              v-for="r in remotes"
              :key="r.name + r.url"
              class="forge-remote-row"
              :disabled="!r.slug"
              :title="r.slug || r.url"
              @click="bindFromRemote(r)"
            >
              <Github :size="15" class="forge-remote-icon" />
              <span class="forge-remote-text">
                <span class="forge-remote-name">
                  {{ r.name }}
                  <!-- The remote's own scheme, when it states one. An ssh remote
                       states none, so nothing is shown rather than guessing
                       https: the server resolves that case from the credential's
                       recorded hint. -->
                  <span v-if="r.scheme" class="forge-remote-scheme">{{ r.scheme }}</span>
                </span>
                <span class="forge-remote-url">{{ r.slug || r.url }}</span>
                <!-- Rows bind on click, so this hint is the only pre-submit
                     signal on this path. -->
                <span v-if="r.host && !isOfficialForgeHost(r.host)" class="forge-remote-warning">
                  {{ t('forge.bind.nonOfficialHost') }}
                </span>
              </span>
              <ChevronRight :size="15" class="forge-remote-chevron" />
            </button>
          </div>
        </div>

        <div v-if="remotes.length" class="forge-bind-divider" />

        <div class="forge-bind-section">
          <div class="forge-bind-label">{{ t('forge.bind.manual') }}</div>
          <input
            v-model="manualUrl"
            class="forge-input"
            :placeholder="t('forge.bind.urlPlaceholder')"
            @keyup.enter="manualUrl && bindFromUrl()"
          />
          <!-- Warn before submit, not after: the server accepts any host, so a
               self-hosted instance would otherwise bind with no indication that
               it will be sent the credential. -->
          <div v-if="manualUrlNonOfficial" class="forge-bind-warning">
            <AlertTriangle :size="14" />
            <span>{{ t('forge.bind.nonOfficialHost') }}</span>
          </div>
        </div>

        <div v-if="bindError" class="forge-bind-error">
          <AlertCircle :size="14" />
          <span>{{ bindError }}</span>
        </div>
      </div>
      <template #footer>
        <button class="fbtn" @click="bindDialogOpen = false">{{ t('common.cancel') }}</button>
        <button class="fbtn fbtn-primary" :disabled="!manualUrl" @click="bindFromUrl">
          {{ t('forge.bind.submit') }}
        </button>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Github, Rss, MessageSquare, CircleQuestionMark, GitPullRequest, Activity,
  ChevronRight, ChevronDown, AlertCircle, AlertTriangle, Unlink, CheckCheck,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import ForgeDetail from '@/components/forge/ForgeDetail.vue'
import ForgePipelineDetail from '@/components/forge/ForgePipelineDetail.vue'
import ForgeOverviewList from '@/components/forge/ForgeOverviewList.vue'
import { useForgeItems, useForgePipelines, FORGE_PIPELINE_FILTERS } from '@/composables/useForge'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { fetchForgeRemotes, setForgeBinding, deleteForgeBinding, type ForgeRemote, type ForgePipelineRun, type ForgeItem, ForgeApiError } from '@/utils/forgeApi'
import { isOfficialForgeHost, isNonOfficialRemote } from '@/utils/forgeHost'
import { useForgeUnread } from '@/composables/useForgeUnread'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgePanel'
const props = defineProps<{
  active: boolean
  projectPath: string
}>()
const emit = defineEmits<{
  (e: 'request-project'): void
  (e: 'quote', payload: { item: { type: 'issue' | 'pr' | 'pipeline'; number: number; title: string; url: string; slug: string; label?: string } }): void
}>()

const { t } = useI18n()
const items = useForgeItems(() => props.projectPath)
const pipelines = useForgePipelines(() => props.projectPath)
// The same count the dock badge shows, so the "mark all read" button reflects
// the badge rather than a separately-tracked number that could disagree.
const { forgeUnreadCount } = useForgeUnread()

/**
 * The three peer tabs of the forge panel. Declared as data rather than three
 * hand-written buttons so adding a view cannot leave the tab bar and the
 * rendered branch out of step.
 */
const forgeTabs = [
  // First because it answers "what changed?" — the reason to open the panel —
  // and because it spans all three of the category tabs that follow.
  { key: 'overview' as const, labelKey: 'forge.overview.title', icon: Rss },
  { key: 'issue' as const, labelKey: 'forge.type.issues', icon: CircleQuestionMark },
  { key: 'pr' as const, labelKey: 'forge.type.prs', icon: GitPullRequest },
  { key: 'pipeline' as const, labelKey: 'forge.type.pipelines', icon: Activity },
]
type ForgeTabKey = typeof forgeTabs[number]['key']

/**
 * The tab's own glyph, for reuse in that tab's empty state.
 *
 * Derived from the registry rather than repeated, so an empty state cannot drift
 * from the tab it belongs to. `pipeline` is keyed explicitly because the issue
 * and PR lists share one empty state and it must not inherit the PR glyph for
 * issues.
 */
function tabIcon(key: ForgeTabKey) {
  return forgeTabs.find(t => t.key === key)?.icon
}

const activeTab = ref<ForgeTabKey>('overview')

/**
 * State chips for the current item type.
 *
 * "merged" is offered only for change requests: GitLab issues have no merged
 * lifecycle and its API rejects state=merged on the issues endpoint, and GitHub
 * issues have no merge concept at all. Showing the chip on the issues tab would
 * present a filter that is always empty.
 */
const stateOptions = computed<Array<'open' | 'closed' | 'merged' | 'all'>>(() =>
  items.type.value === 'pr'
    ? ['open', 'closed', 'merged', 'all']
    : ['open', 'closed', 'all'],
)
const mineOptions: Array<'all' | 'assigned' | 'created' | 'review'> = ['all', 'assigned', 'created', 'review']

const searchInput = ref('')
const detailOpen = ref(false)
const detailNumber = ref(0)
const pipelineDetailId = ref(0)
const bindDialogOpen = ref(false)
const remotes = ref<ForgeRemote[]>([])
const manualUrl = ref('')
const bindError = ref('')

/**
 * Whether the typed URL points at a host the server will not vouch for.
 *
 * Derived rather than stored so the warning tracks the input live. Unparseable
 * input is not flagged here — submitting it produces the backend's own
 * invalid-URL error, which is the more accurate message.
 */
const manualUrlNonOfficial = computed(() => isNonOfficialRemote(manualUrl.value))
const repoMenuOpen = ref(false)
const repoBadgeRef = ref<HTMLElement | null>(null)

/** Switch tabs, loading the target view's data on first use. */
function setActiveTab(key: ForgeTabKey) {
  if (activeTab.value === key) return
  closeDetail()
  activeTab.value = key
  if (key === 'pipeline') {
    void pipelines.load()
  } else if (key === 'overview') {
    // Nothing to load here: the list remounts (its branch was just un-hidden)
    // and its own immediate watcher fetches on first activation. Calling
    // reload() now would be a no-op — the template ref is still null until the
    // DOM updates. Critically, it must NOT reach items.setType, which only
    // accepts an item type.
  } else {
    // The issue/PR list shares one composable; changing the type reloads it.
    items.setType(key)
  }
}

/**
 * Refresh the binding and the active tab's list.
 *
 * `force` bypasses the shared binding cache. Post-write callers (bind/unbind)
 * must pass it: the server state is known to have changed, and a cached value
 * would show the repository the user just switched away from.
 */
async function refresh(force = false) {
  await items.loadBinding(force)
  if (!items.isBound.value) return
  // Only the visible tab has data worth reloading; the others load on switch.
  // The unread tab has its own composable, so it must be named explicitly —
  // otherwise the header button would reload an invisible list and appear dead.
  if (activeTab.value === 'pipeline') await pipelines.load()
  else if (activeTab.value === 'overview') overviewListRef.value?.reload()
  else await items.load()
}

/**
 * Click handler for the refresh affordances.
 *
 * Exists so a DOM click cannot pass its MouseEvent into `refresh(force)` — a
 * truthy event would bypass the binding cache on every click.
 */
function onRefreshClick() {
  void refresh()
}

onMounted(refresh)

// Reload when the project changes or the panel becomes active.
watch(() => props.projectPath, () => {
  closeDetail()
  void refresh()
})
watch(() => props.active, (isActive) => {
  if (!isActive || !items.isBound.value) return
  if (activeTab.value === 'pipeline') {
    if (pipelines.pipelines.value.length === 0 && !pipelines.loading.value) void pipelines.load()
    return
  }
  if (items.items.value.length === 0 && !items.loading.value) void items.load()
})

function openDetail(it: ForgeItem) {
  // Opening the row is what marks it read. Deliberately not awaited: the view
  // opens immediately and the badge settles in the background.
  void items.markItemRead(it)
  items.type.value = it.type
  detailNumber.value = it.number
  detailOpen.value = true
}

function openPipelineDetail(run: ForgePipelineRun) {
  void pipelines.markItemRead(run)
  pipelineDetailId.value = run.id
  detailOpen.value = true
}

/** Mark every unread item in this repository read. */
function markAllRead() {
  // All three lists share one repo-level read state, so clear whichever is
  // loaded. The others re-read on their next load and come back already read.
  void items.markAllRead()
  void pipelines.markAllRead()
  // Without this the badge would drop to zero while the unread rows still showed
  // their dots — the list and the badge disagreeing, which is the bug this
  // feature exists to fix. No request: the writes above already covered it.
  overviewListRef.value?.clearLocal()
}
function closeDetail() {
  detailOpen.value = false
  detailNumber.value = 0
  pipelineDetailId.value = 0
}

/** The unread list, so the header's "mark all read" can clear its rows too. */
const overviewListRef = ref<{ reload: () => void; clearLocal: () => void } | null>(null)

/**
 * Open the item an unread row points at.
 *
 * In-component, so no cross-component seam is needed: the detail refs are right
 * here. Read state is NOT handled here — the list already marked the row read
 * server-side using the opaque itemKey, which is the only form that works for a
 * pipeline (its number is 0, so a rebuilt key would be "pipeline/0").
 */
function onOverviewOpenItem(payload: {
  type: 'issue' | 'pr' | 'pipeline'
  number: number
  runId: number
}) {
  if (payload.type === 'pipeline') {
    activeTab.value = 'pipeline'
    detailNumber.value = 0
    pipelineDetailId.value = payload.runId
    // Load the list behind the detail. Without this, closing the detail lands on
    // the Pipelines tab with nothing in it (its own loader only runs on tab
    // switch or on activation, neither of which happened).
    void pipelines.load()
  } else {
    activeTab.value = payload.type
    // Keep the item list's own type in sync so closing the detail shows the
    // matching list rather than the previously-viewed category.
    items.type.value = payload.type
    pipelineDetailId.value = 0
    detailNumber.value = payload.number
    void items.load()
  }
  detailOpen.value = true
}

/**
 * Open a change request linked from a pipeline run.
 *
 * Reuses the issue/PR detail path rather than opening the browser: the linked PR
 * is a first-class item in this panel, so landing in its detail (with comments
 * and the quote action) is the useful destination. The run's own "open in
 * browser" button remains available for the platform page.
 *
 * `items.type` is set directly rather than via `setType`, which would kick off
 * its own load for the wrong list; `items.load()` below fetches the PR list the
 * detail will return to.
 */
function onOpenLinkedPr(number: number) {
  activeTab.value = 'pr'
  items.type.value = 'pr'
  pipelineDetailId.value = 0
  detailNumber.value = number
  void items.load()
}

/**
 * Open one of a change request's CI runs, from the PR detail's CI section.
 *
 * The reverse of onOpenLinkedPr: switch to the Pipelines tab and point the
 * pipeline detail at that run. The pipeline list is loaded so closing the detail
 * lands on a populated list rather than an empty one (the same reason
 * onOverviewOpenItem loads it).
 */
function onOpenItemPipeline(runId: number) {
  activeTab.value = 'pipeline'
  detailNumber.value = 0
  pipelineDetailId.value = runId
  void pipelines.load()
}

// Register the drill-down back handler so the edge-swipe gesture and the Android
// hardware back button close the detail view (same contract as tasks/git).
// Gated on `active` so an inactive tab never intercepts a back press.
useFeatureBackHandler(
  'forge',
  () => props.active && detailOpen.value,
  () => closeDetail(),
  PRIORITY_PAGE,
)

function onListScroll(e: Event) {
  const el = e.target as HTMLElement
  if (!el) return
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 120) {
    void items.loadMore()
  }
}

function onPipelineScroll(e: Event) {
  const el = e.target as HTMLElement
  if (!el) return
  if (el.scrollTop + el.clientHeight >= el.scrollHeight - 120) {
    void pipelines.loadMore()
  }
}

/** First 7 characters of a commit hash, the conventional short form. */
function shortSha(sha: string): string {
  return sha.slice(0, 7)
}

/**
 * A platform without CI support is not an error to retry — it is a fact about
 * the platform, so it gets its own wording instead of the generic error title.
 */
function pipelineErrorTitle(code: string): string {
  if (code === 'ForgeNoPipelines') return t('forge.pipeline.noPlatform')
  return errorTitle(code)
}

async function openBindDialog() {
  bindDialogOpen.value = true
  bindError.value = ''
  try {
    const res = await fetchForgeRemotes()
    remotes.value = res.remotes ?? []
  } catch (err) {
    appLog.w(TAG, 'load remotes failed', err)
    remotes.value = []
  }
}

/** Header badge → "change repository": reopen the binding dialog. */
function openRebindDialog() {
  repoMenuOpen.value = false
  void openBindDialog()
}

/** Header badge → "unbind": clear the binding, then fall back to the unbound card. */
async function unbindRepo() {
  repoMenuOpen.value = false
  try {
    await deleteForgeBinding()
  } catch (err) {
    appLog.w(TAG, 'unbind failed', err)
  }
  closeDetail()
  await refresh(true)
}

async function bindFromRemote(r: ForgeRemote) {
  if (!r.slug) return
  // The remote's own scheme is forwarded so the binding persists how the
  // instance is actually reached. It is absent for an ssh remote, which is not
  // the same as https — the server resolves that from the credential hint.
  await submitBinding({
    platform: r.platform,
    host: r.host,
    scheme: r.scheme,
    owner: r.owner,
    repo: r.repo,
  })
}

async function bindFromUrl() {
  await submitBinding({ url: manualUrl.value })
}

async function submitBinding(input: {
  url?: string
  platform?: string
  host?: string
  scheme?: string
  owner?: string
  repo?: string
}) {
  bindError.value = ''
  try {
    await setForgeBinding(input)
    bindDialogOpen.value = false
    manualUrl.value = ''
    // The binding just changed server-side, so bypass the cache.
    await refresh(true)
  } catch (err) {
    bindError.value = err instanceof ForgeApiError ? err.message : String(err)
  }
}

function onQuote(payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string } }) {
  emit('quote', payload)
}

/**
 * Quote a whole CI run into the chat.
 *
 * The payload carries an explicit `label`, because the generic fallback
 * (`slug#number`) renders a run as `owner/repo#42` — indistinguishable from a
 * pull request number. The label names the run instead.
 */
function onPipelineQuote(run: ForgePipelineRun) {
  emit('quote', {
    item: {
      type: 'pipeline',
      number: run.number,
      title: run.name,
      url: run.url,
      slug: run.slug,
      label: `${run.slug} ${t('forge.type.pipelines')} #${run.number}`,
    },
  })
}

function errorTitle(code: string): string {
  switch (code) {
    case 'ForgeAuthFailed': return t('forge.error.auth')
    case 'ForgeRateLimited': return t('forge.error.rateLimit')
    case 'ForgeNetworkError': return t('forge.error.network')
    // A not-found on a host with no token: the server cannot tell "private" from
    // "nonexistent" (private repos answer 404), so the title names the likelier
    // fix instead of letting the user hunt for a typo in the repository path.
    case 'ForgeNoCredential': return t('forge.error.noCredential')
    case 'NoForgeBinding': return t('forge.empty.noBindingHeader')
    default: return t('forge.error.generic')
  }
}

function formatTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleDateString()
}
</script>

<style scoped>
.forge-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary);
}

/* ── Panel header — matches every other list page header ── */
.forge-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding:0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color);
  gap: var(--space-3);
}
.forge-header-title {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1;
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}

/* Clickable repository badge — the context switcher for the bound remote,
   matching the app header's project/branch badges. */
.forge-repo-badge {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  padding: 3px 7px;
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
}
.forge-repo-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
@media (hover: hover) {
  .forge-repo-badge:hover {
    background: var(--bg-tertiary);
  }
}
.forge-repo-menu-item {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-4) var(--space-6);
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: var(--font-size-md);
  text-align: left;
  cursor: pointer;
}
@media (hover: hover) {
  .forge-repo-menu-item:hover {
    background: var(--bg-secondary);
  }
}
.forge-repo-menu-item.danger {
  color: var(--color-red);
}
/* Header icon button — same 28px round treatment as .header-btn elsewhere */
.forge-header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: background var(--duration-slow) ease, color var(--duration-slow) ease;
}
@media (hover: hover) {
  .forge-header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary);
    color: var(--accent-color);
  }
}

/* ── Empty / unbound state cards ── */
.forge-card {
  max-width: 340px;
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: var(--space-4);
}
.forge-card-icon {
  color: var(--text-muted);
  opacity: var(--opacity-muted);
  margin-bottom: var(--space-1);
}
.forge-card-header {
  font-size: var(--font-size-xl);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
.forge-card-body {
  color: var(--text-muted);
  font-size: var(--font-size-md);
  line-height: var(--line-height-normal);
}
.forge-card-options {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  margin-top: var(--space-5);
  width: 100%;
  align-items: center;
}

/* ── Type tabs — same treatment as the stats panel tab bar (connected
   rectangular tabs, active gets a tinted background + bottom accent line) ── */
.forge-tabs {
  display: flex;
  align-items: stretch;
  height: 34px;
  flex-shrink: 0;
  background: var(--bg-secondary);
  border-bottom: 1px solid var(--border-color);
}
.forge-tab {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  flex: 1;
  min-width: 0;
  padding:0 var(--space-7);
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  cursor: pointer;
  user-select: none;
  -webkit-tap-highlight-color: transparent;
  position: relative;
}
/* The tab bar is a fixed-height strip, so a label that does not fit must be
   ellipsised rather than wrapped — wrapping would clip it mid-line. The icon
   keeps its size and only the text shrinks. */
.forge-tab-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-tab > :deep(svg) {
  flex-shrink: 0;
}
@media (hover: hover) {
  .forge-tab:hover {
    background: var(--bg-tertiary);
    color: var(--text-primary);
  }
}
.forge-tab.active {
  color: var(--text-primary);
  background: color-mix(in srgb, var(--text-primary) 8%, transparent);
}
.forge-tab.active::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 2px;
  background: var(--accent-color);
}

/* Filter rows and chips are declared GLOBALLY (web/css/components.css): the
   activity tab renders its own toolbar from a child component, and a scoped rule
   only applies to the component that declares it. See the note there. */

/* ── Search ── */
.forge-search {
  padding: var(--space-4) var(--space-6);
  flex-shrink: 0;
}

/* ── Error card ──
   The card/loading chrome is shared (web/css/components.css). Only the margin
   differs here: the list card sits directly under the toolbar, so it wants less
   vertical breathing room than the detail page. */
.forge-error-card {
  margin: var(--space-4) var(--space-6);
}
.forge-error-title {
  color: var(--text-primary);
}


/* ── List ──
   .forge-row and .forge-row-text are declared GLOBALLY (web/css/components.css):
   the activity tab renders its own rows from a child component, so a scoped rule
   here would leave those rows with no flex layout, padding or ellipsis at all.
   Only the classes exclusive to this panel's two lists stay scoped. */
.forge-row-number {
  color: var(--text-muted);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.forge-row-comments {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.forge-loading-more {
  padding: var(--space-6);
  display: flex;
  justify-content: center;
}

/* ── Bind dialog ── */
.forge-bind-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
  /* ModalDialog's .modal-body ships with no padding of its own, so the content
     supplies it — without this the form sits flush against the card edges. */
  padding:14px var(--space-7) var(--space-7);
}
.forge-bind-section {
  display: flex;
  flex-direction: column;
}
.forge-bind-label {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-muted);
  margin-bottom: var(--space-4);
}
.forge-bind-remotes {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
/* A remote row reads as one tappable object: brand mark, a two-line
   name/url stack, and a chevron signalling it commits a choice. */
.forge-remote-row {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  width: 100%;
  padding:9px var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  cursor: pointer;
  transition: border-color var(--duration-base) ease, background var(--duration-base) ease;
  text-align: left;
}
@media (hover: hover) {
  .forge-remote-row:hover:not(:disabled) {
    border-color: var(--accent-color);
    background: var(--bg-secondary);
  }
  .forge-remote-row:hover:not(:disabled) .forge-remote-chevron {
    color: var(--accent-color);
  }
}
.forge-remote-row:active:not(:disabled) {
  background: var(--bg-tertiary);
}
.forge-remote-row:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}
.forge-remote-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}
.forge-remote-text {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 1;
  min-width: 0;
}
.forge-remote-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
}
/* The remote's own API scheme, shown only when it states one. Muted and
   monospace: it is reference information about how the instance is reached, not
   a status or a warning. */
.forge-remote-scheme {
  margin-left: var(--space-3);
  font-family: var(--font-mono);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-normal);
  color: var(--text-muted);
  padding: 0 var(--space-2);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-full);
}
.forge-remote-url {
  font-size: 11.5px;
  color: var(--text-muted);
  font-family: var(--font-mono);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* Non-official host hint inside a remote row. Amber, not red: binding is
   allowed, the user just has to know where the credential is going. */
.forge-remote-warning {
  font-size: 11px;
  color: var(--color-yellow);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.forge-remote-chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: color var(--duration-base) ease;
}
/* Separates the "pick a remote" shortcut from the manual URL fallback without
   another full-weight heading. */
.forge-bind-divider {
  height: 1px;
  background: var(--border-color);
  margin: -2px 0;
}
.forge-input {
  width: 100%;
  box-sizing: border-box;
  padding:9px var(--space-6);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: var(--font-size-md);
}
.forge-input::placeholder {
  color: var(--text-muted);
}
.forge-input:focus {
  outline: none;
  border-color: var(--accent-color);
  box-shadow: 0 0 0 2px var(--focus-ring);
}
.forge-bind-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 8%, transparent);
  color: var(--color-red);
  font-size: 12.5px;
  line-height: var(--line-height-snug);
}
/* Pre-submit warning for a non-official host. Amber rather than red because it
   does not block: the bind is allowed, it just needs to be a knowing choice. */
.forge-bind-warning {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-sm);
  border: 1px solid color-mix(in srgb, var(--color-yellow) 35%, transparent);
  background: color-mix(in srgb, var(--color-yellow) 12%, transparent);
  color: var(--color-yellow);
  font-size: 12.5px;
  line-height: var(--line-height-snug);
}
.forge-bind-warning svg {
  flex-shrink: 0;
}
</style>
