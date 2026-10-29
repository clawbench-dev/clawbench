import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { flashElement } from '@/utils/domFlash'
import { formatIndexMsg } from '@/utils/userMsgIndexUtils.ts'
import { anchorCount } from '@/utils/btwAnchors.ts'

/**
 * Composable for user message index overlay logic.
 * Extracted from ChatMessageList.vue for testability.
 */
export function useUserMsgIndex(options: {
  getMessages: () => Record<string, unknown>[]
  getCurrentSessionId: () => string
  getHasMore: () => boolean
  getLoadingMore: () => boolean
  emitLoadMore: () => void
  getMessagesRef: () => HTMLElement | null
  hideScrollFab: () => void
  setProgrammaticScrolling: (val: boolean) => void
  /** Mark whether the viewport is pinned to the bottom of the message list.
   *  Jumping to an older message must set this false so streaming auto-follow
   *  doesn't snap the view back down. */
  setAtBottom: (val: boolean) => void
  /** How to highlight the jumped-to message. Defaults to an immediate flash;
   *  the chat list injects a queue that defers the flash until its smooth
   *  scroll settles, so a long-distance jump still shows the full animation. */
  highlightMessage?: (el: Element) => void
  /**
   * /btw anchors, keyed by anchor message id (the same map the chat list draws
   * its markers from). The conversation index marks those rows with an icon.
   * Read as a live getter so marks track records loaded after the list is
   * built; absent (e.g. the share TOC) simply renders no marks.
   */
  getBtwAnchors?: () => Record<string, unknown[]>
}) {
  const { t } = useI18n()

  const doHighlight = options.highlightMessage
    ?? ((el: Element) => flashElement(el, { className: 'chat-message-highlight' }))

  const hasUserMessages = computed(() => options.getMessages().some(m => m.role === 'user'))
  const userMsgIndexList = ref<Record<string, unknown>[]>([])
  const drawer = useTabDrawer('chat')
  /** Read-only access — use drawer.open()/close()/toggle() to mutate */
  const showUserMsgIndex = drawer.isOpen
  const loadingTarget = ref(false)
  const loadingIndex = ref(false)

  function formatIndexLabel(msg: { role?: string; summary?: string; content?: string; files?: string[] }) {
    return formatIndexMsg(msg, {
      attachment: t('chat.messageList.userMsgIndexAttachment'),
      noText: t('chat.messageList.conversationIndexNoText'),
    })
  }

  /** Fetch (or reuse a cached) list of messages for the current session. */
  async function ensureIndexLoaded(): Promise<void> {
    if (userMsgIndexList.value.length > 0) return
    await fetchIndex()
  }

  /** Fetch the index unconditionally, replacing whatever is cached. */
  async function fetchIndex(): Promise<void> {
    if (!options.getCurrentSessionId()) return
    loadingIndex.value = true
    try {
      const resp = await fetch(`/api/ai/chat/user-messages?session_id=${encodeURIComponent(options.getCurrentSessionId())}`)
      if (resp.ok) {
        const data = await resp.json()
        userMsgIndexList.value = data.messages || []
      }
    } catch {
      // Offline fallback: the in-memory list only holds the pages loaded so far,
      // but it already carries both roles, so the drawer stays usable.
      userMsgIndexList.value = options.getMessages().filter(m => m.role === 'user' || m.role === 'assistant')
    } finally {
      loadingIndex.value = false
    }
  }

  /**
   * Reload the index when the conversation has grown past it.
   *
   * The index is a point-in-time snapshot from the API, which was fine while
   * its only consumer was a drawer that fetched on open. The always-visible
   * rail would otherwise keep showing the list as it looked when the session
   * was opened — new turns would never appear on it. `lastMessageId` is the
   * newest message the chat list currently holds; a mismatch means at least one
   * message has landed since the fetch.
   *
   * Skipped while a turn is still streaming: the newest row would be an
   * unfinished reply, and the index excludes in-flight rows by construction.
   */
  async function refreshIndexIfStale(lastMessageId: number | string | null | undefined): Promise<void> {
    if (lastMessageId == null || lastMessageId === '') return
    const list = userMsgIndexList.value
    if (list.length === 0) {
      await ensureIndexLoaded()
      return
    }
    const lastIndexed = list[list.length - 1]?.id
    if (String(lastIndexed) === String(lastMessageId)) return
    // Refresh IN PLACE. Clearing first would empty the array for one render,
    // and the always-visible rail unmounts on an empty list — losing its scroll
    // position, so the newly added tail row lands off-screen and the reader never
    // sees it. The fetch is atomic: the old list stays until the new one arrives.
    await fetchIndex()
  }

  async function toggleUserMsgIndex() {
    if (drawer.isOpen.value) {
      drawer.close()
      return
    }
    drawer.open()
    await ensureIndexLoaded()
  }

  /**
   * Whether a message has a /btw side question anchored to it, so the index can
   * mark its row.
   *
   * Exposed as a lookup rather than an extra field on `userMsgIndexList`: the
   * list is a straight copy of the API payload, and other consumers depend on
   * that shape. Reading the anchors here keeps the rule shared with the chat
   * area (both go through utils/btwAnchors) and stays reactive, since callers
   * pass a Vue-backed map.
   */
  function hasBtw(msg: { id?: unknown } | null | undefined): boolean {
    return anchorCount(options.getBtwAnchors?.() || {}, msg) > 0
  }

  function closeUserMsgIndex() {
    drawer.close()
  }

  function highlightMessage(el: Element) {
    doHighlight(el)
  }

  function _scrollAndHighlight(item: Element) {
    closeUserMsgIndex()
    options.hideScrollFab()
    // The user is navigating away from the bottom — stop streaming auto-follow
    // so the view stays at the jumped-to message instead of being pulled back.
    options.setAtBottom(false)
    options.setProgrammaticScrolling(true)
    item.scrollIntoView({ behavior: 'smooth', block: 'center' })
    highlightMessage(item)
    // Programmatic ownership is released by ChatMessageList's scroll-stop
    // detection (onScrollStopped) when the smooth scroll settles — no fixed
    // timeout here, which would cut a long scrollIntoView short and let its
    // scroll events be misread as user scrolls.
  }

  /**
   * Scroll to a message and flash it, paging older history in as needed.
   *
   * Returns true when the target was found and scrolled to, false when it could
   * not be reached (not in the loaded window and history is exhausted, or the
   * paging loop gave up). Callers that navigate from outside the list — e.g. a
   * session-search hit whose message id may predate the loaded window — need the
   * failure signal to fall back to a sensible view instead of leaving the reader
   * wherever they happened to be.
   */
  async function jumpToUserMessage(msg: { id: number | string }): Promise<boolean> {
    const targetId = msg.id
    const el = options.getMessagesRef()
    if (!el) return false

    const messages = options.getMessages()
    const msgIndex = messages.findIndex(m => m.id === targetId)
    if (msgIndex >= 0) {
      await nextTick()
      const items = el.querySelectorAll('.chat-messages-list > .chat-message')
      if (items[msgIndex]) {
        _scrollAndHighlight(items[msgIndex])
        return true
      }
    }

    if (!options.getHasMore()) return false
    loadingTarget.value = true
    try {
      const maxRounds = 50
      for (let round = 0; round < maxRounds; round++) {
        const idx = options.getMessages().findIndex(m => m.id === targetId)
        if (idx >= 0) {
          await nextTick()
          await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
          const items = el.querySelectorAll('.chat-messages-list > .chat-message')
          if (items[idx]) {
            _scrollAndHighlight(items[idx])
            return true
          }
          break
        }
        // History is exhausted (or the last load returned nothing) — the target
        // simply is not in this session. Without this, every remaining round
        // would burn its 500ms timeout: a stale search hit (message deleted, or
        // the index is behind) would spin for ~25s with loadingTarget held.
        if (!options.getHasMore()) return false
        options.emitLoadMore()
        await new Promise<void>(resolve => {
          let timer: ReturnType<typeof setTimeout> | null = null
          const unwatch = watch(() => options.getLoadingMore(), (val) => {
            if (val) { clearTimeout(timer!); unwatch(); resolve() }
          })
          timer = setTimeout(() => { unwatch(); resolve() }, 500)
        })
        if (options.getLoadingMore()) {
          await new Promise<void>(resolve => {
            const unwatch = watch(() => options.getLoadingMore(), (val) => {
              if (!val) { unwatch(); resolve() }
            })
            setTimeout(() => { unwatch(); resolve() }, 5000)
          })
        }
        await nextTick()
        await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      }
      return false
    } finally {
      loadingTarget.value = false
    }
  }

  function scrollToMessage(msgId: number | string) {
    const el = options.getMessagesRef()
    if (!el) return
    const msgIndex = options.getMessages().findIndex(m => m.id === msgId)
    if (msgIndex < 0) return
    const items = el.querySelectorAll('.chat-messages-list > .chat-message')
    if (items[msgIndex]) {
      options.setAtBottom(false)
      options.setProgrammaticScrolling(true)
      items[msgIndex].scrollIntoView({ behavior: 'smooth', block: 'center' })
      highlightMessage(items[msgIndex])
      // Ownership released via scroll-stop detection (onScrollStopped).
    }
  }

  /**
   * Jump to the previous/next message (any role — user or assistant) in the
   * loaded conversation, wrapping around the list. Uses the same
   * `jumpToUserMessage` path so the target is scrolled & highlighted identically
   * to a conversation-index selection. Returns false when there are no messages.
   */
  async function jumpToAdjacentMessage(direction: 'prev' | 'next', currentActiveId: number | string | null): Promise<boolean> {
    const messages = options.getMessages()
    if (messages.length === 0) return false
    let idx = messages.findIndex(m => m.id === currentActiveId)
    if (idx < 0) idx = 0
    const next = direction === 'next'
      ? (idx + 1) % messages.length
      : (idx - 1 + messages.length) % messages.length
    await jumpToUserMessage(messages[next] as { id: number | string })
    return true
  }

  return {
    hasUserMessages,
    userMsgIndexList,
    showUserMsgIndex,
    drawer,
    loadingTarget,
    loadingIndex,
    formatIndexLabel,
    hasBtw,
    /**
     * Load the index without opening the drawer.
     *
     * The chat-area navigation rail renders the SAME index permanently, so it
     * needs the rows before (and regardless of whether) the drawer is ever
     * opened. Idempotent: `ensureIndexLoaded` returns immediately once the list
     * is populated, so calling it on every session change costs nothing.
     */
    ensureIndexLoaded,
    refreshIndexIfStale,
    toggleUserMsgIndex,
    closeUserMsgIndex,
    jumpToUserMessage,
    jumpToAdjacentMessage,
    highlightMessage,
    scrollToMessage,
  }
}
