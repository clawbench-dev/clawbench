import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  chatMessageReducer,
  mergeDbMessages,
  type ChatMessage,
  type ChatMessageAction,
} from '@/utils/chatStreamUtils.ts'

/** Apply a sequence of actions, returning the final state. */
function run(state: ChatMessage[], actions: ChatMessageAction[]): ChatMessage[] {
  let s = state
  for (const a of actions) {
    s = chatMessageReducer(s, a)
  }
  return s
}

const u = (partial: Partial<ChatMessage> & { id: unknown }): ChatMessage => ({
  role: 'user',
  content: '',
  blocks: [],
  files: [],
  createdAt: '',
  ...partial,
} as ChatMessage)

const a = (partial: Partial<ChatMessage> & { id: unknown }): ChatMessage => ({
  role: 'assistant',
  content: '',
  blocks: [],
  createdAt: '',
  ...partial,
} as ChatMessage)

describe('chatMessageReducer — optimistic + structural', () => {
  it('optimistic_push then optimistic_remove', () => {
    const state = run([], [
      { type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', pending: true, seq: 1 }) },
    ])
    expect(state).toHaveLength(1)
    expect(state[0].pending).toBe(true)

    const after = run(state, [{ type: 'optimistic_remove', id: 'pending-1' }])
    expect(after).toHaveLength(0)
  })

  it('optimistic_remove_content removes only the matching pending message', () => {
    const state = run(
      [
        u({ id: 'a', content: 'earlier', pending: true, seq: 1 }),
        u({ id: 'b', content: 'hello', pending: true, seq: 2 }),
      ],
      [{ type: 'optimistic_remove_content', content: 'hello' }],
    )
    expect(state).toHaveLength(1)
    expect(state[0].content).toBe('earlier')
  })

  it('clear_pending removes only pending messages', () => {
    const state = run([u({ id: 1, content: 'done' }), u({ id: 'p2', pending: true, seq: 1 })], [
      { type: 'clear_pending' },
    ])
    expect(state.map((m) => m.id)).toEqual([1])
  })

  it('clear empties the array', () => {
    const state = run([u({ id: 1 })], [{ type: 'clear' }])
    expect(state).toHaveLength(0)
  })

  it('prepend_older puts older messages first and re-sorts', () => {
    const state = run([u({ id: 3, content: 'newer' })], [
      { type: 'prepend_older', olderMsgs: [u({ id: 1, content: 'older' }), u({ id: 2, content: 'mid' })] },
    ])
    expect(state.map((m) => m.id)).toEqual([1, 2, 3])
  })
})

describe('chatMessageReducer — WS block-level events', () => {
  it('ws_content accumulates into the streaming message', () => {
    const sm = a({ id: 'drain-1', streaming: true, seq: 1 })
    const state = run([sm], [
      { type: 'ws_content', text: 'hel' },
      { type: 'ws_content', text: 'lo' },
    ])
    expect(sm.blocks![0]).toMatchObject({ type: 'text', text: 'hello' })
  })

  it('ws_content ignores when no streaming message', () => {
    const state = run([u({ id: 1 })], [{ type: 'ws_content', text: 'x' }])
    expect(state).toHaveLength(1)
    expect(state[0].blocks).toEqual([])
  })

  it('ws_thinking accumulates and ws_thinking_done marks done', () => {
    const sm = a({ id: 'drain-1', streaming: true, seq: 1 })
    const state = run([sm], [
      { type: 'ws_thinking', text: 'think', key: 'k1' },
      { type: 'ws_thinking', text: 'ing', key: 'k1' },
      { type: 'ws_thinking_done' },
    ])
    const t = sm.blocks!.find((b) => b.type === 'thinking')!
    expect(t.text).toBe('thinking')
    expect(t.done).toBe(true)
  })

  it('ws_content_reset clears blocks', () => {
    const sm = a({ id: 'drain-1', streaming: true, blocks: [{ type: 'text', text: 'x' }], seq: 1 })
    run([sm], [{ type: 'ws_content_reset' }])
    expect(sm.blocks).toEqual([])
  })

  it('ws_tool_use + ws_tool_result mark tool blocks done', () => {
    const sm = a({ id: 'drain-1', streaming: true, seq: 1 })
    run([sm], [
      { type: 'ws_tool_use', data: { id: 't1', name: 'Read', input: { path: '/a' } } },
      { type: 'ws_tool_result', data: { id: 't1', name: 'Read', status: 'success' } },
    ])
    const tb = sm.blocks!.find((b) => b.type === 'tool_use')!
    expect(tb.done).toBe(true)
    expect(tb.status).toBe('success')
  })
})

describe('chatMessageReducer — ws_queue_cancel', () => {
  it('removes pending bubbles by id and by queueId field', () => {
    const state = run(
      [u({ id: 'p1', pending: true, seq: 1 }), u({ id: 5, queueId: 'p2', pending: true, seq: 2 }), u({ id: 9 })],
      [{ type: 'ws_queue_cancel', queueIds: ['p1', 'p2'] }],
    )
    expect(state.map((m) => m.id)).toEqual([9])
  })
})

// ── Race 1: optimistic_push → db_load → ws_queue_drain ──
describe('chatMessageReducer — Race 1: optimistic bubble survives db_load and drains', () => {
  it('bubble is not wiped by db_load and drain matches by id', () => {
    let state: ChatMessage[] = []
    // 1. User sends message 2 while 1 is generating → optimistic bubble.
    state = run(state, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 1 }) }])
    expect(state).toHaveLength(1)

    // 2. A loadHistory returns msg1 + reply1 + msg2 (DB row, queued) BEFORE drain.
    state = run(state, [{
      type: 'db_load',
      sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1' }),
        u({ id: 3, content: '2', queueId: 'pending-2', queued: true }),
      ],
    }])
    // Bubble must survive (matched by queueId), still pending, id still string.
    expect(state.map((m) => m.id)).toEqual([1, 2, 'pending-2'])
    expect(state[2].pending).toBe(true)

    // 3. queue_drain arrives → matches the surviving bubble by id, no duplicate.
    state = run(state, [{ type: 'ws_queue_drain', queueId: 'pending-2', text: '2', files: [], dbMessageId: 3 }])
    const users = state.filter((m) => m.role === 'user' && m.content === '2')
    expect(users).toHaveLength(1)
  })

  it('drain matches a bubble that already adopted a numeric DB id (queueId field survives)', () => {
    let state: ChatMessage[] = []
    state = run(state, [
      { type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 1 }) },
    ])
    // db_load replaces nothing but marks queued; then the bubble's id becomes numeric
    // (e.g. stream_start or a later merge) while queueId field survives.
    const bubble = state[0]
    bubble.queueId = 'pending-2'
    bubble.id = 3
    bubble.pending = true
    delete bubble.seq

    state = run(state, [{ type: 'ws_queue_drain', queueId: 'pending-2', text: '2', files: [], dbMessageId: 3 }])
    const users = state.filter((m) => m.role === 'user' && m.content === '2')
    expect(users).toHaveLength(1)
  })
})

// ── Race 2: done lost → stream_finalize + db_load(forceNotRunning) ──
describe('chatMessageReducer — Race 2: stream_finalize + db_load do not truncate content', () => {
  it('keeps already-streamed content when the done event was missed', () => {
    let state: ChatMessage[] = []
    // Message 1 + reply1 (streaming, partial content already received).
    state = run(state, [
      { type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) },
    ])
    state = run(state, [
      { type: 'stream_placeholder', msg: a({ id: 'drain-1', streaming: true, seq: 2 }) },
      { type: 'ws_content', text: 'partial reply content' },
    ])
    const sm = state.find((m) => m.streaming)!
    expect(sm.content + (sm.blocks?.[0]?.text || '')).toContain('partial')

    // done event lost → session_update completed arrives → stream_finalize +
    // db_load(forceNotRunning). The reply must NOT be truncated to empty.
    state = run(state, [
      { type: 'stream_finalize' },
      { type: 'db_load', sessionRunning: false, dbMessages: [u({ id: 1, content: '1' })] },
    ])
    const reply = state.find((m) => m.role === 'assistant')
    expect(reply).toBeDefined()
    const text = reply!.blocks?.map((b) => (b.text || '')).join('') || reply!.content
    expect(text).toContain('partial reply content')
  })
})

// ── Race 3: db_load → optimistic_push → stream_placeholder ──
describe('chatMessageReducer — Race 3: new stream coexists with background db_load', () => {
  it('stream placeholder survives a db_load merge that came before it', () => {
    let state: ChatMessage[] = []
    // db_load arrives first (stale snapshot).
    state = run(state, [{
      type: 'db_load',
      sessionRunning: false,
      dbMessages: [u({ id: 1, content: '1' }), a({ id: 2, content: 'reply1' })],
    }])
    // Then the user sends message 2 and a new stream placeholder appears.
    state = run(state, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 1 }) }])
    state = run(state, [{ type: 'stream_placeholder', msg: a({ id: 'drain-2', streaming: true, seq: 2, parentQueueId: 'pending-2' }) }])

    const streaming = state.find((m) => m.streaming)
    expect(streaming).toBeDefined()
    expect(state.map((m) => m.id)).toEqual([1, 2, 'pending-2', 'drain-2'])
  })
})

describe('mergeDbMessages', () => {
  it('adopts DB id into a finalized drain-* reply when session not running', () => {
    const state = [a({ id: 'drain-99', content: 'reply', createdAt: '2026-01-01T00:00:00Z', seq: 1 })]
    const merged = mergeDbMessages(state, [
      a({ id: 7, content: 'reply', createdAt: '2026-01-01T00:00:01Z' }),
    ], false)
    const reply = merged.find((m) => m.role === 'assistant')
    expect(reply?.id).toBe(7)
  })

  it('does NOT adopt drain-* while a stream is live (defers to next idle loadHistory)', () => {
    const state = [
      a({ id: 'drain-99', content: 'reply', createdAt: '2026-01-01T00:00:00Z' }),
      a({ id: 'drain-new', streaming: true, createdAt: '2026-01-01T00:00:02Z', seq: 1 }),
    ]
    const merged = mergeDbMessages(state, [
      a({ id: 7, content: 'reply', createdAt: '2026-01-01T00:00:01Z' }),
    ], true)
    // Adoption is deferred while streaming: drain-99 keeps its transient id.
    expect(merged.some((m) => m.id === 'drain-99')).toBe(true)
    // The DB row appears alongside (transient duplicate, reconciled later) —
    // it is history, never the live reply, so it is never truncated.
    expect(merged.some((m) => m.id === 7)).toBe(true)
  })

  it('keeps streaming placeholder when db snapshot does not contain it', () => {
    const state = [a({ id: 'drain-1', streaming: true, seq: 1 })]
    const merged = mergeDbMessages(state, [u({ id: 1, content: '1' })], true)
    expect(merged.some((m) => m.streaming)).toBe(true)
  })
})
