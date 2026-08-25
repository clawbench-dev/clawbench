import { describe, expect, it } from 'vitest'
import { chatMessageReducer, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils.ts'

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
})
