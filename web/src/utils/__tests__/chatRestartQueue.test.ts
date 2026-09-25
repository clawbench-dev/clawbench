import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import {
  getQueue,
  syncFromHistory,
  removeQueued,
  resetQueuesForTest,
} from '@/composables/useMessageQueue.ts'

/**
 * APP restart with a queued message.
 *
 * A queued message is no longer a row in the conversation list: the backend
 * keeps it in its own queue table and only writes it into chat_history when it
 * is dequeued. After a restart, loadHistory returns it in the `queue` field,
 * which is fed to syncFromHistory. Cancel/insert/interrupt all key on queueId.
 */
describe('APP restart with a queued message', () => {
  beforeEach(() => resetQueuesForTest())
  afterEach(() => resetQueuesForTest())

  it('loadHistory populates the queue store from the queue snapshot', () => {
    syncFromHistory('sess-1', [
      { queueId: 'pending-x', text: 'cancel me' },
    ])
    const queue = getQueue('sess-1')
    expect(queue).toHaveLength(1)
    expect(queue[0].queueId).toBe('pending-x')
    expect(queue[0].text).toBe('cancel me')
  })

  it('the cancel button keys on queueId (the backend DELETE looks up queue_id)', () => {
    // The backend DELETE /api/ai/queue looks up queue_id, so the UI must send
    // the queueId string — never a numeric DB id (which a queued message does
    // not have yet anyway).
    syncFromHistory('sess-1', [{ queueId: 'pending-x', text: 'cancel me' }])
    removeQueued('sess-1', 'pending-x')
    expect(getQueue('sess-1')).toHaveLength(0)
  })

  it('a queueId that does not match leaves the queue untouched', () => {
    syncFromHistory('sess-1', [{ queueId: 'pending-x', text: 'cancel me' }])
    removeQueued('sess-1', 'pending-other')
    expect(getQueue('sess-1')).toHaveLength(1)
    expect(getQueue('sess-1')[0].queueId).toBe('pending-x')
  })

  it('an empty queue snapshot clears the stored queue (all drained)', () => {
    syncFromHistory('sess-1', [{ queueId: 'pending-x', text: 'cancel me' }])
    expect(getQueue('sess-1')).toHaveLength(1)
    syncFromHistory('sess-1', [])
    expect(getQueue('sess-1')).toHaveLength(0)
  })
})
