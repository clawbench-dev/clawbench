<template>
  <div ref="rootEl" class="mail-panel">
    <div v-if="initialLoading" class="mail-panel__loading">
      <PanelSkeleton variant="list" />
    </div>

    <!-- No mailbox configured: the panel does not grow its own account form —
         configuration lives in Settings → 邮箱账号, one place for the whole app. -->
    <EmptyState
      v-else-if="accounts.length === 0"
      :icon="Mail"
      :title="t('mail.empty.title')"
      :desc="t('mail.empty.desc')"
    >
      <template #action>
        <button class="fbtn fbtn-primary" data-action="mail-empty-settings" @click="emit('open-settings')">
          {{ t('mail.empty.action') }}
        </button>
      </template>
    </EmptyState>

    <template v-else>
      <!-- Wide: list on the left, the opened message or composer beside it, so
           reading and writing never take the list away. Narrow: the same views
           drill down — the toolbar belongs to the list only there, and a
           drill-down page owns the whole panel below the app header. -->
      <MailFolderBar
        v-if="split || view === 'list'"
        v-model:account-id="activeAccountId"
        :accounts="accounts"
        :folders="list.folders.value"
        :folder-id="list.activeFolderId.value"
        :query="list.query.value"
        :refreshing="refreshing"
        :marking="markingRead"
        @select-folder="onSelectFolder"
        @refresh="refreshAll"
        @compose="openCompose"
        @mark-all-read="markAllRead"
        @setup="emit('open-settings')"
        @update:query="onQuery"
      />

      <SplitView
        class="mail-panel__split"
        orientation="horizontal"
        :enabled="split && view !== 'list'"
        :ratio="splitRatio"
        :min-left="SPLIT_MIN_LIST"
        :min-right="SPLIT_MIN_DETAIL"
        @update:ratio="onSplitRatioChange"
      >
        <template #left>
          <MailList
            v-if="split || view === 'list'"
            :messages="list.messages.value"
            :loading="list.loading.value"
            :loading-more="list.loadingMore.value"
            :error="list.error.value"
            :has-more="list.hasMore.value"
            :query="list.query.value"
            :active-id="view === 'reader' ? readerMessageId : 0"
            @open="openMessage"
            @load-more="list.loadMore"
            @retry="list.reload"
          />
        </template>

        <template #right>
          <MailReader
            v-if="view === 'reader'"
            :key="readerMessageId"
            :message-id="readerMessageId"
            @back="backToList"
            @reply="startReply"
            @deleted="onMessageDeleted"
            @updated="onMessageUpdated"
          />

          <MailCompose
            v-if="view === 'compose'"
            :key="'compose-' + composeSeq"
            :account-id="activeAccountId"
            :initial="composeInitial"
            @close="closeCompose"
            @sent="onSent"
          />
        </template>
      </SplitView>
    </template>
  </div>
</template>

<script setup lang="ts">
/**
 * The 邮件 dock tab.
 *
 * The layout follows the panel's own width, not the window's: the dock pane is
 * 40% of the window, so the same window can hold a wide or a narrow mailbox.
 *
 *   narrow (<520px) — drill down, one view at a time:
 *     list ──点一行──▶ reader ──返回──▶ list
 *       └──写邮件──▶ compose ◀──回复 / 转发──┘
 *
 *   wide (≥ both panes fit) — list and detail side by side, with the shared
 *     draggable divider; with nothing open the list keeps the whole width and
 *     no second column is reserved:
 *     [ list | reader or compose ]
 *
 * The composer used to be a modal on top of the list; it is a pane now, so it
 * never covers the mail it was started from.
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Mail } from 'lucide-vue-next'
import PanelSkeleton from '@/components/common/PanelSkeleton.vue'
import SplitView from '@/components/common/SplitView.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import MailFolderBar from './MailFolderBar.vue'
import MailList from './MailList.vue'
import MailReader from './MailReader.vue'
import MailCompose from './MailCompose.vue'
import { useMailAccounts } from '@/composables/useMailAccounts'
import { useMailMessages } from '@/composables/useMailMessages'
import { refreshMailUnread } from '@/composables/useMailUnread'
import { useFeatureBackHandler, PRIORITY_PAGE } from '@/composables/useEdgeSwipeBack'
import { apiGet, apiPost } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useToast } from '@/composables/useToast'
import { useUiScale } from '@/composables/useUiScale'
import { DEFAULT_MAIL_SPLIT_RATIO, layoutEpoch, readPanelState, writePanelState } from '@/utils/panelState'
import { mailErrorMessage, type MailDraftFile, type MailMessage, type MailReplyContext } from '@/utils/mailTypes'
import { takePendingMailCompose } from '@/composables/useMailCompose'

const props = defineProps<{ active?: boolean }>()
const emit = defineEmits<{ 'open-settings': [] }>()

const { t } = useI18n()
const accountApi = useMailAccounts()
const list = useMailMessages()

const accounts = computed(() => accountApi.accounts.value)
const activeAccountId = ref(0)
const view = ref<'list' | 'reader' | 'compose'>('list')
const readerMessageId = ref(0)
const initialLoading = ref(true)
const refreshing = ref(false)
const markingRead = ref(false)
// Where the composer returns to: a reply started from the reader goes back to
// that message, a new mail started from the folder bar goes back to the list.
const composeReturn = ref<'list' | 'reader'>('list')
const composeInitial = ref<Partial<MailReplyContext> & { body?: string; attachments?: MailDraftFile[]; draftId?: number }>({})
// Bumped whenever a composer request arrives from outside the panel. The view
// is already 'compose' in that case, so the v-if alone would keep the mounted
// component (and its half-typed draft) instead of showing the new message.
const composeSeq = ref(0)

// Pane minimums in design px. The split appears only when both fit: below that
// the two views would each be too narrow to read, so they drill down instead.
// `clientWidth` is CSS px, so the threshold is scaled like every other geometry
// comparison (see designToCss).
const SPLIT_MIN_LIST = 200
const SPLIT_MIN_DETAIL = 300
const SPLIT_MIN_PANEL = SPLIT_MIN_LIST + SPLIT_MIN_DETAIL + 1

const uiScale = useUiScale()
const rootEl = ref<HTMLElement | null>(null)
const panelWidth = ref(0)
const split = computed(() => panelWidth.value >= SPLIT_MIN_PANEL * uiScale.value)

// The divider between the panes is draggable. The ratio is layout state, so it
// lives in the shared registry: it survives reloads and follows the user's
// layout-memory choice.
const splitRatio = ref(clampMailSplitRatio(readPanelState('mailSplitRatio')))

function onSplitRatioChange(ratio: number): void {
  splitRatio.value = ratio
  writePanelState('mailSplitRatio', ratio)
}

/** Keep a genuine split — reject degenerate 0/1 values from old storage. */
function clampMailSplitRatio(ratio: number): number {
  return ratio > 0 && ratio < 1 ? ratio : DEFAULT_MAIL_SPLIT_RATIO
}

watch(layoutEpoch, () => {
  splitRatio.value = clampMailSplitRatio(readPanelState('mailSplitRatio'))
})

let widthObserver: ResizeObserver | null = null

// Guards the account watcher while loadAll() is picking the account itself:
// without it the first render loads every folder twice.
let bootstrapping = false

watch(activeAccountId, (id) => {
  if (!id || bootstrapping) return
  void activateAccount(id)
})

/** Load one account's folders and land on its inbox. */
async function activateAccount(id: number): Promise<void> {
  await list.loadFolders(id)
  const keep = list.folders.value.some((f) => f.id === list.activeFolderId.value)
  if (keep) return
  const preferred = list.folders.value.find((f) => f.specialUse === 'inbox') ?? list.folders.value[0]
  if (preferred) await list.selectFolder(preferred.id)
  else list.activeFolderId.value = 0
}

/**
 * Read the account list and bring the panel up to date.
 *
 * Called on mount and every time the tab becomes active — the settings page is
 * a different screen, so a mailbox added there leaves this panel holding a
 * stale (often empty) account list, and coming back used to show "还没有配置
 * 邮箱" until the whole app was reloaded.
 *
 * A mailbox that has never synced is synced here rather than waiting for the
 * three-minute worker: it has no folders and no messages, so the tab would
 * otherwise open empty with no explanation.
 */
async function loadAll(): Promise<void> {
  await accountApi.load()
  if (accounts.value.length === 0) {
    activeAccountId.value = 0
    return
  }
  const target = accounts.value.find((a) => a.id === activeAccountId.value) ?? accounts.value[0]
  if (!target.lastSyncAt) {
    refreshing.value = true
    try {
      await accountApi.sync(target.id)
      await accountApi.load()
    } catch (e) {
      appLog.w('MailPanel', 'initial sync failed', e)
    } finally {
      refreshing.value = false
    }
  }
  bootstrapping = true
  activeAccountId.value = target.id
  bootstrapping = false
  await activateAccount(target.id)
}

onMounted(async () => {
  window.addEventListener('clawbench-open-mail', handleOpenMail)
  window.addEventListener('clawbench-compose-mail', handleComposeMail)
  measureWidth()
  if (typeof ResizeObserver !== 'undefined' && rootEl.value) {
    widthObserver = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width
      if (typeof width === 'number') panelWidth.value = width
    })
    widthObserver.observe(rootEl.value)
  }
  try {
    await loadAll()
  } finally {
    initialLoading.value = false
  }
  // A composer requested from another panel may have been parked before this
  // one mounted (the tab is lazily created). Consume it once the account list
  // exists, otherwise the composer would open with no account to send from.
  await consumePendingCompose()
})

onBeforeUnmount(() => {
  window.removeEventListener('clawbench-open-mail', handleOpenMail)
  window.removeEventListener('clawbench-compose-mail', handleComposeMail)
  widthObserver?.disconnect()
  widthObserver = null
})

function measureWidth(): void {
  panelWidth.value = rootEl.value?.clientWidth ?? 0
}

// Re-opening the tab refreshes everything: mail may have arrived while the user
// was elsewhere, and a mailbox may have been added or removed in Settings.
// Only the local cache is re-read — a full IMAP sync on every tab switch would
// make opening the tab feel slow and fight the background worker.
watch(
  () => props.active,
  (active) => {
    if (!active) return
    void loadAll()
  },
)

async function refreshAll(): Promise<void> {
  if (!activeAccountId.value) return
  refreshing.value = true
  try {
    await accountApi.sync(activeAccountId.value)
    await list.loadFolders(activeAccountId.value)
    await list.reload()
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailPanel', 'refresh failed', e)
  } finally {
    refreshing.value = false
  }
}

async function onSelectFolder(id: number): Promise<void> {
  view.value = 'list'
  await list.selectFolder(id)
}

/**
 * Mark every message of the current folder as read.
 *
 * The server call marks the whole mailbox, so the list and the folder counters
 * are refetched afterwards instead of patching rows one by one — the local
 * cache only holds the newest page and would drift from the server's count.
 */
async function markAllRead(): Promise<void> {
  const folderId = list.activeFolderId.value
  if (!folderId || markingRead.value) return
  markingRead.value = true
  try {
    await apiPost(`/api/mail/folders/${folderId}/read-all`, {})
    await list.reload()
    await list.loadFolders(activeAccountId.value)
    void refreshMailUnread()
  } catch (e) {
    appLog.w('MailPanel', 'mark all read failed', e)
    useToast().show(mailErrorMessage(e, t), { icon: 'warning', type: 'error' })
  } finally {
    markingRead.value = false
  }
}

async function onQuery(q: string): Promise<void> {
  view.value = 'list'
  await list.setQuery(q)
}

function openMessage(id: number): void {
  readerMessageId.value = id
  view.value = 'reader'
}

/**
 * Opening an email from a quote's "jump to source".
 *
 * App switches to the 邮件 tab, then this opens the reader. The id comes from
 * the quote, so the message does not have to be on the loaded list page (it may
 * be older than what the folder has cached) — MailReader fetches by id itself.
 */
function handleOpenMail(event: Event): void {
  const detail = (event as CustomEvent<{ messageId?: number }>).detail
  if (detail?.messageId) openMessage(detail.messageId)
}

/**
 * Open the composer from a request parked by another panel (分享中心's
 * 「用邮件发送」). The subject and body arrive filled in; the recipient is left
 * for the user, since nobody but them knows who the link is for.
 */
function handleComposeMail(): void {
  void consumePendingCompose()
}

async function consumePendingCompose(): Promise<void> {
  const req = takePendingMailCompose()
  if (!req) return
  // The account list may still be loading on a cold mount; without an account
  // the composer would have nothing to send from.
  if (accounts.value.length === 0) {
    await loadAll()
  }
  if (accounts.value.length === 0) return
  composeInitial.value = { subject: req.subject, body: req.body, attachments: req.attachments }
  composeSeq.value += 1
  composeReturn.value = 'list'
  view.value = 'compose'
}

async function backToList(): Promise<void> {
  view.value = 'list'
  await list.reload()
  await list.loadFolders(activeAccountId.value)
}

function onMessageDeleted(): void {
  list.removeMessage(readerMessageId.value)
  view.value = 'list'
  void list.loadFolders(activeAccountId.value)
}

function onMessageUpdated(msg: MailMessage): void {
  const prev = list.messages.value.find((m) => m.id === msg.id)
  list.patchMessage(msg)
  // A seen flip also moves the folder's unread counter, which the row alone
  // does not cover: the folder bar (and the picker's "(N)") would keep showing
  // the old number until the next sync. Opening a message flips it too — the
  // reader reports the loaded row for exactly this reason.
  if (!prev || prev.seen !== msg.seen) void list.loadFolders(activeAccountId.value)
}

/**
 * Edge swipe / Android back: one level up, never out of the tab.
 *
 * The panel is a drill-down (list → reader → compose), so both deeper views
 * must answer the gesture. Compose returns to wherever it was opened from —
 * a reply goes back to the message, a new mail goes back to the list.
 */
const canGoBack = computed(() => !!props.active && view.value !== 'list')

useFeatureBackHandler(
  'mail',
  () => canGoBack.value,
  () => {
    if (view.value === 'compose') closeCompose()
    else void backToList()
  },
  PRIORITY_PAGE,
)

function openCompose(): void {
  composeInitial.value = {}
  composeSeq.value += 1
  composeReturn.value = 'list'
  view.value = 'compose'
}

function closeCompose(): void {
  view.value = composeReturn.value
}

async function startReply(mode: 'reply' | 'replyAll' | 'forward'): Promise<void> {
  try {
    const data = await apiGet<{ context: MailReplyContext }>(
      `/api/mail/messages/${readerMessageId.value}/reply-context?mode=${mode}`,
    )
    composeInitial.value = data.context
    composeSeq.value += 1
    composeReturn.value = 'reader'
    view.value = 'compose'
  } catch (e) {
    appLog.w('MailPanel', 'reply context failed', e)
  }
}

async function onSent(): Promise<void> {
  view.value = composeReturn.value
  await list.reload()
  await list.loadFolders(activeAccountId.value)
  void refreshMailUnread()
}
</script>

<style scoped>
.mail-panel__loading {
  padding: var(--space-2, 4px);
}
.mail-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  height: 100%;
  overflow: hidden;
}
/* SplitView root. Narrow: a plain column, and SplitView's panes collapse to
   `display: contents` so the list / reader / composer flow exactly as before.
   Wide: SplitView switches itself to a row with the draggable divider. */
.mail-panel__split {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}
/* SplitView's horizontal panes are plain blocks; the mail views are flex
   columns that need a definite height so their inner scroll areas scroll
   instead of growing the pane. Only in split mode — the narrow layout relies
   on the panes being `display: contents`. */
.mail-panel__split.split-view--active :deep(.split-view__left),
.mail-panel__split.split-view--active :deep(.split-view__right) {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
</style>
