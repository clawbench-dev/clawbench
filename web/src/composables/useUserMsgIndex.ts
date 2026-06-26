import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { truncateUserMsg } from '@/utils/userMsgIndexUtils.ts'

/**
 * Composable for user message index overlay logic.
 * Extracted from ChatMessageList.vue for testability.
 */
export function useUserMsgIndex(options: {
  getMessages: () => any[]
  getCurrentSessionId: () => string
  getHasMore: () => boolean
  getLoadingMore: () => boolean
  emitLoadMore: () => void
  getMessagesRef: () => HTMLElement | null
  hideScrollFab: () => void
  setProgrammaticScrolling: (val: boolean) => void
}) {
  const { t } = useI18n()

  const hasUserMessages = computed(() => options.getMessages().some(m => m.role === 'user'))
  const userMsgIndexList = ref<any[]>([])
  const showUserMsgIndex = ref(false)
  const loadingTarget = ref(false)
  const loadingIndex = ref(false)

  function formatTruncateUserMsg(msg: { content?: string; files?: string[] }) {
    return truncateUserMsg(msg, t('chat.messageList.userMsgIndexAttachment'))
  }

  async function toggleUserMsgIndex() {
    if (showUserMsgIndex.value) {
      showUserMsgIndex.value = false
      return
    }
    showUserMsgIndex.value = true
    if (!options.getCurrentSessionId()) return
    loadingIndex.value = true
    try {
      const resp = await fetch(`/api/ai/chat/user-messages?session_id=${encodeURIComponent(options.getCurrentSessionId())}`)
      if (!resp.ok) return
      const data = await resp.json()
      userMsgIndexList.value = data.messages || []
    } catch {
      userMsgIndexList.value = options.getMessages().filter(m => m.role === 'user')
    } finally {
      loadingIndex.value = false
    }
  }

  function closeUserMsgIndex() {
    showUserMsgIndex.value = false
  }

  function highlightMessage(el: Element) {
    el.classList.add('chat-message-highlight')
    setTimeout(() => el.classList.remove('chat-message-highlight'), 1500)
  }

  function _scrollAndHighlight(item: Element) {
    closeUserMsgIndex()
    options.hideScrollFab()
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
      options.setProgrammaticScrolling(true)
      items[msgIndex].scrollIntoView({ behavior: 'smooth', block: 'center' })
      highlightMessage(items[msgIndex])
      setTimeout(() => { options.setProgrammaticScrolling(false) }, 600)
    }
  }

  return {
    hasUserMessages,
    userMsgIndexList,
    showUserMsgIndex,
    loadingTarget,
    loadingIndex,
    formatTruncateUserMsg,
    toggleUserMsgIndex,
    closeUserMsgIndex,
    jumpToUserMessage,
    highlightMessage,
    scrollToMessage,
  }
}
