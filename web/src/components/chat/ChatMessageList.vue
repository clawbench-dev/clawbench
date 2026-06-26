<template>
  <div class="chat-messages-wrapper">
  <div class="chat-messages" id="aiChatMessages" ref="messagesRef" @click="handleChatClick" @scroll="handleScroll">
    <!-- Lazy load feedback -->
    <div class="chat-load-area">
      <Transition name="load-hint-fade">
        <div v-if="loadingMore" class="chat-load-more">
          <span class="chat-load-spinner"></span>
          <span>{{ t('chat.messageList.loadingMore') }}</span>
        </div>
        <div v-else-if="hasMore && remainingCount > 0" class="chat-load-hint" @click="emit('load-more')">
          <ChevronUp :size="14" />
          <span>{{ t('chat.messageList.moreOlderMessages', { count: remainingCount }) }}</span>
        </div>
        <div v-else-if="showAllLoaded" class="chat-load-done">
          <span>{{ t('chat.messageList.allMessagesLoaded') }}</span>
        </div>
      </Transition>
    </div>

    <div class="chat-messages-list">
      <div v-if="messages.length === 0" class="chat-empty">
      <template v-if="currentAgent">
        <div class="agent-welcome">
          <span class="agent-welcome-icon">{{ currentAgent.icon }}</span>
          <div class="agent-welcome-info">
            <span class="agent-welcome-name">{{ currentAgent.name }}</span>
            <span class="agent-welcome-specialty">{{ currentAgent.specialty }}</span>
            <div class="agent-welcome-tags">
              <span class="agent-welcome-tag agent-welcome-backend">{{ currentAgent.backend }}</span>
              <span v-if="currentAgent.model" class="agent-welcome-tag agent-welcome-model">{{ currentAgent.model }}</span>
            </div>
          </div>
        </div>
        <span class="agent-welcome-hint">{{ t('chat.messageList.startConversation') }}</span>
      </template>
      <span v-else>{{ t('chat.messageList.startConversationAI') }}</span>
    </div>

    <!-- Key strategy:
      - DB messages: 'db-{numericId}' (stable, never changes)
      - Drain messages: 'db-drain-{ts}-{suffix}' (stable, self-cleaning on loadHistory)
      - Optimistic push: 'db-local-{ts}' (stable, replaced by DB ID on loadHistory)
      - Pending messages (no id): 'local-{index}' (unstable, but temporary)
    -->
    <ChatMessageItem
      v-for="(msg, i) in messages"
      :key="msg.id ? 'db-' + msg.id : 'local-' + i"
      :msg="msg"
      :index="i"
      :expandedTools="expandedTools"
      :blockTasks="blockTasks"
      :blockAskQuestions="blockAskQuestions"
      :blockRagResults="blockRagResults"
      :agents="agents"
      :staticBlockCache="staticBlockCache"
      :active="active"
      @toggle-tool="$emit('toggle-tool', $event)"
      @show-tool-detail="$emit('show-tool-detail', $event)"
      @show-thinking-detail="$emit('show-thinking-detail', $event)"
      @show-metadata="$emit('show-metadata', $event)"
      @file-tag-click="$emit('file-tag-click', $event)"
      @task-card-click="$emit('task-card-click', $event)"
      @send-message="$emit('send-message', $event)"
      @render-flush="emit('render-flush')"
      @toggle-summary="$emit('toggle-summary', $event)"
      @resume-session="$emit('resume-session', $event)"
      @show-rag-detail="$emit('show-rag-detail', $event)"
      @remove-pending="$emit('remove-pending', $event)"
    />
    </div>
  </div>

  <!-- Floating scroll buttons — outside scroll container, inside relative wrapper -->
  <Transition name="scroll-fab">
    <div v-if="scrolledUp || scrolledDown" ref="scrollFabRef" class="scroll-fab-group scroll-fab-bottom">
      <Transition name="scroll-fab-swap" mode="out-in">
        <div v-if="scrolledUp" key="up" class="scroll-fab-dir">
          <button class="scroll-fab-round" @click="scrollToTop" :title="t('chat.messageList.scrollToTop')">
            <ChevronsUp :size="18" />
          </button>
          <button class="scroll-fab-round" @click="scrollToPreviousMessage" :title="t('chat.messageList.scrollToPrev')">
            <ArrowUp :size="18" />
          </button>
        </div>
        <div v-else key="down" class="scroll-fab-dir">
          <button class="scroll-fab-round" @click="scrollToBottomSmooth" :title="t('chat.messageList.scrollToBottom')">
            <ChevronsDown :size="18" />
          </button>
          <button class="scroll-fab-round" @click="scrollToNextMessage" :title="t('chat.messageList.scrollToNext')">
            <ArrowDown :size="18" />
          </button>
        </div>
      </Transition>
      <button v-if="hasUserMessages" class="scroll-fab-round" @click="toggleUserMsgIndex" :title="t('chat.messageList.userMsgIndex')">
        <List :size="18" />
      </button>
    </div>
  </Transition>

  <!-- User message index overlay -->
  <Transition name="user-msg-overlay">
    <div v-if="showUserMsgIndex" class="user-msg-overlay" @click.self="closeUserMsgIndex" @keydown.escape="closeUserMsgIndex">
      <div ref="popoverRef" class="user-msg-panel" role="dialog" :aria-label="t('chat.messageList.userMsgIndexTitle')">
        <div class="user-msg-panel-header">
          <MessageSquare :size="16" class="user-msg-panel-icon" />
          <span>{{ t('chat.messageList.userMsgIndexTitle') }}</span>
          <span class="user-msg-panel-count">{{ userMsgIndexList.length }}</span>
        </div>
        <div v-if="loadingIndex" class="user-msg-panel-loading">
          <span class="chat-load-spinner"></span>
          <span>{{ t('chat.messageList.loadingMore') }}</span>
        </div>
        <div v-else-if="loadingTarget" class="user-msg-panel-loading">
          <span class="chat-load-spinner"></span>
          <span>{{ t('chat.messageList.loadingMore') }}</span>
        </div>
        <div class="user-msg-panel-list">
          <div
            v-for="(um, idx) in userMsgIndexList"
            :key="um.id || idx"
            class="user-msg-item"
            tabindex="0"
            role="button"
            @click="jumpToUserMessage(um)"
            @keydown.enter="jumpToUserMessage(um)"
          >
            <span class="user-msg-item-node">
              <span class="user-msg-item-index">{{ idx + 1 }}</span>
            </span>
            <span class="user-msg-item-text">{{ formatTruncateUserMsg(um) }}</span>
          </div>
        </div>
      </div>
    </div>
  </Transition>

  </div>
</template>

<script setup>
import { ref, nextTick, inject, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUp, ChevronsUp, ArrowUp, ChevronsDown, ArrowDown, List, MessageSquare } from 'lucide-vue-next'
import ChatMessageItem from './ChatMessageItem.vue'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useDialog } from '@/composables/useDialog'
import { useUserMsgIndex } from '@/composables/useUserMsgIndex.ts'
import { store } from '@/stores/app.ts'
import { computeRemainingCount } from '@/utils/messageListUtils.ts'

const { t } = useI18n()

const props = defineProps({
  messages: Array,
  expandedTools: Object,
  blockTasks: Object,
  blockAskQuestions: Object,
  blockRagResults: Object,
  agents: Array,
  currentAgent: Object,
  currentSessionId: String,
  hasMore: Boolean,
  loadingMore: Boolean,
  totalMessages: { type: Number, default: 0 },
  staticBlockCache: Object,
  active: { type: Boolean, default: true },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-thinking-detail', 'show-metadata', 'file-tag-click', 'file-open', 'load-more', 'task-card-click', 'send-message', 'remove-pending', 'render-flush', 'toggle-summary', 'resume-session', 'show-rag-detail'])

const messagesRef = ref(null)
const { handleDblClick } = useDoubleClickCopy()
const { openFilePath } = useFilePathAnnotation()
const dialog = useDialog()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

// How many older messages are not yet loaded
const remainingCount = computed(() => {
  return computeRemainingCount(props.hasMore, props.totalMessages, props.messages.length)
})

// "All loaded" brief hint: shown for 2s after last load completes with no more
const showAllLoaded = ref(false)
let allLoadedTimer = null

watch(() => props.hasMore, (hasMore, prevHasMore) => {
  // When transitioning from hasMore=true to hasMore=false (just finished loading all)
  if (!hasMore && prevHasMore && props.messages.length > 0) {
    showAllLoaded.value = true
    clearTimeout(allLoadedTimer)
    allLoadedTimer = setTimeout(() => { showAllLoaded.value = false }, 2000)
  }
})

// Reset isAtBottom so auto-scroll re-engages for the new session
watch(() => props.messages, () => {
  isAtBottom.value = true
  scrolledUp.value = false
  scrolledDown.value = false
  lastScrollTop = 0
  programmaticScrolling = false
  clearTimeout(scrollUpTimer)
  clearTimeout(scrollDownTimer)
})

// Clear user message index on session switch — handled by useUserMsgIndex

// Inject bottomSheetRef from parent for closing
const chatUI = inject('chatUI', {})
const hotSwitchProject = inject('hotSwitchProject', null)

async function handleChatClick(event) {
  // 1. Handle localhost URL clicks (icon button or <a> tag) — App mode only
  if (handleLocalhostUrlClick(event)) return

  // 2. Worktree action button — show modal with "Switch" or "Open directory"
  const wtBtn = (event.target).closest('.chat-worktree-btn')
  if (wtBtn) {
    event.preventDefault()
    event.stopPropagation()
    const wtPath = wtBtn.getAttribute('data-worktree-path')
    const filePath = wtBtn.getAttribute('data-file-path')
    if (wtPath) {
      const switchLabel = t('chat.attach.switchWorktree')
      const openLabel = t('chat.attach.openDirectory')
      // Use prompt dialog as a two-option chooser:
      // confirm → switch to worktree, cancel → open directory (if available)
      const result = await dialog.confirm(
        filePath ? `${switchLabel}\n${openLabel}` : switchLabel,
        {
          title: t('chat.attach.openWorktree'),
          confirmText: switchLabel,
          cancelText: filePath ? openLabel : t('common.cancel'),
        }
      )
      if (result) {
        // Switch to worktree
        if (hotSwitchProject) {
          await hotSwitchProject(wtPath)
        } else {
          await store.setProject(wtPath)
        }
      } else if (filePath) {
        // Open directory
        const ok = await openFilePath(filePath)
        if (ok) chatUI.navigateToFileViewer?.()
      }
    }
    return
  }

  // 3. Commit hash click (span or button) — check before file-path to prevent
  //    7-char hex hashes from being misinterpreted as file paths.
  //    Note: do NOT call navigateToFileViewer() here — handleNavigateToCommit
  //    in App.vue switches to the history tab which hides the chat panel.
  const commitEl = (event.target).closest('.chat-commit-hash, .chat-commit-open-btn')
  if (commitEl) {
    event.preventDefault()
    event.stopPropagation()
    const sha = commitEl.getAttribute('data-commit-sha')
    if (sha) {
      window.dispatchEvent(new CustomEvent('navigate-to-commit', { detail: { sha } }))
    }
    return
  }

  // 4. File-path button handler
  const btn = (event.target).closest('.chat-file-open-btn')
  if (btn) {
    event.preventDefault()
    event.stopPropagation()
    const filePath = btn.getAttribute('data-file-path')
    const lineStart = btn.getAttribute('data-line-start')
    const lineEnd = btn.getAttribute('data-line-end')
    if (filePath) {
      const ok = await openFilePath(filePath, lineStart ? parseInt(lineStart, 10) : undefined, lineEnd ? parseInt(lineEnd, 10) : undefined)
      if (ok) chatUI.navigateToFileViewer?.()
    }
    return
  }

  handleDblClick(event, async (href) => {
    const ok = await openFilePath(href)
    if (ok) chatUI.navigateToFileViewer?.()
  })
}

let loadMorePending = false
// Track whether the user is at the bottom of the chat.
// When the user scrolls back to the bottom during streaming, auto-scroll resumes.
const isAtBottom = ref(true)

// Whether user has scrolled up/down enough to show floating scroll buttons
// Only one group shows at a time — whichever direction the user last scrolled toward
const scrolledUp = ref(false)
const scrolledDown = ref(false)
const scrollFabRef = ref(null)

// Auto-hide timers for scroll buttons
let scrollUpTimer = null
let scrollDownTimer = null
let lastScrollTop = 0
const SCROLL_BUTTON_HIDE_DELAY = 3000

const NEAR_EDGE_THRESHOLD = 100
const SCROLL_BUTTON_TRIGGER = 200
const SCROLL_DELTA_THRESHOLD = 10

// Flag to suppress handleScroll button logic during programmatic smooth scroll
let programmaticScrolling = false

function handleScroll() {
  if (!messagesRef.value) return
  const el = messagesRef.value

  const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
  const nearBottom = distFromBottom < NEAR_EDGE_THRESHOLD
  const nearTop = el.scrollTop < NEAR_EDGE_THRESHOLD
  isAtBottom.value = nearBottom

  // When near edges during programmatic scroll, hide buttons immediately
  if (programmaticScrolling) {
    if (nearTop && scrolledUp.value) {
      scrolledUp.value = false
      clearTimeout(scrollUpTimer)
    }
    if (nearBottom && scrolledDown.value) {
      scrolledDown.value = false
      clearTimeout(scrollDownTimer)
    }
    return
  }

  // Hide scroll buttons when near the edges
  if (nearTop && scrolledUp.value) {
    scrolledUp.value = false
    clearTimeout(scrollUpTimer)
  }
  if (nearBottom && scrolledDown.value) {
    scrolledDown.value = false
    clearTimeout(scrollDownTimer)
  }

  // Determine scroll direction
  const scrollDelta = el.scrollTop - lastScrollTop
  lastScrollTop = el.scrollTop

  // Ignore tiny scroll movements (e.g. finger tremor on mobile) to prevent accidental FAB appearance
  if (Math.abs(scrollDelta) < SCROLL_DELTA_THRESHOLD) return

  // Scrolled up (toward top): show top buttons, hide bottom — but not if already near top
  const shouldShowUp = scrollDelta < 0 && distFromBottom > SCROLL_BUTTON_TRIGGER && !nearTop
  // Scrolled down (toward bottom): show bottom buttons, hide top — but not if already near bottom
  const shouldShowDown = scrollDelta > 0 && !nearBottom && distFromBottom > SCROLL_BUTTON_TRIGGER

  if (shouldShowUp) {
    scrolledDown.value = false
    clearTimeout(scrollDownTimer)
    scrolledUp.value = true
    clearTimeout(scrollUpTimer)
    scrollUpTimer = setTimeout(() => { scrolledUp.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  } else if (shouldShowDown) {
    scrolledUp.value = false
    clearTimeout(scrollUpTimer)
    scrolledDown.value = true
    clearTimeout(scrollDownTimer)
    scrollDownTimer = setTimeout(() => { scrolledDown.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  }

  if (loadMorePending) return
  if (!props.hasMore || props.loadingMore) return
  if (el.scrollTop < 50) {
    loadMorePending = true
    emit('load-more')
    nextTick(() => { loadMorePending = false })
  }
}

// Hide scroll FAB on outside click
function hideScrollFab() {
  scrolledUp.value = false
  scrolledDown.value = false
  clearTimeout(scrollUpTimer)
  clearTimeout(scrollDownTimer)
}

function onDocumentClick(e) {
  if (!scrollFabRef.value) return
  if (!scrollFabRef.value.contains(e.target)) {
    hideScrollFab()
  }
}

onMounted(() => document.addEventListener('click', onDocumentClick, true))
onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick, true))

function scrollToBottom(force = false) {
  nextTick(() => {
    if (!messagesRef.value) return
    const el = messagesRef.value
    if (force || isAtBottom.value) {
      el.scrollTop = el.scrollHeight
      // Verify the scroll actually reached the bottom — content may have grown
      // between the scrollToBottom call and this nextTick callback, or may grow
      // after this callback completes (streaming text, throttled render flush).
      // Use requestAnimationFrame to re-check after the browser has laid out
      // the DOM changes, and do a second scroll if still not at the bottom.
      requestAnimationFrame(() => {
        if (!messagesRef.value) return
        const el = messagesRef.value
        const gap = el.scrollHeight - el.scrollTop - el.clientHeight
        if (gap > 0) {
          el.scrollTop = el.scrollHeight
        }
        // Final isAtBottom state based on actual scroll position after correction
        isAtBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_EDGE_THRESHOLD
        // For force scrolls, also do a delayed re-scroll to catch async content
        // rendering (Mermaid, KaTeX, collapse transitions) that settles later.
        if (force) {
          setTimeout(() => {
            if (!messagesRef.value) return
            const el = messagesRef.value
            el.scrollTop = el.scrollHeight
            isAtBottom.value = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_EDGE_THRESHOLD
          }, 300)
        }
      })
    }
  })
}

function scrollToTop() {
  if (!messagesRef.value) return
  clearTimeout(scrollUpTimer)
  scrollUpTimer = setTimeout(() => { scrolledUp.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  programmaticScrolling = true
  messagesRef.value.scrollTo({ top: 0, behavior: 'smooth' })
  // Smooth scroll takes ~300-500ms; clear flag after settling
  setTimeout(() => { programmaticScrolling = false }, 600)
}

function scrollToPreviousMessage() {
  if (!messagesRef.value) return
  clearTimeout(scrollUpTimer)
  scrollUpTimer = setTimeout(() => { scrolledUp.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  programmaticScrolling = true
  const el = messagesRef.value
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  if (items.length === 0) { programmaticScrolling = false; return }
  // Find the first message whose bottom is above the viewport top
  for (let i = items.length - 1; i >= 0; i--) {
    const rect = items[i].getBoundingClientRect()
    const containerRect = el.getBoundingClientRect()
    if (rect.bottom < containerRect.top + 8) {
      items[i].scrollIntoView({ behavior: 'smooth', block: 'start' })
      setTimeout(() => { programmaticScrolling = false }, 600)
      return
    }
  }
  // If no message is above, scroll to top
  el.scrollTo({ top: 0, behavior: 'smooth' })
  setTimeout(() => { programmaticScrolling = false }, 600)
}

function scrollToNextMessage() {
  if (!messagesRef.value) return
  clearTimeout(scrollDownTimer)
  scrollDownTimer = setTimeout(() => { scrolledDown.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  programmaticScrolling = true
  const el = messagesRef.value
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  if (items.length === 0) { programmaticScrolling = false; return }
  // Find the first message whose top is below the viewport bottom
  for (let i = 0; i < items.length; i++) {
    const rect = items[i].getBoundingClientRect()
    const containerRect = el.getBoundingClientRect()
    if (rect.top > containerRect.bottom - 8) {
      items[i].scrollIntoView({ behavior: 'smooth', block: 'start' })
      setTimeout(() => { programmaticScrolling = false }, 600)
      return
    }
  }
  // If no message is below, scroll to bottom
  programmaticScrolling = false
  scrollToBottomSmooth()
}

function scrollToBottomSmooth() {
  if (!messagesRef.value) return
  clearTimeout(scrollDownTimer)
  scrollDownTimer = setTimeout(() => { scrolledDown.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  programmaticScrolling = true
  const el = messagesRef.value
  el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  setTimeout(() => { programmaticScrolling = false }, 600)
}

// ── User message index ──
const {
  hasUserMessages,
  userMsgIndexList,
  showUserMsgIndex,
  loadingTarget,
  loadingIndex,
  formatTruncateUserMsg,
  toggleUserMsgIndex,
  closeUserMsgIndex,
  jumpToUserMessage,
  scrollToMessage: scrollToMessageUserMsg,
} = useUserMsgIndex({
  getMessages: () => props.messages,
  getCurrentSessionId: () => props.currentSessionId || '',
  getHasMore: () => props.hasMore,
  getLoadingMore: () => props.loadingMore,
  emitLoadMore: () => emit('load-more'),
  getMessagesRef: () => messagesRef.value,
  hideScrollFab,
  setProgrammaticScrolling: (val) => { programmaticScrolling = val },
})
// Watch session switch to reset user msg index
watch(() => props.currentSessionId, () => {
  showUserMsgIndex.value = false
  userMsgIndexList.value = []
})
const popoverRef = ref(null)

defineExpose({
  scrollToBottom,
  scrollToTop,
  scrollToPreviousMessage,
  scrollToNextMessage,
  scrollToBottomSmooth,
  scrollToMessage: scrollToMessageUserMsg,
  messagesRef,
  isAtBottom: () => isAtBottom.value,
  scrolledUp,
  scrolledDown,
})
</script>

<style scoped>
/* Wrapper: positioning context for floating scroll buttons */
.chat-messages-wrapper {
  flex: 1;
  position: relative;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.chat-messages {
  flex: 1;
  overflow-y: auto;
  padding: 12px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Message list container */
.chat-messages-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.chat-empty {
  text-align: center;
  padding: 32px 16px;
  color: var(--text-muted);
  font-size: 13px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
}

.agent-welcome {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 10px;
  max-width: 280px;
  width: 100%;
  text-align: left;
}

.agent-welcome-icon {
  font-size: 28px;
  flex-shrink: 0;
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-tertiary);
  border-radius: 10px;
}

.agent-welcome-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.agent-welcome-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}

.agent-welcome-specialty {
  font-size: 11px;
  color: var(--text-secondary);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

.agent-welcome-tags {
  display: flex;
  gap: 4px;
  margin-top: 2px;
}

.agent-welcome-tag {
  font-size: 9px;
  padding: 1px 6px;
  border-radius: 3px;
  font-weight: 500;
  flex-shrink: 0;
}

.agent-welcome-backend {
  background: rgba(0, 102, 204, 0.1);
  color: var(--accent-color);
}

.agent-welcome-model {
  background: rgba(100, 100, 100, 0.08);
  color: var(--text-muted);
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.agent-welcome-hint {
    font-size: 12px;
    color: color-mix(in srgb, var(--text-muted) 70%, transparent);
}

/* Lazy load feedback area */
.chat-load-area {
  position: relative;
  min-height: 0;
}

.chat-load-more,
.chat-load-hint,
.chat-load-done {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 0;
  font-size: 12px;
  color: var(--text-muted);
}

.chat-load-hint {
  cursor: pointer;
  transition: color 0.15s, opacity 0.15s;
  -webkit-tap-highlight-color: transparent;
}

.chat-load-hint:active {
  opacity: 0.6;
}

@media (hover: hover) {
  .chat-load-hint:hover {
    color: var(--text-secondary);
  }
}

.chat-load-done {
  color: var(--text-muted);
  opacity: 0.7;
  font-size: 11px;
}

.chat-load-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--border-color);
  border-top-color: var(--text-secondary);
  border-radius: 50%;
  animation: tool-spin 0.6s linear infinite;
}

@keyframes tool-spin {
  to { transform: rotate(360deg); }
}

/* Transition for load hint switching */
.load-hint-fade-enter-active {
  transition: opacity 0.2s ease-out;
}
.load-hint-fade-leave-active {
  transition: opacity 0.15s ease-in;
}
.load-hint-fade-enter-from,
.load-hint-fade-leave-to {
  opacity: 0;
}


/* ── Floating scroll buttons ── */
.scroll-fab-group {
  position: absolute;
  left: 0;
  right: 0;
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 6px;
  z-index: 3;
  pointer-events: none;
  padding: 6px 0;
}

.scroll-fab-bottom {
  bottom: 0;
}

.scroll-fab-dir {
  display: flex;
  align-items: center;
  gap: 6px;
}

/* Direction swap transition (out-in) */
.scroll-fab-swap-enter-active {
  transition: opacity 0.15s ease-out, transform 0.15s ease-out;
}

.scroll-fab-swap-leave-active {
  transition: opacity 0.1s ease-in, transform 0.1s ease-in;
}

.scroll-fab-swap-enter-from {
  opacity: 0;
  transform: translateY(6px);
}

.scroll-fab-swap-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

.scroll-fab-round {
  pointer-events: auto;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  border: 1.5px solid var(--border-color);
  border-radius: 14px;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, transform 0.15s, border-color 0.15s;
  -webkit-tap-highlight-color: transparent;
}

.scroll-fab-round:active {
  transform: scale(0.93);
}

@media (hover: hover) {
  .scroll-fab-round:hover {
    background: var(--bg-tertiary);
    color: var(--accent-color);
    border-color: var(--accent-color);
  }
}

.scroll-fab-enter-active {
  transition: opacity 0.25s ease-out, transform 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
}
.scroll-fab-leave-active {
  transition: opacity 0.2s ease-in, transform 0.2s ease-in;
}
.scroll-fab-bottom.scroll-fab-enter-from {
  opacity: 0;
  transform: translateY(16px) scale(0.9);
}
.scroll-fab-bottom.scroll-fab-leave-to {
  opacity: 0;
  transform: translateY(10px) scale(0.9);
}

/* ── User message index overlay ── */
.user-msg-overlay {
  position: fixed;
  inset: 0;
  z-index: 50;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
}

.user-msg-panel {
  min-width: 260px;
  max-width: 360px;
  max-height: 70vh;
  width: 90vw;
  background: var(--bg-secondary);
  border: 1px solid var(--border-color);
  border-radius: 16px;
  box-shadow: var(--shadow-lg);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.user-msg-panel-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 14px 18px 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.user-msg-panel-icon {
  color: var(--accent-color);
  flex-shrink: 0;
}

.user-msg-panel-count {
  margin-left: auto;
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted);
  background: var(--bg-tertiary);
  border-radius: 8px;
  padding: 2px 10px;
}

.user-msg-panel-loading {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 18px;
  font-size: 12px;
  color: var(--text-muted);
}

.user-msg-panel-list {
  overflow-y: auto;
  padding: 4px 8px 12px 4px;
}

.user-msg-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 4px 8px 4px 4px;
  border-radius: 8px;
  cursor: pointer;
  transition: background 0.15s;
  -webkit-tap-highlight-color: transparent;
  position: relative;
}

.user-msg-item:active {
  opacity: 0.7;
}

@media (hover: hover) {
  .user-msg-item:hover {
    background: var(--bg-tertiary);
  }
}

/* Timeline connector line: full height of item, passing through node center */
.user-msg-item::before {
  content: '';
  position: absolute;
  left: 16px; /* center of 24px node + 4px item padding-left */
  top: 0;
  bottom: 0;
  width: 1.5px;
  background: var(--border-color);
}

/* First item: line starts from node center, not from top */
.user-msg-item:first-child::before {
  top: 16px; /* node center: 4px padding + 12px half node */
}

/* Last item: no connector line */
.user-msg-item:last-child::before {
  display: none;
}

/* Timeline node: circle with number */
.user-msg-item-node {
  position: relative;
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--bg-tertiary);
  z-index: 1;
}

.user-msg-item-index {
  font-size: 11px;
  font-weight: 700;
  color: var(--accent-color);
  line-height: 1;
}

.user-msg-item-text {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
  word-break: break-word;
  /* Separator line below text */
  border-bottom: 1px solid var(--border-color);
  padding-bottom: 6px;
  flex: 1;
  min-width: 0;
}

/* Last item: no separator */
.user-msg-item:last-child .user-msg-item-text {
  border-bottom: none;
  padding-bottom: 0;
}

/* Overlay transition */
.user-msg-overlay-enter-active {
  transition: opacity 0.2s ease-out;
}

.user-msg-overlay-leave-active {
  transition: opacity 0.15s ease-in;
}

.user-msg-overlay-enter-from,
.user-msg-overlay-leave-to {
  opacity: 0;
}

/* ── Message highlight flash ── */
:deep(.chat-message-highlight) {
  animation: msg-highlight-flash 1.5s ease-out;
}

@keyframes msg-highlight-flash {
  0%, 15% { box-shadow: inset 0 0 0 2px var(--accent-color); }
  30%, 45% { box-shadow: inset 0 0 0 2px transparent; }
  60%, 75% { box-shadow: inset 0 0 0 2px var(--accent-color); }
  90%, 100% { box-shadow: inset 0 0 0 2px transparent; }
}
</style>
