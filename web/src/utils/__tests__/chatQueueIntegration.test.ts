import { describe, expect, it } from 'vitest'
import { chatMessageReducer, sortMessages, messageSortValue, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils.ts'

/**
 * Integration test for the REAL full queued-message flow.
 *
 * Simulates the exact event sequence the frontend produces when a user sends
 * three messages in quick succession (msg1 starts AI; msg2/msg3 queue) and the
 * backend drains them one by one, interleaved with background loadHistory
 * merges (done → db_load). Asserts the conversational order matches what a
 * page reload (authoritative DB order) would show — at every stage, not just
 * at the end.
 *
 * Queued messages are NOT in the conversation array any more: msg2/msg3 live
 * in the queue store until the backend materializes them into chat_history on
 * drain. So the message list only ever contains DB-backed rows and the live
 * streaming placeholder (whose id is the DB reply row id, assigned by
 * stream_start).
 */

function run(state: ChatMessage[], actions: ChatMessageAction[]): ChatMessage[] {
  let s = state
  for (const a of actions) s = chatMessageReducer(s, a)
  return s
}
const u = (p: Partial<ChatMessage> & { id: unknown }): ChatMessage =>
  ({ role: 'user', content: '', blocks: [], files: [], createdAt: '', ...p }) as ChatMessage
const a = (p: Partial<ChatMessage> & { id: unknown }): ChatMessage =>
  ({ role: 'assistant', content: '', blocks: [], createdAt: '', ...p }) as ChatMessage

const display = (s: ChatMessage[]) =>
  s.map((m) => `${m.role}:${String(m.id)}${m.streaming ? '(s)' : ''}`).join(' | ')

describe('chat queue full-flow integration', () => {
  it('msg1 direct, msg2/msg3 queued: order matches reload at every stage', () => {
    let s: ChatMessage[] = []

    // 1. sendMessageNow('1') → optimistic bubble, POST returns msgId, stream_start
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 1 }])
    // stream_start for reply1: no streaming msg yet → placeholder created with
    // the DB reply row id, then ws_stream_start (idempotent).
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 2, streaming: true, seq: 2, createdAt: '2026-01-01T00:00:00Z' }) }])
    s = run(s, [{ type: 'ws_stream_start', messageId: 2 }])
    s = run(s, [{ type: 'ws_content', text: 'reply1' }])

    // 2. msg2/msg3 queue while reply1 streams. They go to the queue store, NOT
    //    the message list — so the list is unchanged.
    expect(display(s)).toBe('user:1 | assistant:2(s)')

    // 3. done(reply1) → background loadHistory: DB rows arrive. Reply1
    //    placeholder keeps its object (matched by id=2). The queued rows are
    //    NOT in the conversation list (they are returned in the `queue` field).
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 1, content: '1', createdAt: '2026-01-01T00:00:05Z' }),
        a({ id: 2, content: 'reply1', createdAt: '2026-01-01T00:00:01Z' }),
      ],
    }])
    expect(display(s)).toBe('user:1 | assistant:2')

    // 4. drain('2'): the backend materializes msg2 into chat_history and starts
    //    its own turn. user_message adds the row; stream_start opens the
    //    placeholder with reply2's DB id.
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 3, content: '2' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 4, streaming: true, seq: 3, createdAt: '2026-01-01T00:00:08Z' }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    expect(display(s)).toBe('user:1 | assistant:2 | user:3 | assistant:4(s)')

    // 5. drain('3') → msg3 materialized (id=5), reply3 placeholder id=6.
    //    The turn boundary finalizes reply2 first (finalizeStreamingForDrain).
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 5, content: '3' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 6, streaming: true, seq: 4, createdAt: '2026-01-01T00:00:09Z' }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    expect(display(s)).toBe('user:1 | assistant:2 | user:3 | assistant:4 | user:5 | assistant:6(s)')

    // 6. Final db_load after all done — everything converges to DB order.
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1', createdAt: '2026-01-01T00:00:01Z' }),
        u({ id: 3, content: '2', createdAt: '2026-01-01T00:00:06Z' }),
        a({ id: 4, content: 'reply2', createdAt: '2026-01-01T00:01:08Z' }),
        u({ id: 5, content: '3', createdAt: '2026-01-01T00:00:07Z' }),
        a({ id: 6, content: 'reply3', createdAt: '2026-01-01T00:01:09Z' }),
      ],
    }])
    // Exactly the reload order: msg1, reply1, msg2, reply2, msg3, reply3.
    const finalIds = s.map((m) => (m.role === 'assistant' ? `a${String(m.id)}` : `u${String(m.id)}`))
    expect(finalIds).toEqual(['u1', 'a2', 'u3', 'a4', 'u5', 'a6'])
  })

  it('msg1 direct only: bubble adopts id on db_load, reply adopts on final reload', () => {
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: 'hello', seq: 1 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 1 }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 2, streaming: true, seq: 2, createdAt: '2026-01-01T00:00:00Z' }) }])
    s = run(s, [{ type: 'ws_content', text: 'world' }])
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 1, content: 'hello' }),
        a({ id: 2, content: 'world', createdAt: '2026-01-01T00:00:01Z' }),
      ],
    }])
    expect(display(s)).toBe('user:1 | assistant:2')
  })

  // ── User-reported bug: "reply1 done, msg2/msg3 and their replies appear
  //    ABOVE msg1/reply1 until everything finishes; reload fixes it."
  //    With queued messages out of the array, the remaining risk is the live
  //    streaming placeholder staying transient (huge sort value) while the
  //    drained user rows adopt DB ids. The id-based placeholder match on
  //    db_load must pull it back into DB order.
  it('db_load with persist lag keeps every message in conversational order', () => {
    const t0 = '2026-01-01T00:00:00Z'
    const tLate = '2026-01-01T00:01:00Z' // backend persisted 60s after the bubble
    let s: ChatMessage[] = []

    // msg1 direct send (id=pending-1), reply1 placeholder
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1, createdAt: t0 }) }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, createdAt: t0 }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply1' }])

    // reply1 done, background loadHistory arrives LATE (createdAt 60s later)
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 1, content: '1', createdAt: tLate }),
        a({ id: 2, content: 'reply1', createdAt: tLate }),
      ],
    }])
    // msg1 adopted (content match, persist lag immune), reply1 adopted.
    expect(display(s)).toBe('user:1 | assistant:2')

    // drain msg2 → materialized as id=3, reply2 placeholder id=4
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 3, content: '2' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 4, streaming: true, seq: 3, createdAt: tLate }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    expect(display(s)).toBe('user:1 | assistant:2 | user:3 | assistant:4(s)')

    // drain msg3
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 5, content: '3' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 6, streaming: true, seq: 4, createdAt: tLate }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    expect(display(s)).toBe('user:1 | assistant:2 | user:3 | assistant:4 | user:5 | assistant:6(s)')

    // final db_load
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1', createdAt: tLate }),
        u({ id: 3, content: '2', createdAt: tLate }),
        a({ id: 4, content: 'reply2', createdAt: tLate }),
        u({ id: 5, content: '3', createdAt: tLate }),
        a({ id: 6, content: 'reply3', createdAt: tLate }),
      ],
    }])
    const finalIds = s.map((m) => (m.role === 'assistant' ? `a${String(m.id)}` : `u${String(m.id)}`))
    expect(finalIds).toEqual(['u1', 'a2', 'u3', 'a4', 'u5', 'a6'])
  })

  // ── Regression: msg1 bubble not yet adopted, later messages already have DB
  //    ids. All transient messages sort in seq space; DB-backed rows sort by id
  //    and therefore always come first.
  it('unadopted msg1 bubble sorts after DB-backed rows (transient seq space)', () => {
    const state: ChatMessage[] = [
      u({ id: 38308, content: '2' }),
      a({ id: 38309, content: 'reply2' }),
      u({ id: 'pending-abc', content: '1', seq: 1 }),
      a({ id: 'drain-r1', content: 'reply1', seq: 2 }),
    ]
    sortMessages(state)
    const order = state.map((m) => (m.role === 'user' ? `u:${String(m.id)}` : `a:${String(m.id)}`))
    // DB rows (numeric ids) first, then transients by seq.
    expect(order).toEqual(['u:38308', 'a:38309', 'u:pending-abc', 'a:drain-r1'])
  })

  // ── Realistic session-with-history scenario (user-reported):
  //    history loaded (no seq), then msg1 sent (adopted via user_message
  //    self-echo), msg2/msg3 queued then drained. Order must be:
  //    history, msg1, reply1, msg2, reply2, msg3, reply3 — at every stage.
  it('with history: msg1 adopted, msg2/3 drained — conversational order at every stage', () => {
    let s: ChatMessage[] = []
    // history from loadHistory (DB ids, no seq)
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 38348, content: 'old' }),
        a({ id: 38349, content: 'old reply' }),
      ],
    }])
    expect(display(s)).toBe('user:38348 | assistant:38349')

    // sendMessageNow('1') — bubble, then user_message self-echo adopts id 38350
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 10 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 38350 }])
    // reply1 placeholder (id 38351) + finalize when reply1 done
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 38351, streaming: true, seq: 11 }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply1' }])
    s = run(s, [{ type: 'stream_finalize' }])
    expect(display(s)).toBe('user:38348 | assistant:38349 | user:38350 | assistant:38351')

    // drain msg2 → materialized as 38352, reply2 placeholder 38353
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 38352, content: '2' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 38353, streaming: true, seq: 12 }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    expect(display(s)).toBe(
      'user:38348 | assistant:38349 | user:38350 | assistant:38351 | user:38352 | assistant:38353(s)'
    )

    // drain msg3
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 38354, content: '3' } }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 38355, streaming: true, seq: 13 }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    expect(display(s)).toBe(
      'user:38348 | assistant:38349 | user:38350 | assistant:38351 | user:38352 | assistant:38353 | user:38354 | assistant:38355(s)'
    )
  })

  // ── Regression: db_load adopts msg1 BEFORE the user_message self-echo
  //    arrives. The bubble is adopted via content match (db_load path).
  it('db_load adopts msg1 first; late self-echo is a no-op', () => {
    let s: ChatMessage[] = []
    // msg1 bubble + reply placeholder
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1, createdAt: '2026-01-01T00:00:01Z' }) }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, createdAt: '2026-01-01T00:00:01Z' }) }])
    // db_load arrives with msg1 row (persist lag: createdAt a few seconds later)
    s = run(s, [{
      type: 'db_load',
      dbMessages: [
        u({ id: 38350, content: '1', createdAt: '2026-01-01T00:00:04Z' }),
        a({ id: 38351, content: 'reply1', streaming: true, createdAt: '2026-01-01T00:00:01Z' }),
      ],
    }])
    // msg1 adopted (content match); reply1 DB streaming row merged into the
    // live placeholder (id 38351) — no duplicate.
    const msg1 = s.find((m) => m.role === 'user')
    expect(msg1?.id).toBe(38350)
    const replies = s.filter((m) => m.role === 'assistant')
    expect(replies).toHaveLength(1)
    expect(replies[0].id).toBe(38351)
    // order: msg1, reply1 (still streaming — live placeholder merged with id)
    expect(display(s)).toBe('user:38350 | assistant:38351(s)')

    // late self-echo adopt — bubble id already numeric, findIndex by pending-1 misses → no-op
    const lenBefore = s.length
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 38350 }])
    expect(s.length).toBe(lenBefore)
    expect(s.find((m) => m.role === 'user')?.id).toBe(38350)
  })

  // ── Cross-device: phone sends a message while browser also sends one. The
  //    phone message arrives as a user_message remote (numeric DB id); the
  //    browser message is adopted via self-echo. Both must sort by DB id —
  //    never by client receive order (seq), which would interleave them wrong.
  it('cross-device remote message and local message sort by DB id, not receive order', () => {
    let s: ChatMessage[] = []
    // history
    s = run(s, [{ type: 'db_load', dbMessages: [
      u({ id: 1, content: 'q1' }), a({ id: 2, content: 'r1' }),
      u({ id: 3, content: 'q2' }), a({ id: 4, content: 'r2' }),
    ] }])
    // phone sends (DB id 7) → remote user_message with numeric id
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 7, content: 'from phone', senderClientId: 'phone', backend: 'codebuddy' } }])
    const phone = s.find((m) => m.content === 'from phone')
    expect(phone?.id).toBe(7)
    expect(phone?._remote).toBe(true)
    // numeric id remote must sort by id, NOT TRANSIENT_BASE+seq
    expect(messageSortValue(phone!)).toBe(7)
    // browser sends its own msg (DB id 5) → adopted via self-echo
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-local', content: 'from browser', seq: 1 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-local', dbId: 5 }])
    // Correct DB order: local(5) then phone(7) — not receive order.
    expect(display(s)).toBe('user:1 | assistant:2 | user:3 | assistant:4 | user:5 | user:7')
  })

  // ── Cross-device: db_load adopts the remote message (byId), clearing its
  //    transient _remote markers so it stays a plain DB row.
  it('db_load clears _remote markers on an adopted remote message', () => {
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 9, content: 'from phone', senderClientId: 'phone', backend: 'codebuddy' } }])
    s = run(s, [{ type: 'db_load', dbMessages: [
      u({ id: 1, content: 'q1' }), a({ id: 2, content: 'r1' }),
      u({ id: 9, content: 'from phone' }),
    ] }])
    const phone = s.find((m) => m.content === 'from phone')
    expect(phone?._remote).toBeUndefined()
    expect((phone as any)?._remoteQueueId).toBeUndefined()
    expect(phone?.id).toBe(9)
  })

  // ── Regression: interleaving direct-sent and queued messages. All adopted
  //    messages must sort by DB id (single id domain), so a drained queued
  //    message (id 11) never jumps above a later direct message (id 12).
  it('direct→queued→direct interleaving sorts all adopted messages by DB id', () => {
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 10 }]) // direct, adopted
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-3', content: '3', seq: 3 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-3', dbId: 12 }]) // direct, adopted
    // drain queued msg2 → arrives as its own user_message with id 11
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 11, content: '2' } }])
    const msg2 = s.find((m) => m.content === '2')
    expect(msg2?.id).toBe(11)
    const userIds = s.filter((m) => m.role === 'user').map((m) => String(m.id))
    expect(userIds).toEqual(['10', '11', '12'])
  })
})
