<template>
  <div class="chat-input-wrapper" ref="rootRef">
    <!-- Top action bar (above input box) -->
    <div class="chat-top-actions">
      <div class="chat-action-group">
        <span class="chat-group-label" :title="t('chat.actions.session')">
          {{ t('chat.actions.session') }}
        </span>
        <button v-if="!sessionPanelOpen" class="chat-action-btn" data-action="session"
          :class="{ 'has-unread': chatUnreadCount > 0, 'has-running': chatRunning }"
          @click="$emit('open-session-tab', 'sessions')"
          :title="t('chat.actions.session')">
          <List :size="14" />
        </button>
        <button class="chat-action-btn"
          @click="handleCreateClick"
          @contextmenu.prevent="emit('create-session')"
          :title="t('chat.create.selectAgentOrLongPress')">
          <Plus :size="14" />
        </button>
        <button class="chat-action-btn"
          @click="$emit('open-session-search')"
          :title="t('chat.actions.sessionSearch')">
          <Search :size="14" />
        </button>
        <button class="chat-action-btn"
          @click="$emit('open-user-msg-index')"
          :title="t('chat.actions.userMsgIndex')">
          <MessagesSquare :size="14" />
        </button>
        <button
          v-if="isACPTransport"
          class="chat-action-btn acp-sync-btn"
          :class="{ disabled: acpSyncDisabled }"
          :disabled="acpSyncDisabled"
          @click="!acpSyncDisabled && $emit('sync-acp-session')"
          :title="acpSyncTitle"
          :aria-label="t('chat.actions.acpSync')"
        >
          <LoadingIndicator v-if="props.acpSyncing" size="sm" inline />
          <ArrowRightLeft v-else :size="14" :stroke-width="1.5" />
        </button>
        <button class="chat-action-btn chat-action-btn-archive" :class="{ disabled: !currentSessionId }"
          @click="handleArchive"
          :title="currentSessionId ? t('chat.actions.archiveCurrentSession') : t('chat.actions.noSessionToArchive')">
          <Archive :size="14" />
        </button>
      </div>
      <button class="chat-action-btn auto-speech-btn" :class="{ active: autoSpeechEnabled }"
        @click="$emit('toggle-auto-speech')"
        :title="t('chat.actions.autoSpeech')">
        <Volume2 :size="14" />
        <span class="chat-action-label">{{ t('chat.actions.autoSpeech') }}</span>
      </button>
    </div>
    <!-- Conversation recommendation banner (推荐回复) — sits above the input box so it never steals input space -->
    <Transition name="paste-fade">
      <div v-if="showRecommendationChip && recommendation" class="recommendation-chip">
        <Sparkles :size="14" :stroke-width="1.5" class="recommendation-icon" />
        <span class="recommendation-text" :class="{ expanded: recommendationExpanded }" @click="toggleRecommendationExpand" :title="recommendationExpanded ? t('chat.recommendationCollapse') : t('chat.recommendationExpand')">{{ recommendation }}</span>
        <button class="recommendation-accept" @click.stop="acceptRecommendation" :title="t('tool.askUser.recommendationFill')">{{ t('tool.askUser.recommendationFill') }}</button>
      </div>
    </Transition>
    <!-- Input container -->
    <div class="chat-input-container">
      <!-- Paste overlay (dynamic feedback while uploading pasted files from clipboard) -->
      <Transition name="paste-fade">
        <div v-if="isPasteOver" class="paste-overlay">
          <Loader2 :size="18" class="paste-spinner-icon" />
          <span>{{ t('chat.attach.uploading') }}</span>
        </div>
      </Transition>
      <!-- Attachment tags (horizontal scrollable cards — quote + pending uploads + attached file refs) -->
      <div v-if="quoteItems.length > 0 || attachedFiles.length > 0 || pendingFiles.length > 0" class="chat-attachment-tags">
        <!-- Staged quote cards (same size as file cards, accent-colored) -->
        <span v-for="(quote, quoteIndex) in quoteItems" :key="quote.id || quoteIndex" class="chat-file-attachment attachment-quote" :title="quote.note || quote.filePath" @click="$emit('quote-click', quote)">
          <Code2 :size="14" :stroke-width="1.5" class="attachment-quote-icon" />
          <span class="attachment-filename">{{ quoteFileName(quote) }}{{ quoteLineRange(quote) }}</span>
          <button class="attachment-close-btn" @click.stop="$emit('remove-quote', quote.id)" :title="t('common.remove')">×</button>
        </span>
        <!-- Attached file reference cards (shared component, includes pending uploads with local Blob preview) -->
        <AttachmentTags :files="attachedFiles" :pending-files="pendingFiles" @file-click="$emit('file-tag-click', $event)" @remove="handleRemoveAttached" @remove-pending="removeFile" />
      </div>
      <!-- Input row: attach + clear + textarea + stop + send -->
      <div class="chat-input-row">
        <div class="attach-menu-wrapper" ref="attachMenuRef">
          <button v-if="voiceState === 'recording' || voiceState === 'transcribing'" class="chat-attach-btn voice-rec-btn" :class="{ recording: voiceState === 'recording', transcribing: voiceState === 'transcribing' }" disabled :title="voiceState === 'recording' ? t('chat.voice.recording') : t('chat.voice.transcribing')">
            <span v-if="voiceState === 'recording'" class="voice-wave" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i></span>
            <Loader2 v-else :size="14" class="spin-icon" />
          </button>
          <button v-else class="chat-attach-btn" @click.stop="toggleAttachMenu" :disabled="inputDisabled" :title="t('chat.actions.attachment')">
            <Paperclip :size="16" />
          </button>
        </div>
        <button v-if="inputText" class="chat-clear-btn" @click="inputText = ''" :title="t('chat.input.clearInput')">
          <XCircle :size="16" />
        </button>
        <textarea class="chat-textarea"
          ref="textareaRef"
          v-model="inputText"
          :disabled="inputDisabled"
          :placeholder="dynamicPlaceholder"
          rows="1"
          @keydown="onTextareaKeydown"
          @paste="onPaste"
          @focus="onTextareaFocus"
          @blur="onTextareaBlur"
          ></textarea>
        <button v-if="!stopPrimed" class="chat-send-btn" ref="sendBtnRef" :class="{ queued: loading, shortcut: !hasInputContent }" @click.stop="handleSendClick" @pointerdown="onSendPointerDown" @pointerup="onSendPointerUp" @pointerleave="onSendPointerUp" :title="!hasInputContent ? t('chat.input.quickMenu') : loading ? t('chat.input.enqueue') : t('chat.input.send')">
          <!-- Empty input: green lightning (quick-menu shortcut) -->
          <Zap v-if="!hasInputContent" :size="16" />
          <!-- Queue mode: inbox with down arrow (enqueue) -->
          <Inbox v-else-if="loading" :size="16" />
          <!-- Normal mode: paper plane (send) -->
          <Send v-else :size="16" />
        </button>
        <button v-if="loading" class="chat-stop-btn" :class="{ primed: stopPrimed, cancelling: cancelling }" @click="handleStopClick" :title="stopPrimed ? t('chat.input.confirmStop') : t('chat.input.stopGenerating')" :disabled="cancelling">
          <Loader2 v-if="cancelling" class="spin-icon" :size="16" />
          <Square v-else :size="16" fill="currentColor" />
        </button>
      </div>
      <!-- Attach drawer (BottomSheet) -->
      <AttachDrawer
        ref="attachDrawerRef"
        :open="attachDrawer.effectiveOpen.value"
        :current-file="currentFile?.path"
        :current-dir="currentDir"
        :attached-files="attachedFiles"
        :recent-referenced-files="recentReferencedFiles"
        @close="attachDrawer.close()"
        @add-attached="handleAttachFile"
        @remove-attached="handleRemoveAttached"
        @file-open="(path) => emit('file-tag-click', path)"
      />
      <!-- Teleported quick-send menu -->
      <PopupMenu v-model:show="showQuickMenu" :target-element="sendBtnRef" :max-width="260" :max-height="280" :menu-items-count="quickSendItems.length + 1">
        <div class="quick-send-title">{{ t('chat.quickSend.title') }}</div>
        <button v-for="item in quickSendItems" :key="item.id"
          class="quick-send-item"
          :class="{ 'qs-pressing': quickSendPressingId === item.id }"
          @click="handleQuickSendClick(item)"
          @touchstart="onQuickSendTouchStart(item, $event)"
          @touchmove="onQuickSendTouchMove"
          @touchend="onQuickSendTouchEnd"
          @touchcancel="onQuickSendTouchEnd"
          @contextmenu.prevent
        >
          {{ item.label }}
          <div v-if="quickSendPressingId === item.id" class="qs-fill-bar" />
        </button>
        <div class="quick-send-divider" />
        <button class="quick-send-item" @click="showQuickMenu = false; quickSendDrawer.open()">
          ⚙️ {{ t('chat.quickSend.edit') }}
        </button>
      </PopupMenu>
      <!-- Session settings drawer -->
      <SessionDrawer
        :open="settingsDrawer.effectiveOpen.value"
        :agent-id="currentAgentId"
        :initial-tab="settingsDrawerInitialTab"
        @close="settingsDrawer.close()"
        @switch-model="handleSwitchModel"
        @switch-thinking-effort="handleSwitchThinkingEffort"
        @switch-mode="handleSwitchMode"
        @switch-transport="handleSwitchTransport"
      />
      <QuickSendDrawer :open="quickSendDrawer.effectiveOpen.value" @close="quickSendDrawer.close()" />
      <!-- @ command autocomplete menu (ClawBench built-in) -->
      <PopupMenu v-model:show="showAtMenu" :target-element="textareaRef" anchor="left" :max-width="260" :max-height="200" :menu-items-count="atMenuItems.length">
        <div class="at-menu-title">{{ t('chat.atCommand.title') }}</div>
        <button v-for="(cmd, idx) in atMenuItems" :key="cmd.key" class="at-menu-item" :class="{ 'at-menu-selected': idx === atMenuIndex }" :data-at-idx="idx" @mousedown.prevent="handleAtSelect(cmd)">
          <span class="at-menu-label" v-html="highlightText(cmd.label, cmd.query)" />
          <span class="at-menu-desc">{{ cmd.description }}</span>
        </button>
      </PopupMenu>
      <!-- Slash command autocomplete menu (ACP backend commands — only in acp-stdio transport) -->
      <PopupMenu v-if="isACPTransport && availableCommands.length > 0" v-model:show="showSlashMenu" :target-element="textareaRef" anchor="left" :max-width="300" :max-height="240" :menu-items-count="slashMenuItems.length">
        <div class="at-menu-title">{{ t('chat.slashCommand.title') }}</div>
        <button v-for="(cmd, idx) in slashMenuItems" :key="cmd.key" class="at-menu-item" :class="{ 'at-menu-selected': idx === slashMenuIndex }" :data-slash-idx="idx" @mousedown.prevent="handleSlashSelect(cmd)">
          <span class="at-menu-label slash-label" v-html="highlightText(cmd.label, cmd.query)" />
          <span class="at-menu-desc">{{ cmd.description }}</span>
        </button>
      </PopupMenu>
      <!-- Context usage detail popup -->
      <PopupMenu v-if="showUsageInfo" v-model:show="showUsagePopup" :target-element="usageElRef" :max-width="220" :max-height="320" :menu-items-count="10">
        <div class="usage-popup">
          <div class="usage-popup-header">
            <Activity :size="14" />
            <span>{{ t('chat.sessionInfo.contextUsage') }}</span>
          </div>
          <div class="usage-popup-bar">
            <div class="usage-popup-bar-track">
              <div class="usage-popup-bar-fill" :style="{ width: Math.min(usagePct, 100) + '%', background: usageColor }"></div>
            </div>
            <span class="usage-popup-pct" :style="{ color: usageColor }">{{ usagePct }}%</span>
          </div>
          <div class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.used') }}</span>
            <span class="usage-popup-value">{{ contextUsed.toLocaleString() }}</span>
          </div>
          <div class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.size') }}</span>
            <span class="usage-popup-value">{{ contextSize.toLocaleString() }}</span>
          </div>
          <div class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.remaining') }}</span>
            <span class="usage-popup-value">{{ Math.max(contextSize - contextUsed, 0).toLocaleString() }}</span>
          </div>
          <div v-if="contextInputTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.inputTokens') }}</span>
            <span class="usage-popup-value">{{ contextInputTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextOutputTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.outputTokens') }}</span>
            <span class="usage-popup-value">{{ contextOutputTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextTotalTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.totalTokens') }}</span>
            <span class="usage-popup-value">{{ contextTotalTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCachedReadTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cachedReadTokens') }}</span>
            <span class="usage-popup-value">{{ contextCachedReadTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCachedWriteTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cachedWriteTokens') }}</span>
            <span class="usage-popup-value">{{ contextCachedWriteTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextThoughtTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.thoughtTokens') }}</span>
            <span class="usage-popup-value">{{ contextThoughtTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCost > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.contextCost') }}</span>
            <span class="usage-popup-value">${{ contextCost.toFixed(2) }} {{ contextCurrency || 'USD' }}</span>
          </div>
        </div>
      </PopupMenu>
    </div>
    <!-- Session info bar (model + mode) -->
    <div class="chat-session-info" v-if="currentModelName || showModeInfo || showUsageInfo">
      <span class="session-info-model" @click.stop="openSettingsDrawer('model')"><ProviderIcon :model-name="currentModelName || ''" :size="11" />{{ currentModelName }}</span>
      <template v-if="showModeInfo">
        <span class="session-info-divider"></span>
        <span class="session-info-mode" :class="{ 'session-info-mode-auto': autoApprove }" @click.stop="onModeClick" v-long-press="onModeLongPress" @mousedown.stop="onModeMouseDown" @mouseup.stop="onModeMouseUp"><Compass :size="11" />{{ currentModeName }}</span>
      </template>
      <template v-if="showUsageInfo">
        <span class="session-info-divider"></span>
        <span ref="usageElRef" class="session-info-usage" @click.stop="showUsagePopup = !showUsagePopup">
          <Activity :size="11" />
          <span class="usage-bar">
            <span class="usage-bar-fill" :style="{ width: Math.min(usagePct, 100) + '%', background: usageColor }"></span>
          </span>
        </span>
      </template>
      <template v-if="showCompactBtn">
        <span class="session-info-divider"></span>
        <button class="session-info-compact" :disabled="inputDisabled" @click.stop="handleCompact" :title="t('chat.sessionInfo.compact')" :aria-label="t('chat.sessionInfo.compact')">
          <Minimize2 :size="11" />
          {{ t('chat.sessionInfo.compact') }}
        </button>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, watch, onBeforeUnmount, onMounted, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { Code2, List, Plus, Search, Archive, Volume2, Paperclip, XCircle, Inbox, Send, Square, Zap, Loader2, Compass, Activity, MessagesSquare, Minimize2, Sparkles, ArrowRightLeft } from 'lucide-vue-next'
import { highlightText } from '@/utils/searchUtils.ts'
import { computeRecentReferencedFiles } from '@/utils/chatInputUtils.ts'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import AttachDrawer from '@/components/chat/AttachDrawer.vue'
import AttachmentTags from '@/components/chat/AttachmentTags.vue'
import { useTabDrawer } from '@/composables/useTabDrawer'
import AsyncComponentLoader from '@/components/common/AsyncComponentLoader.vue'
const QuickSendDrawer = defineAsyncComponent({ loader: () => import('@/components/chat/QuickSendDrawer.vue'), loadingComponent: AsyncComponentLoader })
import SessionDrawer from '@/components/chat/SessionDrawer.vue'
import { createStopButtonMachine } from '@/utils/stopButtonMachine.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { useQuickSend } from '@/composables/useQuickSend'
import { useChatKeyboard } from '@/composables/useChatKeyboard'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { useAgents } from '@/composables/useAgents'
import { useToast } from '@/composables/useToast'
import { useFileUpload } from '@/composables/useFileUpload'
import { useVoiceInput } from '@/composables/useVoiceInput'
import { useChatRecommendation } from '@/composables/useChatRecommendation'
import { appLog } from '@/utils/appLog'
import { apiGet } from '@/utils/api'

const { t } = useI18n()
const { availableCommands, availableModes, currentTransport: sessionTransport, autoApprove, toggleAutoApprove, contextUsed, contextSize, contextInputTokens, contextOutputTokens, contextTotalTokens, contextCachedReadTokens, contextCachedWriteTokens, contextThoughtTokens, contextCost, contextCurrency } = useSessionIdentity()
const { supportsACP, hasPreferredMode } = useAgents()
const toast = useToast()
const { uploadAndAttach, pendingFiles, removeFile } = useFileUpload()

// isACP: true when the current agent supports ACP (has acpCommand).
// Used for mode chips — these are ACP features
// that apply regardless of the current session's transport mode.
const isACP = computed(() => supportsACP(props.currentAgentId || ''))

// isACPTransport: true when the current session is using ACP transport.
// Slash commands are only available in ACP transport mode — even if the
// agent supports dual transport, CLI sessions don't have slash commands.
const isACPTransport = computed(() => {
  if (sessionTransport.value) return sessionTransport.value === 'acp-stdio'
  return props.currentTransport === 'acp-stdio'
})

// ACP 同步按钮的禁用状态与提示：空会话（无 ACP 会话）或当前会话运行中时不可同步。
const currentSessionRunning = computed(() => !!props.currentSessionRunning)
const sessionEmpty = computed(() => !props.messages || props.messages.length === 0)
const acpSyncDisabled = computed(() => props.acpSyncing || currentSessionRunning.value || sessionEmpty.value)
const acpSyncTitle = computed(() => {
  if (props.acpSyncing) return t('chat.actions.acpSyncSyncing')
  if (currentSessionRunning.value) return t('chat.actions.acpSyncRunning')
  if (sessionEmpty.value) return t('chat.actions.acpSyncEmpty')
  return t('chat.actions.acpSync')
})

const showModeInfo = computed(() => isACP.value && (availableModes.value.length > 0 || hasPreferredMode(props.currentAgentId || '')))

function onModeClick() {
  if (modeMouseLongFired) {
    modeMouseLongFired = false
    return
  }
  openSettingsDrawer('mode')
}

// Long-press on mode chip → toggle auto-approve
let modeMouseTimer = null
let modeMouseLongFired = false

function onModeLongPress() {
  doToggleAutoApprove()
}

function onModeMouseDown() {
  modeMouseLongFired = false
  modeMouseTimer = setTimeout(() => {
    modeMouseLongFired = true
    doToggleAutoApprove()
  }, 500)
}

function onModeMouseUp() {
  if (modeMouseTimer) {
    clearTimeout(modeMouseTimer)
    modeMouseTimer = null
  }
}

function doToggleAutoApprove() {
  const next = !autoApprove.value
  toggleAutoApprove(next)
  toast.show(next ? t('chat.autoApprove.enabled') : t('chat.autoApprove.disabled'), {
    icon: next ? '✅' : '🔒',
    type: next ? 'success' : 'info',
  })
}
const showUsageInfo = computed(() => isACPTransport.value || contextSize.value > 0)
const usagePct = computed(() => contextSize.value > 0 ? Math.round((contextUsed.value / contextSize.value) * 100) : 0)
const usageColor = computed(() => {
  const pct = usagePct.value
  if (pct >= 95) return '#ef4444'
  if (pct >= 90) return '#f97316'
  if (pct >= 75) return '#eab308'
  return '#22c55e'
})
const hasCompactCommand = computed(() => availableCommands.value.some(cmd => cmd.name === '/compact' || cmd.name === 'compact'))
const showCompactBtn = computed(() => usagePct.value >= 75 && hasCompactCommand.value && isACPTransport.value)
const dialog = useDialog()
const quickSendStore = useQuickSend()
const { items: quickSendItems, fetchItems } = quickSendStore
const settingsDrawerInitialTab = ref('model')
const quickSendDrawer = useTabDrawer('chat', quickSendStore.showEditDialog)
const settingsDrawer = useTabDrawer('chat')

// ── Rotating placeholder ──
const placeholderIndex = ref(0)
let placeholderTimer = null

// The candidate hints cycle when the textarea is empty, unfocused, and not in queue/upload mode.
// When quickSendItems exist, the cycle includes the quick-send tip; otherwise it's skipped.
const placeholderHints = computed(() => {
  const hints = [t('chat.input.placeholder')]
  if (quickSendItems.value.length > 0) {
    hints.push(t('chat.input.placeholderQuickSend'))
  }
  hints.push(t('chat.input.placeholderCommand'))
  return hints
})

function startPlaceholderRotation() {
  stopPlaceholderRotation()
  if (placeholderHints.value.length <= 1) return
  placeholderTimer = setInterval(() => {
    placeholderIndex.value = (placeholderIndex.value + 1) % placeholderHints.value.length
  }, 4000)
}

function stopPlaceholderRotation() {
  if (placeholderTimer) {
    clearInterval(placeholderTimer)
    placeholderTimer = null
  }
}

// Reset index when hints change (e.g. quickSendItems loaded) so we don't go out of bounds
watch(placeholderHints, () => {
  if (placeholderIndex.value >= placeholderHints.value.length) {
    placeholderIndex.value = 0
  }
})

const isTextareaFocused = ref(false)

const dynamicPlaceholder = computed(() => {
  if (props.loading) return t('chat.input.placeholderQueue')
  if (isTextareaFocused.value) return t('chat.input.placeholder')
  // Unfocused & empty: cycle through hints
  return placeholderHints.value[placeholderIndex.value] || t('chat.input.placeholder')
})

const props = defineProps({
  inputDisabled: Boolean,
  loading: Boolean,
  currentFile: Object,
  currentDir: String,
  attachedFiles: Array,
  quotes: { type: Array, default: () => [] },
  // Backward-compatible single quote prop for isolated consumers/tests.
  quoteData: Object,
  messages: Array,
  autoSpeechEnabled: Boolean,
  currentSessionId: String,
  chatUnreadCount: Number,
  chatRunning: Boolean,
  acpSyncing: Boolean,
  currentModelId: String,
  currentModelName: String,
  currentModeName: String,
  currentTransport: String,
  currentAgentId: String,
  currentSessionRunning: Boolean,
  active: Boolean,
  sessionPanelOpen: Boolean,
})

const emit = defineEmits([
  'send',
  'cancel',
  'add-attached',
  'remove-attached',
  'remove-attached-by-path',
  'remove-quote',
  'quote-click',
  'open-session-tab',
  'open-session-search',
  'file-tag-click',
  'toggle-auto-speech',
  'create-session',
  'show-agent-selector',
  'archive-session',
  'destroy-session',
  'open-user-msg-index',
  'switch-model',
  'switch-thinking-effort',
  'switch-mode',
  'switch-transport',
  'sync-acp-session',
])

const inputText = ref('')

// ── Conversation recommendation (推荐回复) ───────────────
// Recommendation state is bound per session via useChatRecommendation: the
// displayed value is derived from the currently active session's slot, so a
// recommendation from another session can never leak into the active view.
//
// Each recommendation is also bound to the assistant message it was generated
// for, and only surfaced when that message is the session's current last
// completed assistant message. This prevents briefly flashing a stale
// recommendation (from an earlier reply) while the current reply's
// recommendation is still being generated asynchronously.
//
// Only a non-streaming assistant message with a real numeric DB id is eligible:
// during streaming the placeholder message carries a temporary "drain-…" id (or
// a still-streaming row), which would never match the persisted recommendation's
// message_id and would silently hide every recommendation.
const lastAssistantMessageId = () => {
  const msgs = props.messages || []
  for (let i = msgs.length - 1; i >= 0; i--) {
    if (msgs[i].role !== 'assistant' || msgs[i].streaming) continue
    const id = Number(msgs[i].id)
    if (Number.isInteger(id) && id > 0) return id
  }
  return undefined
}

const rec = useChatRecommendation({
  activeSessionId: () => props.currentSessionId || undefined,
  loading: () => props.loading,
  isLastMessageAssistant: () => {
    const msgs = props.messages || []
    const last = msgs[msgs.length - 1]
    return !!last && last.role === 'assistant'
  },
  lastAssistantMessageId,
  fetchRemote: async (sessionId, messageId) => {
    const data = await apiGet(`/api/chat/recommendation?session_id=${encodeURIComponent(sessionId)}&message_id=${encodeURIComponent(String(messageId))}`)
    return data?.recommendation || ''
  },
})
const { current: recommendation, show: showRecommendationChip } = rec

// Whether the recommendation banner is expanded to show its full text (default
// collapsed to a single line). Reset whenever a new recommendation arrives.
const recommendationExpanded = ref(false)

function onRecommendationEvent(evt) {
  const detail = evt.detail || {}
  if (detail.session_id == null || detail.message_id == null) return
  rec.upsert(detail.session_id, detail.recommendation, detail.message_id)
  recommendationExpanded.value = false
}

function toggleRecommendationExpand() {
  recommendationExpanded.value = !recommendationExpanded.value
}

function acceptRecommendation() {
  const text = rec.accept()
  if (text) inputText.value = text
}

// Clear any currently surfaced recommendation (chip + stored text).
function clearRecommendation() {
  const id = props.currentSessionId
  if (id) rec.invalidate(id)
}

// ── Recommendation reconcile after a completed reply ──
// A device that missed the live chat_recommendation broadcast (e.g. a mobile
// WebView suspended while the backend generated the recommendation) still needs
// to surface it. The recommendation is persisted server-side and generated
// asynchronously *after* the reply's 'done' event. Once the reply is finalized
// (its placeholder becomes a real non-streaming DB message), we fetch the
// recommendation bound to that exact message. This fires both on session switch
// (history loads) and right after a reply completes, and is a no-op when the
// live broadcast already cached a value. ensureFetched() is idempotent — it
// no-ops when the slot is cached, when the session is streaming, or when the
// fetch returns empty.
const lastAssistantMsgId = computed(() => lastAssistantMessageId())
watch(lastAssistantMsgId, (mid) => {
  const sid = props.currentSessionId
  if (!sid || mid === undefined) return
  void rec.ensureFetched(sid, mid)
})

// ── Voice input (ASR) ───────────────────────────────
const voiceInput = useVoiceInput()
const { state: voiceState, inputText: voiceInputText, toggle: toggleVoice, start: startVoice, stop: stopVoice, shortcutKey: voiceShortcutKey } = voiceInput

const VOICE_LONG_PRESS_MS = 500
let voicePressTimer = null
let voicePointerDown = false
let voiceLongPressActive = false
// Set when a long-press starts recording so the synthetic click fired on release
// is suppressed in handleSendClick (mirrors quickSendJustTriggered).
let voiceJustRecorded = false

function onSendPointerDown() {
  voicePointerDown = true
  voiceLongPressActive = false
  voiceJustRecorded = false
  if (voicePressTimer) { clearTimeout(voicePressTimer) }
  voicePressTimer = setTimeout(() => {
    if (voicePointerDown && !hasInputContent.value) {
      voiceLongPressActive = true
      voiceJustRecorded = true
      void toggleVoice()
    }
  }, VOICE_LONG_PRESS_MS)
}

function onSendPointerUp() {
  voicePointerDown = false
  if (voicePressTimer) { clearTimeout(voicePressTimer); voicePressTimer = null }
  if (voiceLongPressActive) {
    voiceLongPressActive = false
    void toggleVoice()
  }
}

// Sync recognized voice text into the input box (never auto-sends).
watch(voiceInputText, (val) => {
  if (val && val !== inputText.value) {
    inputText.value = val
  }
})

function onVoiceShortcutDown(e) {
  // NOTE: Only the hardcoded F9 shortcut is currently supported.
  // stt.shortcut_key may hold other values; matching those is out of scope
  // and would require a lookup table of key/alt/ctrl/meta/shift combos.
  const sc = voiceShortcutKey()
  if (sc === 'F9' && e.code === 'F9' && !e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey && !e.repeat) {
    e.preventDefault()
    startVoice()
  }
}

function onVoiceShortcutUp(e) {
  const sc = voiceShortcutKey()
  if (sc === 'F9' && e.code === 'F9' && !e.altKey && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
    e.preventDefault()
    stopVoice()
  }
}

// Safety net: if the window loses focus while F9 is held (keyup may never fire),
// end the recording so it doesn't get stuck.
function onVoiceBlurStop() {
  if (voiceState.value === 'recording') {
    stopVoice()
  }
}

const rootRef = ref(null)
const textareaRef = ref(null)
const isPasteOver = ref(false)
let pasteOverlayTimer = 0
const attachDrawer = useTabDrawer('chat')
const attachDrawerRef = ref(null)
const attachMenuRef = ref(null) // kept for ref stability, no longer used for PopupMenu
const showQuickMenu = ref(false)
const sendBtnRef = ref(null)

function openSettingsDrawer(tab) {
  settingsDrawerInitialTab.value = tab
  settingsDrawer.open()
}

// ── Context usage popup ──
const showUsagePopup = ref(false)
const usageElRef = ref(null)
const atCommands = computed(() => {
  return [
    { key: '@chatsearch', label: '@chatsearch', description: t('chat.atCommand.chatsearchDesc') },
    { key: '@task', label: '@task', description: t('chat.atCommand.taskDesc') },
  ]
})

// ── Slash command autocomplete (ACP backend commands) ──
const showSlashMenu = ref(false)
const slashMenuIndex = ref(-1)

// ── @ command autocomplete ──
const showAtMenu = ref(false)
const atMenuIndex = ref(-1)

const atMenuItems = computed(() => {
  const text = inputText.value
  if (!text.startsWith('@')) return []
  const query = text.slice(1) // strip leading '@'
  const cmds = atCommands.value // unwrap computed ref
  if (!query) return cmds.map(cmd => ({ ...cmd, query: '' })) // empty query → show all
  const lowerQ = query.toLowerCase()
  return cmds
    .filter(cmd => cmd.key.toLowerCase().includes(lowerQ))
    .map(cmd => ({ ...cmd, query }))
})

const slashMenuItems = computed(() => {
  const text = inputText.value
  if (!text.startsWith('/')) return []
  const query = text.slice(1) // strip leading '/'
  if (!query) return availableCommands.value.map(cmd => ({
    key: '/' + cmd.name,
    label: '/' + cmd.name,
    description: cmd.description,
    inputHint: cmd.inputHint || '',
    query: '',
  }))
  const lowerQ = query.toLowerCase()
  return availableCommands.value
    .filter(cmd => cmd.name.toLowerCase().includes(lowerQ))
    .map(cmd => ({
      key: '/' + cmd.name,
      label: '/' + cmd.name,
      description: cmd.description,
      inputHint: cmd.inputHint || '',
      query,
    }))
})

// Directly control menu visibility from inputText changes
watch(inputText, () => {
  const text = inputText.value
  // @ command menu
  const shouldShowAt = text.startsWith('@')
    && !text.includes(' ')
    && atMenuItems.value.length > 0
  showAtMenu.value = shouldShowAt
  // Slash command menu
  const shouldShowSlash = text.startsWith('/')
    && !text.includes(' ')
    && slashMenuItems.value.length > 0
  showSlashMenu.value = shouldShowSlash
})

// Reset selection when menu items change
watch(slashMenuItems, () => { slashMenuIndex.value = -1 })
watch(atMenuItems, () => { atMenuIndex.value = -1 })

// Scroll selected menu item into view
watch(slashMenuIndex, (idx) => {
  if (idx < 0) return
  nextTick(() => {
    // Menus are teleported to <body>, so query from document, not rootRef.
    const el = document.querySelector('[data-slash-idx="' + idx + '"]')
    el?.scrollIntoView({ block: 'nearest' })
  })
})
watch(atMenuIndex, (idx) => {
  if (idx < 0) return
  nextTick(() => {
    // Menus are teleported to <body>, so query from document, not rootRef.
    const el = document.querySelector('[data-at-idx="' + idx + '"]')
    el?.scrollIntoView({ block: 'nearest' })
  })
})

function handleAtSelect(cmd) {
  inputText.value = cmd.key + ' '
  showAtMenu.value = false
  atMenuIndex.value = -1
  nextTick(() => {
    const el = textareaRef.value
    if (el) el.focus()
  })
}

function handleSlashSelect(cmd) {
  inputText.value = cmd.key + ' '
  showSlashMenu.value = false
  slashMenuIndex.value = -1
  nextTick(() => {
    const el = textareaRef.value
    if (el) el.focus()
  })
}

// ── Menu keyboard navigation (PC: ArrowUp/Down + Enter + Escape) ──
function handleMenuKeydown(e) {
  // Determine which menu is active (slash takes priority if both open)
  const isSlash = showSlashMenu.value
  const isAt = showAtMenu.value
  if (!isSlash && !isAt) return false

  // Escape closes the active menu
  if (e.key === 'Escape') {
    e.preventDefault()
    if (isSlash) { showSlashMenu.value = false; slashMenuIndex.value = -1 }
    else { showAtMenu.value = false; atMenuIndex.value = -1 }
    return true
  }

  const items = isSlash ? slashMenuItems.value : atMenuItems.value
  const indexRef = isSlash ? slashMenuIndex : atMenuIndex
  if (items.length === 0) return false

  if (e.key === 'ArrowDown') {
    e.preventDefault()
    indexRef.value = (indexRef.value + 1) % items.length
    return true
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    indexRef.value = indexRef.value <= 0 ? items.length - 1 : indexRef.value - 1
    return true
  }
  if (e.key === 'Enter' && indexRef.value >= 0 && indexRef.value < items.length) {
    e.preventDefault()
    const selected = items[indexRef.value]
    if (isSlash) handleSlashSelect(selected)
    else handleAtSelect(selected)
    return true
  }
  return false
}

function onTextareaKeydown(e) {
  // Menu keyboard navigation takes priority
  if (handleMenuKeydown(e)) return
  // Default: Enter (without modifier) sends
  if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault()
    emit('send', inputText.value.trim())
  }
}

// Keyboard detection for iOS (no adjustResize) — activates visualViewport monitoring
// when textarea is focused so App.vue can compensate the layout.
const chatKeyboard = useChatKeyboard()

// Stop button two-click confirmation state
const stopPrimed = ref(false)
const cancelling = ref(false)
const stopMachine = createStopButtonMachine({
  onConfirm: () => {
    cancelling.value = true
    emit('cancel')
  },
  onPrimeReset: () => { stopPrimed.value = false },
})

function handleStopClick() {
  const result = stopMachine.click()
  stopPrimed.value = result.primed
  if (result.confirmed) {
    stopPrimed.value = false
  }
}

// Per-session draft cache: save input text when switching away, restore when switching back
const draftCache = new Map()

watch(() => props.currentSessionId, (newId, oldId) => {
  // Save draft from the old session
  if (oldId) {
    const text = inputText.value
    if (text) {
      draftCache.set(oldId, text)
    }
    // Don't delete existing draft when inputText is empty — saveDraft() may have
    // already saved it before clearInputPreserveDraft() cleared the visible text.
    // Only clearInput() (called after message send) explicitly deletes the draft.
  }
  // Restore draft for the new session (or clear if none)
  inputText.value = newId ? (draftCache.get(newId) || '') : ''
  // autoResizeTextarea is called automatically by the inputText watcher
})

// On session switch, restore the persisted recommendation for that session into
// its own slot (immediate if already cached, otherwise fetched) — the displayed
// value is derived from the active session's slot, so no cross-session leakage.
watch(() => props.currentSessionId, () => {
  // A new conversation starts with a collapsed banner.
  recommendationExpanded.value = false
  // The recommendation for the new session is fetched once its last assistant
  // message is loaded (see the lastAssistantMsgId watcher), so we don't need to
  // fetch here with a possibly-unloaded message id.
})

const quoteItems = computed(() => props.quotes.length > 0
  ? props.quotes
  : props.quoteData ? [props.quoteData] : [])

// When a new assistant message starts streaming, any previously surfaced
// recommendation belongs to the last completed reply and is stale — invalidate
// the active session's slot so the in-flight value can't be reused.
watch(() => props.loading, (val) => {
  if (val && props.currentSessionId) {
    rec.invalidate(props.currentSessionId)
  }
})

const hasInputContent = computed(() => inputText.value.trim() || props.attachedFiles.length > 0 || quoteItems.value.length > 0)

// Extract recently referenced files from message history
const recentReferencedFiles = computed(() => {
  return computeRecentReferencedFiles(props.messages, props.attachedFiles, props.currentFile?.path)
})

function handleCreateClick(e) {
  // On desktop, click = show agent selector (short tap equivalent)
  if (e.detail === 0) return
  emit('show-agent-selector')
}

async function handleArchive() {
  if (!props.currentSessionId) return
  const confirmed = await dialog.confirm(t('chat.archive.confirm'), {
    dangerous: true,
    extraText: t('chat.archive.destroyBtn'),
    extraPrimedText: t('chat.archive.destroyBtnPrimed'),
    onExtraAction: () => emit('destroy-session'),
  })
  if (confirmed) {
    emit('archive-session')
  }
}

function quoteFileName(quote) {
  if (!quote?.filePath) return ''
  return quote.filePath.split('/').pop() || quote.filePath
}

function quoteLineRange(quote) {
  if (!quote?.startLine) return ''
  const s = quote.startLine
  const e = quote.endLine
  if (e && e !== s) return `:${s}-${e}`
  return `:${s}`
}

function autoResizeTextarea() {
  const el = textareaRef.value
  if (!el) return
  el.style.height = 'auto'
  const computed = getComputedStyle(el)
  const lineHeight = parseFloat(computed.lineHeight) || 20
  const paddingTop = parseFloat(computed.paddingTop) || 0
  const paddingBottom = parseFloat(computed.paddingBottom) || 0
  const maxContentHeight = lineHeight * 3
  const maxHeight = maxContentHeight + paddingTop + paddingBottom
  el.style.height = Math.min(el.scrollHeight, maxHeight) + 'px'
}

function onTextareaFocus() {
  chatKeyboard.activate()
  autoResizeTextarea()
  isTextareaFocused.value = true
  stopPlaceholderRotation()
}

function onTextareaBlur() {
  chatKeyboard.debounceDeactivate()
  autoResizeTextarea()
  isTextareaFocused.value = false
  // Start rotation when unfocused (only if empty input)
  if (!inputText.value.trim()) {
    startPlaceholderRotation()
  }
  // Close @ and / command menus when textarea loses focus (clicking menu items uses
  // @mousedown.prevent so blur won't fire for those interactions)
  nextTick(() => {
    showAtMenu.value = false
    showSlashMenu.value = false
  })
}

// Watch inputText changes (both user input and programmatic changes like draft restore)
// to ensure textarea height stays in sync with content
watch(inputText, () => nextTick(() => autoResizeTextarea()))

function onPaste(e) {
  const now = Date.now()
  if (now - lastPasteTimestamp < 300) {
    if (e.cancelable) e.preventDefault()
    if (typeof e.stopPropagation === 'function') e.stopPropagation()
    return
  }

  const clipboardData = e.clipboardData
  if (!clipboardData) return

  const files = []

  // 1. Process DataTransferItemList items (image files & screenshot blobs)
  if (clipboardData.items) {
    for (const item of clipboardData.items) {
      if (item.kind === 'file') {
        const raw = item.getAsFile()
        if (!raw) continue
        // Clipboard images (e.g. screenshots) may have no name or empty name.
        // Backend requires non-empty extension, so give a default name with extension.
        if (!raw.name || raw.name === '' || !raw.name.includes('.')) {
          const ext = raw.type === 'image/png' ? '.png'
            : raw.type === 'image/jpeg' ? '.jpg'
            : raw.type === 'image/webp' ? '.webp'
            : raw.type === 'image/gif' ? '.gif'
            : raw.type === 'image/bmp' ? '.bmp'
            : raw.type === 'image/svg+xml' ? '.svg'
            : '.png'
          files.push(new File([raw], `clipboard_${Date.now()}_${files.length}${ext}`, { type: raw.type || 'image/png' }))
        } else {
          files.push(raw)
        }
      }
    }
  }

  // 2. Fallback to clipboardData.files if items yielded no files
  if (files.length === 0 && clipboardData.files && clipboardData.files.length > 0) {
    for (let i = 0; i < clipboardData.files.length; i++) {
      const raw = clipboardData.files[i]
      if (raw.type.startsWith('image/') || raw.name.match(/\.(png|jpe?g|gif|webp|bmp|svg)$/i)) {
        if (!raw.name || raw.name === '' || !raw.name.includes('.')) {
          const ext = raw.type === 'image/png' ? '.png'
            : raw.type === 'image/jpeg' ? '.jpg'
            : raw.type === 'image/webp' ? '.webp'
            : raw.type === 'image/gif' ? '.gif'
            : raw.type === 'image/bmp' ? '.bmp'
            : raw.type === 'image/svg+xml' ? '.svg'
            : '.png'
          files.push(new File([raw], `clipboard_${Date.now()}_${i}${ext}`, { type: raw.type || 'image/png' }))
        } else {
          files.push(raw)
        }
      }
    }
  }

  // 3. Check plain text for data:image/... base64 string
  if (files.length === 0 && typeof clipboardData.getData === 'function') {
    const textData = clipboardData.getData('text/plain')
    if (textData && textData.trim().startsWith('data:image/')) {
      const file = dataUrlToFile(textData.trim(), `clipboard_${Date.now()}_0`)
      if (file) {
        files.push(file)
      }
    }
  }

  // 4. Check HTML for data:image/... base64 images (synchronous)
  if (files.length === 0 && typeof clipboardData.getData === 'function') {
    const htmlData = clipboardData.getData('text/html')
    if (htmlData) {
      const matches = htmlData.match(/src=["'](data:image\/[a-zA-Z0-9+/.-]+;base64,[^"']+)["']/gi)
      if (matches) {
        let count = 0
        for (const match of matches) {
          const src = match.replace(/^src=["']/i, '').replace(/["']$/i, '')
          const file = dataUrlToFile(src, `clipboard_${Date.now()}_${count++}`)
          if (file) files.push(file)
        }
      }
    }
  }

  if (files.length > 0) {
    lastPasteTimestamp = now
    if (e.cancelable) e.preventDefault()
    if (typeof e.stopPropagation === 'function') e.stopPropagation()
    if (typeof e.stopImmediatePropagation === 'function') e.stopImmediatePropagation()

    const uniqueFiles = dedupePasteFiles(files)
    appLog.d('ChatInputBar', 'Pasted image/files detected, uploading and attaching:', uniqueFiles.map(f => f.name))
    // Show dynamic paste uploading feedback tied to upload lifecycle
    const generation = ++pasteUploadGeneration
    clearTimeout(pasteOverlayTimer)
    isPasteOver.value = true
    uploadAndAttach(uniqueFiles).finally(() => {
      if (generation !== pasteUploadGeneration) return
      pasteOverlayTimer = setTimeout(() => {
        isPasteOver.value = false
      }, 200)
    })
  }
}

function dataUrlToFile(dataUrl, filename) {
  try {
    const parts = dataUrl.split(',')
    if (parts.length < 2) return null
    const mimeMatch = parts[0].match(/:(.*?);/)
    const mime = mimeMatch ? mimeMatch[1] : 'image/png'
    const bstr = atob(parts[1])
    let n = bstr.length
    const u8arr = new Uint8Array(n)
    while (n--) {
      u8arr[n] = bstr.charCodeAt(n)
    }
    const ext = mime === 'image/png' ? '.png'
      : mime === 'image/jpeg' ? '.jpg'
      : mime === 'image/webp' ? '.webp'
      : mime === 'image/gif' ? '.gif'
      : '.png'
    const name = filename.includes('.') ? filename : `${filename}${ext}`
    return new File([u8arr], name, { type: mime })
  } catch {
    return null
  }
}

let lastPasteTimestamp = 0
let pasteUploadGeneration = 0

function dedupePasteFiles(files) {
  const seen = new Set()
  const result = []
  for (const f of files) {
    const key = `${f.name}_${f.size}_${f.type}_${f.lastModified}`
    if (!seen.has(key)) {
      seen.add(key)
      result.push(f)
    }
  }
  return result
}

function handleWindowPaste(e) {
  if (e.defaultPrevented) return

  const target = e.target
  const isChatTextarea = target === textareaRef.value
  if (isChatTextarea) return

  const isOtherInput = target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)
  if (isOtherInput) return

  onPaste(e)
}

function clearInput() {
  inputText.value = ''
  // Also clear the draft cache for current session so it doesn't linger
  if (props.currentSessionId) {
    draftCache.delete(props.currentSessionId)
  }
}

/** Save current input text to draft cache without clearing it (called before session switch). */
function saveDraft() {
  if (props.currentSessionId) {
    const text = inputText.value
    if (text) {
      draftCache.set(props.currentSessionId, text)
    } else {
      draftCache.delete(props.currentSessionId)
    }
  }
}

/** Clear visible input text but preserve the draft cache (used during session switch). */
function clearInputPreserveDraft() {
  inputText.value = ''
}

function handleAttachFile(filePath, isDir) {
  emit('add-attached', filePath, isDir)
}

function handleRemoveAttached(filePath) {
  emit('remove-attached-by-path', filePath)
}

async function toggleAttachMenu() {
  attachDrawer.toggle()
}

function handleSendClick() {
  if (voiceJustRecorded) {
    voiceJustRecorded = false
    return
  }
  if (inputText.value.trim()) {
    emit('send', inputText.value.trim())
  } else if (props.attachedFiles.length > 0 || quoteItems.value.length > 0) {
    emit('send', '')
  } else {
    toggleQuickMenu()
  }
}

// — Quick-send long-press →
const QUICK_SEND_LONG_PRESS_MS = 500
const quickSendPressingId = ref(null)
let quickSendPressTimer = null
let quickSendMoved = false
let quickSendJustTriggered = false
let quickSendTouchStartPos = { x: 0, y: 0 }
let quickSendCurrentItem = null

function handleQuickSendClick(item) {
  // Desktop: click directly sends
  // Mobile: touchend handles send and sets quickSendJustTriggered to prevent this click from re-sending
  if (quickSendJustTriggered) {
    quickSendJustTriggered = false
    return
  }
  showQuickMenu.value = false
  emit('send', item.command)
}

function injectToInput(text) {
  const current = inputText.value.trim()
  inputText.value = current ? current + '\n' + text : text
  nextTick(() => {
    textareaRef.value?.focus()
  })
}

function onQuickSendTouchStart(item, e) {
  quickSendMoved = false
  quickSendJustTriggered = false
  quickSendCurrentItem = item
  const touch = e.touches[0]
  quickSendTouchStartPos = { x: touch.clientX, y: touch.clientY }
  quickSendPressingId.value = item.id

  quickSendPressTimer = setTimeout(() => {
    if (!quickSendMoved && quickSendPressingId.value === item.id) {
      // Long-press triggered → inject into input box
      quickSendJustTriggered = true
      quickSendPressingId.value = null
      quickSendCurrentItem = null
      injectToInput(item.command)
      showQuickMenu.value = false
    }
  }, QUICK_SEND_LONG_PRESS_MS)
}

function onQuickSendTouchMove(e) {
  if (!quickSendPressingId.value) return
  const touch = e.touches[0]
  const dx = touch.clientX - quickSendTouchStartPos.x
  const dy = touch.clientY - quickSendTouchStartPos.y
  if (Math.abs(dx) > 10 || Math.abs(dy) > 10) {
    quickSendMoved = true
    cancelQuickSendPress()
  }
}

function onQuickSendTouchEnd() {
  if (quickSendPressTimer) {
    clearTimeout(quickSendPressTimer)
    quickSendPressTimer = null
  }
  // Short tap (no long-press triggered): send directly
  if (quickSendPressingId.value !== null && !quickSendJustTriggered && quickSendCurrentItem) {
    const item = quickSendCurrentItem
    quickSendCurrentItem = null
    quickSendPressingId.value = null
    quickSendJustTriggered = true // prevent synthetic click from re-sending
    showQuickMenu.value = false
    emit('send', item.command)
  } else {
    quickSendPressingId.value = null
    quickSendCurrentItem = null
  }
}

function cancelQuickSendPress() {
  if (quickSendPressTimer) {
    clearTimeout(quickSendPressTimer)
    quickSendPressTimer = null
  }
  quickSendPressingId.value = null
  quickSendCurrentItem = null
}

function toggleQuickMenu() {
  showQuickMenu.value = !showQuickMenu.value
}

function handleCompact() {
  emit('send', '/compact')
}

function handleSwitchModel(model) {
  emit('switch-model', model)
}

function handleSwitchThinkingEffort(level) {
  emit('switch-thinking-effort', level)
}

function handleSwitchMode(mode) {
  emit('switch-mode', mode)
}

function handleSwitchTransport(transport) {
  emit('switch-transport', transport)
}

// Menu mutual exclusion: opening one closes the others
watch(() => attachDrawer.isOpen.value, (v) => { if (v) { showQuickMenu.value = false; settingsDrawer.close(); showSlashMenu.value = false; showUsagePopup.value = false } })
watch(showQuickMenu, (v) => { if (v) { attachDrawer.close(); settingsDrawer.close(); showSlashMenu.value = false; showUsagePopup.value = false } })
watch(() => settingsDrawer.isOpen.value, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; showSlashMenu.value = false; showUsagePopup.value = false } })
watch(showSlashMenu, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; settingsDrawer.close(); showUsagePopup.value = false } })
watch(showUsagePopup, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; settingsDrawer.close(); showSlashMenu.value = false } })

onMounted(() => {
  fetchItems()
  startPlaceholderRotation()
  window.addEventListener('paste', handleWindowPaste, true)
  window.addEventListener('keydown', onVoiceShortcutDown)
  window.addEventListener('keyup', onVoiceShortcutUp)
  window.addEventListener('blur', onVoiceBlurStop)
  window.addEventListener('clawbench-recommendation', onRecommendationEvent)
})

onBeforeUnmount(() => {
  pasteUploadGeneration++
  window.removeEventListener('paste', handleWindowPaste, true)
  window.removeEventListener('keydown', onVoiceShortcutDown)
  window.removeEventListener('keyup', onVoiceShortcutUp)
  window.removeEventListener('blur', onVoiceBlurStop)
  window.removeEventListener('clawbench-recommendation', onRecommendationEvent)
  stopMachine.destroy()
  if (quickSendPressTimer) {
    clearTimeout(quickSendPressTimer)
    quickSendPressTimer = null
  }
  if (voicePressTimer) {
    clearTimeout(voicePressTimer)
    voicePressTimer = null
  }
  voiceInput.cancel()
  clearTimeout(pasteOverlayTimer)

  stopPlaceholderRotation()
})

// Reset stop confirmation state when loading ends (AI finished or cancelled)
watch(() => props.loading, (val) => {
  if (!val) {
    stopPrimed.value = false
    cancelling.value = false
    stopMachine.reset()
  }
})

defineExpose({
  clearInput,
  saveDraft,
  clearInputPreserveDraft,
  clearRecommendation,
  inputText,
  deleteDraft: (sessionId) => { draftCache.delete(sessionId) },
  hasDraft: (sessionId) => draftCache.has(sessionId),
  getDraft: (sessionId) => draftCache.get(sessionId) ?? null,
  injectToInput,
  handleQuickSendClick,
  onQuickSendTouchStart,
  onQuickSendTouchMove,
  onQuickSendTouchEnd,
  cancelQuickSendPress,
  quickSendPressingId,
  handleArchive,
})
</script>

<style scoped>
/* Outer wrapper: top actions + input box stacked vertically */
.chat-input-wrapper {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  margin: 0 0 8px;
  padding: 8px 8px 0;
  box-shadow: inset 0 1px 0 var(--border-color, #e5e5e5);
}

/* Session info bar (model + mode, below input box) */
.chat-session-info {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 4px;
  padding: 4px 8px 0;
  font-size: 11px;
  line-height: 1.4;
  color: var(--text-muted, #999);
  overflow: hidden;
  white-space: nowrap;
  min-width: 0;
}

.session-info-model,
.session-info-mode {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 14px;
  cursor: pointer;
  transition: color 0.15s;
  user-select: none;
  -webkit-user-select: none;
}

.session-info-model:active {
  color: var(--accent-color, #0066cc);
}

.session-info-mode:active {
  color: var(--accent-color, #0066cc);
}

.session-info-mode-auto {
  color: #4caf50;
}

.session-info-mode-auto:active {
  color: #388e3c;
}

.session-info-model svg,
.session-info-mode svg,
.session-info-usage svg {
  flex-shrink: 0;
}

.session-info-divider {
  flex-shrink: 1;
  width: 1px;
  height: 10px;
  background: var(--border-color, #e5e5e5);
}

.session-info-usage {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
  cursor: pointer;
}

.session-info-compact {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 0;
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
  border-radius: 4px;
  color: var(--text-muted, #999);
  font-size: 11px;
  line-height: 1.4;
  transition: color 0.15s;
  user-select: none;
  -webkit-user-select: none;
}

.session-info-compact:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.session-info-compact:active:not(:disabled) {
  color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .session-info-compact:hover:not(:disabled) {
    color: var(--accent-color, #0066cc);
  }
}

.usage-bar {
  position: relative;
  width: 28px;
  height: 6px;
  border-radius: 3px;
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
  flex-shrink: 0;
}

.usage-bar-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: 3px;
  transition: width 0.3s ease, background 0.3s ease;
}

/* Top action bar (above input box, compact) */
.chat-top-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 4px 6px;
  overflow: hidden;
}

/* Session button group */
.chat-action-group {
  display: inline-flex;
  align-items: stretch;
  border-radius: 20px;
  overflow: hidden;
  border: 1px solid var(--border-color, #e5e5e5);
  flex-shrink: 0;
}

/* Auto-speech toggle button */
.auto-speech-btn {
  flex-shrink: 0;
}

.chat-action-group .chat-action-btn {
    border-radius: 0;
    height: auto;
}

.chat-action-group .chat-action-btn:first-child {
    border-radius: 0;
}

/* Group label: subtle text identifying the button group */
.chat-group-label {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 5px 6px;
    color: var(--text-muted, #999);
    background: var(--bg-tertiary, #f0f0f0);
    pointer-events: none;
    user-select: none;
    border-right: 1px solid var(--border-color, #e5e5e5);
    font-size: 11px;
    line-height: 1.3;
}

.chat-action-group .chat-action-btn:last-child {
    border-radius: 0 999px 999px 0;
}

.chat-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding: 5px 8px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 1;
  transition: color 0.15s, background 0.15s, transform 0.1s;
  -webkit-tap-highlight-color: transparent;
  user-select: none;
}

@media (hover: hover) {
  .chat-action-btn:hover {
    color: var(--accent-color, #0066cc);
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.chat-action-btn:active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  transform: scale(0.92);
}

.chat-action-btn.active {
  color: var(--accent-color, #0066cc);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
}

.chat-action-btn.active:active {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 25%, transparent);
  transform: scale(0.92);
}

.chat-action-btn-archive:not(.disabled) {
  color: var(--text-muted, #999);
}

@media (hover: hover) {
  .chat-action-btn-archive:not(.disabled):hover {
    color: var(--color-warning, #e6a23c);
    background: color-mix(in srgb, var(--color-warning, #e6a23c) 10%, transparent);
  }
}

.chat-action-btn-archive:not(.disabled):active {
  color: var(--color-warning, #e6a23c);
  background: color-mix(in srgb, var(--color-warning, #e6a23c) 18%, transparent);
  transform: scale(0.92);
}

.chat-action-btn-archive.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.acp-sync-btn.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* Unread session indicator — static accent dot only (no background tint, no flash animation).
 * The user is already on the chat tab, so flashing is unnecessary and distracting.
 * A small dot is enough to indicate other sessions have unread messages.
 * Can stack with .has-running sweep light: unread = dot, running = sweep. */
.chat-action-btn.has-unread {
    position: relative;
}

.chat-action-btn.has-unread::after {
    content: '';
    position: absolute;
    top: 2px;
    right: 2px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent-color, #0066cc);
    z-index: 1;
}

/* Running session indicator — refined sweep light with accent color blend */
/* Stacks with .has-unread: sweep light (::before) + unread dot (::after) coexist */
.chat-action-btn.has-running {
    position: relative;
    overflow: hidden;
    color: var(--accent-color, #0066cc);
    background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, transparent);
}

/* When both unread and running, keep running's background as-is */
.chat-action-btn.has-unread.has-running {
}

.chat-action-btn.has-running:active {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 25%, transparent);
    transform: scale(0.92);
}

.chat-action-btn.has-running::before {
    content: '';
    position: absolute;
    top: 0;
    left: 0;
    width: 40%;
    height: 100%;
    transform: translateX(-140%);
    background: linear-gradient(
        90deg,
        transparent 0%,
        color-mix(in srgb, var(--accent-color, #0066cc) 12%, rgba(255,255,255,0.08)) 25%,
        color-mix(in srgb, var(--accent-color, #0066cc) 30%, rgba(255,255,255,0.22)) 50%,
        color-mix(in srgb, var(--accent-color, #0066cc) 12%, rgba(255,255,255,0.08)) 75%,
        transparent 100%
    );
    animation: sweep-light 2.4s cubic-bezier(0.4, 0, 0.2, 1) infinite;
}

@keyframes sweep-light {
    0% { transform: translateX(-40%); opacity: 0; }
    10% { opacity: 1; }
    90% { opacity: 1; }
    100% { transform: translateX(200%); opacity: 0; }
}

.chat-action-btn svg {
  flex-shrink: 0;
}

/* Unified input container */
.chat-input-container {
  display: flex;
  flex-direction: column;
  background: var(--bg-tertiary, #f0f0f0);
  flex: none;
  min-width: 0;
  border: none;
  border-radius: 20px;
  overflow: hidden;
  position: relative;
  transition: background 0.2s, box-shadow 0.2s;
}

.chat-input-container:focus-within {
  background: var(--bg-primary, #fff);
  box-shadow: 0 0 0 1px var(--accent-color, #0066cc);
}

.paste-overlay {
  position: absolute;
  inset: 0;
  z-index: 11;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, var(--bg-primary, #fff));
  color: var(--accent-color, #0066cc);
  font-size: 13px;
  font-weight: 500;
  border-radius: 20px;
  pointer-events: none;
}

.paste-spinner-icon {
  animation: paste-spin 0.8s linear infinite;
}

@keyframes paste-spin {
  to { transform: rotate(360deg); }
}

.paste-fade-enter-active,
.paste-fade-leave-active {
  transition: opacity 0.3s ease;
}
.paste-fade-enter-from,
.paste-fade-leave-to {
  opacity: 0;
}

/* Attach button (inside input row) */
.chat-attach-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: color 0.15s, background 0.15s;
}

.chat-attach-btn:hover:not(:disabled) {
  color: var(--accent-color, #0066cc);
  background: var(--bg-tertiary, #f0f0f0);
}

.chat-attach-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Clear input button (next to attach button) */
.chat-clear-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding: 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 4px;
  transition: color 0.15s, background 0.15s;
  flex-shrink: 0;
  align-self: flex-end;
}

.chat-clear-btn:hover {
  color: var(--danger-color, #dc3545);
  background: color-mix(in srgb, var(--danger-color, #dc3545) 8%, transparent);
}

/* Attachment tags row — horizontal scroll, no wrap */
.chat-attachment-tags {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: 6px;
  padding: 4px 6px;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.chat-attachment-tags::-webkit-scrollbar {
  display: none;
}

/* The nested container rendered by AttachmentTags adds its own 4px 6px padding,
   which would push normal attachment cards below the quote card. Zero it so the
   quote and file cards sit on the same horizontal line. */
.chat-attachment-tags :deep(.chat-attachment-tags) {
  padding: 0;
}

/* Conversation recommendation banner (推荐回复) — rendered above the input box */
.recommendation-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 0 6px;
  padding: 6px 10px;
  border-radius: 10px;
  background: var(--color-accent-soft, rgba(88, 120, 255, 0.12));
  border: 1px solid rgba(88, 120, 255, 0.35);
  font-size: 12px;
  color: var(--color-text-primary);
}

.recommendation-icon {
  flex-shrink: 0;
  color: var(--color-accent, #5878ff);
}

.recommendation-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  word-break: break-word;
}

.recommendation-text.expanded {
  white-space: normal;
}

.recommendation-accept {
  flex-shrink: 0;
  border: none;
  background: var(--color-accent, #5878ff);
  color: #fff;
  border-radius: 8px;
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
}

/* Base attachment card styles */
.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border-radius: 12px;
  height: 40px;
  padding: 0 8px;
  padding-right: 24px;
  flex-shrink: 0;
  max-width: 150px;
  position: relative;
  font-size: 12px;
  text-decoration: none;
  cursor: pointer;
  transition: opacity 0.15s;
  box-sizing: border-box;
}

.attachment-file-icon {
  flex-shrink: 0;
}

.attachment-filename {
  font-family: monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.attachment-filesize {
  font-size: 10px;
  color: var(--text-muted, #999);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Image-only card: square thumbnail */
.chat-file-attachment.attachment-image-only {
  width: 40px;
  height: 40px;
  padding: 0;
  overflow: hidden;
  border-radius: 10px;
}

.attachment-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

/* Quote icon */
.attachment-quote-icon {
  flex-shrink: 0;
}

/* Close button — inside card top-right, small circle */
.attachment-close-btn {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: none;
  background: rgba(0, 0, 0, 0.5);
  color: #fff;
  font-size: 10px;
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.15s;
  z-index: 1;
}

.attachment-close-btn:hover {
  background: var(--danger-color, #dc3545);
}

/* Input area attachment card style */
.chat-attachment-tags .attachment-ref {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 20%, transparent);
  color: var(--accent-color, #0066cc);
}

.chat-attachment-tags .attachment-ref .attachment-filename {
  color: var(--accent-color, #0066cc);
}

.chat-attachment-tags .attachment-ref:hover {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
}

/* Quote card — accent-colored, same size as file cards */
.chat-attachment-tags .attachment-quote {
  background: color-mix(in srgb, var(--accent-color, #4f9cf7) 8%, transparent);
  border: 1px dashed var(--accent-color, #4f9cf7);
  color: var(--accent-color, #4f9cf7);
  cursor: pointer;
}

.chat-attachment-tags .attachment-quote .attachment-filename {
  color: var(--accent-color, #4f9cf7);
}

.chat-attachment-tags .attachment-quote:hover {
  background: color-mix(in srgb, var(--accent-color, #4f9cf7) 15%, transparent);
}

/* Input row */
.chat-input-row {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  padding: 4px 6px 6px;
}

.chat-textarea {
  flex: 1;
  padding: 4px 8px;
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 16px;
  line-height: 20px;
  outline: none;
  resize: none;
  overflow-y: auto;
  min-height: 28px;
  max-height: calc(20px * 3 + 4px + 4px); /* 3 lines + padding-top + padding-bottom */
  font-family: inherit;
}

.chat-textarea::placeholder {
  color: var(--text-muted, #999);
}

.chat-textarea:disabled {
  opacity: 0.5;
}

.chat-send-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: 50%;
  cursor: pointer;
  transition: background 0.15s, opacity 0.15s, transform 0.15s;
  flex-shrink: 0;
}
.chat-send-btn:hover { background: #0055aa; }
.chat-send-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.chat-send-btn.disabled { opacity: 0.5; cursor: not-allowed; }

/* Send button in queue mode: orange to distinguish from normal send */
.chat-send-btn.queued {
  background: #e67e22;
}
.chat-send-btn.queued:hover { background: #d35400; }

/* Send button when input is empty: green lightning (quick-menu shortcut) */
.chat-send-btn.shortcut {
  background: #27ae60;
}
.chat-send-btn.shortcut:hover { background: #219a52; }

/* Stop button — default: dim red solid */
.chat-stop-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  background: color-mix(in srgb, var(--danger-color, #dc3545) 40%, transparent);
  color: color-mix(in srgb, #fff 60%, var(--danger-color, #dc3545));
  border: none;
  border-radius: 50%;
  cursor: pointer;
  transition: all 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
  flex-shrink: 0;
}
.chat-stop-btn:active { opacity: 0.75; }

/* Light theme: boost stop button default visibility */
:not([data-theme="dark"]) .chat-stop-btn:not(.primed):not(.cancelling) {
  background: color-mix(in srgb, var(--danger-color, #dc3545) 55%, transparent);
  color: color-mix(in srgb, #fff 75%, var(--danger-color, #dc3545));
}

/* Stop button — primed (first click, awaiting confirmation): bright red + heartbeat */
.chat-stop-btn.primed {
  background: var(--danger-color, #dc3545);
  color: #fff;
  transform: scale(1.15);
  animation: stop-heartbeat 0.8s ease-in-out infinite;
}

/* Stop button — cancelling (API request in flight): spinner, dimmed */
.chat-stop-btn.cancelling {
  background: color-mix(in srgb, var(--danger-color, #dc3545) 25%, transparent);
  color: color-mix(in srgb, #fff 50%, var(--danger-color, #dc3545));
  cursor: wait;
  animation: none;
  transform: none;
}

/* Pressed in primed state: scale feedback */
.chat-stop-btn.primed:active {
  transform: scale(1.0);
  animation: none;
}

@keyframes stop-heartbeat {
  0%, 100% { box-shadow: 0 0 0 0 rgba(220, 53, 69, 0.5); }
  50%      { box-shadow: 0 0 0 8px rgba(220, 53, 69, 0); }
}

.spin-icon {
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to   { transform: rotate(360deg); }
}

/* Voice recording indicator — red circle with animation, shown in the attach slot */
.chat-attach-btn.voice-rec-btn {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: #ff3b30;
  color: #fff;
  padding: 0;
  flex-shrink: 0;
}
.chat-attach-btn.voice-rec-btn:disabled {
  opacity: 1;
  cursor: default;
}
.chat-attach-btn.voice-rec-btn.transcribing {
  background: var(--accent-color, #0066cc);
}

/* Audio-wave animation: animated vertical bars */
.voice-wave {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  height: 14px;
}
.voice-wave i {
  display: block;
  width: 2px;
  height: 6px;
  border-radius: 1px;
  background: currentColor;
  animation: voice-wave 1s ease-in-out infinite;
}
.voice-wave i:nth-child(1) { animation-delay: 0s; }
.voice-wave i:nth-child(2) { animation-delay: 0.1s; }
.voice-wave i:nth-child(3) { animation-delay: 0.2s; }
.voice-wave i:nth-child(4) { animation-delay: 0.3s; }
.voice-wave i:nth-child(5) { animation-delay: 0.4s; }
@keyframes voice-wave {
  0%, 100% { height: 4px; }
  50% { height: 13px; }
}

.chat-action-label {
  font-size: 11px;
  line-height: 1.3;
}


</style>

<!-- Unscoped styles for teleported menu content (PopupMenu uses Teleport to body, scoped styles won't reach it) -->
<style>
/* Quick-send menu content styles */
.quick-send-title {
  padding: 6px 14px 2px;
  font-size: 11px;
  color: var(--text-muted, #999);
  font-weight: 500;
  letter-spacing: 0.3px;
}

.quick-send-item {
  display: block;
  width: 100%;
  padding: 8px 14px;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
  transition: background 0.12s, color 0.12s;
  position: relative;
  overflow: hidden;
}

.quick-send-item:hover {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

/* Quick-send: pressing state → subtle accent tint hints at long-press (fills input) */
.quick-send-item.qs-pressing {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}

/* Quick-send: progressive fill bar → long-press fills input box instead of sending */
.qs-fill-bar {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 3px;
  background: var(--accent-color, #0066cc);
  border-radius: 0 2px 2px 0;
  animation: qs-fill 500ms linear forwards;
}

@keyframes qs-fill {
  from { width: 0; }
  to { width: 100%; }
}
.quick-send-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: 3px 6px;
}

/* @ command autocomplete menu styles */
.at-menu-title {
  padding: 6px 12px;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted, #999);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.at-menu-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 12px;
  border: none;
  background: none;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}

.at-menu-item:hover,
.at-menu-item.at-menu-selected {
  background: color-mix(in srgb, var(--accent-color) 12%, transparent);
}

.at-menu-label {
  font-size: 13px;
  font-weight: 600;
  color: #8b5cf6;
  white-space: nowrap;
}

:root[data-theme="dark"] .at-menu-label {
  color: #a78bfa;
}

.at-menu-label.slash-label {
  color: #0ea5e9;
}

:root[data-theme="dark"] .at-menu-label.slash-label {
  color: #38bdf8;
}

.at-menu-label mark {
  background: rgba(255, 230, 0, 0.5);
  color: inherit;
  padding: 0 1px;
  font-weight: 700;
}

:root[data-theme="dark"] .at-menu-label mark {
  background: rgba(255, 230, 0, 0.35);
}

.at-menu-desc {
  font-size: 12px;
  color: var(--text-secondary, #495057);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Context usage detail popup */
.usage-popup {
  padding: 8px 12px;
  min-width: 180px;
}

.usage-popup-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.usage-popup-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.usage-popup-bar-track {
  flex: 1;
  height: 8px;
  border-radius: 4px;
  background: color-mix(in srgb, var(--text-primary) 15%, transparent);
  overflow: hidden;
}

.usage-popup-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.3s ease, background 0.3s ease;
}

.usage-popup-pct {
  font-size: 14px;
  font-weight: 700;
  flex-shrink: 0;
  min-width: 36px;
  text-align: right;
}

.usage-popup-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 3px 0;
  font-size: 12px;
}

.usage-popup-label {
  color: var(--text-secondary, #6c757d);
}

.usage-popup-value {
  color: var(--text-primary);
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}
</style>
