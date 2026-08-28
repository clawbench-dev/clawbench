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
import { appLog } from '@/utils/appLog'

export interface ChatMessageStore {
  /** Apply an action to the messages array (reducer is pure). */
  dispatch: (action: ChatMessageAction) => void
}

/** Compact order snapshot for diagnostics: role:id[pending/streaming/pq]. */
function orderSnapshot(msgs: ChatMessage[]): string {
  return msgs.map((m) => {
    const flags =
      (m.pending ? 'P' : '') + (m.streaming ? 'S' : '') + (m.parentQueueId ? `~${m.parentQueueId}` : '')
    const id = m.id === undefined || m.id === null ? '?' : String(m.id)
    return `${m.role[0]}:${id}${flags}`
  }).join(' ')
}

/** Create a message store bound to the given messages ref. */
export function createChatMessageStore(messages: Ref<ChatMessage[]>): ChatMessageStore {
  const tag = 'MsgStore'
  return {
    dispatch(action: ChatMessageAction) {
      const before = orderSnapshot(messages.value)
      messages.value = chatMessageReducer(messages.value, action)
      const after = orderSnapshot(messages.value)
      // Log every structural mutation (order + identities) so a repro can be
      // traced end-to-end via the log relay. Block-level actions that return
      // the same array are skipped to avoid per-token noise.
      if (before !== after || action.type.startsWith('ws_') || action.type === 'db_load') {
        appLog.d(tag, `dispatch ${action.type} | ${before} => ${after}`)
      }
    },
  }
}
