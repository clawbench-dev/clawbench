<template>
  <div class="content-blocks">
    <!-- Summary mode: render summary as a single text block.
         Using v-show for summary to avoid Vue Fragment patching issues when
         switching between v-if/v-else branches with nested template v-for.
         Previously, v-if/v-else with template wrappers caused the v-else
         branch to render as an empty comment node because Vue 3's patch
         algorithm fails to correctly transition between different Fragment
         structures (summary div vs blocks template v-for). -->
    <div v-show="showingSummary && summary" v-html="renderTextBlock(summary || '', msgId, 0, false)"></div>
    <!-- Original content mode -->
    <template v-if="!showingSummary || !summary">
    <template v-for="(block, bi) in blocks" :key="stableBlockKey(bi, block)">
      <!-- Thinking block: streaming shows inline content, collapsed shows clickable chip -->
      <div v-if="block.type === 'thinking'"
        :ref="isThinkingStreaming(block) ? (el) => setThinkingRef(stableBlockKey(bi, block), el) : undefined"
        class="chat-thinking"
        :class="{
          'thinking-streaming': isThinkingStreaming(block),
          'thinking-collapsed': !isThinkingStreaming(block),
          'thinking-collapsing': !!collapsingThinking[stableBlockKey(bi, block)],
        }"
        :style="collapsingThinking[stableBlockKey(bi, block)] ? { maxHeight: collapsingMaxHeight[stableBlockKey(bi, block)] + 'px' } : {}"
        @click.stop="!isThinkingStreaming(block) && handleThinkingClick(block, bi)"
      >
        <div class="thinking-header">
          <Brain :size="12" class="thinking-icon" />
          <span class="thinking-label">{{ t('chat.message.deepThinking') }}</span>
          <!-- Status indicators: right-aligned, same pattern as tool_use -->
          <span v-if="isThinkingStreaming(block)" class="thinking-spinner"></span>
          <!-- Cancelled marker: show inline in thinking header when this is the last block and message was cancelled.
               Prevents the cancelled mark from being visually hidden/trapped under the collapsed thinking chip. -->
          <span v-else-if="isLastBlock(bi) && cancelled" class="chat-cancelled-mark-inline">{{ t('chat.contentBlocks.cancelled') }}</span>
          <CheckCircle2 v-else :size="14" color="#22c55e" class="thinking-check" />
        </div>
        <!-- Inline streaming content: only visible during streaming or collapse animation -->
        <div v-if="isThinkingStreaming(block) || !!collapsingThinking[stableBlockKey(bi, block)]" class="thinking-inline-content" v-html="getThinkingHtml(bi, block)"></div>
      </div>
      <!-- Tool use block -->
      <template v-else-if="block.type === 'tool_use'">
        <div class="chat-tool-call" :class="{ done: block.done }" :data-category="getToolIcon(block.name).category" @click.stop="handleToolClick(block, key(bi), bi)">
          <component :is="getToolIcon(block.name).icon" :size="12" class="tool-icon" />
          <span class="tool-name">{{ toolDisplayName(block.name, block.input, block.display_name) }}</span>
          <span v-if="toolCallSummary(block)" class="tool-summary">{{ toolCallSummary(block) }}</span>
          <!-- Loading: spinner -->
          <span v-if="!block.done" class="tool-spinner"></span>
          <!-- Done with error: red X -->
          <XCircle v-else-if="block.status === 'error'" :size="14" color="#ef4444" class="tool-error-icon" />
          <!-- Done (success or unknown): green check -->
          <CheckCircle2 v-else :size="14" color="#22c55e" class="tool-check" />
        </div>
        <!-- Inline detail for auto-expand tools (AskUserQuestion, PermissionApproval) -->
        <div v-if="shouldAutoExpand(block)" class="tool-detail" :data-tool-name="block.name" :data-session-id="sessionId" :data-tool-call-id="block.id" @click="handleToolDetailClick">
          <div v-html="formatToolInput(block.input, block.name, { done: block.done, status: block.status, output: block.output })"></div>
        </div>
      </template>
      <!-- Error block -->
      <div v-else-if="block.type === 'error'" class="chat-error-card">
        <AlertTriangle :size="14" class="error-icon" />
        <span class="error-text">{{ getWarningText(block) }}</span>
      </div>
      <!-- Warning block: severe (disconnect/timeout/restart) renders as error-level red -->
      <div v-else-if="block.type === 'warning' && isSevereWarning(block)" class="chat-error-card">
        <AlertTriangle :size="14" class="error-icon" />
        <span class="error-text">{{ getWarningText(block) }}</span>
      </div>
      <!-- Warning block: normal (parse errors, stderr) renders as amber -->
      <div v-else-if="block.type === 'warning'" class="chat-warning-card">
        <AlertCircle :size="14" class="warning-icon" />
        <span class="warning-text">{{ getWarningText(block) }}</span>
      </div>
      <!-- Scheduled task card(s) — simplified: click navigates to Tasks tab -->
      <template v-else-if="block.type === 'text' && hasScheduledTasks(bi)">
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <div v-for="(sKey, sIdx) in scheduledTaskKeys(bi)" :key="sIdx" class="scheduled-task-card" :class="{ deleted: blockTasks[sKey].deleted }" @click="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task && $emit('task-card-click', blockTasks[sKey].taskId)">
          <div class="stask-header">
            <span v-if="blockTasks[sKey].deleted" class="stask-icon">🗑️</span>
            <span v-else class="stask-icon">⏰</span>
            <template v-if="blockTasks[sKey].deleted">{{ t('chat.contentBlocks.taskDeleted') }}</template>
            <template v-else-if="blockTasks[sKey].loading">{{ t('chat.contentBlocks.loading') }}</template>
            <template v-else>{{ blockTasks[sKey].task?.name || t('chat.contentBlocks.scheduledTaskCreated') }}</template>
            <span v-if="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task" class="stask-status-badge" :class="blockTasks[sKey].task.status">{{ statusLabelSimple(blockTasks[sKey].task) }}</span>
          </div>
          <div v-if="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task" class="stask-body">
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.frequency') }}</strong>{{ humanizeCron(blockTasks[sKey].task.cronExpr) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong>{{ getAgentIcon(blockTasks[sKey].task.agentId) }} {{ getAgentName(blockTasks[sKey].task.agentId) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.repeat') }}</strong>{{ repeatLabel(blockTasks[sKey].task.repeatMode, blockTasks[sKey].task.maxRuns) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.status') }}</strong><span class="stask-status-dot" :class="statusClass(blockTasks[sKey].task)"></span>{{ statusLabel(blockTasks[sKey].task) }}</div>
            <div v-if="blockTasks[sKey].task.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatTime(blockTasks[sKey].task.lastRunAt) }}</div>
            <div v-if="blockTasks[sKey].task.nextRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.nextRun') }}</strong>{{ formatTime(blockTasks[sKey].task.nextRunAt) }}</div>
          </div>
          <div class="stask-view-btn" v-if="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task">
            {{ t('chat.contentBlocks.viewDetail') }}
            <ChevronRight :size="12" />
          </div>
        </div>
      </template>
      <!-- Ask question card (from <ask-question> XML tag in text) — must come before generic text block -->
      <template v-else-if="block.type === 'text' && blockAskQuestions[blockTaskKey(bi)]">
        <!-- Surrounding text (with ask-question tag stripped) -->
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <div class="chat-tool-call done" data-category="ask" @click.stop="$emit('toggle-tool', key(bi))">
          <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
          <span class="tool-name">{{ t('tool.askUser.name') }}</span>
          <span class="tool-summary">{{ askQuestionSummary(blockAskQuestions[blockTaskKey(bi)]) }}</span>
          <CheckCircle2 :size="14" color="#f59e0b" class="tool-warn" />
        </div>
        <div v-if="expandedTools[key(bi)] || true" class="tool-detail" data-tool-name="AskUserQuestion" @click="handleToolDetailClick" v-html="formatToolInput(blockAskQuestions[blockTaskKey(bi)], 'AskUserQuestion')"></div>
      </template>
      <!-- RAG results card (from <rag-results> XML tag in text) — must come before generic text block -->
      <template v-else-if="block.type === 'text' && blockRagResults[blockTaskKey(bi)]">
        <!-- Surrounding text (with rag-results tag stripped) -->
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <div v-for="(ragItem, ragIdx) in blockRagResults[blockTaskKey(bi)]" :key="ragIdx" class="rag-result-card" @click.stop="emit('show-rag-detail', ragItem)">
          <div class="rag-header">
            <span class="rag-icon">🔍</span>
            <span class="rag-title">{{ ragItem.sessionTitle || t('chat.contentBlocks.ragUntitled') }}</span>
          </div>
          <div v-if="ragItem.summary" class="rag-summary">{{ ragItem.summary }}</div>
          <div v-if="ragItem.createdAt" class="rag-time">{{ formatTime(ragItem.createdAt) }}</div>
        </div>
      </template>
      <!-- Text block with @ command badge (user message starting with @chatsearch/@task) -->
      <template v-else-if="block.type === 'text' && extractAtCommand(block.text || '')">
        <span class="at-command-badge">{{ extractAtCommand(block.text).command }}</span>
        <span v-if="extractAtCommand(block.text).rest.trim()" class="at-command-rest">{{ extractAtCommand(block.text).rest.trim() }}</span>
      </template>
      <!-- Text block with slash command badge (user message starting with /command from ACP backend) -->
      <template v-else-if="block.type === 'text' && extractSlashCommand(block.text || '')">
        <span class="slash-command-badge">{{ extractSlashCommand(block.text).command }}</span>
        <span v-if="extractSlashCommand(block.text).rest.trim()" class="at-command-rest">{{ extractSlashCommand(block.text).rest.trim() }}</span>
      </template>
      <!-- Text block: streaming uses throttled render to avoid UI freeze -->
      <div v-else-if="block.type === 'text'" v-html="getBlockHtml(bi, block)"></div>
    </template>
    </template>
    <!-- Loading dots while AI is still streaming (not when cancelled, and not when showing summary) -->
    <div v-if="streaming && !cancelled && !(showingSummary && summary)" class="placeholder-dots"><span></span><span></span><span></span></div>
    <!-- Cancelled marker: hidden when the last block is thinking (shown inline in thinking-header instead) -->
    <div v-if="cancelled && !isLastBlockThinking" class="chat-cancelled-mark">{{ t('chat.contentBlocks.cancelled') }}</div>
  </div>
</template>

<script setup>
import { ref, watch, onUnmounted, computed, nextTick, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { handleToolAction, shouldAutoExpandTool } from '@/utils/renderToolDetail.ts'
import { getToolIcon, toolDisplayName } from '@/utils/icons'
import { Brain, ChevronRight, CheckCircle2, AlertCircle, AlertTriangle, XCircle } from 'lucide-vue-next'
import { renderMarkdown } from '@/composables/useMarkdownRenderer.ts'
import {
  isSevereWarning,
  getWarningText as getWarningTextUtil,
  statusClass as statusClassUtil,
  statusLabel as statusLabelUtil,
  statusLabelSimple as statusLabelSimpleUtil,
  formatTime as formatTimeUtil,
  askQuestionSummary as askQuestionSummaryUtil,
  blockKey,
  blockTaskKey as blockTaskKeyUtil,
  buildTaskKeyIndex,
  hasScheduledTasks as hasScheduledTasksUtil,
  scheduledTaskKeys as scheduledTaskKeysUtil,
  extractAtCommand,
  extractSlashCommand,
} from '@/utils/contentBlocks.ts'

const { t, locale } = useI18n()

// Auto-expand tools (AskUserQuestion, PermissionApproval) need input to render inline.
// In slim format, input is absent from DB-loaded content — fetch from API automatically.
const fetchedAutoExpandBlocks = new Set()
onMounted(() => {
  for (let i = 0; i < props.blocks.length; i++) {
    const block = props.blocks[i]
    if (block.type === 'tool_use' && shouldAutoExpandTool(block.name || '')) {
      const hasInput = block.input && Object.keys(block.input).length > 0
      if (!hasInput && block.id && props.msgId) {
        const cacheKey = `${block.id}:${props.msgId}`
        if (fetchedAutoExpandBlocks.has(cacheKey)) continue
        fetchedAutoExpandBlocks.add(cacheKey)
        fetchToolCallInputForAutoExpand(block, props.msgId)
      }
    }
  }
})
async function fetchToolCallInputForAutoExpand(block, msgId) {
  try {
    const resp = await fetch(`/api/ai/chat/tool-call?tool_id=${encodeURIComponent(block.id)}&message_id=${encodeURIComponent(msgId)}`)
    if (!resp.ok) return
    const data = await resp.json()
    if (data.input) {
      const input = typeof data.input === 'string' ? JSON.parse(data.input) : data.input
      if (input && Object.keys(input).length > 0) {
        block.input = input
      }
    }
    if (data.output && !block.output) {
      block.output = data.output
    }
  } catch { /* best effort */ }
}

// Re-export utility functions with i18n context bound
function getWarningText(block) { return getWarningTextUtil(block, t) }
function statusClass(task) { return statusClassUtil(task) }
function statusLabel(task) { return statusLabelUtil(task, t) }
function statusLabelSimple(task) { return statusLabelSimpleUtil(task, t) }
function formatTime(iso) { return formatTimeUtil(iso, locale.value, t) }
function askQuestionSummary(input) { return askQuestionSummaryUtil(input) }

function shouldAutoExpand(block) {
  return shouldAutoExpandTool(block.name || '')
}

/** Handle tool call bar click: open overlay for regular tools, toggle inline for AskUserQuestion. */
function handleToolClick(block, blockKeyStr, blockIdx) {
  // AskUserQuestion stays inline — toggle expand state
  if (shouldAutoExpand(block)) {
    emit('toggle-tool', blockKeyStr)
    return
  }
  // All other tools: open the overlay with block data
  // Slim format: input/output may be absent — overlay will fetch from API if needed
  emit('show-tool-detail', {
    name: block.name,
    input: block.input,
    output: block.output,
    status: block.status,
    done: block.done,
    display_name: block.display_name,
    summary: block.summary,
    tool_id: block.id,
    msgId: props.msgId,
    blockIdx,
  })
}

const props = defineProps({
  blocks: { type: Array, default: () => [] },
  msgId: { type: [String, Number], default: '' },
  msgIndex: { type: Number, default: 0 },
  sessionId: { type: String, default: '' },
  expandedTools: { type: Object, default: () => ({}) },
  blockTasks: { type: Object, default: () => ({}) },
  blockAskQuestions: { type: Object, default: () => ({}) },
  blockRagResults: { type: Object, default: () => ({}) },
  streaming: { type: Boolean, default: false },
  cancelled: { type: Boolean, default: false },
  summary: { type: String, default: null },
  showingSummary: { type: Boolean, default: false },
  // Render functions
  renderTextBlock: { type: Function, required: true },
  formatToolInput: { type: Function, required: true },
  toolCallSummary: { type: Function, required: true },
  humanizeCron: { type: Function, default: () => '' },
  repeatLabel: { type: Function, default: () => '' },
  truncate: { type: Function, default: (s) => s },
  getAgentIcon: { type: Function, default: () => '' },
  getAgentName: { type: Function, default: () => '' },
  // Performance: static block cache from useChatRender (Problem 6)
  staticBlockCache: { type: Object, default: null },
  active: { type: Boolean, default: true },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-thinking-detail', 'task-card-click', 'send-message', 'render-flush', 'resume-session', 'show-rag-detail'])

// Key helper: use msgId if available, otherwise msgIndex
function key(bi) {
  return blockKey(props.msgId, bi)
}

// Key for blockTasks/blockAskQuestions lookup — prefix format used in useChatRender.ts
function blockTaskKey(bi) {
  return blockTaskKeyUtil(props.msgId, bi)
}

// Pre-computed index: block index → sorted array of scheduled task keys.
const taskKeyIndex = computed(() => buildTaskKeyIndex(props.msgId, props.blockTasks))

// Check if a block has any scheduled tasks
function hasScheduledTasks(bi) {
  return hasScheduledTasksUtil(taskKeyIndex.value, bi)
}

// Return all scheduled task keys for a block, sorted by tag index
function scheduledTaskKeys(bi) {
  return scheduledTaskKeysUtil(taskKeyIndex.value, bi)
}

/** Generate a stable key for a block, used for v-for :key and animation state.
 *  tool_use: block.id (unique tool call ID from backend)
 *  thinking: block._key (stable key assigned at creation/parsing)
 *  text: text-${bi} (text blocks merge so index is stable)
 *  other: type-bi (fallback) */
function stableBlockKey(bi, block) {
  if (block.type === 'tool_use' && block.id) return block.id
  if (block.type === 'thinking' && block._key) return block._key
  return `${block.type || 'other'}-${bi}`
}

function handleThinkingClick(block, bi) {
  emit('show-thinking-detail', { text: block.text, msgId: props.msgId, blockKey: block._key })
}

/** Whether a thinking block should show inline streaming content.
 *  Thinking blocks can be marked as done via the `thinking_done` SSE event
 *  (ACP backend), which sets `block.done = true`. When explicitly done, the
 *  spinner and inline content should hide immediately. Otherwise, fall back
 *  to the message-level `streaming` prop. */
function isThinkingStreaming(block) {
  if (block.done) return false
  return props.streaming
}

/** Whether the given block index is the last block in the blocks array. */
function isLastBlock(bi) {
  return bi === (props.blocks?.length || 0) - 1
}

/** Whether the last block is a thinking block (used to avoid duplicate cancelled marker). */
const isLastBlockThinking = computed(() => {
  const blocks = props.blocks
  if (!blocks || blocks.length === 0) return false
  return blocks[blocks.length - 1].type === 'thinking'
})

// ── Thinking block collapse animation state ──
const collapsingThinking = ref({})   // { [blockKey]: true } for blocks mid-collapse
const collapsingMaxHeight = ref({})  // { [blockKey]: maxHeightPx } for animation
let _collapseElRefs = {}             // { [blockKey]: HTMLElement } tracked during streaming
let _collapseTimers = []             // setTimeout IDs for collapse animation (cleaned up on unmount)

// Collapse animation constants — must stay in sync with CSS
const COLLAPSED_CHIP_HEIGHT = 28     // px — matches .thinking-collapsed { max-height: 28px }
const COLLAPSE_READ_DELAY = 3000     // ms — time to read thinking output before collapsing
const COLLAPSE_TRANSITION_MS = 400   // ms — must match CSS transition-duration + buffer
const COLLAPSE_PLACEHOLDER_MAX = 99999 // large sentinel to keep content visible during nextTick

/** Track thinking block element refs during streaming for collapse animation. */
function setThinkingRef(key, el) {
  if (el) {
    _collapseElRefs[key] = el
  } else {
    delete _collapseElRefs[key]
  }
}

/** Click inside expanded tool-detail: dispatch to tool action handlers first, then fall through to generic behavior. */
function handleToolDetailClick(event) {
  // Try tool-specific action handler first (via data-tool-name on the .tool-detail container)
  const toolName = event.currentTarget.dataset?.toolName
  if (toolName && handleToolAction(toolName, event, emit)) return
  // Allow file-open buttons, commit-hash elements, and table rows to bubble
  if (event.target.closest('.chat-file-open-btn') || event.target.closest('.chat-commit-hash, .chat-commit-open-btn') || event.target.closest('.chat-worktree-btn') || event.target.closest('tbody tr[data-row-idx]')) {
    return
  }
  event.stopPropagation()
}

// ── Throttled streaming render ──
const blockHtmlCache = ref({})
let _throttleTimer = null
let _throttlePending = false
const THROTTLE_MS = 300

function flushBlockHtml() {
  _throttleTimer = null
  if (!_throttlePending) return
  // Skip rendering when panel not visible
  if (!props.active) {
    _throttlePending = false
    return
  }
  _throttlePending = false
  const newCache = {}
  for (let i = 0; i < (props.blocks?.length || 0); i++) {
    const block = props.blocks[i]
    const key = stableBlockKey(i, block)
    if (block.type === 'text') {
      // streaming=true: deferred rendering — pure markdown only
      newCache[key] = props.renderTextBlock(block.text, props.msgId, i, true)
    } else if (block.type === 'thinking') {
      // Thinking blocks use renderMarkdown during streaming
      newCache[`t-${key}`] = renderMarkdown(block.text)
    }
  }
  blockHtmlCache.value = newCache
  // Throttled render flush can change content height (paragraph wrapping, code blocks, etc.)
  // without a corresponding onScrollBottom call from the stream handler. Notify the parent
  // so it can re-sync the scroll position if the user is at the bottom.
  emit('render-flush')
}

function getBlockHtml(bi, block) {
  if (!props.streaming) {
    // Non-streaming: full pipeline with cache
    if (props.staticBlockCache) {
      const cached = props.staticBlockCache.get(props.msgId, bi, block.text)
      if (cached !== undefined) {
        // If this entry was deferred (skipEnhancements=true), schedule an upgrade
        // but return the fast-rendered version immediately for instant display
        if (props.staticBlockCache.isDeferred(props.msgId, bi, block.text)) {
          props.staticBlockCache.scheduleUpgrade()
        }
        return cached
      }
      // First render: use fast path (skipEnhancements=true) for instant display,
      // then schedule upgrade to full pipeline (KaTeX, annotations, etc.)
      const fastHtml = props.renderTextBlock(block.text, props.msgId, bi, false, true)
      props.staticBlockCache.set(props.msgId, bi, block.text, fastHtml, true)
      props.staticBlockCache.scheduleUpgrade()
      return fastHtml
    }
    return props.renderTextBlock(block.text, props.msgId, bi, false)
  }
  // Streaming + panel not visible: skip expensive markdown parsing
  if (!props.active) {
    return ''
  }
  // Streaming: deferred rendering with throttling
  const key = stableBlockKey(bi, block)
  if (blockHtmlCache.value[key] !== undefined) {
    if (!_throttleTimer) {
      const newCache = { ...blockHtmlCache.value }
      newCache[key] = props.renderTextBlock(block.text, props.msgId, bi, true)
      blockHtmlCache.value = newCache
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value[key]
  }
  const html = props.renderTextBlock(block.text, props.msgId, bi, true)
  blockHtmlCache.value = { ...blockHtmlCache.value, [key]: html }
  return html
}

/** Get HTML for thinking block content during streaming (uses renderMarkdown with throttling). */
function getThinkingHtml(bi, block) {
  if (!props.streaming || !props.active) {
    return renderMarkdown(block.text)
  }
  const cacheKey = `t-${stableBlockKey(bi, block)}`
  // Streaming: deferred rendering with throttling (same pattern as text blocks)
  if (blockHtmlCache.value[cacheKey] !== undefined) {
    if (!_throttleTimer) {
      const newCache = { ...blockHtmlCache.value }
      newCache[cacheKey] = renderMarkdown(block.text)
      blockHtmlCache.value = newCache
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value[cacheKey]
  }
  const html = renderMarkdown(block.text)
  blockHtmlCache.value = { ...blockHtmlCache.value, [cacheKey]: html }
  return html
}

watch(() => props.streaming, (streaming, wasStreaming) => {
  if (wasStreaming && !streaming) {
    if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
    _throttlePending = false
    // Snapshot element refs before Vue clears them
    // (isThinkingStreaming becomes false → :ref becomes undefined → ref cleared).
    const refSnapshot = { ..._collapseElRefs }
    _collapseElRefs = {}
    // Only process blocks not already being collapsed by Trigger B (thinking_done)
    const collapsingKeys = Object.keys(refSnapshot).filter(
      idxStr => !collapsingThinking.value[idxStr]
    )
    if (collapsingKeys.length > 0) {
      // Immediately mark as "collapsing" with a large maxHeight so the template
      // keeps showing inline content and doesn't flash to the 28px collapsed chip.
      const placeholderCollapsing = {}
      const placeholderMaxHeight = {}
      for (const idxStr of collapsingKeys) {
        placeholderCollapsing[idxStr] = true
        placeholderMaxHeight[idxStr] = COLLAPSE_PLACEHOLDER_MAX
      }
      Object.assign(collapsingThinking.value, placeholderCollapsing)
      Object.assign(collapsingMaxHeight.value, placeholderMaxHeight)
    }
    // Clear throttle cache and force a full re-render of thinking HTML
    // so the DOM reflects the complete block.text before we measure scrollHeight.
    // Without this, scrollHeight may reflect stale throttled HTML (up to 300ms old),
    // causing the collapse animation to clip the bottom of the content.
    blockHtmlCache.value = {}
    // Wait for Vue to flush the DOM with complete content, then measure heights.
    nextTick(() => {
      const newCollapsing = {}
      const newMaxHeights = {}
      for (const idxStr of collapsingKeys) {
        const el = refSnapshot[idxStr]
        // el.isConnected: defensive check — element must still be in the document
        if (el && el.isConnected && el.scrollHeight) {
          newCollapsing[idxStr] = true
          newMaxHeights[idxStr] = el.scrollHeight
        }
      }
      if (Object.keys(newCollapsing).length > 0) {
        Object.assign(collapsingThinking.value, newCollapsing)
        Object.assign(collapsingMaxHeight.value, newMaxHeights)
        // Delay before starting collapse animation so user can read the thinking output
        const t1 = setTimeout(() => {
          const updatedMaxHeights = {}
          Object.keys(newCollapsing).forEach(idx => {
            updatedMaxHeights[idx] = COLLAPSED_CHIP_HEIGHT
          })
          Object.assign(collapsingMaxHeight.value, updatedMaxHeights)
          // After transition completes, clean up animation state
          const t2 = setTimeout(() => {
            for (const idx of Object.keys(newCollapsing)) {
              delete collapsingThinking.value[idx]
              delete collapsingMaxHeight.value[idx]
            }
          }, COLLAPSE_TRANSITION_MS)
          _collapseTimers.push(t2)
        }, COLLAPSE_READ_DELAY)
        _collapseTimers.push(t1)
      } else {
        // No measurable elements — clean up placeholder state
        for (const idxStr of collapsingKeys) {
          delete collapsingThinking.value[idxStr]
          delete collapsingMaxHeight.value[idxStr]
        }
      }
    })
  }
})

// Watch for thinking blocks that become "done" mid-stream (via thinking_done SSE event).
// This triggers the collapse animation immediately instead of waiting for streaming to end.
watch(() => props.blocks.filter(b => b.type === 'thinking' && b.done).map(b => b.done), (_newDones, _oldDones) => {
  if (!props.streaming) return // Only relevant during streaming
  // Find thinking blocks that just became done (newly true), not already collapsing
  const refSnapshot = {}
  for (let i = 0; i < props.blocks.length; i++) {
    const block = props.blocks[i]
    if (block.type === 'thinking' && block.done) {
      const key = stableBlockKey(i, block)
      const el = _collapseElRefs[key]
      if (el && !collapsingThinking.value[key]) {
        refSnapshot[key] = el
        delete _collapseElRefs[key]
      }
    }
  }
  const collapsingKeys = Object.keys(refSnapshot)
  if (collapsingKeys.length === 0) return
  // Immediately mark as collapsing with placeholder maxHeight to prevent
  // flash to 28px collapsed chip while we wait for nextTick.
  const placeholderCollapsing = {}
  const placeholderMaxHeight = {}
  for (const idxStr of collapsingKeys) {
    placeholderCollapsing[idxStr] = true
    placeholderMaxHeight[idxStr] = COLLAPSE_PLACEHOLDER_MAX
  }
  Object.assign(collapsingThinking.value, placeholderCollapsing)
  Object.assign(collapsingMaxHeight.value, placeholderMaxHeight)
  // Clear throttle cache so DOM re-renders with complete thinking content
  blockHtmlCache.value = {}
  // Wait for Vue to flush DOM, then measure scrollHeight of complete content
  nextTick(() => {
    const newCollapsing = {}
    const newMaxHeights = {}
    for (const idxStr of collapsingKeys) {
      const el = refSnapshot[idxStr]
      if (el && el.isConnected && el.scrollHeight) {
        newCollapsing[idxStr] = true
        newMaxHeights[idxStr] = el.scrollHeight
      }
    }
    if (Object.keys(newCollapsing).length > 0) {
      Object.assign(collapsingThinking.value, newCollapsing)
      Object.assign(collapsingMaxHeight.value, newMaxHeights)
      // Delay before starting collapse animation so user can read the thinking output
      const t1 = setTimeout(() => {
        const updatedMaxHeights = {}
        Object.keys(newCollapsing).forEach(idx => {
          updatedMaxHeights[idx] = COLLAPSED_CHIP_HEIGHT
        })
        Object.assign(collapsingMaxHeight.value, updatedMaxHeights)
        const t2 = setTimeout(() => {
          for (const idx of Object.keys(newCollapsing)) {
            delete collapsingThinking.value[idx]
            delete collapsingMaxHeight.value[idx]
          }
        }, COLLAPSE_TRANSITION_MS)
        _collapseTimers.push(t2)
      }, COLLAPSE_READ_DELAY)
      _collapseTimers.push(t1)
    } else {
      // No measurable elements — clean up placeholder state
      for (const idxStr of collapsingKeys) {
        delete collapsingThinking.value[idxStr]
        delete collapsingMaxHeight.value[idxStr]
      }
    }
  })
})

// Reset cache when panel becomes active — allows re-render with fresh markdown
watch(() => props.active, (active) => {
  if (active) {
    blockHtmlCache.value = {}
    if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
    _throttlePending = false
  }
})

onUnmounted(() => {
  if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
  _collapseTimers.forEach(t => clearTimeout(t))
  _collapseTimers = []
})
</script>

<style scoped>
.placeholder-dots {
  display: flex;
  gap: 4px;
  align-items: center;
  padding: 8px 0 4px;
}
.placeholder-dots span {
  width: 7px; height: 7px;
  border-radius: 50%;
  background: var(--text-muted, #999);
  animation: dot-bounce 1.2s infinite ease-in-out;
}
.placeholder-dots span:nth-child(1) { animation-delay: 0s; }
.placeholder-dots span:nth-child(2) { animation-delay: 0.2s; }
.placeholder-dots span:nth-child(3) { animation-delay: 0.4s; }

@keyframes dot-bounce {
  0%, 80%, 100% { transform: scale(0.6); opacity: 0.4; }
  40% { transform: scale(1); opacity: 1; }
}

.chat-cancelled-mark {
  display: inline-block;
  font-size: 11px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 2px 8px;
  border-radius: 4px;
  margin-top: 4px;
}

/* Inline cancelled marker inside thinking header — always visible even when thinking is collapsed */
.chat-cancelled-mark-inline {
  font-size: 11px;
  color: var(--text-muted, #999);
  background: var(--bg-tertiary, #f0f0f0);
  padding: 1px 6px;
  border-radius: 4px;
  margin-left: auto;
}





.chat-error-card {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin: 2px 0;
  border-left: 3px solid #ef4444;
  background: rgba(239, 68, 68, 0.08);
}

.chat-error-card .error-icon {
  flex-shrink: 0;
  color: #ef4444;
}

.chat-error-card .error-text {
  font-size: 12px;
  font-weight: 500;
  color: #dc2626;
}

:root[data-theme="dark"] .chat-error-card {
  border-left-color: #f87171;
  background: rgba(248, 113, 113, 0.1);
}

:root[data-theme="dark"] .chat-error-card .error-icon {
  color: #f87171;
}

:root[data-theme="dark"] .chat-error-card .error-text {
  color: #fca5a5;
}

.chat-warning-card {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin: 2px 0;
  border-left: 3px solid #f59e0b;
  background: rgba(245, 158, 11, 0.08);
}

.chat-warning-card .warning-icon {
  flex-shrink: 0;
  color: #f59e0b;
}

.chat-warning-card .warning-text {
  font-size: 12px;
  font-weight: 500;
  color: #d97706;
  white-space: pre-wrap;
  word-break: break-word;
}

:root[data-theme="dark"] .chat-warning-card {
  border-left-color: #fbbf24;
  background: rgba(251, 191, 36, 0.1);
}

:root[data-theme="dark"] .chat-warning-card .warning-icon {
  color: #fbbf24;
}

:root[data-theme="dark"] .chat-warning-card .warning-text {
  color: #fcd34d;
}

/* Thinking block */
.chat-thinking {
  --thinking-accent: #8b5cf6;
  background: color-mix(in srgb, var(--thinking-accent) 6%, var(--bg-secondary));
  border: 1px solid color-mix(in srgb, var(--thinking-accent) 15%, var(--border-color));
  border-radius: 4px;
  margin: 4px 0;
  overflow: hidden;
  width: 100%;
}

:root[data-theme="dark"] .chat-thinking {
  --thinking-accent: #a78bfa;
}

/* Collapsed state: clickable chip (header only) */
.chat-thinking.thinking-collapsed {
  cursor: pointer;
  max-height: 28px;
  transition: max-height 0.35s ease-out;
}

.chat-thinking.thinking-collapsed:hover {
  background: color-mix(in srgb, var(--thinking-accent) 12%, var(--bg-secondary));
}

/* Streaming state: inline content visible, no height constraint */
.chat-thinking.thinking-streaming {
  max-height: none;
}

/* Collapse animation state: transitioning from full to collapsed */
.chat-thinking.thinking-collapsing {
  transition: max-height 0.35s ease-out;
  overflow: hidden;
}

.thinking-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  font-size: 12px;
  color: var(--text-secondary);
}

.thinking-icon {
  color: color-mix(in srgb, var(--thinking-accent) 80%, transparent);
  flex-shrink: 0;
}

.thinking-label {
  font-weight: 600;
  color: var(--thinking-accent);
  font-size: 11px;
}

.thinking-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid var(--border-color);
  border-top-color: var(--thinking-accent);
  border-radius: 50%;
  animation: tool-spin 0.6s linear infinite;
  flex-shrink: 0;
  margin-left: auto;
}

.thinking-check {
  flex-shrink: 0;
  margin-left: auto;
}

.thinking-inline-content {
  padding: 0 8px 6px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-secondary);
  word-break: break-word;
}

/* Markdown styles inside thinking inline content */
.thinking-inline-content p { margin: 0 0 0.5em; }
.thinking-inline-content pre {
  margin: 0.5em 0;
  padding: 6px 8px;
  border-radius: 4px;
  overflow-x: auto;
  font-size: 11px;
}
.thinking-inline-content code {
  font-size: 11px;
  padding: 1px 4px;
  border-radius: 3px;
}
.thinking-inline-content pre code {
  padding: 0;
  background: none;
}
.thinking-inline-content blockquote {
  margin: 0.5em 0;
  padding: 4px 8px;
  border-left: 2px solid var(--text-tertiary, #aaa);
}
.thinking-inline-content h1,
.thinking-inline-content h2,
.thinking-inline-content h3 {
  font-size: 13px;
  font-weight: 600;
  margin: 0.5em 0 0.3em;
}
.thinking-inline-content ul,
.thinking-inline-content ol {
  margin: 0.3em 0;
  padding-left: 1.5em;
}
.thinking-inline-content table {
  border-collapse: collapse;
  margin: 0.5em 0;
  font-size: 11px;
}
.thinking-inline-content th,
.thinking-inline-content td {
  border: 1px solid var(--border-color);
  padding: 2px 6px;
}
.thinking-inline-content th {
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  font-weight: 600;
}
.thinking-inline-content strong {
  font-weight: 600;
  color: var(--text-primary);
}
.thinking-inline-content em {
  font-style: italic;
}
.thinking-inline-content mark {
  background: rgba(245, 158, 11, 0.2);
  color: inherit;
  padding: 1px 2px;
  border-radius: 2px;
}

/* Tool calls display */
.chat-tool-call {
  --tool-accent: var(--text-muted);
  display: flex;
  flex-wrap: nowrap;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--tool-accent) 6%, var(--bg-secondary));
  border: 1px solid color-mix(in srgb, var(--tool-accent) 15%, var(--border-color));
  padding: 3px 8px;
  border-radius: 4px;
  cursor: pointer;
  width: 100%;
  margin-top: 4px;
  overflow: hidden;
}

.chat-tool-call[data-category="file"]     { --tool-accent: var(--accent-color); }
.chat-tool-call[data-category="bash"]     { --tool-accent: #10b981; }
.chat-tool-call[data-category="search"]   { --tool-accent: #8b5cf6; }
.chat-tool-call[data-category="task"]     { --tool-accent: #f59e0b; }
.chat-tool-call[data-category="plan"]     { --tool-accent: var(--accent-color); }
.chat-tool-call[data-category="agent"]    { --tool-accent: #ec4899; }
.chat-tool-call[data-category="skill"]    { --tool-accent: #06b6d4; }
.chat-tool-call[data-category="ask"]      { --tool-accent: #f97316; }
.chat-tool-call[data-category="permission"] { --tool-accent: #eab308; }
.chat-tool-call[data-category="fallback"] { --tool-accent: var(--text-muted); }

.chat-tool-call:hover {
  background: color-mix(in srgb, var(--tool-accent) 12%, var(--bg-secondary));
}

.chat-tool-call .tool-icon {
    color: color-mix(in srgb, var(--tool-accent) 80%, transparent);
    flex-shrink: 0;
}

.chat-tool-call .tool-name {
  font-weight: 600;
  color: var(--tool-accent);
  font-size: 11px;
}

.chat-tool-call .tool-summary {
  color: var(--text-tertiary, #888);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-tool-call .tool-check {
  flex-shrink: 0;
  margin-left: auto;
}

.chat-tool-call .tool-warn {
  flex-shrink: 0;
  margin-left: auto;
}

.chat-tool-call .tool-error-icon {
  flex-shrink: 0;
  margin-left: auto;
}

/* Inline tool detail — only used by AskUserQuestion (other tools use ToolDetailDrawer) */
.tool-detail {
  margin: 2px 0 4px 0;
  padding: 6px 8px;
  font-size: 11px;
  line-height: 1.4;
  background: var(--bg-primary);
  border-radius: 4px;
  border: 1px solid var(--border-color);
  white-space: normal;
  overflow-x: hidden;
  overflow-y: auto;
  max-height: 500px;
  cursor: default;
}

.tool-spinner {
  width: 10px;
  height: 10px;
  border: 1.5px solid var(--border-color);
  border-top-color: var(--tool-accent);
  border-radius: 50%;
  animation: tool-spin 0.6s linear infinite;
  flex-shrink: 0;
  margin-left: auto;
}

@keyframes tool-spin {
  to { transform: rotate(360deg); }
}

.scheduled-task-card {
  margin: 8px 0;
  border: 1px solid color-mix(in srgb, var(--accent-color, #4a90d9) 30%, var(--border-color, #dee2e6));
  border-radius: 8px;
  overflow: hidden;
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 6%, var(--bg-primary, #fff));
}

.scheduled-task-card.deleted {
  opacity: 0.5;
  border-color: var(--border-color, #dee2e6);
  background: var(--bg-secondary);
}

.scheduled-task-card.deleted .stask-header {
  background: var(--bg-tertiary);
  color: var(--text-muted, #999);
  border-bottom-color: var(--border-color, #dee2e6);
}

.stask-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 12%, transparent);
  color: var(--accent-color, #4a90d9);
  font-weight: 600;
  font-size: 12px;
  border-bottom: 1px solid color-mix(in srgb, var(--accent-color, #4a90d9) 15%, var(--border-color, #dee2e6));
  cursor: pointer;
}

.stask-icon {
  margin-right: 4px;
}

.stask-body {
  padding: 10px 12px;
  font-size: 12px;
  line-height: 1.6;
}

.stask-row {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
}

.stask-row strong {
  min-width: 70px;
  color: var(--text-secondary, #495057);
}

.stask-view-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 6px 0;
  font-size: 12px;
  color: var(--accent-color, #0066cc);
  font-weight: 500;
}

.stask-status-badge {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  font-weight: 500;
  margin-left: auto;
}

.stask-status-badge.active { background: rgba(34, 197, 94, 0.12); color: #22c55e; }
.stask-status-badge.paused { background: rgba(234, 179, 8, 0.12); color: #eab308; }
.stask-status-badge.completed { background: var(--bg-tertiary, #e9ecef); color: var(--text-muted, #999); }

.stask-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  align-self: center;
  margin-right: 4px;
}

.stask-status-dot.status-active {
  background: #4caf50;
}

.stask-status-dot.status-paused {
  background: #ff9800;
}

.stask-status-dot.status-completed {
  background: #9e9e9e;
}

/* RAG result card */
.rag-result-card {
  margin: 6px 0;
  border: 1px solid color-mix(in srgb, #8b5cf6 30%, var(--border-color, #dee2e6));
  border-radius: 8px;
  background: color-mix(in srgb, #8b5cf6 6%, var(--bg-primary, #fff));
  cursor: pointer;
  transition: box-shadow 0.15s, border-color 0.15s;
}

.rag-result-card:hover {
  border-color: color-mix(in srgb, #8b5cf6 50%, var(--border-color, #dee2e6));
  box-shadow: 0 2px 8px color-mix(in srgb, #8b5cf6 15%, transparent);
}

.rag-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  background: color-mix(in srgb, #8b5cf6 12%, transparent);
  color: #8b5cf6;
  font-weight: 600;
  font-size: 12px;
  border-bottom: 1px solid color-mix(in srgb, #8b5cf6 15%, var(--border-color, #dee2e6));
  overflow: hidden;
}

:root[data-theme="dark"] .rag-header {
  color: #a78bfa;
  background: color-mix(in srgb, #a78bfa 12%, transparent);
  border-bottom-color: color-mix(in srgb, #a78bfa 15%, var(--border-color, #dee2e6));
}

.rag-icon {
  margin-right: 4px;
}

.rag-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rag-summary {
  padding: 8px 12px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary, #495057);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  text-overflow: ellipsis;
  word-break: break-word;
  position: relative;
}

/* Fade-out gradient at bottom of clamped summary — hints at truncated content */
.rag-summary::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 1.4em;
  background: linear-gradient(to bottom, transparent, color-mix(in srgb, #8b5cf6 6%, var(--bg-primary, #fff)));
  pointer-events: none;
}

:root[data-theme="dark"] .rag-summary::after {
  background: linear-gradient(to bottom, transparent, color-mix(in srgb, #a78bfa 6%, var(--bg-primary, #1a1a1a)));
}

.rag-time {
  padding: 0 12px 6px;
  font-size: 11px;
  color: var(--text-muted, #999);
}

/* @ command badge in user messages */
.at-command-badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  background: color-mix(in srgb, #8b5cf6 15%, transparent);
  color: #8b5cf6;
  font-size: 12px;
  font-weight: 600;
  margin-right: 4px;
  vertical-align: baseline;
  line-height: 1.6;
}

:root[data-theme="dark"] .at-command-badge {
  background: color-mix(in srgb, #a78bfa 15%, transparent);
  color: #a78bfa;
}

/* Inside user bubble: use white-based palette for contrast against colored background */
.chat-message.user .at-command-badge {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.95);
}

/* Slash command badge in user messages (ACP backend commands) */
.slash-command-badge {
  display: inline-block;
  padding: 1px 8px;
  border-radius: 10px;
  background: color-mix(in srgb, #0ea5e9 15%, transparent);
  color: #0ea5e9;
  font-size: 12px;
  font-weight: 600;
  margin-right: 4px;
  vertical-align: baseline;
  line-height: 1.6;
}

:root[data-theme="dark"] .slash-command-badge {
  background: color-mix(in srgb, #38bdf8 15%, transparent);
  color: #38bdf8;
}

/* Inside user bubble: use white-based palette for contrast against colored background */
.chat-message.user .slash-command-badge {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.95);
}

.at-command-rest {
  /* Rest of the message text after the badge */
}
</style>

<style>
/* Non-scoped styles for v-html penetration — tool detail rendering */


:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="bash"]   { --tool-accent: #34d399; }
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="search"] { --tool-accent: #a78bfa; }
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="task"]   { --tool-accent: #fbbf24; }
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="agent"]  { --tool-accent: #f472b6; }
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="skill"]  { --tool-accent: #22d3ee; }

/* Tool output section */
.content-blocks .tool-detail .tool-output-section {
  margin-top: 6px;
  border-top: 1px solid var(--border-color);
  padding-top: 6px;
}

.content-blocks .tool-detail .tool-output-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
}

.content-blocks .tool-detail .tool-output-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
  font-weight: 600;
}

:root[data-theme="dark"] .content-blocks .tool-detail .tool-output-label {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.content-blocks .tool-detail .tool-output-status {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  font-weight: 600;
}

.content-blocks .tool-detail .tool-output-success {
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
}

:root[data-theme="dark"] .content-blocks .tool-detail .tool-output-success {
  background: rgba(74, 222, 128, 0.15);
  color: #4ade80;
}

.content-blocks .tool-detail .tool-output-error {
  background: rgba(239, 68, 68, 0.12);
  color: #dc2626;
}

:root[data-theme="dark"] .content-blocks .tool-detail .tool-output-error {
  background: rgba(248, 113, 113, 0.15);
  color: #fca5a5;
}

.content-blocks .tool-detail .tool-output-body {
  max-height: 200px;
  overflow-y: auto;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .tool-output-body pre {
  margin: 0;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .tool-output-default pre {
  background: var(--bg-tertiary);
  border-radius: 4px;
  padding: 6px 8px;
}

.content-blocks .tool-detail .tool-file-header {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-bottom: 4px;
  padding-bottom: 4px;
  padding-right: 22px;
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
}

.content-blocks .tool-detail .tool-file-header .chat-file-open-btn {
  position: absolute;
  top: 0;
  right: 0;
  flex-shrink: 0;
}

.content-blocks .tool-detail .tool-file-path {
  font-family: 'SF Mono', 'Fira Code', Menlo, monospace;
  font-size: 11px;
  font-weight: 600;
  color: var(--accent-color);
  word-break: break-all;
  flex: 1;
  min-width: 0;
}

.content-blocks .tool-detail .edit-diff-view {
  display: flex;
  flex-direction: column;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .edit-diff-replace-all {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(245, 158, 11, 0.12);
  color: #d97706;
  font-weight: 600;
  white-space: nowrap;
}

.content-blocks .tool-detail .edit-diff-scroll {
  overflow-x: auto;
}

.content-blocks .tool-detail .edit-diff-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  min-width: max-content;
}

.content-blocks .tool-detail .edit-diff-del {
  background: rgba(239, 68, 68, 0.08);
  color: #dc2626;
  white-space: pre;
}

.content-blocks .tool-detail .edit-diff-add {
  background: rgba(34, 197, 94, 0.08);
  color: #16a34a;
  white-space: pre;
}

:root[data-theme="dark"] .content-blocks .tool-detail .edit-diff-del {
  background: rgba(248, 113, 113, 0.1);
  color: #fca5a5;
}

:root[data-theme="dark"] .content-blocks .tool-detail .edit-diff-add {
  background: rgba(74, 222, 128, 0.1);
  color: #86efac;
}

:root[data-theme="dark"] .content-blocks .tool-detail .edit-diff-replace-all {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}

.content-blocks .tool-detail .file-preview-view {
  display: flex;
  flex-direction: column;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .file-preview-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  overflow-x: auto;
}

.content-blocks .tool-detail .file-preview-line {
  white-space: pre;
  color: var(--text-primary);
}

.content-blocks .tool-detail .file-preview-meta {
  white-space: normal;
  color: var(--text-muted, #999);
  font-style: italic;
  padding: 4px 0;
}

.content-blocks .tool-detail .file-write-view {
  display: flex;
  flex-direction: column;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .file-write-badge {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(59, 130, 246, 0.12);
  color: #2563eb;
  font-weight: 600;
  white-space: nowrap;
}

:root[data-theme="dark"] .content-blocks .tool-detail .file-write-badge {
  background: rgba(96, 165, 250, 0.15);
  color: #93c5fd;
}

.content-blocks .tool-detail .file-write-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  overflow-x: auto;
}

.content-blocks .tool-detail .file-write-line {
  white-space: pre;
  color: var(--text-primary);
}

.content-blocks .tool-detail .tool-json-body {
  white-space: pre;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  overflow-x: auto;
}

.content-blocks .tool-detail .tool-json-body code {
  font-family: inherit;
}

.content-blocks .tool-detail .bash-terminal-view {
  white-space: normal;
}

.content-blocks .tool-detail .bash-terminal-desc {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .bash-terminal-body {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  line-height: 1.5;
  background: var(--bg-tertiary);
  border-radius: 4px;
  padding: 6px 8px;
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .bash-prompt {
  color: #16a34a;
  font-weight: 700;
  margin-right: 4px;
}

:root[data-theme="dark"] .content-blocks .tool-detail .bash-prompt {
  color: #4ade80;
}

.content-blocks .tool-detail .bash-command {
  color: var(--text-primary);
}

/* ── AskUserQuestion card ── */
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="ask"] { --tool-accent: #fb923c; }
:root[data-theme="dark"] .content-blocks .chat-tool-call[data-category="permission"] { --tool-accent: #fbbf24; }

.content-blocks .tool-detail .ask-question-view {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.content-blocks .tool-detail .ask-question-empty {
  color: var(--text-muted, #999);
  font-style: italic;
  font-size: 11px;
}

.content-blocks .tool-detail .ask-question-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.content-blocks .tool-detail .ask-question-header {
  font-size: 12px;
  font-weight: 600;
  color: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-header {
  color: #fb923c;
}

.content-blocks .tool-detail .ask-question-text {
  font-size: 12px;
  color: var(--text-primary);
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-question-options {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.content-blocks .tool-detail .ask-question-option {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 6px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
  user-select: none;
  -webkit-user-select: none;
}

.content-blocks .tool-detail .ask-question-option:hover {
  background: color-mix(in srgb, #f97316 6%, var(--bg-secondary));
  border-color: color-mix(in srgb, #f97316 30%, var(--border-color));
}

.content-blocks .tool-detail .ask-question-option.selected {
  background: color-mix(in srgb, #f97316 10%, var(--bg-secondary));
  border-color: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-option.selected {
  background: color-mix(in srgb, #fb923c 12%, var(--bg-secondary));
  border-color: #fb923c;
}

.content-blocks .tool-detail .ask-option-indicator {
  flex-shrink: 0;
  font-size: 14px;
  line-height: 1.3;
  color: var(--text-muted, #999);
}

.content-blocks .tool-detail .ask-question-option.selected .ask-option-indicator {
  color: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-option.selected .ask-option-indicator {
  color: #fb923c;
}

.content-blocks .tool-detail .ask-option-content {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.content-blocks .tool-detail .ask-option-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-primary);
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-option-desc {
  font-size: 11px;
  color: var(--text-secondary);
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
}

.content-blocks .tool-detail .ask-question-submit {
  align-self: flex-end;
  padding: 5px 16px;
  border: none;
  border-radius: 6px;
  background: #f97316;
  color: white;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s, background 0.15s;
}

.content-blocks .tool-detail .ask-question-submit:hover:not(:disabled) {
  background: #ea580c;
}

.content-blocks .tool-detail .ask-question-submit:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-submit {
  background: #16a34a;
  cursor: default;
  opacity: 1;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-submit {
  background: #fb923c;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-submit:hover:not(:disabled) {
  background: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-submit {
  background: #22c55e;
}

.content-blocks .tool-detail .ask-question-supplementary {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.content-blocks .tool-detail .ask-supplementary-label {
  font-size: 11px;
  font-weight: 500;
  color: var(--text-secondary);
}

.content-blocks .tool-detail .ask-supplementary-input {
  width: 100%;
  padding: 5px 8px;
  border: 1px solid var(--border-color);
  border-radius: 6px;
  background: var(--bg-primary);
  color: var(--text-primary);
  font-size: 12px;
  line-height: 1.4;
  outline: none;
  transition: border-color 0.15s;
  box-sizing: border-box;
}

.content-blocks .tool-detail .ask-supplementary-input::placeholder {
  color: var(--text-muted, #999);
  font-size: 11px;
}

.content-blocks .tool-detail .ask-supplementary-input:focus {
  border-color: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-supplementary-input:focus {
  border-color: #fb923c;
}

/* ── Grep search view ── */
.content-blocks .tool-detail .grep-search-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .grep-pattern-row,
.content-blocks .tool-detail .grep-path-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.content-blocks .tool-detail .grep-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .content-blocks .tool-detail .grep-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .grep-pattern-text,
.content-blocks .tool-detail .grep-path-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.content-blocks .tool-detail .grep-tags-row,
.content-blocks .tool-detail .bash-tags-row,
.content-blocks .tool-detail .web-search-tags-row,
.content-blocks .tool-detail .web-fetch-tags-row,
.content-blocks .tool-detail .glob-tags-row {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 2px;
}

.content-blocks .tool-detail .grep-mode-tag {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.08);
  color: #8b5cf6;
  font-weight: 500;
}

:root[data-theme="dark"] .content-blocks .tool-detail .grep-mode-tag {
  background: rgba(167, 139, 250, 0.12);
  color: #a78bfa;
}

/* ── Glob pattern view ── */
.content-blocks .tool-detail .glob-pattern-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .glob-pattern-row,
.content-blocks .tool-detail .glob-path-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.content-blocks .tool-detail .glob-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .content-blocks .tool-detail .glob-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .glob-pattern-text,
.content-blocks .tool-detail .glob-path-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

/* ── WebSearch view ── */
.content-blocks .tool-detail .web-search-view {
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .web-search-query {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  color: var(--text-primary);
}

.content-blocks .tool-detail .web-search-icon {
  flex-shrink: 0;
  font-size: 12px;
  line-height: 1.4;
}

.content-blocks .tool-detail .web-search-text {
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── WebFetch view ── */
.content-blocks .tool-detail .web-fetch-view {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .web-fetch-url-row {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

.content-blocks .tool-detail .web-fetch-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(139, 92, 246, 0.12);
  color: #7c3aed;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
  line-height: 1.5;
}

:root[data-theme="dark"] .content-blocks .tool-detail .web-fetch-label {
  background: rgba(167, 139, 250, 0.15);
  color: #a78bfa;
}

.content-blocks .tool-detail .web-fetch-link {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  color: var(--accent-color);
  text-decoration: none;
  word-break: break-all;
}

.content-blocks .tool-detail .web-fetch-link:hover {
  text-decoration: underline;
}

.content-blocks .tool-detail .web-fetch-text {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-primary);
}

.content-blocks .tool-detail .web-fetch-prompt {
  color: var(--text-secondary);
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
}

/* ── Agent call view ── */
.content-blocks .tool-detail .agent-call-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .agent-call-header {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.content-blocks .tool-detail .agent-type-badge {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(236, 72, 153, 0.12);
  color: #db2777;
  font-weight: 600;
  white-space: nowrap;
}

:root[data-theme="dark"] .content-blocks .tool-detail .agent-type-badge {
  background: rgba(244, 114, 182, 0.15);
  color: #f472b6;
}

.content-blocks .tool-detail .agent-call-desc {
  color: var(--text-primary);
  font-weight: 500;
}

.content-blocks .tool-detail .agent-call-prompt {
  color: var(--text-secondary);
  font-size: 11px;
  white-space: normal;
  word-break: break-word;
  padding: 6px 8px;
  background: var(--bg-tertiary);
  border-radius: 4px;
  font-family: inherit;
  line-height: 1.6;
}
.content-blocks .tool-detail .agent-call-prompt p:first-child {
  margin-top: 0;
}
.content-blocks .tool-detail .agent-call-prompt p:last-child {
  margin-bottom: 0;
}
.content-blocks .tool-detail .agent-call-prompt h1,
.content-blocks .tool-detail .agent-call-prompt h2,
.content-blocks .tool-detail .agent-call-prompt h3,
.content-blocks .tool-detail .agent-call-prompt h4 {
  font-size: 12px;
  font-weight: 600;
  margin: 8px 0 4px;
  color: var(--text-primary);
}
.content-blocks .tool-detail .agent-call-prompt ul,
.content-blocks .tool-detail .agent-call-prompt ol {
  margin: 4px 0;
  padding-left: 20px;
}
.content-blocks .tool-detail .agent-call-prompt li {
  margin: 2px 0;
}
.content-blocks .tool-detail .agent-call-prompt code {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 10px;
  background: color-mix(in srgb, var(--text-secondary) 8%, transparent);
  padding: 1px 4px;
  border-radius: 3px;
}
.content-blocks .tool-detail .agent-call-prompt pre {
  margin: 4px 0;
  padding: 6px 8px;
  background: var(--bg-secondary);
  border-radius: 4px;
  overflow-x: auto;
}
.content-blocks .tool-detail .agent-call-prompt pre code {
  background: none;
  padding: 0;
  font-size: 11px;
}
.content-blocks .tool-detail .agent-call-prompt strong {
  font-weight: 600;
  color: var(--text-primary);
}
.content-blocks .tool-detail .agent-call-prompt hr {
  border: none;
  border-top: 1px solid var(--border-color);
  margin: 6px 0;
}

/* ── Skill call view ── */
.content-blocks .tool-detail .skill-call-view {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 11px;
  line-height: 1.5;
}

.content-blocks .tool-detail .skill-call-header {
  display: flex;
  align-items: center;
  gap: 6px;
}

.content-blocks .tool-detail .skill-call-icon {
  font-size: 12px;
  flex-shrink: 0;
}

.content-blocks .tool-detail .skill-call-name {
  font-weight: 600;
  color: #0891b2;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
}

:root[data-theme="dark"] .content-blocks .tool-detail .skill-call-name {
  color: #22d3ee;
}

.content-blocks .tool-detail .skill-call-args {
  color: var(--text-secondary);
  font-size: 11px;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 4px 8px;
  background: var(--bg-tertiary);
  border-radius: 4px;
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  line-height: 1.5;
}

/* ── PermissionApproval card ── */
.content-blocks .tool-detail .permission-approval-view {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.content-blocks .tool-detail .permission-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: #d97706;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-header {
  color: #fbbf24;
}

.content-blocks .tool-detail .permission-icon {
  font-size: 14px;
  flex-shrink: 0;
}

.content-blocks .tool-detail .permission-title {
  color: #d97706;
  font-weight: 600;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-title {
  color: #fbbf24;
}

.content-blocks .tool-detail .permission-tool-name {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-primary);
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
}

.content-blocks .tool-detail .permission-tool-detail {
  font-size: 11px;
  display: flex;
  align-items: baseline;
  gap: 6px;
}

.content-blocks .tool-detail .permission-detail-label {
  font-size: 9px;
  padding: 1px 4px;
  border-radius: 3px;
  background: rgba(234, 179, 8, 0.12);
  color: #b45309;
  font-weight: 600;
  white-space: nowrap;
  flex-shrink: 0;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-detail-label {
  background: rgba(251, 191, 36, 0.15);
  color: #fbbf24;
}

.content-blocks .tool-detail .permission-tool-detail code {
  font-family: 'SF Mono', 'Fira Code', Menlo, Monaco, monospace;
  font-size: 11px;
  color: var(--text-primary);
  word-break: break-all;
}

.content-blocks .tool-detail .permission-options {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.content-blocks .tool-detail .permission-btn {
  padding: 5px 14px;
  border: none;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s, background 0.15s;
}

.content-blocks .tool-detail .permission-btn-allow {
  background: #22c55e;
  color: white;
}

.content-blocks .tool-detail .permission-btn-allow:hover:not(:disabled) {
  background: #16a34a;
}

.content-blocks .tool-detail .permission-btn-reject {
  background: #ef4444;
  color: white;
}

.content-blocks .tool-detail .permission-btn-reject:hover:not(:disabled) {
  background: #dc2626;
}

.content-blocks .tool-detail .permission-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-btn-allow {
  background: #4ade80;
  color: #1a1a1a;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-btn-allow:hover:not(:disabled) {
  background: #22c55e;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-btn-reject {
  background: #f87171;
  color: #1a1a1a;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-btn-reject:hover:not(:disabled) {
  background: #ef4444;
}

.content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-allow {
  background: #22c55e;
  opacity: 1;
}

.content-blocks .tool-detail .permission-approval-view.permission-responded .permission-btn-reject {
  background: #ef4444;
  opacity: 1;
}

.content-blocks .tool-detail .permission-result {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 600;
  margin-top: 6px;
}

.content-blocks .tool-detail .permission-result-approved {
  background: #dcfce7;
  color: #166534;
}

.content-blocks .tool-detail .permission-result-denied {
  background: #fee2e2;
  color: #991b1b;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-result-approved {
  background: #166534;
  color: #dcfce7;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-result-denied {
  background: #991b1b;
  color: #fee2e2;
}

.content-blocks .tool-detail .permission-auto-approved .permission-header {
  opacity: 0.85;
}

.content-blocks .tool-detail .permission-result-auto-approved {
  display: inline-block;
  padding: 4px 12px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 500;
  background: #dcfce7;
  color: #15803d;
  border: 1px solid #bbf7d0;
}

:root[data-theme="dark"] .content-blocks .tool-detail .permission-result-auto-approved {
  background: #166534;
  color: #dcfce7;
  border-color: #15803d;
}
</style>
