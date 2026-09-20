<template>
  <div class="chat-input-wrapper" ref="rootRef">
    <!-- Top action bar (above input box) -->
    <div class="chat-top-actions" ref="actionBarRef" :class="{ 'show-labels': showActionLabels }">
      <div class="chat-action-group">
        <span class="chat-group-label" :title="t('chat.actions.session')">
          {{ t('chat.actions.session') }}
        </span>
        <button class="chat-action-btn" data-action="session"
          :class="{ 'has-unread': chatUnreadCount > 0, 'has-running': chatRunning }"
          @click="$emit('open-session-tab', 'sessions')"
          :title="t('chat.actions.session')">
          <List :size="14" />
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.session') }}</span>
        </button>
        <button class="chat-action-btn"
          @click="handleCreateClick"
          @contextmenu.prevent="emit('create-session')"
          :title="t('chat.create.selectAgentOrLongPress')">
          <Plus :size="14" />
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.create') }}</span>
        </button>
        <button class="chat-action-btn"
          @click="$emit('open-session-search')"
          :title="t('chat.actions.sessionSearch')">
          <Search :size="14" />
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.search') }}</span>
        </button>
        <button class="chat-action-btn"
          @click="$emit('open-user-msg-index')"
          :title="t('chat.actions.userMsgIndex')">
          <MessagesSquare :size="14" />
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.jump') }}</span>
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
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.sync') }}</span>
        </button>
        <button class="chat-action-btn chat-action-btn-archive" :class="{ disabled: !currentSessionId }"
          @click="handleArchive"
          :title="currentSessionId ? t('chat.actions.archiveCurrentSession') : t('chat.actions.noSessionToArchive')">
          <Archive :size="14" />
          <span class="chat-action-label">{{ t('chat.actions.wideLabels.archive') }}</span>
        </button>
      </div>
      <button class="chat-action-btn auto-speech-btn" :class="{ active: autoSpeechEnabled }"
        @click="$emit('toggle-auto-speech')"
        :title="t('chat.actions.autoSpeech')">
        <Volume2 :size="14" />
        <span class="chat-action-label">{{ t('chat.actions.wideLabels.speak') }}</span>
      </button>
      <RefreshButton v-if="currentSessionId" data-action="refresh-session" class="chat-action-btn" :loading="refreshingSession" :title="t('chat.actions.reloadSession')" @click="$emit('refresh-session')">
        <span class="chat-action-label">{{ t('chat.actions.wideLabels.refresh') }}</span>
      </RefreshButton>
    </div>
    <!-- Conversation recommendation banner (推荐回复) — sits above the input box so it never steals input space -->
    <Transition name="recommend-slide">
      <div v-if="showRecommendationChip && recommendation" class="recommendation-chip">
        <Sparkles :size="13" :stroke-width="1.5" class="recommendation-icon" />
        <span class="recommendation-text" :class="{ expanded: recommendationExpanded }" @click="toggleRecommendationExpand" :title="recommendationExpanded ? t('chat.recommendationCollapse') : t('chat.recommendationExpand')">{{ recommendation }}</span>
        <button class="recommendation-accept" @click.stop="acceptRecommendation" :title="t('tool.askUser.recommendationFill')">{{ t('tool.askUser.recommendationFill') }}</button>
      </div>
    </Transition>
    <!-- Input container -->
    <div class="chat-input-container">
      <!-- Paste overlay (dynamic feedback while uploading pasted files from clipboard) -->
      <Transition name="paste-fade">
        <div v-if="isPasteOver" class="paste-overlay">
          <LoadingIndicator class="paste-spinner" size="sm" inline />
          <span>{{ t('chat.attach.uploading') }}</span>
        </div>
      </Transition>
      <!-- Attachment tags (horizontal scrollable cards — quote + pending uploads + attached file refs) -->
      <div v-if="hasAttachmentTags" class="chat-attachment-tags">
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
            <LoadingIndicator v-else class="attach-btn-spinner" size="sm" inline />
          </button>
          <button v-else class="chat-attach-btn" @click.stop="toggleAttachMenu" :disabled="inputDisabled" :title="t('chat.actions.attachment')">
            <Paperclip :size="15" />
          </button>
        </div>
        <textarea class="chat-textarea"
          :key="inputEpoch"
          ref="textareaRef"
          v-model="inputText"
          :disabled="inputDisabled"
          :placeholder="dynamicPlaceholder"
          rows="1"
          @keydown="onTextareaKeydown"
          @paste="onPaste"
          @focus="onTextareaFocus"
          @blur="onTextareaBlur"
          @beforeinput="onRecoveryBeforeInput"
          @input="onRecoveryInput"
          @touchstart.passive="onTextareaTouchStart"
          @touchend="onTextareaTouchEnd"
          @touchcancel="onTextareaTouchCancel"
          ></textarea>
        <button v-if="!stopPrimed" class="chat-send-btn" ref="sendBtnRef" :class="{ queued: loading, shortcut: !hasInputContent }" @click.stop="handleSendClick" @pointerdown="onSendPointerDown" @pointerup="onSendPointerUp" @pointerleave="onSendPointerUp" :title="!hasInputContent ? t('chat.input.quickMenu') : loading ? t('chat.input.enqueue') : t('chat.input.send')">
          <!-- Empty input: green lightning (quick-menu shortcut) -->
          <Zap v-if="!hasInputContent" :size="15" />
          <!-- Queue mode: inbox with down arrow (enqueue) -->
          <Inbox v-else-if="loading" :size="15" />
          <!-- Normal mode: paper plane (send) -->
          <Send v-else :size="15" />
        </button>
        <button v-if="loading" class="chat-stop-btn" :class="{ primed: stopPrimed, cancelling: cancelling }" @click="handleStopClick" :title="stopPrimed ? t('chat.input.confirmStop') : t('chat.input.stopGenerating')" :disabled="cancelling">
          <LoadingIndicator v-if="cancelling" class="stop-spinner" size="sm" inline />
          <Square v-else :size="15" fill="currentColor" />
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
        @file-open="(entry) => emit('file-tag-click', entry)"
      />
      <!-- Teleported quick-send menu -->
      <PopupMenu v-model:show="showQuickMenu" :target-element="sendBtnRef" :max-width="260" :max-height="280" :menu-items-count="quickSendItems.length + 1">
        <div class="quick-send-title">{{ t('chat.quickSend.title') }}</div>
        <button v-for="item in quickSendItems" :key="item.id"
          class="quick-send-item"
          @click="handleQuickSendClick(item)"
        >
          <span class="qs-label">{{ item.label }}</span>
          <span class="qs-cmd" :title="item.command">{{ item.command }}</span>
          <span class="qs-inject-btn" role="button" tabindex="-1"
            :title="t('chat.quickSend.injectToInput')"
            @click.stop="handleQuickSendInject(item)"
          >
            <TextCursorInput :size="14" />
          </span>
        </button>
        <div class="quick-send-divider" />
        <button class="quick-send-item" @click="showQuickMenu = false; quickSendDrawer.open()">
          <Settings :size="14" /> {{ t('chat.quickSend.edit') }}
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
      <!-- Unified completion menus (shared component).
           Slash commands ("/" prefix) and @ file references share the same
           interaction + rendering; only the trigger parser, data source and
           select behaviour differ. -->
      <CompletionMenu
        :items="commandMenuItems"
        :active-index="commandMenuIndex"
        :show="showCommandMenu"
        :target-element="textareaRef"
        @select="handleCommandSelect"
        @update:show="onCommandMenuShowChange"
      />
      <CompletionMenu
        :items="fileMenuItems"
        :active-index="fileMenuIndex"
        :show="showFileMenu"
        :target-element="textareaRef"
        @select="handleFileSelect"
        @update:show="onFileMenuShowChange"
      />
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
          <!-- Token detail section: every token/cost row grouped under one header.
               used/size/remaining (matching the progress bar above) stay outside.
               cacheHitTokens is deliberately not rendered — it duplicates
               cachedReadTokens ("缓存读"), shown once below. -->
          <div v-if="hasTokenDetail" class="usage-popup-section-title">{{ t('chat.sessionInfo.tokenDetail') }}</div>
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
          <div v-if="contextCacheHitTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheHitTokens') }}</span>
            <span class="usage-popup-value">{{ contextCacheHitTokens.toLocaleString() }}</span>
          </div>
          <div v-if="cacheHitRate !== null" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheHitRate') }}</span>
            <span class="usage-popup-value">{{ cacheHitRate.toFixed(1) }}%</span>
          </div>
          <div v-if="contextCachedWriteTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cachedWriteTokens') }}</span>
            <span class="usage-popup-value">{{ contextCachedWriteTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCacheCreationTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheCreationTokens') }}</span>
            <span class="usage-popup-value">{{ contextCacheCreationTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCacheMissTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.cacheMissTokens') }}</span>
            <span class="usage-popup-value">{{ contextCacheMissTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextThoughtTokens > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.thoughtTokens') }}</span>
            <span class="usage-popup-value">{{ contextThoughtTokens.toLocaleString() }}</span>
          </div>
          <div v-if="contextCredit > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.credit') }}</span>
            <span class="usage-popup-value">{{ contextCredit.toFixed(4) }}</span>
          </div>
          <div v-if="contextCost > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.contextCost') }}</span>
            <span class="usage-popup-value">{{ contextCostDisplay }}</span>
          </div>
          <!-- Context breakdown section (CodeBuddy usageByCategory) -->
          <template v-if="categoryRows.length">
            <div class="usage-popup-section-title">{{ t('chat.sessionInfo.categoryBreakdown') }}</div>
            <div v-for="row in categoryRows" :key="row.key" class="usage-popup-row">
              <span class="usage-popup-label">{{ row.label }}</span>
              <span class="usage-popup-value">{{ row.value.toLocaleString() }}</span>
            </div>
          </template>
          <div class="usage-popup-compact">
            <button class="usage-popup-compact-btn" @click.stop="handleCompact(); showUsagePopup = false" :title="t('chat.sessionInfo.compact')" :aria-label="t('chat.sessionInfo.compact')">
              <Minimize2 :size="13" />
              {{ t('chat.sessionInfo.compact') }}
            </button>
          </div>
        </div>
      </PopupMenu>
    </div>
    <!-- Session info bar (model + mode) — always rendered to reserve vertical space,
         preventing layout shift when async model/mode data loads after messages -->
    <div class="chat-session-info">
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

    </div>
  </div>
</template>

<script setup>
import { ref, computed, nextTick, watch, onBeforeUnmount, onMounted, defineAsyncComponent } from 'vue'
import { pendingChatInput as pendingChatInputRef, consumePendingChatInput } from '@/utils/chatInputInjection'
import { useI18n } from 'vue-i18n'
import { Code2, List, Plus, Search, Archive, Volume2, Paperclip, Inbox, Send, Square, Zap, Compass, Activity, MessagesSquare, Minimize2, Sparkles, ArrowRightLeft, Settings, TextCursorInput } from 'lucide-vue-next'
import { computeRecentReferencedFiles, isImeCompositionEvent } from '@/utils/chatInputUtils.ts'
import { fuzzyMatch, parseAtQuery, parseSlashQuery, buildFileCandidates } from '@/utils/completionMatch.ts'
import { normalizeFileEntry } from '@/utils/fileAttachmentUtils.ts'
import { joinPath } from '@/utils/path.ts'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import PopupMenu from '@/components/common/PopupMenu.vue'
import CompletionMenu from '@/components/common/CompletionMenu.vue'
import FileIcon from '@/components/common/FileIcon.vue'
import RefreshButton from '@/components/common/RefreshButton.vue'
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
import { usePlatformDetect } from '@/composables/usePlatformDetect'
import { useChatContext } from '@/composables/useChatContext'
import { setChatDraft, getChatDraft, hasChatDraft, deleteChatDraft } from '@/utils/chatDraftStore.ts'
import { useCompletionMenu } from '@/composables/useCompletionMenu'
import { useSelectAllDeleteRecovery } from '@/composables/useSelectAllDeleteRecovery'
import { useRecentFiles } from '@/composables/useRecentFiles'
import { useShareIn } from '@/composables/useShareIn'
import { useUploadRecent } from '@/composables/useUploadRecent'
import { store } from '@/stores/app.ts'
import { apiGet } from '@/utils/api'

const { t } = useI18n()
const { availableCommands, availableModes, currentTransport: sessionTransport, autoApprove, toggleAutoApprove, contextUsed, contextSize, contextInputTokens, contextOutputTokens, contextTotalTokens, contextCachedReadTokens, contextCachedWriteTokens, contextThoughtTokens, contextCost, contextCurrency, contextCacheCreationTokens, contextCacheHitTokens, contextCacheMissTokens, contextCredit, contextUsageByCategory } = useSessionIdentity()
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
// Cache hit rate = hit / (hit + miss). Shown as a percentage when at least one
// side is known. Returns null when neither is reported (no cache stats).
const cacheHitRate = computed(() => {
  const hit = contextCacheHitTokens.value
  const miss = contextCacheMissTokens.value
  if (hit <= 0 && miss <= 0) return null
  const total = hit + miss
  if (total <= 0) return 0
  return (hit / total) * 100
})
// True when any token/cost row inside the Token Detail section has a value.
// The section header only renders then — otherwise the popup would show an
// empty header between used/size/remaining and the context breakdown.
const hasTokenDetail = computed(() =>
  contextInputTokens.value > 0 || contextOutputTokens.value > 0 ||
  contextTotalTokens.value > 0 || contextCachedReadTokens.value > 0 ||
  contextCachedWriteTokens.value > 0 || contextCacheCreationTokens.value > 0 ||
  contextCacheHitTokens.value > 0 || contextCacheMissTokens.value > 0 ||
  contextThoughtTokens.value > 0 || contextCredit.value > 0 || contextCost.value > 0,
)
// Context breakdown rows from CodeBuddy usageByCategory (by value, i18n labels).
const categoryRows = computed(() => {
  const cat = contextUsageByCategory.value
  if (!cat) return []
  const labels = {
    conversation: 'chat.sessionInfo.catConversation',
    tools: 'chat.sessionInfo.catTools',
    systemPrompt: 'chat.sessionInfo.catSystemPrompt',
    skills: 'chat.sessionInfo.catSkills',
    mcp: 'chat.sessionInfo.catMCP',
  }
  return Object.entries(cat)
    .filter(([, v]) => (v ?? 0) > 0)
    .map(([key, value]) => ({ key, value: value ?? 0, label: labels[key] ? t(labels[key]) : key }))
})
// Currency symbols for common ISO 4217 codes. Unknown codes fall back to the
// bare code (e.g. "123 CNY"). When the backend reports an amount with NO
// currency (CodeBuddy sends cost.amount == credit with an empty currency),
// the row shows the bare number — no dollar sign, no currency code, nothing
// fabricated on top of what the agent actually reported.
const currencySymbols = { USD: '$', CNY: '¥', EUR: '€', GBP: '£', JPY: '¥', HKD: 'HK$', KRW: '₩', AUD: 'A$', CAD: 'C$' }
const contextCostDisplay = computed(() => {
  const amount = contextCost.value
  if (amount <= 0) return ''
  const cur = contextCurrency.value
  // Always two decimals — sub-cent amounts round to 0.00 rather than
  // switching to a higher-precision format.
  const formatted = amount.toFixed(2)
  if (!cur) return formatted
  const sym = currencySymbols[cur] ?? ''
  return sym ? `${sym}${formatted}` : `${formatted} ${cur}`
})
const dialog = useDialog()
const quickSendStore = useQuickSend()
const { items: quickSendItems, fetchItems } = quickSendStore
const settingsDrawerInitialTab = ref('model')
const quickSendDrawer = useTabDrawer('chat', quickSendStore.showEditDialog)
const settingsDrawer = useTabDrawer('chat')

// ── Rotating placeholder ──
const placeholderIndex = ref(0)
let placeholderTimer = null

// `!isPC` = mobile surface (Android/iOS app, phone/tablet browser, iPadOS) —
// exactly where the swipe-history gesture is available, so its hint is shown
// only there.
const { isPC } = usePlatformDetect()

// The candidate hints cycle when the textarea is empty, unfocused, and not in queue/upload mode.
// The plain "type a message" hint is omitted — it's implied by the empty input box.
// When quickSendItems exist, the cycle includes the quick-send tip; otherwise it's skipped.
const placeholderHints = computed(() => {
  const hints = []
  if (!isPC.value) {
    hints.push(t('chat.input.placeholderSwipeHistory'))
  }
  if (quickSendItems.value.length > 0) {
    hints.push(t('chat.input.placeholderQuickSend'))
  }
  hints.push(t('chat.input.placeholderCommand'))
  hints.push(t('chat.input.placeholderFileRef'))
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
  refreshingSession: Boolean,
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
  'refresh-session',
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
// is suppressed in handleSendClick.
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
const actionBarRef = ref(null)
/**
 * Action-bar labels visibility: labels are shown only when the chat pane is
 * wide enough to hold them. We force labels on, measure whether the action bar
 * overflows its container, and toggle a flag. This keeps labels off on narrow
 * chat panes regardless of wide-screen mode.
 */
const showActionLabels = ref(false)
let actionBarObserver = null
let actionBarMeasureTimer = null

function measureActionLabels() {
  const el = actionBarRef.value
  if (!el) return
  // Force the labels-on layout synchronously via .measure-labels (shows the
  // label spans) so scrollWidth reflects the intended final width — the group
  // label is always present — then compare against the available clientWidth.
  el.classList.add('measure-labels')
  const overflow = el.scrollWidth > el.clientWidth + 2
  el.classList.remove('measure-labels')
  showActionLabels.value = !overflow
}
function scheduleMeasureActionLabels() {
  if (actionBarMeasureTimer) clearTimeout(actionBarMeasureTimer)
  actionBarMeasureTimer = setTimeout(() => {
    actionBarMeasureTimer = null
    measureActionLabels()
  }, 50)
}
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
// ── Unified completion menus (slash commands + @ file references) ──
// Both menus share useCompletionMenu (state/keyboard/select) and
// CompletionMenu.vue (rendering); they differ only in trigger parsing, data
// source and select behaviour:
//   - slash: input starts with "/" and has no space; selecting writes the
//            command back and closes.
//   - at:    an "@" preceded by whitespace; selecting attaches the file,
//            removes the "@query" and keeps the menu open for multi-select.
const clawbenchCommands = computed(() => {
  return [
    { key: '/cb-chatsearch', label: '/cb-chatsearch', description: t('chat.clawbenchCommand.chatsearchDesc') },
    { key: '/cb-task', label: '/cb-task', description: t('chat.clawbenchCommand.taskDesc') },
    { key: '/cb-usage', label: '/cb-usage', description: t('chat.clawbenchCommand.usageDesc') },
  ]
})

// Slash candidates. Command names arrive inconsistently: CodeBuddy ACP reports
// skills slashless ("mmx-cli"), while pre-scanned names may keep a leading "/".
// Normalize for display and dedupe on the canonical (slash-stripped) name so
// the same command cannot appear twice.
const slashCandidates = computed(() => {
  const items = []
  for (const cmd of clawbenchCommands.value) {
    items.push({ key: cmd.key, label: cmd.label, description: cmd.description, source: 'clawbench' })
  }
  if (isACPTransport.value) {
    const toSlash = (name) => (name.startsWith('/') ? name : '/' + name)
    const seen = new Set()
    for (const cmd of availableCommands.value) {
      const canonical = cmd.name.startsWith('/') ? cmd.name.slice(1) : cmd.name
      if (!canonical || seen.has(canonical)) continue
      seen.add(canonical)
      items.push({ key: toSlash(cmd.name), label: toSlash(cmd.name), description: cmd.description, source: 'agent' })
    }
  }
  return items
})

const commandMenuItems = computed(() => {
  // Establish the reactive dependency on the caret position.
  void caretVersion.value
  const trigger = parseSlashQuery(inputText.value, currentCaret())
  const all = slashCandidates.value
  if (!trigger) return []
  if (!trigger.query) return all
  const matched = []
  for (const item of all) {
    const m = fuzzyMatch(trigger.query, item.label)
    if (!m) continue
    matched.push({ ...item, positions: m.positions, score: m.score })
  }
  matched.sort((a, b) => (b.score || 0) - (a.score || 0))
  return matched
})

const commandMenu = useCompletionMenu({
  items: commandMenuItems,
  getTrigger: () => parseSlashQuery(inputText.value, currentCaret()),
  getText: () => inputText.value,
  closeOnSelect: true,
  onSelect: () => {
    nextTick(() => textareaRef.value?.focus())
  },
  // Replace the typed "/query" token in place with "/command " so any text the
  // user already typed around the slash survives (mirrors the @ menu's
  // range-based edit; only the replacement differs).
  buildReplacement: (item) => item.key + ' ',
  applyText: (value, caret) => {
    inputText.value = value
    nextTick(() => {
      const el = textareaRef.value
      if (!el) return
      el.focus()
      el.setSelectionRange(caret, caret)
    })
  },
})

// ── @ file reference candidates ──
const recentFiles = useRecentFiles()
const { recentShares, fetchRecentShares } = useShareIn()
const { recentUploads, fetchRecentUploads } = useUploadRecent()
const fileSourcesLoaded = ref(false)

/**
 * Caret position for @-trigger parsing. The textarea's selectionStart is only
 * meaningful while it is focused and its DOM value matches the bound text;
 * otherwise (programmatic draft restore, blurred input) treat the caret as the
 * end of the text so a trailing "@query" still resolves.
 */
function currentCaret() {
  const el = textareaRef.value
  if (!el || el.value !== inputText.value) return inputText.value.length
  if (typeof el.selectionStart === 'number') return el.selectionStart
  return inputText.value.length
}

// selectionStart is a plain DOM property — Vue cannot track it. This counter is
// bumped on every caret move so computeds that parse the caret re-evaluate
// (otherwise the candidate list would stay filtered by the previous query).
const caretVersion = ref(0)

const fileMenuItems = computed(() => {
  // Establish the reactive dependency on the caret position.
  void caretVersion.value
  const sources = {
    recentOpen: recentFiles.entries.value.map(e => ({ path: e.path })),
    // Every entry type the listing can return — files, images AND directories.
    // Filtering on `type === 'file'` silently dropped images/PDFs (the backend
    // tags them 'image') and directories. isDir rides along so the select
    // handler can attach a directory with the right flag.
    currentDir: store.state.dirEntries.map(e => ({
      path: joinPath(store.state.currentDir, e.name),
      isDir: e.type === 'dir',
    })),
    recentRef: recentReferencedFiles.value.map(r => ({ path: r.path, isDir: r.isDir })),
    recentUpload: recentUploads.value.map(u => ({ path: u.path })),
    recentShare: recentShares.value.map(s => ({ path: s.path })),
  }
  const query = parseAtQuery(inputText.value, currentCaret())?.query ?? ''
  const attached = props.attachedFiles.map(f => f.path)
  // Every file row shows its type icon (the shared menu renders it when present).
  // The project root lets absolute attachment paths match relative candidates.
  return buildFileCandidates(sources, query, attached, store.state.projectRoot)
    .map(item => ({ ...item, icon: FileIcon }))
})

const fileMenu = useCompletionMenu({
  items: fileMenuItems,
  getTrigger: () => parseAtQuery(inputText.value, currentCaret()),
  getText: () => inputText.value,
  closeOnSelect: false,
  stickyAfterSelect: true,
  onSelect: (item) => {
    emit('add-attached', item.key, item.isDir === true)
  },
  applyText: (value, caret) => {
    inputText.value = value
    nextTick(() => {
      const el = textareaRef.value
      if (!el) return
      el.focus()
      el.setSelectionRange(caret, caret)
    })
  },
})

// Lazily load the share/upload sources the first time an @ trigger appears,
// then merge them in. Menus elsewhere only fetch these when the attach drawer
// opens. After the fetch resolves the candidate list changes, so the menu is
// refreshed — otherwise an @ typed while both sources (and the current dir)
// were empty would leave the menu hidden until the next keystroke.
//
// The refresh is gated on the textarea still being focused: this fetch is slow
// enough that the user can type "@", click away (blur closes the menu) and only
// then have it resolve. Without the guard the late refresh would resurrect a
// menu the user had already dismissed — refresh() has no way to tell "the menu
// was never shown" from "the user just closed it".
let fileSourcesPromise = null
function ensureFileSourcesLoaded() {
  if (fileSourcesLoaded.value) return fileSourcesPromise
  fileSourcesLoaded.value = true
  fileSourcesPromise = Promise.all([fetchRecentShares(), fetchRecentUploads()])
    .then(() => {
      if (isTextareaFocused.value) fileMenu.refresh()
    })
    .catch(() => {
      // Allow a later attempt after a transient failure.
      fileSourcesLoaded.value = false
      fileSourcesPromise = null
    })
  return fileSourcesPromise
}

// Menu visibility: driven by the composables' refresh(), which reads the
// current trigger from the text/caret. Kept as refs so the rest of the
// component (and existing tests) can observe them.
const showCommandMenu = commandMenu.show
const commandMenuIndex = commandMenu.activeIndex
const showFileMenu = fileMenu.show
const fileMenuIndex = fileMenu.activeIndex

function onCommandMenuShowChange(v) {
  if (!v) commandMenu.close()
}
function onFileMenuShowChange(v) {
  if (!v) fileMenu.close()
}

/**
 * Dismiss both completion popups together.
 *
 * The slash-command and @ file menus are the same kind of popup anchored to the
 * same textarea, so every dismissal site must close both — otherwise one menu
 * survives a lifecycle event that dismisses the other (the @ menu used to stay
 * open after a blank click or a tab switch, because only the command menu was
 * closed on blur).
 */
function closeCompletionMenus() {
  commandMenu.close()
  fileMenu.close()
}

// Recompute both menus whenever the input text changes. A history entry loaded
// by ArrowUp/ArrowDown must not pop the menu. currentCaret() tolerates the
// textarea DOM lagging the ref (programmatic restore), so this stays sync.
// Share/upload sources are fetched as soon as an @ trigger appears — not only
// once the menu is visible, so an empty current dir does not suppress the
// merge from those two remote sources.
watch(inputText, () => {
  if (historyNavSuppressMenu) return
  commandMenu.refresh()
  if (parseAtQuery(inputText.value, currentCaret())) ensureFileSourcesLoaded()
  fileMenu.refresh()
})

// Selection/caret moves (arrow keys inside the textarea, clicks) can enter or
// leave a trigger without changing the text — re-evaluate both menus on those
// events (both are caret-aware now).
// The counter is only bumped when the caret actually moved: applyText() calls
// setSelectionRange(), which synchronously fires selectionchange again, and an
// unconditional bump would loop forever.
let lastCaret = -1
function onTextareaSelectionChange() {
  if (historyNavSuppressMenu) return
  // selectionchange is a document-level event: it also fires when the user
  // selects text anywhere else — e.g. clicking a chat message to select a word.
  // The caret is only meaningful while the textarea owns focus. Refreshing on
  // an unfocused change reopens the menu that the blur just closed, so a click
  // on chat text appeared to "not close" the menu (it closed, then immediately
  // reopened) and needed several clicks to win.
  if (!isTextareaFocused.value) return
  const caret = currentCaret()
  if (caret !== lastCaret) {
    lastCaret = caret
    caretVersion.value++
  }
  if (parseAtQuery(inputText.value, caret)) ensureFileSourcesLoaded()
  commandMenu.refresh()
  fileMenu.refresh()
}

// Default to first item when the command list changes (VSCode-style).
watch(commandMenuItems, () => {
  if (commandMenu.show.value && commandMenuIndex.value < 0) commandMenuIndex.value = 0
})

// Scroll the highlighted item into view. Menus are teleported to <body>, so
// query from document rather than the component root.
function scrollActiveIntoView(idx) {
  if (idx < 0) return
  nextTick(() => {
    const el = document.querySelector('[data-completion-idx="' + idx + '"]')
    el?.scrollIntoView({ block: 'nearest' })
  })
}
watch(commandMenuIndex, scrollActiveIntoView)
watch(fileMenuIndex, scrollActiveIntoView)

function handleCommandSelect(cmd) {
  // Route through the composable so the trigger range is removed + menu closed
  // with the same code path as the keyboard. `source` disambiguates a built-in
  // and an agent command that share a name.
  commandMenu.selectByKey(cmd.key, cmd.source)
}

function handleFileSelect(item) {
  fileMenu.selectByKey(item.key)
}

// ── Input history navigation (ArrowUp/ArrowDown) ──────────
// Per-session, in-memory only. Derived from the session's persisted user
// messages (excludes pending optimistic bubbles and queued messages, matching
// the server-side user-message index source), newest first. Each entry carries
// both the text and the attached files so a history restore can rebuild both.
const historyInputs = computed(() => {
  const msgs = props.messages || []
  const list = []
  for (let i = msgs.length - 1; i >= 0; i--) {
    const m = msgs[i]
    if (m.role !== 'user' || m.pending || m.queued) continue
    const text = typeof m.content === 'string' ? m.content.trim() : ''
    const files = Array.isArray(m.files) ? m.files : []
    if (text) list.push({ text, files })
  }
  return list
})

// Navigation position. -1 = not navigating (fresh input). Valid positions walk
// the history from newest (0) to oldest (len-1).
const historyIndex = ref(-1)
// Input state captured when entering history navigation, so ArrowDown can return
// to what the user was typing (text + attachments) before they browsed history.
const historyDraft = ref({ text: '', files: [] })
// Suppress the @ / slash autocomplete menus while history navigation replaces
// the input programmatically (a history entry starting with @ or / must not
// pop the menu).
let historyNavSuppressMenu = false

function resetInputHistory() {
  historyIndex.value = -1
  historyDraft.value = { text: '', files: [] }
}

function textareaCursorRow(el) {
  const text = el.value
  const pos = el.selectionStart ?? text.length
  let row = 0
  for (let i = 0; i < pos; i++) {
    if (text[i] === '\n') row++
  }
  return row
}

// Shared history step used by both ArrowUp/ArrowDown and horizontal swipe on the
// textarea. `isUp` = newer → older (ArrowUp / swipe left), `false` = older →
// newer (ArrowDown / swipe right). `isGesture` skips the keyboard-only multiline
// caret guard (a swipe has no caret to protect). Returns true when the
// navigation consumed the event.
function stepHistory(isUp, isGesture = false) {
  const el = textareaRef.value
  if (!el) return false
  const text = inputText.value
  const rows = (text.match(/\n/g) || []).length + 1
  // In a multiline input, ArrowUp navigates history only from the first row and
  // ArrowDown only from the last row; elsewhere the arrows move the caret. A
  // swipe gesture has no caret conflict, so this guard applies to keyboard only.
  if (!isGesture && rows > 1) {
    const row = textareaCursorRow(el)
    if (isUp && row > 0) return false
    if (!isUp && row < rows - 1) return false
  }
  if (historyInputs.value.length === 0) return false

  const chatContext = useChatContext()
  const applyHistoryText = (entry) => {
    historyNavSuppressMenu = true
    inputText.value = entry.text
    // Restore the entry's attached files: replace the current attachments with
    // the history message's files (normalized, in case legacy string entries
    // are present).
    chatContext.attachedFiles.value = (entry.files || [])
      .map(f => normalizeFileEntry(f))
      .filter(f => f && f.path)
    // Reset the caret to the end so the multiline guard's cursor-row check
    // (selectionStart-based) never reads a stale position from the old text.
    const ta = textareaRef.value
    if (ta) {
      const end = entry.text.length
      ta.setSelectionRange(end, end)
    }
  }
  if (isUp) {
    // From fresh input (or while editing a history entry), capture the current
    // text and attachments once so ArrowDown can restore them.
    if (historyIndex.value === -1 && (text.trim() || chatContext.attachedFiles.value.length > 0)) {
      historyDraft.value = {
        text,
        files: chatContext.attachedFiles.value.slice(),
      }
    }
    historyIndex.value = Math.min(historyIndex.value + 1, historyInputs.value.length - 1)
    applyHistoryText(historyInputs.value[historyIndex.value])
  } else {
    if (historyIndex.value <= -1) {
      // Already at (or below) the draft position: a further ArrowDown must not
      // clear the input — the user's typed draft stays put so they can ArrowUp
      // back into history again.
      return false
    }
    historyIndex.value -= 1
    if (historyIndex.value < 0) {
      // Back to the fresh-input position: restore the captured draft. Keep the
      // draft intact (no reset) so the next ArrowDown stays here instead of
      // clearing the input.
      applyHistoryText(historyDraft.value)
    } else {
      applyHistoryText(historyInputs.value[historyIndex.value])
    }
  }
  nextTick(() => {
    historyNavSuppressMenu = false
  })
  return true
}

function handleHistoryKeydown(e) {
  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return false
  // Vertical swipe style: single step per gesture, but keyboard arrows must not
  // accidentally trigger on a repeat-held key while navigating within a
  // multiline textarea — the multiline guard in stepHistory handles that.
  if (!stepHistory(e.key === 'ArrowUp')) return false
  e.preventDefault()
  return true
}

// ── Horizontal swipe history navigation (touch on the textarea, inactive only) ──
// When the textarea is NOT focused, left/right swipes step the input history
// (left = newer → older, right = older → newer). An inactive textarea has no
// caret/scroll conflicts, so horizontal swipe is a clean, low-conflict gesture.
// While focused, the keyboard ArrowUp/ArrowDown history navigation applies
// instead (swipes are left untouched so the input keeps native touch behavior).
const SWIPE_HISTORY_THRESHOLD = 60 // px horizontal
const SWIPE_HISTORY_MAX_DURATION = 400 // ms

let swipeStartX = 0
let swipeStartY = 0
let swipeStartTime = 0

function resetSwipeGesture() {
  swipeStartX = 0
  swipeStartY = 0
  swipeStartTime = 0
}

function onTextareaTouchStart(e) {
  if (props.inputDisabled) return
  // Only the inactive textarea is a swipe surface; while focused the input
  // keeps native touch behavior (caret placement, text scroll).
  if (isTextareaFocused.value) return
  const touch = e.touches[0]
  if (!touch) return
  if (e.touches.length !== 1) {
    resetSwipeGesture()
    return
  }
  swipeStartX = touch.clientX
  swipeStartY = touch.clientY
  swipeStartTime = Date.now()
}

function onTextareaTouchEnd(e) {
  const touch = e.changedTouches[0]
  if (!touch) return
  const deltaX = touch.clientX - swipeStartX
  const deltaY = touch.clientY - swipeStartY
  const duration = Date.now() - swipeStartTime
  resetSwipeGesture()
  if (duration > SWIPE_HISTORY_MAX_DURATION) return
  // Horizontal-dominant only: ignore vertical swipes (|deltaY| >= |deltaX|) so
  // scrolling the message list keeps working naturally.
  if (Math.abs(deltaY) >= Math.abs(deltaX)) return
  if (Math.abs(deltaX) < SWIPE_HISTORY_THRESHOLD) return
  // Swipe left = newer → older (deltaX < 0), swipe right = older → newer.
  // Gesture path skips the keyboard-only multiline caret guard.
  stepHistory(deltaX < 0, true)
}

function onTextareaTouchCancel() {
  resetSwipeGesture()
}

function onTextareaKeydown(e) {
  // IME composition (e.g. Chinese pinyin candidate selection): the browser/IME
  // owns the keystroke — Enter commits the candidate to the input instead of
  // submitting the message.
  if (isImeCompositionEvent(e)) return
  // Menu keyboard navigation takes priority (slash menu first, then @ files).
  if (commandMenu.handleKeydown(e)) return
  if (fileMenu.handleKeydown(e)) return
  // Input history navigation (ArrowUp/ArrowDown), only when the input is active
  if (handleHistoryKeydown(e)) return
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

// Per-session draft cache: save input text when switching away, restore when
// switching back. Backed by a module-level store (not component state) so the
// draft survives the component remount caused by an SPA project switch — see
// chatDraftStore.ts for why.

// Android WebView recovery. Deleting a non-collapsed selection in one go kills
// the IME's InputConnection, after which every keystroke is dropped silently —
// the chat input becomes unresponsive until its textarea is rebuilt.
// `inputEpoch` is the textarea's :key; the rebuild is what restores input.
//
// The rebuild removes the focused element, so the browser fires `blur` and
// `onTextareaBlur` runs: it closes both completion menus and calls
// chatKeyboard.debounceDeactivate(). Closing the menus is what we want anyway —
// the deleted selection took the `@query`/`/query` trigger with it. The
// debounced deactivate is cancelled by the focus() below, which lands in the
// same nextTick and well inside its 150ms window.
const {
  inputEpoch,
  onBeforeInput: onRecoveryBeforeInput,
  onInput: onRecoveryInput,
  reset: resetInputRecovery,
} = useSelectAllDeleteRecovery({
  getElement: () => textareaRef.value,
  onRebuilt: () => {
    // focus() re-fires onTextareaFocus, but not before this runs — set the flag
    // now so the selectionchange guard sees a focused textarea and refreshes
    // the menus' trigger state.
    isTextareaFocused.value = true
    // The inline height lived on the removed node; recompute it for the new one.
    autoResizeTextarea()
  },
})

watch(() => props.currentSessionId, (newId, oldId) => {
  // History navigation is per-session: a session switch must start fresh from
  // the new session's newest history entry.
  resetInputHistory()
  // A detection whose input event never arrived must not survive into the next
  // session's first keystroke.
  resetInputRecovery()
  // Save draft from the old session
  if (oldId) {
    const text = inputText.value
    if (text) {
      setChatDraft(oldId, text)
    }
    // Don't delete existing draft when inputText is empty — saveDraft() may have
    // already saved it before clearInputPreserveDraft() cleared the visible text.
    // Only clearInput() (called after message send) explicitly deletes the draft.
  }
  // Restore draft for the new session (or clear if none). This also runs on the
  // remount after a project switch: currentSessionId is reset to '' first, then
  // initSessionFromAPI() sets the restored session id, firing this watcher.
  inputText.value = newId ? getChatDraft(newId) : ''
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

// The tags row must render only when it has VISIBLE children. pendingFiles
// retains completed (non-uploading) entries as a mirror of attachedFiles, but
// AttachmentTags only draws in-flight ones — counting those mirrors here would
// mount a childless container whose padding shows as dead vertical space.
const hasAttachmentTags = computed(() =>
  quoteItems.value.length > 0
  || props.attachedFiles.length > 0
  || pendingFiles.value.some(f => f.uploading),
)

// Extract recently referenced files from message history
const recentReferencedFiles = computed(() => {
  return computeRecentReferencedFiles(props.messages, props.attachedFiles, props.currentFile?.path)
})

function handleCreateClick() {
  // Always open the agent selector, even with a single agent — a one-tap
  // "create" here is easy to mis-tap on mobile and would create an empty
  // session. Requiring an explicit agent selection prevents accidental
  // session creation. 始终弹智能体选择器（哪怕只有一个智能体）——
  // 一次误触即建空会话在移动端很容易发生，强制选择智能体可避免误建。
  emit('show-agent-selector')
}

async function handleArchive() {
  if (!props.currentSessionId) return
  const confirmed = await dialog.confirm(t('chat.archive.confirm'), {
    confirmText: t('chat.actions.archiveSession'),
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
  // Line-height resolves to px (--input-line-height is a px value). Fall back to
  // that token's own 18px rather than a ratio, so the cap stays a whole number
  // if getComputedStyle ever comes back empty.
  const lineHeight = parseFloat(computed.lineHeight) || 18
  const paddingTop = parseFloat(computed.paddingTop) || 0
  const paddingBottom = parseFloat(computed.paddingBottom) || 0
  const maxContentHeight = lineHeight * 10
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
  // Close BOTH completion menus when the textarea loses focus (clicking menu
  // items uses @mousedown.prevent so blur won't fire for those interactions).
  // Both menus share one lifecycle: they are the same kind of popup anchored to
  // the same textarea, so a blur that dismisses one must dismiss the other.
  // Closing only the command menu left the @ file menu hovering after a click
  // on blank space or a switch to another tab.
  nextTick(() => {
    closeCompletionMenus()
  })
}

// Watch inputText changes (both user input and programmatic changes like draft restore)
// to ensure textarea height stays in sync with content
watch(inputText, () => nextTick(() => autoResizeTextarea()))

// Drain text queued by other features (e.g. "analyze this issue/PR"). The
// producer may run before this component is mounted, so the value is polled
// reactively rather than delivered via an event.
watch(pendingChatInputRef, () => {
  const pending = consumePendingChatInput()
  if (pending) injectToInput(pending)
}, { immediate: true })

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
    deleteChatDraft(props.currentSessionId)
  }
  // A new message starts fresh history navigation from the newest entry.
  resetInputHistory()
}

/** Restore the input text after a failed send, including its draft entry.
 *  Used when a message could not be delivered (network down / 5xx) — the text
 *  must not silently disappear. */
function restoreInput(text) {
  inputText.value = text ?? ''
  if (props.currentSessionId && text) {
    setChatDraft(props.currentSessionId, text)
  }
}

/** Save current input text to draft cache without clearing it (called before session switch). */
function saveDraft() {
  if (props.currentSessionId) {
    setChatDraft(props.currentSessionId, inputText.value)
  }
}

/** Clear visible input text but preserve the draft cache (used during session switch). */
function clearInputPreserveDraft() {
  inputText.value = ''
}

function handleAttachFile(filePath, isDir) {
  emit('add-attached', filePath, isDir)
}

/** Remove an attached reference card. Payload is either the full FileEntry
 *  (AttachmentTags cards — a line-range reference removes only its own range)
 *  or a bare path string (AttachDrawer whole-file toggles). */
function handleRemoveAttached(entryOrPath) {
  const entry = typeof entryOrPath === 'string' ? { path: entryOrPath } : entryOrPath
  emit('remove-attached-by-path', entry)
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

// — Quick-send actions →
function handleQuickSendClick(item) {
  showQuickMenu.value = false
  emit('send', item.command)
}

function handleQuickSendInject(item) {
  injectToInput(item.command)
  showQuickMenu.value = false
}

function injectToInput(text) {
  const current = inputText.value.trim()
  inputText.value = current ? current + '\n' + text : text
  nextTick(() => {
    textareaRef.value?.focus()
  })
}

/** Replace the input content with the given text and focus the textarea.
 *  Used by the rewind/回溯 prefill: after truncating a session at an earlier
 *  assistant message, the first removed user message is restored here for
 *  re-editing (replace + focus, unlike injectToInput which appends). */
function prefillInput(text) {
  inputText.value = text ?? ''
  if (props.currentSessionId) {
    setChatDraft(props.currentSessionId, text ?? '')
  }
  resetInputHistory()
  nextTick(() => {
    textareaRef.value?.focus()
  })
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
watch(() => attachDrawer.isOpen.value, (v) => { if (v) { showQuickMenu.value = false; settingsDrawer.close(); closeCompletionMenus(); showUsagePopup.value = false } })
watch(showQuickMenu, (v) => { if (v) { attachDrawer.close(); settingsDrawer.close(); closeCompletionMenus(); showUsagePopup.value = false } })
watch(() => settingsDrawer.isOpen.value, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; closeCompletionMenus(); showUsagePopup.value = false } })
watch(showCommandMenu, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; settingsDrawer.close(); showUsagePopup.value = false; fileMenu.close() } })
watch(showFileMenu, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; settingsDrawer.close(); showUsagePopup.value = false; commandMenu.close() } })
watch(showUsagePopup, (v) => { if (v) { attachDrawer.close(); showQuickMenu.value = false; settingsDrawer.close(); closeCompletionMenus() } })

onMounted(() => {
  fetchItems()
  startPlaceholderRotation()
  window.addEventListener('paste', handleWindowPaste, true)
  window.addEventListener('keydown', onVoiceShortcutDown)
  window.addEventListener('keyup', onVoiceShortcutUp)
  window.addEventListener('blur', onVoiceBlurStop)
  window.addEventListener('clawbench-recommendation', onRecommendationEvent)
  // Caret moves (clicks / arrow keys) can enter or leave an @ trigger without
  // changing the text, so the @ menu must re-evaluate on selection changes.
  document.addEventListener('selectionchange', onTextareaSelectionChange)
  measureActionLabels()
  if (typeof ResizeObserver !== 'undefined' && actionBarRef.value) {
    actionBarObserver = new ResizeObserver(() => scheduleMeasureActionLabels())
    actionBarObserver.observe(actionBarRef.value)
  }
})

onBeforeUnmount(() => {
  // The SPA project switch (App.vue hotSwitchProject) calls resetIdentity() and
  // changes :key="projectKey" in the SAME synchronous tick. Vue then replaces
  // the whole keyed subtree, so this component is unmounted with its props still
  // pointing at the OLD session — the currentSessionId watcher above never
  // observes the change and never saves. Persist the draft here so it survives
  // the remount and can be restored when the user switches back.
  // Only write when there is text: an empty input must not delete a draft that
  // saveDraft() already stored before clearInputPreserveDraft() hid the text.
  if (props.currentSessionId && inputText.value) {
    setChatDraft(props.currentSessionId, inputText.value)
  }
  pasteUploadGeneration++
  window.removeEventListener('paste', handleWindowPaste, true)
  window.removeEventListener('keydown', onVoiceShortcutDown)
  window.removeEventListener('keyup', onVoiceShortcutUp)
  window.removeEventListener('blur', onVoiceBlurStop)
  window.removeEventListener('clawbench-recommendation', onRecommendationEvent)
  document.removeEventListener('selectionchange', onTextareaSelectionChange)
  stopMachine.destroy()
  if (voicePressTimer) {
    clearTimeout(voicePressTimer)
    voicePressTimer = null
  }
  voiceInput.cancel()
  clearTimeout(pasteOverlayTimer)
  if (actionBarObserver) {
    actionBarObserver.disconnect()
    actionBarObserver = null
  }
  if (actionBarMeasureTimer) {
    clearTimeout(actionBarMeasureTimer)
    actionBarMeasureTimer = null
  }
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
  restoreInput,
  saveDraft,
  clearInputPreserveDraft,
  clearRecommendation,
  inputText,
  deleteDraft: (sessionId) => { deleteChatDraft(sessionId) },
  hasDraft: (sessionId) => hasChatDraft(sessionId),
  getDraft: (sessionId) => (hasChatDraft(sessionId) ? getChatDraft(sessionId) : null),
  injectToInput,
  prefillInput,
  handleQuickSendClick,
  handleQuickSendInject,
  handleArchive,
  measureActionLabels,
})
</script>

<style scoped>
/* Outer wrapper: top actions + input box stacked vertically */
.chat-input-wrapper {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  margin:0 0 var(--space-4);
  padding: var(--space-4) var(--space-4) 0;
  box-shadow: inset 0 1px 0 var(--border-color, #e5e5e5);
}

/* Session info bar (model + mode, below input box) */
.chat-session-info {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4) 0;
  font-size: var(--font-size-xs);
  line-height: var(--line-height-snug);
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
  transition: color var(--duration-base);
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



.usage-bar {
  position: relative;
  width: 28px;
  height: 6px;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-primary) 18%, transparent);
  overflow: hidden;
  flex-shrink: 0;
}

.usage-bar-fill {
  position: absolute;
  left: 0;
  top: 0;
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease, background 0.3s ease;
}

/* Top action bar (above input box, compact) */
.chat-top-actions {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-1) var(--space-2) var(--space-3);
  /* When labels are briefly rendered during measurement (or when the chat pane
     is too narrow), allow horizontal scroll with a hidden scrollbar instead of
     clipping the trailing buttons. */
  overflow-x: auto;
  overflow-y: hidden;
  scrollbar-width: none;
}
.chat-top-actions::-webkit-scrollbar {
  display: none;
}

/* Wide-screen short label next to the action icon. Always in the DOM so the
   action bar can be measured with labels forced on (see .measure-labels);
   hidden unless the container proves it has room for them. */
.chat-action-label {
  font-size: var(--font-size-xs);
  line-height: 1;
  white-space: nowrap;
  flex-shrink: 0;
  display: none;
}
.chat-top-actions.show-labels .chat-action-label,
.chat-top-actions.measure-labels .chat-action-label {
  display: inline-block;
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
    padding:5px var(--space-3);
    color: var(--text-muted, #999);
    background: var(--bg-tertiary, #f0f0f0);
    pointer-events: none;
    user-select: none;
    border-right: 1px solid var(--border-color, #e5e5e5);
    font-size: var(--font-size-xs);
    line-height: 1.3;
}

.chat-action-group .chat-action-btn:last-child {
    border-radius: 0 var(--radius-full) var(--radius-full) 0;
}

.chat-action-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding:5px var(--space-4);
  border-radius: var(--radius-xs);
  font-size: var(--font-size-xs);
  line-height: 1;
  transition: color var(--duration-base), background var(--duration-base), transform var(--duration-fast);
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

.chat-action-btn:disabled {
  cursor: not-allowed;
  opacity: var(--opacity-disabled);
  color: var(--text-muted, #999);
}

.chat-action-btn-archive:not(.disabled) {
  color: var(--text-muted, #999);
}

@media (hover: hover) {
  .chat-action-btn-archive:not(.disabled):hover {
    color: var(--color-orange);
    background: color-mix(in srgb, var(--color-orange) 10%, transparent);
  }
}

.chat-action-btn-archive:not(.disabled):active {
  color: var(--color-orange);
  background: color-mix(in srgb, var(--color-orange) 18%, transparent);
  transform: scale(0.92);
}

.chat-action-btn-archive.disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
}

.acp-sync-btn.disabled {
  opacity: var(--opacity-disabled);
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

/* Running session indicator — a sweep of light travelling across the button.
 * Deliberately not the session list's bottom band: this is a chip on the input
 * bar rather than a full-width list row, so a sweep reads well here without
 * the flooding problem that ruled it out for the rows (see --running-sweep in
 * variables.css).
 * Stacks with .has-unread: sweep (::before) + unread dot (::after) coexist. */
.chat-action-btn.has-running {
    position: relative;
    overflow: hidden;
    color: var(--accent-color, #0066cc);
}

.chat-action-btn.has-running:active {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 25%, transparent);
    transform: scale(0.92);
}

.chat-action-btn.has-running::before {
    content: '';
    position: absolute;
    top: 0;
    left: -60%;
    width: 60%;
    height: 100%;
    background: linear-gradient(90deg, transparent, var(--running-sweep), transparent);
    animation: sweep-light 2s ease-in-out infinite;
    pointer-events: none;
    z-index: 0;
}

@keyframes sweep-light {
    0% { left: -60%; }
    100% { left: 100%; }
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
  transition: background var(--duration-slow), box-shadow var(--duration-slow);
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
  gap: var(--space-4);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 8%, var(--bg-primary, #fff));
  color: var(--accent-color, #0066cc);
  font-size: var(--font-size-md);
  font-weight: var(--font-weight-medium);
  border-radius: 20px;
  pointer-events: none;
}

/* Voice transcribing state: replaced a rotating icon; use the unified loader.
   currentColor → the arc picks up the button's own text color. */
.chat-attach-btn .attach-btn-spinner {
  --li-color: currentColor;
}

/* Cancelling state of the stop button — spinner inside the danger-tinted pill */
.chat-stop-btn .stop-spinner {
  --li-color: currentColor;
}

.paste-fade-enter-active,
.paste-fade-leave-active {
  transition: opacity 0.3s ease;
}
.paste-fade-enter-from,
.paste-fade-leave-to {
  opacity: 0;
}

/* Attach button (inside input row).
   A square box the same size as the send/stop buttons, so the row's
   align-items: flex-end lines all three controls up on one baseline. Sizing it
   to the icon instead would leave the paperclip hanging below the text line. */
.chat-attach-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  width: 26px;
  height: 26px;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radius-xs);
  transition: color var(--duration-base), background var(--duration-base);
  flex-shrink: 0;
}

@media (hover: hover) {
  .chat-attach-btn:hover:not(:disabled) {
    color: var(--accent-color, #0066cc);
    background: var(--bg-tertiary, #f0f0f0);
  }
}

.chat-attach-btn:disabled {
  opacity: var(--opacity-muted);
  cursor: not-allowed;
}

/* Attachment tags row — horizontal scroll, no wrap */
.chat-attachment-tags {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
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

/* Conversation recommendation banner slide transition — animates height + opacity so the message area isn't jolted */
.recommend-slide-enter-active {
  transition: max-height 0.25s ease-out, opacity 0.25s ease-out, margin 0.25s ease-out, padding-top 0.25s ease-out, padding-bottom 0.25s ease-out, border-width 0.25s ease-out;
  overflow: hidden;
}
.recommend-slide-leave-active {
  transition: max-height 0.25s ease-in, opacity 0.25s ease-in, margin 0.25s ease-in, padding-top 0.25s ease-in, padding-bottom 0.25s ease-in, border-width 0.25s ease-in;
  overflow: hidden;
}
.recommend-slide-enter-from,
.recommend-slide-leave-to {
  max-height: 0;
  opacity: 0;
  margin-top: 0;
  margin-bottom: 0;
  padding-top: 0;
  padding-bottom: 0;
  border-width: 0;
}
.recommend-slide-enter-to,
.recommend-slide-leave-from {
  max-height: 600px;
}

/* Respect users who prefer reduced motion — snap the banner in/out instead of
   animating height/opacity. */
@media (prefers-reduced-motion: reduce) {
  .recommend-slide-enter-active,
  .recommend-slide-leave-active {
    transition: none;
  }
}

/* Conversation recommendation banner (推荐回复) — rendered above the input box.
   Shorter than the original (tight line box instead of the inherited 1.6 body
   leading) but still airy: the padding keeps a full 6px above and below, so the
   single line of text does not touch the border. */
.recommendation-chip {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin: 0 0 var(--space-3);
  padding: var(--space-3) var(--space-5);
  border-radius: var(--radius-sm);
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 35%, transparent);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-tight);
  color: var(--text-primary);
}

.recommendation-icon {
  flex-shrink: 0;
  color: var(--accent-color, #0066cc);
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
  background: var(--accent-color, #0066cc);
  color: #fff;
  border-radius: var(--radius-xs);
  padding: var(--space-1) var(--space-4);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-tight);
  cursor: pointer;
}

/* Base attachment card styles */
.chat-file-attachment {
  display: inline-flex;
  align-items: center;
  gap: var(--space-3);
  border-radius: var(--radius-lg);
  height: 40px;
  padding:0 var(--space-4);
  padding-right: 24px;
  flex-shrink: 0;
  max-width: 150px;
  position: relative;
  font-size: var(--font-size-sm);
  text-decoration: none;
  cursor: pointer;
  transition: opacity var(--duration-base);
  box-sizing: border-box;
}

.attachment-file-icon {
  flex-shrink: 0;
}

.attachment-filename {
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.attachment-filesize {
  font-size: var(--font-size-2xs);
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
  border-radius: var(--radius-md);
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
  font-size: var(--font-size-2xs);
  line-height: 1;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--duration-base);
  z-index: 1;
}

@media (hover: hover) {
  .attachment-close-btn:hover {
    background: var(--color-red);
  }
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

@media (hover: hover) {
  .chat-attachment-tags .attachment-ref:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
  }

  .chat-attachment-tags .attachment-quote:hover {
    background: color-mix(in srgb, var(--accent-color, #4f9cf7) 15%, transparent);
  }
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

/* Input row.
   The vertical padding is symmetric on purpose: the row is `align-items:
   flex-end`, so any top/bottom difference shows up directly as the buttons
   sitting off-centre against the textarea. It used to be 4px top / 6px bottom
   (an extra 2px of bottom breathing room from the original design), which went
   unnoticed while the controls were larger but became visible once the buttons
   shrank to 26px squares.

   5px is a literal rather than a token because the spacing scale steps 4px → 6px
   (--space-2 → --space-3) with nothing between; 5px keeps the row's total height
   at 36px while making the padding equal. */
.chat-input-row {
  display: flex;
  align-items: flex-end;
  gap: var(--space-1);
  padding: 5px var(--space-3);
}

.chat-textarea {
  flex: 1;
  padding: var(--space-2) var(--space-4);
  border: none;
  background: transparent;
  color: var(--text-primary);
  /* Same type scale as the message body (.chat-message) so what you type reads
     as part of the conversation rather than a separate, larger surface. The
     line box is the integer --input-line-height rather than the unitless
     --line-height-snug, and the caps derive from it, so a single line stays
     vertically centred in WebView (see the token's comment). */
  font-size: var(--font-size-md);
  line-height: var(--input-line-height);
  outline: none;
  resize: none;
  overflow-y: auto;
  min-height: calc(var(--input-line-height) + var(--space-2) * 2);
  max-height: calc(var(--input-line-height) * 10 + var(--space-2) * 2); /* 10 lines + padding-top + padding-bottom */
  font-family: inherit;
}

.chat-textarea::placeholder {
  color: var(--text-muted, #999);
}

.chat-textarea:disabled {
  opacity: var(--opacity-muted);
}

.chat-send-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  background: var(--accent-color, #0066cc);
  color: #fff;
  border: none;
  border-radius: 50%;
  cursor: pointer;
  transition: background var(--duration-base), opacity var(--duration-base), transform var(--duration-base);
  flex-shrink: 0;
}
@media (hover: hover) {
  .chat-send-btn:hover { background: #0055aa; }
}
.chat-send-btn:disabled { opacity: var(--opacity-muted); cursor: not-allowed; }
.chat-send-btn.disabled { opacity: var(--opacity-muted); cursor: not-allowed; }

/* Send button in queue mode: orange to distinguish from normal send */
.chat-send-btn.queued {
  background: #e67e22;
}
@media (hover: hover) {
  .chat-send-btn.queued:hover { background: #d35400; }
}

/* Send button when input is empty: green lightning (quick-menu shortcut) */
.chat-send-btn.shortcut {
  background: #27ae60;
}
@media (hover: hover) {
  .chat-send-btn.shortcut:hover { background: #219a52; }
}

/* Stop button — default: dim red solid */
.chat-stop-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  padding: 0;
  background: color-mix(in srgb, var(--color-red) 40%, transparent);
  color: color-mix(in srgb, #fff 60%, var(--color-red));
  border: none;
  border-radius: 50%;
  cursor: pointer;
  transition: all 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
  flex-shrink: 0;
}
.chat-stop-btn:active { opacity: var(--opacity-soft); }

/* Light theme: boost stop button default visibility */
:not([data-theme-base="dark"]) .chat-stop-btn:not(.primed):not(.cancelling) {
  background: color-mix(in srgb, var(--color-red) 55%, transparent);
  color: color-mix(in srgb, #fff 75%, var(--color-red));
}

/* Stop button — primed (first click, awaiting confirmation): bright red + heartbeat */
.chat-stop-btn.primed {
  background: var(--color-red);
  color: #fff;
  transform: scale(1.15);
  animation: stop-heartbeat 0.8s ease-in-out infinite;
}

/* Stop button — cancelling (API request in flight): spinner, dimmed */
.chat-stop-btn.cancelling {
  background: color-mix(in srgb, var(--color-red) 25%, transparent);
  color: color-mix(in srgb, #fff 50%, var(--color-red));
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

/* Voice recording indicator — red circle with animation, shown in the attach
   slot. Geometry (26px square) comes from the base .chat-attach-btn rule; only
   the shape and colour differ. */
.chat-attach-btn.voice-rec-btn {
  border-radius: 50%;
  background: #ff3b30;
  color: #fff;
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
  gap: var(--space-1);
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


</style>

<!-- Unscoped styles for teleported menu content (PopupMenu uses Teleport to body, scoped styles won't reach it) -->
<style>
/* Quick-send menu content styles */
.quick-send-title {
  padding: var(--space-3) 14px var(--space-1);
  font-size: var(--font-size-xs);
  color: var(--text-muted, #999);
  font-weight: var(--font-weight-medium);
  letter-spacing: 0.3px;
}

.quick-send-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-2) 14px;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  text-align: left;
  transition: background var(--duration-base), color var(--duration-base);
  position: relative;
  overflow: hidden;
  /* Clicking the row sends directly; the trailing icon injects into the input box. */
  user-select: none;
  -webkit-user-select: none;
  -webkit-touch-callout: none;
}

@media (hover: hover) {
  .quick-send-item:hover {
    background: var(--accent-color, #0066cc);
    color: #fff;
  }
}

/* Label (flex-shrink 0) + command (ellipsis) + trailing inject icon */
.qs-label {
  flex-shrink: 0;
  font-weight: var(--font-weight-medium);
  max-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.qs-cmd {
  flex: 1;
  min-width: 0;
  color: var(--text-muted, #999);
  font-family: var(--font-mono);
  font-size: var(--font-size-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Trailing "add to input box" icon button */
.qs-inject-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  border-radius: var(--radius-xs);
  color: var(--text-muted, #999);
  cursor: pointer;
  transition: background var(--duration-base), color var(--duration-base);
}

@media (hover: hover) {
  .qs-inject-btn:hover {
    background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
    color: var(--accent-color, #0066cc);
  }
}

.quick-send-divider {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin:3px var(--space-3);
}

/* Unified command autocomplete menu styles moved to
   components/common/CompletionMenu.vue (shared with the @ file menu). */

/* Context usage detail popup */
.usage-popup {
  padding: var(--space-4) var(--space-6);
  min-width: 180px;
}

.usage-popup-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  margin-bottom: var(--space-4);
}

.usage-popup-section-title {
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-secondary);
  margin-top: var(--space-4);
  margin-bottom: var(--space-2);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border-color);
}

.usage-popup-bar {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  margin-bottom: var(--space-5);
}

.usage-popup-bar-track {
  flex: 1;
  height: 8px;
  border-radius: var(--radius-xs);
  background: color-mix(in srgb, var(--text-primary) 15%, transparent);
  overflow: hidden;
}

.usage-popup-bar-fill {
  height: 100%;
  border-radius: var(--radius-xs);
  transition: width 0.3s ease, background 0.3s ease;
}

.usage-popup-pct {
  font-size: var(--font-size-lg);
  font-weight: var(--font-weight-bold);
  flex-shrink: 0;
  min-width: 36px;
  text-align: right;
}

.usage-popup-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 3px 0;
  font-size: var(--font-size-sm);
}

.usage-popup-label {
  color: var(--text-secondary, #6c757d);
}

.usage-popup-value {
  color: var(--text-primary);
  font-weight: var(--font-weight-medium);
  font-variant-numeric: tabular-nums;
}

.usage-popup-compact {
  margin-top: var(--space-5);
  padding-top: var(--space-4);
  border-top: 1px solid color-mix(in srgb, var(--text-primary) 12%, transparent);
  display: flex;
  justify-content: center;
}

.usage-popup-compact-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: 1px solid color-mix(in srgb, var(--text-primary) 18%, transparent);
  border-radius: var(--radius-sm);
  cursor: pointer;
  padding:5px var(--space-6);
  color: var(--text-secondary, #6c757d);
  font-size: var(--font-size-sm);
  line-height: var(--line-height-snug);
  transition: color var(--duration-base), border-color var(--duration-base);
  user-select: none;
  -webkit-user-select: none;
}

.usage-popup-compact-btn:disabled {
  cursor: not-allowed;
  opacity: var(--opacity-disabled);
}

.usage-popup-compact-btn:active:not(:disabled) {
  color: var(--accent-color, #0066cc);
  border-color: var(--accent-color, #0066cc);
}

@media (hover: hover) {
  .usage-popup-compact-btn:hover:not(:disabled) {
    color: var(--accent-color, #0066cc);
    border-color: var(--accent-color, #0066cc);
  }
}
</style>
