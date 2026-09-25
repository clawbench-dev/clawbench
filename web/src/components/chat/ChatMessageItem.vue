<template>
  <div class="chat-message" :class="[msg.role, { 'has-metadata': msg.role === 'assistant' && msg.metadata }]" :data-msg-key="msg.id ? 'db-' + msg.id : null">

    <!-- Message card (bubble). The meta bar deliberately lives OUTSIDE this
         element so it sits on the panel background for both roles. -->
    <div class="msg-card">
    <!-- Collapsible content wrapper -->
    <div ref="wrapperRef" class="msg-content-wrapper">
      <FileAttachmentList v-if="msg.role === 'user' && msg.files && msg.files.length > 0 && !hasImagesInContent(msg.content)" :files="msg.files" @file-tag-click="$emit('file-tag-click', $event)" />

      <!-- Message content — unified ContentBlocks rendering for both user and assistant -->
      <ContentBlocks
        v-if="msg.blocks"
        :blocks="msg.blocks"
        :msgId="msg.id"
        :msgIndex="index"
        :sessionId="sessionId"
        :expandedTools="expandedTools"
        :blockTasks="blockTasks"
        :blockAskQuestions="blockAskQuestions"
        :streaming="msg.streaming"
        :startedAt="msg.createdAt"
        :cancelled="msg.cancelled"
        :summary="msg.summary"
        :summaryCards="msg.summaryCards"
        :showingSummary="showSummary"
        :renderTextBlock="renderTextBlock"
        :formatToolInput="formatToolInput"
        :toolCallSummary="toolCallSummary"
        :truncate="truncate"
        :getAgentBackend="getAgentBackend"
        :getAgentName="getAgentName"
        :staticBlockCache="staticBlockCache"
        :active="active"
        :readOnly="readOnly"
        @toggle-tool="$emit('toggle-tool', $event)"
        @show-tool-detail="$emit('show-tool-detail', $event)"
        @task-card-click="$emit('task-card-click', $event)"
        @send-message="(text, cardKey) => $emit('send-message', text, cardKey)"
        @render-flush="$emit('render-flush')"
        @toggle-summary="$emit('toggle-summary', msg.id)"
        @resume-session="$emit('resume-session', $event)"
        @reset-session="$emit('reset-session', $event)"

      />
    </div>

    <!-- File changes banner — standalone button above toolbar -->
    <button v-if="msg.role === 'assistant' && !msg.streaming && hasFileChanges" class="chat-file-changes-banner" @click="fileChangesDrawer.open()">
      <FileDiff :size="14" />
      <span>{{ t('chat.fileChanges.title') }}</span>
      <span class="chat-file-changes-count count-badge count-badge--md">{{ fileChanges.created.length + fileChanges.modified.length }}</span>
    </button>

    <!-- Cancelled marker: shown after file changes banner, hidden when last block is thinking (shown inline in thinking-header instead) -->
    <div v-if="msg.cancelled && !isLastBlockThinking" class="chat-cancelled-mark">{{ t('chat.contentBlocks.cancelled') }}</div>
    </div><!-- /.msg-card -->

    <!-- ── Meta bar (OUTSIDE the bubble, both roles) ──
         Assistant: duration + friendly time on the left, actions on the right.
         User: friendly time + copy + details. Same row layout, same styles —
         only which actions are meaningful differs. -->
    <div v-if="showMetaBar" class="chat-meta-bar" :class="msg.role === 'user' ? 'chat-meta-bar-user' : 'chat-meta-bar-assistant'">
      <span class="chat-meta-info">
        <span v-if="msg.role === 'assistant' && msg.metadata?.wallMs" class="chat-meta-duration">{{ formatDuration(msg.metadata.wallMs) }}</span>
        <span v-if="relativeTime" class="chat-meta-time" :class="{ 'chat-meta-sep': msg.role === 'assistant' && msg.metadata?.wallMs }">{{ relativeTime }}</span>
      </span>
      <div class="chat-meta-actions">
        <!-- Summary/original toggle — deliberately FIRST in the row. It is the
             reading-mode control for this message (which view of the reply you
             are looking at), so it leads; everything after it is an action *on*
             the message. Assistant-only, and hidden while streaming since there
             is no settled content to toggle yet. -->
        <template v-if="msg.role === 'assistant'">
          <span v-if="!readOnly && !msg.streaming" ref="toggleWrapRef" class="chat-summary-anchor">
            <SummaryToggle v-if="!msg._summarizing" mode="button" :showing-summary="showSummary" i18n-prefix="chat.message" @toggle="handleToggleSummary" />
            <LoadingIndicator v-else size="sm" inline />
          </span>
          <span v-if="msg._loadingOriginal" class="chat-summary-anchor">
            <LoadingIndicator size="sm" inline />
          </span>
        </template>
        <!-- Quote this message as a whole. Rendered for BOTH roles: quoting a
             user message (e.g. to re-ask about it) is as useful as quoting a
             reply. Gated like the rest of the meta bar, and skipped for queued
             bubbles which have no settled content yet. -->
        <button
          v-if="!readOnly && !msg.streaming && quotableText"
          class="chat-action-btn"
          :title="t('quoteBar.quoteMessage')"
          :aria-label="t('quoteBar.quoteMessage')"
          @click="$emit('quote-message', msg)"
        >
          <MessageSquareQuote :size="14" />
        </button>
        <template v-if="msg.role === 'assistant'">
          <button v-if="msgText && !readOnly" ref="speakBtnRef" class="chat-action-btn chat-speak-btn" :class="{ 'chat-action-btn--wide': autoSpeech.isActive(msg.id), active: autoSpeech.isActive(msg.id), loading: autoSpeech.isGeneratingText(msg.id) }" :title="speakBtnLabel" :aria-label="speakBtnLabel" @click.stop="handleSpeak">
            <!-- Generating states: summarizing / synthesizing -->
            <template v-if="autoSpeech.isGeneratingText(msg.id)">
              <Clock :size="14" class="speak-spinner" />
              <span>{{ autoSpeech.getPhaseLabel(msg.id) ? t('chat.speech.' + autoSpeech.getPhaseLabel(msg.id)) : '' }}</span>
            </template>
            <!-- Playing state -->
            <template v-else-if="autoSpeech.isPlayingAudio(msg.id)">
              <Pause :size="14" />
              <span>{{ t('chat.message.speaking') }}</span>
            </template>
            <!-- Default idle state -->
            <template v-else>
              <Volume2 :size="14" />
            </template>
          </button>
        </template>
        <button v-if="!readOnly && !msg.streaming && (msg.role === 'assistant' || copyableUserText)" class="chat-action-btn" :class="{ 'is-copied': copied }" @click="handleCopyMessage" :title="copied ? t('common.copied') : t('chat.message.copy')" :aria-label="copied ? t('common.copied') : t('chat.message.copy')">
          <span v-if="copied" class="chat-copy-copied-text">{{ t('common.copied') }}</span>
          <Copy v-else :size="14" />
        </button>
        <template v-if="msg.role === 'assistant'">
          <button v-if="!readOnly && !msg.streaming && !hideSessionActions" class="chat-action-btn" @click="$emit('fork-from-message', msg)" :title="t('chat.actions.forkSession')">
            <Split :size="14" />
          </button>
          <button
            v-if="!readOnly && !msg.streaming && !hideSessionActions"
            class="chat-action-btn"
            :disabled="isLastMessage"
            :title="isLastMessage ? t('chat.session.nothingToRewind') : t('chat.actions.rewindSession')"
            @click="$emit('rewind-from-message', msg)"
          >
            <Rewind :size="14" />
          </button>
        </template>
        <button v-if="!readOnly && !msg.streaming" class="chat-action-btn" @click="$emit('show-metadata', msg)" :title="t('chat.message.viewDetails')">
          <Info :size="14" />
        </button>
      </div>
    </div>

    <!-- File changes sheet -->
    <FileChangesDrawer
      :open="fileChangesDrawer.effectiveOpen.value"
      :created="fileChanges.created"
      :modified="fileChanges.modified"
      @close="fileChangesDrawer.close()"
      @open-file="handleOpenFilePayload"
      @select-file="handleSelectFile"
    />

    <!-- File diffs drill-down sheet (all Write/Edit diffs for one file) -->
    <FileDiffsDrawer
      :open="fileDiffsDrawer.effectiveOpen.value"
      :file-path="selectedFile?.path || ''"
      :tool-name="selectedFile?.toolName || ''"
      :blocks="msg.blocks || []"
      :msg-id="msg.id"
      :tool-ids="selectedFile?.toolIds || []"
      :session-id="sessionId"
      :format-tool-input="formatToolInput"
      @close="fileDiffsDrawer.close()"
      @file-open="handleOpenFilePayload"
      @back="handleFileDiffsBack"
    />
  </div>
</template>

<script setup>
import { ref, inject, computed, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, Pause, Volume2, Info, FileDiff, Copy, Split, Rewind, MessageSquareQuote } from 'lucide-vue-next'
import { formatDuration, formatRelativeTime } from '@/utils/format.ts'
import { copyText } from '@/utils/clipboard.ts'
import { extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { extractFileChanges } from '@/utils/chatStreamUtils.ts'
import { isShowingSummary, normalizeDisplayMode } from '@/utils/chatSessionUtils.ts'
import { localConfig } from '@/composables/useSettingsConfig'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import ContentBlocks from './ContentBlocks.vue'
import FileAttachmentList from './FileAttachmentList.vue'
import FileChangesDrawer from './FileChangesDrawer.vue'
import FileDiffsDrawer from './FileDiffsDrawer.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import SummaryToggle from '@/components/common/SummaryToggle.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'

const { t } = useI18n()

const props = defineProps({
  msg: Object,
  index: Number,
  expandedTools: Object,
  blockTasks: Object,
  blockAskQuestions: Object,
  agents: Array,
  staticBlockCache: Object,
  active: { type: Boolean, default: true },
  /** True when this message is the most recent assistant reply in the list (drives the 'mixed' display mode). */
  isLastAssistant: { type: Boolean, default: false },
  /** True when this message is the very last entry in the rendered list — rewind
   *  has nothing to truncate after it, so the rewind button is disabled. */
  isLastMessage: { type: Boolean, default: false },
  /** Read-only hosts (task execution detail) hide the fork/rewind pair: those
   *  two actions need a live session to branch or truncate, which a finished
   *  execution record does not have. The rest of the bar (summary toggle,
   *  speak, copy, details) stays useful there. */
  hideSessionActions: { type: Boolean, default: false },
  /** Public share page: the viewer is anonymous, so every per-message action
   *  is suppressed — speak/fork/rewind need a live session or auth, copy/
   *  details duplicate what the snapshot already renders (details also exposes
   *  token/cost metadata), and quote needs a chat composer the reader does not
   *  have. The summary/original switch is suppressed too: it is an app-side
   *  reading preference, and a read-only transcript has no business offering a
   *  choice the reader cannot evaluate (the snapshot always carries the original
   *  blocks; `showSummary` falls back to the summary only when there are none).
   *  Only the timestamp line is kept — it is information, not a control. */
  readOnly: { type: Boolean, default: false },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'fork-from-message', 'rewind-from-message', 'reset-session', 'quote-message'])

const autoSpeech = inject('autoSpeech')
const wrapperRef = ref(null)
const speakBtnRef = ref(null)
const toggleWrapRef = ref(null)

// ── Summary/original toggle scroll anchoring ──
// The toggle button sits in the bottom meta bar, BELOW the message content.
// Switching summary↔original changes the content height, which would push/pull
// the button vertically and force the user to scroll to find it again. Instead
// we record the button's viewport top + the scroll container's scrollTop before
// toggling, then after the re-render adjust scrollTop so the button stays pinned
// to the exact same screen position.
function handleToggleSummary() {
  const wrap = toggleWrapRef.value
  const scroller = wrap?.closest('.chat-messages')
  const anchor = wrap?.getBoundingClientRect().top
  const baseScroll = scroller?.scrollTop ?? 0
  if (!wrap || !scroller || anchor == null) {
    emit('toggle-summary', props.msg?.id)
    return
  }
  // Guard: if the user has manually scrolled since toggling, stop re-anchoring
  // so we don't fight the user's scroll gesture.
  const toggleStartScroll = scroller.scrollTop
  const SCROLL_DRIFT_GUARD = 20
  const adjust = () => {
    if (Math.abs(scroller.scrollTop - toggleStartScroll) > SCROLL_DRIFT_GUARD) return
    const newTop = wrap.getBoundingClientRect().top
    scroller.scrollTop = baseScroll + (newTop - anchor)
  }
  emit('toggle-summary', props.msg?.id)
  nextTick(adjust)
  // The original view may load lazily (content fetched after nextTick), resizing
  // the message later. Watch the content wrapper briefly and keep re-anchoring
  // until the height settles.
  const contentEl = wrapperRef.value
  if (contentEl && typeof ResizeObserver !== 'undefined') {
    let settled = false
    const ro = new ResizeObserver(() => { if (!settled) adjust() })
    ro.observe(contentEl)
    setTimeout(() => { settled = true; ro.disconnect() }, 600)
  }
}

// Extract text content from message blocks for TTS.
// Uses extractSpeakableText to include AskUserQuestion blocks.
// Falls back to the summary text when blocks are empty (summary-first loading
// strips content), so the read-aloud button stays available in summary view.
// Global display mode: 'summary' | 'original' | 'mixed'. In 'mixed' the LAST
// assistant reply renders full text (behaves like 'original'), older messages
// render their summaries (behaves like 'summary').
const displayMode = computed(() => normalizeDisplayMode(localConfig.messageDisplayMode))

const msgText = computed(() => {
  if (props.msg?.role !== 'assistant') return ''
  const text = extractSpeakableText(props.msg?.blocks || [])
  if (text) return text
  if (showSummary.value && props.msg?.summary) return props.msg.summary
  return ''
})

// Friendly relative timestamp shown in the meta bar for BOTH roles.
// formatRelativeTime returns '' for missing/invalid dates (including Go zero-value
// times), so the label and its separator stay hidden when there is nothing to show.
const relativeTime = computed(() => (props.msg?.createdAt ? formatRelativeTime(props.msg.createdAt) : ''))

// Copyable text for a user message — the raw content (blocks are the same text
// for user rows; `content` survives summary-stripped payloads).
const copyableUserText = computed(() => {
  if (props.msg?.role !== 'user') return ''
  return extractSpeakableText(props.msg?.blocks || []) || (props.msg?.content || '')
})

// Meta bar visibility. Assistant keeps its old gate (it owns the action bar);
// user messages show it as soon as there is a timestamp or copyable text.
const showMetaBar = computed(() => {
  if (props.msg?.role === 'assistant') {
    return !props.msg.streaming && !!(msgText.value || props.msg.blocks?.length || props.msg.summary)
  }
  if (props.msg?.role !== 'user' || props.msg.streaming) return false
  return !!(relativeTime.value || copyableUserText.value)
})

/**
 * The text a "quote this message" action would capture — deliberately the SAME
 * text the user reads, not the raw blocks:
 *   - assistant: `msgText` (extractSpeakableText, which skips tool/thinking noise);
 *   - user: the message content.
 * Empty means there is nothing worth quoting, and the button is hidden.
 */
const quotableText = computed(() => (props.msg?.role === 'user' ? copyableUserText.value : msgText.value))

// Accessible name/tooltip for the read-aloud button. While audio is playing the
// button acts as a stop control, so it must not advertise "read aloud".
const speakBtnLabel = computed(() => {
  const id = props.msg?.id
  if (autoSpeech.isPlayingAudio(id)) return t('chat.message.speaking')
  if (autoSpeech.isGeneratingText(id)) {
    const phase = autoSpeech.getPhaseLabel(id)
    return phase ? t('chat.speech.' + phase) : t('chat.message.readAloud')
  }
  return t('chat.message.readAloud')
})

// Whether to render the summary view. Computed from message state (summary
// exists, content stripped), the user's explicit preference, and the global
// default display mode. While the full text is being lazily fetched in
// original (or mixed-last-assistant) view, keep showing the summary as a
// placeholder so the message bubble is never blank.
const showSummary = computed(() => {
  if (!props.msg) return false
  // readOnly (public share page) always renders the ORIGINAL content: the
  // snapshot never strips blocks, and a read-only transcript has no business
  // offering a summary/original switch the reader cannot evaluate.
  //
  // Never render blank, though: a message with a summary but no blocks (rare —
  // the agent emitted metadata only) still shows its summary.
  if (props.readOnly) {
    const hasBlocks = !!props.msg.blocks?.length
    const hasSummary = props.msg.summary != null && props.msg.summary !== ""
    return !hasBlocks && hasSummary
  }
  return isShowingSummary(props.msg, displayMode.value, { isLastAssistant: props.isLastAssistant })
})

// A summarized message whose content was stripped by the backend has nothing
// to render in original view — request the full text once. Only applies when
// this message is actually rendered as original: global 'original' mode, or
// 'mixed' mode where this is the most recent assistant reply. Older messages
// in 'mixed' show summaries and must NOT trigger lazy fetches (that would pull
// the full content of every summarized message on screen).
// Guarded by _loadingOriginal, blocks-present, and _loadAttempted (latch set
// after the first fetch completes) to fire exactly once even on failure.
const wantsOriginal = computed(() => displayMode.value === 'original' || (displayMode.value === 'mixed' && !!props.isLastAssistant))
const needsLazyOriginal = computed(() =>
  wantsOriginal.value &&
  props.msg?.summary != null && props.msg.summary !== '' &&
  (!props.msg.blocks || props.msg.blocks.length === 0) &&
  props.msg.showingSummary === undefined &&
  props.msg._loadingOriginal !== true &&
  props.msg._loadAttempted !== true
)

watch(needsLazyOriginal, (needs) => {
  if (needs && props.msg) emit('ensure-content', props.msg)
}, { immediate: true })

// Handle speak button click: play or stop (no popover)
function handleSpeak() {
  if (autoSpeech.isActive(props.msg?.id)) {
    autoSpeech.stopAudio()
  } else if (msgText.value && props.msg?.id) {
    autoSpeech.speakText(props.msg.id, msgText.value)
  }
}

const chatRender = inject('chatRender', {})
const chatSession = inject('chatSession', {})

const { renderTextBlock, toolCallSummary, formatToolInput, truncate, hasImagesInContent } = chatRender
const { getAgentBackend, getAgentName } = chatSession
const sessionId = computed(() => chatSession.sessionId?.() || '')

// File changes extraction (Write → created, Edit → modified).
// Uses summaryCards as fallback when blocks are empty (summary-only view).
const fileChanges = computed(() => {
  if (props.msg?.role !== 'assistant' || props.msg.streaming) return { created: [], modified: [] }
  return extractFileChanges(props.msg?.blocks || [], props.msg?.summaryCards)
})
const hasFileChanges = computed(() => fileChanges.value.created.length > 0 || fileChanges.value.modified.length > 0)

/** Whether the last block is a thinking block (avoids duplicate cancelled marker — inline one is shown in thinking-header instead). */
const isLastBlockThinking = computed(() => {
  const blocks = props.msg?.blocks
  if (!blocks || blocks.length === 0) return false
  return blocks[blocks.length - 1].type === 'thinking'
})

const fileChangesDrawer = useTabDrawer('chat')
const fileDiffsDrawer = useTabDrawer('chat')

// File selected for drill-down: { path, toolName: 'Write' | 'Edit', toolIds: string[] }
const selectedFile = ref(null)

function handleSelectFile(payload) {
  selectedFile.value = payload
  fileChangesDrawer.close()
  fileDiffsDrawer.open()
}

function handleFileDiffsBack() {
  fileDiffsDrawer.close()
  fileChangesDrawer.open()
}

// Handles open-file payloads: either a plain path string (from FileChangesDrawer)
// or { path, lineStart, lineEnd } (from FileDiffsDrawer's diff file-open buttons).
function handleOpenFilePayload(payload) {
  const path = typeof payload === 'string' ? payload : payload.path
  const lineStart = typeof payload === 'string' ? undefined : payload.lineStart
  const lineEnd = typeof payload === 'string' ? undefined : payload.lineEnd
  const lineRanges = typeof payload === 'string' ? undefined : payload.lineRanges
  // AI may return absolute paths (e.g. /home/user/project/src/foo.ts).
  // Strip projectRoot prefix so openFilePath doesn't treat them as external.
  const root = store.state.projectRoot
  const relPath = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  if (lineRanges) openFilePath(relPath, lineStart, lineEnd, 'chat', lineRanges)
  else openFilePath(relPath, lineStart, lineEnd, 'chat')
}

// Copy message markdown — only the final conclusion (last text block)
const copied = ref(false)
function handleCopyMessage() {
  if (copied.value) return
  // Role-appropriate payload: assistant reuses the read-aloud extraction (all
  // text + AskUserQuestion blocks, falling back to the summary for the
  // summary-only view); a user message copies its own content. `msgText` is
  // assistant-only (it returns '' for user rows), so using it here would make
  // the user bar's copy button a silent no-op.
  const text = props.msg?.role === 'user' ? copyableUserText.value : msgText.value
  if (!text) return
  copyText(text, () => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  })
}
</script>

<style scoped>
/* ── Message card (the bubble itself) ──
   The meta bar is a SIBLING of this element, so the card owns every bubble
   visual (background, radius, padding, clipping). */
.msg-card {
    padding: var(--space-4) var(--space-6);
    min-width: 0;
    max-width: 100%;
    box-sizing: border-box;
}

/* ── Message content wrapper ── */
.msg-content-wrapper {
  position: relative;
}

/* ── Cancelled marker (shown after file changes banner) ── */
.chat-cancelled-mark {
  display: inline-block;
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-xs);
  margin-top: var(--space-2);
}

/* ── File changes banner ── */
.chat-file-changes-banner {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-3) var(--space-5);
    margin-top: var(--space-3);
    border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
    border-radius: var(--radius-xs);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
    color: var(--accent-color, #0066cc);
    font-size: var(--font-size-sm);
    font-weight: var(--font-weight-medium);
    cursor: pointer;
    transition: background var(--duration-base), border-color var(--duration-base), box-shadow var(--duration-base);
}

@media (hover: hover) {
  .chat-file-changes-banner:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 16%, transparent);
    border-color: color-mix(in srgb, var(--accent-color, #0066cc) 55%, transparent);
    box-shadow: 0 1px 3px color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}

.chat-file-changes-banner svg {
    flex-shrink: 0;
}

.chat-file-changes-count {
    margin-left: auto;
    font-weight: var(--font-weight-semibold);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
}

/* Chat Meta Bar — duration/time info + message actions.
   Sits OUTSIDE the bubble (sibling of .msg-card), so it always renders on the
   panel background and never inherits the user bubble's white-on-accent text. */
.chat-meta-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: var(--space-2);
    gap: var(--space-3);
    padding: 0 var(--space-6);
    min-width: 0;
}

/* Both roles share the row. The user row is already right-aligned and
   shrink-to-fit (align-items: flex-end on .chat-message.user), so the bar needs
   no alignment of its own — only the shared row layout above. */

.chat-meta-info {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--font-size-xs);
    color: color-mix(in srgb, var(--text-secondary) 70%, transparent);
    min-width: 0;
    overflow: hidden;
}

.chat-meta-sep::before {
    content: '·';
    margin-right: var(--space-3);
}

.chat-meta-duration {
    font-variant-numeric: tabular-nums;
}

.chat-meta-time {
    white-space: nowrap;
}

/* Speak button active state */
.chat-action-btn.active {
    opacity: 1;
    color: var(--accent-color, #0066cc);
}

/* Copy button "Copied" feedback state */
.chat-action-btn.is-copied {
    opacity: 1;
    color: var(--accent-color);
}

.chat-copy-copied-text {
    font-size: var(--font-size-xs);
    font-weight: var(--font-weight-medium);
}

@media (hover: hover) {
  .chat-action-btn.active:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  }
}

/* Meta bar action buttons container */
.chat-meta-actions {
    display: flex;
    align-items: center;
    gap: var(--space-1);
}

/* Wrapper around the summary/original toggle button — used as the scroll anchor */
.chat-summary-anchor {
    display: inline-flex;
    align-items: center;
}

/* Speak button loading spinner animation */
.chat-action-btn.loading .speak-spinner {
    animation: speak-spin 1s linear infinite;
}

@keyframes speak-spin {
    to { transform: rotate(360deg); }
}


@media (hover: hover) {
  .chat-meta-bar-user:hover {
    color: var(--text-secondary);
  }
}
</style>

<style>
/* Chat message - non-scoped for v-html penetration.
   .chat-message is now the row container (bubble + meta bar); the bubble
   visuals live on .msg-card so the meta bar can sit outside them. */
.chat-message {
    display: flex;
    flex-direction: column;
    font-size: var(--font-size-md);
    line-height: var(--line-height-snug);
    min-width: 0;
    word-wrap: break-word;
    word-break: break-word;
    max-width: 100%;
    box-sizing: border-box;
    contain: style;
    /* DO NOT reintroduce `content-visibility: auto` here.
       It was tried (16382ef31) to skip layout/paint for offscreen messages, but
       it makes scrollHeight depend on a per-ELEMENT remembered height: an
       offscreen message is laid out from `contain-intrinsic-size: auto 240px`
       until it has been measured once. Message heights range from ~66px to
       several thousand (code blocks, thinking), so that guess is wildly wrong.

       ChatMessageList remounts the whole list on every structural change
       (`:key="listKey"`, which includes the message COUNT — so every send).
       Remounting creates fresh elements, discarding the remembered heights, so
       at that instant scrollHeight collapses to the estimate. The browser then
       clamps scrollTop up to the shrunken maximum (the "jumps to the middle"
       half) and followToBottom pins against that same wrong height; as the
       browser re-measures, the ResizeObserver backstop re-pins and the view
       lurches down again (the second half). Both hops are instant scrollTop
       writes, so it reads as a flicker rather than a scroll.

       Measured on a real 20-message/12.7MB session: scrollTop 5018 → 4050
       (up 968px) → 5710 (down 1660px). Isolated to this rule — with it
       removed, or with no remount, the jump is 0px. */
}

/* ── Leaked form/XML control wrapping guard ──
   A malformed <clawbench-ask-question> payload (e.g. an <option> without a <question>)
   fails isValidAskContent, so detectAskQuestion() reports not-found and the raw
   XML is NOT stripped — it falls through to markdown, where marked passes the
   tags through and DOMPurify keeps them. Those become REAL form elements in the
   bubble.

   The problem: the UA stylesheet gives <option> `white-space: nowrap` (and
   <select> `pre`). With no way to wrap, one long option label lays out as a
   single line — measured 485px past the bubble for a prose label and 5394px for
   a 600-char URL — and .chat-message.assistant's `overflow: hidden` then clips
   it silently with no way to scroll. Resetting white-space restores normal
   wrapping; overflow-wrap guarantees even an unbreakable token breaks.

   Scoped to .chat-message (both roles) and to exactly the tags DOMPurify
   preserves from a leaked payload. None of them is legitimately rendered as
   content in a chat bubble, so this cannot affect real markdown output. */
.chat-message :is(option, optgroup, select, textarea, label, fieldset, legend, header) {
    white-space: normal;
    overflow-wrap: anywhere;
}

/* ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   ⚠️  CRITICAL — Android WebView GPU Ghost Artifact Fix
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
   DO NOT REMOVE this rule. It is the sole fix for a persistent Android
   WebView rendering bug where layout reflow causes GPU compositing
   cross-layer pixel pollution — phantom metadata text (e.g. model name,
   timestamp) from one message appears overlaid on another message.

   Root cause: WebView's GPU compositor incorrectly re-composites adjacent
   layers when a layout reflow occurs (e.g. DOM insertion/removal, height
   changes). This happens ~2s after opening a session when the "all loaded"
   hint's <Transition> leave animation removes a DOM node from .chat-load-area.

   Fix: `will-change: transform` forces each .chat-message into its own
   independent GPU compositing layer. Reflows still happen, but they can no
   longer cause cross-layer pixel contamination.

   Previous attempt (v-if→v-show everywhere) was a whack-a-mole approach
   that was incomplete and lost Transition animations. This single rule
   makes ALL layout reflows harmless in WebView.

   Scoped to [data-app-mode] (WebView only) to avoid unnecessary GPU
   memory overhead on desktop browsers.
   ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ */
:root[data-app-mode] .chat-message {
    will-change: transform;
}

/* ── File attachment in messages ── */
.chat-files {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: var(--space-3);
  margin: var(--space-2) 0;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-files::-webkit-scrollbar {
  display: none;
}

/* File card: filename pill */
.chat-message .chat-file-tag,
.chat-message .chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  border-radius: var(--radius-sm);
  height: 40px;
  padding:0 var(--space-5);
  font-size: var(--font-size-sm);
  text-decoration: none;
  cursor: pointer;
  transition: opacity var(--duration-base);
  flex-shrink: 0;
  box-sizing: border-box;
}

.chat-message .attachment-filename {
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-message .attachment-filesize {
  font-size: var(--font-size-2xs);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Image card: square thumbnail */
.chat-message .chat-file-attachment.attachment-image-only {
  width: 40px;
  height: 40px;
  padding: 0;
  overflow: hidden;
  border-radius: var(--radius-sm);
}

.chat-message .attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.chat-file-tag-path {
  font-family: var(--font-mono);
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.chat-file-tag-path::-webkit-scrollbar {
  display: none;
}

/* User message: common colors */
.chat-message.user .chat-file-tag,
.chat-message.user .chat-file-attachment {
  color: rgba(255, 255, 255, 0.95);
}

.chat-message.user .chat-file-tag-path,
.chat-message.user .attachment-filename {
  color: rgba(255, 255, 255, 0.95);
}

/* User message: solid border (both upload and ref) */
.chat-message.user .attachment-upload,
.chat-message.user .attachment-ref {
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.35);
}

.chat-message.user .attachment-file-icon {
  background: rgba(0, 0, 0, 0.15);
  border-radius: var(--radius-sm);
  padding: var(--space-1);
}

@media (hover: hover) {
  .chat-message.user .attachment-upload:hover,
  .chat-message.user .attachment-ref:hover,
  .chat-message.user .chat-file-tag:hover {
    background: rgba(255, 255, 255, 0.25);
  }
}

/* Assistant message: common colors */
.chat-message.assistant .chat-file-tag,
.chat-message.assistant .chat-file-attachment {
  color: var(--text-secondary);
}

.chat-message.assistant .chat-file-tag-path,
.chat-message.assistant .attachment-filename {
  color: var(--text-secondary);
}

/* Assistant message: solid border (both upload and ref) */
.chat-message.assistant .attachment-upload,
.chat-message.assistant .attachment-ref {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
}

@media (hover: hover) {
  .chat-message.assistant .attachment-upload:hover,
  .chat-message.assistant .attachment-ref:hover,
  .chat-message.assistant .chat-file-tag:hover {
    background: var(--bg-secondary);
  }
}

/* ── Row layout (bubble + meta bar) ──
   .chat-message no longer paints a background: it is the flex row that holds
   .msg-card (the bubble) and .chat-meta-bar (outside it). Kept in the global
   block so the role colours and alignment still reach v-html descendants.

   The user row spans the full column (no margin-right) so its meta bar lines up
   with the assistant one. The 10px right inset belongs to the BUBBLE, not the
   row — keeping it on the row would shift the meta bar inward and misalign the
   two rows' right edges. */
.chat-message.user {
    color: white;
    align-self: stretch;
    align-items: flex-end;
    max-width: 100%;
}

.chat-message.assistant {
    color: var(--text-primary);
    align-self: stretch;
    align-items: stretch;
    position: relative;
    min-width: 0;
    overflow-wrap: break-word;
}

/* ── The bubble itself ── */
.chat-message.assistant .msg-card {
    background: var(--bg-tertiary);
    border-radius: 0;
    overflow: hidden;
}

.chat-message.user .msg-card {
    background: var(--user-msg-color);
    border-radius: 20px 20px 0 20px;
    margin-right: var(--space-5);
    max-width: calc(100% - 20px);
    overflow: hidden;
}

.chat-message.user pre {
    padding: var(--space-5);
    margin: var(--space-3) 0;
    border-radius: var(--radius-sm);
    overflow-x: auto;
    max-width: 100%;
    box-sizing: border-box;
    word-break: normal;
    word-wrap: normal;
    white-space: pre;
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user pre code {
    white-space: pre;
    word-break: normal;
}

/* Word-wrap mode: override pre/code white-space from rules above */
.chat-message.user .code-block-wrapper.word-wrap pre {
    overflow: visible;
    white-space: pre-wrap;
}

.chat-message.user .code-block-wrapper.word-wrap pre code {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.user code {
    font-family: var(--font-mono);
    padding: var(--space-1) var(--space-3);
    font-size: var(--font-size-md);
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user h1,
.chat-message.user h2,
.chat-message.user h3 {
    margin: var(--space-3) 0 3px;
    font-weight: var(--font-weight-semibold);
}

.chat-message.user h1 { font-size: var(--font-size-2xl); }
.chat-message.user h2 { font-size: var(--font-size-lg); }
.chat-message.user h3 { font-size: var(--font-size-md); }

.chat-message.user p {
    margin: 3px 0;
}

.chat-message.user ul,
.chat-message.user ol {
    margin: var(--space-3) 0;
}

.chat-message.user blockquote {
    margin: var(--space-3) 0;
    padding:5px var(--space-5);
    border-left-color: rgba(255, 255, 255, 0.35);
    background: rgba(0, 0, 0, 0.1);
}

.chat-message.user a {
    word-break: break-all;
    overflow-wrap: break-word;
    color: white;
    text-decoration: underline;
    text-underline-offset: 2px;
}

@media (hover: hover) {
  .chat-message.user a:hover {
    color: rgba(255, 255, 255, 0.85);
  }
}

.chat-message.user img {
    margin: var(--space-3) 0;
}

.chat-message.user hr {
    margin: var(--space-4) 0;
    border-top-color: rgba(255, 255, 255, 0.25);
}

.chat-message.user .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    margin: 0.75em 0;
}

.chat-message.user .table-block-wrapper .table-wrap {
    margin: 0;
    border-radius: 0;
}

.chat-message.user table {
    display: block;
    margin: 0;
}

.chat-message.user th {
    font-size: var(--font-size-md);
    color: rgba(255, 255, 255, 0.95);
    background: rgba(0, 0, 0, 0.15);
    border-color: rgba(255, 255, 255, 0.2);
}

.chat-message.user td {
    white-space: nowrap;
    border-color: rgba(255, 255, 255, 0.15);
}

.chat-message.user tr:nth-child(odd) td {
    background: rgba(0, 0, 0, 0.08);
}

.chat-message.user tr:nth-child(even) td {
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user .chat-file-path {
    background: rgba(0, 0, 0, 0.15);
    color: rgba(255, 255, 255, 0.9);
}

.chat-message.user .chat-file-open-btn {
    color: rgba(255, 255, 255, 0.7);
}

@media (hover: hover) {
  .chat-message.user .chat-file-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
  }
}
.chat-message.user .chat-file-open-btn.external {
    color: #f0a04b;
}

.chat-message.user .chat-commit-hash-pending {
    color: inherit;
}

.chat-message.user .chat-commit-hash {
    color: rgba(255, 255, 255, 0.9);
}

.chat-message.user .chat-commit-open-btn {
    color: rgba(255, 255, 255, 0.7);
}

@media (hover: hover) {
  .chat-message.user .chat-commit-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
  }
}

.chat-message.assistant pre {
    padding: var(--space-5);
    margin: var(--space-3) 0;
    border-radius: var(--radius-sm);
    overflow-x: auto;
    max-width: 100%;
    box-sizing: border-box;
    word-break: normal;
    word-wrap: normal;
    white-space: pre;
}

.chat-message.assistant pre code {
    white-space: pre;
    word-break: normal;
}

/* Word-wrap mode: override pre/code white-space from rules above */
.chat-message.assistant .code-block-wrapper.word-wrap pre {
    overflow: visible;
    white-space: pre-wrap;
}

.chat-message.assistant .code-block-wrapper.word-wrap pre code {
    white-space: pre-wrap;
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.assistant code {
    font-family: var(--font-mono);
    padding: var(--space-1) var(--space-3);
    font-size: var(--font-size-md);
}

.chat-message.assistant h1,
.chat-message.assistant h2,
.chat-message.assistant h3 {
    margin: var(--space-3) 0 3px;
    font-weight: var(--font-weight-semibold);
}

.chat-message.assistant h1 { font-size: var(--font-size-2xl); }
.chat-message.assistant h2 { font-size: var(--font-size-lg); }
.chat-message.assistant h3 { font-size: var(--font-size-md); }

.chat-message.assistant p {
    margin: 3px 0;
}

.chat-message.assistant ul,
.chat-message.assistant ol {
    margin: var(--space-3) 0;
}

.chat-message.assistant blockquote {
    margin: var(--space-3) 0;
    padding:5px var(--space-5);
}

.chat-message.assistant a {
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.assistant img {
    margin: var(--space-3) 0;
}

.chat-message.assistant hr {
    margin: var(--space-4) 0;
}

.chat-message.assistant .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: var(--radius-sm) var(--radius-sm) 0 0;
    margin: 0.75em 0;
}

.chat-message.assistant .table-block-wrapper .table-wrap {
    margin: 0;
    border-radius: 0;
}

.chat-message.assistant table {
    display: block;
    margin: 0;
}

.chat-message.assistant th {
    font-size: var(--font-size-md);
    color: var(--text-primary);
}

.chat-message.assistant td {
    white-space: nowrap;
}

/* ── Audio player in chat (non-scoped for v-html penetration) ── */
.chat-message .chat-audio-wrapper {
  margin: var(--space-2) 0;
}

.chat-message .chat-audio-player {
  width: 100%;
  max-width: 280px;
  height: 28px;
  border-radius: var(--radius-sm);
  outline: none;
  display: block;
}

.chat-message .chat-audio-player::-webkit-media-controls-panel {
  padding:0 var(--space-2);
}

.chat-message .chat-audio-player::-webkit-media-controls-play-button {
  margin:0 var(--space-1);
}

.chat-message .chat-audio-player::-webkit-media-controls-current-time-display,
.chat-message .chat-audio-player::-webkit-media-controls-time-remaining-display {
  font-size: var(--font-size-xs);
}

/* ── Video player in chat (non-scoped for v-html penetration) ──
   Same reason as the audio block above: the <video> is injected through
   v-html by convertVideoLinks, so Vue never stamps it with this component's
   scope attribute and a scoped rule can never match it. Without these the
   player falls back to the video's intrinsic resolution and overflows the
   bubble (issue #497). */
.chat-message .chat-video-wrapper {
  margin: var(--space-4) 0;
}

.chat-message .chat-video-player {
  width: 100%;
  max-width: 400px;
  max-height: 225px;
  border-radius: var(--radius-sm);
  outline: none;
  background: #000;
}

/* ── Inline image alignment (non-scoped for v-html penetration) ──
   `chat-img` is stamped on the <img> by rewriteImageUrls, i.e. also injected
   via v-html. Sizing lives in markdown-common.css; this only cancels the
   baseline gap. */
.chat-message .chat-img {
  vertical-align: middle;
}
</style>
