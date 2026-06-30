<template>
  <div class="chat-input-wrapper">
    <!-- Top action bar (above input box) -->
    <div class="chat-top-actions">
      <div class="chat-action-group">
        <span class="chat-group-label" :title="t('chat.actions.session')">
          {{ t('chat.actions.session') }}
        </span>
        <button class="chat-action-btn" :class="{ 'has-unread': chatUnreadCount > 0, 'has-running': chatRunning }"
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
          @click="$emit('open-user-msg-index')"
          :title="t('chat.actions.userMsgIndex')">
          <MessagesSquare :size="14" />
        </button>
        <button class="chat-action-btn chat-action-btn-delete" :class="{ disabled: !currentSessionId }"
          @click="handleDelete"
          :title="currentSessionId ? t('chat.actions.deleteCurrentSession') : t('chat.actions.noSessionToDelete')">
          <Trash2 :size="14" />
        </button>
      </div>
      <button class="chat-action-btn auto-speech-btn" :class="{ active: autoSpeechEnabled }"
        @click="$emit('toggle-auto-speech')"
        :title="t('chat.actions.autoSpeech')">
        <Volume2 :size="14" />
        <span class="chat-action-label">{{ t('chat.actions.autoSpeech') }}</span>
      </button>
    </div>
    <!-- Input container -->
    <div class="chat-input-container" :class="{ 'drag-over': isDragOver }"
      @dragenter="onDragEnter"
      @dragover="onDragOver"
      @dragleave="onDragLeave"
      @drop="onDrop">
      <input type="file" ref="fileInputRef" @change="onFileSelect" style="display:none" multiple />
      <!-- Drop overlay -->
      <div v-if="isDragOver" class="drop-overlay">
        <Upload :size="24" :stroke-width="1.5" />
        <span>{{ t('chat.attach.dropToUpload') }}</span>
      </div>
      <!-- Upload progress bars -->
      <div v-if="uploadingFiles.length > 0" class="chat-upload-progress">
        <div v-for="(f, idx) in uploadingFiles" :key="'prog-' + idx" class="upload-progress-item">
          <div class="upload-progress-bar" :style="{ width: f.progress + '%' }"></div>
        </div>
      </div>
      <!-- Attachment tags -->
      <div v-if="quoteData || attachedFiles.length > 0 || pendingFiles.length > 0" class="chat-attachment-tags">
        <!-- Quote selection chip -->
        <span v-if="quoteData" class="chat-file-attachment attachment-quote" :title="quoteData.filePath" @click="$emit('quote-click')">
          <MessageSquare :size="12" :stroke-width="1.5" />
          <span class="chat-file-name">{{ truncateQuoteText(quoteData.text, 20) }}</span>
          <span v-if="quoteData.startLine" class="quote-line-info">L{{ quoteData.startLine }}</span>
          <button class="attachment-tag-remove" @click.stop="$emit('remove-quote')" :title="t('common.remove')">×</button>
        </span>
        <span v-for="(filePath, idx) in attachedFiles" :key="'att-' + filePath" class="chat-file-attachment attachment-ref" @click="$emit('file-tag-click', filePath)" :title="t('chat.attach.openFile')">
          <Folder v-if="isDirPath(filePath)" :size="12" :stroke-width="1.5" />
          <Paperclip v-else :size="12" :stroke-width="1.5" />
          <span class="chat-file-name">{{ getFileName(filePath) }}</span>
          <button class="attachment-tag-remove" @click.stop="$emit('remove-attached', idx)" :title="t('common.remove')">×</button>
        </span>
        <span v-for="(f, idx) in pendingFiles" :key="'upload-' + idx" class="chat-file-attachment attachment-upload" :class="{ 'is-uploading': f.uploading }">
          <FileImage v-if="f.isImage" :size="12" :stroke-width="1.5" />
          <FileText v-else :size="12" :stroke-width="1.5" />
          <span class="chat-file-name">{{ getFileName(f.path) || t('chat.attach.uploading') }}</span>
          <span v-if="f.uploading" class="attachment-progress-pct">{{ f.progress }}%</span>
          <button class="attachment-tag-remove" @click.stop="$emit('remove-file', idx)" :title="t('common.remove')">×</button>
        </span>
      </div>
      <!-- Input row: attach + clear + textarea + stop + send -->
      <div class="chat-input-row">
        <div class="attach-menu-wrapper" ref="attachMenuRef">
          <button class="chat-attach-btn" @click.stop="toggleAttachMenu" :disabled="inputDisabled" :title="t('chat.actions.attachment')">
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
          @keydown.enter.exact.prevent="$emit('send', inputText.trim())"
          @focus="onTextareaFocus"
          @blur="onTextareaBlur"
          ></textarea>
        <button v-if="!stopPrimed" class="chat-send-btn" ref="sendBtnRef" :class="{ queued: loading, shortcut: !hasInputContent }" @click.stop="handleSendClick" :title="!hasInputContent ? t('chat.input.quickMenu') : loading ? t('chat.input.enqueue') : t('chat.input.send')">
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
      <!-- Teleported attach menu (avoids overflow:hidden clipping) -->
      <PopupMenu v-model:show="showAttachMenu" :target-element="attachMenuRef?.querySelector('.chat-attach-btn')" :max-width="200" :max-height="280" :menu-items-count="attachMenuItemCount">
        <!-- Current file group -->
        <template v-if="currentFile?.path && !attachedFiles.includes(currentFile.path)">
          <div class="attach-menu-group-title">{{ t('chat.attach.currentFile') }}</div>
          <button class="attach-menu-item" @click="handleAttachFile(currentFile.path)">
            <FileText :size="14" :stroke-width="1.5" />
            <span class="attach-menu-item-name">{{ getFileName(currentFile.path) }}</span>
          </button>
        </template>
        <!-- Current directory group -->
        <template v-if="currentDir && !attachedFiles.includes(currentDir)">
          <div class="attach-menu-group-title">{{ t('chat.attach.currentDir') }}</div>
          <button class="attach-menu-item" @click="handleAttachFile(currentDir)">
            <Folder :size="14" :stroke-width="1.5" />
            <span class="attach-menu-item-name">{{ getFileName(currentDir) }}</span>
          </button>
        </template>
        <!-- Recently referenced group -->
        <template v-if="recentReferencedFiles.length > 0">
          <div class="attach-menu-group-title">{{ t('chat.attach.recentReferences') }}</div>
          <button v-for="item in recentReferencedFiles" :key="item.path" class="attach-menu-item" @click="handleAttachFile(item.path)">
            <FileText :size="14" :stroke-width="1.5" />
            <span class="attach-menu-item-name">{{ getFileName(item.path) }}</span>
            <span class="attach-menu-item-count">×{{ item.count }}</span>
          </button>
        </template>
        <!-- Separator + Upload -->
        <div v-if="hasFileGroups" class="attach-menu-separator"></div>
        <button class="attach-menu-item" @click="handleUploadClick">
          <Upload :size="14" :stroke-width="1.5" />
          <span>{{ t('chat.attach.uploadFile') }}</span>
        </button>
      </PopupMenu>
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
        <button class="quick-send-item" @click="showQuickMenu = false; quickSendStore.showEditDialog.value = true">
          ⚙️ {{ t('chat.quickSend.edit') }}
        </button>
      </PopupMenu>
      <!-- Session settings modal -->
      <SessionSettingModal
        :show="settingsDrawer.effectiveOpen.value"
        :agent-id="currentAgentId"
        :initial-tab="settingsModalInitialTab"
        @update:show="showSettingsModal = $event"
        @switch-model="handleSwitchModel"
        @switch-thinking-effort="handleSwitchThinkingEffort"
        @switch-mode="handleSwitchMode"
        @switch-transport="handleSwitchTransport"
      />
      <QuickSendDrawer :open="quickSendDrawer.effectiveOpen.value" @close="quickSendStore.showEditDialog.value = false" />
      <!-- @ command autocomplete menu (ClawBench built-in) -->
      <PopupMenu v-model:show="showAtMenu" :target-element="textareaRef" anchor="left" :max-width="260" :max-height="200" :menu-items-count="atMenuItems.length">
        <div class="at-menu-title">{{ t('chat.atCommand.title') }}</div>
        <button v-for="cmd in atMenuItems" :key="cmd.key" class="at-menu-item" @mousedown.prevent="handleAtSelect(cmd)">
          <span class="at-menu-label">{{ cmd.label }}</span>
          <span class="at-menu-desc">{{ cmd.description }}</span>
        </button>
      </PopupMenu>
      <!-- Slash command autocomplete menu (ACP backend commands — only in acp-stdio transport) -->
      <PopupMenu v-if="isACPTransport && availableCommands.length > 0" v-model:show="showSlashMenu" :target-element="textareaRef" anchor="left" :max-width="300" :max-height="240" :menu-items-count="slashMenuItems.length">
        <div class="at-menu-title">{{ t('chat.slashCommand.title') }}</div>
        <button v-for="cmd in slashMenuItems" :key="cmd.key" class="at-menu-item" @mousedown.prevent="handleSlashSelect(cmd)">
          <span class="at-menu-label slash-label">{{ cmd.label }}</span>
          <span class="at-menu-desc">{{ cmd.description }}</span>
        </button>
      </PopupMenu>
      <!-- Context usage detail popup -->
      <PopupMenu v-if="showUsageInfo" v-model:show="showUsagePopup" :target-element="usageElRef" :max-width="220" :max-height="240" :menu-items-count="4">
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
          <div v-if="contextCost > 0" class="usage-popup-row">
            <span class="usage-popup-label">{{ t('chat.sessionInfo.contextCost') }}</span>
            <span class="usage-popup-value">${{ contextCost.toFixed(2) }} {{ contextCurrency || 'USD' }}</span>
          </div>
        </div>
      </PopupMenu>
    </div>
    <!-- Session info bar (model + mode + thinking + transport) -->
    <div class="chat-session-info" v-if="currentModelName || showModeInfo || showThinkingInfo || showTransportInfo || showUsageInfo">
      <span class="session-info-model" @click.stop="openSettingsModal('model')"><Cpu :size="11" />{{ currentModelName }}</span>
      <template v-if="showModeInfo">
        <span class="session-info-divider"></span>
        <span class="session-info-mode" :class="{ 'session-info-mode-auto': autoApprove }" @click.stop="openSettingsModal('mode')"><Compass :size="11" />{{ currentModeName }}</span>
      </template>
      <template v-if="showThinkingInfo">
        <span class="session-info-divider"></span>
        <span class="session-info-thinking" @click.stop="openSettingsModal('thinking')"><Brain :size="11" />{{ currentThinkingEffortName }}</span>
      </template>
      <template v-if="showTransportInfo">
        <span class="session-info-divider"></span>
        <span class="session-info-transport" @click.stop="openSettingsModal('transport')"><Cable :size="11" />{{ currentTransport === 'acp-stdio' ? 'ACP' : 'CLI' }}</span>
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
import { ref, computed, nextTick, watch, onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessageSquare, List, Plus, Trash2, Volume2, Upload, Paperclip, FileImage, FileText, Folder, XCircle, Inbox, Send, Square, Settings, Zap, Loader2, Cpu, Compass, Brain, Cable, Activity, MessagesSquare } from 'lucide-vue-next'
import { baseName } from '@/utils/path.ts'
import { computeRecentReferencedFiles, computeHasFileGroups, computeAttachMenuItemCount } from '@/utils/chatInputUtils.ts'
import PopupMenu from '@/components/common/PopupMenu.vue'
import QuickSendDrawer from '@/components/chat/QuickSendDrawer.vue'
import SessionSettingModal from '@/components/chat/SessionSettingModal.vue'
import { createStopButtonMachine } from '@/utils/stopButtonMachine.ts'
import { useDialog } from '@/composables/useDialog.ts'
import { useQuickSend } from '@/composables/useQuickSend'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { useChatKeyboard } from '@/composables/useChatKeyboard'
import { useSessionIdentity } from '@/composables/useSessionIdentity'
import { useAgents } from '@/composables/useAgents'

const { t } = useI18n()
const { availableCommands, availableModes, availableThinkingEfforts, currentThinkingEffortName, currentTransport: sessionTransport, autoApprove, contextUsed, contextSize, contextCost, contextCurrency } = useSessionIdentity()
const { supportsDualTransport, hasThinkingEffortLevels } = useAgents()

// isACP: true when the current agent supports ACP (has acpCommand).
// Used for mode chips, thinking effort chips — these are ACP features
// that apply regardless of the current session's transport mode.
const isACP = computed(() => supportsDualTransport(props.currentAgentId || ''))

// isACPTransport: true when the current session is using ACP transport.
// Slash commands are only available in ACP transport mode — even if the
// agent supports dual transport, CLI sessions don't have slash commands.
const isACPTransport = computed(() => {
  if (sessionTransport.value) return sessionTransport.value === 'acp-stdio'
  return props.currentTransport === 'acp-stdio'
})

const showModeInfo = computed(() => availableModes.value.length > 0 && isACP.value)
const showThinkingInfo = computed(() => isACP.value && (availableThinkingEfforts.value.length > 0 || hasThinkingEffortLevels(props.currentAgentId || '')))
const showTransportInfo = computed(() => supportsDualTransport(props.currentAgentId || '') || !isACP.value)
const showUsageInfo = computed(() => contextSize.value > 0)
const usagePct = computed(() => contextSize.value > 0 ? Math.round((contextUsed.value / contextSize.value) * 100) : 0)
const usageColor = computed(() => {
  const pct = usagePct.value
  if (pct >= 95) return '#ef4444'
  if (pct >= 90) return '#f97316'
  if (pct >= 75) return '#eab308'
  return '#22c55e'
})
const usageText = computed(() => {
  const fmt = (n) => {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
    if (n >= 1000) return (n / 1000).toFixed(0) + 'K'
    return String(n)
  }
  return `${fmt(contextUsed.value)}/${fmt(contextSize.value)}`
})
const usageTooltip = computed(() => {
  let tip = `${usageText.value} tokens (${usagePct.value}%)`
  if (contextCost.value > 0) {
    tip += ` | $${contextCost.value.toFixed(2)} ${contextCurrency.value || 'USD'}`
  }
  return tip
})
const dialog = useDialog()
const quickSendStore = useQuickSend()
const { items: quickSendItems, fetchItems } = quickSendStore
const showSettingsModal = ref(false)
const settingsModalInitialTab = ref('model')
const quickSendDrawer = useTabDrawer('chat', quickSendStore.showEditDialog)
const settingsDrawer = useTabDrawer('chat', showSettingsModal)

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
  if (props.pendingFiles.length > 0) return t('chat.input.placeholderOptional')
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
  pendingFiles: Array,
  attachedFiles: Array,
  quoteData: Object,
  messages: Array,
  autoSpeechEnabled: Boolean,
  currentSessionId: String,
  chatUnreadCount: Number,
  chatRunning: Boolean,
  currentModelId: String,
  currentModelName: String,
  currentThinkingEffort: String,
  currentModeName: String,
  currentTransport: String,
  currentAgentId: String,
  active: Boolean,
})

const emit = defineEmits([
  'send',
  'cancel',
  'file-select',
  'file-drop',
  'remove-file',
  'add-attached',
  'remove-attached',
  'remove-quote',
  'quote-click',
  'open-session-tab',
  'file-tag-click',
  'toggle-auto-speech',
  'create-session',
  'show-agent-selector',
  'delete-session',
  'open-user-msg-index',
  'switch-model',
  'switch-thinking-effort',
  'switch-mode',
  'switch-transport',
])

const inputText = ref('')
const textareaRef = ref(null)
const fileInputRef = ref(null)
const isDragOver = ref(false)
const dragCounter = ref(0)
const showAttachMenu = ref(false)
const attachMenuRef = ref(null)
const showQuickMenu = ref(false)
const sendBtnRef = ref(null)

function openSettingsModal(tab) {
  settingsModalInitialTab.value = tab
  showSettingsModal.value = true
}

// ── @ command autocomplete ──
const showAtMenu = ref(false)
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

const atMenuItems = computed(() => {
  const text = inputText.value
  if (!text.startsWith('@')) return []
  const query = text.toLowerCase().slice(1) // strip leading '@'
  const cmds = atCommands.value // unwrap computed ref
  if (!query) return cmds // empty query → show all
  return cmds.filter(cmd => cmd.key.toLowerCase().includes(query))
})

const slashMenuItems = computed(() => {
  const text = inputText.value
  if (!text.startsWith('/')) return []
  const query = text.toLowerCase().slice(1) // strip leading '/'
  if (!query) return availableCommands.value.map(cmd => ({
    key: '/' + cmd.name,
    label: '/' + cmd.name,
    description: cmd.description,
    inputHint: cmd.inputHint || '',
  }))
  return availableCommands.value
    .filter(cmd => cmd.name.toLowerCase().includes(query))
    .map(cmd => ({
      key: '/' + cmd.name,
      label: '/' + cmd.name,
      description: cmd.description,
      inputHint: cmd.inputHint || '',
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

function handleAtSelect(cmd) {
  inputText.value = cmd.key + ' '
  showAtMenu.value = false
  nextTick(() => {
    const el = textareaRef.value
    if (el) el.focus()
  })
}

function handleSlashSelect(cmd) {
  inputText.value = cmd.key + ' '
  showSlashMenu.value = false
  nextTick(() => {
    const el = textareaRef.value
    if (el) el.focus()
  })
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
    } else {
      draftCache.delete(oldId)
    }
  }
  // Restore draft for the new session (or clear if none)
  inputText.value = newId ? (draftCache.get(newId) || '') : ''
  // autoResizeTextarea is called automatically by the inputText watcher
})

const uploadingFiles = computed(() => props.pendingFiles.filter(f => f.uploading))

const hasInputContent = computed(() => inputText.value.trim() || props.pendingFiles.length > 0 || props.attachedFiles.length > 0 || props.quoteData)

// Extract recently referenced files from message history
const recentReferencedFiles = computed(() => {
  return computeRecentReferencedFiles(props.messages, props.attachedFiles, props.currentFile?.path)
})

const hasFileGroups = computed(() => {
  return computeHasFileGroups(props.currentFile?.path, props.currentDir, props.attachedFiles, recentReferencedFiles.value)
})

const attachMenuItemCount = computed(() => {
  return computeAttachMenuItemCount(props.currentFile?.path, props.currentDir, props.attachedFiles, recentReferencedFiles.value)
})

function handleCreateClick(e) {
  // On desktop, click = show agent selector (short tap equivalent)
  if (e.detail === 0) return
  emit('show-agent-selector')
}

async function handleDelete() {
  if (!props.currentSessionId) return
  if (await dialog.confirm(t('chat.delete.confirm'), { dangerous: true })) {
    emit('delete-session')
  }
}

function getFileName(path) {
  return baseName(path)
}

function isDirPath(filePath) {
  return props.currentDir && filePath === props.currentDir
}

function truncateQuoteText(text, maxLen) {
  if (!text) return ''
  const oneLine = text.replace(/\n/g, ' ')
  return oneLine.length > maxLen ? oneLine.slice(0, maxLen) + '...' : oneLine
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

function onFileSelect(e) {
  emit('file-select', e)
}

function onDragEnter(e) {
  e.preventDefault()
  dragCounter.value++
  isDragOver.value = true
}

function onDragOver(e) {
  e.preventDefault()
}

function onDragLeave(e) {
  e.preventDefault()
  dragCounter.value--
  if (dragCounter.value <= 0) {
    dragCounter.value = 0
    isDragOver.value = false
  }
}

function onDrop(e) {
  e.preventDefault()
  dragCounter.value = 0
  isDragOver.value = false
  const files = Array.from(e.dataTransfer?.files || [])
  if (files.length > 0) {
    emit('file-drop', files)
  }
}

function clearInput() {
  inputText.value = ''
  // Also clear the draft cache for current session so it doesn't linger
  if (props.currentSessionId) {
    draftCache.delete(props.currentSessionId)
  }
}

function handleAttachFile(filePath) {
  emit('add-attached', filePath)
}

function handleUploadClick() {
  showAttachMenu.value = false
  if (fileInputRef.value) {
    // Clear previous selection BEFORE opening picker to prevent stale
    // file data on Android WebView when user cancels the picker
    fileInputRef.value.value = ''
    fileInputRef.value.click()
  }
}

function toggleAttachMenu() {
  showAttachMenu.value = !showAttachMenu.value
}

function handleSendClick() {
  if (inputText.value.trim()) {
    emit('send', inputText.value.trim())
  } else if (props.pendingFiles.length > 0 || props.attachedFiles.length > 0) {
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
watch(showAttachMenu, (v) => { if (v) { showQuickMenu.value = false; showSettingsModal.value = false; showSlashMenu.value = false; showUsagePopup.value = false } })
watch(showQuickMenu, (v) => { if (v) { showAttachMenu.value = false; showSettingsModal.value = false; showSlashMenu.value = false; showUsagePopup.value = false } })
watch(showSettingsModal, (v) => { if (v) { showAttachMenu.value = false; showQuickMenu.value = false; showSlashMenu.value = false; showUsagePopup.value = false } })
watch(showSlashMenu, (v) => { if (v) { showAttachMenu.value = false; showQuickMenu.value = false; showSettingsModal.value = false; showUsagePopup.value = false } })
watch(showUsagePopup, (v) => { if (v) { showAttachMenu.value = false; showQuickMenu.value = false; showSettingsModal.value = false; showSlashMenu.value = false } })

onMounted(() => {
  fetchItems()
  startPlaceholderRotation()
})

onBeforeUnmount(() => {
  stopMachine.destroy()
  if (quickSendPressTimer) {
    clearTimeout(quickSendPressTimer)
    quickSendPressTimer = null
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
  inputText,
  deleteDraft: (sessionId) => { draftCache.delete(sessionId) },
  injectToInput,
  handleQuickSendClick,
  onQuickSendTouchStart,
  onQuickSendTouchMove,
  onQuickSendTouchEnd,
  cancelQuickSendPress,
  quickSendPressingId,
})
</script>

<style scoped>
/* Outer wrapper: top actions + input box stacked vertically */
.chat-input-wrapper {
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  margin: 0 8px 8px;
  padding-top: 8px;
  border-top: 1px solid var(--border-color, #e5e5e5);
}

/* Session info bar (model + mode + thinking + transport, below input box) */
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
.session-info-mode,
.session-info-thinking,
.session-info-transport {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex-shrink: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 14px;
  cursor: pointer;
  transition: color 0.15s;
}

.session-info-model:active,
.session-info-thinking:active,
.session-info-transport:active {
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
.session-info-thinking svg,
.session-info-transport svg,
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

.chat-action-btn-delete:not(.disabled) {
  color: var(--text-muted, #999);
}

@media (hover: hover) {
  .chat-action-btn-delete:not(.disabled):hover {
    color: var(--danger-color, #dc3545);
    background: color-mix(in srgb, var(--danger-color, #dc3545) 10%, transparent);
  }
}

.chat-action-btn-delete:not(.disabled):active {
  color: var(--danger-color, #dc3545);
  background: color-mix(in srgb, var(--danger-color, #dc3545) 18%, transparent);
  transform: scale(0.92);
}

.chat-action-btn-delete.disabled {
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

.chat-input-container.drag-over {
  background: var(--bg-primary, #fff);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent-color, #0066cc) 40%, transparent);
}

/* Drop overlay */
.drop-overlay {
  position: absolute;
  inset: 0;
  z-index: 10;
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

/* Upload progress bars at top of input */
.chat-upload-progress {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 4px 8px 0;
}

.upload-progress-item {
  height: 3px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 15%, transparent);
  border-radius: 2px;
  overflow: hidden;
}

.upload-progress-bar {
  height: 100%;
  background: var(--accent-color, #0066cc);
  border-radius: 2px;
  transition: width 0.15s ease;
}

/* Uploading state for attachment tag */
.attachment-upload.is-uploading {
  opacity: 0.7;
}

.attachment-progress-pct {
  font-size: 10px;
  color: var(--accent-color, #0066cc);
  font-variant-numeric: tabular-nums;
}

/* Attach button (inside input row) */
.attach-menu-wrapper {
  position: relative;
  flex-shrink: 0;
}

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

/* Attachment tags row */
.chat-attachment-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  padding: 4px 8px;
}

/* Base attachment tag styles */
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

.chat-file-attachment svg {
  flex-shrink: 0;
}

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

.chat-file-name::-webkit-scrollbar {
  display: none;
}

/* Input area attachment tags */
.chat-attachment-tags .chat-file-attachment {
  max-width: 200px;
}

.chat-attachment-tags .attachment-upload {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--accent-color, #0066cc) 20%, transparent);
  color: var(--accent-color, #0066cc);
  cursor: default;
}

.chat-attachment-tags .attachment-upload .chat-file-name {
  color: var(--accent-color, #0066cc);
}

.chat-attachment-tags .attachment-upload svg {
  stroke: var(--accent-color, #0066cc);
}

.chat-attachment-tags .attachment-upload:hover {
  background: color-mix(in srgb, var(--accent-color, #0066cc) 18%, transparent);
}

.chat-attachment-tags .attachment-ref {
  background: color-mix(in srgb, var(--text-muted, #999) 8%, transparent);
  border: 1px dashed var(--text-secondary, #666);
  color: var(--text-secondary, #666);
}

.chat-attachment-tags .attachment-ref .chat-file-name {
  color: var(--text-secondary, #666);
}

.chat-attachment-tags .attachment-ref svg {
  stroke: var(--text-secondary, #666);
}

.chat-attachment-tags .attachment-ref:hover {
  background: color-mix(in srgb, var(--text-muted, #999) 15%, transparent);
}

/* Quote chip */
.chat-attachment-tags .attachment-quote {
  background: color-mix(in srgb, var(--accent-color, #4f9cf7) 8%, transparent);
  border: 1px dashed var(--accent-color, #4f9cf7);
  color: var(--accent-color, #4f9cf7);
  cursor: pointer;
}

.chat-attachment-tags .attachment-quote .chat-file-name {
  color: var(--accent-color, #4f9cf7);
}

.chat-attachment-tags .attachment-quote svg {
  stroke: var(--accent-color, #4f9cf7);
}

.chat-attachment-tags .attachment-quote:hover {
  background: color-mix(in srgb, var(--accent-color, #4f9cf7) 15%, transparent);
}

.quote-line-info {
  font-size: 10px;
  opacity: 0.7;
  margin-left: 2px;
}

.attachment-tag-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted, #999);
  padding: 0;
  font-size: 14px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 2px;
  transition: color 0.15s, background 0.15s;
}

.attachment-tag-remove:hover {
  color: var(--danger-color, #dc3545);
  background: color-mix(in srgb, var(--danger-color, #dc3545) 10%, transparent);
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

.chat-action-label {
  font-size: 11px;
  line-height: 1.3;
}


</style>

<!-- Unscoped styles for teleported menu content (PopupMenu uses Teleport to body, scoped styles won't reach it) -->
<style>
/* Attach menu content styles */
.attach-menu-group-title {
  padding: 4px 10px 1px;
  font-size: 11px;
  color: var(--text-muted, #999);
  font-weight: 500;
  letter-spacing: 0.3px;
}

.attach-menu-item {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  width: 100%;
  border: none;
  background: none;
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  white-space: nowrap;
  text-align: left;
}

.attach-menu-item:hover {
  background: var(--accent-color, #0066cc);
  color: #fff;
}

.attach-menu-item svg {
  flex-shrink: 0;
  width: 14px;
  height: 14px;
}

.attach-menu-item-name {
  font-family: monospace;
  font-size: 12px;
  min-width: 0;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.attach-menu-item-name::-webkit-scrollbar {
  display: none;
}

.attach-menu-item-count {
  margin-left: auto;
  font-size: 11px;
  color: var(--text-muted, #999);
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.attach-menu-item:hover .attach-menu-item-count {
  color: rgba(255, 255, 255, 0.7);
}

.attach-menu-separator {
  height: 1px;
  background: var(--border-color, #e5e5e5);
  margin: 3px 6px;
}

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

.at-menu-item:hover {
  background: var(--bg-secondary, #f1f3f5);
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
