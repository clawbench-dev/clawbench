<template>
  <div class="chat-messages-wrapper">
  <!-- Lazy load feedback — floating overlay pinned to top of the message area,
       outside the scroll container so it never scrolls with the message flow. -->
  <div class="chat-load-area">
    <Transition name="load-hint-fade">
      <div v-if="loadingMore" class="chat-load-more">
        <LoadingIndicator size="sm" inline />
        <span>{{ t('chat.messageList.loadingMore') }}</span>
      </div>
      <div v-else-if="showMoreHint" class="chat-load-hint" @click="emit('load-more')">
        <ChevronUp :size="14" />
        <span>{{ t('chat.messageList.moreOlderMessages', { count: remainingCount }) }}</span>
      </div>
      <div v-else-if="showAllLoaded" class="chat-load-done">
        <span>{{ t('chat.messageList.allMessagesLoaded') }}</span>
      </div>
    </Transition>
  </div>

  <div class="chat-messages" id="aiChatMessages" ref="messagesRef" @click="handleChatClick" @mousedown="onContainerMouseDown" @touchstart="onScrollAndTableTouchStart" @touchend="onScrollTouchEnd" @touchcancel="onScrollTouchEnd" @wheel="onWheelScroll" @scroll="handleScroll">
    <div class="chat-messages-list" :key="listKey">
      <!-- Session switching in progress: the old messages were cleared but the
           new session's history is still loading — show a centered spinner in
           place of the empty state instead of a full-area overlay mask. -->
      <LoadingIndicator
        v-if="props.switching && messages.length === 0"
        class="chat-switching-indicator"
        size="md"
      />
      <div v-else-if="messages.length === 0" class="chat-empty">
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
      :isLastAssistant="isLastAssistant(msg, i)"
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

  <!-- Code link preview for chat messages -->
  <CodeLinkPreview
    v-if="codeLinkPreview.enabled.value"
    :preview="codeLinkPreview"
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
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import { useDoubleClickCopy } from '@/composables/useDoubleClickCopy.ts'
import { useCodeLinkPreview } from '@/composables/useCodeLinkPreview.ts'
import { useTextSelectionActive } from '@/composables/useTextSelection.ts'
import { useFilePathAnnotation } from '@/composables/useFilePathAnnotation.ts'
import { handleCodeBlockClick, handleTableBlockClick, closeAllTableBlockMenus } from '@/composables/useCodeBlockHeader.ts'
import { useLocalhostUrlClickHandler } from '@/composables/useLocalhostAnnotation.ts'
import { useDialog } from '@/composables/useDialog'
import { useUserMsgIndex } from '@/composables/useUserMsgIndex.ts'
import { useTableRowExpand } from '@/composables/useTableRowExpand.ts'
import { store } from '@/stores/app.ts'
import { computeRemainingCount } from '@/utils/messageListUtils.ts'
import { StreamFrameScheduler } from '@/utils/streamFrameScheduler'
import { isUserScrolling, shouldPin, SCROLL_STOP_MS, NEAR_BOTTOM_PX, RESUME_FOLLOW_PX, updateUserLeftBottom } from '@/utils/scrollState'
import { appLog } from '@/utils/appLog'
import { isLastAssistantMessage } from '@/utils/chatSessionUtils'

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
  /** True while a session switch is in flight (old messages cleared, new history loading). */
  switching: { type: Boolean, default: false },
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
const codeLinkPreview = useCodeLinkPreview({ containerRef: messagesRef })

// Whether a message is the most recent assistant reply (drives the 'mixed'
// display mode: the last assistant reply renders as original text, older ones
// as summaries). Identity comparison against the full ordered list.
function isLastAssistant(msg, _i) {
  return isLastAssistantMessage(props.messages, msg)
}

const { tableRowModal, closeTableRowModal, tableRowPrev, tableRowNext, handleTableRowClick, onTableMouseDown, onTableTouchStart } = useTableRowExpand()

// How many older messages are not yet loaded
const remainingCount = computed(() => {
  return computeRemainingCount(props.hasMore, props.totalMessages, props.messages.length)
})

/**
 * DOM reconciliation key for the message list container.
 *
 * Combines session identity with a structural snapshot of message IDs so
 * that Vue unmounts and rebuilds the entire message list whenever the
 * authoritative DB replaces the array (loadHistory / rebuildFromDb drops
 * transient bubbles).  Without this, a transient message whose id changes
 * from string (pending-xxx) to numeric (DB id) can leave a stale DOM node
 * behind because Vue's diff sees a key change on a sibling but may not
 * correctly prune the old element in certain WebView/GPU compositor states.
 *
 * - First segment: session id (switches force full rebuild on session change).
 * - Second segment: count of messages (catches shrink/grow).
 * - Third segment: first + last message id (catches array replacement with
 *   same count, e.g. loadHistory adopting all transient messages).
 *
 * The key is intentionally cheap (string concat) and only changes on structural
 * events, so streaming text updates (same array, same ids) do NOT re-mount.
 */
const listKey = computed(() => {
  const msgs = props.messages || []
  const first = msgs[0]?.id ?? ''
  const last = msgs[msgs.length - 1]?.id ?? ''
  return `${props.currentSessionId || 'no-session'}|${msgs.length}|${first}|${last}`
})

// Re-observe the content-growth target when listKey rebuilds the DOM
// (.chat-messages-list is recreated). The watcher fires pre-flush; the new
// element exists after the next tick.
watch(listKey, () => {
  nextTick(() => observeContentGrowth())
})

// "More older messages" transient hint: whenever older messages remain, the
// pill briefly appears then auto-hides. `immediate: true` announces remaining
// history on first render (session opened with more to load) without keeping
// it resident; subsequent loads re-arm it via remainingCount change.
const showMoreHint = ref(false)
let moreHintTimer = null

watch(() => props.hasMore && remainingCount.value > 0, (hasRemaining) => {
  clearTimeout(moreHintTimer)
  if (hasRemaining) {
    showMoreHint.value = true
    moreHintTimer = setTimeout(() => { showMoreHint.value = false }, 2500)
  } else {
    // All history loaded — hide immediately so the "all loaded" hint can show.
    showMoreHint.value = false
  }
}, { immediate: true })

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

  // Code link preview: handle desktop clicks or touch taps on code link paths.
  // Only verified *file* paths are intercepted — directories and paths that have
  // not yet been verified (data-path-type unset) fall through to the original
  // handlers below (anchor navigation / open button), preserving the pre-feature
  // behavior for those cases.
  if (codeLinkPreview.enabled.value) {
    const isTouch = codeLinkPreview.isTouchDevice()
    const isModifier = !isTouch && (event.ctrlKey || event.metaKey)
    const linkOrBtn = (event.target).closest('.chat-file-path[data-file-path], .chat-file-open-btn[data-file-path]')
    const pathEl = (event.target).closest('.chat-file-path[data-file-path]')
    const isVerifiedFile = linkOrBtn?.getAttribute('data-path-type') === 'file'
    if (isVerifiedFile && ((isModifier && linkOrBtn) || (!isTouch && pathEl))) {
      codeLinkPreview.handleClick(event)
      return
    }
    if (isVerifiedFile && isTouch && pathEl) {
      codeLinkPreview.handleClick(event)
      return
    }
  }

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
        codeLinkPreview.close()
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
    codeLinkPreview.close()
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
    codeLinkPreview.close()
    const ok = await openFilePath(href, lineStart, lineEnd)
    if (ok) chatUI.navigateToFileViewer?.()
  })
}

let loadMorePending = false
// Re-arm guard for load-more: once fired at the top, stays armed until the
// async loadMore completes (loadingMore flips false). Prevents a scroll at the
// top from firing multiple overlapping load-more requests — previously the
// pending flag was cleared on the next tick, so several small scrolls in the
// top zone each fired a fresh request.
watch(() => props.loadingMore, (loading) => {
  if (!loading) {
    // Load finished (success or failure) — allow the next top scroll to fire.
    loadMorePending = false
  }
})
// Safety net: if hasMore re-appears without a loadingMore cycle (e.g. a
// loadHistory picked up new messages after a load-more was skipped by the
// hasMore guard), re-arm so the top scroll can trigger again.
watch(() => props.hasMore, (hasMore) => {
  if (hasMore) loadMorePending = false
})
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

// Whether the user has deliberately scrolled away from the bottom during a
// stream. While set, ALL stream follow is suppressed — a user reading older
// content must never be yanked back to the bottom. Cleared only when the user
// scrolls back to the bottom, or on session switch.
let userLeftBottom = false

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

// "At the bottom" threshold — shared single source of truth with
// scrollState.ts (NEAR_BOTTOM_PX). The top-edge threshold stays separate:
// it only decides when the scroll-up FAB hides, which should not grow with
// the (deliberately generous) bottom threshold.
const NEAR_TOP_THRESHOLD = 100
// How far the user must scroll away from an edge before a FAB appears.
// Independently equal to NEAR_BOTTOM_PX (both 200) — keep in sync if the
// bottom threshold changes materially, but they serve different purposes:
// NEAR_BOTTOM_PX gates stream-follow, SCROLL_BUTTON_TRIGGER gates FAB display.
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
  // `<=` so the boundary (exactly NEAR_BOTTOM_PX) is "at the bottom", matching
  // the userLeftBottom latch below (`> NEAR_BOTTOM_PX` = left) with no gap.
  const nearBottom = distFromBottom <= NEAR_BOTTOM_PX
  const nearTop = el.scrollTop < NEAR_TOP_THRESHOLD
  isAtBottom.value = nearBottom

  // Scroll-stop detection: any scroll event restarts the window. After
  // SCROLL_STOP_MS with no new events the scroll is considered stopped —
  // onScrollStopped then resets ownership and flushes a deferred force pin.
  // A fling keeps firing scroll events, so the window auto-extends for its
  // whole duration (replacing the old fixed 150ms touchend window).
  clearTimeout(scrollStopTimer)
  scrollStopTimer = setTimeout(onScrollStopped, SCROLL_STOP_MS)
  // ONLY a deliberate user scroll claims ownership: a touch drag
  // (touchstart…touchend), a mouse-wheel scroll, or a scrollbar drag on PC.
  // Content-growth scroll events (async render pushing the viewport,
  // programmatic pins) fire with none of these flags set and must NOT be
  // misread as the user scrolling — otherwise `isUserScrolling` stays true
  // forever (lastScrollAt keeps refreshing on every growth scroll) and every
  // force pin is deferred into a pendingFollow that onScrollStopped never
  // flushes (the growth scroll stream never stops). That is the "fixed session
  // never scrolls to bottom" bug.
  // A fling continues via lastScrollAt: ownership was claimed during the touch
  // and stays 'user' until the scroll-stop window elapses.
  // NOTE: the latch block does NOT gate on `!programmaticScrolling`. During a
  // stream, followToBottom re-arms setProgrammatic(true) on every frame, so
  // programmaticScrolling stays true for the whole stream — gating on it would
  // block user-scroll detection entirely and the user could never escape the
  // stream's pin (the "scrolled far away but still dragged back" bug). User
  // drags are distinguished by the input flags (userTouching / wheelActive /
  // mouseDownActive) which programmatic pins never set.
  // Direction detection needs the previous scroll position from EVERY event —
  // including programmatic stream pins (which return early below and would
  // otherwise skip updating lastScrollTop). If lastScrollTop froze at a stale
  // pre-stream value (typically 0), every subsequent upward drag reads
  // `el.scrollTop < lastScrollTop` as false, so the userLeftBottom latch never
  // fires and streamed pins keep yanking the user back to the bottom — the
  // "无论如何向上拖拽都会被拽回到底部" bug. Capture before any branch.
  const prevScrollTop = lastScrollTop
  lastScrollTop = el.scrollTop

  if (userTouching || wheelActive || mouseDownActive) {
    scrollOwner.value = 'user'
    lastScrollAt = Date.now()
    // Track whether the user deliberately left the bottom. The latch is
    // direction-driven, NOT distance-driven: any upward drag immediately marks
    // a deliberate leave — the user is trying to read older content and a
    // streamed pin must never fight them. The old distance-only check let a
    // user resting inside the near-bottom band (distFromBottom <= NEAR_BOTTOM_PX)
    // stay "at the bottom", so the next streamed pin yanked them back — the
    // "很难拖上去、抽搐" (snap-back jitter) bug. Clearing happens only when
    // they scroll back to within RESUME_FOLLOW_PX of the bottom (an explicit
    // return), via updateUserLeftBottom.
    const prevUserLeftBottom = userLeftBottom
    userLeftBottom = updateUserLeftBottom(userLeftBottom, {
      scrollingUp: el.scrollTop < prevScrollTop,
      distFromBottom,
    })
    if (userLeftBottom && !prevUserLeftBottom) {
      appLog.d('ChatScroll', `userLeftBottom latched: dist=${distFromBottom.toFixed(0)} top=${el.scrollTop.toFixed(0)} h=${el.scrollHeight} prog=${programmaticScrolling}`)
    }
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
    // A programmatic jump to the top (FAB scroll-to-top) reaches scrollTop=0
    // just like a manual scroll — load older history the same way. This check
    // must run BEFORE the programmatic return below, otherwise the top-FAB
    // scroll never triggers load-more (only a subsequent manual scroll does).
    if (loadMorePending) return
    if (!props.hasMore || props.loadingMore) return
    if (el.scrollTop < 50) {
      loadMorePending = true
      emit('load-more')
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

  // Determine scroll direction. lastScrollTop was already updated at the top
  // of handleScroll (before any branch), so `prevScrollTop` is the previous
  // position from the immediately-preceding event.
  const scrollDelta = el.scrollTop - prevScrollTop

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
  // Load-more fires only when truly pinned to the top (< 50px), tighter than
  // NEAR_TOP_THRESHOLD (FAB hide) — near the top the user is still reading,
  // fetching history must not trigger on a casual swipe.
  if (el.scrollTop < 50) {
    loadMorePending = true
    emit('load-more')
  }
}

// Touch tracking: during an active touch drag, pause auto-scroll so it
// doesn't fight the user's scroll gesture (causing "sticky抖动").
function onScrollAndTableTouchStart(e) {
  userTouching = true
  scrollOwner.value = 'user'
  onTableTouchStart(e)  // preserve table-row-expand handling
}

// PC: a mouse-wheel scroll is a deliberate user scroll just like a touch drag.
// Wheel has no explicit "end" event, so we mark it active and let the scroll-
// stop window (onScrollStopped, SCROLL_STOP_MS) clear it — the wheel's scroll
// events keep refreshing lastScrollAt until the wheel stops.
let wheelActive = false

function onWheelScroll() {
  wheelActive = true
  scrollOwner.value = 'user'
  lastScrollAt = Date.now()
}

// PC: mouse press inside the list may start a scrollbar drag — another
// deliberate user scroll with no touch/wheel event. Mark the input active;
// cleared by the scroll-stop window like wheel.
let mouseDownActive = false

function onContainerMouseDown(e) {
  mouseDownActive = true
  scrollOwner.value = 'user'
  lastScrollAt = Date.now()
  onTableMouseDown(e)  // preserve table-row-expand handling
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
  // Any scroll stream stopped (user drag/wheel OR programmatic smooth scroll):
  // release programmatic ownership first so the next scroll events are read as
  // user scrolls again. The input flags and ownership are reset below.
  if (programmaticScrolling) setProgrammatic(false)
  // The scroll has stopped: clear the wheel/mouse-drag active flags (no
  // explicit "end" event exists for them) so later content-growth scrolls are
  // not misread as user input.
  wheelActive = false
  mouseDownActive = false
  scrollOwner.value = 'idle'
  const el = messagesRef.value
  if (!el) return
  const dist = el.scrollHeight - el.scrollTop - el.clientHeight
  // Returning to the bottom is an explicit gesture — clear the latch only when
  // the user actually came back (within RESUME_FOLLOW_PX), not merely inside
  // the generous NEAR_BOTTOM_PX band (which would re-enable snap-back while
  // they rest inside it mid-stream).
  if (dist <= RESUME_FOLLOW_PX) {
    isAtBottom.value = true
    userLeftBottom = false
  }
  if (pendingFollow) {
    pendingFollow = false
    // A deferred force pin is always flushed here. pendingFollow is ONLY set
    // by explicit user-intent pins (sending a message, answering a question
    // card, switching sessions) — the user took an action and expects to see
    // the bottom of the conversation. Delaying it must not drop it: the user
    // may have answered a card while sitting far above the bottom (reading
    // earlier context), and after their scroll stops the pin must still pull
    // them down so they can see their answer land and the AI continue. This is
    // NOT the stream-follow path — stream pins are non-force and never set
    // pendingFollow, so a user reading history is still never yanked.
    scrollToBottom(true)
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
  // Close any open table block copy menus when clicking elsewhere
  if (!e.target.closest('.table-block-copy-menu') && !e.target.closest('.table-block-copy-btn')) {
    closeAllTableBlockMenus()
  }
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

// ── Content-growth observer ──
// The chat list height can grow asynchronously from MANY sources: throttled
// render flush (300ms + rAF), deferred Mermaid rendering, lazy-loaded original
// text, task-card data fills. Any of these can push the viewport away from the
// bottom AFTER the initial scroll-to-bottom ran, and no single code path emits
// a follow-up scroll. This observer is the universal backstop: whenever the
// content height grows while the user has NOT deliberately scrolled away, we
// re-pin to the bottom. Guards mirror followToBottom (never fight an active
// user scroll, never pull back a user who scrolled away).
let contentResizeObserver = null
let contentGrownRaf = 0

function onContentGrown() {
  if (!messagesRef.value) return
  // Coalesce bursts (multiple blocks flushing in one frame) into one pass.
  cancelAnimationFrame(contentGrownRaf)
  contentGrownRaf = requestAnimationFrame(() => {
    const el = messagesRef.value
    if (!el) return
    // Unified pin decision: never fight an active user scroll, never pull
    // back a user who deliberately scrolled away (non-force).
    if (!shouldPin(buildScrollState(), false)) return
    // Skip when the content growth already kept the view glued to the bottom
    // (gap <= 0) — writing the same scrollTop would emit an unnecessary scroll
    // event that restarts the 250ms user-scroll window during streaming.
    if (el.scrollHeight - el.scrollTop - el.clientHeight <= 0) return
    el.scrollTop = el.scrollHeight
    isAtBottom.value = true
  })
}

function observeContentGrowth() {
  contentResizeObserver?.disconnect()
  contentResizeObserver = null
  const el = messagesRef.value
  if (!el || typeof ResizeObserver === 'undefined') return
  // Observe the content wrapper (.chat-messages-list) — its box size IS the
  // content height and grows when async content renders. It is recreated on
  // listKey change, so re-observe via the listKey watcher below.
  const inner = el.firstElementChild
  if (inner) {
    contentResizeObserver = new ResizeObserver(() => onContentGrown())
    contentResizeObserver.observe(inner)
  }
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', handleCtrlArrowMsgJump)
  observeContentGrowth()
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', handleCtrlArrowMsgJump)
  contentResizeObserver?.disconnect()
  contentResizeObserver = null
  cancelAnimationFrame(contentGrownRaf)
  scrollFrameScheduler.cancelAll()
  clearTimeout(scrollStopTimer)
  scrollStopTimer = null
  clearTimeout(programmaticFallbackTimer)
  programmaticFallbackTimer = null
})

function scrollToBottom(force = false) {
  nextTick(() => {
    if (!messagesRef.value) return
    // User is actively scrolling/flinging → never yank the view. A force pin
    // is deferred and flushed once by onScrollStopped (if still near bottom).
    if (isUserScrolling(buildScrollState())) {
      if (force) pendingFollow = true
      appLog.d('ChatScroll', `scrollToBottom deferred (user scrolling) force=${force}`)
      return
    }
    // Mark the write as programmatic so the scroll event it emits is not
    // misread as a user scroll (which would make the rAF correction below
    // suppress itself via isUserScrolling). Ownership is released by
    // onScrollStopped ~SCROLL_STOP_MS after the emitted scroll event.
    if (shouldPin(buildScrollState(), force)) {
      followToBottom(force)
    } else {
      appLog.d('ChatScroll', `scrollToBottom REJECTED force=${force} userLeftBottom=${userLeftBottom}`)
    }
  })
}

/** Current scroll-ownership snapshot fed into the follow decision. */
function buildScrollState() {
  return {
    owner: scrollOwner.value,
    userTouching,
    lastScrollAt,
    now: Date.now(),
    userLeftBottom,
  }
}

function followToBottom(force) {
  setProgrammatic(true)
  // A force pin (send message / answer card / deferred force flush after the
  // user's scroll stops) is an explicit intent to be at the bottom. Clear the
  // "left the bottom" latch so the AI reply streaming BELOW the just-sent
  // message keeps following — otherwise every subsequent non-force pin is
  // rejected and the view stays stuck at the user bubble (the "sends but the
  // streamed reply is never followed" bug).
  if (force) userLeftBottom = false
  const el = messagesRef.value
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
    isAtBottom.value = gap <= NEAR_BOTTOM_PX
    // Already glued to the bottom → nothing to correct. Skipping the write
    // avoids the unconditional scroll event that re-triggers handleScroll's
    // 250ms user-scroll window on every streamed frame (the "sticky jitter"
    // felt when dragging up against a stream).
    if (gap <= 0) return
    if (isUserScrolling(buildScrollState())) return
    if (shouldPin(buildScrollState(), force)) {
      el2.scrollTop = el2.scrollHeight
      isAtBottom.value = true
    }
  })
  // NOTE: the old unconditional force pin timer (300ms) is gone. Async
  // content growth (Mermaid, KaTeX, lazy original fetch, thinking collapse)
  // is handled by the rAF correction above AND the content-growth observer
  // (onContentGrown) — the observer catches growth that arrives after the
  // correction frame; if the user started scrolling in between, pendingFollow
  // + onScrollStopped take over instead of fighting.
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
 * a user scroll, and so shouldPin treats programmatic jumps correctly.
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
  // The user explicitly asked to return to the bottom — clear the
  // "user left the bottom" latch so follow resumes once the smooth
  // scroll settles. Without this, a user who scrolled up earlier and then
  // tapped the bottom FAB would stay "latched" (userLeftBottom=true) and the
  // next streamed content would be rejected — the list would appear to stop
  // auto-scrolling despite the user being at the bottom.
  userLeftBottom = false
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
  setAtBottom: (val) => {
    isAtBottom.value = val
    // Jumping to a mid-list message means the user deliberately left the
    // bottom — latch userLeftBottom so shouldStayPinned() turns false and
    // stream follow / render-flush re-pin do NOT yank them back.
    if (!val) userLeftBottom = true
  },
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

// Watch session switch to reset scroll state and user msg index.
// Session switches always land at the bottom (switchSession force-scrolls), so
// no position save/restore — only a full state-machine reset for the freshly
// rebuilt list.
watch(() => props.currentSessionId, () => {
  // Session switch always lands at the bottom (switchSession force-scrolls),
  // so no position save/restore here — just reset the scroll state machine for
  // the freshly rebuilt list.
  isAtBottom.value = true
  scrolledUp.value = false
  scrolledDown.value = false
  lastScrollTop = 0
  setProgrammatic(false)
  lastScrollAt = 0
  pendingFollow = false
  userLeftBottom = false
  clearTimeout(scrollStopTimer)
  scrollStopTimer = null
  userTouching = false
  // Clear all user-input scroll markers (touch / wheel / mouse-drag) so the
  // freshly rebuilt list starts from a clean ownership state.
  wheelActive = false
  mouseDownActive = false
  clearTimeout(scrollUpTimer)
  clearTimeout(scrollDownTimer)
  scrollFrameScheduler.cancelAll()
  scrollTick.value = 0
  userMsgIndexDrawer.close()
  codeLinkPreview.close()
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
  if (!el || !newMsgs || newMsgs.length === 0) return

  // Session switches always force-scroll to the bottom; this watcher only
  // anchors the viewport when the message array is replaced (loadHistory /
  // prepend) while the user is NOT at the bottom — keep the first visible
  // message in place instead of letting the browser's scrollTop clamping jump.
  if (!oldMsgs || oldMsgs.length === 0) return
  if (el.scrollHeight - el.scrollTop - el.clientHeight <= NEAR_BOTTOM_PX) return // at bottom → let scrollToBottom pin
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
  // Whether the view should stay pinned to the bottom: the user has NOT
  // deliberately scrolled away (userLeftBottom). Unlike isAtBottom — which is
  // a live-geometry flag that can briefly read false while content is still
  // rendering after a pin — this only reflects user intent, so async render
  // flush / lazy-load growth must keep re-pinning when it is true.
  shouldStayPinned: () => !userLeftBottom,
  scrolledUp,
  scrolledDown,
  closeUserMsgIndex,
  toggleUserMsgIndex,
  closeCodePreview: () => codeLinkPreview.close(),
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

/* Lazy load feedback area — floating pill pinned to the top of the message
   area. Sits above the scroll container (absolute, no layout footprint) so
   it never scrolls with the message flow and never pushes messages down. */
.chat-load-area {
  position: absolute;
  top: 8px;
  left: 0;
  right: 0;
  display: flex;
  justify-content: center;
  z-index: 5;
  pointer-events: none;
}

.chat-load-more,
.chat-load-hint,
.chat-load-done {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 5px 12px;
  font-size: 12px;
  color: var(--text-secondary);
  background: color-mix(in srgb, var(--bg-primary) 82%, transparent);
  border: 1px solid var(--border-color, rgba(128, 128, 128, 0.35));
  border-radius: 999px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  pointer-events: auto;
}

.chat-load-hint {
  cursor: pointer;
  transition: color 0.15s, opacity 0.15s, background 0.15s;
  -webkit-tap-highlight-color: transparent;
}

.chat-load-hint:active {
  opacity: 0.6;
}

@media (hover: hover) {
  .chat-load-hint:hover {
    color: var(--text-primary);
  }
}

/* "All messages loaded" shares the exact same visual style as the
 * "N older messages" hint — only the content differs. */

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

/* ── Message highlight flash ──
   Message jumps (message index / prev / next / Ctrl+↑↓) flash the whole
   bubble's BACKGROUND with the theme accent tint — the text color is left
   untouched (only `background-color` animates, which always paints beneath
   the text, so user-bubble white text and assistant text never change). Each
   bubble role mixes the accent over its own resting theme background
   (user: --user-msg-color / assistant: --bg-tertiary); the animation pulses
   to brighter tints and returns to the resting background (no forwards fill,
   class removal restores the base rule). Timing mirrors the canonical
   `line-flash` (assets/code-viewer.css): 1.2s, two diminishing blinks. */
:deep(.chat-message.user.chat-message-highlight) {
  --msg-base-bg: var(--user-msg-color);
  animation: msg-highlight-flash 1.2s ease-out 1;
}
:deep(.chat-message.assistant.chat-message-highlight) {
  --msg-base-bg: var(--bg-tertiary);
  animation: msg-highlight-flash 1.2s ease-out 1;
}

@keyframes msg-highlight-flash {
  0%, 20%, 40%, 60%, 80%, 100% { background-color: var(--msg-base-bg); }
  10%, 30% { background-color: color-mix(in srgb, var(--accent-color) 65%, var(--msg-base-bg)); }
  50%, 70% { background-color: color-mix(in srgb, var(--accent-color) 45%, var(--msg-base-bg)); }
  90% { background-color: color-mix(in srgb, var(--accent-color) 25%, var(--msg-base-bg)); }
}
</style>
