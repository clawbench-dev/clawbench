import { describe, expect, it } from 'vitest'
import { chatMessageReducer, type ChatMessage, type ChatMessageAction } from '@/utils/chatStreamUtils'
function run(s: ChatMessage[], acts: ChatMessageAction[]) { let st = s; for (const a of acts) st = chatMessageReducer(st, a); return st }
const u = (p: any): ChatMessage => ({ role: 'user', content: '', blocks: [], files: [], createdAt: '', ...p })
const a = (p: any): ChatMessage => ({ role: 'assistant', content: '', blocks: [], createdAt: '', ...p })

describe('APP restart with a queued message', () => {
  it('loadHistory marks a queued row pending (id is numeric, queueId kept)', () => {
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'db_load', dbMessages: [
      u({ id: 1, content: 'q1' }), a({ id: 2, content: 'r1' }),
      u({ id: 38700, content: 'cancel me', queueId: 'pending-x', queued: true }),
    ] }])
    const q = s.find((m) => m.content === 'cancel me')
    expect(q?.pending).toBe(true)
    expect(q?.id).toBe(38700)
    expect(q?.queueId).toBe('pending-x')
  })

  it('remove_pending matches by queueId after restart (id is numeric — NOT the queueId)', () => {
    // Regression: after an app restart the queued message's id is the DB id
    // (38700), but the cancel button must use queueId — the backend DELETE and
    // remove_pending both key on queue_id, not the numeric id.
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'db_load', dbMessages: [
      u({ id: 38700, content: 'cancel me', queueId: 'pending-x', queued: true }),
    ] }])
    // Cancel emits msg.queueId (see ChatMessageItem pending-remove button).
    s = run(s, [{ type: 'remove_pending', queueId: 'pending-x' }])
    expect(s.find((m) => m.content === 'cancel me')).toBeUndefined()
  })

  it('the backend DELETE keys on queue_id, not the numeric id (the pre-fix cancel path)', () => {
    // Pre-fix ChatMessageItem emitted msg.id (38700). remove_pending would
    // still match (String(id) === '38700'), BUT the backend DELETE /api/ai/queue
    // looks up queue_id='pending-x' and finds nothing → the message stays.
    // The queueId value sent is what matters end-to-end; assert the numeric id
    // would NOT be a valid backend queueId.
    let s: ChatMessage[] = []
    s = run(s, [{ type: 'db_load', dbMessages: [
      u({ id: 38700, content: 'cancel me', queueId: 'pending-x', queued: true }),
    ] }])
    const q = s.find((m) => m.content === 'cancel me')
    // The cancel button must send queueId, never the numeric DB id.
    expect(q?.queueId).toBe('pending-x')
    expect(String(q?.id)).not.toBe(q?.queueId)
  })
})
