<template>
  <div class="chat-messages-wrapper">
  <div class="chat-messages" id="aiChatMessages" ref="messagesRef" @click="handleChatClick" @mousedown="onTableMouseDown" @touchstart="onScrollAndTableTouchStart" @touchend="onScrollTouchEnd" @touchcancel="onScrollTouchEnd" @scroll="handleScroll">
    <!-- Lazy load feedback -->
    <div class="chat-load-area">
      <Transition name="load-hint-fade">
        <div v-if="loadingMore" class="chat-load-more">
          <LoadingIndicator size="sm" inline />
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
      <template v-if="agents && agents.length === 0">
        <Bot :size="40" class="no-agents-icon" />
        <span class="no-agents-title">{{ t('chat.messageList.noAgentsTitle') }}</span>
        <span class="no-agents-desc">{{ t('chat.messageList.noAgentsDesc') }}</span>
        <button class="no-agents-btn" @click="openWelcome">
          <Settings :size="16" />
          <span>{{ t('chat.messageList.noAgentsAction') }}</span>
        </button>
      </template>
      <template v-else-if="currentAgent">
        <div class="agent-welcome">
          <span class="agent-welcome-icon"><AgentIcon :backend="currentAgent.backend" :name="currentAgent.name" :size="28" /></span>
          <div class="agent-welcome-info">
            <span class="agent-welcome-name">{{ currentAgent.name }}</span>
            <span class="agent-welcome-specialty">{{ currentAgent.specialty }}</span>
            <div class="agent-welcome-tags">
              <span class="agent-welcome-tag agent-welcome-backend">{{ currentAgent.backend }}</span>
              <span v-if="currentAgent.model" class="agent-welcome-tag agent-welcome-model"><ProviderIcon :model-name="currentAgent.model" :size="11" />{{ currentAgent.model }}</span>
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
      :agents="agents"
      :staticBlockCache="staticBlockCache"
      :active="active"
      @toggle-tool="$emit('toggle-tool', $event)"
      @show-tool-detail="$emit('show-tool-detail', $event)"
      @show-metadata="$emit('show-metadata', $event)"
      @file-tag-click="$emit('file-tag-click', $event)"
      @task-card-click="$emit('task-card-click', $event)"
      @send-message="$emit('send-message', $event)"
      @render-flush="emit('render-flush')"
      @toggle-summary="$emit('toggle-summary', $event)"
      @ensure-content="$emit('ensure-content', $event)"
      @resume-session="$emit('resume-session', $event)"
      @reset-session="$emit('reset-session', $event)"

      @remove-pending="$emit('remove-pending', $event)"
      @fork-from-message="$emit('fork-from-message', $event)"
    />
    </div>
  </div>

  <!-- Floating scroll buttons — outside scroll container, inside relative wrapper -->
  <Transition name="scroll-fab">
    <div v-if="(scrolledUp || scrolledDown) && !textSelecting" ref="scrollFabRef" class="scroll-fab-group scroll-fab-bottom">
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
    </div>
  </Transition>

  <!-- User message index drawer -->
  <UserMsgIndexDrawer
    :open="userMsgIndexDrawer.effectiveOpen.value"
    :messages="userMsgIndexList"
    :active-id="nearestUserMsgId"
    :loading="loadingIndex"
    :jumping="loadingTarget"
    @close="closeUserMsgIndex"
    @select="jumpToUserMessage"
    @fork="$emit('fork-from-message', $event)"
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
import { ref, nextTick, inject, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronUp, ChevronsUp, ArrowUp, ChevronsDown, ArrowDown, Bot, Settings } from 'lucide-vue-next'
import ChatMessageItem from './ChatMessageItem.vue'
import AgentIcon from '@/components/common/AgentIcon.vue'
import LoadingIndicator from '@/components/common/LoadingIndicator.vue'
import ProviderIcon from '@/components/common/ProviderIcon.vue'
import UserMsgIndexDrawer from './UserMsgIndexDrawer.vue'
import TableRowModal from '@/components/common/TableRowModal.vue'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useTextSelectionActive } from '@/composables/useTextSelection.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useDialog } from '@/composables/useDialog'
import { useUserMsgIndex } from '@/composables/useUserMsgIndex.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { store } from '@/stores/app.ts'
import { computeRemainingCount } from '@/utils/messageListUtils.ts'
import { StreamFrameScheduler } from '@/utils/streamFrameScheduler'
import { isUserScrolling, shouldFollowStream, SCROLL_STOP_MS } from '@/utils/scrollState'

const { t } = useI18n()

function openWelcome() {
  window.dispatchEvent(new CustomEvent('clawbench-show-welcome'))
}

const props = defineProps({
  messages: Array,
  expandedTools: Object,
  blockTasks: Object,
  blockAskQuestions: Object,
  agents: Array,
  currentAgent: Object,
  currentSessionId: String,
  hasMore: Boolean,
  loadingMore: Boolean,
  totalMessages: { type: Number, default: 0 },
  staticBlockCache: Object,
  active: { type: Boolean, default: true },
})

const emit = defineEmits(['toggle-tool', 'show-tool-detail', 'show-metadata', 'file-tag-click', 'file-open', 'load-more', 'task-card-click', 'send-message', 'remove-pending', 'render-flush', 'toggle-summary', 'ensure-content', 'resume-session', 'fork-from-message', 'reset-session'])

const messagesRef = ref(null)
const { handleDblClick } = useDoubleClickCopy()
const { openFilePath } = useFilePathAnnotation()
const dialog = useDialog()
const { handleLocalhostUrlClick } = useLocalhostUrlClickHandler()

const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

// How many older messages are not yet loaded
const remainingCount = computed(() => {
  return computeRemainingCount(props.hasMore, props.totalMessages, props.messages.length)
})

// "All loaded" brief hint: shown for 2s after a user-initiated load-more completes with no more.
// Only triggers when loadingMore was recently true (i.e. user explicitly loaded more),
// not when hasMore changes during initial session load.
const showAllLoaded = ref(false)
let allLoadedTimer = null
let hadRecentLoadMore = false

watch(() => props.loadingMore, (loading) => {
  if (loading) hadRecentLoadMore = true
})

watch(() => props.hasMore, (hasMore, prevHasMore) => {
  if (!hasMore && prevHasMore && props.messages.length > 0 && hadRecentLoadMore) {
    showAllLoaded.value = true
    clearTimeout(allLoadedTimer)
    allLoadedTimer = setTimeout(() => { showAllLoaded.value = false }, 2000)
  }
  // Reset when hasMore becomes false (session fully loaded or switched)
  if (!hasMore) hadRecentLoadMore = false
})

// Note: isAtBottom reset on session switch is handled by the currentSessionId watcher below.

// Clear user message index on session switch — handled by useUserMsgIndex

// Inject bottomSheetRef from parent for closing
const chatUI = inject('chatUI', {})
const hotSwitchProject = inject('hotSwitchProject', null)

async function handleChatClick(event) {
  // 0. Code block header buttons (copy/wrap)
  if (handleCodeBlockClick(event)) return

  // 0.5. Table block header buttons (copy/wrap)
  if (handleTableBlockClick(event)) return

  // 1. Handle localhost URL clicks (icon button or <a> tag) — App mode only
  if (handleLocalhostUrlClick(event)) return

  // 2. Table row click — open row-form modal
  if (handleTableRowClick(event)) return

  // 3. Worktree action button — show modal with "Switch" or "Open directory"
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

  // 4. Commit hash click (span or button) — check before file-path to prevent
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

  // 5. File-path button handler
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

  handleDblClick(event, async (href, lineStart, lineEnd) => {
    const ok = await openFilePath(href, lineStart, lineEnd)
    if (ok) chatUI.navigateToFileViewer?.()
  })
}

let loadMorePending = false
// Track whether the user is at the bottom of the chat.
// When the user scrolls back to the bottom during streaming, auto-scroll resumes.
// Kept as a ref for external consumers (useUserMsgIndex.setAtBottom,
// ChatPanelContent.handleSummaryUpdate); internal decisions read the container
// geometry live instead of this cached flag.
const isAtBottom = ref(true)

// ── Scroll ownership state machine ──
// Unified replacement for the scattered programmaticScrolling/userTouching
// flags: who owns the scroll viewport, when the last scroll event arrived, and
// whether a force pin was deferred while the user was scrolling.
const scrollOwner = ref('idle')
let lastScrollAt = 0
let scrollStopTimer = null
let pendingFollow = false

// Hide the floating scroll buttons while the user is selecting text.
const { active: textSelecting } = useTextSelectionActive()

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

// Track active touch drag on the scroll container to prevent auto-scroll
// from fighting the user's manual scroll gesture ("sticky抖动" fix).
// NOTE: this flag alone is NOT sufficient — a fling keeps scrolling after
// touchend, so isUserScrolling() also checks the scroll-stop window.
let userTouching = false

// Throttle scrollTick for nearestUserMsgId recomputation
const scrollFrameScheduler = new StreamFrameScheduler()

function handleScroll() {
  if (!scrollFrameScheduler.has('tick')) {
    scrollFrameScheduler.schedule('tick', () => { scrollTick.value++ })
  }
  if (!messagesRef.value) return
  const el = messagesRef.value

  const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight
  const nearBottom = distFromBottom < NEAR_EDGE_THRESHOLD
  const nearTop = el.scrollTop < NEAR_EDGE_THRESHOLD
  isAtBottom.value = nearBottom

  // Scroll-stop detection: any scroll event restarts the window. After
  // SCROLL_STOP_MS with no new events the scroll is considered stopped —
  // onScrollStopped then resets ownership and flushes a deferred force pin.
  // A fling keeps firing scroll events, so the window auto-extends for its
  // whole duration (replacing the old fixed 150ms touchend window).
  clearTimeout(scrollStopTimer)
  scrollStopTimer = setTimeout(onScrollStopped, SCROLL_STOP_MS)
  // Only a user-initiated scroll (not programmatic smooth scroll) claims
  // ownership — otherwise a scrollIntoView jump would be mistaken for the
  // user actively scrolling and suppress stream follow.
  if (!programmaticScrolling) {
    scrollOwner.value = 'user'
    lastScrollAt = Date.now()
  }

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

  // Scrolled up (toward top): show up buttons, hide down — but not if already near top
  const shouldShowUp = scrollDelta < 0 && distFromBottom > SCROLL_BUTTON_TRIGGER && !nearTop
  // Scrolled down (toward bottom): show down buttons, hide up — but not if already near bottom
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

// Touch tracking: during an active touch drag, pause auto-scroll so it
// doesn't fight the user's scroll gesture (causing "sticky抖动").
function onScrollAndTableTouchStart(e) {
  userTouching = true
  scrollOwner.value = 'user'
  onTableTouchStart(e)  // preserve table-row-expand handling
}

function onScrollTouchEnd() {
  // The old fixed 150ms delay is replaced by scroll-stop detection: the touch
  // flag alone gates auto-scroll, while the fling's continued scroll events
  // keep refreshing lastScrollAt (so isUserScrolling() stays true) until the
  // fling actually stops, at which point onScrollStopped restores follow.
  userTouching = false
}

/**
 * Called SCROLL_STOP_MS after the last scroll event. Resets scroll ownership
 * and flushes a deferred force pin — but only if the user is still near the
 * bottom (they scrolled away while a force pin was pending → don't pull them).
 * pendingFollow is ALWAYS cleared here, whether or not the pin is flushed —
 * otherwise a stale flag would fire a pin the next time the user scrolls back
 * to the bottom.
 */
function onScrollStopped() {
  // A programmatic smooth scroll (FAB jump, message index) stops producing
  // scroll events here too — end its ownership so subsequent events are read
  // as user scrolls again (replaces the old fixed 600ms programmatic timeout).
  if (programmaticScrolling) {
    setProgrammatic(false)
    return
  }
  scrollOwner.value = 'idle'
  const el = messagesRef.value
  if (!el) return
  const dist = el.scrollHeight - el.scrollTop - el.clientHeight
  if (dist <= NEAR_EDGE_THRESHOLD) isAtBottom.value = true
  if (pendingFollow) {
    pendingFollow = false
    if (dist <= NEAR_EDGE_THRESHOLD) {
      scrollToBottom(true)
    }
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

// Desktop: Ctrl+Up/Down to jump between messages (user or assistant), matching
// the conversation index navigation (wraps around; same scroll+highlight path).
function handleCtrlArrowMsgJump(e) {
  if (!props.active) return
  const tag = e.target?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  if (e.target?.closest?.('.terminal-panel')) return
  if (!(e.ctrlKey || e.metaKey)) return
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    jumpToAdjacentMessage('prev', nearestMessageId.value)
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    jumpToAdjacentMessage('next', nearestMessageId.value)
  }
}

onMounted(() => document.addEventListener('click', onDocumentClick, true))
onMounted(() => document.addEventListener('keydown', handleCtrlArrowMsgJump))
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', handleCtrlArrowMsgJump)
  scrollFrameScheduler.cancelAll()
  clearTimeout(scrollStopTimer)
  scrollStopTimer = null
  clearTimeout(programmaticFallbackTimer)
  programmaticFallbackTimer = null
})

function scrollToBottom(force = false) {
  nextTick(() => {
    if (!messagesRef.value) return
    const el = messagesRef.value
    // Live geometry — never trust the cached isAtBottom ref, which lags the
    // actual scroll position (scroll events are async).
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight
    const state = () => ({
      owner: scrollOwner.value,
      userTouching,
      lastScrollAt,
      now: Date.now(),
      nearBottomDist: dist,
    })

    // User is actively scrolling/flinging → never yank the view. A force pin
    // is deferred and flushed once by onScrollStopped (if still near bottom).
    if (isUserScrolling(state())) {
      if (force) pendingFollow = true
      return
    }
    if (!shouldFollowStream(state(), force)) return

    // Mark the write as programmatic so the scroll event it emits is not
    // misread as a user scroll (which would make the rAF correction below
    // suppress itself via isUserScrolling). Ownership is released by
    // onScrollStopped ~SCROLL_STOP_MS after the emitted scroll event.
    setProgrammatic(true)
    el.scrollTop = el.scrollHeight
    // Verify the scroll actually reached the bottom — content may have grown
    // between the scrollToBottom call and this nextTick callback, or may grow
    // after this callback completes (streaming text, throttled render flush).
    // Re-check after the browser has laid out the DOM changes, and re-scroll if
    // still not at the bottom. Same guards as the initial scroll: never override
    // an active user scroll, never follow once the user has scrolled away
    // (unless force — async lazy content must still be corrected even if the
    // user is stationary but not at the bottom).
    requestAnimationFrame(() => {
      if (!messagesRef.value) return
      const el2 = messagesRef.value
      const gap = el2.scrollHeight - el2.scrollTop - el2.clientHeight
      // Sync the externally-consumed isAtBottom flag: a pin with zero gap
      // emits no scroll event, so handleScroll never runs.
      isAtBottom.value = gap <= NEAR_EDGE_THRESHOLD
      if (gap <= 0) return
      const state2 = {
        owner: scrollOwner.value,
        userTouching,
        lastScrollAt,
        now: Date.now(),
        nearBottomDist: gap,
      }
      if (isUserScrolling(state2)) return
      if (shouldFollowStream(state2, force)) {
        el2.scrollTop = el2.scrollHeight
        isAtBottom.value = true
      }
    })
    // NOTE: the old unconditional force pin timer (300ms) is gone. Async
    // content growth (Mermaid, KaTeX, lazy original fetch, thinking collapse)
    // is handled by the rAF correction above; if the user started scrolling in
    // between, pendingFollow + onScrollStopped take over instead of fighting.
  })
}

function scrollToTop() {
  if (!messagesRef.value) return
  clearTimeout(scrollUpTimer)
  scrollUpTimer = setTimeout(() => { scrolledUp.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  setProgrammatic(true)
  messagesRef.value.scrollTo({ top: 0, behavior: 'smooth' })
  // Ownership released by onScrollStopped when the smooth scroll settles.
}

function highlightMessage(el) {
  el.classList.add('chat-message-highlight')
  setTimeout(() => el.classList.remove('chat-message-highlight'), 1500)
}

// Fallback timeout so programmatic ownership never gets stuck: if a smooth
// scroll produces no scroll events (e.g. target already in view), onScrollStopped
// never fires; this caps programmatic ownership and resets it.
let programmaticFallbackTimer = null
const PROGRAMMATIC_MAX_MS = 1500

/**
 * Unified programmatic-scroll flag setter. Keeps scrollOwner in sync so a
 * programmatic smooth scroll (FAB, message index jump) is never mistaken for
 * a user scroll, and so shouldFollowStream treats programmatic jumps correctly.
 *
 * Ownership is normally released by onScrollStopped (SCROLL_STOP_MS after the
 * last scroll event of the smooth scroll), replacing the old fixed 600ms
 * timeout — a long scrollIntoView no longer gets misread as a user scroll.
 */
function setProgrammatic(val) {
  programmaticScrolling = val
  scrollOwner.value = val ? 'programmatic' : 'idle'
  clearTimeout(programmaticFallbackTimer)
  programmaticFallbackTimer = null
  if (val) {
    // Safety net in case the smooth scroll never emits a scroll event.
    programmaticFallbackTimer = setTimeout(() => {
      programmaticScrolling = false
      scrollOwner.value = 'idle'
      programmaticFallbackTimer = null
    }, PROGRAMMATIC_MAX_MS)
  }
}

/** Scroll a message element into view at the top of the viewport, with highlight animation. */
function scrollAndHighlight(itemEl) {
  setProgrammatic(true)
  highlightMessage(itemEl)
  itemEl.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

function scrollToPreviousMessage() {
  if (!messagesRef.value) return
  clearTimeout(scrollUpTimer)
  scrollUpTimer = setTimeout(() => { scrolledUp.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  setProgrammatic(true)
  const el = messagesRef.value
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  if (items.length === 0) { setProgrammatic(false); return }
  // Find the first message whose bottom is above the viewport top
  for (let i = items.length - 1; i >= 0; i--) {
    const rect = items[i].getBoundingClientRect()
    const containerRect = el.getBoundingClientRect()
    if (rect.bottom < containerRect.top + 8) {
      scrollAndHighlight(items[i])
      return
    }
  }
  // If no message is above, scroll to top
  el.scrollTo({ top: 0, behavior: 'smooth' })
}

function scrollToNextMessage() {
  if (!messagesRef.value) return
  clearTimeout(scrollDownTimer)
  scrollDownTimer = setTimeout(() => { scrolledDown.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  setProgrammatic(true)
  const el = messagesRef.value
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  if (items.length === 0) { setProgrammatic(false); return }
  // Find the first message whose top is below the viewport bottom
  for (let i = 0; i < items.length; i++) {
    const rect = items[i].getBoundingClientRect()
    const containerRect = el.getBoundingClientRect()
    if (rect.top > containerRect.bottom - 8) {
      scrollAndHighlight(items[i])
      return
    }
  }
  // If no message is below, scroll to bottom
  setProgrammatic(false)
  scrollToBottomSmooth()
}

function scrollToBottomSmooth() {
  if (!messagesRef.value) return
  clearTimeout(scrollDownTimer)
  scrollDownTimer = setTimeout(() => { scrolledDown.value = false }, SCROLL_BUTTON_HIDE_DELAY)
  setProgrammatic(true)
  const el = messagesRef.value
  el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
  // Ownership released by onScrollStopped when the smooth scroll settles.
}

// ── User message index ──
const {
  userMsgIndexList,
  drawer: userMsgIndexDrawer,
  loadingTarget,
  loadingIndex,
  toggleUserMsgIndex,
  closeUserMsgIndex,
  jumpToUserMessage,
  jumpToAdjacentMessage,
  scrollToMessage: scrollToMessageUserMsg,
} = useUserMsgIndex({
  getMessages: () => props.messages,
  getCurrentSessionId: () => props.currentSessionId || '',
  getHasMore: () => props.hasMore,
  getLoadingMore: () => props.loadingMore,
  emitLoadMore: () => emit('load-more'),
  getMessagesRef: () => messagesRef.value,
  hideScrollFab,
  setProgrammaticScrolling: (val) => { setProgrammatic(val) },
  setAtBottom: (val) => { isAtBottom.value = val },
})

// Nearest user message to viewport center — used for activeId highlight in index
const scrollTick = ref(0)
const nearestUserMsgId = computed(() => {
  void scrollTick.value // dependency trigger
  const el = messagesRef.value
  if (!el) return null
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  const containerRect = el.getBoundingClientRect()
  const center = containerRect.top + containerRect.height / 2
  let nearestUserIdx = null
  let minDist = Infinity
  for (let i = 0; i < items.length; i++) {
    const msg = props.messages[i]
    if (!msg || msg.role !== 'user') continue
    const rect = items[i].getBoundingClientRect()
    const dist = Math.abs(rect.top + rect.height / 2 - center)
    if (dist < minDist) {
      minDist = dist
      nearestUserIdx = i
    }
  }
  if (nearestUserIdx === null) return null
  return props.messages[nearestUserIdx].id
})

// Nearest message of any role to viewport center — anchor for Ctrl+↑/↓ jump
const nearestMessageId = computed(() => {
  void scrollTick.value // dependency trigger
  const el = messagesRef.value
  if (!el) return null
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  const containerRect = el.getBoundingClientRect()
  const center = containerRect.top + containerRect.height / 2
  let nearestIdx = null
  let minDist = Infinity
  for (let i = 0; i < items.length; i++) {
    const msg = props.messages[i]
    if (!msg) continue
    const rect = items[i].getBoundingClientRect()
    const dist = Math.abs(rect.top + rect.height / 2 - center)
    if (dist < minDist) {
      minDist = dist
      nearestIdx = i
    }
  }
  if (nearestIdx === null) return null
  return props.messages[nearestIdx].id
})

// Watch session switch to reset scroll state and user msg index
watch(() => props.currentSessionId, () => {
  isAtBottom.value = true
  scrolledUp.value = false
  scrolledDown.value = false
  lastScrollTop = 0
  setProgrammatic(false)
  lastScrollAt = 0
  pendingFollow = false
  clearTimeout(scrollStopTimer)
  scrollStopTimer = null
  userTouching = false
  clearTimeout(scrollUpTimer)
  clearTimeout(scrollDownTimer)
  scrollFrameScheduler.cancelAll()
  scrollTick.value = 0
  userMsgIndexDrawer.close()
  userMsgIndexList.value = []
  // Reset "all loaded" hint and load-more tracking on session switch
  showAllLoaded.value = false
  hadRecentLoadMore = false
  clearTimeout(allLoadedTimer)
})

// ── Scroll anchoring on message array replacement ──
// loadHistory / session switch can replace the whole messages array. When the
// user is NOT at the bottom, keep the viewport anchored to the first visible
// message instead of letting the browser's scrollTop clamping jump the view.
// rebuildFromDb preserves object identity for matched rows (stable v-for keys,
// no DOM rebuild) so this watcher only fires on real array replacement.
let scrollAnchor = null

function captureAnchor(el) {
  const items = el.querySelectorAll('.chat-messages-list > .chat-message')
  const containerRect = el.getBoundingClientRect()
  for (const item of items) {
    const rect = item.getBoundingClientRect()
    if (rect.bottom > containerRect.top && rect.top < containerRect.bottom) {
      return { key: item.getAttribute('data-msg-key') || '', offset: rect.top - containerRect.top }
    }
  }
  // Fallback: no visible message — remember the container position itself
  return { key: '', offset: 0 }
}

function restoreAnchor(el, anchor) {
  if (anchor.key) {
    const items = el.querySelectorAll('.chat-messages-list > .chat-message')
    for (const item of items) {
      if (item.getAttribute('data-msg-key') === anchor.key) {
        const rect = item.getBoundingClientRect()
        const containerRect = el.getBoundingClientRect()
        const desiredTop = containerRect.top + anchor.offset
        el.scrollTop += rect.top - desiredTop
        return
      }
    }
  }
  // Fallback: anchor message gone — preserve relative position by restoring
  // the previous scrollTop-delta (same idea as handleLoadMore).
  if (el.__prevScrollHeight && el.__prevScrollTop != null) {
    const delta = el.scrollHeight - el.__prevScrollHeight
    el.scrollTop = Math.max(0, el.__prevScrollTop + delta)
  }
  delete el.__prevScrollHeight
  delete el.__prevScrollTop
}

watch(() => props.messages, (newMsgs, oldMsgs) => {
  const el = messagesRef.value
  if (!el || !oldMsgs || oldMsgs.length === 0 || !newMsgs || newMsgs.length === 0) return
  if (el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_EDGE_THRESHOLD) return // at bottom → let scrollToBottom pin
  el.__prevScrollHeight = el.scrollHeight
  el.__prevScrollTop = el.scrollTop
  scrollAnchor = captureAnchor(el)
  nextTick(() => {
    if (!scrollAnchor || !messagesRef.value) return
    restoreAnchor(messagesRef.value, scrollAnchor)
    scrollAnchor = null
  })
})

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
  closeUserMsgIndex,
  toggleUserMsgIndex,
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
  padding: 12px 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* Message list container */
.chat-messages-list {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.chat-empty {
  text-align: center;
  padding: 32px 16px;
  color: var(--text-muted);
  font-size: 13px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  flex: 1;
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

/* No agents empty state */
.no-agents-icon {
  color: var(--text-muted);
  opacity: 0.5;
}

.no-agents-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
}

.no-agents-desc {
  font-size: 12px;
  color: var(--text-muted);
  max-width: 240px;
  text-align: center;
  line-height: 1.5;
}

.no-agents-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.15s ease, border-color 0.15s ease;
  -webkit-tap-highlight-color: transparent;
}

.no-agents-btn:active {
  background: var(--bg-tertiary);
}

@media (hover: hover) {
  .no-agents-btn:hover {
    background: var(--bg-tertiary);
    border-color: var(--accent-color);
  }
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
  gap: 12px;
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
  width: 30px;
  height: 30px;
  background: var(--bg-primary);
  color: var(--text-secondary);
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.35));
  border-radius: 50%;
  cursor: pointer;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.18);
  opacity: 0.6;
  transition: background 0.15s, color 0.15s, transform 0.15s, border-color 0.15s, opacity 0.15s;
  -webkit-tap-highlight-color: transparent;
}

.scroll-fab-round:focus-visible {
  opacity: 1;
  background: var(--bg-tertiary);
}
@media (hover: hover) {
  .scroll-fab-round:hover {
    opacity: 1;
    background: var(--bg-tertiary);
  }
}

.scroll-fab-round:not(:disabled):active {
  transform: scale(0.94);
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
