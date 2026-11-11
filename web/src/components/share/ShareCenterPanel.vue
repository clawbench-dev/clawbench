<template>
  <div class="share-center">
    <!-- Header: breadcrumb + filters + refresh. The filters are chips rather
         than a dropdown because there are only four states and they are the
         main way to find a link again ("which conversations did I share?"). -->
    <div class="share-header">
      <div class="share-breadcrumb">
        <Share2 :size="14" class="crumb-icon" />
        <span>{{ t('shareCenter.title') }}</span>
        <span v-if="items.length > 0" class="share-count">{{ t('shareCenter.count', { count: items.length }) }}</span>
      </div>
      <ActionBar class="share-header-actions" mode="menu" align="end" size="header">
        <!-- Both buttons carry ONLY `header-btn`: size, radius, colour, hover
             and press all come from the shared ActionBar row rules, and the
             bar decides whether the labels fit. Restating any of it here is
             how the refresh button ended up with a browser-drawn border while
             the button next to it had none. -->
        <RefreshButton
          class="header-btn"
          :loading="loading"
          :disabled="loading"
          :title="t('shareCenter.refresh')"
          @click="reload"
        >
          <span class="action-label">{{ t('shareCenter.refresh') }}</span>
        </RefreshButton>
        <button
          class="header-btn is-danger"
          :disabled="items.length === 0 || loading || clearing"
          :title="t('shareCenter.clearAllTip')"
          data-action="clear-all"
          @click="clearAll"
        >
          <Trash2 :size="14" />
          <span class="action-label">{{ t('shareCenter.clearAll') }}</span>
        </button>
      </ActionBar>
    </div>

    <div class="share-filters" role="tablist">
      <button
        v-for="f in filters"
        :key="f.value"
        type="button"
        class="share-filter"
        :class="{ 'is-on': filter === f.value }"
        role="tab"
        :aria-selected="filter === f.value"
        :data-filter="f.value"
        @click="filter = f.value"
      >
        {{ t(f.labelKey) }}
        <span v-if="counts[f.value]" class="share-filter-count">{{ counts[f.value] }}</span>
      </button>
    </div>

    <div class="share-body">
      <div v-if="loading && items.length === 0" class="share-skeleton">
        <PanelSkeleton variant="list" :rows="8" :meta="true" :label="t('common.loading')" />
      </div>

      <div v-else-if="errorText" class="share-hint share-hint--error">
        <AlertTriangle class="empty-state__icon" />
        <span>{{ errorText }}</span>
        <button class="share-retry" type="button" @click="reload">{{ t('common.retry') }}</button>
      </div>

      <EmptyState
        v-else-if="visible.length === 0"
        :icon="Share2"
        :title="items.length === 0 ? t('shareCenter.empty') : t('shareCenter.emptyFiltered')"
        :desc="items.length === 0 ? t('shareCenter.emptyDesc') : t('shareCenter.emptyFilteredDesc')"
      />

      <div v-else class="share-list">
        <div
          v-for="item in visible"
          :key="item.key"
          class="share-row"
          :class="{ 'is-open': openKey === item.key, 'is-gone': item.sourceGone }"
          :data-share-key="item.key"
        >
          <button type="button" class="share-row-main" @click="toggle(item)">
            <span class="share-kind-icon" :class="`kind-${item.kind}`">
              <Folder v-if="item.kind === 'dir'" :size="15" />
              <FileText v-else-if="item.kind === 'file'" :size="15" />
              <MessageSquareShare v-else-if="item.kind === 'session'" :size="15" />
              <Video v-else :size="15" />
            </span>
            <span class="share-row-text">
              <span class="share-row-name-line">
                <span class="share-row-name" :title="item.name">{{ item.name }}</span>
                <span v-if="item.sourceGone" class="share-badge share-badge--warn">
                  {{ t(item.goneLabelKey) }}
                </span>
                <span v-if="item.running" class="share-badge share-badge--live">
                  {{ t('shareCenter.meetingRunning') }}
                </span>
                <!-- A finished meeting whose record is not published has a link
                     that only works while the meeting runs. Saying so on the row
                     is the difference between "shared" and "shared until I hang
                     up". -->
                <span
                  v-else-if="item.kind === 'meeting' && item.shareEnabled === false"
                  class="share-badge share-badge--muted"
                  :title="t('shareCenter.meetingPrivateHint')"
                >
                  {{ t('shareCenter.meetingPrivate') }}
                </span>
              </span>
              <span class="share-row-meta">
                <span class="share-row-kind">{{ t(kindLabelKey(item)) }}</span>
                <template v-if="item.detail">
                  <span class="share-row-sep">·</span>
                  <span class="share-row-detail" :title="item.detail">{{ item.detail }}</span>
                </template>
                <template v-if="item.createdAt">
                  <span class="share-row-sep">·</span>
                  <span>{{ t('shareCenter.sharedAt', { time: relative(item.createdAt) }) }}</span>
                </template>
              </span>
            </span>
            <ChevronDown :size="15" class="share-row-chevron" />
          </button>

          <!-- Expanded: the addresses and the actions. Only one row is open at
               a time — the address list is tall, and two open rows would push
               the actions of the first out of view. -->
          <div v-if="openKey === item.key" class="share-row-panel">
            <ShareAddressList :links="links" />

            <p v-if="item.kind === 'meeting'" class="share-panel-hint">
              <Info :size="12" />
              {{ item.shareEnabled ? t('shareCenter.meetingSharedHint') : t('shareCenter.meetingPrivateHint') }}
            </p>

            <!-- No standalone "copy" button: the address field above already
                 carries one, and a second one two lines below it was the same
                 action twice. The share button degrades to copying on hosts
                 without a sheet, which is where the remaining copy lives. -->
            <ActionBar class="share-actions" mode="menu" size="bar">
              <button class="fbtn" type="button" @click="openLink">
                <ExternalLink :size="14" />
                <span class="action-label">{{ t('shareCenter.open') }}</span>
              </button>
              <button
                class="fbtn"
                type="button"
                :title="canShare ? t('shareCenter.shareTo') : t('shareCenter.copyInstead')"
                data-action="share-to"
                @click="shareTo(item)"
              >
                <Share2 :size="14" />
                <span class="action-label">{{ canShare ? t('shareCenter.shareTo') : t('shareCenter.copy') }}</span>
              </button>
              <button class="fbtn" type="button" data-action="mail" @click="sendByMail(item)">
                <Mail :size="14" />
                <span class="action-label">{{ t('shareCenter.mail') }}</span>
              </button>
              <!-- Meetings have no "revoke": the link IS the record. Unpublishing
                   is the closest thing, and it only affects the post-meeting
                   link — a running room stays joinable. -->
              <button
                class="fbtn fbtn-danger"
                type="button"
                data-action="revoke"
                @click="revokeOne(item)"
              >
                <Trash2 :size="14" />
                <span class="action-label">{{ t('shareCenter.revoke') }}</span>
              </button>
            </ActionBar>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  ChevronDown,
  ExternalLink,
  FileText,
  Folder,
  Info,
  Mail,
  MessageSquareShare,
  Share2,
  Trash2,
  Video,
} from 'lucide-vue-next'
import ActionBar from '@/components/common/ActionBar.vue'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ShareAddressList from '@/components/common/ShareAddressList.vue'
import { useDialog } from '@/composables/useDialog'
import { useToast } from '@/composables/useToast'
import { useShareCenter, type ShareItem } from '@/composables/useShareCenter'
import { onShareChanged } from '@/composables/useShareEvents'
import type { ShareLink } from '@/composables/useShareLinks'
import { openExternalUrl } from '@/utils/externalLink'
import { formatRelativeTime } from '@/utils/format'
import { appLog } from '@/utils/appLog'
import { serverConfig } from '@/composables/useSettingsConfig'
import { useMailCompose } from '@/composables/useMailCompose'
import { builtInMailAvailable, openSystemMail, shareCapability, shareLink } from '@/utils/shareActions'

const TAG = 'ShareCenterPanel'

const props = defineProps<{ active?: boolean }>()

const { t } = useI18n()
const dialog = useDialog()
const toast = useToast()
const mail = useMailCompose()
const center = useShareCenter()

const { items, loading, errors } = center

type FilterValue = 'all' | 'file' | 'session' | 'meeting'
const filter = ref<FilterValue>('all')

const filters: Array<{ value: FilterValue; labelKey: string }> = [
  { value: 'all', labelKey: 'shareCenter.filterAll' },
  { value: 'file', labelKey: 'shareCenter.filterFile' },
  { value: 'session', labelKey: 'shareCenter.filterSession' },
  { value: 'meeting', labelKey: 'shareCenter.filterMeeting' },
]

/** Files and folders share one filter: both are "a path on disk". */
function matchesFilter(item: ShareItem, f: FilterValue): boolean {
  if (f === 'all') return true
  if (f === 'file') return item.kind === 'file' || item.kind === 'dir'
  return item.kind === f
}

const visible = computed(() => items.value.filter((i) => matchesFilter(i, filter.value)))

const counts = computed<Record<FilterValue, number>>(() => {
  const out: Record<FilterValue, number> = { all: items.value.length, file: 0, session: 0, meeting: 0 }
  for (const i of items.value) {
    if (i.kind === 'file' || i.kind === 'dir') out.file += 1
    else if (i.kind === 'session') out.session += 1
    else out.meeting += 1
  }
  return out
})

/**
 * Which lists failed. Shown only when NOTHING loaded: a partial failure with
 * rows on screen is already visible as missing rows, and an error banner on top
 * of a working list reads as "the whole panel is broken".
 */
const errorText = computed(() => {
  if (items.value.length > 0) return ''
  const kinds: string[] = []
  if (errors.value.files) kinds.push(t('shareCenter.filterFile'))
  if (errors.value.sessions) kinds.push(t('shareCenter.filterSession'))
  if (errors.value.meetings) kinds.push(t('shareCenter.filterMeeting'))
  if (kinds.length === 0) return ''
  if (kinds.length === 3) return t('shareCenter.loadFailed')
  return t('shareCenter.partialFailed', { kinds: kinds.join(' / ') })
})

// Unsubscribe handle for the "a share changed elsewhere" notification.
let offShareChanged: (() => void) | null = null

const openKey = ref('')
const links = ref<ShareLink[]>([])
let linksGen = 0

const canShare = computed(() => shareCapability() === 'sheet')

function kindLabelKey(item: ShareItem): string {
  switch (item.kind) {
    case 'dir': return 'shareCenter.kindDir'
    case 'file': return 'shareCenter.kindFile'
    case 'session': return 'shareCenter.kindSession'
    default: return 'shareCenter.kindMeeting'
  }
}

function relative(iso: string): string {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : formatRelativeTime(iso)
}

async function reload(): Promise<void> {
  await center.load()
  // The open row's address list belongs to the previous data set; rebuild it so
  // a revoked row cannot keep showing a live link.
  const stillThere = items.value.find((i) => i.key === openKey.value)
  if (!stillThere) {
    openKey.value = ''
    links.value = []
    return
  }
  await loadLinks(stillThere)
}

async function loadLinks(item: ShareItem): Promise<void> {
  const gen = ++linksGen
  try {
    const all = await center.linksFor(item)
    if (gen !== linksGen) return
    links.value = all
  } catch (e) {
    appLog.w(TAG, 'build share links failed', e)
    if (gen !== linksGen) return
    links.value = []
  }
}

function toggle(item: ShareItem): void {
  if (openKey.value === item.key) {
    openKey.value = ''
    links.value = []
    return
  }
  openKey.value = item.key
  links.value = []
  void loadLinks(item)
}

function primaryUrl(): string {
  return links.value[0]?.url ?? ''
}

function openLink(): void {
  const url = primaryUrl()
  if (!url) return
  openExternalUrl(url)
}

async function shareTo(item: ShareItem): Promise<void> {
  const url = primaryUrl()
  if (!url) return
  const result = await shareLink(url, item.name)
  // On hosts without a share sheet the action degrades to copying — say which
  // one happened instead of claiming a sheet was shown.
  toast.show(t(result === 'shared' ? 'shareCenter.shareTo' : 'shareCenter.copied'), {
    icon: 'link',
    type: 'success',
    duration: 1800,
  })
}

/**
 * Send the link by email.
 *
 * The built-in client wins when it is available: it keeps the user inside the
 * app and the message actually goes out from their configured mailbox. When no
 * mailbox is configured (or the feature is switched off server-side) the system
 * mail app takes over — a `mailto:` URL, which every host can hand to the OS.
 */
async function sendByMail(item: ShareItem): Promise<void> {
  const url = primaryUrl()
  if (!url) return
  const subject = t('shareCenter.mailSubject', { name: item.name })
  const body = t('shareCenter.mailBody', { url })

  if (builtInMailAvailable(serverConfig.value as Record<string, unknown>)) {
    const opened = await mail.compose(subject, body)
    if (opened) return
  }
  openSystemMail(subject, body)
}

async function revokeOne(item: ShareItem): Promise<void> {
  const confirmed = await dialog.confirm(
    t(item.kind === 'meeting' ? 'shareCenter.confirmRevokeMeeting' : 'shareCenter.confirmRevoke', { name: item.name }),
    { dangerous: true },
  )
  if (!confirmed) return
  try {
    await center.revoke(item)
    toast.show(
      t(item.kind === 'meeting' ? 'shareCenter.revokedMeeting' : 'shareCenter.revoked'),
      { icon: 'link', type: 'success', duration: 2200 },
    )
    if (openKey.value === item.key) {
      openKey.value = ''
      links.value = []
    }
  } catch (e) {
    appLog.w(TAG, 'revoke failed', e)
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  }
}

const clearing = ref(false)

async function clearAll(): Promise<void> {
  const confirmed = await dialog.confirm(
    t('shareCenter.confirmClearAll', { count: items.value.length }),
    { dangerous: true },
  )
  if (!confirmed) return
  clearing.value = true
  try {
    await center.revokeAll()
    openKey.value = ''
    links.value = []
    toast.show(t('shareCenter.cleared'), { icon: 'link', type: 'success', duration: 2200 })
  } catch (e) {
    appLog.w(TAG, 'clear-all failed', e)
    toast.show(e instanceof Error ? e.message : String(e), { icon: 'warning', type: 'error', duration: 3000 })
  } finally {
    clearing.value = false
  }
}

onMounted(() => {
  void center.load()
  // Shares are created from three other screens (a file's toolbar, a
  // conversation's dialog, a meeting's result page). Re-activating the tab
  // covers "share it, then come look", but not having this tab open on the
  // other half of a wide screen while the share is made — there the list stayed
  // stale until the user switched away and back. The emit closes that gap.
  //
  // `reload()` rather than `load()`: it also rebuilds the open row's address
  // list, so a regenerated link is never shown as the old one.
  offShareChanged = onShareChanged(() => {
    if (props.active) void reload()
  })
})

onBeforeUnmount(() => {
  offShareChanged?.()
})

// Re-entering the tab refreshes: links are created from three other screens, so
// a list loaded once would go stale the moment the user shares something else.
watch(
  () => props.active,
  (active) => {
    if (active) void reload()
  },
)
</script>

<style scoped>
.share-center {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.share-header {
  display: flex;
  align-items: center;
  height: var(--header-height);
  padding: 0 var(--space-2) 0 var(--space-6);
  flex-shrink: 0;
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  gap: var(--space-3);
}

.share-breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  white-space: nowrap;
}

.crumb-icon {
  color: var(--text-secondary);
  flex-shrink: 0;
}

.share-count {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-normal);
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

/* Wider than its content so the ActionBar can decide whether the labels fit. */
.share-header-actions {
  flex: 1 1 auto;
}

/* Only what the shared ActionBar row rules do NOT set. Size, radius, colour,
   hover and press all come from `.ab--header button` in css/components.css
   (size="header"); restating them here is what made the refresh button and the
   clear button disagree about having a border. */
.header-btn {
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  gap: var(--space-2);
}

.header-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

/* ── Filters ── */
.share-filters {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-6);
  flex-shrink: 0;
  overflow-x: auto;
  scrollbar-width: none;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
}

.share-filters::-webkit-scrollbar {
  display: none;
}

.share-filter {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
  height: calc(26 * var(--dp));
  padding: 0 var(--space-4);
  border: 1px solid var(--border-color, #d0d7de);
  border-radius: var(--radius-full);
  background: transparent;
  color: var(--text-secondary);
  font-size: var(--font-size-xs);
  cursor: pointer;
  transition: background var(--duration-fast), color var(--duration-fast), border-color var(--duration-fast);
}

.share-filter.is-on {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
  color: var(--accent-color);
}

.share-filter-count {
  font-variant-numeric: tabular-nums;
  opacity: 0.75;
}

/* ── Body ── */
.share-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: var(--panel-body-pad, var(--space-5, 10px));
  /* Column flex so the shared empty state can take the body height. The list
     is a single auto-height child, so it still starts at the top. */
  display: flex;
  flex-direction: column;
}

.share-skeleton {
  padding: var(--space-2) 0;
}

.share-hint {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-5);
  padding: calc(32 * var(--dp)) var(--space-6);
  text-align: center;
  color: var(--text-muted);
  font-size: var(--font-size-md);
}

.share-hint--error {
  color: var(--color-red);
}

.share-retry {
  padding: var(--space-3) calc(14 * var(--dp));
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color, #dee2e6);
  background: var(--bg-tertiary, #f0f0f0);
  color: var(--text-secondary, #666);
  font-size: var(--font-size-md);
  cursor: pointer;
}

.share-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

/* ── Row ── */
.share-row {
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.2));
  border-radius: var(--radius-md);
  background: var(--bg-primary);
  overflow: hidden;
  transition: border-color var(--duration-fast);
}

.share-row.is-open {
  border-color: color-mix(in srgb, var(--accent-color) 45%, transparent);
}

.share-row-main {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  width: 100%;
  padding: var(--space-5) var(--space-5);
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  color: inherit;
  font: inherit;
}

@media (hover: hover) {
  .share-row-main:hover {
    background: color-mix(in srgb, var(--text-primary) 4%, transparent);
  }
}

/* One colour per kind, so the four sources are told apart at a glance.
   All four come from the status palette, NOT from `--accent-color`: the accent
   is a different colour in each of the 36 themes, and on several of them it
   sits close to one of the status hues — the folder and the conversation would
   then read as the same colour in some themes and not others. The status
   palette is defined identically across every theme, so the mapping holds
   everywhere.
   File is green rather than grey on purpose: leaving one kind grey would make
   it read as "no type" instead of "the file kind". */
.share-kind-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: calc(28 * var(--dp));
  height: calc(28 * var(--dp));
  border-radius: var(--radius-sm);
  color: var(--kind-color);
  background: color-mix(in srgb, var(--kind-color) 12%, transparent);
}

.share-kind-icon.kind-dir { --kind-color: var(--color-info); }
.share-kind-icon.kind-file { --kind-color: var(--color-green); }
.share-kind-icon.kind-session { --kind-color: var(--color-purple); }
.share-kind-icon.kind-meeting { --kind-color: var(--color-orange); }

.share-row-text {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
  flex: 1;
}

.share-row-name-line {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-width: 0;
}

.share-row-name {
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* A row whose source is gone is dimmed but still fully actionable: the link
   works, which is exactly why it must stay reachable to revoke. */
.share-row.is-gone .share-row-name {
  opacity: var(--opacity-muted);
}

.share-badge {
  flex-shrink: 0;
  font-size: var(--font-size-2xs);
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
}

/* Kept identical to SessionSearchDrawer's archived badge: same server state,
   same look, in both lists. */
.share-badge--warn {
  font-size: var(--font-size-2xs);
  padding: 1px calc(5 * var(--dp));
  border-radius: var(--radius-xs);
  background: rgba(230, 162, 60, 0.12);
  color: var(--color-orange);
}

.share-badge--live {
  background: color-mix(in srgb, var(--color-green) 14%, transparent);
  color: var(--color-green);
}

.share-badge--muted {
  background: color-mix(in srgb, var(--text-muted) 14%, transparent);
  color: var(--text-muted);
}

.share-row-meta {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}

.share-row-kind {
  flex-shrink: 0;
}

.share-row-sep {
  opacity: 0.6;
}

.share-row-detail {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

.share-row-chevron {
  flex-shrink: 0;
  color: var(--text-muted);
  transition: transform var(--duration-fast);
}

.share-row.is-open .share-row-chevron {
  transform: rotate(180deg);
}

/* ── Expanded panel ── */
.share-row-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: 0 var(--space-5) var(--space-5);
  border-top: 1px solid color-mix(in srgb, var(--border-color) 60%, transparent);
  padding-top: var(--space-4);
}

.share-panel-hint {
  display: flex;
  align-items: flex-start;
  gap: var(--space-2);
  margin: 0;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
  line-height: var(--line-height-normal);
}

.share-actions {
  width: 100%;
}

/* The shared .fbtn pill, sized for an inline action row rather than a footer. */
.share-actions :deep(.fbtn) {
  height: calc(30 * var(--dp));
  font-size: var(--font-size-sm);
}
</style>
