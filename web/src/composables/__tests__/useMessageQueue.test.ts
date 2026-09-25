import { describe, expect, it, beforeEach, afterEach } from 'vitest'
import {
  queuedMessages,
  queuedCount,
  setActiveQueueSession,
  getQueue,
  syncFromHistory,
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
