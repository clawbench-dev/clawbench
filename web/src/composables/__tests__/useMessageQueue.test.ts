import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import { nextTick, watchEffect } from 'vue'
import {
  queuedMessages,
  queuedCount,
  setActiveQueueSession,
  getQueue,
  syncFromHistory,
  beginQueueSnapshot,
  markSendCommitted,
  addQueued,
  removeQueued,
  removeQueuedMany,
  clearQueue,
  resetQueuesForTest,
  useMessageQueue,
} from '@/composables/useMessageQueue'
import { isInFlightSend, resetInFlightSendsForTest, trackInFlightSend } from '@/utils/chatStreamUtils'

beforeEach(() => {
  resetQueuesForTest()
  resetInFlightSendsForTest()
})
afterEach(() => {
  resetQueuesForTest()
  resetInFlightSendsForTest()
})

describe('useMessageQueue', () => {
  it('exposes the queue API from useMessageQueue()', () => {
    const api = useMessageQueue()
    expect(api.queuedMessages).toBe(queuedMessages)
    expect(api.queuedCount).toBe(queuedCount)
    expect(typeof api.setActiveQueueSession).toBe('function')
    expect(typeof api.getQueue).toBe('function')
    expect(typeof api.syncFromHistory).toBe('function')
    expect(typeof api.addQueued).toBe('function')
    expect(typeof api.removeQueued).toBe('function')
    expect(typeof api.removeQueuedMany).toBe('function')
    expect(typeof api.clearQueue).toBe('function')
  })
})

// The queue panel's COLLAPSED header renders `messages.length`. That count is
// derived from `queuedMessages`, a computed over a plain Map — so the store must
// publish a NEW array on every mutation. Mutating in place and bumping the
// version ref is NOT enough: Vue skips notifying dependents when the computed
// produces the same reference it produced last time. The bug was exactly this:
// the FIRST add worked (the `|| []` fallback happened to allocate a fresh
// array) while every later add reused that array, freezing the collapsed count
// at 1 until expanding the panel forced a re-render.
describe('queuedMessages reactivity (collapsed panel count)', () => {
  it('notifies dependents on EVERY add, not just the first', async () => {
    setActiveQueueSession('s1')
    const seen: number[] = []
    watchEffect(() => {
      seen.push(queuedMessages.value.length)
    })
    await nextTick()

    addQueued('s1', { queueId: 'q1', text: 'one' })
    await nextTick()
    addQueued('s1', { queueId: 'q2', text: 'two' })
    await nextTick()
    addQueued('s1', { queueId: 'q3', text: 'three' })
    await nextTick()

    expect(queuedMessages.value.length).toBe(3)
    expect(seen, `the collapsed count must track every add; saw ${JSON.stringify(seen)}`).toEqual([0, 1, 2, 3])
  })

  it('publishes a new array reference on each add (the mechanism)', () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'one' })
    const first = queuedMessages.value
    addQueued('s1', { queueId: 'q2', text: 'two' })
    expect(queuedMessages.value, 'same reference = dependents never re-run').not.toBe(first)
  })

  it('notifies when an existing entry is refreshed by the same queueId', async () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'q1', text: 'first' })
    await nextTick()

    const seen: string[] = []
    watchEffect(() => {
      seen.push(queuedMessages.value[0]?.text ?? '')
    })
    await nextTick()

    addQueued('s1', { queueId: 'q1', text: 'updated' })
    await nextTick()

    expect(getQueue('s1')[0].text).toBe('updated')
    expect(seen, 'a content refresh must reach the panel').toContain('updated')
  })
})

describe('addQueued', () => {
  it('appends a new entry', () => {
    addQueued('s1', { queueId: 'q1', text: 'hello', files: [] })
    const queue = getQueue('s1')
    expect(queue).toHaveLength(1)
    expect(queue[0].queueId).toBe('q1')
    expect(queue[0].text).toBe('hello')
    expect(typeof queue[0].createdAt).toBe('string')
  })

  it('dedupes by queueId and refreshes the existing entry in place', () => {
    addQueued('s1', { queueId: 'q1', text: 'first', files: [] })
    addQueued('s1', { queueId: 'q1', text: 'updated', files: [] })
    const queue = getQueue('s1')
    expect(queue).toHaveLength(1)
    expect(queue[0].text).toBe('updated')
  })

  it('keeps insertion order across distinct queueIds', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['a', 'b'])
  })

  it('ignores an empty queueId', () => {
    addQueued('s1', { queueId: '', text: 'x', files: [] })
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('normalizes filePaths into files and dedupes', () => {
    addQueued('s1', {
      queueId: 'q1',
      text: 'x',
      files: [{ path: '/a', isDir: false }],
      filePaths: ['/a', '/b'],
    })
    const files = getQueue('s1')[0].files
    expect(files.map((f) => f.path)).toEqual(['/a', '/b'])
  })
})

describe('removeQueued', () => {
  it('removes only the matching entry', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    removeQueued('s1', 'a')
    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['b'])
  })

  it('drops the session key entirely when the last entry is removed', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    removeQueued('s1', 'a')
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('releases the in-flight guard for the removed entry', () => {
    addQueued('s1', { queueId: 'q1', text: 'A', files: [] })
    trackInFlightSend('q1')
    expect(isInFlightSend('q1')).toBe(true)
    removeQueued('s1', 'q1')
    expect(isInFlightSend('q1')).toBe(false)
  })

  it('is a no-op for an unknown session or queueId', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    removeQueued('s1', 'missing')
    removeQueued('other', 'a')
    expect(getQueue('s1')).toHaveLength(1)
  })
})

describe('removeQueuedMany', () => {
  it('removes every listed entry and leaves the rest', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    addQueued('s1', { queueId: 'c', text: 'C', files: [] })
    removeQueuedMany('s1', ['a', 'c'])
    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['b'])
  })

  it('releases the in-flight guard for every removed entry', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    trackInFlightSend('a')
    trackInFlightSend('b')
    removeQueuedMany('s1', ['a', 'b'])
    expect(isInFlightSend('a')).toBe(false)
    expect(isInFlightSend('b')).toBe(false)
  })

  it('is a no-op for an empty list', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    removeQueuedMany('s1', [])
    expect(getQueue('s1')).toHaveLength(1)
  })
})

describe('clearQueue', () => {
  it('drops the whole session queue', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    clearQueue('s1')
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('releases the in-flight guard for every entry', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    trackInFlightSend('a')
    trackInFlightSend('b')
    clearQueue('s1')
    expect(isInFlightSend('a')).toBe(false)
    expect(isInFlightSend('b')).toBe(false)
  })

  it('leaves other sessions untouched', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s2', { queueId: 'b', text: 'B', files: [] })
    clearQueue('s1')
    expect(getQueue('s1')).toHaveLength(0)
    expect(getQueue('s2')).toHaveLength(1)
  })
})

describe('syncFromHistory', () => {
  it('replaces the session queue from the snapshot', () => {
    addQueued('s1', { queueId: 'stale', text: 'stale', files: [] })
    syncFromHistory('s1', [
      { queueId: 'a', text: 'A' },
      { queueId: 'b', text: 'B' },
    ])
    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['a', 'b'])
  })

  it('clears the queue when the snapshot is empty', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    syncFromHistory('s1', [])
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('clears the queue when the snapshot is undefined', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    syncFromHistory('s1', undefined)
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('PRESERVES an in-flight entry absent from the snapshot', () => {
    // The snapshot may have been fetched before the enqueue POST committed its
    // row, so the optimistic entry must survive it.
    addQueued('s1', { queueId: 'inflight', text: 'pending post', files: [] })
    trackInFlightSend('inflight')

    syncFromHistory('s1', [{ queueId: 'server', text: 'from server' }])

    const ids = getQueue('s1').map((m) => m.queueId)
    expect(ids).toContain('inflight')
    expect(ids).toContain('server')
    // The guard survives while the snapshot still lacks the entry.
    expect(isInFlightSend('inflight')).toBe(true)
  })

  it('drops a non-in-flight entry absent from the snapshot (no guard)', () => {
    addQueued('s1', { queueId: 'gone', text: 'no guard', files: [] })
    syncFromHistory('s1', [])
    expect(getQueue('s1')).toHaveLength(0)
  })

  it('releases the guard once the snapshot contains the entry', () => {
    addQueued('s1', { queueId: 'inflight', text: 'pending post', files: [] })
    trackInFlightSend('inflight')

    syncFromHistory('s1', [{ queueId: 'inflight', text: 'now committed' }])

    expect(isInFlightSend('inflight')).toBe(false)
    // The authoritative snapshot content wins.
    expect(getQueue('s1')).toHaveLength(1)
    expect(getQueue('s1')[0].text).toBe('now committed')
  })

  it('is a no-op for an empty sessionId', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    syncFromHistory('', [{ queueId: 'x', text: 'X' }])
    expect(getQueue('s1')).toHaveLength(1)
  })
})

// ── Drained-while-backgrounded entries ──
//
// Reported: queue a message, background the app, let the queue drain, then
// return to the foreground — the queue card is still there although the message
// finished. The drain's WS events (user_message / queue_drain) were missed
// while backgrounded, and the queue row is DELETED server-side at dequeue, so
// the guard's only release path ("a snapshot contains the entry") can never
// fire. The entry then survived even the authoritative foreground reopen, whose
// empty queue was overridden by the guard.
describe('syncFromHistory — authoritative absence of a committed entry', () => {
  it('drops a committed entry absent from a snapshot issued after the POST resolved', () => {
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1') // the enqueue POST resolved → row committed

    // A later snapshot (generation > the commit's) has an empty queue: the row
    // was drained/cancelled and the client missed the event.
    const gen = beginQueueSnapshot()
    syncFromHistory('s1', [], gen)

    expect(getQueue('s1')).toHaveLength(0)
    // The guard is released too, so an even later stale rebuild cannot
    // resurrect the entry.
    expect(isInFlightSend('q1')).toBe(false)
  })

  it('PRESERVES an un-acked entry absent from any snapshot (POST still in flight)', () => {
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    // No markSendCommitted: the POST has not resolved, so no snapshot can prove
    // the row is missing.
    const gen = beginQueueSnapshot()
    syncFromHistory('s1', [], gen)

    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['q1'])
    expect(isInFlightSend('q1')).toBe(true)
  })

  it('PRESERVES a committed entry against a snapshot issued BEFORE the POST resolved', () => {
    // The race the guard exists for: the snapshot request started before the
    // enqueue POST committed, so its missing row proves nothing.
    const genBeforeCommit = beginQueueSnapshot()
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1') // POST resolves after the snapshot was requested

    syncFromHistory('s1', [], genBeforeCommit)

    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['q1'])
    expect(isInFlightSend('q1')).toBe(true)
  })

  it('drops a committed entry once a snapshot is issued after its commit', () => {
    const genBeforeCommit = beginQueueSnapshot()
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1')

    // The pre-commit snapshot preserves it…
    syncFromHistory('s1', [], genBeforeCommit)
    expect(getQueue('s1')).toHaveLength(1)

    // …and the next snapshot (issued after the commit) is authoritative.
    const genAfterCommit = beginQueueSnapshot()
    syncFromHistory('s1', [], genAfterCommit)
    expect(getQueue('s1')).toHaveLength(0)
    expect(isInFlightSend('q1')).toBe(false)
  })

  it('keeps a committed entry that the later snapshot still lists', () => {
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1')

    const gen = beginQueueSnapshot()
    syncFromHistory('s1', [{ queueId: 'q1', text: 'still queued' }], gen)

    expect(getQueue('s1')).toHaveLength(1)
    expect(getQueue('s1')[0].text).toBe('still queued')
    expect(isInFlightSend('q1')).toBe(false)
  })

  it('releases the committed-send bookkeeping on explicit removal', () => {
    addQueued('s1', { queueId: 'q1', text: 'queued', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1')

    removeQueued('s1', 'q1')

    // A later authoritative empty snapshot must not need the bookkeeping, and
    // re-adding the same id must not inherit the old commit generation.
    expect(isInFlightSend('q1')).toBe(false)
    expect(getQueue('s1')).toHaveLength(0)
  })

  // WARN-501: the generation bookkeeping keys on queueId, so a queueId that is
  // REUSED for a new message must not inherit the previous commit's generation.
  // Otherwise a snapshot issued before the new message was committed would look
  // "newer than the commit" and wrongly prove the new entry absent — deleting a
  // message the user just sent.
  it('does not let a reused queueId inherit an earlier commit generation', () => {
    // First message commits, then is cleared — releasing its bookkeeping.
    addQueued('s1', { queueId: 'q1', text: 'first', files: [] })
    trackInFlightSend('q1')
    markSendCommitted('q1')
    const genAfterFirstCommit = beginQueueSnapshot()
    syncFromHistory('s1', [], genAfterFirstCommit)
    expect(getQueue('s1')).toHaveLength(0)

    // The same queueId is reused for a NEW message whose POST has not resolved.
    const genBeforeSecondCommit = beginQueueSnapshot()
    addQueued('s1', { queueId: 'q1', text: 'second', files: [] })
    trackInFlightSend('q1')

    // A snapshot requested before the second POST resolved must NOT be able to
    // prove the new entry absent, even though the id was committed once before.
    syncFromHistory('s1', [], genBeforeSecondCommit)

    expect(getQueue('s1').map((m) => m.queueId)).toEqual(['q1'])
    expect(getQueue('s1')[0].text).toBe('second')
    expect(isInFlightSend('q1')).toBe(true)
  })
})

describe('queuedMessages / queuedCount follow setActiveQueueSession', () => {
  it('reads the active session queue', () => {
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    addQueued('s2', { queueId: 'b', text: 'B', files: [] })

    setActiveQueueSession('s1')
    expect(queuedMessages.value.map((m) => m.queueId)).toEqual(['a'])
    expect(queuedCount.value).toBe(1)

    setActiveQueueSession('s2')
    expect(queuedMessages.value.map((m) => m.queueId)).toEqual(['b'])
    expect(queuedCount.value).toBe(1)
  })

  it('returns an empty list for an unknown session', () => {
    setActiveQueueSession('unknown')
    expect(queuedMessages.value).toEqual([])
    expect(queuedCount.value).toBe(0)
  })

  it('reflects mutations for the active session', () => {
    setActiveQueueSession('s1')
    addQueued('s1', { queueId: 'a', text: 'A', files: [] })
    expect(queuedCount.value).toBe(1)
    addQueued('s1', { queueId: 'b', text: 'B', files: [] })
    expect(queuedCount.value).toBe(2)
    removeQueued('s1', 'a')
    expect(queuedMessages.value.map((m) => m.queueId)).toEqual(['b'])
  })
})
