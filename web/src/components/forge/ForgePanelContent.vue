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
      />

      <ForgeDetail
        v-else-if="detailOpen"
        :type="items.type.value"
        :number="detailNumber"
        @back="closeDetail"
        @quote="onQuote"
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
            <span>{{ t(tab.labelKey) }}</span>
          </button>
        </div>

        <!-- Pipelines: a repository-level view with its own filters and list. -->
        <template v-if="activeTab === 'pipeline'">
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
              <Inbox :size="34" :stroke-width="1.5" class="forge-empty-icon" />
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
            <Inbox :size="34" :stroke-width="1.5" class="forge-empty-icon" />
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
                <span class="forge-remote-name">{{ r.name }}</span>
                <span class="forge-remote-url">{{ r.slug || r.url }}</span>
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
import { ref, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Github, Inbox, MessageSquare, CircleQuestionMark, GitPullRequest, Activity,
  ChevronRight, ChevronDown, AlertCircle, Unlink, CheckCheck,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import ForgeDetail from '@/components/forge/ForgeDetail.vue'
import ForgePipelineDetail from '@/components/forge/ForgePipelineDetail.vue'
import { useForgeItems, useForgePipelines, FORGE_PIPELINE_FILTERS } from '@/composables/useForge'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { fetchForgeRemotes, setForgeBinding, deleteForgeBinding, type ForgeRemote, type ForgePipelineRun, type ForgeItem, ForgeApiError } from '@/utils/forgeApi'
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
  { key: 'issue' as const, labelKey: 'forge.type.issues', icon: CircleQuestionMark },
  { key: 'pr' as const, labelKey: 'forge.type.prs', icon: GitPullRequest },
  { key: 'pipeline' as const, labelKey: 'forge.type.pipelines', icon: Activity },
]
type ForgeTabKey = typeof forgeTabs[number]['key']

const activeTab = ref<ForgeTabKey>('issue')

const stateOptions: Array<'open' | 'closed' | 'all'> = ['open', 'closed', 'all']
const mineOptions: Array<'all' | 'assigned' | 'created' | 'review'> = ['all', 'assigned', 'created', 'review']

const searchInput = ref('')
const detailOpen = ref(false)
const detailNumber = ref(0)
const pipelineDetailId = ref(0)
const bindDialogOpen = ref(false)
const remotes = ref<ForgeRemote[]>([])
const manualUrl = ref('')
const bindError = ref('')
const repoMenuOpen = ref(false)
const repoBadgeRef = ref<HTMLElement | null>(null)

/** Switch tabs, loading the target view's data on first use. */
function setActiveTab(key: ForgeTabKey) {
  if (activeTab.value === key) return
  closeDetail()
  activeTab.value = key
  if (key === 'pipeline') {
    void pipelines.load()
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
  // Only the visible tab has data worth reloading; the other loads on switch.
  if (activeTab.value === 'pipeline') await pipelines.load()
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
  // Both lists share one repo-level read state, so clear whichever is loaded.
  // The other list re-reads on its next load and will come back already read.
  void items.markAllRead()
  void pipelines.markAllRead()
}
function closeDetail() {
  detailOpen.value = false
  detailNumber.value = 0
  pipelineDetailId.value = 0
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
  await submitBinding({ platform: r.platform, host: r.host, owner: r.owner, repo: r.repo })
}

async function bindFromUrl() {
  await submitBinding({ url: manualUrl.value })
}

async function submitBinding(input: { url?: string; platform?: string; host?: string; owner?: string; repo?: string }) {
  bindError.value = ''
  try {
    await setForgeBinding(input)
    bindDialogOpen.value = false
    manualUrl.value = ''
    // The binding just changed server-side, so bypass the cache.
    await refresh(true)
  } catch (err) {
    if (err instanceof ForgeApiError) {
      bindError.value = err.code === 'UnsafeHost' ? t('forge.bind.unsafeHost') : err.message
    } else {
      bindError.value = String(err)
    }
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
.forge-state {
  display: flex;
  align-items: center;
  justify-content: center;
  padding:40px var(--space-8);
  flex: 1;
}
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

/* Filter rows sit under the tab bar. */
.forge-toolbar {
  padding: var(--space-4) var(--space-6) 0;
  flex-shrink: 0;
}

/* ── Filter chips ── */
.forge-chips {
  display: flex;
  gap: var(--space-3);
  align-items: center;
}
.forge-chips-scroll {
  overflow-x: auto;
  scrollbar-width: none;
  padding-bottom: var(--space-1);
}
.forge-chips-scroll::-webkit-scrollbar { display: none; }
/* Separates the state group from the mine group without a second row. */
.forge-chips-divider {
  width: 1px;
  height: 14px;
  background: var(--border-color);
  flex-shrink: 0;
  margin:0 var(--space-1);
}
.forge-chip {
  padding: var(--space-2) var(--space-6);
  border-radius: var(--radius-full);
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-md);
  line-height: 18px;
  white-space: nowrap;
  cursor: pointer;
  flex-shrink: 0;
  transition: background var(--duration-base) ease, border-color var(--duration-base) ease, color var(--duration-base) ease;
}
@media (hover: hover) {
  .forge-chip:not(.active):hover {
    border-color: var(--accent-color);
    color: var(--accent-color);
  }
}
.forge-chip.active {
  background: var(--accent-color);
  border-color: var(--accent-color);
  color: #fff;
}

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

.forge-empty-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
  padding: 32px 24px;
}
.forge-empty-icon {
  color: var(--text-muted);
  opacity: var(--opacity-muted);
}
.forge-empty-title {
  font-size: var(--font-size-lg);
  color: var(--text-muted);
}

/* ── List ── */
.forge-list {
  flex: 1;
  overflow-y: auto;
  border-top: 1px solid var(--border-color);
}
.forge-row {
  display: flex;
  align-items: flex-start;
  gap: var(--space-5);
  padding:11px var(--space-6);
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
  transition: background var(--duration-base) ease;
}
@media (hover: hover) {
  .forge-row:hover {
    background: var(--bg-secondary);
  }
}
.forge-row:active {
  background: var(--bg-tertiary);
}
.forge-row-main {
  flex: 1;
  min-width: 0;
}
.forge-row-title {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  min-width: 0;
}
.forge-row-number {
  color: var(--text-muted);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.forge-row-text {
  color: var(--text-primary);
  font-size: var(--font-size-lg);
  line-height: var(--line-height-snug);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.forge-row-meta {
  display: flex;
  align-items: center;
  gap: var(--space-5);
  margin-top: var(--space-2);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.forge-row-comments {
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.forge-row-chevron {
  color: var(--text-hint);
  flex-shrink: 0;
  align-self: center;
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
.forge-remote-url {
  font-size: 11.5px;
  color: var(--text-muted);
  font-family: var(--font-mono);
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
</style>
