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
        :blockRagResults="blockRagResults"
        :streaming="msg.streaming"
        :cancelled="msg.cancelled"
        :summary="msg.summary"
        :showingSummary="msg.showingSummary"
        :renderTextBlock="renderTextBlock"
        :formatToolInput="formatToolInput"
        :toolCallSummary="toolCallSummary"
        :humanizeCron="humanizeCron"
        :repeatLabel="repeatLabel"
        :truncate="truncate"
        :getAgentIcon="getAgentIcon"
        :getAgentName="getAgentName"
        :staticBlockCache="staticBlockCache"
        :active="active"
        @toggle-tool="$emit('toggle-tool', $event)"
        @show-tool-detail="$emit('show-tool-detail', $event)"
        @show-thinking-detail="$emit('show-thinking-detail', $event)"
        @task-card-click="$emit('task-card-click', $event)"
        @send-message="$emit('send-message', $event)"
        @render-flush="$emit('render-flush')"
        @toggle-summary="$emit('toggle-summary', msg.id)"
        @resume-session="$emit('resume-session', $event)"
        @show-rag-detail="$emit('show-rag-detail', $event)"
      />
    </div>

    <!-- Pending hint for queued user messages -->
    <div v-if="msg.pending" class="pending-hint">
      <span class="pending-spinner"></span>
      {{ t('chat.pending.queuing') }}
      <button class="pending-remove" @click="$emit('remove-pending', msg.content)" :title="t('common.remove')">×</button>
    </div>

    <!-- Bottom bar for assistant messages -->
    <div v-if="msg.role === 'assistant' && !msg.streaming && (msgText || msg.blocks?.length)" class="chat-meta-bar">
      <span class="chat-meta-info">
        <span v-if="msg.metadata?.wallMs" class="chat-meta-duration">{{ formatDuration(msg.metadata.wallMs) }}</span>
      </span>
      <div class="chat-meta-actions">
        <button v-if="hasFileChanges" class="chat-action-btn chat-action-btn--wide" @click="fileChangesOpen = true" :title="t('chat.fileChanges.title')">
          <FileDiff :size="14" />
          <span>{{ t('chat.fileChanges.title') }}</span>
        </button>
        <SummaryToggle v-if="msg.summary && !msg.streaming" mode="button" :showing-summary="msg.showingSummary" i18n-prefix="chat.message" @toggle="$emit('toggle-summary', msg.id)" />
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
      @close="fileChangesOpen = false"
      @open-file="handleOpenFile"
    />
    <!-- Bottom bar for user messages -->
    <div v-if="msg.role === 'user' && !msg.pending" class="chat-meta-bar chat-meta-bar-user">
      <span class="chat-meta-info">
      </span>
      <button class="chat-action-btn chat-info-btn-user" @click="$emit('show-metadata', msg)" :title="t('chat.message.viewDetails')">
        <Info :size="14" />
      </button>
    </div>

  </div>
</template>

<script setup>
import { ref, inject, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Clock, Pause, Volume2, Info, FileDiff } from 'lucide-vue-next'
import { formatDuration } from '@/utils/format.ts'
import { extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { extractFileChanges } from '@/utils/chatStreamUtils.ts'
import { openFilePath } from '@/composables/useFilePathAnnotation.ts'
import { store } from '@/stores/app.ts'
import ContentBlocks from './ContentBlocks.vue'
import FileAttachmentList from './FileAttachmentList.vue'
import FileChangesDrawer from './FileChangesDrawer.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import SummaryToggle from '@/components/common/SummaryToggle.vue'


const { t } = useI18n()

const props = defineProps({
  msg: Object,
  index: Number,
  expandedTools: Object,
  blockTasks: Object,
  blockAskQuestions: Object,
  blockRagResults: Object,
  agents: Array,
  staticBlockCache: Object,
  active: { type: Boolean, default: true },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-thinking-detail', 'show-metadata', 'file-tag-click', 'task-card-click', 'send-message', 'render-flush', 'toggle-summary', 'resume-session', 'show-rag-detail', 'remove-pending'])

const autoSpeech = inject('autoSpeech')
const wrapperRef = ref(null)
const speakBtnRef = ref(null)

// Extract text content from message blocks for TTS.
// Uses extractSpeakableText to include AskUserQuestion blocks.
const msgText = computed(() => {
  if (props.msg?.role !== 'assistant') return ''
  return extractSpeakableText(props.msg?.blocks || [])
})

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
const { getAgentIcon, getAgentName } = chatSession

// File changes extraction (Write → created, Edit → modified)
const fileChanges = computed(() => {
  if (props.msg?.role !== 'assistant' || props.msg.streaming) return { created: [], modified: [] }
  return extractFileChanges(props.msg?.blocks || [])
})
const hasFileChanges = computed(() => fileChanges.value.created.length > 0 || fileChanges.value.modified.length > 0)

const fileChangesOpen = ref(false)
const fileChangesDrawer = useTabDrawer('chat', fileChangesOpen)

function handleOpenFile(path) {
  // AI may return absolute paths (e.g. /home/user/project/src/foo.ts).
  // Strip projectRoot prefix so openFilePath doesn't treat them as external.
  const root = store.state.projectRoot
  const relPath = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  openFilePath(relPath)
}
</script>

<style scoped>
/* Audio player in chat */
.chat-audio-wrapper {
  margin: 8px 0;
}

.chat-audio-player {
  width: 100%;
  max-width: 280px;
  height: 36px;
  border-radius: var(--radius-sm);
  outline: none;
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
.chat-message .chat-img-thumbnail {
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
}

.chat-message .chat-img-thumbnail:hover {
  transform: scale(1.02);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

/* ── Message content wrapper ── */
.msg-content-wrapper {
  position: relative;
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

.chat-action-btn.active:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
}

/* Meta bar action buttons container */
.chat-meta-actions {
    display: flex;
    align-items: center;
    gap: 2px;
}

/* Speak button loading spinner animation */
.chat-speak-btn.loading .speak-spinner {
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

.pending-remove:hover {
    color: rgba(255, 255, 255, 1);
}

@keyframes pending-spin {
    to { transform: rotate(360deg); }
}

@keyframes pending-fade-in {
    from { opacity: 0; transform: translateY(6px); }
    to { opacity: 1; transform: translateY(0); }
}

.chat-meta-bar-user:hover {
    color: var(--text-secondary);
}

.chat-info-btn-user {
    color: rgba(255, 255, 255, 0.7);
}

.chat-info-btn-user:hover {
    color: rgba(255, 255, 255, 0.9);
    background: rgba(255, 255, 255, 0.1);
}

.chat-meta-bar-user .chat-meta-info {
    color: rgba(255, 255, 255, 0.7);
}
</style>

<style>
/* Chat message - non-scoped for v-html penetration */
.chat-message {
    padding: 8px 12px;
    border-radius: var(--radius-md);
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
  flex-wrap: wrap;
  gap: 4px;
  margin: 4px 0;
}

/* Common file tag styles - shared by both current file and uploaded attachments */
.chat-file-tag,
.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-radius: 8px;
  padding: 1px 6px;
  margin-bottom: 4px;
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: opacity 0.15s;
  white-space: nowrap;
  max-width: 200px;
}

.chat-file-tag-icon,
.chat-file-attachment svg {
  flex-shrink: 0;
}

.chat-file-tag-path,
.chat-file-name {
  font-family: monospace;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.chat-file-tag-path::-webkit-scrollbar,
.chat-file-name::-webkit-scrollbar {
  display: none;
}

/* User message: common colors */
.chat-message.user .chat-file-tag,
.chat-message.user .chat-file-attachment {
  color: rgba(255, 255, 255, 0.95);
}

.chat-message.user .chat-file-tag-path,
.chat-message.user .chat-file-name {
  color: rgba(255, 255, 255, 0.95);
}

.chat-message.user .chat-file-tag-icon,
.chat-message.user .chat-file-attachment svg {
  stroke: rgba(255, 255, 255, 0.95);
}

/* User message: uploaded - solid border */
.chat-message.user .attachment-upload {
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.35);
}

/* User message: referenced - dashed border */
.chat-message.user .attachment-ref {
  background: rgba(255, 255, 255, 0.15);
  border: 1px dashed rgba(255, 255, 255, 0.6);
}

.chat-message.user .attachment-ref:hover,
.chat-message.user .chat-file-tag:hover {
  background: rgba(255, 255, 255, 0.25);
}

/* Assistant message: common colors */
.chat-message.assistant .chat-file-tag,
.chat-message.assistant .chat-file-attachment {
  color: var(--text-secondary);
}

.chat-message.assistant .chat-file-tag-path,
.chat-message.assistant .chat-file-name {
  color: var(--text-secondary);
}

.chat-message.assistant .chat-file-tag-icon,
.chat-message.assistant .chat-file-attachment svg {
  stroke: var(--text-secondary);
}

/* Assistant message: uploaded - solid border */
.chat-message.assistant .attachment-upload {
  background: var(--bg-primary);
  border: 1px solid var(--border-color);
}

/* Assistant message: referenced - dashed border */
.chat-message.assistant .attachment-ref {
  background: color-mix(in srgb, var(--text-muted, #999) 8%, transparent);
  border: 1px dashed var(--text-secondary);
}

.chat-message.assistant .attachment-ref:hover,
.chat-message.assistant .chat-file-tag:hover {
  background: var(--bg-secondary);
}

.chat-message.user {
    background: var(--user-msg-color);
    color: white;
    align-self: flex-end;
    border-radius: 16px 16px 0 16px;
    overflow: hidden;
}

.chat-message.assistant {
    background: var(--bg-tertiary);
    color: var(--text-primary);
    align-self: stretch;
    border-radius: 16px 16px 16px 0;
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

.chat-message.user code {
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
    color: #b8daff;
}

.chat-message.user a:hover {
    color: #9dc5f0;
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
    border-radius: 6px;
    margin: 0.75em 0;
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

.chat-message.user .chat-file-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
}
.chat-message.user .chat-file-open-btn.external {
    color: #f0a04b;
}

.chat-message.user .chat-commit-hash {
    color: rgba(255, 255, 255, 0.9);
}

.chat-message.user .chat-commit-open-btn {
    color: rgba(255, 255, 255, 0.7);
}

.chat-message.user .chat-commit-open-btn:hover {
    color: white;
    background: rgba(255, 255, 255, 0.15);
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
    border-radius: 6px;
    margin: 0.75em 0;
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
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
  background: var(--bg-secondary);
  padding: 8px;
}

.chat-message .mermaid:hover {
  transform: scale(1.02);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.chat-message .mermaid svg {
  max-width: 100%;
  max-height: 184px;
  height: auto;
}
</style>
