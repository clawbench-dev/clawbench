import { describe, expect, it, beforeEach } from 'vitest'
import { ref, type Ref } from 'vue'
import { createChatMessageStore } from '@/composables/useChatMessageStore'
import { chatMessageReducer, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils.ts'

/** Build a ChatMessage with the minimal required fields. */
function msg(partial: Partial<ChatMessage> & { id: unknown }): ChatMessage {
  return {
    role: 'assistant',
    content: '',
    blocks: [],
    createdAt: '',
    ...partial,
  } as ChatMessage
}

describe('createChatMessageStore — thin reducer wrapper', () => {
  let messages: Ref<ChatMessage[]>

  beforeEach(() => {
    messages = ref([])
  })

  it('dispatch applies the reducer and mutates the messages ref', () => {
    const store = createChatMessageStore(messages)
    messages.value = [msg({ id: 1, role: 'user', content: 'hi' } as Partial<ChatMessage> & { id: unknown })]

    store.dispatch({ type: 'optimistic_push', msg: msg({ id: 'p1', role: 'user', content: 'q', pending: true, seq: 1 }) })

    expect(messages.value).toHaveLength(2)
    expect(messages.value[1]).toMatchObject({ id: 'p1', pending: true })
  })

  it('dispatch applies block-level streaming actions (content / thinking)', () => {
    const store = createChatMessageStore(messages)
    const sm = msg({ id: 'drain-1', streaming: true, seq: 1 })
    messages.value = [sm]

    store.dispatch({ type: 'ws_content', text: 'hel' })
    store.dispatch({ type: 'ws_content', text: 'lo' })
    store.dispatch({ type: 'ws_thinking', text: 'thinking', key: 'k1' })

    expect(sm.blocks![0]).toMatchObject({ type: 'text', text: 'hello' })
    expect(sm.blocks!.some((b) => b.type === 'thinking' && b.text === 'thinking')).toBe(true)
  })

  it('dispatch behaves identically to calling the reducer directly (no behavior divergence)', () => {
    const store = createChatMessageStore(messages)
    const direct = chatMessageReducer([], { type: 'clear' })
    store.dispatch({ type: 'clear' })
    expect(messages.value).toEqual(direct)
  })

  // An empty content delta must not open a text block. Such a block is persisted
  // without a `text` key (Go's `omitempty`), so a reload hands the renderer
  // undefined and blanks the whole message (message 65835). Mirrors the
  // AccumulateBlock guard on the backend and ws_thinking's guard below it.
  it('ws_content with empty text does not open a block', () => {
    const store = createChatMessageStore(messages)
    const sm = msg({ id: 'empty-1', streaming: true, seq: 1 })
    messages.value = [sm]

    store.dispatch({ type: 'ws_content', text: '' })
    expect(sm.blocks).toHaveLength(0)

    // A real delta afterwards still opens the block normally.
    store.dispatch({ type: 'ws_content', text: 'real' })
    expect(sm.blocks).toHaveLength(1)
    expect(sm.blocks![0]).toMatchObject({ type: 'text', text: 'real' })

    // And an empty delta does not disturb it.
    store.dispatch({ type: 'ws_content', text: '' })
    expect(sm.blocks).toHaveLength(1)
    expect(sm.blocks![0].text).toBe('real')
  })
})
