<template>
  <div class="exec-detail-page">
    <!-- Header: breadcrumb + actions -->
    <div class="exec-detail-header">
      <TaskBreadcrumb />
    </div>

    <!-- Scrollable message content -->
    <div class="exec-detail-content" ref="contentRef" @click="handleContentClick" @mousedown="onTableMouseDown" @touchstart="onContentTouchStart" @touchend="onContentTouchEnd" @touchcancel="onContentTouchEnd" @scroll="handleScroll">
      <!-- Summary / Original tab bar (hidden during live streaming) -->
      <SummaryToggle v-if="hasSummary && !execStream.isStreaming.value && !isRunning" mode="tab" :showing-summary="activeTab === 'summary'" i18n-prefix="task.exec" @toggle="setTab(activeTab === 'summary' ? 'original' : 'summary')" />
      <ChatMessageItem
        v-if="activeMsgData"
        :msg="activeMsgData"
        :index="0"
        :expandedTools="expandedTools"
        :blockTasks="{}"
        :blockAskQuestions="{}"
        @toggle-tool="toggleTool"
        @show-tool-detail="handleShowToolDetail"
        @show-metadata="showMetadata"
        @task-card-click="() => {}"
        @render-flush="scrollToBottom"
      />
      <div v-else-if="execDetail?.status === 'cancelled'" class="exec-cancelled-notice">{{ t('task.exec.cancelledNotice') }}</div>
      <div v-else class="exec-detail-empty">{{ isRunning ? t('task.exec.startingPreview') : t('task.exec.noTextOutput') }}</div>
    </div>

    <!-- Fixed bottom action bar -->
    <div class="exec-detail-actions">
      <button v-if="showContinueBtn" class="action-btn accent" :disabled="continueLoading || isRunning" @click="onContinueConversation" :title="t('task.exec.continueConversation')">
        <MessageSquare :size="14" />
        <span class="action-text">{{ continueLoading ? t('task.exec.continueConversationLoading') : t('task.exec.continueConversation') }}</span>
      </button>
      <span class="actions-spacer"></span>
      <button v-if="isRunning" class="action-btn danger" :disabled="cancelling" @click="onTerminate" :title="t('task.exec.cancel')">
        <Square :size="14" />
        <span class="action-text">{{ cancelling ? t('common.loading') : t('task.exec.cancel') }}</span>
      </button>
      <button class="action-btn" :class="{ spinning: refreshing }" :disabled="refreshing" @click="onRefresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
    </div>

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
      @close="closeOverlay"
      @file-open="handleFileOpenInOverlay"
      @click="handleOverlayRetryClick"
    />

    <!-- Metadata Modal -->
    <ChatMetadataModal
      :show="metadataModal.show"
      :data="metadataModal.data"
      :backend="metadataModal.backend"
      :createdAt="metadataModal.createdAt"
      :relatedFile="metadataModal.relatedFile"
      :messageId="metadataModal.messageId"
      :sessionId="metadataModal.sessionId"
      :ftsIndexed="metadataModal.ftsIndexed"
      :vecIndexed="metadataModal.vecIndexed"
      :formatDetailTime="chatRender.formatDetailTime"
      @close="metadataModal.show = false"
    />

    <!-- Table row expand modal -->
    <TableRowModal
      :data="tableRowModal"
      @close="closeTableRowModal"
      @prev="tableRowPrev"
      @next="tableRowNext"
    />
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick, provide, onUnmounted, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, MessageSquare, Square } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import ToolDetailDrawer from '@/components/chat/ToolDetailDrawer.vue'
import ChatMetadataModal from '@/components/chat/ChatMetadataModal.vue'
import SummaryToggle from '@/components/common/SummaryToggle.vue'
import { useChatRender } from '@/composables/useChatRender.ts'
import { useAgents } from '@/composables/useAgents'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { store as appStore } from '@/stores/app.ts'
import { useAutoSpeech } from '@/composables/useAutoSpeech.ts'
import { useTaskTab } from '@/composables/useTaskTab.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { useTaskExecStream } from '@/composables/useTaskExecStream.ts'
import { apiPut } from '@/utils/api.ts'
import { useToast } from '@/composables/useToast.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { formatToolOutput } from '@/utils/renderToolDetail.ts'
import TableRowModal from '@/components/common/TableRowModal.vue'

const props = defineProps({
  execDetail: Object,
  taskName: String,
  taskId: Number,
})

const emit = defineEmits(['close', 'open-file'])

const { t } = useI18n()
const { refreshExecDetail } = useTaskTab()
const identity = useSessionIdentity()
const theme = inject('theme', ref('light'))
const { openFilePath, verifyFilePaths } = useFilePathAnnotation()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()
const switchTab = inject('switchTab', () => {})
const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

// ── Continue conversation logic ──
const continueLoading = ref(false)
const isRunning = computed(() => props.execDetail?.status === 'running')

// ── Terminate (cancel) running execution ──
const cancelling = ref(false)
const toast = useToast()
const dialog = useDialog()

// ── Live preview stream ──
const execStatusRef = computed(() => props.execDetail?.status || '')
const execSessionIdRef = computed(() => props.execDetail?.sessionId || null)
const execStream = useTaskExecStream({
  sessionId: execSessionIdRef,
  status: execStatusRef,
  onComplete: () => {
    // Execution completed while previewing — do a final refresh
    refreshExecDetail()
    // Refresh git state — task execution may have modified files or switched branches
    appStore.loadGitBranch().catch(() => {})
  },
})
const showContinueBtn = computed(() => {
  // Show button for completed or cancelled executions, not for running ones
  const status = props.execDetail?.status
  return status && status !== 'running' && props.taskId && props.execDetail?.id
})

async function onTerminate() {
  if (!props.taskId || !props.execDetail?.id || cancelling.value) return
  if (!await dialog.confirm(t('task.exec.confirmCancel'))) return
  cancelling.value = true
  try {
    await apiPut(`/api/tasks/${props.taskId}`, {
      action: 'cancel',
      executionId: String(props.execDetail.id),
    })
    toast.show(t('task.exec.cancelled'), { icon: '✅', type: 'success' })
    execStream.stopPreview()
    refreshExecDetail()
  } catch (err) {
    if (err instanceof Error && err.message.includes('404')) {
      toast.show(t('task.exec.alreadyFinished'), { icon: 'ℹ️', type: 'info' })
    } else {
      toast.show(t('task.exec.actionFailedDetail', { error: err?.message || String(err) }), { icon: '⚠️', type: 'error' })
    }
  } finally {
    cancelling.value = false
  }
}

async function onContinueConversation() {
  if (!props.taskId || !props.execDetail?.id || continueLoading.value) return
  continueLoading.value = true
  try {
    await identity.continueFromExecution(props.taskId, Number(props.execDetail.id), switchTab)
  } finally {
    continueLoading.value = false
  }
}

// ── Refresh logic ──
const refreshing = ref(false)

async function onRefresh() {
  refreshing.value = true
  try {
    await refreshExecDetail()
  } finally {
    refreshing.value = false
  }
}

// ── Agents (for getAgentBackend/getAgentName) ──
const { getAgentBackend, getAgentName } = useAgents()

// ── ChatRender — full pipeline for markdown rendering ──
const messages = ref([])
const chatRender = useChatRender({ messages, theme, currentSessionId: ref('') })

// ── Provide dependencies that ChatMessageItem injects ──
provide('chatRender', {
  renderTextBlock: chatRender.renderTextBlock,
  formatMessageTime: chatRender.formatMessageTime,
  toolCallSummary: chatRender.toolCallSummary,
  formatToolInput: chatRender.formatToolInput,
  humanizeCron: chatRender.humanizeCron,
  repeatLabel: chatRender.repeatLabel,
  truncate: chatRender.truncate,
  hasImagesInContent: chatRender.hasImagesInContent,
})
provide('chatSession', { getAgentBackend, getAgentName })
provide('chatUI', { navigateToFileViewer: () => emit('close') })
provide('autoSpeech', useAutoSpeech())
provide('layoutRefreshKey', ref(0))

// ── Summary / Original toggle ──
const hasSummary = computed(() => props.execDetail?.summary != null && props.execDetail.summary !== '')
const activeTab = ref(hasSummary.value ? 'summary' : 'original')

function setTab(tab) {
  activeTab.value = tab
}

// ── Build a synthetic message object for ChatMessageItem (original content) ──
const msgData = computed(() => {
  if (!props.execDetail?.content && props.execDetail?.status !== 'cancelled') return null
  const { blocks } = chatRender.parseAssistantContent(props.execDetail.content || '{}')
  // Use messageId (DB chat_history ID) for tool detail fetch; fallback only if unavailable
  const msgId = props.execDetail.messageId || props.execDetail.id || 'exec'
  if (!blocks || blocks.length === 0) {
    // For running executions with empty content, return a streaming placeholder
    // so the live indicator bar is shown instead of "no text output"
    if (isRunning.value) {
      return {
        id: msgId,
        role: 'assistant',
        content: '',
        blocks: [],
        metadata: null,
        createdAt: props.execDetail.createdAt || '',
        streaming: true,
        cancelled: false,
      }
    }
    return null
  }
  return {
    id: msgId,
    role: 'assistant',
    content: props.execDetail.content,
    blocks,
    metadata: props.execDetail.metadata || null,
    createdAt: props.execDetail.createdAt || '',
    // While the execution is still running, always mark the message as
    // streaming so the "streaming status" indicator is shown. Otherwise a
    // running task's partial DB content renders like a completed answer.
    streaming: isRunning.value,
    cancelled: false,
  }
})

// ── Build a synthetic message object for ChatMessageItem (summary content) ──
const summaryMsgData = computed(() => {
  if (!props.execDetail?.summary) return null
  const summaryJson = JSON.stringify({ blocks: [{ type: 'text', text: props.execDetail.summary }] })
  const { blocks } = chatRender.parseAssistantContent(summaryJson)
  if (!blocks || blocks.length === 0) return null
  return {
    id: (props.execDetail.id || 'exec') + '-summary',
    role: 'assistant',
    content: summaryJson,
    blocks,
    metadata: props.execDetail.metadata || null,
    createdAt: props.execDetail.createdAt || '',
    streaming: false,
    cancelled: false,
  }
})

// ── Active message data based on tab ──
const activeMsgData = computed(() => {
  // When live streaming via WS, merge DB history blocks with streaming blocks
  // so the user sees both prior content and new real-time output.
  if (execStream.isStreaming.value && execStream.streamingMsg.value) {
    const sm = execStream.streamingMsg.value
    if (sm.blocks && sm.blocks.length > 0) {
      const dbBlocks = msgData.value?.blocks
      if (dbBlocks && dbBlocks.length > 0) {
        // Merge: DB history first, then streaming increments
        return { ...sm, blocks: [...dbBlocks, ...sm.blocks] }
      }
      return sm
    }
    // Streaming started but no content yet — show DB history so it doesn't flash away
    if (msgData.value) return msgData.value
  }
  // After streaming stops, the streamingMsg still has blocks (streaming flag removed).
  // Merge with DB history so prior content is preserved while waiting for refreshExecDetail.
  if (!execStream.isStreaming.value && execStream.streamingMsg.value) {
    const sm = execStream.streamingMsg.value
    if (sm.blocks && sm.blocks.length > 0) {
      const dbBlocks = msgData.value?.blocks
      if (dbBlocks && dbBlocks.length > 0) {
        return { ...sm, blocks: [...dbBlocks, ...sm.blocks], streaming: false }
      }
      return { ...sm, streaming: false }
    }
  }
  // When not streaming, use the DB content (refreshed by refreshExecDetail)
  // we always show whatever partial content is available rather than "connecting..."
  // During a running execution a summary isn't final yet, so never present it
  // as a completed answer.
  if (activeTab.value === 'summary' && summaryMsgData.value && !isRunning.value) return summaryMsgData.value
  return msgData.value
})

// ── Expanded tools state ──
const expandedTools = ref({})

function toggleTool(key) {
  expandedTools.value = { ...expandedTools.value, [key]: !expandedTools.value[key] }
}

// ── Tool Detail Overlay ──

/** Look up the tool_use block from the streaming message by msgId + blockIdx */
function findLiveToolBlock({ msgId, blockIdx }) {
  const sm = execStream.streamingMsg.value
  if (!sm || !sm.blocks) return null
  // For streaming messages, msgId comes from the streaming message id
  if (String(sm.id) !== String(msgId)) return null
  const block = sm.blocks[blockIdx]
  return (block && block.type === 'tool_use') ? block : null
}

const {
  drawer: toolDetailDrawer,
  isOpen: toolDetailIsOpen,
  toolDetailOverlay,
  toolDetailData,
  activeToolOverlay,
  handleShowToolDetail,
  handleOverlayRetryClick,
  handleFileOpenInOverlay,
  fetchToolCallDetail,
  closeOverlay,
} = useToolDetailDrawer({
  chatRender,
  tabId: 'tasks',
  onFileOpen: (path, lineStart, lineEnd) => {
    openFilePath(path, lineStart, lineEnd)
    emit('open-file', { path, lineStart, lineEnd })
  },
  findLiveBlock: findLiveToolBlock,
  sessionId: () => props.execDetail?.sessionId,
})

// Reactively update tool overlay content as block output/done/status changes during streaming
watch(
  () => {
    if (!activeToolOverlay.value) return null
    const block = findLiveToolBlock(activeToolOverlay.value)
    if (!block) return null
    return { output: block.output, done: block.done, status: block.status, input: block.input, name: block.name, summary: block.summary, display_name: block.display_name }
  },
  (data) => {
    if (data === null || !toolDetailIsOpen.value) return
    const { formatToolInput } = chatRender
    const hasInput = data.input && Object.keys(data.input).length > 0
    toolDetailData.value.outputHtml = data.output ? formatToolOutput(data.output, data.name) : toolDetailData.value.outputHtml
    toolDetailData.value.status = data.status || ''
    toolDetailData.value.done = !!data.done
    toolDetailData.value.inputHtml = hasInput ? formatToolInput(data.input, data.name, { done: data.done, status: data.status, output: data.output }) : toolDetailData.value.inputHtml
    toolDetailData.value.summary = data.summary || toolDetailData.value.summary
  }
)

// Clean up overlay state when drawer closes
watch(() => toolDetailIsOpen.value, (open) => {
  if (!open) {
    activeToolOverlay.value = null
  }
})

// Re-fetch tool detail when messageId becomes available (e.g. after refreshExecDetail completes)
// The initial execData from the history list may lack messageId, causing fetchToolCallDetail
// to fail. When refreshExecDetail updates selectedExecData with a valid messageId,
// retry the fetch if the overlay is still open and content is empty.
watch(() => props.execDetail?.messageId, (newMsgId) => {
  if (!newMsgId || !toolDetailIsOpen.value) return
  const ids = toolDetailData.value._fetchIds
  if (!ids) return
  // Only retry if input is still empty (fetch hasn't succeeded yet)
  if (toolDetailData.value.inputHtml && !toolDetailData.value.inputHtml.includes('tool-call-loading') && !toolDetailData.value.inputHtml.includes('tool-call-empty')) return
  // Retry with the correct messageId
  const block = findLiveToolBlock(activeToolOverlay.value || { msgId: '', blockIdx: 0 })
  fetchToolCallDetail(ids.toolId, newMsgId, block || { name: toolDetailData.value.name })
})

// Clear stale streamingMsg once DB content is available (DB is authoritative)
watch(() => props.execDetail?.content, (newContent) => {
  if (newContent && !execStream.isStreaming.value && execStream.streamingMsg.value) {
    execStream.streamingMsg.value = null
  }
})

// ── Metadata Modal ──
const metadataModal = ref({
  show: false,
  data: {},
  backend: '',
  createdAt: '',
  relatedFile: '',
  messageId: null,
  sessionId: '',
  ftsIndexed: false,
  vecIndexed: false,
})

function showMetadata() {
  const exec = props.execDetail
  if (!exec) return
  metadataModal.value.data = exec.metadata || {}
  metadataModal.value.backend = exec.backend || ''
  metadataModal.value.createdAt = exec.createdAt || ''
  metadataModal.value.relatedFile = ''
  metadataModal.value.messageId = exec.id || null
  metadataModal.value.sessionId = ''
  metadataModal.value.ftsIndexed = false
  metadataModal.value.vecIndexed = false
  metadataModal.value.show = true
}

// ── Delegated click handler for .chat-file-open-btn ──
const contentRef = ref(null)

// ── Auto-follow scroll (mirrors chat streaming UX) ──
// When live streaming output, keep pinned to the bottom unless the user
// manually scrolls elsewhere. Scrolling back to the bottom resumes following.
const isAtBottom = ref(true)
const NEAR_BOTTOM_THRESHOLD = 100
let userTouching = false

function handleScroll() {
  if (!contentRef.value) return
  const el = contentRef.value
  const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
  isAtBottom.value = distFromBottom < NEAR_BOTTOM_THRESHOLD
}

function onContentTouchStart(e) {
  userTouching = true
  onTableTouchStart(e)
}

function onContentTouchEnd() {
  // Short delay so the final scroll event from the user's gesture lands
  // before auto-scroll re-engages (prevents snap-back fighting the touch).
  setTimeout(() => { userTouching = false }, 150)
}

function scrollToBottom() {
  if (!contentRef.value || !isAtBottom.value || userTouching) return
  const el = contentRef.value
  el.scrollTop = el.scrollHeight
  // Re-check after layout — content may grow during streaming.
  // Only correct if the user hasn't scrolled up since (sticky-jitter guard).
  requestAnimationFrame(() => {
    if (!contentRef.value || !isAtBottom.value || userTouching) return
    const c = contentRef.value
    const gap = c.scrollHeight - c.scrollTop - c.clientHeight
    if (gap > 0) c.scrollTop = c.scrollHeight
  })
}

// Follow streaming updates while at the bottom
watch(activeMsgData, () => {
  nextTick(scrollToBottom)
})

function handleContentClick(event) {
  // 0. Code block header buttons (copy/wrap)
  if (handleCodeBlockClick(event)) return

  // 0.5. Table block header buttons (copy/wrap)
  if (handleTableBlockClick(event)) return

  // 1. Handle localhost URL clicks (icon button or <a> tag) — App mode only
  if (handleLocalhostUrlClick(event)) return

  // 2. Handle table row click — open row-form modal
  if (handleTableRowClick(event)) return

  // 3. Handle commit-hash clicks (span or button)
  const commitEl = event.target.closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) {
      window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    }
    return
  }

  // 4. Handle worktree action buttons
  const wtBtn = event.target.closest('.chat-worktree-btn')
  if (wtBtn) {
    event.preventDefault()
    event.stopPropagation()
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    if (wtPath) {
      appStore.setProject(wtPath)
    }
    return
  }

  // 5. Handle file-open buttons
  const btn = event.target.closest('.chat-file-open-btn')
  if (!btn) return
  event.preventDefault()
  event.stopPropagation()
  const filePath = btn.getAttribute('data-file-path')
  const lineStart = btn.getAttribute('data-line-start')
  const lineEnd = btn.getAttribute('data-line-end')
  if (filePath) {
    openFilePath(filePath, lineStart ? parseInt(lineStart, 10) : undefined, lineEnd ? parseInt(lineEnd, 10) : undefined)
    emit('open-file', { path: filePath, lineStart: lineStart ? parseInt(lineStart, 10) : undefined, lineEnd: lineEnd ? parseInt(lineEnd, 10) : undefined })
  }
}

// ── Reset state when exec detail changes ──
watch(() => props.execDetail, (newVal, oldVal) => {
  expandedTools.value = {}
  closeOverlay()
  metadataModal.value.show = false
  activeTab.value = hasSummary.value ? 'summary' : 'original'
  isAtBottom.value = true

  // Start live preview when execution becomes running
  if (newVal?.status === 'running' && newVal?.sessionId) {
    execStream.startPreview()
    // Fetch latest content from API for running executions — the initial data
    // from the history list may lack content, and WS only delivers new events
    if (!newVal.content) {
      refreshExecDetail()
    }
  }
  // Stop preview when execution is no longer running
  if (oldVal?.status === 'running' && newVal?.status !== 'running') {
    execStream.stopPreview()
  }

  // Verify file path annotations after content re-renders.
  // ChatRender.renderMarkdown calls verifyFilePaths targeting #aiChatMessages,
  // but this component renders outside that container, so non-existent file
  // path buttons are never removed. Run verification against our own container.
  nextTick(() => {
    if (contentRef.value) {
      const paths = [...contentRef.value.querySelectorAll('.chat-file-open-btn[data-file-path]')]
        .map(btn => btn.getAttribute('data-file-path'))
        .filter(Boolean)
      if (paths.length > 0) verifyFilePaths([...new Set(paths)], contentRef.value)
    }
  })
}, { immediate: true })

onUnmounted(() => {
  execStream.stopPreview()
})
</script>

<style scoped>
.exec-detail-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: var(--bg-primary, #ffffff);
}

.exec-detail-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.exec-detail-content {
  flex: 1;
  overflow-y: auto;
  padding: 12px 0;
}

/* Fixed bottom action bar */
.exec-detail-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.actions-spacer {
  flex: 1;
}

.action-btn {
  height: 28px;
  border: none;
  border-radius: 14px;
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  padding: 0 10px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  transition: all 0.15s ease;
}

.action-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (hover: hover) {
  .action-btn:hover:not(:disabled) {
    background: var(--border-color, #e5e5e5);
    transform: translateY(-1px);
  }
}

.action-btn:active:not(:disabled) {
  transform: scale(0.96);
}

.action-btn.accent {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 20%, var(--bg-secondary, #f1f3f5));
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .action-btn.accent:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 35%, var(--bg-secondary, #f1f3f5));
    color: #fff;
  }
}

.action-btn.spinning svg {
  animation: exec-spin 1s linear infinite;
}

.action-text {
  white-space: nowrap;
}

@keyframes exec-spin {
  100% { transform: rotate(360deg); }
}

.exec-detail-empty {
  text-align: center;
  padding: 40px 12px;
  color: var(--text-muted, #999);
  font-size: 14px;
}

.exec-cancelled-notice {
  padding: 3rem 1rem;
  text-align: center;
  color: var(--text-muted, #999);
  font-style: italic;
  font-size: 14px;
}
</style>
