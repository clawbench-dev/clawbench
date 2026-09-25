import { describe, expect, it } from 'vitest'
import {
  chatMessageReducer,
  rebuildFromDb,
  messageSortValue,
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
      { type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) },
    ])
    expect(state).toHaveLength(1)
    expect(state[0].content).toBe('1')

    const after = run(state, [{ type: 'optimistic_remove', id: 'pending-1' }])
    expect(after).toHaveLength(0)
  })

  it('optimistic_remove_content removes only the matching transient message', () => {
    const state = run(
      [
        u({ id: 'a', content: 'earlier', seq: 1 }),
        u({ id: 'b', content: 'hello', seq: 2 }),
      ],
      [{ type: 'optimistic_remove_content', content: 'hello' }],
    )
    expect(state).toHaveLength(1)
    expect(state[0].content).toBe('earlier')
  })

  it('optimistic_adopt_id adopts DB id and sorts by id (not seq space)', () => {
    // A directly-sent bubble (sendMessageNow) learns its DB id from the POST
    // response / user_message self-echo. Its DB id IS its real conversational
    // position (send order = persist order), so it sorts by id alongside
    // history — NOT in seq space where it would interleave with remote messages
    // by client receive order.
    const state = run(
      [
        u({ id: 'pending-1', content: '1', seq: 1 }),
        u({ id: 'pending-2', content: '2', seq: 2 }),
      ],
      [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 10 }],
    )
    const msg1 = state.find((m) => m.role === 'user' && m.content === '1')
    expect(msg1?.id).toBe(10)
    expect(msg1?.seq).toBeUndefined()
    // Sorts by DB id, not by seq — the not-yet-adopted msg2 stays in seq space.
    expect(messageSortValue(msg1!)).toBe(10)
    expect(messageSortValue(msg1!)).toBeLessThan(messageSortValue(state.find((m) => m.content === '2')!))
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

  it('prepend_older dedups rows already present — a raced full-history page cannot double the conversation', () => {
    // Reported AABBCC duplicate: a loadMore fired while the array was empty
    // sent an invalid before_id, so the backend returned the FULL already-loaded
    // history; prepend_older then prepended a copy of every message (AABBCC).
    // The reducer must drop any incoming row whose id already exists.
    const state = run(
      [
        u({ id: 38954, content: 'A' }),
        a({ id: 38955, content: 'A reply' }),
        u({ id: 38960, content: 'B' }),
        a({ id: 38961, content: 'B reply' }),
        u({ id: 38966, content: 'C' }),
        a({ id: 38967, content: 'C reply' }),
      ],
      [
        {
          type: 'prepend_older',
          olderMsgs: [
            u({ id: 38954, content: 'A' }),
            a({ id: 38955, content: 'A reply' }),
            u({ id: 38960, content: 'B' }),
            a({ id: 38961, content: 'B reply' }),
            u({ id: 38966, content: 'C' }),
            a({ id: 38967, content: 'C reply' }),
          ],
        },
      ],
    )
    expect(state.map((m) => m.id)).toEqual([38954, 38955, 38960, 38961, 38966, 38967])
    expect(state).toHaveLength(6)
  })

  it('prepend_older still prepends genuinely older rows when mixed with duplicates', () => {
    const state = run(
      [u({ id: 38954, content: 'loaded' }), a({ id: 38955, content: 'reply' })],
      [
        {
          type: 'prepend_older',
          olderMsgs: [
            u({ id: 38950, content: 'genuinely older' }),
            a({ id: 38951, content: 'older reply' }),
            u({ id: 38954, content: 'already loaded duplicate' }),
          ],
        },
      ],
    )
    // Only the genuinely older rows (38950, 38951) are prepended; 38954 skipped.
    expect(state.map((m) => m.id)).toEqual([38950, 38951, 38954, 38955])
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

// ── Race 1: optimistic_push → db_load → user_message (drain) ──
describe('chatMessageReducer — Race 1: optimistic bubble converges to its DB row', () => {
  it('bubble is not duplicated by db_load and the drained user_message lands once', () => {
    let state: ChatMessage[] = []
    // 1. User sends message 2 while 1 is generating → optimistic bubble.
    state = run(state, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', seq: 1 }) }])
    expect(state).toHaveLength(1)

    // 2. A loadHistory returns msg1 + reply1 (msg2 is still queued, so it is
    //    NOT in the conversation snapshot — it comes back in the `queue` field).
    state = run(state, [{
      type: 'db_load',
      sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1' }),
      ],
    }])
    // The stale optimistic bubble is dropped: the DB is authoritative and has
    // no row for it (a queued message has no chat_history row yet).
    expect(state.map((m) => m.id)).toEqual([1, 2])

    // 3. queue_drain arrives → the backend materialized msg2 as id=3 and emits
    //    user_message; it lands exactly once.
    state = run(state, [{ type: 'ws_user_message', data: { messageId: 3, content: '2' } }])
    const users = state.filter((m) => m.role === 'user' && m.content === '2')
    expect(users).toHaveLength(1)
    expect(users[0].id).toBe(3)
  })

  it('user_message dedups against an existing bubble with the same DB id', () => {
    let state: ChatMessage[] = []
    state = run(state, [
      { type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', seq: 1 }) },
    ])
    state = run(state, [{ type: 'optimistic_adopt_id', id: 'pending-2', dbId: 3 }])

    // A replayed/duplicate user_message for the same row must not add a second copy.
    state = run(state, [{ type: 'ws_user_message', data: { messageId: 3, content: '2' } }])
    const users = state.filter((m) => m.role === 'user' && m.content === '2')
    expect(users).toHaveLength(1)
  })
})

// ── Race 2: done lost → stream_finalize + db_load ──
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
    // db_load. The rebuild keeps the authoritative DB rows — the finalized
    // reply row carries the streamed content, so nothing is truncated.
    state = run(state, [
      { type: 'stream_finalize' },
      { type: 'db_load', dbMessages: [u({ id: 1, content: '1' }), a({ id: 2, content: 'partial reply content' })] },
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
      dbMessages: [u({ id: 1, content: '1' }), a({ id: 2, content: 'reply1' })],
    }])
    // Then the user sends message 2 and a new stream placeholder appears.
    state = run(state, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', seq: 1 }) }])
    state = run(state, [{ type: 'stream_placeholder', msg: a({ id: 'drain-2', streaming: true, seq: 2 }) }])

    const streaming = state.find((m) => m.streaming)
    expect(streaming).toBeDefined()
    expect(state.map((m) => m.id)).toEqual([1, 2, 'pending-2', 'drain-2'])
  })
})

describe('chatMessageReducer — ws_stream_split', () => {
  it('finalizes the before-half and opens a new streaming placeholder for the after-half', () => {
    let state: ChatMessage[] = []
    state = run(state, [
      // NB: the fixture originally passed a bare ChatMessage here where an
      // action was expected — the reducer's `default` branch made it a no-op, so
      // Q1 never entered the array. Push it for real so the ordering assertion
      // below is about a DB row, not an empty array.
      { type: 'optimistic_push', msg: u({ id: 1, content: 'q' }) },
      { type: 'stream_placeholder', msg: a({ id: 'drain-1', streaming: true, seq: 1 }) },
      { type: 'ws_content', text: 'before' },
    ])
    state = run(state, [{ type: 'ws_stream_split', messageId: 4 }])
    const assistants = state.filter((m) => m.role === 'assistant')
    expect(assistants).toHaveLength(2)

    // The before-half keeps its transient string id (it never adopted a DB id);
    // the after-half carries the new numeric DB id. Identity is what pins each
    // half, not array position: the numeric ids sort by id and the transient
    // string id sorts last, so the after-half precedes the before-half.
    const before = state.find((m) => m.id === 'drain-1')!
    const after = state.find((m) => m.id === 4)!
    expect(before.streaming).toBeFalsy()
    expect(after.streaming).toBe(true)
    expect(state.map((m) => m.id)).toEqual([1, 4, 'drain-1'])
  })
})

describe('rebuildFromDb (db_load)', () => {
  it('rebuild drops a finalized drain-* reply that has no DB row; DB row is the truth', () => {
    const state = [a({ id: 'drain-99', content: 'reply', createdAt: '2026-01-01T00:00:00Z', seq: 1 })]
    const merged = rebuildFromDb(state, [
      a({ id: 7, content: 'reply', createdAt: '2026-01-01T00:00:01Z' }),
    ])
    const reply = merged.find((m) => m.role === 'assistant')
    expect(reply?.id).toBe(7)
    expect(merged.some((m) => m.id === 'drain-99')).toBe(false)
  })

  // ── Queued messages have no chat_history row, so a db_load snapshot that
  //    does not contain the optimistic bubble is authoritative: the bubble is
  //    dropped (it lives in the queue store, which is synced separately).
  it('drops an optimistic bubble with no DB row (still queued, not in chat_history)', () => {
    const state = [
      u({ id: 1, content: 'msg1' }),
      a({ id: 2, content: 'reply1' }),
      u({ id: 'pending-2', content: 'msg2', seq: 1 }),
    ]
    const merged = rebuildFromDb(state, [
      u({ id: 1, content: 'msg1' }),
      a({ id: 2, content: 'reply1' }),
    ])
    const msg2 = merged.find((m) => m.role === 'user' && m.content === 'msg2')
    expect(msg2).toBeUndefined()
    expect(merged.map((m) => m.id)).toEqual([1, 2])
  })

  // ── Realistic streaming sequence: msg2 queued while reply1 streams, then a
  //    db_load arrives BEFORE drain. msg2 is not in the snapshot; the live
  //    streaming placeholder must be preserved when the session is running.
  it('full queue flow: live placeholder survives db_load, drained row lands once', () => {
    let state: ChatMessage[] = []
    // User sends msg1 → optimistic user row
    state = run(state, [{ type: 'optimistic_push', msg: u({ id: 1, content: 'msg1', seq: 1 }) }])
    // stream_start → placeholder for reply1 (DB id 2) with content
    state = run(state, [
      { type: 'stream_placeholder', msg: a({ id: 2, streaming: true, seq: 2 }) },
      { type: 'ws_content', text: 'reply1' },
    ])
    // While reply1 streams, user sends msg2 → it goes to the queue store only
    // (not represented in the messages array).

    // db_load arrives: msg1 row present, reply1 streaming row present. The live
    // placeholder keeps its object identity.
    state = run(state, [{
      type: 'db_load',
      sessionRunning: true,
      dbMessages: [
        u({ id: 1, content: 'msg1' }),
        a({ id: 2, streaming: true, content: '', blocks: [{ type: 'text', text: 'reply1' }] }),
      ],
    }])
    const streaming = state.filter((m) => m.role === 'assistant' && m.streaming)
    expect(streaming).toHaveLength(1)
    expect(streaming[0].id).toBe(2)

    // queue_drain(msg2): msg2 materialized as id=3 → user_message + new turn.
    state = run(state, [{ type: 'stream_finalize' }])
    state = run(state, [{ type: 'ws_user_message', data: { messageId: 3, content: 'msg2' } }])
    state = run(state, [{ type: 'stream_placeholder', msg: a({ id: 4, streaming: true, seq: 3 }) }])
    const msg2 = state.find((m) => m.role === 'user' && m.content === 'msg2')
    expect(msg2?.id).toBe(3)
    // Conversational order preserved: msg1 < reply1 < msg2 < reply2(streaming).
    const order = state.map((m) => (m.role === 'user' ? `u:${m.content}` : `a:${m.id}`))
    expect(order).toEqual(['u:msg1', 'a:2', 'u:msg2', 'a:4'])
  })
})

describe('ws_stream_start', () => {
  it('assigns the DB id to the streaming placeholder', () => {
    const state = run([
      a({ id: 'drain-1', content: '', blocks: [], streaming: true, seq: 2 }),
    ], [
      { type: 'ws_stream_start', messageId: 202 },
    ])
    const sm = state.find((m) => m.role === 'assistant' && m.streaming)!
    expect(sm.id).toBe(202)
  })

  it('does not rename a placeholder that already carries a numeric id', () => {
    // A mid-turn split opens a second streaming row; a stale/duplicate
    // stream_start for the first row must not collapse the two back into one.
    const state = run([
      a({ id: 202, content: '', blocks: [], streaming: true, seq: 2 }),
    ], [
      { type: 'ws_stream_start', messageId: 101 },
    ])
    const sm = state.find((m) => m.role === 'assistant' && m.streaming)!
    expect(sm.id).toBe(202)
  })

  it('is a no-op when no streaming placeholder exists', () => {
    const state = run([u({ id: 1 })], [{ type: 'ws_stream_start', messageId: 202 }])
    expect(state.map((m) => m.id)).toEqual([1])
  })
})

describe('rebuildFromDb (live placeholder)', () => {

  it('keeps the LIVE placeholder when a DB streaming row matches it (by id)', () => {
    // ws_stream_start assigned the DB id to the live placeholder; the rebuild
    // must keep the placeholder object (content preserved) and not append a
    // duplicate DB row.
    const state = [a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:02Z', seq: 1 })]
    const merged = rebuildFromDb(state, [
      a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:02Z' }),
    ])
    expect(merged).toHaveLength(1)
    expect(merged[0].id).toBe(7)
    expect(merged[0].streaming).toBe(true)
  })

  it('preserves the live placeholder object identity (keeps streamed content)', () => {
    const live = a({ id: 7, streaming: true, blocks: [{ type: 'text', text: 'partial' }], seq: 1 })
    const merged = rebuildFromDb([live], [
      a({ id: 7, streaming: true, blocks: [] }),
    ])
    expect(merged).toHaveLength(1)
    // Same object identity → the already-rendered content keeps its DOM.
    expect(merged[0]).toBe(live)
    expect(merged[0].blocks![0].text).toBe('partial')
  })

  // ── Bug regression (issue #419): session switch resets the streaming elapsed
  //    timer. Switching back to a running session re-subscribes → the backend
  //    emits stream_start, which recreates the placeholder stamped with the
  //    SWITCH time (createdAt = now) while the REST db_load is still in flight.
  //    rebuildFromDb keeps that placeholder object — its createdAt (the switch
  //    moment) must be replaced by the DB streaming row's older created_at (the
  //    true stream start), otherwise the "⋯ 45s" counter restarts from 0 on
  //    every switch.
  it('adopts the DB row created_at as the elapsed anchor when the live placeholder is newer (session-switch reset)', () => {
    const state = [a({
      id: 7,
      streaming: true,
      // stream_start fired by the re-subscribe raced ahead of db_load → the
      // placeholder was stamped with the switch moment, well AFTER stream start.
      createdAt: '2026-01-01T00:01:00Z',
      seq: 1,
    })]
    const merged = rebuildFromDb(state, [
      a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:00Z' }),
    ])
    expect(merged).toHaveLength(1)
    expect(merged[0].id).toBe(7)
    expect(merged[0].streaming).toBe(true)
    // The elapsed timer anchor must be the stream start, not the switch moment.
    expect(merged[0].createdAt).toBe('2026-01-01T00:00:00Z')
  })

  it('keeps the live placeholder createdAt when it is not newer than the DB row', () => {
    const state = [a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:00Z', seq: 1 })]
    const merged = rebuildFromDb(state, [
      // DB row flushed slightly later (content flush lag) — placeholder time is
      // already the true start, so it must NOT be bumped forward.
      a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:01Z' }),
    ])
    expect(merged[0].createdAt).toBe('2026-01-01T00:00:00Z')
  })

  it('adopts the DB created_at when the live placeholder has no usable createdAt', () => {
    const state = [a({ id: 7, streaming: true, createdAt: '', seq: 1 })]
    const merged = rebuildFromDb(state, [
      a({ id: 7, streaming: true, createdAt: '2026-01-01T00:00:00Z' }),
    ])
    expect(merged[0].createdAt).toBe('2026-01-01T00:00:00Z')
  })

  it('drops the streaming placeholder when the DB snapshot has no streaming row for it (done was missed)', () => {
    const state = [a({ id: 'drain-1', streaming: true, seq: 1 })]
    const merged = rebuildFromDb(state, [u({ id: 1, content: '1' })])
    // No streaming DB row → placeholder dropped; the DB row is authoritative.
    expect(merged.some((m) => m.streaming)).toBe(false)
    expect(merged).toHaveLength(1)
  })

  it('ws_error replaces live streaming blocks with the error block', () => {
    const state = run(
      [a({ id: 'drain-1', content: 'partial', blocks: [{ type: 'text', text: 'partial' }], streaming: true, seq: 1 })],
      [{ type: 'ws_error', text: 'backend crashed', reason: 'backend_exit' }],
    )
    const sm = state.find((m) => m.role === 'assistant')
    expect(sm?.blocks).toEqual([{ type: 'error', text: 'backend crashed', reason: 'backend_exit' }])
    expect(sm?.streaming).toBe(true) // flag cleared later by forceCleanup
  })

  it('ws_error with no streaming assistant appends to the last assistant (no reload needed)', () => {
    // Regression: after the stream ended (done), a backend crash emits an error
    // event but there is no live streaming placeholder — the error must still
    // surface immediately on the last assistant instead of only after reload.
    const state = run(
      [
        u({ id: 1, content: 'q1' }),
        a({ id: 2, content: 'reply1', blocks: [{ type: 'text', text: 'reply1' }] }),
      ],
      [{ type: 'ws_error', text: 'peer disconnected', reason: 'backend_exit' }],
    )
    const lastAssistant = state[state.length - 1]
    expect(lastAssistant.role).toBe('assistant')
    expect(lastAssistant.blocks).toContainEqual({ type: 'error', text: 'peer disconnected', reason: 'backend_exit' })
  })

  it('ws_error creates an assistant message when none exists', () => {
    const state = run(
      [u({ id: 1, content: 'q1' })],
      [{ type: 'ws_error', text: 'boom', reason: 'backend_exit' }],
    )
    expect(state.some((m) => m.role === 'assistant' && (m.blocks ?? []).some((b) => b.type === 'error'))).toBe(true)
  })
})
