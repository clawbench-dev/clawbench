<template>
  <div class="chat-panel-content">
    <!-- Messages -->
    <ChatMessageList
      ref="messageListRef"
      :messages="renderedMessages"
      :expandedTools="render.expandedTools.value"
      :blockTasks="render.blockTasks"
      :blockAskQuestions="render.blockAskQuestions"
      :staticBlockCache="render.staticBlockCache"
      :agents="agentsList"
      :currentAgent="currentAgent"
      :currentSessionId="identity.currentSessionId.value"
      :hasMore="session.hasMore.value"
      :loadingMore="session.loadingMore.value"
      :switching="session.switching.value"
      :totalMessages="session.totalMessages.value"
      :active="props.active"
      :midTurnSupported="midTurnSupported"
      :pendingActionBusy="pendingActionBusy"
      @touchstart.passive="swipeSession.onTouchStart"
      @touchend="swipeSession.onTouchEnd"
      @toggle-tool="render.toggleToolDetail"
      @show-tool-detail="handleShowToolDetail"
      @show-metadata="showMetadata"
      @file-tag-click="handleFileTagClick"
      @quote-message="handleQuoteMessage"
      @load-more="handleLoadMore"
      @task-card-click="(taskId) => $emit('task-card-click', taskId)"
      @send-message="handleToolSendMessage"
      @remove-pending="handleRemovePending"
      @pending-action="handlePendingAction"
      @render-flush="handleRenderFlush"
      @toggle-summary="handleToggleSummary"
      @ensure-content="(msg) => ensureMessageContent(msg)"
      @resume-session="handleResumeSession"
      @reset-session="handleResetSession"
      @fork-from-message="handleForkFromMessage"
      @rewind-from-message="handleRewindFromMessage"
    />

    <!-- Session swipe indicator — floats above the message area -->
    <Transition name="session-indicator">
      <div v-if="swipeSession.indicatorText.value" class="session-switch-indicator" :class="swipeSession.indicatorDirection.value">
        <div class="session-indicator-row">
          <span class="session-indicator-text">{{ swipeSession.indicatorText.value }}</span>
        </div>
        <div v-if="showPositionIndicator" class="session-indicator-position">
          <div v-if="swipeSession.sessionTotal.value <= 15" class="session-dots">
            <span v-for="i in swipeSession.sessionTotal.value" :key="i"
                  class="session-dot" :class="{ active: i - 1 === swipeSession.sessionIndex.value }" />
          </div>
          <div v-else class="session-capsule">
            <div class="session-capsule-track">
              <div class="session-capsule-slider" :style="capsuleSliderStyle" />
            </div>
          </div>
          <span class="session-position-count">{{ swipeSession.sessionIndex.value + 1 }}/{{ swipeSession.sessionTotal.value }}</span>
        </div>
      </div>
    </Transition>

    <!-- Plan progress panel -->
    <PlanPanel
      :entries="planEntries"
      :collapsed="planCollapsed"
      :has-update="planHasUpdate"
      @toggle-collapse="togglePlanCollapse"
    />

    <!-- Unified input container — hidden when no agents configured -->
    <ChatInputBar
      v-if="agentsList.length > 0"
      ref="inputBarRef"
      :inputDisabled="inputDisabled"
      :loading="loading"
      :currentFile="currentFile"
      :currentDir="currentDir"
      :attachedFiles="attachedFiles"
      :quotes="stagedQuotes"
      :messages="renderedMessages"
      :autoSpeechEnabled="autoSpeech.enabled.value"
      :refreshingSession="refreshingSession"
      :currentSessionId="identity.currentSessionId.value"
      :chatUnreadCount="store.state.chatUnreadCount"
      :chatRunning="identity.runningSessions.value.size > 0"
      :currentSessionRunning="identity.runningSessions.value.has(identity.currentSessionId.value)"
      :currentModelId="identity.currentModelId.value"
      :currentModelName="identity.currentModelName.value"
      :currentModeName="identity.currentModeName.value"
      :currentTransport="identity.currentTransport.value"
      :currentAgentId="identity.currentAgentId.value"
      :acpSyncing="acpSyncing"
      :active="props.active"
      @send="sendMessage"
      @cancel="stream.cancelStream"
      @add-attached="addAttachedFile"
      @remove-attached="removeAttachedFile"
      @remove-attached-by-path="handleRemoveAttachedEntry"
      @remove-quote="removeStagedQuote($event)"
      @quote-click="handleQuoteClick"
      @open-session-tab="identity.openSessionTab"
      @open-session-search="$emit('open-session-search')"
      @file-tag-click="handleFileTagClick"
      @toggle-auto-speech="autoSpeech.toggle"
      @create-session="() => manager.createSession()"
      @show-agent-selector="handleShowAgentSelector"
      @archive-session="() => manager.archiveCurrentSession((draftId) => inputBarRef.value?.deleteDraft(draftId))"
      @destroy-session="() => manager.destroyCurrentSession((draftId) => inputBarRef.value?.deleteDraft(draftId))"
      @open-user-msg-index="handleOpenUserMsgIndex"
      @refresh-session="handleRefreshSession"
      @switch-model="handleSwitchModel"
      @switch-thinking-effort="handleSwitchThinkingEffort"
      @switch-mode="handleSwitchMode"
      @switch-transport="handleSwitchTransport"
      @sync-acp-session="handleSyncAcpSession"
    />

  </div>

  <!-- Quote detail drawer — a SINGLETON, opened for whichever quote card was
       clicked (input chip or sent bubble). Not mounted per message: the drawer
       shows one quote at a time, and ChatMessageItem already mounts two drawers
       per message. -->
  <QuoteDetailDrawer
    :open="quoteDetail.open.value"
    :quote="quoteDetail.quote.value"
    :saving="quoteNoteSaving"
    @close="quoteDetail.close()"
    @save="saveQuoteNote"
    @jump="jumpToQuoteSource"
  />

  <!-- Metadata Modal -->
  <ChatMetadataModal
    :show="metadataDrawer.effectiveOpen.value"
    :data="metadataModal.data"
    :backend="metadataModal.backend"
    :createdAt="metadataModal.createdAt"
    :relatedFile="metadataModal.relatedFile"
    :messageId="metadataModal.messageId"
    :sessionId="metadataModal.sessionId"
    :ftsIndexed="metadataModal.ftsIndexed"
    :vecIndexed="metadataModal.vecIndexed"
    :formatDetailTime="render.formatDetailTime"
    @close="metadataDrawer.close()"
  />

  <!-- Tool Detail Overlay -->
  <ToolDetailDrawer
    :show="toolDetailDrawer.effectiveOpen.value"
    :toolName="toolDetailOverlay.name"
    :toolSubagentType="toolDetailOverlay.subagentType"
    :toolSummary="toolDetailOverlay.summary"
    :toolInputHtml="toolDetailOverlay.inputHtml"
    :toolOutputHtml="toolDetailOverlay.outputHtml"
    :toolStatus="toolDetailOverlay.status"
    :toolDone="toolDetailOverlay.done"
    :toolDuration="toolDetailOverlay.duration"
    :displayNameOverride="toolDetailOverlay.displayNameOverride"
    @close="closeToolDetailOverlay()"
    @file-open="handleFileOpenInOverlay"
    @send-message="handleToolSendMessage"
    @click="handleOverlayRetryClick"
  />

  <!-- Agent Selector for Fork -->
  <AgentSelectorDrawer
    :open="forkAgentSelectorDrawer.effectiveOpen.value"
    :modelValue="identity.currentAgentId.value"
    :title="t('chat.session.selectAgentForFork')"
    :default-badge="t('chat.sessionSetting.defaultBadge')"
    :set-default-title="t('session.setAsDefaultAgent')"
    :config-title="t('session.configAgent')"
    @update:open="v => { if (v) forkAgentSelectorDrawer.open(); else { forkAgentSelectorDrawer.close(); forkPending.value = null } }"
    @select="handleForkAgentSelect"
  />
</template>

<script setup>
import { ref, computed, watch, onUnmounted, onMounted, inject, provide, toRef, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'
import { NEAR_BOTTOM_PX } from '@/utils/scrollState'
import { apiGet, apiPost, apiPatch } from '@/utils/api'
import { gt } from '@/composables/useLocale'
import { useTabDrawer } from '@/composables/useTabDrawer'
import ChatMetadataModal from './ChatMetadataModal.vue'
import ToolDetailDrawer from './ToolDetailDrawer.vue'
import QuoteDetailDrawer from './QuoteDetailDrawer.vue'
import ChatInputBar from './ChatInputBar.vue'
import ChatMessageList from './ChatMessageList.vue'
import PlanPanel from './PlanPanel.vue'
import { usePlanProgress } from '@/composables/usePlanProgress'
import { useChatRender } from '@/composables/useChatRender.ts'
import { formatToolOutput, revertAskSubmission } from '@/utils/renderToolDetail.ts'
import { useChatStream } from '@/composables/useChatStream.ts'
import { useChatSession, loadSessionsOnce } from '@/composables/useChatSession.ts'
import { useSessionIdentity, getSessionId } from '@/composables/useSessionIdentity.ts'
import { useSessionManager } from '@/composables/useSessionManager.ts'
import { createChatMessageStore } from '@/composables/useChatMessageStore.ts'
import { useAcpSession } from '@/composables/useAcpSession'

import { useAgents, populateACPStateFromCache } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useNotification } from '@/composables/useNotification.ts'
import { applySummaryUpdate, isShowingSummary, isLastAssistantMessage, normalizeDisplayMode } from '@/utils/chatSessionUtils.ts'
import { localConfig } from '@/composables/useSettingsConfig'
import { nextClientSeq } from '@/utils/chatStreamUtils.ts'
import { useFileUpload } from '@/composables/useFileUpload.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { relativizeProjectPath } from '@/utils/quoteQuestionUtils.ts'
import { resetQuotePin } from '@/composables/useQuoteQuestion.ts'
import { fromStagedQuote, fromFileEntry, materializeQuotes } from '@/utils/quoteItem.ts'
import { useQuoteDetail } from '@/composables/useQuoteDetail.ts'
import { pendingMessageNavigation, consumePendingMessageNavigation, setPendingMessageNavigation } from '@/composables/useMessageNavigation.ts'
import { openExternalUrl } from '@/utils/externalLink.ts'
import { buildSendPayload } from '@/utils/fileAttachmentUtils.ts'
import { enqueueAndMaybeStart } from '@/utils/chatQueueSend.ts'
import { trackInFlightSend, untrackInFlightSend } from '@/utils/chatStreamUtils.ts'
import { refreshCurrentFile } from '@/composables/useFileRefresh.ts'
import { sameFilePath } from '@/utils/path.ts'
import { playNotificationSound } from '@/composables/useNotificationSound.ts'
import { useAutoSpeech, extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { useSwipeSession } from '@/composables/useSwipeSession.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { useAppForeground } from '@/composables/useAppForeground'
import { store } from '@/stores/app.ts'

import { useDialog } from '@/composables/useDialog'

import AgentSelectorDrawer from '@/components/common/AgentSelectorDrawer.vue'

import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer.ts'

const { t } = useI18n()
const TAG = 'ChatPanel'

const props = defineProps({
    active: Boolean,
    // Focus-aware keyboard gating: global chat shortcuts (Ctrl+←/→, Ctrl+Delete)
    // fire only when the chat pane is the one the user is working in.
    keyboardActive: { type: Boolean, default: true },
    currentFile: Object,
    currentDir: String,
})
const emit = defineEmits(['open', 'message', 'task-card-click', 'open-session-search'])

// ── Singletons ──
const identity = useSessionIdentity()
const agentsComposable = useAgents()
const { agents: agentsList, getAgent, getAgentBackend, getAgentName } = agentsComposable

/** Whether the current agent's backend can join a running turn. Drives the
 *  queued bubble's single action: "insert into the current reply" vs
 *  "interrupt and send".
 *
 *  Two conditions, because the capability is per-backend but the ability is
 *  per-transport: injection goes through the agent's live ACP connection
 *  (ai.InjectMidTurn returns a decline when there is none), so a CLI session of
 *  a steer-capable backend cannot actually insert. Showing "insert" there would
 *  make the label lie about what the button does — the one thing it must not
 *  do — so fall back to "interrupt and send" for CLI sessions. */
const midTurnSupported = computed(() => {
  if (!agentsComposable.supportsMidTurn(identity.currentAgentId.value || '')) return false
  const transport = identity.currentTransport.value
  // Unknown transport: assume ACP, since a steer-capable backend defaults to it
  // (the button still fails safe — the backend declines and the UI reports it).
  return transport !== 'cli'
})
/** queueId (or id) of the queued bubble whose action request is in flight. */
const pendingActionBusy = ref('')
const messages = ref([])
const messageStore = createChatMessageStore(messages)
/** Rendered messages = persisted messages (pending messages already in messages.value with pending: true) */
const renderedMessages = computed(() => messages.value)
const inputDisabled = ref(false)
const loading = ref(false)
const currentAgent = computed(() => getAgent(identity.currentAgentId.value) || null)
const inputBarRef = ref(null)
const messageListRef = ref(null)
const metadataModal = ref({
  data: {},
  backend: '',
  createdAt: '',
  relatedFile: '',
  messageId: null,
  sessionId: '',
  ftsIndexed: false,
  vecIndexed: false
})
const metadataDrawer = useTabDrawer('chat')
const forkAgentSelectorDrawer = useTabDrawer('chat', { autoRestore: false })
const forkPending = ref(null) // { sessionId, beforeMessageId }
const toast = useToast()
const acpSyncing = ref(false)
const acpSession = useAcpSession({ currentAgentId: identity.currentAgentId })

async function handleSyncAcpSession() {
  const sid = identity.currentSessionId.value
  if (!sid) return

  // 同步会重写历史记录，可能与现有消息有出入，需用户确认后才执行。
  const confirmed = await dialog.confirm(t('chat.acpSession.syncConfirm'), {
    title: t('chat.acpSession.syncConfirmTitle'),
    dangerous: true,
  })
  if (!confirmed) return

  acpSyncing.value = true
  // 同步期间禁用输入：避免用户在 LoadSession 回放窗口内发消息，导致回复通知被
  // 改路由进回放缓冲（而非实时流）。
  const prevInputDisabled = inputDisabled.value
  inputDisabled.value = true
  try {
    const res = await acpSession.acpSyncSession(sid)
    if (res) {
      await session.loadHistory(false, true, true)
      toast.show(t('chat.acpSession.synced', { count: res.added }), {
        type: res.added > 0 ? 'success' : 'info',
        icon: '🔄',
      })
    }
  } finally {
    inputDisabled.value = prevInputDisabled
    acpSyncing.value = false
  }
}

const dialog = useDialog()
const notification = useNotification()
const autoSpeech = useAutoSpeech()
const theme = inject('theme', ref('light'))
const { openFilePath } = useFilePathAnnotation()
const quoteDetail = useQuoteDetail()

async function handleFileTagClick(fileEntry) {
    // A quote card routes here through the shared file-tag-click event (the
    // quote branch is taken first so a quote never reaches path resolution).
    if (typeof fileEntry !== 'string' && fileEntry?.kind === 'quote') {
        handleSentQuoteClick(fileEntry)
        return
    }
    // AttachmentTags emits the full FileEntry; history file cards may pass a path string.
    const filePath = typeof fileEntry === 'string' ? fileEntry : fileEntry?.path
    const startLine = typeof fileEntry === 'string' ? undefined : fileEntry?.startLine
    const endLine = typeof fileEntry === 'string' ? undefined : fileEntry?.endLine
    if (filePath) {
        // Attachment paths from backend are absolute; strip projectRoot prefix
        // so openFilePath doesn't treat in-project files as external.
        const relPath = relativizeProjectPath(filePath, store.state.projectRoot)
        // openFilePath decides the destination tab itself (file → view, dir → browse).
        await openFilePath(relPath, startLine, endLine, 'chat')
    }
}

/**
 * Quote a whole chat message (the meta bar's 引用 button).
 *
 * The captured text is what the user reads: for an assistant reply that is
 * extractSpeakableText (which skips tool/thinking noise), for a user message its
 * content. Staging it here — rather than building a message — puts it on the
 * exact same path as a selection quote: a card in the input, clickable into the
 * detail drawer.
 */
function handleQuoteMessage(msg) {
    if (!msg) return
    const isUser = msg.role === 'user'
    const text = (isUser
        ? (extractSpeakableText(msg.blocks || []) || msg.content || '')
        : (extractSpeakableText(msg.blocks || []) || msg.summary || '')).trim()
    if (!text) return

    const id = msg.id !== undefined && msg.id !== null ? Number(msg.id) : NaN
    addStagedQuote({
        text,
        filePath: '',
        language: '',
        startLine: 0,
        endLine: 0,
        sourceKind: 'message',
        // Optimistic/local messages have no DB id; the quote is still valid, it
        // just cannot be addressed later (and the drawer hides the jump action).
        ...(Number.isFinite(id) && id > 0 ? { messageId: id } : {}),
    }, '')
}

/** Remove an attached reference entry (from AttachmentTags cards or AttachDrawer
 *  whole-file toggles). Ranged references remove only their own range. */
function handleRemoveAttachedEntry(entry) {
    const path = typeof entry === 'string' ? entry : entry?.path
    if (!path) return
    const startLine = typeof entry === 'string' ? undefined : entry?.startLine
    const endLine = typeof entry === 'string' ? undefined : entry?.endLine
    removeAttachedFileByPath(path, startLine, endLine)
    // A drag-drop / clipboard-paste upload also has a mirror entry in
    // pendingFiles (upload lifecycle) that feeds the send payload. Removing
    // only the attached card left the file in the message and kept the
    // now-empty tags row mounted. Whole-file removals clear that mirror too;
    // a ranged removal of a project file has no pending mirror to clear.
    if (startLine === undefined) removePendingByPath(path)
}

/**
 * Open the quote detail drawer for a staged (un-sent) quote card.
 *
 * The drawer shows the quoted content plus the annotation (editable), and a
 * jump-to-source button for file/forge quotes. It replaces the old behaviour of
 * navigating straight to the file, which gave no way to read or edit the note.
 */
function handleQuoteClick(q) {
    if (!q) return
    quoteDetail.openQuoteDetail(fromStagedQuote(q), { mode: 'staged' })
}

/**
 * Open the quote detail drawer for a quote that was already sent.
 *
 * The annotation is persisted on the message, so saving goes through the PATCH
 * endpoint (saveQuoteNote).
 */
function handleSentQuoteClick(entry) {
    if (!entry) return
    quoteDetail.openQuoteDetail(fromFileEntry(entry), { mode: 'sent' })
}

/** True while the annotation save request is in flight (disables the button). */
const quoteNoteSaving = ref(false)

/**
 * Persist an edited annotation.
 *
 * Staged quotes are edited locally — they have not been sent, so there is
 * nothing in the DB yet. Sent quotes go through the PATCH endpoint, addressed
 * by the message id and the quote's stable id.
 */
async function saveQuoteNote(note) {
    const q = quoteDetail.quote.value
    if (!q) return

    if (quoteDetail.mode.value === 'staged') {
        updateStagedQuoteNote(q.id, note)
        quoteDetail.close()
        return
    }

    if (!q.messageId || !q.id) return
    quoteNoteSaving.value = true
    try {
        await apiPatch('/api/ai/chat/quote', {
            sessionId: identity.currentSessionId.value,
            messageId: q.messageId,
            quoteId: q.id,
            note,
        })
        // Keep the open drawer in sync so `dirty` resets and a second edit
        // starts from the saved value.
        quoteDetail.quote.value = { ...q, note }
        // Reflect the change in the rendered message without a full reload.
        const msg = messages.value.find(m => String(m.id) === String(q.messageId))
        if (msg && Array.isArray(msg.files)) {
            const entry = msg.files.find(f => f?.kind === 'quote' && f.id === q.id)
            if (entry) entry.note = note
        }
    } catch (err) {
        appLog.w(TAG, 'save quote note failed', err)
        toast.show(t('quoteBar.sendFailed', { error: err?.message || String(err) }), { icon: '⚠️', type: 'error' })
    } finally {
        quoteNoteSaving.value = false
    }
}

/**
 * Jump from the drawer to the quote's source.
 *
 * Order matters: the most specific locator wins, because a quote can carry more
 * than one. A git-diff quote, for instance, has both a file path and a commit —
 * and the user who selected a hunk in a commit view means "that commit", not
 * "that file at its current state". Each branch therefore jumps to the
 * narrowest thing the quote can identify, and only falls through when that
 * locator is absent.
 *
 * All four cross-panel jumps reuse the EXISTING navigation channels rather than
 * inventing new ones: `navigate-to-commit` and `clawbench-open-task` are already
 * handled by App.vue, and the session jump goes through the same session-open
 * event the notification taps use.
 */
async function jumpToQuoteSource(q) {
    if (!q) return

    // 1. Commit: open the git history at that commit (not the file, which is
    //    what the path alone would open).
    if (q.commitSha) {
        window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha: q.commitSha } }))
        return
    }

    // 2. Task: open the scheduled task's detail view.
    if (q.taskId) {
        window.dispatchEvent(new CustomEvent('clawbench-open-task', {
            detail: { taskId: q.taskId, executionId: q.executionId },
        }))
        return
    }

    // 3. Session + message: switch to the session and scroll to the message.
    //    The request is parked in module state because the session may still
    //    have to load (and may be a different project) — see useMessageNavigation.
    if (q.sessionId) {
        if (q.messageId) setPendingMessageNavigation(q.sessionId, q.messageId)
        window.dispatchEvent(new CustomEvent('clawbench-open-session', {
            detail: { sessionId: q.sessionId },
        }))
        return
    }

    // 4. External address (a forge issue/PR, a CI run, a comment anchor).
    if (q.url) {
        openExternalUrl(q.url)
        return
    }

    // 5. A file: open it at its line range.
    if (!q.filePath) return
    const relPath = relativizeProjectPath(q.filePath, store.state.projectRoot)
    await openFilePath(relPath, q.startLine, q.endLine, 'chat')
}

const { planEntries, planCollapsed, planHasUpdate, togglePlanCollapse } = usePlanProgress()

const render = useChatRender({ messages, theme, currentSessionId: identity.currentSessionId })

/** Look up the tool_use block from the live messages array by msgId + blockIdx */
function findToolBlock({ msgId, blockIdx }) {
  const msg = messages.value.find(m => String(m.id) === msgId)
  if (!msg || !msg.blocks) return null
  const block = msg.blocks[blockIdx]
  return (block && block.type === 'tool_use') ? block : null
}

const {
  drawer: toolDetailDrawer,
  isOpen: toolDetailIsOpen,
  toolDetailData,
  toolDetailOverlay,
  activeToolOverlay,
  handleShowToolDetail,
  handleOverlayRetryClick,
  fetchToolCallDetail,
  handleFileOpenInOverlay,
  closeOverlay: closeToolDetailOverlay,
} = useToolDetailDrawer({
  chatRender: render,
  tabId: 'chat',
  onFileOpen: async (path, lineStart, lineEnd, lineRanges) => {
    // openFilePath decides the destination tab itself (file → view, dir → browse).
    await openFilePath(path, lineStart, lineEnd, 'chat', lineRanges)
  },
  findLiveBlock: (ids) => findToolBlock(ids),
  // Ask cards are keyed per (session, card) so an answer typed in the drawer and
  // one typed in the list are the SAME answer. Without this the drawer falls back
  // to the 'no-session' key and the two views would keep divergent copies.
  sessionId: () => identity.currentSessionId.value,
})

// Thinking overlay removed — thinking blocks now expand/collapse inline
// Debounce map for onToolUpdate fetches — max one fetch per 3s per tool
const toolUpdateFetchDebounce = new Map()

// Reliable app foreground/background signal (Android JS bridge + fallback).
const { appInForeground } = useAppForeground()

const session = useChatSession({
  currentSessionId: identity.currentSessionId,
  messages,
  dispatch: messageStore.dispatch,
  loading,
  inputDisabled,
  blockTasks: render.blockTasks,
  blockAskQuestions: render.blockAskQuestions,
  expandedTools: render.expandedTools,
  onParseAssistantContent: (content) => render.parseAssistantContent(content),
  onExtractScheduledTasks: (msgs) => render.extractScheduledTasks(msgs),
  onRenderUpdate: (forceFull) => render.updateRenderedContents(forceFull),
  onScrollBottom: (force) => scrollBottom(force),
  onDisconnectStream: () => stream.disconnectStream(),
  onOpen: () => emit('open'),
  onStreamDone: playNotificationSound,
  onEnsureStreamingPlaceholder: () => stream.ensureStreamingPlaceholder({ reuseExistingStreaming: true }),
  onResubscribeStream: (sid) => stream.resubscribe(sid),
})

// onStreamEnd: fires when current session stream completes with a reason
// - 'done': normal completion → play sound, auto-speech; queue sync handled by
//   useSessionManager's watch(loading) safety net (loading true→false triggers fetchQueue)
// - 'cancelled': user cancelled → clear locally for immediate UI response
// - 'error': error occurred → don't touch pending messages; backend preserves queue
async function onStreamEnd(reason) {
  if (reason === 'done') {
    playNotificationSound()
    if (autoSpeech.enabled.value) {
      const lastMsg = messages.value[messages.value.length - 1]
      if (lastMsg?.role === 'assistant') {
        const fullText = extractSpeakableText(lastMsg.blocks || [])
        if (fullText && lastMsg.id) {
          autoSpeech.speakMessage(lastMsg.id, fullText)
        } else {
          // Output ended but no speakable text — restore screen lock
          autoSpeech.onOutputEndNoSpeech()
        }
      } else {
        autoSpeech.onOutputEndNoSpeech()
      }
    } else {
      // Auto-speech off — restore screen lock since no TTS will play
      autoSpeech.onOutputEndNoSpeech()
    }
    // Recalculate chatUnread after stream completes — mark the current session
    // read first so the session list reflects the cleared unread state, then
    // refresh so chatUnread is false if no other sessions have unread messages.
    // Only mark read while the app is in the foreground: a session that
    // completes in the background (app paused, floating window showing) must
    // keep its unread badge so the floating window displays it.
    const sid = identity.currentSessionId.value
    if (sid && appInForeground.value) {
      await session.markSessionRead(sid).catch(() => {})
    }
    loadSessionsOnce()
    // Refresh git branch — AI agent may have checked out a different branch
    store.loadGitBranch().catch(() => {})
  } else if (reason === 'cancelled') {
    // Backend already cleared queue; clear locally for immediate UI response
    messageStore.dispatch({ type: 'clear_pending' })
    // Restore screen lock — output was cancelled, no TTS will play
    autoSpeech.onOutputEndNoSpeech()
    // User was viewing this session while cancelling — clear its unread badge.
    // Guarded on foreground: a cancellation arriving while the app is paused
    // must leave the badge intact for the floating window.
    const sid = identity.currentSessionId.value
    if (sid && appInForeground.value) {
      session.markSessionRead(sid).catch(() => {})
    }
    // Refresh git state — agent may have modified files before cancellation
    store.loadGitBranch().catch(() => {})
  }
  // 'error': don't touch pending messages — backend preserves queue
  if (reason === 'error') {
    // Restore screen lock — output errored, no TTS will play
    autoSpeech.onOutputEndNoSpeech()
    // Refresh git state — agent may have modified files before error
    store.loadGitBranch().catch(() => {})
  }
}

// Suppress screen lock when AI output starts with auto-speech enabled.
// Using watch instead of calling onOutputStart() at each loading=true site
// ensures all output entry points are covered (sendMessage, switchSession,
// loadHistory for running session, etc.).
watch(loading, (newVal, oldVal) => {
  if (newVal && !oldVal) {
    autoSpeech.onOutputStart()
  }
})

const stream = useChatStream({
  messages,
  dispatch: messageStore.dispatch,
  currentSessionId: identity.currentSessionId,
  currentBackend: identity.currentBackend,
  loading,
  onRenderNeeded: (forceFull) => render.updateRenderedContents(forceFull),
  onScrollBottom: (force) => scrollBottom(force),
  onLoadHistory: () => session.loadHistory(false),
  onMessage: () => emit('message'),
  onOpen: () => emit('open'),
  isOpen: toRef(props, 'active'),
  onParseAssistantContent: (content) => render.parseAssistantContent(content),
  onToast: (msg, opts) => toast.show(msg, opts),
  onNotification: (title, opts) => notification.show(title, opts),
  onStreamEnd,
  onReplayDone: () => { inputDisabled.value = false },
  onFileModified: (filePath) => {
    // Chat-driven file refresh: when AI's Write/Edit tool completes,
    // refresh the file preview if the modified file is currently being viewed.
    // This is a defense-in-depth mechanism alongside the fsnotify-based file watcher.
    const currentFilePath = store.state.currentFile?.path

    // Path matching: tool paths may be relative, absolute (project-internal or
    // external), or have "./" prefixes. sameFilePath normalizes separators,
    // relativizes project paths under the project root, then suffix-matches
    // on "/" boundaries (bare basenames match only by full equality).
    const isMatch = sameFilePath(filePath, currentFilePath || '', store.state.projectRoot)

    if (isMatch && currentFilePath) {
      // refreshCurrentFile handles both file content and directory listing
      refreshCurrentFile({ loadDir: true, clearOnError: true })
    } else {
      // File not currently viewed, but still refresh directory listing
      const currentDir = store.state.currentDir
      if (currentDir !== undefined) {
        store.loadFiles(currentDir, false, 0, true)
      }
    }
  },
  onToolResult: (toolId) => {
    // Tool finished — if overlay is showing this tool, fetch final output immediately
    if (activeToolOverlay.value) {
      const block = findToolBlock(activeToolOverlay.value)
      if (block && block.id === toolId) {
        fetchToolCallDetail(block.id, activeToolOverlay.value.msgId, block)
      }
    }
  },
  onToolUpdate: (toolId) => {
    // Tool status/summary changed during streaming — fetch interim output
    if (!activeToolOverlay.value) return
    const block = findToolBlock(activeToolOverlay.value)
    if (!block || block.id !== toolId || block.done) return
    // Debounce: max once per 3s per tool
    if (toolUpdateFetchDebounce.has(toolId)) return
    toolUpdateFetchDebounce.set(toolId, setTimeout(() => {
      toolUpdateFetchDebounce.delete(toolId)
      if (!activeToolOverlay.value) return
      const currentBlock = findToolBlock(activeToolOverlay.value)
      if (currentBlock && currentBlock.id === toolId && !currentBlock.done) {
        fetchToolCallDetail(toolId, activeToolOverlay.value.msgId, currentBlock)
      }
    }, 3000))
  },
})

const { pendingFiles, attachedFiles, addAttachedFile, removeAttachedFile, removePendingByPath, cleanupPreviewUrls, clearPendingFiles } = useFileUpload()
const { stagedQuotes, addStagedQuote, removeStagedQuote, updateStagedQuoteNote, clearAll, removeAttachedFileByPath, snapshotAttachments, restoreAttachments, discardAttachmentDraft } = useChatContext()

const manager = useSessionManager({
  messages,
  dispatch: messageStore.dispatch,
  loading,
  switchSessionCore: session.switchSession,
  createSessionCore: session.createSession,
  archiveSessionCore: session.archiveSession,
  destroySessionCore: session.destroySession,
  continueFromExecutionCore: session.continueFromExecution,
  forkSessionCore: session.forkSession,
  rewindSessionCore: session.rewindSession,
  checkContinueSessionCore: session.checkContinueSession,
  disconnectStream: stream.disconnectStream,
  updateRenderedContents: (forceFull) => render.updateRenderedContents(forceFull),
  clearInputState: () => {
    // Save the typed text (per-session draftCache) before clearing.
    inputBarRef.value?.saveDraft()
    // Snapshot attachments + staged quotes under the session we're leaving so
    // they can be restored when the user switches back. Also fold in pending
    // uploads that finished uploading (they hold a server path) — their
    // blob preview URLs are dropped with clearPendingFiles, but the attached
    // file ref (path) must survive the round-trip.
    const leavingSessionId = getSessionId()
    if (leavingSessionId) {
      const pendingPaths = pendingFiles.value.filter(f => f.path && !f.uploading).map(f => f.path)
      for (const p of pendingPaths) addAttachedFile(p)
    }
    snapshotAttachments(leavingSessionId)
    clearAll()
    inputBarRef.value?.clearInputPreserveDraft()
    clearPendingFiles()
  },
  // Restore attachments/quotes after the switch completes (currentSessionId
  // now points at the target session). Also carries cleanupDraft so
  // archived/destroyed sessions drop their attachment snapshot.
  restoreInputState: Object.assign(() => {
    restoreAttachments(getSessionId())
  }, {
    cleanupDraft: (sessionId) => discardAttachmentDraft(sessionId),
  }),
  scrollBottom: (force) => scrollBottom(force),
})

// Register identity actions — all paths now go through manager
manager.registerIdentityActions({
  sendMessage: (text) => sendMessage(text),
  openChatPanel: () => emit('open'),
})

const swipeSession = useSwipeSession({
  currentSessionId: identity.currentSessionId,
  switchSession: manager.switchSession,
})

const showPositionIndicator = computed(() =>
  swipeSession.sessionIndex.value >= 0 && swipeSession.sessionTotal.value > 1
)

const capsuleSliderStyle = computed(() => {
  const total = swipeSession.sessionTotal.value
  const idx = swipeSession.sessionIndex.value
  if (total <= 1 || idx < 0) return {}
  const trackWidth = 80
  const sliderWidth = Math.max(6, trackWidth / total)
  const maxOffset = trackWidth - sliderWidth
  const left = total > 1 ? (idx / (total - 1)) * maxOffset : 0
  return {
    width: `${sliderWidth}px`,
    left: `${left}px`,
  }
})

provide('chatRender', {
  renderTextBlock: render.renderTextBlock,
  formatMessageTime: render.formatMessageTime,
  toolCallSummary: render.toolCallSummary,
  formatToolInput: render.formatToolInput,
  truncate: render.truncate,
  hasImagesInContent: render.hasImagesInContent,
})
provide('chatSession', { getAgentBackend, getAgentName, sessionId: () => identity.currentSessionId.value })
// openFilePath (via open-file-overlay / open-file-manager events) already routes to
// the correct tab (file → view, dir → browse), so this is a no-op to avoid overriding.
provide('chatUI', { navigateToFileViewer: () => {} })
provide('autoSpeech', autoSpeech)

// 子抽屉的视觉隐藏由 useTabDrawer.effectiveOpen 自动处理（切换 tab 时
// effectiveOpen 变 false，openRef 保留原值），不需要在 active 变化时
// 手动清 openRef，否则切回 chat tab 后抽屉不会恢复。
// 面板打开时刷新渲染（修复 display:none 期间的过时布局状态）
// immediate: true 确保首次挂载时（active 已为 true）也会加载历史记录
//
// 首次打开（应用启动 / 首次进入 chat tab）: forceScrollBottom=true ——
// 此时没有既有滚动位置（DOM 是新建的，scrollTop=0），若用 false 会停在
// 消息列表顶部。历史版本首次加载强制滚到底部，tab 重构时被统一改成 false
// （为 tab 重开保留位置），漏掉了首次打开路径。
// tab 重开（active false→true，已加载过）: forceScrollBottom=false ——
// 保留用户上次的滚动位置（DOM 用 v-show 保留），仅在用户本来就靠近底部时
// 跟随新内容滚到底部。
let hasLoadedOnce = false
watch(() => props.active, async (val) => {
  if (val) {
    const isFirstOpen = !hasLoadedOnce
    // Open/Re-open: load history (with overlay, skip if unchanged) and fix stale layout state from v-show display:none
    // skipIfUnchanged=true preserves scroll position when no new messages arrived while tab was hidden
    await session.loadHistory(isFirstOpen, true, true)
    hasLoadedOnce = true
  }
}, { immediate: true })

// Reactively update tool overlay content as block.output/done/status changes during streaming
watch(
  () => {
    if (!activeToolOverlay.value) return null
    const block = findToolBlock(activeToolOverlay.value)
    if (!block) return null
    return { output: block.output, done: block.done, status: block.status, input: block.input, name: block.name, summary: block.summary, display_name: block.display_name }
  },
  (data) => {
    if (data === null || !toolDetailIsOpen.value) return
    const { formatToolInput } = render
    const hasInput = data.input && Object.keys(data.input).length > 0
    toolDetailData.value.outputHtml = data.output ? formatToolOutput(data.output, data.name) : toolDetailData.value.outputHtml
    toolDetailData.value.status = data.status || ''
    toolDetailData.value.done = !!data.done
    toolDetailData.value.inputHtml = hasInput ? formatToolInput(data.input, data.name, { done: data.done, status: data.status, output: data.output }) : toolDetailData.value.inputHtml
    toolDetailData.value.summary = data.summary || toolDetailData.value.summary
  }
)

// Clean up overlay state when overlay closes
watch(() => toolDetailIsOpen.value, (show) => {
  if (!show) {
    activeToolOverlay.value = null
    // Clear tool update debounce timers
    for (const timer of toolUpdateFetchDebounce.values()) clearTimeout(timer)
    toolUpdateFetchDebounce.clear()
  }
})

async function handleShowAgentSelector() {
  await agentsComposable.loadAgents()
  // Always open the selector, even with a single agent — a direct create
  // here would be a one-tap action that is easy to mis-tap on mobile,
  // creating an empty session. Requiring an explicit selection prevents it.
  // 始终打开选择器（哪怕只有一个智能体）——直接创建是一键动作，
  // 移动端容易误触生成空会话；强制选择可避免误建。
  identity.openAgentSelector()
}

function handleSwitchModel(model) {
  identity.currentModelId.value = model.id
  identity.currentModelName.value = model.name
  // Persist model selection immediately so it survives page reload
  persistSessionUpdate({ modelId: model.id })
}

function handleSwitchThinkingEffort(level) {
  if (!level || level === identity.thinkingEffortState.currentId.value) return
  identity.thinkingEffortState.currentId.value = level
  // Resolve and set the friendly name for display
  const levelObj = identity.thinkingEffortState.available.value.find(l => l.id === level)
  identity.thinkingEffortState.currentName.value = levelObj?.name || level
  // Persist thinking effort selection immediately so it survives page reload
  persistSessionUpdate({ thinkingEffort: level })
}

function handleSwitchMode(mode) {
  if (!mode?.id || mode.id === identity.modeState.currentId.value) return
  identity.modeState.currentId.value = mode.id
  identity.modeState.currentName.value = mode.name || mode.id
  // Persist mode selection immediately so it survives page reload
  persistSessionUpdate({ modeId: mode.id })
}

function handleSwitchTransport(transport) {
  identity.currentTransport.value = transport
  // Persist transport selection immediately so it survives page reload
  persistSessionUpdate({ transport })
  // When switching from ACP to CLI for this session, clear ACP-specific state.
  if (transport === 'cli') {
    identity.clearModeState()
    identity.clearCommandState()
    identity.clearThinkingEffortState()
  }
}

function handleOpenUserMsgIndex() {
  messageListRef.value?.toggleUserMsgIndex()
}

async function handleForkFromMessage(msg) {
  const sid = identity.currentSessionId.value
  if (!sid) return
  if (await dialog.confirm(t('chat.session.forkFromMessageConfirm'))) {
    messageListRef.value?.closeUserMsgIndex()
    await agentsComposable.loadAgents()
    // If only one agent, fork directly (inherits source session's agent)
    if (agentsList.value.length <= 1) {
      await manager.forkSession(sid, msg.id)
      return
    }
    // Multiple agents — show selector with source session's agent pre-selected
    forkPending.value = { sessionId: sid, beforeMessageId: msg.id }
    forkAgentSelectorDrawer.open()
  }
}

function handleForkAgentSelect(agentId) {
  forkAgentSelectorDrawer.close()
  const pending = forkPending.value
  if (!pending) return
  forkPending.value = null
  manager.forkSession(pending.sessionId, pending.beforeMessageId, agentId)
}

// Rewind/回溯: truncate the current session at this assistant message, reset the
// AI-side session so the conversation restarts here, and pre-fill the input box
// with the first removed user message for re-editing. Nothing is auto-sent.
async function handleRewindFromMessage(msg) {
  const sid = identity.currentSessionId.value
  if (!sid) return
  const ok = await dialog.confirm(t('chat.session.rewindFromMessageConfirm'), { dangerous: true })
  if (!ok) return
  messageListRef.value?.closeUserMsgIndex()
  const restoredText = await manager.rewindSession(sid, msg.id)
  if (restoredText) {
    inputBarRef.value?.prefillInput(restoredText)
  }
}

/** Persist session-scoped settings (mode, thinkingEffort, model, transport)
 *  immediately via PATCH so they survive page reload without sending a message. */
function persistSessionUpdate(fields) {
  const sid = identity.currentSessionId.value
  if (!sid) return
  const url = `/api/ai/session/update?session_id=${encodeURIComponent(sid)}`
  fetch(url, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(fields),
  }).catch(() => { /* best effort — next POST /api/ai/chat will also persist */ })
}

async function sendMessage(text) {
    const inputText = text !== undefined ? text : (inputBarRef.value?.inputText?.trim() || '')

     // Quotes ride as structured attachment cards, not as text baked into the
     // message. Materialise the staged quotes into FileEntry form here (the one
     // conversion point) so both the direct and the enqueue path carry them —
     // the enqueue path used to rely on the quotes already being in `inputText`,
     // which silently dropped them whenever the session was busy.
     const quoteEntries = materializeQuotes(stagedQuotes.value)

     const hasFiles = pendingFiles.value.length > 0 || attachedFiles.value.length > 0 || quoteEntries.length > 0

     if ((!inputText && !hasFiles) || inputDisabled.value) return false

     // A pending upload has no server path yet. Sending it would silently submit
     // an empty attachment and let the request complete against the next draft.
     if (pendingFiles.value.some(file => file.uploading)) {
       toast.show(t('chat.attach.uploading'), { icon: '⚠️', type: 'info' })
       return false
     }

     // If AI is generating, enqueue the message instead of sending immediately
     if (loading.value) {
       // Capture file arrays before clearing (they're passed by reference)
       const capturedAttached = [...attachedFiles.value, ...quoteEntries]
       const capturedPending = pendingFiles.value.filter(f => f.path).map(f => ({ path: f.path, isDir: false }))
       // Clear input state synchronously so user sees immediate feedback
       clearAll()
       resetQuotePin()
       inputBarRef.value?.clearInput()
       clearPendingFiles()
       // Push a pending user message and enqueue it. The backend handles the
       // "session not running" race internally (B2 self-heal), so no
       // needs_start/resubmit round-trip is needed here. Shared with the
       // AskUserQuestion-card path for identical enqueue behavior.
       try {
         await enqueueAndMaybeStart({
           sessionId: identity.currentSessionId.value,
           text: inputText || '',
           attachedFiles: capturedAttached,
           pendingFiles: capturedPending,
           pushMessage: (msg) => messageStore.dispatch({ type: 'optimistic_push', msg }),
           onPendingRendered: () => { render.updateRenderedContents(); scrollBottom(true) },
           enqueue: (sid, text, attached, pending, qid) => manager.enqueueMessage(sid, text, attached, pending, qid),
         })
         // The attachments were queued with the message — drop the snapshot.
         discardAttachmentDraft(identity.currentSessionId.value)
       } catch (err) {
         // Enqueue failed (network down / 5xx) — the message was not delivered.
         // enqueueMessage already toasted queueFailed; here we just restore the
         // input so the user's text isn't lost.
         appLog.w(TAG, 'enqueue path failed, restoring input', err)
         try {
           inputBarRef.value?.restoreInput(inputText || '')
         } catch (e) {
           appLog.e(TAG, 'restoreInput failed', e)
         }
         return false
       }
       return true
     }

    // Build file paths and entries from attachedFiles + the materialised quote
    // cards (unified channel). buildSendPayload preserves kind/url on URL
    // attachments and routes line-range and quote entries through the entries
    // channel only (never filePaths) or the backend would strip their ranges or
    // reject an empty path.
    const { allFiles, filePaths } = buildSendPayload(pendingFiles.value, [...attachedFiles.value, ...quoteEntries])

    // Clear input state before async request
    clearAll()
    resetQuotePin()
    inputBarRef.value?.clearInput()
    clearPendingFiles()

    try {
      await sendMessageNow(inputText, filePaths, allFiles)
      // The attachment/quotes were consumed by this message — drop the
      // snapshot so switching away and back doesn't resurrect them
      // (mirrors clearInput() deleting the text draft on send).
      discardAttachmentDraft(getSessionId())
    } catch (err) {
      // Send failed (network down / 5xx) — the message was not delivered.
      // Restore the input so the user's text isn't lost. sendMessageNow's
      // catch has already toasted the failure; this catch only decides
      // whether the typed text comes back.
      appLog.w(TAG, 'sendMessage failed, restoring input', err)
      try {
        inputBarRef.value?.restoreInput(inputText)
      } catch (e) {
        appLog.e(TAG, 'restoreInput failed', e)
      }
      // Report the failure to the caller. Returning rather than throwing keeps
      // the fire-and-forget call sites (@send, the registered identity action)
      // free of unhandled rejections, while still letting an ask-card answer
      // learn that it must become answerable again.
      return false
    }
    return true
}

/** Actually send a message to the backend (no queue check). */
async function sendMessageNow(text, filePaths, files) {
    // Pre-generate a pending- ID in case the session is already running and
    // the message gets enqueued. This avoids in-place ID mutation (v-for key
    // instability) and ensures the backend receives queueId for precise matching.
    const pendingId = `pending-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
    const optimisticMsg = {
      role: 'user',
      id: pendingId,
      queueId: pendingId,
      content: text || '',
      blocks: text ? [{ type: 'text', text: text || '' }] : [],
      filePath: filePaths.length > 0 ? filePaths[0] : '',
      files: (files || []).map(f => typeof f === 'string' ? { path: f, isDir: false } : f),
      createdAt: new Date().toISOString(),
      seq: nextClientSeq(),
    }
    // Guard this optimistic bubble against a stale loadHistory snapshot that
    // was fetched before the POST committed its row (see trackInFlightSend in
    // chatStreamUtils). rebuildFromDb keeps the bubble while the queueId is
    // tracked; the registry clears itself once a db_load contains the row.
    trackInFlightSend(pendingId)
    messageStore.dispatch({ type: 'optimistic_push', msg: optimisticMsg })

    render.updateRenderedContents()
    loading.value = true
    scrollBottom(true)

    try {
        const effectiveAgentId = identity.currentAgentId.value

        if (!identity.currentSessionId.value) {
            // No session yet — the user hasn't loaded a session. This shouldn't
            // happen during normal operation (loadHistory always sets currentSessionId).
            // Instead of letting the backend auto-create a ghost session, recover first.
            try { await session.loadHistory(true, false) } catch { /* best effort */ }
            if (!identity.currentSessionId.value) {
                throw new Error(gt('chat.session.requestFailed', { status: 'No session' }))
            }
        }
        const safeUrl = `/api/ai/chat?session_id=${encodeURIComponent(identity.currentSessionId.value)}`
        const resp = await fetch(safeUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text, queueId: pendingId, filePaths, files: files || [], agentId: effectiveAgentId, modelId: identity.currentModelId.value || undefined, thinkingEffort: identity.currentThinkingEffort.value || undefined, modeId: identity.currentModeId.value || undefined, transport: identity.currentTransport.value || undefined, clientId: localStorage.getItem('clawbench_client_id') || undefined }),
        })
        const data = await resp.json()
        if (!resp.ok) {
            const err = new Error(data.error || gt('chat.metadata.unknownError'))
            err.msgKey = data.msgKey
            throw err
        }
        // Update session ID if backend created a new one
        if (data.sessionId && !identity.currentSessionId.value) {
            identity.currentSessionId.value = data.sessionId
        }
        // Direct-send path: adopt the DB id immediately so this bubble sorts
        // with DB-backed messages instead of staying transient (huge sort
        // value) until the next loadHistory — otherwise it renders AFTER later
        // queued messages that already adopted their DB ids (misorder).
        if (data.msgId && !data.running) {
            messageStore.dispatch({ type: 'optimistic_adopt_id', id: pendingId, dbId: data.msgId })
        }
        // Session already running — another request is in progress
        if (data.running) {
            // The message was queued for the next turn (sending never joins the
            // running turn), so mark it pending: it waits for its own drain.
            const localIdx = messages.value.findLastIndex(
                (m) => m.role === 'user' && m.id === pendingId
            )
            if (localIdx !== -1) {
                messages.value[localIdx].pending = true
            }
            stream.connectStream(identity.currentSessionId.value, { reuseExistingStreaming: true })
            // Proactively sync ACP state for the running session
            if (effectiveAgentId && agentsComposable.supportsACP(effectiveAgentId)) {
                populateACPStateFromCache(effectiveAgentId)
            }
            return
        }
        stream.connectStream(identity.currentSessionId.value)
        // After connecting stream, proactively sync ACP state (mode, thinking, commands)
        // from the server cache. For ACP agents, the backend caches mode state after
        // the first prompt, but the frontend's clearModeState() during session switch
        // may have cleared availableModes before the SSE mode_update event arrives.
        // This ensures mode/thinking chips appear immediately.
        if (effectiveAgentId && agentsComposable.supportsACP(effectiveAgentId)) {
            populateACPStateFromCache(effectiveAgentId)
        }
    } catch (err) {
        // Surface the failure FIRST so no later cleanup step can swallow the
        // user-visible toast. dispatch/disconnectStream/autoSpeech below are
        // best-effort; if any of them throws, the error would otherwise skip
        // straight to sendMessage's catch (which restores the input text) and
        // the "发送失败" toast would never appear.
        toast.show(t('toast.sendFailed'), { icon: '⚠️', type: 'error' })
        appLog.e(TAG, 'sendMessageNow failed', err)
        // Remove the optimistically pushed user message on failure — its POST
        // never committed, so the in-flight guard must also be released.
        untrackInFlightSend(pendingId)
        messageStore.dispatch({ type: 'optimistic_remove', id: pendingId })
        stream.disconnectStream()
        loading.value = false
        // Restore screen lock on send failure — output won't proceed
        autoSpeech.onOutputEndNoSpeech()
        // Clear session ID on error to prevent using invalid session
        if (err.msgKey === 'SessionBackendNotFound' || err.msgKey === 'SessionNotFound') {
            identity.currentSessionId.value = ''
        }
        // Re-throw so callers (sendMessage) can restore the input box — a
        // failed send must not silently swallow the user's typed text.
        throw err
    }
}

/** Handle a tool-triggered message send (e.g. AskUserQuestion answer).
 *  If the AI stream is still running, enqueues the message for delivery after stream ends.
 *
 *  `cardKey` identifies the ask card the answer came from. Submitting an answer
 *  persists a `submitted` flag so the card still reads as answered after a
 *  reload; when the send does NOT go through that flag must be undone, or the
 *  user would be stuck with a card they can no longer answer. */
async function handleToolSendMessage(text, cardKey) {
    if (!text) return
    let delivered = true
    if (loading.value) {
      // Shared with the normal input path: push a pending user message and
      // enqueue it. The backend's B2 self-heal handles the session-ended race.
      // On failure, enqueueMessage already shows the toast and rolls back the
      // pending message.
      try {
        await enqueueAndMaybeStart({
          sessionId: identity.currentSessionId.value,
          text,
          attachedFiles: [],
          pendingFiles: [],
          pushMessage: (msg) => messageStore.dispatch({ type: 'optimistic_push', msg }),
          onPendingRendered: () => { render.updateRenderedContents(); scrollBottom(true) },
          enqueue: (sid, msg, attached, pending, qid) => manager.enqueueMessage(sid, msg, attached, pending, qid),
        })
      } catch {
        /* failure already surfaced by enqueueMessage */
        delivered = false
      }
    } else {
      delivered = await sendMessage(text)
    }
    // The answer never reached the backend — make the card answerable again,
    // keeping the selection and note so retrying is one tap.
    if (!delivered && cardKey) revertAskSubmission(cardKey)
}

function scrollBottom(force = false) {
    messageListRef.value?.scrollToBottom(force)
}

/**
 * Consume a pending "open this message" request once its session is loaded.
 *
 * A quote's jump may target a session that is not on screen yet (and may be in
 * another project), so the request is parked in module state by
 * jumpToQuoteSource and picked up here. It waits for the CURRENT session to be
 * the requested one before scrolling: acting earlier would search the wrong
 * session's message list and silently do nothing.
 *
 * `immediate` + watching the message count covers both arrival paths — the
 * session may already be open (messages present) or still loading (the watcher
 * fires when they land).
 */
watch(
    [() => identity.currentSessionId.value, () => messages.value.length],
    ([sid]) => {
        const req = pendingMessageNavigation.value
        if (!req || req.sessionId !== sid) return
        // Consume only once the target message actually exists in this session.
        // An optimistic or not-yet-paged message leaves the request pending, so
        // a later arrival can still satisfy it.
        const present = messages.value.some(m => m.id === req.messageId)
        if (!present) return
        consumePendingMessageNavigation()
        // The list renders on the next tick; scroll after it exists.
        nextTick(() => messageListRef.value?.scrollToMessage(req.messageId))
    },
    { immediate: true },
)

// Async render flush (throttled 300ms + rAF) grows the content height AFTER
// the initial scroll-to-bottom already ran. If the user has not scrolled away
// (shouldStayPinned — e.g. just switched into this session), force re-pin so
// the list stays glued to the bottom. If the user scrolled away, nothing happens.
function handleRenderFlush() {
    if (messageListRef.value?.shouldStayPinned?.()) scrollBottom(true)
}

async function handleLoadMore() {
    const el = messageListRef.value?.messagesRef
    if (!el) return
    const oldScrollHeight = el.scrollHeight
    const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    const wasAtBottom = distFromBottom < NEAR_BOTTOM_PX
    // When not at the bottom, anchor the viewport to the first visible message
    // instead of relying on the pure scrollHeight delta — prepended content plus
    // async growth (Mermaid, lazy original text) can shift the delta and drift
    // the view. Same anchoring idea as ChatMessageList's array-replacement watch.
    let anchorKey = ''
    let anchorOffset = 0
    if (!wasAtBottom) {
      const items = el.querySelectorAll('.chat-messages-list > .chat-message')
      const containerRect = el.getBoundingClientRect()
      for (const item of items) {
        const rect = item.getBoundingClientRect()
        if (rect.bottom > containerRect.top && rect.top < containerRect.bottom) {
          anchorKey = item.getAttribute('data-msg-key') || ''
          anchorOffset = rect.top - containerRect.top
          break
        }
      }
    }
    await session.loadMoreMessages()
    // Wait for DOM update + one frame for async rendering (Mermaid, KaTeX)
    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    if (wasAtBottom) {
      el.scrollTop = el.scrollHeight
      return
    }
    if (anchorKey) {
      const items = el.querySelectorAll('.chat-messages-list > .chat-message')
      for (const item of items) {
        if (item.getAttribute('data-msg-key') === anchorKey) {
          const rect = item.getBoundingClientRect()
          const containerRect = el.getBoundingClientRect()
          const desiredTop = containerRect.top + anchorOffset
          el.scrollTop += rect.top - desiredTop
          return
        }
      }
    }
    // Fallback: anchor message gone — scrollHeight delta
    const newScrollHeight = el.scrollHeight
    el.scrollTop = newScrollHeight - oldScrollHeight
}

/** Handle remove-pending event from ChatMessageItem.
 *  The event passes the pending message's queueId (msg.id).
 *  Passes it directly to the manager for backend DELETE. */
function handleRemovePending(queueId) {
    manager.handleRemovePending(queueId)
}

/** Single adaptive action on a queued bubble. The backend capability decides
 *  what it does; the button label already told the user which, so here we only
 *  route to the matching endpoint. */
async function handlePendingAction(queueId) {
    if (!queueId || pendingActionBusy.value) return
    pendingActionBusy.value = String(queueId)
    try {
        await manager.handlePendingAction(
            String(queueId),
            midTurnSupported.value ? 'insert' : 'interrupt',
        )
    } finally {
        pendingActionBusy.value = ''
    }
}

function showMetadata(msg) {
    metadataModal.value.data = msg.metadata || {}
    metadataModal.value.backend = msg.backend || ''
    metadataModal.value.createdAt = msg.createdAt || ''
    metadataModal.value.relatedFile = (msg.files && msg.files.length > 0) ? msg.files[0].path || msg.files[0] : ''
    metadataModal.value.messageId = msg.id || null
    // Streaming/finalized placeholders don't carry a sessionId (created
    // client-side). Fall back to the current session so the detail modal
    // always shows one.
    metadataModal.value.sessionId = msg.sessionId || identity.currentSessionId.value || ''
    metadataModal.value.ftsIndexed = false
    metadataModal.value.vecIndexed = false
    metadataDrawer.open()

    // Async: fetch FTS/Vec index status from RAG store
    if (msg.id) {
      apiGet(`/api/rag/message-index-status?id=${msg.id}`).then((data) => {
        metadataModal.value.ftsIndexed = !!data.fts_indexed
        metadataModal.value.vecIndexed = !!data.vec_indexed
      }).catch(() => {
        // RAG not configured or message not found — leave as false
      })
    }
}

// Wire up WS event handler for session_update
const { onEvent } = useGlobalEvents()
const removeEventHandler = onEvent((event, data) => {
    if (event === 'session_update') {
        session.onSessionEvent(data)
    }
})

// Handle summary_update from WebSocket (dispatched by useGlobalEvents as custom event)
function handleSummaryUpdate(e) {
    const data = e.detail
    if (!data?.targetID) return
    const msgId = String(data.targetID)
    const msg = messages.value.find(m => String(m.id) === msgId)
    if (!msg) return
    const atBottom = messageListRef.value?.isAtBottom() ?? true
    applySummaryUpdate(msg, data.summary, data.summaryCards, atBottom)
}

// Toggle summary/original view for a message
async function handleToggleSummary(msgId) {
    const msg = messages.value.find(m => m.id === msgId)
    if (!msg) return
    // Historical messages with no summary: generate one on demand first.
    if (msg.summary == null || msg.summary === '') {
        await generateMessageSummary(msg)
        return
    }
    const mode = normalizeDisplayMode(localConfig.messageDisplayMode)
    const showingNow = isShowingSummary(msg, mode, { isLastAssistant: isLastAssistantMessage(messages.value, msg) })
    // Switching FROM summary TO original: if blocks weren't loaded (content stripped by backend), fetch the full message.
    // Reset _loadAttempted so a previously failed load can be retried on explicit user action.
    if (showingNow && (!msg.blocks || msg.blocks.length === 0)) {
        msg._loadAttempted = false
        await ensureMessageContent(msg)
    }
    // Record the user's explicit preference. If they were showing the summary,
    // toggle to original; otherwise toggle to summary.
    msg.showingSummary = !showingNow
}

// Generate a reading summary for a historical message on demand, then switch to
// the summary view so the freshly-generated summary is displayed.
async function generateMessageSummary(msg) {
    if (msg._summarizing) return
    msg._summarizing = true
    try {
        const data = await apiPost(`/api/rag/message/summarize?id=${msg.id}`, {})
        if (data.summary) {
            msg.summary = data.summary
            if (data.summaryCards) msg.summaryCards = data.summaryCards
            msg.showingSummary = true
        }
    } catch (err) {
        appLog.w(TAG, 'failed to generate summary on demand', err)
    } finally {
        msg._summarizing = false
    }
}

// Lazily fetch the full message content when the original view is requested but
// blocks were omitted (backend strips content for summarized messages).
async function ensureMessageContent(msg) {
    if (msg._loadingOriginal) return
    msg._loadingOriginal = true
    try {
        const full = await apiGet(`/api/rag/message?id=${msg.id}`)
        const { blocks } = render.parseAssistantContent(full.content || '')
        msg.blocks = blocks
        if (full.files) msg.files = full.files
        // The newly filled blocks grow the container height, but the browser
        // keeps the old scrollTop — so a force-scrolled view (session switch
        // back into this chat) ends up visually stuck mid-list. Re-sync once:
        // - at bottom (session switch): shouldStayPinned → pinned back to bottom
        // - user manually toggled original while reading: shouldStayPinned=false → keep position
        if (messageListRef.value?.shouldStayPinned?.()) scrollBottom(true)
    } catch (err) {
        appLog.w(TAG, 'failed to load original content', err)
    } finally {
        msg._loadingOriginal = false
        msg._loadAttempted = true
    }
}


// Reload/reopen the current session from the ActionBar refresh button.
// Delegates to session.handleManualRefresh which mirrors the WS reconnect
// resync flow (refresh runningSessions + branch on running state) but ALWAYS
// forces a loadHistory so every refresh re-renders against the authoritative
// server state — messages, stream subscription, mode/usage/commands all stay
// consistent with the backend.
const refreshingSession = ref(false)
async function handleRefreshSession() {
  if (refreshingSession.value || !identity.currentSessionId.value) return
  refreshingSession.value = true
  try {
    await session.handleManualRefresh()
  } catch (err) {
    appLog.w(TAG, 'failed to refresh session', err)
  } finally {
    refreshingSession.value = false
  }
}

// Reset a stuck ACP agent session: kill the connection so the next prompt
// starts a fresh agent session, then re-send the last user message to recover.
async function handleResetSession() {
    const sid = identity.currentSessionId.value
    if (!sid) return
    try {
        await apiPost('/api/ai/session/reset', { sessionId: sid })
        // Re-send the last persisted user message so the conversation continues
        // in the freshly-reset agent session.
        const lastUserMsg = [...messages.value].reverse().find(m => m.role === 'user' && !m.pending)
        if (lastUserMsg?.content) await sendMessage(lastUserMsg.content)
    } catch {
        toast.show(t('chat.contentBlocks.resetSessionFailed'), { icon: '⚠️', type: 'error' })
    }
}

// Resume a session from RAG search results (direct event, no detail drawer)
async function handleResumeSession({ sessionId, sessionTitle }) {
    if (!sessionId) return
    const confirmed = await dialog.confirm(
        t('chat.contentBlocks.ragResumeConfirm', { title: sessionTitle || t('chat.contentBlocks.ragUntitled') }),
        { title: t('chat.contentBlocks.ragResume'), confirmText: t('common.confirm') }
    )
    if (!confirmed) return
    try {
        const resp = await fetch('/api/ai/session/resume', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: sessionId }),
        })
        if (!resp.ok) {
            const data = await resp.json().catch(() => ({}))
            toast.show(data.error || t('chat.contentBlocks.ragResumeFailed'), { icon: '⚠️', type: 'error' })
            return
        }
        await session.switchSession(sessionId)
    } catch {
        toast.show(t('chat.contentBlocks.ragResumeFailed'), { icon: '⚠️', type: 'error' })
    }
}

// Desktop: Ctrl+Left/Right to switch sessions (always enabled, independent of swipeSession toggle)
function handleCtrlArrowSessionSwitch(e) {
  if (!props.keyboardActive) return
  const tag = e.target?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  if (e.target?.closest?.('.terminal-panel')) return
  if (!(e.ctrlKey || e.metaKey)) return
  if (e.key === 'ArrowLeft') {
    e.preventDefault()
    swipeSession.swipeToPrev()
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    swipeSession.swipeToNext()
  }
}

// Desktop: Ctrl+U/Cmd+U to jump to the next unread session
function handleJumpUnread(e) {
  if (!props.keyboardActive) return
  const tag = e.target?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  if (e.target?.closest?.('.terminal-panel')) return
  if (!(e.ctrlKey || e.metaKey)) return
  if (e.key !== 'u' && e.key !== 'U') return
  e.preventDefault()
  swipeSession.jumpToNextUnread().then(target => {
    if (!target) toast.show(t('chat.shortcutJumpUnread.none'), { icon: '📭', type: 'info', duration: 2500 })
  })
}

// Desktop: Ctrl+K/Cmd+K to open the global session list drawer
function handleOpenSessionList(e) {
  if (!props.keyboardActive) return
  const tag = e.target?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  if (e.target?.closest?.('.terminal-panel')) return
  if (!(e.ctrlKey || e.metaKey)) return
  if (e.key !== 'k' && e.key !== 'K') return
  e.preventDefault()
  identity.openSessionTab()
}

// Desktop: Ctrl+Delete to archive current session
function handleDeleteKey(e) {
  if (!props.keyboardActive) return
  if (e.key !== 'Delete' || !(e.ctrlKey || e.metaKey)) return
  inputBarRef.value?.handleArchive()
}

// Start one-time session load when component mounts
onMounted(() => {
    // Request notification permission on mount
    notification.requestPermission().catch(err => {
        appLog.w(TAG, 'Failed to request notification permission:', err)
    })

    session.loadSessionsOnce()
    window.addEventListener('clawbench-reconnect', session.handleWsReconnect)
    window.addEventListener('clawbench-summary-update', handleSummaryUpdate)
    document.addEventListener('keydown', handleCtrlArrowSessionSwitch)
    document.addEventListener('keydown', handleJumpUnread)
    document.addEventListener('keydown', handleOpenSessionList)
    document.addEventListener('keydown', handleDeleteKey)
})

// Cleanup preview URLs on unmount
onUnmounted(() => {
    removeEventHandler()
    cleanupPreviewUrls()
    stream.disconnectStream()
    // Clear tool update debounce timers
    for (const timer of toolUpdateFetchDebounce.values()) clearTimeout(timer)
    toolUpdateFetchDebounce.clear()
    window.removeEventListener('clawbench-reconnect', session.handleWsReconnect)
    window.removeEventListener('clawbench-summary-update', handleSummaryUpdate)
    document.removeEventListener('keydown', handleCtrlArrowSessionSwitch)
    document.removeEventListener('keydown', handleJumpUnread)
    document.removeEventListener('keydown', handleOpenSessionList)
    document.removeEventListener('keydown', handleDeleteKey)
    session.removeForegroundReadListener()
    notification.closeAll()
})
</script>

<style scoped>
.chat-panel-content {
  position: relative;
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* Session swipe indicator — floats at top of message area */
.session-switch-indicator {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-8) var(--space-4);
  background: var(--bg-primary);
  color: var(--text-primary);
  border-radius: 24px;
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  letter-spacing: 0.3px;
  position: absolute;
  top: 48px;
  left: 0;
  right: 0;
  justify-content: center;
  z-index: 10;
  max-width: 260px;
  margin: 0 auto;
  border: 1px solid var(--border-color);
  box-shadow: var(--shadow-md);
}

.session-indicator-row {
  display: flex;
  align-items: center;
  justify-content: center;
}

.session-indicator-text {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}

/* Position indicator — row 2 */
.session-indicator-position {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

/* Dots bar (<=15 sessions) */
.session-dots {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.session-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--text-hint);
  transition: all var(--duration-base) ease-out;
}

.session-dot.active {
  width: 6px;
  height: 6px;
  background: var(--accent-color);
}

/* Capsule progress bar (>15 sessions) */
.session-capsule {
  display: flex;
  align-items: center;
}

.session-capsule-track {
  width: 80px;
  height: 3px;
  border-radius: var(--radius-xs);
  background: var(--text-hint);
  position: relative;
}

.session-capsule-slider {
  position: absolute;
  top: 0;
  height: 3px;
  border-radius: var(--radius-xs);
  background: var(--accent-color);
  transition: left var(--duration-slow) ease-out;
}

/* Numeric label */
.session-position-count {
  font-size: var(--font-size-2xs);
  color: var(--text-hint);
  white-space: nowrap;
  min-width: 24px;
  text-align: center;
}

.session-switch-indicator.left {
  animation: indicator-slide-left 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
}

.session-switch-indicator.right {
  animation: indicator-slide-right 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
}

@keyframes indicator-slide-left {
  from {
    opacity: 0;
    transform: translateX(30px) scale(0.9);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

@keyframes indicator-slide-right {
  from {
    opacity: 0;
    transform: translateX(-30px) scale(0.9);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}

.session-indicator-enter-active {
  transition: opacity var(--duration-base) ease-out;
}

.session-indicator-leave-active {
  transition: opacity var(--duration-slow) ease-in, transform var(--duration-slow) ease-in;
}

.session-indicator-enter-from {
  opacity: 0;
}

.session-indicator-leave-to {
  opacity: 0;
  transform: scale(0.95);
}
</style>

<style>
/* Tool call empty state — unscoped so it works inside v-html */
.tool-call-loading {
  display: flex;
  justify-content: center;
  padding: 24px;
}
.tool-call-loading::after {
  content: '';
  width: 20px;
  height: 20px;
  border: 2px solid var(--border-color, #e5e7eb);
  border-top-color: var(--accent-color, #6366f1);
  border-radius: 50%;
  animation: tool-call-spin 0.6s linear infinite;
}
@keyframes tool-call-spin {
  to { transform: rotate(360deg); }
}
.tool-call-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-4);
  padding: var(--space-8) var(--space-6);
  color: var(--text-muted, #9ca3af);
}
.tool-call-empty-msg {
  font-size: var(--font-size-md);
  font-style: italic;
}
.tool-call-retry-btn {
  font-size: var(--font-size-sm);
  padding: var(--space-2) var(--space-6);
  border-radius: var(--radius-sm);
  border: 1px solid var(--border-color, #e5e7eb);
  background: var(--bg-secondary, #f3f4f6);
  color: var(--text-secondary, #6b7280);
  cursor: pointer;
  transition: all var(--duration-base);
}
@media (hover: hover) {
  .tool-call-retry-btn:hover {
    background: var(--bg-tertiary, #e5e7eb);
    color: var(--text-primary, #111827);
  }
}
</style>
