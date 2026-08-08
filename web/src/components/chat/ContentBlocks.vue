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
    <!-- Summary mode: render structured cards (tools / scheduled tasks / ask-questions)
         directly from summaryCards — NOT by traversing blocks (which may be empty in
         summary-first loading). -->
    <template v-if="showingSummary && summary">
      <!-- Tool cards from summaryCards.tools (auto-expand tools render inline detail) -->
      <template v-for="(tool, ti) in summaryTools" :key="'sum-tool-' + ti">
        <div class="chat-tool-call done" :data-category="getToolIcon(tool.name).category" @click.stop="handleSummaryToolClick(tool, ti)">
          <component :is="getToolIcon(tool.name).icon" :size="12" class="tool-icon" />
          <span class="tool-name">{{ toolDisplayName(tool.name, tool.input, tool.display_name) }}</span>
          <CheckCircle2 :size="14" color="#22c55e" class="tool-check" />
        </div>
        <div v-if="shouldAutoExpandTool(tool.name || '')" class="tool-detail" :data-tool-name="tool.name" @click="handleToolDetailClick">
          <div v-html="formatToolInput(tool.input || {}, tool.name)"></div>
        </div>
      </template>
      <!-- Scheduled task cards from summaryCards.taskIDs (task data fetched in real time) -->
      <template v-for="(tid, ti) in summaryTaskIDs" :key="'sum-task-' + tid">
        <div class="scheduled-task-card" :class="{ deleted: summaryTaskData[tid]?.deleted }" @click="!summaryTaskData[tid]?.deleted && !summaryTaskData[tid]?.loading && summaryTaskData[tid]?.task && $emit('task-card-click', tid)">
          <div class="stask-header">
            <Archive v-if="summaryTaskData[tid]?.deleted" :size="14" class="stask-icon" />
            <Clock v-else :size="14" class="stask-icon" />
            <template v-if="summaryTaskData[tid]?.deleted">{{ t('chat.contentBlocks.taskDeleted') }}</template>
            <template v-else-if="summaryTaskData[tid]?.loading">{{ t('chat.contentBlocks.loading') }}</template>
            <template v-else>{{ summaryTaskData[tid]?.task?.name || t('chat.contentBlocks.scheduledTaskCreated') }}</template>
            <span v-if="!summaryTaskData[tid]?.deleted && !summaryTaskData[tid]?.loading && summaryTaskData[tid]?.task" class="stask-status-badge" :class="summaryTaskData[tid].task.status">{{ statusLabelSimple(summaryTaskData[tid].task) }}</span>
          </div>
          <div v-if="!summaryTaskData[tid]?.deleted && !summaryTaskData[tid]?.loading && summaryTaskData[tid]?.task" class="stask-body">
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.frequency') }}</strong>{{ humanizeCron(summaryTaskData[tid].task.cronExpr) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong><AgentIcon :backend="getAgentBackend(summaryTaskData[tid].task.agentId)" :name="getAgentName(summaryTaskData[tid].task.agentId)" :size="14" class="stask-agent-icon" /> {{ getAgentName(summaryTaskData[tid].task.agentId) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.repeat') }}</strong>{{ repeatLabel(summaryTaskData[tid].task.repeatMode, summaryTaskData[tid].task.maxRuns) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.status') }}</strong><span class="stask-status-dot" :class="statusClass(summaryTaskData[tid].task)"></span>{{ statusLabel(summaryTaskData[tid].task) }}</div>
            <div v-if="summaryTaskData[tid].task.lastRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.lastRun') }}</strong>{{ formatTime(summaryTaskData[tid].task.lastRunAt) }}</div>
            <div v-if="summaryTaskData[tid].task.nextRunAt" class="stask-row"><strong>{{ t('chat.contentBlocks.nextRun') }}</strong>{{ formatTime(summaryTaskData[tid].task.nextRunAt) }}</div>
          </div>
          <div class="stask-view-btn" v-if="!summaryTaskData[tid]?.deleted && !summaryTaskData[tid]?.loading && summaryTaskData[tid]?.task">
            {{ t('chat.contentBlocks.viewDetail') }}
            <ChevronRight :size="12" />
          </div>
        </div>
      </template>
      <!-- Ask-question cards from summaryCards.askQuestions (rendered via formatToolInput) -->
      <template v-if="summaryAskQuestions.length">
        <div class="chat-tool-call done" data-category="ask" @click.stop="$emit('toggle-tool', 'summary-ask')">
          <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
          <span class="tool-name">{{ t('tool.askUser.name') }}</span>
          <CheckCircle2 :size="14" color="#f59e0b" class="tool-warn" />
        </div>
        <div class="tool-detail" data-tool-name="AskUserQuestion" @click="handleToolDetailClick" v-html="formatToolInput({ questions: summaryAskQuestions }, 'AskUserQuestion')"></div>
      </template>
    </template>
    <!-- Original content mode -->
    <template v-if="!showingSummary || !summary">
    <template v-for="(block, bi) in blocks" :key="stableBlockKey(bi, block)">
      <!-- Thinking block: streaming or expanded shows inline content, collapsed shows clickable chip -->
      <div v-if="block.type === 'thinking'"
        :ref="(el) => setThinkingRef(stableBlockKey(bi, block), el)"
        class="chat-thinking"
        :class="{
          'thinking-streaming': isThinkingStreaming(block),
          'thinking-expanded-done': isThinkingExpandedDone(block, bi),
          'thinking-collapsed': isThinkingCollapsed(block, bi),
          'thinking-collapsing': !!collapsingThinking[stableBlockKey(bi, block)],
          'thinking-expanding': !!expandingThinking[stableBlockKey(bi, block)],
        }"
      >
        <div class="thinking-header" @click.stop="handleThinkingClick(block, bi)">
          <Brain :size="12" class="thinking-icon" />
          <span class="thinking-label">{{ t('chat.message.deepThinking') }}</span>
          <!-- Status indicators: right-aligned, same pattern as tool_use -->
          <span v-if="isThinkingStreaming(block)" class="thinking-spinner"></span>
          <!-- Cancelled marker: show inline in thinking header when this is the last block and message was cancelled.
               Prevents the cancelled mark from being visually hidden/trapped under the collapsed thinking chip. -->
          <span v-else-if="isLastBlock(bi) && cancelled" class="chat-cancelled-mark-inline">{{ t('chat.contentBlocks.cancelled') }}</span>
          <ChevronUp v-else-if="isThinkingExpandedDone(block, bi) || expandingThinking[stableBlockKey(bi, block)]" :size="12" class="thinking-chevron" />
          <ChevronDown v-else :size="12" class="thinking-chevron" />
        </div>
        <!-- Content wrapper: CSS grid 0fr/1fr transition for smooth expand/collapse -->
        <div class="thinking-content-wrapper"
          :class="{
            'thinking-content-open': isThinkingStreaming(block) || isThinkingExpandedDone(block, bi) || !!expandingThinking[stableBlockKey(bi, block)],
          }"
        >
          <div class="thinking-inline-content" v-html="getThinkingHtml(bi, block)"></div>
        </div>
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
            <Archive v-if="blockTasks[sKey].deleted" :size="14" class="stask-icon" />
            <Clock v-else :size="14" class="stask-icon" />
            <template v-if="blockTasks[sKey].deleted">{{ t('chat.contentBlocks.taskDeleted') }}</template>
            <template v-else-if="blockTasks[sKey].loading">{{ t('chat.contentBlocks.loading') }}</template>
            <template v-else>{{ blockTasks[sKey].task?.name || t('chat.contentBlocks.scheduledTaskCreated') }}</template>
            <span v-if="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task" class="stask-status-badge" :class="blockTasks[sKey].task.status">{{ statusLabelSimple(blockTasks[sKey].task) }}</span>
          </div>
          <div v-if="!blockTasks[sKey].deleted && !blockTasks[sKey].loading && blockTasks[sKey].task" class="stask-body">
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.frequency') }}</strong>{{ humanizeCron(blockTasks[sKey].task.cronExpr) }}</div>
            <div class="stask-row"><strong>{{ t('chat.contentBlocks.executor') }}</strong><AgentIcon :backend="getAgentBackend(blockTasks[sKey].task.agentId)" :name="getAgentName(blockTasks[sKey].task.agentId)" :size="14" class="stask-agent-icon" /> {{ getAgentName(blockTasks[sKey].task.agentId) }}</div>
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
      <!-- Ask question card (from <ask-question> XML tag in text) — must come before generic text block.
           detectAskQuestionInText triggers renderTextBlock which fills blockAskQuestions;
           the card UI only renders when blockAskQuestions[key] has data. -->
      <template v-else-if="block.type === 'text' && (blockAskQuestions[blockTaskKey(bi)] || detectAskQuestionInText(block))">
        <!-- Surrounding text (with ask-question tag stripped) -->
        <div v-if="getBlockHtml(bi, block)" v-html="getBlockHtml(bi, block)"></div>
        <template v-if="blockAskQuestions[blockTaskKey(bi)]">
          <div class="chat-tool-call done" data-category="ask" @click.stop="$emit('toggle-tool', key(bi))">
            <component :is="getToolIcon('AskUserQuestion').icon" :size="12" class="tool-icon" />
            <span class="tool-name">{{ t('tool.askUser.name') }}</span>
            <span class="tool-summary">{{ askQuestionSummary(blockAskQuestions[blockTaskKey(bi)]) }}</span>
            <CheckCircle2 :size="14" color="#f59e0b" class="tool-warn" />
          </div>
          <div v-if="expandedTools[key(bi)] || true" class="tool-detail" data-tool-name="AskUserQuestion" @click="handleToolDetailClick" v-html="formatToolInput(blockAskQuestions[blockTaskKey(bi)], 'AskUserQuestion')"></div>
        </template>
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

  </div>
</template>

<script setup>
import { ref, watch, onUnmounted, computed, onMounted, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import { handleToolAction, shouldAutoExpandTool } from '@/utils/renderToolDetail.ts'
import { getToolIcon, toolDisplayName } from '@/utils/icons'
import { Brain, ChevronRight, ChevronDown, ChevronUp, AlertCircle, AlertTriangle, XCircle, Clock, Archive } from 'lucide-vue-next'
import AgentIcon from '@/components/common/AgentIcon.vue'
import { renderMarkdownHtml } from '@/composables/useMarkdownRenderer.ts'
import { store } from '@/stores/app.ts'
import { apiGet } from '@/utils/api'
import { appLog } from '@/utils/appLog'
import { useThinkingContent } from '@/composables/useThinkingContent.ts'
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

const TAG = 'ContentBlocks'

const { t, locale } = useI18n()
const thinkingContent = useThinkingContent()

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
    let url = `/api/ai/chat/tool-call?tool_id=${encodeURIComponent(block.id)}&message_id=${encodeURIComponent(msgId)}`
    if (props.sessionId) url += `&session_id=${encodeURIComponent(props.sessionId)}`
    const resp = await fetch(url)
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
  streaming: { type: Boolean, default: false },
  cancelled: { type: Boolean, default: false },
  summary: { type: String, default: null },
  summaryCards: { type: Object, default: null },
  showingSummary: { type: Boolean, default: false },
  // Render functions
  renderTextBlock: { type: Function, required: true },
  formatToolInput: { type: Function, required: true },
  toolCallSummary: { type: Function, required: true },
  humanizeCron: { type: Function, default: () => '' },
  repeatLabel: { type: Function, default: () => '' },
  truncate: { type: Function, default: (s) => s },
  getAgentBackend: { type: Function, default: () => '' },
  getAgentName: { type: Function, default: () => '' },
  // Performance: static block cache from useChatRender (Problem 6)
  staticBlockCache: { type: Object, default: null },
  active: { type: Boolean, default: true },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'task-card-click', 'send-message', 'render-flush', 'resume-session'])

// Key helper: use msgId if available, otherwise msgIndex
function key(bi) {
  return blockKey(props.msgId, bi)
}

// Key for blockTasks/blockAskQuestions lookup — prefix format used in useChatRender.ts
function blockTaskKey(bi) {
  return blockTaskKeyUtil(props.msgId, bi)
}

// Quick check if block text contains <ask-question> tag — used in v-else-if condition
// to enter the ask-question branch (which triggers renderTextBlock to fill blockAskQuestions).
// The actual card UI is gated by blockAskQuestions[key] being truthy, so false positives
// from this simple check are harmless — they just trigger a renderTextBlock call that
// won't populate blockAskQuestions if the content isn't a real structured question.
function detectAskQuestionInText(block) {
  return block.text && block.text.includes('<ask-question')
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

// ── Summary-mode structured cards (rendered from summaryCards, no block traversal) ──
const summaryTools = computed(() => props.summaryCards?.tools || [])
const summaryTaskIDs = computed(() => props.summaryCards?.taskIDs || [])
const summaryAskQuestions = computed(() => props.summaryCards?.askQuestions || [])

if (import.meta.env.DEV) {
  watch(
    () => [props.msgId, props.showingSummary, props.summary, props.blocks?.length],
    () => {
      appLog.d('ContentBlocks', `[render] msgId=${props.msgId} showSum=${props.showingSummary} summaryLen=${String(props.summary || '').length} summaryHead="${String(props.summary || '').slice(0, 30)}" blocks=${props.blocks?.length ?? 0}`)
    },
    { immediate: true },
  )
}

// Local map of scheduled-task data keyed by task id, fetched in real time when the
// summary is visible with taskIDs. Unlike blockTasks (which scans text blocks), summary
// mode has empty blocks, so we fetch directly from the /api/tasks list API.
const summaryTaskData = reactive({})

async function fetchSummaryTaskData() {
  const ids = summaryTaskIDs.value
  if (!ids || ids.length === 0) return
  const pending = ids.filter((id) => !summaryTaskData[id]?.task && !summaryTaskData[id]?.loading && !summaryTaskData[id]?.deleted)
  if (pending.length === 0) return
  for (const id of pending) {
    summaryTaskData[id] = { taskId: id, task: null, loading: true, deleted: false }
  }
  try {
    const data = await apiGet('/api/tasks')
    const taskMap = new Map((data.tasks || []).map((t) => [t.id, t]))
    for (const id of pending) {
      const entry = summaryTaskData[id]
      entry.loading = false
      const task = taskMap.get(id)
      if (task) entry.task = task
      else entry.deleted = true
    }
  } catch (error) {
    appLog.e(TAG, '[ContentBlocks] fetchSummaryTaskData failed', error)
    for (const id of pending) summaryTaskData[id].loading = false
  }
}

watch(summaryTaskIDs, fetchSummaryTaskData, { immediate: true })

// Keep summary task cards in sync with store.state.tasks (global polling) so status
// changes refresh in real time. Deletion is owned by fetchSummaryTaskData (which uses
// the authoritative /api/tasks list); the store watch only syncs status. It must NOT
// mark tasks deleted when the store list is empty — an empty list can be an app reset
// or a not-yet-populated store, not an actual deletion.
watch(
  () => store.state.tasks,
  (tasks) => {
    const ids = summaryTaskIDs.value
    if (!ids || ids.length === 0) return
    const taskMap = new Map((tasks || []).map((t) => [t.id, t]))
    for (const id of ids) {
      const entry = summaryTaskData[id]
      if (!entry || entry.deleted) continue
      const updated = taskMap.get(id)
      if (updated) {
        entry.task = updated
        entry.loading = false
      }
    }
  },
  { deep: true },
)

function handleSummaryToolClick(tool, ti) {
  const name = tool.name || ''
  if (shouldAutoExpandTool(name)) {
    emit('toggle-tool', `summary-tool-${ti}`)
    return
  }
  emit('show-tool-detail', {
    name,
    input: tool.input,
    output: tool.output,
    status: 'success',
    done: true,
    display_name: tool.display_name,
    tool_id: tool.id,
    msgId: props.msgId,
    blockIdx: ti,
  })
}

/** Generate a stable key for a block, used for v-for :key and animation state.
 *  tool_use: block.id (unique tool call ID from backend)
 *  thinking: block.think_id (stable backend-assigned ID, survives re-opens),
 *            falling back to block._key (key assigned at creation/parsing)
 *  text: text-${bi} (text blocks merge so index is stable)
 *  other: type-bi (fallback) */
function stableBlockKey(bi, block) {
  if (block.type === 'tool_use' && block.id) return block.id
  if (block.type === 'thinking') {
    if (block.think_id) return block.think_id
    if (block._key) return block._key
  }
  return `${block.type || 'other'}-${bi}`
}

function handleThinkingClick(block, bi) {
  const blockKey = stableBlockKey(bi, block)
  if (isThinkingCollapsed(block, bi)) {
    // Expand inline with animation
    expandingThinking.value[blockKey] = true
    thinkingExpanded.value[blockKey] = true
    blockHtmlCache.value = {}
    // Slim block (think_id, no text): lazy-load the thinking text on expand
    if (!block.text && block.think_id) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId)
        .catch(() => { /* error surfaced via errors ref */ })
    }
    // Clean up expanding state after animation
    const t = setTimeout(() => {
      delete expandingThinking.value[blockKey]
    }, EXPAND_TRANSITION_MS)
    _collapseTimers.push(t)
  } else if (isThinkingExpandedDone(block, bi)) {
    // Retry failed lazy-load when clicking an error-state slim block;
    // otherwise collapse.
    if (!block.text && block.think_id && thinkingContent.errors.value[block.think_id]) {
      thinkingContent.loadThinking(block.think_id, props.msgId, props.sessionId)
        .catch(() => { /* error surfaced via errors ref */ })
    } else {
      triggerThinkingCollapse(blockKey)
    }
  }
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

/** Whether a thinking block is done (not streaming) but still expanded with inline content visible. */
function isThinkingExpandedDone(block, bi) {
  if (isThinkingStreaming(block)) return false
  if (collapsingThinking.value[stableBlockKey(bi, block)]) return false
  return !!thinkingExpanded.value[stableBlockKey(bi, block)]
}

/** Whether a thinking block is collapsed to a chip (done, not streaming, not expanded, not collapsing). */
function isThinkingCollapsed(block, bi) {
  if (isThinkingStreaming(block)) return false
  if (collapsingThinking.value[stableBlockKey(bi, block)]) return false
  if (thinkingExpanded.value[stableBlockKey(bi, block)]) return false
  return !props.streaming || block.done // block is done
}

/** Whether the given block index is the last block in the blocks array. */
function isLastBlock(bi) {
  return bi === (props.blocks?.length || 0) - 1
}

// ── Thinking block collapse/expand animation state ──
const collapsingThinking = ref({})   // { [blockKey]: true } for blocks mid-collapse
const expandingThinking = ref({})    // { [blockKey]: true } for blocks mid-expand
const thinkingExpanded = ref({})     // { [blockKey]: true } — completed blocks that are still expanded (only collapses on manual click)
let _collapseElKeys = new Set()       // blockKeys of thinking blocks tracked during streaming
let _collapseTimers = []             // setTimeout IDs for collapse animation (cleaned up on unmount)

// Animation constants
const EXPAND_TRANSITION_MS = 300     // ms — expand animation duration
const COLLAPSE_TRANSITION_MS = 350   // ms — collapse animation duration

/** Trigger the collapse animation for a completed thinking block. */
function triggerThinkingCollapse(blockKey) {
  // Mark as collapsing — this removes thinking-content-open from the wrapper,
  // triggering the CSS grid 0fr transition. We also clear thinkingExpanded so
  // the wrapper transitions from 1fr→0fr immediately.
  collapsingThinking.value[blockKey] = true
  delete thinkingExpanded.value[blockKey]
  blockHtmlCache.value = {}
  // After transition completes, clean up collapsing state
  const t = setTimeout(() => {
    delete collapsingThinking.value[blockKey]
  }, COLLAPSE_TRANSITION_MS)
  _collapseTimers.push(t)
}

/** Track thinking block keys during streaming for collapse animation. */
function setThinkingRef(key, el) {
  if (el) {
    _collapseElKeys.add(key)
  } else {
    _collapseElKeys.delete(key)
  }
}

/** Click inside expanded tool-detail: dispatch to tool action handlers first, then fall through to generic behavior. */
function handleToolDetailClick(event) {
  // Try tool-specific action handler first (via data-tool-name on the .tool-detail container)
  const toolName = event.currentTarget.dataset?.toolName
  if (toolName && handleToolAction(toolName, event, emit)) return
  // Allow file-open buttons, file-path spans, commit-hash elements, and table rows to bubble
  if (event.target.closest('.chat-file-open-btn') || event.target.closest('.chat-file-path') || event.target.closest('.chat-commit-hash, .chat-commit-open-btn') || event.target.closest('.chat-worktree-btn') || event.target.closest('tbody tr[data-row-idx]')) {
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
      // Thinking blocks use renderMarkdownHtml during streaming
      newCache[`t-${key}`] = renderMarkdownHtml(block.text)
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

/** Get HTML for thinking block content. Live blocks render text inline;
 *  slim blocks (think_id) render from the lazy-load cache/loading/error state. */
function getThinkingHtml(bi, block) {
  if (block.text) {
    return getThinkingTextHtml(block.text, bi, block)
  }
  if (block.think_id) {
    const text = thinkingContent.cachedText(block.think_id)
    if (text) return renderMarkdownHtml(text)
    if (thinkingContent.errors.value[block.think_id]) {
      return `<div class="thinking-load-error"><span>${t('chat.contentBlocks.thinkingLoadFailed')}</span><button class="thinking-retry-btn" onclick="this.closest('.chat-thinking').querySelector('.thinking-header').click()">${t('chat.contentBlocks.retry')}</button></div>`
    }
    return '<div class="placeholder-dots"><span></span><span></span><span></span></div>'
  }
  return ''
}

/** Existing throttled streaming/inline render path (unchanged behavior). */
function getThinkingTextHtml(text, bi, block) {
  if (!props.streaming || !props.active) {
    return renderMarkdownHtml(text)
  }
  const cacheKey = `t-${stableBlockKey(bi, block)}`
  // Streaming: deferred rendering with throttling (same pattern as text blocks)
  if (blockHtmlCache.value[cacheKey] !== undefined) {
    if (!_throttleTimer) {
      const newCache = { ...blockHtmlCache.value }
      newCache[cacheKey] = renderMarkdownHtml(text)
      blockHtmlCache.value = newCache
      _throttleTimer = setTimeout(flushBlockHtml, THROTTLE_MS)
    } else {
      _throttlePending = true
    }
    return blockHtmlCache.value[cacheKey]
  }
  const html = renderMarkdownHtml(text)
  blockHtmlCache.value = { ...blockHtmlCache.value, [cacheKey]: html }
  return html
}

watch(() => props.streaming, (streaming, wasStreaming) => {
  if (wasStreaming && !streaming) {
    if (_throttleTimer) { clearTimeout(_throttleTimer); _throttleTimer = null }
    _throttlePending = false
    // Collapse all completed thinking blocks when message ends
    for (const blockKey of _collapseElKeys) {
      delete thinkingExpanded.value[blockKey]
      delete expandingThinking.value[blockKey]
      delete collapsingThinking.value[blockKey]
    }
    // Clear throttle cache and force a full re-render of thinking HTML
    blockHtmlCache.value = {}
  }
})

// Watch for thinking blocks that become "done" mid-stream (via thinking_done SSE event).
// Only the block currently being streamed stays expanded — when its output
// completes it collapses immediately. Blocks the user manually expanded are kept open.
let _prevDoneKeys = new Set()
watch(() => props.blocks.filter(b => b.type === 'thinking' && b.done).map(b => stableBlockKey(props.blocks.indexOf(b), b)), (doneKeys) => {
  if (!props.streaming) {
    // Not streaming: nothing to collapse live; remember the done set for later.
    _prevDoneKeys = new Set(doneKeys)
    return
  }
  const doneSet = new Set(doneKeys)
  for (const key of doneKeys) {
    // Collapse only the blocks that JUST finished streaming (newly done),
    // skipping ones already collapsed/collapsing or manually expanded.
    if (_prevDoneKeys.has(key)) continue
    if (thinkingExpanded.value[key] || collapsingThinking.value[key]) continue
    triggerThinkingCollapse(key)
  }
  _prevDoneKeys = doneSet
  // Clear throttle cache so DOM re-renders with complete thinking content
  blockHtmlCache.value = {}
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

/* Slim thinking block lazy-load error state */
.thinking-load-error {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
  color: #dc2626;
}

.thinking-retry-btn {
  border: 1px solid color-mix(in srgb, #ef4444 40%, var(--border-color));
  background: transparent;
  color: #dc2626;
  font-size: 11px;
  padding: 1px 8px;
  border-radius: 4px;
  cursor: pointer;
}

.thinking-retry-btn:hover {
  background: rgba(239, 68, 68, 0.08);
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

/* Thinking block — callout style distinct from tool calls */
.chat-thinking {
  --thinking-accent: #8b5cf6;
  --thinking-transition: 300ms ease;
  background: color-mix(in srgb, var(--thinking-accent) 4%, transparent);
  border: none;
  border-left: 3px solid color-mix(in srgb, var(--thinking-accent) 50%, transparent);
  border-radius: 0 6px 6px 0;
  margin: 6px 0;
  width: 100%;
}

:root[data-theme="dark"] .chat-thinking {
  --thinking-accent: #a78bfa;
}

/* Collapsed state: pill-shaped clickable chip */
.chat-thinking.thinking-collapsed {
  border-radius: 12px;
  border-left: none;
  border: 1px solid color-mix(in srgb, var(--thinking-accent) 20%, var(--border-color));
  background: color-mix(in srgb, var(--thinking-accent) 6%, var(--bg-secondary));
}

.chat-thinking.thinking-collapsed .thinking-header {
  cursor: pointer;
}

.chat-thinking.thinking-collapsed:hover {
  background: color-mix(in srgb, var(--thinking-accent) 12%, var(--bg-secondary));
  border-color: color-mix(in srgb, var(--thinking-accent) 35%, var(--border-color));
}

/* Expanded-done state: callout style, header is clickable to collapse */
.chat-thinking.thinking-expanded-done .thinking-header {
  cursor: pointer;
}

.chat-thinking.thinking-expanded-done:hover {
  background: color-mix(in srgb, var(--thinking-accent) 7%, transparent);
  border-left-color: color-mix(in srgb, var(--thinking-accent) 65%, transparent);
}

/* Streaming state: callout style */
.chat-thinking.thinking-streaming {
  /* no extra rules needed — base callout style applies */
}

/* Collapse animation state: transitioning border from callout to pill */
.chat-thinking.thinking-collapsing {
  border-radius: 12px;
  border-left: none;
  border: 1px solid color-mix(in srgb, var(--thinking-accent) 20%, var(--border-color));
  background: color-mix(in srgb, var(--thinking-accent) 6%, var(--bg-secondary));
}

.chat-thinking.thinking-collapsing .thinking-header {
  cursor: pointer;
}

/* Expand animation state: transitioning border from pill to callout */
.chat-thinking.thinking-expanding {
  /* Uses base callout style — border transition handled by content wrapper */
}

/* Content wrapper: CSS grid 0fr↔1fr transition for buttery smooth expand/collapse */
.thinking-content-wrapper {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  transition: grid-template-rows var(--thinking-transition), opacity 200ms ease, padding 200ms ease;
}

.thinking-content-wrapper.thinking-content-open {
  grid-template-rows: 1fr;
  opacity: 1;
  padding: 0 10px 3px;
}

.thinking-inline-content {
  overflow: hidden;
  min-height: 0;
  font-size: 12px;
  line-height: 1.65;
  color: var(--text-secondary);
  word-break: break-word;
}

.thinking-header {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
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
  letter-spacing: 0.02em;
}

.thinking-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid color-mix(in srgb, var(--thinking-accent) 20%, var(--border-color));
  border-top-color: var(--thinking-accent);
  border-radius: 50%;
  animation: tool-spin 0.6s linear infinite;
  flex-shrink: 0;
  margin-left: auto;
}

.thinking-chevron {
  flex-shrink: 0;
  margin-left: auto;
  color: var(--text-tertiary, #999);
  transition: color 0.15s;
}

.chat-thinking.thinking-expanded-done:hover .thinking-chevron,
.chat-thinking.thinking-collapsed:hover .thinking-chevron {
  color: var(--thinking-accent);
}

/* Markdown styles inside thinking inline content */
.thinking-inline-content p { margin: 0 0 0.5em; }
.thinking-inline-content p:last-child { margin-bottom: 0; }
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
  border-radius: 999px;
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
  overflow-x: clip;
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
  border-radius: 0;
  background: color-mix(in srgb, var(--accent-color, #4a90d9) 6%, var(--bg-primary, #fff));
  cursor: pointer;
  transition: box-shadow 0.15s, border-color 0.15s;
}

.scheduled-task-card:hover {
  border-color: color-mix(in srgb, var(--accent-color, #4a90d9) 50%, var(--border-color, #dee2e6));
  box-shadow: 0 2px 8px color-mix(in srgb, var(--accent-color, #4a90d9) 15%, transparent);
}

.scheduled-task-card.deleted {
  opacity: 0.5;
  border-color: var(--border-color, #dee2e6);
  background: var(--bg-secondary);
  cursor: default;
  box-shadow: none;
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
  flex-shrink: 0;
  margin-right: 2px;
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

.stask-agent-icon {
  vertical-align: middle;
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

.content-blocks .tool-detail .tool-output-content pre {
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
  user-select: none;
  -webkit-user-select: none;
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

.content-blocks .tool-detail .ask-question-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.content-blocks .tool-detail .ask-question-recommend {
  padding: 5px 16px;
  border: 1px solid #8b5cf6;
  border-radius: 6px;
  background: transparent;
  color: #8b5cf6;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}

.content-blocks .tool-detail .ask-question-recommend:hover {
  background: color-mix(in srgb, #8b5cf6 10%, var(--bg-secondary));
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-recommend {
  border-color: #a78bfa;
  color: #a78bfa;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-recommend:hover {
  background: color-mix(in srgb, #a78bfa 12%, var(--bg-secondary));
}

.content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-recommend {
  background: #8b5cf6;
  color: white;
  border-color: #8b5cf6;
  cursor: default;
  opacity: 1;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-recommend {
  background: #a78bfa;
  border-color: #a78bfa;
}

.content-blocks .tool-detail .ask-question-submit {
  align-self: flex-end;
  padding: 5px 16px;
  border: 1px solid #f97316;
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
  border-color: #16a34a;
  cursor: default;
  opacity: 1;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-submit {
  background: #fb923c;
  border-color: #fb923c;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-submit:hover:not(:disabled) {
  background: #f97316;
}

:root[data-theme="dark"] .content-blocks .tool-detail .ask-question-view.ask-submitted .ask-question-submit {
  background: #22c55e;
  border-color: #22c55e;
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
