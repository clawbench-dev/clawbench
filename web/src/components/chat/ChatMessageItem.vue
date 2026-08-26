<template>
  <div class="chat-message" :class="[msg.role, { 'has-metadata': msg.role === 'assistant' && msg.metadata, pending: msg.pending }]" :data-msg-key="msg.id ? 'db-' + msg.id : null">

    <!-- Collapsible content wrapper -->
    <div ref="wrapperRef" class="msg-content-wrapper">
      <FileAttachmentList v-if="msg.role === 'user' && msg.files && msg.files.length > 0 && !hasImagesInContent(msg.content)" :files="msg.files" @file-tag-click="$emit('file-tag-click', $event)" />

      <!-- Message content — unified ContentBlocks rendering for both user and assistant -->
      <ContentBlocks
        v-if="msg.blocks"
        :blocks="msg.blocks"
        :msgId="msg.id"
        :msgIndex="index"
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
        :humanizeCron="humanizeCron"
        :repeatLabel="repeatLabel"
        :truncate="truncate"
        :getAgentBackend="getAgentBackend"
        :getAgentName="getAgentName"
        :staticBlockCache="staticBlockCache"
        :active="active"
        @toggle-tool="$emit('toggle-tool', $event)"
        @show-tool-detail="$emit('show-tool-detail', $event)"
        @task-card-click="$emit('task-card-click', $event)"
        @send-message="$emit('send-message', $event)"
        @render-flush="$emit('render-flush')"
        @toggle-summary="$emit('toggle-summary', msg.id)"
        @resume-session="$emit('resume-session', $event)"
        @reset-session="$emit('reset-session', $event)"

      />
    </div>

    <!-- Pending hint for queued user messages -->
    <div v-if="msg.pending" class="pending-hint">
      <span class="pending-spinner"></span>
      {{ t('chat.pending.queuing') }}
      <button class="pending-remove" @click="$emit('remove-pending', msg.queueId || msg.id)" :title="t('common.remove')">×</button>
    </div>

    <!-- File changes banner — standalone button above toolbar -->
    <button v-if="msg.role === 'assistant' && !msg.streaming && hasFileChanges" class="chat-file-changes-banner" @click="fileChangesDrawer.open()">
      <FileDiff :size="14" />
      <span>{{ t('chat.fileChanges.title') }}</span>
      <span class="chat-file-changes-count">{{ fileChanges.created.length + fileChanges.modified.length }}</span>
    </button>

    <!-- Cancelled marker: shown after file changes banner, hidden when last block is thinking (shown inline in thinking-header instead) -->
    <div v-if="msg.cancelled && !isLastBlockThinking" class="chat-cancelled-mark">{{ t('chat.contentBlocks.cancelled') }}</div>

    <!-- Bottom bar for assistant messages -->
    <div v-if="msg.role === 'assistant' && !msg.streaming && (msgText || msg.blocks?.length || msg.summary)" class="chat-meta-bar">
      <span class="chat-meta-info">
        <span v-if="msg.metadata?.wallMs" class="chat-meta-duration">{{ formatDuration(msg.metadata.wallMs) }}</span>
      </span>
      <div class="chat-meta-actions">
        <span v-if="!msg.streaming" ref="toggleWrapRef" class="chat-summary-anchor">
          <SummaryToggle v-if="!msg._summarizing" mode="button" :showing-summary="showSummary" i18n-prefix="chat.message" @toggle="handleToggleSummary" />
          <button v-else class="chat-action-btn chat-action-btn--wide" disabled>
            <Clock :size="14" class="speak-spinner" />
            <span>{{ t('chat.message.summarizing') }}</span>
          </button>
        </span>
        <span v-if="msg._loadingOriginal" class="chat-loading-original">{{ t('chat.message.loadingOriginal') }}</span>
        <button v-if="msgText" ref="speakBtnRef" class="chat-action-btn chat-action-btn--wide" :class="{ active: autoSpeech.isActive(msg.id), loading: autoSpeech.isGeneratingText(msg.id) }" @click.stop="handleSpeak">
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
            <span>{{ t('chat.message.readAloud') }}</span>
          </template>
        </button>
        <button v-if="!msg.streaming" class="chat-action-btn" :class="{ 'is-copied': copied }" @click="handleCopyMessage" :title="copied ? t('common.copied') : t('chat.message.copy')" :aria-label="copied ? t('common.copied') : t('chat.message.copy')">
          <span v-if="copied" class="chat-copy-copied-text">{{ t('common.copied') }}</span>
          <Copy v-else :size="14" />
        </button>
        <button v-if="!msg.streaming" class="chat-action-btn" @click="$emit('fork-from-message', msg)" :title="t('chat.actions.forkSession')">
          <Split :size="14" />
        </button>
        <button v-if="!msg.streaming" class="chat-action-btn" @click="$emit('show-metadata', msg)" :title="t('chat.message.viewDetails')">
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
import { Clock, Pause, Volume2, Info, FileDiff, Copy, Split } from 'lucide-vue-next'
import { formatDuration } from '@/utils/format.ts'
import { copyText } from '@/utils/clipboard.ts'
import { extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { extractFileChanges } from '@/utils/chatStreamUtils.ts'
import { isShowingSummary } from '@/utils/chatSessionUtils.ts'
import { localConfig } from '@/composables/useSettingsConfig'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import ContentBlocks from './ContentBlocks.vue'
import FileAttachmentList from './FileAttachmentList.vue'
import FileChangesDrawer from './FileChangesDrawer.vue'
import FileDiffsDrawer from './FileDiffsDrawer.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import SummaryToggle from '@/components/common/SummaryToggle.vue'


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
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'remove-pending', 'fork-from-message', 'reset-session'])

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
  // Recompute scroll so the button stays at its original viewport top.
  const adjust = () => {
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
const displayMode = computed(() => (localConfig.messageDisplayMode === 'original' ? 'original' : 'summary'))

const msgText = computed(() => {
  if (props.msg?.role !== 'assistant') return ''
  const text = extractSpeakableText(props.msg?.blocks || [])
  if (text) return text
  if (showSummary.value && props.msg?.summary) return props.msg.summary
  return ''
})

// Whether to render the summary view. Computed from message state (summary
// exists, content stripped), the user's explicit preference, and the global
// default display mode. While the full text is being lazily fetched in
// original mode, keep showing the summary as a placeholder so the message
// bubble is never blank.
const showSummary = computed(() => (props.msg ? isShowingSummary(props.msg, displayMode.value) : false))

// In global original mode, a summarized message whose content was stripped by
// Backend stripped content has nothing to render in original view — request the full text
// once. Guarded by _loadingOriginal, blocks-present, and _loadAttempted (latch
// set after the first fetch completes) to fire exactly once even on failure.
const needsLazyOriginal = computed(() =>
  displayMode.value === 'original' &&
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

const { renderTextBlock, toolCallSummary, formatToolInput, humanizeCron, repeatLabel, truncate, hasImagesInContent } = chatRender
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
  // AI may return absolute paths (e.g. /home/user/project/src/foo.ts).
  // Strip projectRoot prefix so openFilePath doesn't treat them as external.
  const root = store.state.projectRoot
  const relPath = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  openFilePath(relPath, lineStart, lineEnd)
}

// Copy message markdown — only the final conclusion (last text block)
const copied = ref(false)
function handleCopyMessage() {
  if (copied.value) return
  // Reuse the same text extraction as read-aloud: all text + AskUserQuestion
  // blocks, falling back to the summary for summary-only (empty-blocks) view.
  const text = msgText.value
  if (!text) return
  copyText(text, () => {
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  })
}
</script>

<style scoped>
/* Video player in chat */
.chat-video-wrapper {
  margin: 8px 0;
}

.chat-video-player {
  width: 100%;
  max-width: 400px;
  max-height: 225px;
  border-radius: var(--radius-sm);
  outline: none;
  background: #000;
}

/* Image thumbnails in user messages */
.chat-image-thumb {
  max-width: 80px;
  max-height: 80px;
  object-fit: cover;
  border-radius: 6px;
  display: block;
}

/* Image thumbnail style */
.chat-message .chat-img {
  vertical-align: middle;
}

/* ── Message content wrapper ── */
.msg-content-wrapper {
  position: relative;
}

/* ── Cancelled marker (shown after file changes banner) ── */
.chat-cancelled-mark {
  display: inline-block;
  font-size: 11px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 2px 8px;
  border-radius: 4px;
  margin-top: 4px;
}

/* ── File changes banner ── */
.chat-file-changes-banner {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    padding: 6px 10px;
    margin-top: 6px;
    border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
    border-radius: 2px;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
    color: var(--accent-color, #0066cc);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    transition: background 0.15s, border-color 0.15s, box-shadow 0.15s;
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
    font-size: 11px;
    font-weight: 600;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
    border-radius: 2px;
    padding: 0 6px;
    min-width: 18px;
    text-align: center;
    line-height: 18px;
}

/* Chat Meta Bar — contains model/duration info + detail button */
.chat-meta-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 4px;
    gap: 6px;
}

.chat-meta-info {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    color: color-mix(in srgb, var(--text-secondary) 70%, transparent);
    min-width: 0;
    overflow: hidden;
}

.chat-meta-sep::before {
    content: '·';
    margin-right: 6px;
}

.chat-meta-duration {
    font-variant-numeric: tabular-nums;
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
    font-size: 11px;
    font-weight: 500;
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
    gap: 2px;
}

/* Wrapper around the summary/original toggle button — used as the scroll anchor */
.chat-summary-anchor {
    display: inline-flex;
    align-items: center;
}

/* Loading hint shown while lazily fetching the original message content */
.chat-loading-original {
    font-size: 12px;
    color: var(--text-secondary, #888);
    padding: 0 6px;
}

/* Speak button loading spinner animation */
.chat-action-btn.loading .speak-spinner {
    animation: speak-spin 1s linear infinite;
}

@keyframes speak-spin {
    to { transform: rotate(360deg); }
}

/* User message meta bar */
.chat-meta-bar-user {
    color: color-mix(in srgb, var(--text-secondary) 60%, transparent);
    transition: color 0.2s;
}

/* ── Pending (queued) user message styles ── */
.chat-message.user.pending {
    color: rgba(255, 255, 255, 0.55);
    background: color-mix(in srgb, var(--user-msg-color) 55%, transparent);
    border: 1px dashed rgba(255, 255, 255, 0.5);
    animation: pending-fade-in 0.25s ease-out;
}

.pending-hint {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: 10px;
    color: rgba(255, 255, 255, 0.7);
    flex-basis: 100%;
    margin-top: 4px;
}

.pending-spinner {
    width: 10px;
    height: 10px;
    border: 1.5px solid rgba(255, 255, 255, 0.3);
    border-top-color: rgba(255, 255, 255, 0.8);
    border-radius: 50%;
    animation: pending-spin 0.6s linear infinite;
}

.pending-remove {
    background: none;
    border: none;
    cursor: pointer;
    color: rgba(255, 255, 255, 0.6);
    padding: 0 2px;
    font-size: 13px;
    line-height: 1;
    transition: color 0.15s;
}

@media (hover: hover) {
  .pending-remove:hover {
    color: rgba(255, 255, 255, 1);
  }
}

@keyframes pending-spin {
    to { transform: rotate(360deg); }
}

@keyframes pending-fade-in {
    from { opacity: 0; transform: translateY(6px); }
    to { opacity: 1; transform: translateY(0); }
}

@media (hover: hover) {
  .chat-meta-bar-user:hover {
    color: var(--text-secondary);
  }
}

.chat-info-btn-user {
    color: rgba(255, 255, 255, 0.7);
}

@media (hover: hover) {
  .chat-info-btn-user:hover {
    color: rgba(255, 255, 255, 0.9);
    background: rgba(255, 255, 255, 0.1);
  }
}

.chat-meta-bar-user .chat-meta-info {
    color: rgba(255, 255, 255, 0.7);
}
</style>

<style>
/* Chat message - non-scoped for v-html penetration */
.chat-message {
    padding: 8px 12px;
    font-size: 13px;
    line-height: 1.4;
    min-width: 0;
    word-wrap: break-word;
    overflow-wrap: break-word;
    word-break: break-word;
    max-width: 100%;
    box-sizing: border-box;
    contain: style;
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
  gap: 6px;
  margin: 4px 0;
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
  gap: 6px;
  border-radius: 6px;
  height: 40px;
  padding: 0 10px;
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: opacity 0.15s;
  flex-shrink: 0;
  box-sizing: border-box;
}

.chat-message .attachment-filename {
  font-family: monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-message .attachment-filesize {
  font-size: 10px;
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
  border-radius: 8px;
}

.chat-message .attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.chat-file-tag-path {
  font-family: monospace;
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
  border-radius: 6px;
  padding: 2px;
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

.chat-message.user {
    background: var(--user-msg-color);
    color: white;
    align-self: flex-end;
    border-radius: 20px 20px 0 20px;
    margin-right: 10px;
    max-width: calc(100% - 20px);
    overflow: hidden;
}

.chat-message.assistant {
    background: var(--bg-tertiary);
    color: var(--text-primary);
    align-self: stretch;
    border-radius: 0;
    position: relative;
    min-width: 0;
    overflow: hidden;
    overflow-wrap: break-word;
}

.chat-message.user pre {
    padding: 10px;
    margin: 6px 0;
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
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    padding: 2px 6px;
    font-size: 13px;
    background: rgba(0, 0, 0, 0.15);
}

.chat-message.user h1,
.chat-message.user h2,
.chat-message.user h3 {
    margin: 6px 0 3px;
    font-weight: 600;
}

.chat-message.user h1 { font-size: 16px; }
.chat-message.user h2 { font-size: 14px; }
.chat-message.user h3 { font-size: 13px; }

.chat-message.user p {
    margin: 3px 0;
}

.chat-message.user ul,
.chat-message.user ol {
    margin: 6px 0;
}

.chat-message.user blockquote {
    margin: 6px 0;
    padding: 5px 10px;
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
    margin: 6px 0;
}

.chat-message.user hr {
    margin: 8px 0;
    border-top-color: rgba(255, 255, 255, 0.25);
}

.chat-message.user .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: 6px 6px 0 0;
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
    font-size: 13px;
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
    padding: 10px;
    margin: 6px 0;
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
    font-family: 'SF Mono', Monaco, 'Cascadia Code', 'Segoe UI Mono', 'Roboto Mono', Consolas, 'Liberation Mono', monospace;
    padding: 2px 6px;
    font-size: 13px;
}

.chat-message.assistant h1,
.chat-message.assistant h2,
.chat-message.assistant h3 {
    margin: 6px 0 3px;
    font-weight: 600;
}

.chat-message.assistant h1 { font-size: 16px; }
.chat-message.assistant h2 { font-size: 14px; }
.chat-message.assistant h3 { font-size: 13px; }

.chat-message.assistant p {
    margin: 3px 0;
}

.chat-message.assistant ul,
.chat-message.assistant ol {
    margin: 6px 0;
}

.chat-message.assistant blockquote {
    margin: 6px 0;
    padding: 5px 10px;
}

.chat-message.assistant a {
    word-break: break-all;
    overflow-wrap: break-word;
}

.chat-message.assistant img {
    margin: 6px 0;
}

.chat-message.assistant hr {
    margin: 8px 0;
}

.chat-message.assistant .table-wrap {
    overflow-x: auto;
    border: none;
    border-radius: 6px 6px 0 0;
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
    font-size: 13px;
    color: var(--text-primary);
}

.chat-message.assistant td {
    white-space: nowrap;
}

/* Mermaid diagram thumbnail */
.chat-message .mermaid {
  max-width: 200px;
  max-height: 200px;
  overflow: hidden;
  border-radius: 6px;
  margin: 4px 0;
  background: var(--bg-secondary);
  padding: 8px;
  position: relative;
}

/* Mermaid expand icon — top-right corner, visible on hover (PC mode) */
.chat-message .mermaid .lightbox-expand-icon {
  display: none;
  position: absolute;
  top: 4px;
  right: 4px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 14px;
  line-height: 24px;
  text-align: center;
  cursor: pointer;
  z-index: 2;
  align-items: center;
  justify-content: center;
}

.chat-message .mermaid .lightbox-expand-icon::after {
  content: '⤢';
}

@media (hover: hover) {
  .chat-message .mermaid:hover .lightbox-expand-icon {
    display: flex;
  }
}

.chat-message .mermaid svg {
  max-width: 100%;
  max-height: 184px;
  height: auto;
}

/* ── Lightbox: raster image wrapper — positions the expand icon overlay ──
   These must be NON-scoped: the image/svg markup arrives via v-html, so the
   injected elements carry no data-v-* attribute and scoped styles never match. */
.chat-message .lightbox-img-wrap,
.chat-message .lightbox-svg-wrap {
  position: relative;
  display: inline-block;
}

.chat-message .lightbox-img-wrap .lightbox-img {
  cursor: default;
}

/* Expand icon — top-right corner, visible on hover (PC mode) */
.chat-message .lightbox-img-wrap .lightbox-expand-icon,
.chat-message .lightbox-svg-wrap .lightbox-expand-icon {
  display: none;
  position: absolute;
  top: 4px;
  right: 4px;
  width: 24px;
  height: 24px;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  cursor: pointer;
  z-index: 2;
  pointer-events: auto;
}

@media (hover: hover) {
  .chat-message .lightbox-img-wrap:hover .lightbox-expand-icon,
  .chat-message .lightbox-svg-wrap:hover .lightbox-expand-icon {
    display: flex;
    align-items: center;
    justify-content: center;
  }
}

/* Use a simple "expand" character as the icon (no SVG dependency in HTML strings) */
.chat-message .lightbox-img-wrap .lightbox-expand-icon::after,
.chat-message .lightbox-svg-wrap .lightbox-expand-icon::after {
  content: '⤢';
  font-size: 14px;
  line-height: 1;
}

/* Inline SVG thumbnail (non-mermaid) — constrained like the mermaid preview */
.chat-message .lightbox-svg-wrap svg.lightbox-svg {
  max-width: 200px;
  max-height: 200px;
  height: auto;
  border-radius: 6px;
}

/* ── Audio player in chat (non-scoped for v-html penetration) ── */
.chat-message .chat-audio-wrapper {
  margin: 4px 0;
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
  padding: 0 4px;
}

.chat-message .chat-audio-player::-webkit-media-controls-play-button {
  margin: 0 2px;
}

.chat-message .chat-audio-player::-webkit-media-controls-current-time-display,
.chat-message .chat-audio-player::-webkit-media-controls-time-remaining-display {
  font-size: 11px;
}
</style>
