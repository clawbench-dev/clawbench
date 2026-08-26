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
  s.map((m) => `${m.role}:${String(m.id)}${m.streaming ? '(s)' : ''}${m.pending ? '(p)' : ''}`).join(' | ')

describe('chat queue full-flow integration', () => {
  it('msg1 direct, msg2/msg3 queued: order matches reload at every stage', () => {
    let s: ChatMessage[] = []

    // 1. sendMessageNow('1') → optimistic bubble, POST returns, stream_start
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) }])
    s = run(s, [{ type: 'ws_stream_start', messageId: 1 }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, parentQueueId: 'pending-1', createdAt: '2026-01-01T00:00:00Z' }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply1' }])

    // 2. msg2/msg3 queue while reply1 streams
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 3 }) }])
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-3', content: '3', pending: true, seq: 4 }) }])

    // Streaming reply1 anchored after msg1, queued bubbles after it.
    // msg1's optimistic bubble keeps its string id until db_load adopts it
    // (sendMessageNow never mutates the id in place).
    expect(display(s)).toBe('user:pending-1 | assistant:drain-r1(s) | user:pending-2(p) | user:pending-3(p)')

    // 3. done(reply1) → background loadHistory: DB rows arrive. msg1 bubble
    //    adopts id=1 (content match), reply1 placeholder adopts id=2 (createdAt
    //    match), msg2/msg3 matched by queueId → pending stays.
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1', createdAt: '2026-01-01T00:00:05Z' }),
        a({ id: 2, content: 'reply1', createdAt: '2026-01-01T00:00:01Z' }),
        u({ id: 3, content: '2', queueId: 'pending-2', queued: true, createdAt: '2026-01-01T00:00:06Z' }),
        u({ id: 4, content: '3', queueId: 'pending-3', queued: true, createdAt: '2026-01-01T00:00:07Z' }),
      ],
    }])
    // No duplicates: msg1 is id=1, reply1 is id=2 (not drain-r1).
    expect(display(s)).toBe('user:1 | assistant:2 | user:pending-2(p) | user:pending-3(p)')

    // 4. drain('2') → msg2 adopts id=3, reply2 placeholder anchors to msg2
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-2', text: '2', files: [], dbMessageId: 3 }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    const reply2 = s.find((m) => m.role === 'assistant' && m.streaming)!
    // Simulate a LONG AI reply: placeholder created at drain time, DB row only
    // finalized 60s later. The ±5s createdAt fallback would fail — the queueId
    // match (parentQueueId === DB row queueId) must carry the adoption.
    reply2.createdAt = '2026-01-01T00:00:08Z'
    expect(display(s)).toBe(`user:1 | assistant:2 | user:3 | assistant:${String(reply2.id)}(s) | user:pending-3(p)`)

    // 5. drain('3') → msg3 adopts id=4, reply3 placeholder anchors to msg3
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-3', text: '3', files: [], dbMessageId: 4 }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    const reply3 = s.find((m) => m.role === 'assistant' && m.streaming)!
    reply3.createdAt = '2026-01-01T00:00:09Z'
    expect(display(s)).toBe(
      `user:1 | assistant:2 | user:3 | assistant:${String(reply2.id)} | user:4 | assistant:${String(reply3.id)}(s)`
    )

    // 6. Final db_load after all done — reply placeholders adopt DB ids via
    //    queueId match (DB reply rows carry queueId = the drained queue).
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1', createdAt: '2026-01-01T00:00:01Z' }),
        u({ id: 3, content: '2', queueId: 'pending-2', queued: false, createdAt: '2026-01-01T00:00:06Z' }),
        a({ id: 5, content: 'reply2', queueId: 'pending-2', createdAt: '2026-01-01T00:01:08Z' }),
        u({ id: 4, content: '3', queueId: 'pending-3', queued: false, createdAt: '2026-01-01T00:00:07Z' }),
        a({ id: 6, content: 'reply3', queueId: 'pending-3', createdAt: '2026-01-01T00:01:09Z' }),
      ],
    }])
    // Exactly the reload order: msg1, reply1, msg2, reply2, msg3, reply3.
    const finalIds = s.map((m) => (m.role === 'assistant' ? `a${String(m.id)}` : `u${String(m.id)}`))
    expect(finalIds).toEqual(['u1', 'a2', 'u3', 'a5', 'u4', 'a6'])
    expect(s.some((m) => m.pending)).toBe(false)
  })

  it('msg1 direct only: bubble adopts id on db_load, reply adopts on final reload', () => {
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: 'hello', seq: 1 }) }])
    s = run(s, [{ type: 'ws_stream_start', messageId: 1 }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, parentQueueId: '1', createdAt: '2026-01-01T00:00:00Z' }) }])
    s = run(s, [{ type: 'ws_content', text: 'world' }])
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: 'hello' }),
        a({ id: 2, content: 'world', createdAt: '2026-01-01T00:00:01Z' }),
      ],
    }])
    expect(display(s)).toBe('user:1 | assistant:2')
  })

  // ── User-reported bug: "reply1 done, msg2/msg3 and their replies appear
  //    ABOVE msg1/reply1 until everything finishes; reload fixes it."
  //    Root cause: msg1's optimistic bubble and reply1's drain-* placeholder
  //    stay transient (huge sort values) when the background loadHistory lags,
  //    while msg2/msg3 adopt DB ids → they sort above msg1/reply1. Also the
  //    old createdAt±5s adoption failed under backend persist lag (>5s),
  //    appending duplicate DB rows.
  it('db_load with persist lag keeps every message in conversational order', () => {
    const t0 = '2026-01-01T00:00:00Z'
    const tLate = '2026-01-01T00:01:00Z' // backend persisted 60s after the bubble
    let s: ChatMessage[] = []

    // msg1 direct send (id=pending-1, NOT pending), reply1 placeholder
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1, createdAt: t0 }) }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, parentQueueId: 'pending-1', createdAt: t0 }) }])
    s = run(s, [{ type: 'ws_content', text: 'reply1' }])
    // msg2/msg3 queue
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 3, createdAt: t0 }) }])
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-3', content: '3', pending: true, seq: 4, createdAt: t0 }) }])

    // reply1 done, background loadHistory arrives LATE (createdAt 60s later)
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1', createdAt: tLate }),
        a({ id: 2, content: 'reply1', createdAt: tLate }),
        u({ id: 3, content: '2', queueId: 'pending-2', queued: true, createdAt: tLate }),
        u({ id: 4, content: '3', queueId: 'pending-3', queued: true, createdAt: tLate }),
      ],
    }])
    // msg1 adopted (content match, persist lag immune), reply1 adopted, msg2/3
    // still queued bubbles AFTER reply1.
    expect(display(s)).toBe('user:1 | assistant:2 | user:pending-2(p) | user:pending-3(p)')

    // drain msg2 → adopts id=3 (parent msg1 is DB-backed now)
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-2', text: '2', files: [], dbMessageId: 3 }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    const r2 = s.find((m) => m.role === 'assistant' && m.streaming)!
    r2.createdAt = tLate
    expect(display(s)).toBe(`user:1 | assistant:2 | user:3 | assistant:${String(r2.id)}(s) | user:pending-3(p)`)

    // drain msg3
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-3', text: '3', files: [], dbMessageId: 4 }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    const r3 = s.find((m) => m.role === 'assistant' && m.streaming)!
    r3.createdAt = tLate
    expect(display(s)).toBe(
      `user:1 | assistant:2 | user:3 | assistant:${String(r2.id)} | user:4 | assistant:${String(r3.id)}(s)`
    )

    // final db_load — reply2/3 adopted via queueId match
    s = run(s, [{ type: 'stream_finalize' }])
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 1, content: '1' }),
        a({ id: 2, content: 'reply1', createdAt: tLate }),
        u({ id: 3, content: '2', queueId: 'pending-2', queued: false, createdAt: tLate }),
        a({ id: 5, content: 'reply2', queueId: 'pending-2', createdAt: tLate }),
        u({ id: 4, content: '3', queueId: 'pending-3', queued: false, createdAt: tLate }),
        a({ id: 6, content: 'reply3', queueId: 'pending-3', createdAt: tLate }),
      ],
    }])
    const finalIds = s.map((m) => (m.role === 'assistant' ? `a${String(m.id)}` : `u${String(m.id)}`))
    expect(finalIds).toEqual(['u1', 'a2', 'u3', 'a5', 'u4', 'a6'])
    expect(s.some((m) => m.pending)).toBe(false)
  })

  // ── Regression: msg1 bubble not yet adopted, msg2/msg3 already drained
  //    (adopted DB ids but still carry queueId → seq-space sort). All live
  //    messages sort in seq space, so msg1 (earliest seq) comes first.
  it('unadopted msg1 bubble sorts before drained msg2/msg3 (all in seq space)', () => {
    const state: ChatMessage[] = [
      u({ id: 'pending-abc', content: '1', seq: 1 }),
      a({ id: 'drain-r1', content: 'reply1', seq: 2, parentQueueId: 'pending-abc' }),
      u({ id: 38308, content: '2', queueId: 'pending-2', seq: 3 }),
      a({ id: 'drain-r2', content: 'reply2', seq: 4, parentQueueId: 'pending-2' }),
      u({ id: 38309, content: '3', queueId: 'pending-3', seq: 5 }),
      a({ id: 'drain-r3', content: 'reply3', seq: 6, parentQueueId: 'pending-3' }),
    ]
    sortMessages(state)
    const order = state.map((m) => (m.role === 'user' ? `u:${String(m.id)}` : `a:${String(m.id)}`))
    expect(order).toEqual(['u:pending-abc', 'a:drain-r1', 'u:38308', 'a:drain-r2', 'u:38309', 'a:drain-r3'])
  })

  // ── Realistic session-with-history scenario (user-reported):
  //    history loaded (no seq), then msg1 sent (adopted via user_message
  //    self-echo), msg2/msg3 queued then drained. Order must be:
  //    history, msg1, reply1, msg2, reply2, msg3, reply3 — at every stage.
  it('with history: msg1 adopted, msg2/3 drained — conversational order at every stage', () => {
    let s: ChatMessage[] = []
    // history from loadHistory (DB ids, no seq, no queueId)
    s = run(s, [{
      type: 'db_load', sessionRunning: false,
      dbMessages: [
        u({ id: 38348, content: 'old' }),
        a({ id: 38349, content: 'old reply' }),
      ],
    }])
    expect(display(s)).toBe('user:38348 | assistant:38349')

    // sendMessageNow('1') — bubble, then user_message self-echo adopts id 38350
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 10 }) }])
    s = run(s, [{ type: 'optimistic_adopt_id', id: 'pending-1', dbId: 38350 }])
    // reply1 placeholder anchored to msg1
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 11, parentQueueId: 'pending-1' }) }])
    // msg2/msg3 queue
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-2', content: '2', pending: true, seq: 12 }) }])
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-3', content: '3', pending: true, seq: 13 }) }])
    // order: history, msg1, reply1, msg2(pending), msg3(pending)
    const mid = display(s)
    expect(mid.startsWith('user:38348 | assistant:38349 | user:38350 | assistant:drain-r1')).toBe(true)

    // drain msg2 → adopts id 38351 (keeps seq space)
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-2', text: '2', files: [], dbMessageId: 38351 }])
    s = run(s, [{ type: 'ws_content', text: 'reply2' }])
    const r2 = s.find((m) => m.role === 'assistant' && m.streaming)!
    // order: history, msg1, reply1, msg2, reply2, msg3(pending)
    expect(display(s)).toBe(
      `user:38348 | assistant:38349 | user:38350 | assistant:drain-r1 | user:38351 | assistant:${String(r2.id)}(s) | user:pending-3(p)`
    )

    // drain msg3
    s = run(s, [{ type: 'ws_queue_drain', queueId: 'pending-3', text: '3', files: [], dbMessageId: 38352 }])
    s = run(s, [{ type: 'ws_content', text: 'reply3' }])
    const r3 = s.find((m) => m.role === 'assistant' && m.streaming)!
    expect(display(s)).toBe(
      `user:38348 | assistant:38349 | user:38350 | assistant:drain-r1 | user:38351 | assistant:${String(r2.id)} | user:38352 | assistant:${String(r3.id)}(s)`
    )
  })

  // ── Regression: db_load adopts msg1 BEFORE the user_message self-echo
  //    arrives. The bubble is adopted via content match (db_load path), the
  //    reply's parentQueueId must be rewritten to the new DB id so the anchor
  //    keeps resolving. The late self-echo adopt is then a no-op (id already
  //    numeric → findIndex by pending-1 misses).
  it('db_load adopts msg1 first; reply anchor rewritten; late self-echo is no-op', () => {
    let s: ChatMessage[] = []
    // msg1 bubble + reply placeholder
    s = run(s, [{ type: 'optimistic_push', msg: u({ id: 'pending-1', content: '1', seq: 1 }) }])
    s = run(s, [{ type: 'stream_placeholder', msg: a({ id: 'drain-r1', streaming: true, seq: 2, parentQueueId: 'pending-1', createdAt: '2026-01-01T00:00:01Z' }) }])
    // db_load arrives with msg1 row (persist lag: createdAt far apart)
    s = run(s, [{
      type: 'db_load', sessionRunning: true,
      dbMessages: [
        u({ id: 38350, content: '1', createdAt: '2026-01-01T00:01:00Z' }),
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
    // reply's parentQueueId rewritten from pending-1 → 38350
    expect(String(replies[0].parentQueueId)).toBe('38350')
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
    s = run(s, [{ type: 'db_load', sessionRunning: false, dbMessages: [
      u({ id: 1, content: 'q1' }), a({ id: 2, content: 'r1' }),
      u({ id: 3, content: 'q2' }), a({ id: 4, content: 'r2' }),
    ] }])
    // phone sends (DB id 7) → remote user_message with numeric id
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 7, content: 'from phone', senderClientId: 'phone', queueId: 'pending-phone', backend: 'codebuddy' } }])
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
    s = run(s, [{ type: 'ws_user_message', data: { messageId: 9, content: 'from phone', senderClientId: 'phone', queueId: 'pending-phone', backend: 'codebuddy' } }])
    s = run(s, [{ type: 'db_load', sessionRunning: false, dbMessages: [
      u({ id: 1, content: 'q1' }), a({ id: 2, content: 'r1' }),
      u({ id: 9, content: 'from phone' }),
    ] }])
    const phone = s.find((m) => m.content === 'from phone')
    expect(phone?._remote).toBeUndefined()
    expect((phone as any)?._remoteQueueId).toBeUndefined()
    expect(phone?.id).toBe(9)
  })
})
