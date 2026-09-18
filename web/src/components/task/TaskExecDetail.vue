<template>
  <div class="exec-detail-page">
    <!-- Header: breadcrumb + refresh button -->
    <div class="exec-detail-header">
      <TaskBreadcrumb />
      <RefreshButton class="header-btn refresh-btn" :loading="refreshing" :disabled="refreshing" :title="t('common.refresh')" @click="onRefresh" />
    </div>

    <!-- Summary / Original tabs — pinned directly under the header, OUTSIDE the
         scroll container, so they stay put while the message scrolls. Hidden
         while the run is still streaming (there is no final summary yet). -->
    <div v-if="hasSummary && !execStream.isStreaming.value && !isRunning" class="exec-detail-tabs">
      <SummaryToggle mode="tab" :showing-summary="activeTab === 'summary'" i18n-prefix="task.exec" @toggle="setTab(activeTab === 'summary' ? 'original' : 'summary')" />
    </div>

    <!-- Scrollable message content -->
    <div class="exec-detail-content" ref="contentRef" @click="handleContentClick" @mousedown="onTableMouseDown" @touchstart="onContentTouchStart" @touchend="onContentTouchEnd" @touchcancel="onContentTouchEnd" @scroll="handleScroll">
      <!-- Trigger source: links an event-triggered run back to its issue/PR and
           shows the exact context block that was prepended to the prompt, so
           the run can be read against what actually triggered it. -->
      <div v-if="execDetail?.eventUrl || execDetail?.eventSummary" class="exec-event-card">
        <a v-if="execDetail?.eventUrl" class="exec-event-source" :href="execDetail.eventUrl" target="_blank" rel="noopener noreferrer">
          <Zap :size="12" />
          <span>{{ eventSourceText || t('task.exec.eventTriggeredFrom') }}</span>
          <ExternalLink :size="11" />
        </a>
        <button
          v-if="execDetail?.eventSummary"
          class="exec-event-toggle"
          @click="eventContextOpen = !eventContextOpen"
        >
          <ChevronDown :size="12" :class="{ 'exec-event-chevron-collapsed': !eventContextOpen }" class="exec-event-chevron" />
          <span>{{ t('task.exec.eventContext') }}</span>
        </button>
        <pre v-if="execDetail?.eventSummary && eventContextOpen" class="exec-event-context">{{ execDetail.eventSummary }}</pre>
      </div>
      <ChatMessageItem
        v-if="activeMsgData"
        :msg="activeMsgData"
        :index="0"
        :expandedTools="expandedTools"
        :blockTasks="{}"
        :blockAskQuestions="{}"
        hideSessionActions
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
      <button v-if="showContinueBtn" class="fbtn fbtn-primary accent" :disabled="continueLoading || isRunning" @click="onContinueConversation" :title="t('task.exec.continueConversation')">
        <MessageSquare :size="14" />
        <span class="action-text">{{ continueLoading ? t('task.exec.continueConversationLoading') : t('task.exec.continueConversation') }}</span>
      </button>
      <span class="actions-spacer"></span>
      <button v-if="isRunning" class="fbtn fbtn-danger danger" :disabled="cancelling" @click="onTerminate" :title="t('task.exec.cancel')">
        <Square :size="14" />
        <span class="action-text">{{ cancelling ? t('common.loading') : t('task.exec.cancel') }}</span>
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

    <!-- Code link preview for annotated file paths in the execution content -->
    <CodeLinkPreview v-if="codeLinkPreview.enabled.value" :preview="codeLinkPreview" />
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick, provide, onUnmounted, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquare, Square, Zap, ExternalLink, ChevronDown } from 'lucide-vue-next'
import TaskBreadcrumb from '@/components/task/TaskBreadcrumb.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
import ChatMessageItem from '@/components/chat/ChatMessageItem.vue'
import ToolDetailDrawer from '@/components/chat/ToolDetailDrawer.vue'
import ChatMetadataModal from '@/components/chat/ChatMetadataModal.vue'
import SummaryToggle from '@/components/common/SummaryToggle.vue'
import { useChatRender } from '@/composables/useChatRender.ts'
import { useAgents } from '@/composables/useAgents'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useCodeLinkPreview, handleVerifiedFilePathClick } from '@/composables/useCodeLinkPreview.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { store as appStore } from '@/stores/app.ts'
import { useAutoSpeech } from '@/composables/useAutoSpeech.ts'
import { useTaskTab } from '@/composables/useTaskTab.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer.ts'
import '@/assets/modal-footer-btn.css'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { useTaskExecStream } from '@/composables/useTaskExecStream.ts'
import { terminateExecution } from '@/utils/taskExecUtils.ts'
import { formatToolOutput } from '@/utils/renderToolDetail.ts'
import { eventSourceLabel } from '@/utils/forgeEventLabels'
import TableRowModal from '@/components/common/TableRowModal.vue'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'

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
const { openFilePath, verifyFilePaths, readLineTargetFromEl } = useFilePathAnnotation()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()
const switchTab = inject('switchTab', () => {})
const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

// ── Continue conversation logic ──
const continueLoading = ref(false)
const isRunning = computed(() => props.execDetail?.status === 'running')

// ── Event trigger context ──
// The run's source is identified by its URL; the backend also stored the exact
// context block it prepended to the prompt, which the user can expand to see
// what the AI was told about the event.
const eventContextOpen = ref(false)
const eventSourceText = computed(() => eventSourceLabel(props.execDetail?.eventUrl))

// ── Terminate (cancel) running execution ──
const cancelling = ref(false)

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
  cancelling.value = true
  try {
    // Backend runningExecutions map is keyed by session ID, not the DB id.
    // Prefer sessionId for running executions; fall back to DB id.
    const executionId = props.execDetail?.sessionId || String(props.execDetail.id)
    // terminateExecution refreshes exactly once on the success path — do NOT
    // also refresh off its return value (that would double-refresh).
    await terminateExecution({
      taskId: props.taskId,
      executionId,
      onStopPreview: () => execStream.stopPreview(),
      onRefresh: () => refreshExecDetail(),
    })
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
// Minimum spin duration so the refresh animation is always visible,
// even when the API responds almost instantly.
const REFRESH_MIN_MS = 600

async function onRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await Promise.all([
      refreshExecDetail(),
      new Promise(resolve => setTimeout(resolve, REFRESH_MIN_MS)),
    ])
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
  truncate: chatRender.truncate,
  hasImagesInContent: chatRender.hasImagesInContent,
})
provide('chatSession', { getAgentBackend, getAgentName })
provide('chatUI', { navigateToFileViewer: () => emit('close') })
provide('autoSpeech', useAutoSpeech())

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
  onFileOpen: (path, lineStart, lineEnd, lineRanges) => {
    if (lineRanges) openFilePath(path, lineStart, lineEnd, 'task', lineRanges)
    else openFilePath(path, lineStart, lineEnd, 'task')
    emit('open-file', { path, lineStart, lineEnd, lineRanges })
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

// Code link preview for annotated file paths in the execution content.
const codeLinkPreview = useCodeLinkPreview({ containerRef: contentRef, source: 'task' })

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

  // 2.5. Annotated *verified* file paths open the code link preview (same
  // behaviour as chat messages). Only data-path-type="file" elements are
  // intercepted — directories / unverified paths fall through below.
  if (handleVerifiedFilePathClick(event, codeLinkPreview)) return

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
    codeLinkPreview.close()
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    if (wtPath) {
      appStore.setProject(wtPath)
    }
    return
  }

  // 5. Handle file-open buttons or directory path text
  const btn = event.target.closest('.chat-file-open-btn[data-file-path]')
  const dirEl = event.target.closest('.chat-file-path[data-file-path][data-path-type="dir"]')
  const linkOrBtn = btn || dirEl
  if (!linkOrBtn) return
  event.preventDefault()
  event.stopPropagation()
  codeLinkPreview.close()
  const { filePath, lineStart, lineEnd, lineRanges } = readLineTargetFromEl(linkOrBtn)
  if (filePath) {
    if (lineRanges) openFilePath(filePath, lineStart, lineEnd, 'task', lineRanges)
    else openFilePath(filePath, lineStart, lineEnd, 'task')
    emit('open-file', { path: filePath, lineStart, lineEnd, lineRanges })
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
    // The execution content may have been re-rendered while a code link
    // preview was open; if its anchor element was detached, close it so the
    // floating card does not linger over stale content. Only auto-close the
    // unpinned (transient) card — pinned / sheet previews are dismissed by
    // the user.
    const anchor = codeLinkPreview.target?.value?.anchorEl
    if (anchor && !anchor.isConnected && codeLinkPreview.mode?.value === 'transient') codeLinkPreview.close()
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
  gap: var(--space-3);
  height: var(--header-height);
  padding:0 var(--space-2) 0 var(--space-6);
  background: var(--bg-primary);
  border-bottom: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

/* Refresh button in the breadcrumb bar (unified with TaskDetailPage) */
.header-btn {
  width: 28px;
  height: 28px;
  border: none;
  border-radius: var(--radius-lg);
  background: var(--bg-secondary, #f1f3f5);
  color: var(--text-secondary, #666);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  transition: all var(--duration-slow) ease;
}

.header-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

@media (hover: hover) {
  .header-btn:hover:not(:disabled) {
    background: var(--bg-tertiary, #eef1f4);
    color: var(--accent-color, #0066cc);
  }
}

.header-btn:active:not(:disabled) {
  transform: scale(0.9);
}

/* Pinned tab strip. Sits between the header and the scroller as a flex sibling
   (not sticky inside the scroller) — the scroller's content includes the event
   band, which is full-bleed and rendered above the message; a sticky bar inside
   would start life below that band and never reach the header. As a sibling it
   is flush against the header's bottom border with no gap, and the tab bar's
   own margin-bottom is zeroed so the message starts right below it. */
.exec-detail-tabs {
  flex-shrink: 0;
}

.exec-detail-tabs :deep(.summary-toggle-bar) {
  margin-bottom: 0;
}

.exec-detail-content {
  flex: 1;
  overflow-y: auto;
  padding: var(--space-6) 0;
}

/* The event band belongs flush under whatever precedes it — the header, or the
   pinned tab strip — because it is a header strip, not a floating card. The
   scroll container's top padding would otherwise leave a 12px strip of page
   background above it. Drop that padding only when the band is present; the
   message-only case keeps its breathing room below the tabs / header. */
.exec-detail-content:has(> .exec-event-card) {
  padding-top: 0;
}

/* Trigger source for event-triggered runs: a source link plus the collapsible
   context block the backend prepended to the prompt.

   Rendered as a full-bleed band — no outer margin, no radius — so it reads as
   the run's header strip rather than a floating card. Two details make it hold
   together:
   - Horizontal padding matches the chat content below (--space-6), so the
     source pill, the toggle and the message text share one left edge. At
     padding:0 all three sat flush against the viewport edge.
   - A bottom border delimits the band. Without it the band's own background
     ran straight into the summary tab bar's identical --bg-secondary, and the
     two merged into one featureless grey block. */
.exec-event-card {
  margin: 0;
  padding: var(--space-5) var(--space-6);
  background: var(--bg-secondary, #f3f4f6);
  border-bottom: 1px solid var(--border-color, #e5e7eb);
  border-radius: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.exec-event-source {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  align-self: flex-start;
  padding: var(--space-1) var(--space-4);
  border-radius: var(--radius-full);
  background: var(--bg-primary, #fff);
  border: 1px solid var(--border-color, #e5e7eb);
  color: var(--text-secondary, #4b5563);
  font-size: var(--font-size-sm);
  text-decoration: none;
  max-width: 100%;
}

/* The owner/repo slug can outrun the viewport, so the text truncates rather
   than widening the pill. ellipsis needs all three of these on the *text*
   item — the container's own text-overflow is inert here because the label is
   a flex item, not inline text. The icons are pinned so they survive the
   squeeze instead of collapsing to nothing. */
.exec-event-source span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.exec-event-source > svg {
  flex-shrink: 0;
}

.exec-event-source:hover {
  color: var(--accent-color, #2563eb);
  border-color: var(--accent-color, #2563eb);
}

.exec-event-toggle {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  align-self: flex-start;
  border: none;
  background: transparent;
  padding: var(--space-1) 0;
  color: var(--text-muted, #6b7280);
  font-size: var(--font-size-xs);
  cursor: pointer;
}

@media (hover: hover) {
  .exec-event-toggle:hover { color: var(--text-secondary, #4b5563); }
}

.exec-event-chevron {
  transition: transform var(--duration-slow) ease;
}

.exec-event-chevron-collapsed {
  transform: rotate(-90deg);
}

.exec-event-context {
  margin: 0;
  padding: var(--space-4) var(--space-5);
  border: 1px dashed var(--border-color, #d1d5db);
  border-radius: 0;
  background: var(--bg-primary, #fff);
  font-size: var(--font-size-xs);
  line-height: var(--line-height-relaxed);
  color: var(--text-secondary, #4b5563);
  white-space: pre-wrap;
  word-break: break-word;
  overflow-x: auto;
  font-family: var(--font-mono);
}

/* Fixed bottom action bar */
.exec-detail-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  background: var(--bg-primary, #ffffff);
  border-top: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

.actions-spacer {
  flex: 1;
}

.action-text {
  white-space: nowrap;
}

.exec-detail-empty {
  text-align: center;
  padding:40px var(--space-6);
  color: var(--text-muted, #999);
  font-size: var(--font-size-lg);
}

.exec-cancelled-notice {
  padding: 3rem 1rem;
  text-align: center;
  color: var(--text-muted, #999);
  font-style: italic;
  font-size: var(--font-size-lg);
}
</style>
