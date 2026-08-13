import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useTabDrawer } from '@/composables/useTabDrawer'
import { formatUserMsg } from '@/utils/userMsgIndexUtils.ts'

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
}) {
  const { t } = useI18n()

  const hasUserMessages = computed(() => options.getMessages().some(m => m.role === 'user'))
  const userMsgIndexList = ref<Record<string, unknown>[]>([])
  const drawer = useTabDrawer('chat')
  /** Read-only access — use drawer.open()/close()/toggle() to mutate */
  const showUserMsgIndex = drawer.isOpen
  const loadingTarget = ref(false)
  const loadingIndex = ref(false)

  function formatUserMsgLabel(msg: { content?: string; files?: string[] }) {
    return formatUserMsg(msg, t('chat.messageList.userMsgIndexAttachment'))
  }

  /** Fetch (or reuse a cached) list of user messages for the current session. */
  async function ensureIndexLoaded(): Promise<void> {
    if (userMsgIndexList.value.length > 0) return
    if (!options.getCurrentSessionId()) return
    loadingIndex.value = true
    try {
      const resp = await fetch(`/api/ai/chat/user-messages?session_id=${encodeURIComponent(options.getCurrentSessionId())}`)
      if (resp.ok) {
        const data = await resp.json()
        userMsgIndexList.value = data.messages || []
      }
    } catch {
      userMsgIndexList.value = options.getMessages().filter(m => m.role === 'user')
    } finally {
      loadingIndex.value = false
    }
  }

  async function toggleUserMsgIndex() {
    if (drawer.isOpen.value) {
      drawer.close()
      return
    }
    drawer.open()
    await ensureIndexLoaded()
  }

  function closeUserMsgIndex() {
    drawer.close()
  }

  function highlightMessage(el: Element) {
    el.classList.add('chat-message-highlight')
    setTimeout(() => el.classList.remove('chat-message-highlight'), 1500)
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
    setTimeout(() => { options.setProgrammaticScrolling(false) }, 600)
  }

  async function jumpToUserMessage(msg: { id: number | string }) {
    const targetId = msg.id
    const el = options.getMessagesRef()
    if (!el) return

    const messages = options.getMessages()
    const msgIndex = messages.findIndex(m => m.id === targetId)
    if (msgIndex >= 0) {
      await nextTick()
      const items = el.querySelectorAll('.chat-messages-list > .chat-message')
      if (items[msgIndex]) {
        _scrollAndHighlight(items[msgIndex])
        return
      }
    }

    if (!options.getHasMore()) return
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
            return
          }
          break
        }
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
      setTimeout(() => { options.setProgrammaticScrolling(false) }, 600)
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
    formatUserMsgLabel,
    toggleUserMsgIndex,
    closeUserMsgIndex,
    jumpToUserMessage,
    jumpToAdjacentMessage,
    highlightMessage,
    scrollToMessage,
  }
}
