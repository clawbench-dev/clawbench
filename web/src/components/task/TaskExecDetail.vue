<template>
  <div class="exec-detail-page">
    <!-- Header: breadcrumb + actions -->
    <div class="exec-detail-header">
      <TaskBreadcrumb />
    </div>

    <!-- Scrollable message content -->
    <div class="exec-detail-content" ref="contentRef" @click="handleContentClick" @mousedown="onTableMouseDown" @touchstart="onTableTouchStart">
      <!-- Live preview indicator -->
      <div v-if="execStream.isStreaming.value" class="exec-live-bar">
        <span class="exec-live-dot"></span>
        <span class="exec-live-text">{{ t('task.exec.livePreview') }}</span>
        <span v-if="execStream.isPolling.value" class="exec-live-polling">{{ t('task.exec.previewPolling') }}</span>
      </div>
      <!-- Summary / Original tab bar (hidden during live streaming) -->
      <SummaryToggle v-if="hasSummary && !execStream.isStreaming.value" mode="tab" :showing-summary="activeTab === 'summary'" i18n-prefix="task.exec" @toggle="setTab(activeTab === 'summary' ? 'original' : 'summary')" />
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
      <button class="action-btn" :class="{ spinning: refreshing }" :disabled="refreshing" @click="onRefresh" :title="t('common.refresh')">
        <RefreshCw :size="14" />
      </button>
    </div>

    <!-- Tool Detail Overlay -->
    <ToolDetailDrawer
      :show="toolDetailOverlay.show"
      :toolName="toolDetailOverlay.name"
      :toolSubagentType="toolDetailOverlay.subagentType"
      :toolSummary="toolDetailOverlay.summary"
      :toolInputHtml="toolDetailOverlay.inputHtml"
      :toolOutputHtml="toolDetailOverlay.outputHtml"
      :toolStatus="toolDetailOverlay.status"
      :toolDone="toolDetailOverlay.done"
      :displayNameOverride="toolDetailOverlay.displayNameOverride"
      @close="toolDetailShow = false"
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
      :indexed="metadataModal.indexed"
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
import { RefreshCw, MessageSquare } from 'lucide-vue-next'
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

// ── Live preview stream ──
const execStatusRef = computed(() => props.execDetail?.status || '')
const execSessionIdRef = computed(() => props.execDetail?.sessionId || null)
const execStream = useTaskExecStream({
  sessionId: execSessionIdRef,
  status: execStatusRef,
  onRefresh: refreshExecDetail,
  onComplete: () => {
    // Execution completed while previewing — do a final refresh
    refreshExecDetail()
  },
})
const showContinueBtn = computed(() => {
  // Show button for completed or cancelled executions, not for running ones
  const status = props.execDetail?.status
  return status && status !== 'running' && props.taskId && props.execDetail?.id
})

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

// ── Agents (for getAgentIcon/getAgentName) ──
const { agents: agentsList, getAgentIcon, getAgentName } = useAgents()

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
provide('chatSession', { getAgentIcon, getAgentName })
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
  if (!blocks || blocks.length === 0) {
    // For running executions with empty content, return a streaming placeholder
    // so the live indicator bar is shown instead of "no text output"
    if (isRunning.value) {
      return {
        id: props.execDetail.messageId || props.execDetail.id || 'exec',
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
    id: props.execDetail.messageId || props.execDetail.id || 'exec',
    role: 'assistant',
    content: props.execDetail.content,
    blocks,
    metadata: props.execDetail.metadata || null,
    createdAt: props.execDetail.createdAt || '',
    streaming: false,
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
  // When live streaming via SSE, prefer the streaming message (has real-time blocks)
  if (execStream.isStreaming.value && !execStream.isPolling.value && execStream.streamingMsg.value) {
    const sm = execStream.streamingMsg.value
    // Show streaming message if it has blocks (real-time content)
    if (sm.blocks && sm.blocks.length > 0) return sm
  }
  // For polling mode, or when SSE hasn't produced blocks yet,
  // use the DB content (refreshed by onRefresh/polling) — this ensures
  // we always show whatever partial content is available rather than "connecting..."
  if (activeTab.value === 'summary' && summaryMsgData.value) return summaryMsgData.value
  return msgData.value
})

// ── Expanded tools state ──
const expandedTools = ref({})

function toggleTool(key) {
  expandedTools.value = { ...expandedTools.value, [key]: !expandedTools.value[key] }
}

// ── Tool Detail Overlay ──
const {
  show: toolDetailShow,
  toolDetailOverlay,
  handleShowToolDetail,
  handleOverlayRetryClick,
  handleFileOpenInOverlay,
} = useToolDetailDrawer({
  chatRender,
  onFileOpen: (path, lineStart, lineEnd) => {
    openFilePath(path, lineStart, lineEnd)
    emit('open-file', { path, lineStart, lineEnd })
  },
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
  indexed: false,
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
  metadataModal.value.indexed = false
  metadataModal.value.show = true
}

// ── Delegated click handler for .chat-file-open-btn ──
const contentRef = ref(null)

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
  toolDetailShow.value = false
  metadataModal.value.show = false
  activeTab.value = hasSummary.value ? 'summary' : 'original'

  // Start live preview when execution becomes running
  if (newVal?.status === 'running' && newVal?.sessionId) {
    execStream.startPreview()
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
  padding: 12px 8px;
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
  background: var(--accent-color, #0066cc);
  color: #fff;
}

@media (hover: hover) {
  .action-btn.accent:hover:not(:disabled) {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 85%, black);
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

/* Live preview indicator */
.exec-live-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  margin-bottom: 8px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, transparent);
  border-radius: 8px;
  font-size: 12px;
  color: var(--accent-color, #0066cc);
}

.exec-live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent-color, #0066cc);
  animation: exec-live-pulse 1.5s ease-in-out infinite;
}

.exec-live-text {
  font-weight: 500;
}

.exec-live-polling {
  color: var(--text-muted, #999);
  font-size: 11px;
}

@keyframes exec-live-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}
</style>
