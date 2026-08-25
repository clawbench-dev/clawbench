/**
 * Thin dispatch wrapper around chatMessageReducer.
 *
 * All mutations of the chat messages array flow through this store's
 * dispatch(action); ChatPanelContent owns the messages ref, and composables
 * (useChatStream / useChatSession / useSessionManager) receive the dispatch
 * function instead of mutating messages.value directly. This is the single
 * write channel that eliminates the multi-writer ordering races.
 */
import type { Ref } from 'vue'
import { chatMessageReducer, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils.ts'

export interface ChatMessageStore {
  /** Apply an action to the messages array (reducer is pure). */
  dispatch: (action: ChatMessageAction) => void
}

/** Create a message store bound to the given messages ref. */
export function createChatMessageStore(messages: Ref<ChatMessage[]>): ChatMessageStore {
  return {
    dispatch(action: ChatMessageAction) {
      messages.value = chatMessageReducer(messages.value, action)
    },
  }
}
