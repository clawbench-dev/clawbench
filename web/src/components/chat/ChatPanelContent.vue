<template>
  <div class="chat-panel-content">
    <!-- Messages -->
    <ChatMessageList
      ref="messageListRef"
      :messages="renderedMessages"
      :expandedTools="render.expandedTools.value"
      :blockTasks="render.blockTasks"
      :blockAskQuestions="render.blockAskQuestions"
      :blockRagResults="render.blockRagResults"
      :agents="agentsList"
      :currentAgent="currentAgent"
      :currentSessionId="identity.currentSessionId.value"
      :hasMore="session.hasMore.value"
      :loadingMore="session.loadingMore.value"
      :totalMessages="session.totalMessages.value"
      :active="props.active"
      @touchstart="swipeSession.onTouchStart"
      @touchend="swipeSession.onTouchEnd"
      @toggle-tool="render.toggleToolDetail"
      @show-tool-detail="handleShowToolDetail"
      @show-thinking-detail="handleShowThinkingDetail"
      @show-metadata="showMetadata"
      @file-tag-click="handleFileTagClick"
      @load-more="handleLoadMore"
      @task-card-click="(taskId) => $emit('task-card-click', taskId)"
      @send-message="handleToolSendMessage"
      @remove-pending="handleRemovePending"
      @render-flush="scrollBottom()"
      @toggle-summary="handleToggleSummary"
      @resume-session="handleResumeSession"
      @show-rag-detail="handleRagDetail"
      @fork-from-message="handleForkFromMessage"
    />

    <!-- Session switching overlay — placed here to cover the entire message area -->
    <Transition name="loading-fade">
      <div v-if="session.switching.value" class="loading-mask">
        <div class="loading-mask-spinner"></div>
      </div>
    </Transition>

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

    <!-- Unified input container -->
    <ChatInputBar
      ref="inputBarRef"
      :inputDisabled="inputDisabled"
      :loading="loading"
      :currentFile="currentFile"
      :currentDir="currentDir"
      :pendingFiles="pendingFiles"
      :attachedFiles="attachedFiles"
      :quoteData="quoteData"
      :messages="renderedMessages"
      :autoSpeechEnabled="autoSpeech.enabled.value"
      :currentSessionId="identity.currentSessionId.value"
      :chatUnreadCount="store.state.chatUnreadCount"
      :chatRunning="store.state.chatRunning"
      :currentModelId="identity.currentModelId.value"
      :currentModelName="identity.currentModelName.value"
      :currentThinkingEffort="identity.currentThinkingEffort.value"
      :currentModeName="identity.currentModeName.value"
      :currentTransport="identity.currentTransport.value"
      :currentAgentId="identity.currentAgentId.value"
      :active="props.active"
      @send="sendMessage"
      @cancel="stream.cancelStream"
      @file-select="handleFileSelect"
      @file-drop="handleFileDrop"
      @remove-file="removeFile"
      @add-attached="addAttachedFile"
      @remove-attached="removeAttachedFile"
      @remove-quote="setQuoteData(null)"
      @quote-click="handleQuoteClick"
      @open-session-tab="identity.openSessionTab"
      @file-tag-click="handleFileTagClick"
      @toggle-auto-speech="autoSpeech.toggle"
      @create-session="() => manager.createSession()"
      @show-agent-selector="handleShowAgentSelector"
      @delete-session="() => manager.deleteCurrentSession((draftId) => inputBarRef.value?.deleteDraft(draftId))"
      @open-user-msg-index="handleOpenUserMsgIndex"
      @switch-model="handleSwitchModel"
      @switch-thinking-effort="handleSwitchThinkingEffort"
      @switch-mode="handleSwitchMode"
      @switch-transport="handleSwitchTransport"
    />

  </div>

  <!-- Metadata Modal -->
  <ChatMetadataModal
    :show="metadataDrawer.effectiveOpen.value"
    :data="metadataModal.data"
    :backend="metadataModal.backend"
    :createdAt="metadataModal.createdAt"
    :relatedFile="metadataModal.relatedFile"
    :messageId="metadataModal.messageId"
    :sessionId="metadataModal.sessionId"
    :indexed="metadataModal.indexed"
    :formatDetailTime="render.formatDetailTime"
    @close="metadataShow = false"
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
    :displayNameOverride="toolDetailOverlay.displayNameOverride"
    @close="toolDetailShow = false"
    @file-open="handleFileOpenInOverlay"
    @send-message="handleToolSendMessage"
    @click="handleOverlayRetryClick"
  />
  <!-- RAG search result detail drawer -->
  <RagDetailDrawer :item="ragDetailDrawer.effectiveOpen.value ? ragDetailItem : null" @close="ragDetailShow = false; ragDetailItem.value = null" @resume="handleResumeFromDetail" />
</template>

<script setup>
import { ref, computed, watch, onUnmounted, onMounted, inject, provide, toRef, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { appLog } from '@/utils/appLog'
import { gt } from '@/composables/useLocale'
import { useTabDrawer } from '@/composables/useTabDrawer'
import HeaderMarquee from '@/components/common/HeaderMarquee.vue'
import RagDetailDrawer from './RagDetailDrawer.vue'
import ChatMetadataModal from './ChatMetadataModal.vue'
import ToolDetailDrawer from './ToolDetailDrawer.vue'
import ChatInputBar from './ChatInputBar.vue'
import ChatMessageList from './ChatMessageList.vue'
import PlanPanel from './PlanPanel.vue'
import { usePlanProgress } from '@/composables/usePlanProgress'
import { useChatRender } from '@/composables/useChatRender.ts'
import { formatToolOutput } from '@/utils/renderToolDetail.ts'
import { useChatStream } from '@/composables/useChatStream.ts'
import { useChatSession, loadSessionsOnce } from '@/composables/useChatSession.ts'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useSessionManager } from '@/composables/useSessionManager.ts'
import { usePendingStore, createPendingMessage } from '@/composables/usePendingStore.ts'
import { useAgents, populateACPStateFromCache } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useNotification } from '@/composables/useNotification.ts'
import { applySummaryUpdate } from '@/utils/chatSessionUtils.ts'
import { useFileUpload } from '@/composables/useFileUpload.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { refreshCurrentFile } from '@/composables/useFileRefresh.ts'
import { playNotificationSound } from '@/composables/useNotificationSound.ts'
import { useAutoSpeech, extractSpeakableText } from '@/composables/useAutoSpeech.ts'
import { useSwipeSession } from '@/composables/useSwipeSession.ts'
import { useGlobalEvents } from '@/composables/useGlobalEvents'
import { store } from '@/stores/app.ts'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import { useDialog } from '@/composables/useDialog'

import '@/assets/loading-mask.css'
import { useToolDetailDrawer } from '@/composables/useToolDetailDrawer.ts'

const { t } = useI18n()
const TAG = 'ChatPanel'

const props = defineProps({
    active: Boolean,
    currentFile: Object,
    currentDir: String,
})
const emit = defineEmits(['open', 'message', 'open-file', 'task-card-click'])

// ── Singletons ──
const identity = useSessionIdentity()
const agentsComposable = useAgents()
const { agents: agentsList, getAgent, getAgentIcon, getAgentName, getAgentModels, isMultiModel, getDefaultModelId } = agentsComposable
// Expose as `agents` for template access to getAgentModels/isMultiModel
const agents = agentsComposable

const messages = ref([])
const pendingStore = usePendingStore()
/** Rendered messages = persisted messages + pending messages for current session */
const renderedMessages = computed(() => [
  ...messages.value,
  ...pendingStore.getPending(identity.currentSessionId.value),
])
const inputDisabled = ref(false)
const loading = ref(false)
// Incremented when the panel reopens, so ChatMessageItem can re-check
// overflow after being hidden (display:none gives scrollHeight=0).
const layoutRefreshKey = ref(0)
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
  indexed: false
})
const metadataShow = ref(false)
const metadataDrawer = useTabDrawer('chat', metadataShow)
const toast = useToast()
const dialog = useDialog()
const notification = useNotification()
const autoSpeech = useAutoSpeech()
const theme = inject('theme', ref('light'))
const switchTab = inject('switchTab', () => {})
const { openFilePath } = useFilePathAnnotation()

async function handleFileTagClick(filePath) {
    if (filePath) {
        const ok = await openFilePath(filePath)
        if (ok) switchTab('browse')
    }
}

function handleQuoteClick() {
    const q = quoteData.value
    if (q?.filePath) {
        store.selectFile(q.filePath).then(() => {
            switchTab('browse')
        })
    }
}

const { planEntries, planCollapsed, planHasUpdate, hasPlan, togglePlanCollapse, clearPlanState } = usePlanProgress()

const render = useChatRender({ messages, theme, currentSessionId: identity.currentSessionId })

/** Look up the thinking block from the live messages array by msgId + blockKey */
function findThinkingBlock({ msgId, blockKey }) {
  if (!blockKey) return null
  const msg = messages.value.find(m => String(m.id) === msgId)
  if (!msg || !msg.blocks) return null
  const block = msg.blocks.find(b => b._key === blockKey)
  return (block && block.type === 'thinking') ? block : null
}

/** Look up the tool_use block from the live messages array by msgId + blockIdx */
function findToolBlock({ msgId, blockIdx }) {
  const msg = messages.value.find(m => String(m.id) === msgId)
  if (!msg || !msg.blocks) return null
  const block = msg.blocks[blockIdx]
  return (block && block.type === 'tool_use') ? block : null
}

const {
  show: toolDetailShow,
  toolDetailData,
  toolDetailOverlay,
  activeToolOverlay,
  handleShowToolDetail,
  handleOverlayRetryClick,
  fetchToolCallDetail,
  handleFileOpenInOverlay,
  closeOverlay,
} = useToolDetailDrawer({
  chatRender: render,
  onFileOpen: async (path, lineStart, lineEnd) => {
    const ok = await openFilePath(path, lineStart, lineEnd)
    if (ok) switchTab('browse')
  },
  findLiveBlock: (ids) => findToolBlock(ids),
})
const toolDetailDrawer = useTabDrawer('chat', toolDetailShow)

// Active thinking overlay: tracks which block is being shown so we can reactively update
const activeThinkingOverlay = ref(null) // { msgId, blockKey } or null
let thinkingRenderTimer = null
let streamingRefreshTimer = null

const session = useChatSession({
  currentSessionId: identity.currentSessionId,
  messages,
  loading,
  inputDisabled,
  blockTasks: render.blockTasks,
  blockAskQuestions: render.blockAskQuestions,
  blockRagResults: render.blockRagResults,
  expandedTools: render.expandedTools,
  onParseAssistantContent: (content) => render.parseAssistantContent(content),
  onExtractScheduledTasks: (msgs) => render.extractScheduledTasks(msgs),
  onRenderUpdate: (forceFull) => render.updateRenderedContents(forceFull),
  onScrollBottom: (force) => scrollBottom(force),
  onConnectStream: (sessionId) => stream.connectStream(sessionId),
  onStopPolling: () => stream.stopPolling(),
  onDisconnectStream: () => stream.disconnectStream(),
  onOpen: () => emit('open'),
  onStreamDone: playNotificationSound,
})

// onStreamEnd: fires when current session stream completes with a reason
// - 'done': normal completion → play sound, auto-speech; queue sync handled by
//   useSessionManager's watch(loading) safety net (loading true→false triggers fetchQueue)
// - 'cancelled': user cancelled → clear locally for immediate UI response
// - 'error': error occurred → don't touch pending messages; backend preserves queue
function onStreamEnd(reason) {
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
    // Recalculate chatUnread after stream completes — the current session's
    // unreadCount is now 0 (UpdateLastRead called by loadHistory), so
    // chatUnread should be false if no other sessions have unread messages.
    loadSessionsOnce()
    // Refresh git branch — AI agent may have checked out a different branch
    store.loadGitBranch().catch(() => {})
  } else if (reason === 'cancelled') {
    // Backend already cleared queue; clear locally for immediate UI response
    pendingStore.clearPending(identity.currentSessionId.value)
    // Restore screen lock — output was cancelled, no TTS will play
    autoSpeech.onOutputEndNoSpeech()
  }
  // 'error': don't touch pending messages — backend preserves queue
  if (reason === 'error') {
    // Restore screen lock — output errored, no TTS will play
    autoSpeech.onOutputEndNoSpeech()
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
  currentSessionId: identity.currentSessionId,
  currentBackend: identity.currentBackend,
  loading,
  pendingStore,
  onRenderNeeded: (forceFull) => render.updateRenderedContents(forceFull),
  onScrollBottom: (force) => scrollBottom(force),
  onLoadHistory: () => session.loadHistory(),
  onMessage: () => emit('message'),
  onOpen: () => emit('open'),
  isOpen: toRef(props, 'active'),
  onParseAssistantContent: (content) => render.parseAssistantContent(content),
  onToast: (msg, opts) => toast.show(msg, opts),
  onNotification: (title, opts) => notification.show(title, opts),
  onStreamEnd,
  onFileModified: (filePath) => {
    // Chat-driven file refresh: when AI's Write/Edit tool completes,
    // refresh the file preview if the modified file is currently being viewed.
    // This is a defense-in-depth mechanism alongside the fsnotify-based file watcher.
    const currentFilePath = store.state.currentFile?.path

    // Path matching: tool paths may be relative, absolute, or have different prefixes.
    // Use suffix matching: if the current file path ends with the tool's file path,
    // or vice versa, they match.
    const normA = filePath.replace(/\\/g, '/')
    const normB = (currentFilePath || '').replace(/\\/g, '/')
    const isMatch = normA === normB ||
      normA.endsWith('/' + normB) ||
      normB.endsWith('/' + normA)

    if (isMatch && currentFilePath) {
      // refreshCurrentFile handles both file content and directory listing
      refreshCurrentFile({ loadDir: true })
    } else {
      // File not currently viewed, but still refresh directory listing
      const currentDir = store.state.currentDir
      if (currentDir !== undefined) {
        store.loadFiles(currentDir)
      }
    }
  },
})

const { pendingFiles, attachedFiles, handleFileSelect, handleFileDrop, removeFile, addAttachedFile, removeAttachedFile, cleanupPreviewUrls, clearPendingFiles } = useFileUpload()
const { quoteData, setQuoteData, clearAll } = useChatContext()

const manager = useSessionManager({
  messages,
  loading,
  pendingStore,
  switchSessionCore: session.switchSession,
  createSessionCore: session.createSession,
  deleteSessionCore: session.deleteSession,
  continueFromExecutionCore: session.continueFromExecution,
  forkSessionCore: session.forkSession,
  checkContinueSessionCore: session.checkContinueSession,
  disconnectStream: stream.disconnectStream,
  stopPolling: stream.stopPolling,
  updateRenderedContents: (forceFull) => render.updateRenderedContents(forceFull),
  clearInputState: () => {
    clearAll()
    inputBarRef.value?.clearInput()
    clearPendingFiles()
  },
  scrollBottom: (force) => scrollBottom(force),
  sendMessageNow: (text, filePaths, files) => sendMessageNow(text, filePaths, files),
})

// Register identity actions — all paths now go through manager
manager.registerIdentityActions({
  sendMessage: (text, filePaths) => sendMessage(text, filePaths),
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
  humanizeCron: render.humanizeCron,
  repeatLabel: render.repeatLabel,
  truncate: render.truncate,
  hasImagesInContent: render.hasImagesInContent,
})
provide('chatSession', { getAgentIcon, getAgentName })
provide('chatUI', { navigateToFileViewer: () => switchTab('browse') })
provide('autoSpeech', autoSpeech)
provide('layoutRefreshKey', layoutRefreshKey)

// 面板打开时刷新渲染（修复 display:none 期间的过时布局状态）
// immediate: true 确保首次挂载时（active 已为 true）也会加载历史记录
// 子抽屉不再在此关闭 — useTabDrawer 的 effectiveOpen computed 已在 tab 不活跃时自动隐藏抽屉，
// 切回 tab 时 openRef 保留原值，抽屉自动恢复。
watch(() => props.active, async (val) => {
  if (val) {
    // Open/Re-open: load history (with overlay, skip if unchanged) and fix stale layout state from v-show display:none
    // skipIfUnchanged=true preserves scroll position when no new messages arrived while tab was hidden
    await session.loadHistory(false, true, true)
    // Bump layoutRefreshKey AFTER loadHistory so ChatMessageItem re-checks
    // collapse state with the fresh messages and valid scrollHeight.
    nextTick(() => {
      layoutRefreshKey.value++
    })
  }
}, { immediate: true })

// Reactively update thinking overlay content as block.text changes during streaming
watch(
  () => {
    if (!activeThinkingOverlay.value || !toolDetailOverlay.value.show) return null
    const block = findThinkingBlock(activeThinkingOverlay.value)
    return block ? block.text : null
  },
  (text) => {
    if (text === null) return
    // Debounce: avoid re-rendering markdown on every SSE event
    if (thinkingRenderTimer) clearTimeout(thinkingRenderTimer)
    thinkingRenderTimer = setTimeout(() => {
      const b = findThinkingBlock(activeThinkingOverlay.value)
      toolDetailData.value.inputHtml = `<div class="thinking-overlay-md">${renderMarkdown(text)}</div>`
      toolDetailData.value.done = b ? !!b.done : !loading.value
    }, 300)
  }
)

// Reactively update tool overlay content as block.output/done/status changes during streaming
watch(
  () => {
    if (!activeToolOverlay.value) return null
    const block = findToolBlock(activeToolOverlay.value)
    if (!block) return null
    return { output: block.output, done: block.done, status: block.status, input: block.input, name: block.name, summary: block.summary, display_name: block.display_name }
  },
  (data) => {
    if (data === null || !toolDetailShow.value) return
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
watch(() => toolDetailShow.value, (show) => {
  if (!show) {
    activeThinkingOverlay.value = null
    activeToolOverlay.value = null
    if (thinkingRenderTimer) { clearTimeout(thinkingRenderTimer); thinkingRenderTimer = null }
    if (streamingRefreshTimer) { clearInterval(streamingRefreshTimer); streamingRefreshTimer = null }
  }
})

// Streaming refresh: when overlay is open and streaming is not done, poll every 1s
// to refresh content. This supplements the passive watch — SSE doesn't push tool output,
// and thinking text updates may be missed during SSE reconnection/buffering.
function startStreamingRefresh() {
  if (streamingRefreshTimer) clearInterval(streamingRefreshTimer)
  streamingRefreshTimer = setInterval(() => {
    if (!toolDetailShow.value) {
      clearInterval(streamingRefreshTimer)
      streamingRefreshTimer = null
      return
    }
    // Thinking overlay: re-render from live block text
    if (activeThinkingOverlay.value) {
      const block = findThinkingBlock(activeThinkingOverlay.value)
      if (block) {
        toolDetailData.value.inputHtml = `<div class="thinking-overlay-md">${renderMarkdown(block.text)}</div>`
        toolDetailData.value.done = !!block.done
        // Block is done — no further updates needed
        if (block.done) {
          clearInterval(streamingRefreshTimer)
          streamingRefreshTimer = null
          return
        }
      }
      if (!loading.value) {
        clearInterval(streamingRefreshTimer)
        streamingRefreshTimer = null
      }
      return
    }
    // Tool overlay: fetch output from API if not done
    if (activeToolOverlay.value) {
      const block = findToolBlock(activeToolOverlay.value)
      if (block && block.done) {
        clearInterval(streamingRefreshTimer)
        streamingRefreshTimer = null
        return
      }
      if (block && block.tool_id && block.msgId) {
        fetchToolCallDetail(block.tool_id, block.msgId, block)
      }
      if (!loading.value && block && block.done) {
        clearInterval(streamingRefreshTimer)
        streamingRefreshTimer = null
      }
    }
  }, 1000)
}

watch(
  () => ({ show: toolDetailOverlay.value.show, done: toolDetailOverlay.value.done }),
  ({ show, done }) => {
    if (show && !done) {
      startStreamingRefresh()
    } else if (streamingRefreshTimer) {
      clearInterval(streamingRefreshTimer)
      streamingRefreshTimer = null
    }
  }
)

async function handleShowAgentSelector() {
  await agentsComposable.loadAgents()
  // If only one agent exists, skip the selector and create directly
  if (agentsList.value.length === 1) {
    manager.createSession(agentsList.value[0].id)
    return
  }
  identity.openAgentSelector()
}

function handleSwitchModel(model) {
  identity.currentModelId.value = model.id
  identity.currentModelName.value = model.name
  // Persist model selection immediately so it survives page reload
  persistSessionUpdate({ modelId: model.id })
}

function handleSwitchThinkingEffort(level) {
  identity.currentThinkingEffort.value = level
  // Resolve and set the friendly name for display
  const levelObj = identity.availableThinkingEfforts.value.find(l => l.id === level)
  identity.currentThinkingEffortName.value = levelObj?.name || level
  // Persist thinking effort selection immediately so it survives page reload
  persistSessionUpdate({ thinkingEffort: level })
}

function handleSwitchMode(mode) {
  if (!mode?.id || mode.id === identity.currentModeId.value) return
  identity.currentModeId.value = mode.id
  identity.currentModeName.value = mode.name || mode.id
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
    await manager.forkSession(sid, msg.id)
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

async function sendMessage(text, extraFilePaths) {
    const inputText = text !== undefined ? text : (inputBarRef.value?.inputText?.trim() || '')
    const hasFiles = pendingFiles.value.length > 0 || attachedFiles.value.length > 0 || quoteData.value

    if ((!inputText && !hasFiles) || inputDisabled.value) return

    // If AI is generating, enqueue the message instead of sending immediately
    if (loading.value) {
      // Capture file arrays before clearing (they're passed by reference)
      const capturedAttached = [...attachedFiles.value]
      const capturedPending = pendingFiles.value.map(f => f.path)
      // Merge all file paths for the pending message (deduplicated)
      const mergedPaths = [...new Set([...(extraFilePaths || []), ...(capturedAttached.length > 0 ? capturedAttached : [])])]
      const allFiles = [...capturedPending, ...mergedPaths]
      // Clear input state synchronously so user sees immediate feedback
      clearAll()
      inputBarRef.value?.clearInput()
      clearPendingFiles()
      // Push a pending user message into pendingStore (per-session)
      pendingStore.addPending(identity.currentSessionId.value, createPendingMessage(inputText || '', allFiles))
      render.updateRenderedContents()
      scrollBottom(true)
      // Enqueue to backend (POST /api/ai/queue)
      const result = await manager.enqueueMessage(inputText, extraFilePaths, capturedAttached, capturedPending)
      // Race condition: if AI finished right as we enqueued, the backend
      // dequeued the message and wants us to resubmit as a new chat.
      if (result.needsStart) {
        // The pending flag was already removed by enqueueMessage.
        // The user message is in messages.value without pending flag —
        // remove it since sendMessageNow will push its own copy.
        const idx = messages.value.findLastIndex(
          m => m.role === 'user' && m.content === (result.message || inputText) && !m.pending && typeof m.id === 'string' && m.id.startsWith('local-')
        )
        if (idx !== -1) messages.value.splice(idx, 1)
        await sendMessageNow(result.message || inputText, result.filePaths || mergedPaths, result.files || allFiles)
      }
      return
    }

    // Merge attached files from the input bar with extra file paths (e.g. from quote-question)
    // Deduplicate paths that may appear in both extraFilePaths and attachedFiles
    const filePaths = [...new Set([...(extraFilePaths || []), ...(attachedFiles.value.length > 0 ? attachedFiles.value : [])])]
    const uploadedFiles = pendingFiles.value.map(f => ({ path: f.path }))
    const projectFiles = filePaths.map(p => ({ path: p }))
    const allFiles = [...uploadedFiles, ...projectFiles].map(f => f.path)

    // Clear input state before async request
    clearAll()
    inputBarRef.value?.clearInput()
    clearPendingFiles()

    await sendMessageNow(inputText, filePaths, allFiles)
}

/** Actually send a message to the backend (no queue check). */
async function sendMessageNow(text, filePaths, files) {
    messages.value.push({
        role: 'user',
        id: `local-${Date.now()}`,
        content: text || '',
        blocks: text ? [{ type: 'text', text: text || '' }] : [],
        filePath: filePaths.length > 0 ? filePaths[0] : '',
        files: (files || []).map(p => ({ path: p })),
        createdAt: new Date().toISOString()
    })

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
            body: JSON.stringify({ message: text, filePaths, files: files || [], agentId: effectiveAgentId, modelId: identity.currentModelId.value || undefined, thinkingEffort: identity.currentThinkingEffort.value || undefined, modeId: identity.currentModeId.value || undefined, transport: identity.currentTransport.value || undefined }),
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
        // Session already running — another request is in progress
        if (data.running) {
            // Session already running — the message was enqueued.
            // Move the optimistically pushed user message from messages.value
            // to pendingStore, since it's now a queued/pending message.
            const localIdx = messages.value.findLastIndex(
                (m) => m.role === 'user' && m.content === (text || '') && typeof m.id === 'string' && m.id.startsWith('local-')
            )
            if (localIdx !== -1) {
                messages.value.splice(localIdx, 1)
            }
            pendingStore.addPending(identity.currentSessionId.value, createPendingMessage(text || '', files || []))
            if (data.queued && data.queue) {
                pendingStore.syncFromBackendQueue(identity.currentSessionId.value, data.queue)
            }
            stream.connectStream(identity.currentSessionId.value)
            // Proactively sync ACP state for the running session
            if (effectiveAgentId && agentsComposable.supportsDualTransport(effectiveAgentId)) {
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
        if (effectiveAgentId && agentsComposable.supportsDualTransport(effectiveAgentId)) {
            populateACPStateFromCache(effectiveAgentId)
        }
    } catch (err) {
        // Remove the optimistically pushed local user message on failure
        const localIdx = messages.value.findLastIndex(
            (m) => m.role === 'user' && m.content === (text || '') && typeof m.id === 'string' && m.id.startsWith('local-')
        )
        if (localIdx !== -1) {
            messages.value.splice(localIdx, 1)
        }
        stream.stopPolling()
        stream.disconnectStream()
        loading.value = false
        // Restore screen lock on send failure — output won't proceed
        autoSpeech.onOutputEndNoSpeech()
        toast.show(t('toast.sendFailed'), { icon: '⚠️', type: 'error' })
        // Clear session ID on error to prevent using invalid session
        if (err.msgKey === 'SessionBackendNotFound' || err.msgKey === 'SessionNotFound') {
            identity.currentSessionId.value = ''
        }
    }
}

/** Handle a tool-triggered message send (e.g. AskUserQuestion answer).
 *  If the AI stream is still running, enqueues the message for delivery after stream ends. */
async function handleToolSendMessage(text) {
    if (!text) return
    if (loading.value) {
      // Push a pending user message into pendingStore (per-session)
      pendingStore.addPending(identity.currentSessionId.value, createPendingMessage(text))
      render.updateRenderedContents()
      scrollBottom(true)
      manager.enqueueMessage(text)
    } else {
      await sendMessage(text)
    }
}

function scrollBottom(force = false) {
    messageListRef.value?.scrollToBottom(force)
}

async function handleLoadMore() {
    const el = messageListRef.value?.messagesRef
    if (!el) return
    const oldScrollHeight = el.scrollHeight
    await session.loadMoreMessages()
    // Wait for DOM update + one frame for async rendering (Mermaid, KaTeX)
    await nextTick()
    await new Promise(resolve => requestAnimationFrame(resolve))
    const newScrollHeight = el.scrollHeight
    el.scrollTop = newScrollHeight - oldScrollHeight
}

/** Handle remove-pending event from ChatMessageItem.
 *  The event passes the pending message's content text (not index).
 *  We look up the pendingIndex by content in the pendingStore for the
 *  backend API, and also remove from pendingStore optimistically. */
function handleRemovePending(content) {
    const sessionId = identity.currentSessionId.value
    const pending = pendingStore.getPending(sessionId)
    const pendingIndex = pending.findIndex(m => m.content === content)
    if (pendingIndex < 0) return
    manager.handleRemovePending(pendingIndex)
}

function showMetadata(msg) {
    metadataModal.value.data = msg.metadata || {}
    metadataModal.value.backend = msg.backend || ''
    metadataModal.value.createdAt = msg.createdAt || ''
    metadataModal.value.relatedFile = (msg.files && msg.files.length > 0) ? msg.files[0] : ''
    metadataModal.value.messageId = msg.id || null
    metadataModal.value.sessionId = msg.sessionId || ''
    metadataModal.value.indexed = !!msg.indexed
    metadataShow.value = true
}

function handleShowThinkingDetail({ text, msgId, blockKey }) {
  // Store identifiers for reactive lookup (survives messages array replacement on loadHistory)
  activeThinkingOverlay.value = { msgId: String(msgId), blockKey }

  // Initial render
  const block = findThinkingBlock(activeThinkingOverlay.value)
  const currentText = block ? block.text : text // fallback to snapshot if lookup fails

  toolDetailShow.value = true
  toolDetailData.value = {
    name: 'DeepThink',
    displayNameOverride: t('chat.message.deepThinking'),
    summary: '',
    inputHtml: `<div class="thinking-overlay-md">${renderMarkdown(currentText)}</div>`,
    outputHtml: '',
    status: '',
    done: block ? !!block.done : !loading.value,
    _fetchIds: toolDetailData.value._fetchIds,
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
    applySummaryUpdate(msg, data.summary, atBottom)
}

// Toggle summary/original view for a message
function handleToggleSummary(msgId) {
    const msg = messages.value.find(m => m.id === msgId)
    if (!msg) return
    // Pin the toggle button's viewport position so it doesn't jump when
    // the message content height changes (summary → original or vice versa).
    const scrollEl = messageListRef.value?.messagesRef
    let btnTop = null
    if (scrollEl) {
        // Find the SummaryToggle button inside this message's meta bar
        const msgKey = msg.id ? 'db-' + msg.id : null
        if (msgKey) {
            const msgEl = scrollEl.querySelector(`[data-msg-key="${msgKey}"] .chat-meta-bar`)
            if (msgEl) btnTop = msgEl.getBoundingClientRect().top
        }
    }
    msg.showingSummary = !msg.showingSummary
    if (btnTop !== null && scrollEl) {
        nextTick(() => {
            const msgKey = msg.id ? 'db-' + msg.id : null
            if (!msgKey) return
            const msgEl = scrollEl.querySelector(`[data-msg-key="${msgKey}"] .chat-meta-bar`)
            if (!msgEl) return
            const newTop = msgEl.getBoundingClientRect().top
            const delta = newTop - btnTop
            if (Math.abs(delta) > 1) {
                scrollEl.scrollTop += delta
            }
        })
    }
}

// RAG detail drawer
const ragDetailItem = ref(null)
const ragDetailShow = ref(false)
const ragDetailDrawer = useTabDrawer('chat', ragDetailShow)

function handleRagDetail(ragItem) {
    ragDetailItem.value = ragItem
    ragDetailShow.value = true
}

async function handleResumeFromDetail(item) {
    ragDetailItem.value = null
    ragDetailShow.value = false
    if (!item?.sessionId) return
    const confirmed = await dialog.confirm(
        t('chat.contentBlocks.ragResumeConfirm', { title: item.sessionTitle || t('chat.contentBlocks.ragUntitled') }),
        { title: t('chat.contentBlocks.ragResume'), confirmText: t('common.confirm') }
    )
    if (!confirmed) return
    try {
        const resp = await fetch('/api/ai/session/resume', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: item.sessionId }),
        })
        if (!resp.ok) {
            const data = await resp.json().catch(() => ({}))
            toast.show(data.error || t('chat.contentBlocks.ragResumeFailed'), { icon: '⚠️', type: 'error' })
            return
        }
        await session.switchSession(item.sessionId)
    } catch (err) {
        toast.show(t('chat.contentBlocks.ragResumeFailed'), { icon: '⚠️', type: 'error' })
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
    } catch (err) {
        toast.show(t('chat.contentBlocks.ragResumeFailed'), { icon: '⚠️', type: 'error' })
    }
}

// Start one-time session load when component mounts
onMounted(() => {
    // Request notification permission on mount
    notification.requestPermission().catch(err => {
        appLog.w(TAG, 'Failed to request notification permission:', err)
    })

    session.loadSessionsOnce()
    document.addEventListener('visibilitychange', session.handleVisibilityChange)
    window.addEventListener('clawbench-summary-update', handleSummaryUpdate)
})

// Cleanup preview URLs on unmount
onUnmounted(() => {
    removeEventHandler()
    cleanupPreviewUrls()
    stream.disconnectStream()
    stream.stopPolling()
    session.stopMsgCountPolling()
    if (thinkingRenderTimer) { clearTimeout(thinkingRenderTimer); thinkingRenderTimer = null }
    document.removeEventListener('visibilitychange', session.handleVisibilityChange)
    document.removeEventListener('visibilitychange', manager._visibilityHandler)
    window.removeEventListener('clawbench-summary-update', handleSummaryUpdate)
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

/* Make panel content a positioning context so the switching overlay covers
   the message+input area only (not the header above it). */
:deep(.chat-panel-content) {
  position: relative;
}

/* Session swipe indicator — floats at top of message area */
.session-switch-indicator {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 10px 20px 8px;
  background: var(--bg-primary);
  color: var(--text-primary);
  border-radius: 24px;
  font-size: 13px;
  font-weight: 500;
  letter-spacing: 0.3px;
  position: absolute;
  top: 48px;
  left: 0;
  right: 0;
  display: flex;
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
  gap: 6px;
}

/* Dots bar (<=15 sessions) */
.session-dots {
  display: flex;
  align-items: center;
  gap: 4px;
}

.session-dot {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--text-tertiary, rgba(128, 128, 128, 0.4));
  transition: all 0.15s ease-out;
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
  border-radius: 2px;
  background: var(--text-tertiary, rgba(128, 128, 128, 0.3));
  position: relative;
}

.session-capsule-slider {
  position: absolute;
  top: 0;
  height: 3px;
  border-radius: 2px;
  background: var(--accent-color);
  transition: left 0.2s ease-out;
}

/* Numeric label */
.session-position-count {
  font-size: 10px;
  color: var(--text-tertiary, rgba(128, 128, 128, 0.6));
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
  transition: opacity 0.15s ease-out;
}

.session-indicator-leave-active {
  transition: opacity 0.2s ease-in, transform 0.2s ease-in;
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
  border-top-color: var(--primary, #6366f1);
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
  gap: 8px;
  padding: 20px 12px;
  color: var(--text-muted, #9ca3af);
}
.tool-call-empty-msg {
  font-size: 13px;
  font-style: italic;
}
.tool-call-retry-btn {
  font-size: 12px;
  padding: 4px 12px;
  border-radius: 6px;
  border: 1px solid var(--border-color, #e5e7eb);
  background: var(--bg-secondary, #f3f4f6);
  color: var(--text-secondary, #6b7280);
  cursor: pointer;
  transition: all 0.15s;
}
.tool-call-retry-btn:hover {
  background: var(--bg-tertiary, #e5e7eb);
  color: var(--text-primary, #111827);
}
</style>
