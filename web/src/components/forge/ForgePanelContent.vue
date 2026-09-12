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
      <!-- Detail view replaces the list in place (currentView pattern). -->
      <ForgeDetail
        v-if="detailOpen"
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
          <RefreshButton
            class="forge-header-btn"
            :loading="items.loading.value"
            :title="t('nav.refresh')"
            @click="refresh"
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
             State/mine below are independent filters, so they stay as chips. -->
        <div class="forge-tabs">
          <button
            class="forge-tab"
            :class="{ active: items.type.value === 'issue' }"
            @click="items.setType('issue')"
          >
            <CircleQuestionMark :size="13" />
            <span>{{ t('forge.type.issues') }}</span>
          </button>
          <button
            class="forge-tab"
            :class="{ active: items.type.value === 'pr' }"
            @click="items.setType('pr')"
          >
            <GitPullRequest :size="13" />
            <span>{{ t('forge.type.prs') }}</span>
          </button>
        </div>

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
          <button class="fbtn" @click="refresh">{{ t('forge.retry') }}</button>
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
            @click="openDetail(it)"
          >
            <span class="forge-state-dot" :class="`state-${it.state}`"></span>
            <div class="forge-row-main">
              <div class="forge-row-title">
                <span class="forge-row-number">#{{ it.number }}</span>
                <span class="forge-row-text">{{ it.title }}</span>
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
  Github, Inbox, MessageSquare, CircleQuestionMark, GitPullRequest,
  ChevronRight, ChevronDown, AlertCircle, Unlink,
} from 'lucide-vue-next'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ModalDialog from '@/components/common/ModalDialog.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import ForgeDetail from '@/components/forge/ForgeDetail.vue'
import { useForgeItems } from '@/composables/useForge'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { fetchForgeRemotes, setForgeBinding, deleteForgeBinding, type ForgeRemote, ForgeApiError } from '@/utils/forgeApi'
import { appLog } from '@/utils/appLog'

const TAG = 'ForgePanel'
const props = defineProps<{
  active: boolean
  projectPath: string
}>()
const emit = defineEmits<{
  (e: 'request-project'): void
  (e: 'quote', payload: { item: { type: 'issue' | 'pr'; number: number; title: string; url: string; slug: string } }): void
}>()

const { t } = useI18n()
const items = useForgeItems(() => props.projectPath)

const stateOptions: Array<'open' | 'closed' | 'all'> = ['open', 'closed', 'all']
const mineOptions: Array<'all' | 'assigned' | 'created' | 'review'> = ['all', 'assigned', 'created', 'review']

const searchInput = ref('')
const detailOpen = ref(false)
const detailNumber = ref(0)
const bindDialogOpen = ref(false)
const remotes = ref<ForgeRemote[]>([])
const manualUrl = ref('')
const bindError = ref('')
const repoMenuOpen = ref(false)
const repoBadgeRef = ref<HTMLElement | null>(null)

async function refresh() {
  await items.loadBinding()
  if (items.isBound.value) await items.load()
}

onMounted(refresh)

// Reload when the project changes or the panel becomes active.
watch(() => props.projectPath, () => {
  closeDetail()
  void refresh()
})
watch(() => props.active, (isActive) => {
  if (isActive && items.isBound.value && items.items.value.length === 0 && !items.loading.value) {
    void items.load()
  }
})

function openDetail(it: { type: 'issue' | 'pr'; number: number }) {
  items.type.value = it.type
  detailNumber.value = it.number
  detailOpen.value = true
}
function closeDetail() {
  detailOpen.value = false
  detailNumber.value = 0
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
  await refresh()
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
    await refresh()
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
  padding: 0 4px 0 12px;
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color);
  gap: 6px;
}
.forge-header-title {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

/* Clickable repository badge — the context switcher for the bound remote,
   matching the app header's project/branch badges. */
.forge-repo-badge {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  padding: 3px 7px;
  border: none;
  border-radius: var(--radius-sm);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 12px;
  font-weight: 600;
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
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 13px;
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
  border-radius: 14px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: background 0.2s ease, color 0.2s ease;
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
  padding: 40px 20px;
  flex: 1;
}
.forge-card {
  max-width: 340px;
  width: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  gap: 8px;
}
.forge-card-icon {
  color: var(--text-muted);
  opacity: 0.6;
  margin-bottom: 2px;
}
.forge-card-header {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}
.forge-card-body {
  color: var(--text-muted);
  font-size: 13px;
  line-height: 1.5;
}
.forge-card-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
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
  padding: 0 16px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 500;
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
  padding: 8px 12px 0;
  flex-shrink: 0;
}

/* ── Filter chips ── */
.forge-chips {
  display: flex;
  gap: 6px;
  align-items: center;
}
.forge-chips-scroll {
  overflow-x: auto;
  scrollbar-width: none;
  padding-bottom: 2px;
}
.forge-chips-scroll::-webkit-scrollbar { display: none; }
/* Separates the state group from the mine group without a second row. */
.forge-chips-divider {
  width: 1px;
  height: 14px;
  background: var(--border-color);
  flex-shrink: 0;
  margin: 0 2px;
}
.forge-chip {
  padding: 4px 12px;
  border-radius: 999px;
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 18px;
  white-space: nowrap;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;
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
  padding: 8px 12px;
  flex-shrink: 0;
}

/* ── Error card ── */
.forge-error-card {
  margin: 8px 12px;
  padding: 12px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 1px solid color-mix(in srgb, var(--color-red) 35%, var(--border-color));
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 6%, var(--bg-secondary));
}
.forge-error-icon {
  color: var(--color-red);
  flex-shrink: 0;
}
.forge-error-text { flex: 1; min-width: 0; }
.forge-error-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 2px;
}
.forge-error-body {
  color: var(--text-secondary);
  font-size: 12px;
}

/* ── Loading / empty ── */
.forge-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
}
.forge-empty-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 32px 24px;
}
.forge-empty-icon {
  color: var(--text-muted);
  opacity: 0.5;
}
.forge-empty-title {
  font-size: 14px;
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
  gap: 10px;
  padding: 11px 12px;
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
  transition: background 0.15s ease;
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
  gap: 6px;
  min-width: 0;
}
.forge-row-number {
  color: var(--text-muted);
  font-family: var(--font-mono);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}
.forge-row-text {
  color: var(--text-primary);
  font-size: 14px;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.forge-row-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 12px;
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
/* Status dot sits on the title's first-line baseline, not the row's vertical
   centre (the row is two lines tall). */
.forge-state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  margin-top: 6px;
}
.forge-state-dot.state-open { background: var(--color-success); }
.forge-state-dot.state-closed { background: var(--color-red); }
.forge-state-dot.state-merged { background: var(--color-purple); }
.forge-loading-more {
  padding: 12px;
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
  padding: 14px 16px 16px;
}
.forge-bind-section {
  display: flex;
  flex-direction: column;
}
.forge-bind-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.forge-bind-remotes {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
/* A remote row reads as one tappable object: brand mark, a two-line
   name/url stack, and a chevron signalling it commits a choice. */
.forge-remote-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  cursor: pointer;
  transition: border-color 0.15s ease, background 0.15s ease;
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
  opacity: 0.5;
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
  font-size: 13px;
  font-weight: 600;
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
  transition: color 0.15s ease;
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
  padding: 9px 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: 13px;
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
  gap: 6px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--color-red) 8%, transparent);
  color: var(--color-red);
  font-size: 12.5px;
  line-height: 1.4;
}
</style>
