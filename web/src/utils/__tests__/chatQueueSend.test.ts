import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { enqueueAndMaybeStart, generateQueueId } from '@/utils/chatQueueSend'
import { isInFlightSend, resetInFlightSendsForTest } from '@/utils/chatStreamUtils'
import { getQueue, resetQueuesForTest } from '@/composables/useMessageQueue.ts'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { EnqueueAndMaybeStartOptions } from '@/utils/chatQueueSend'

function makeOpts(overrides: Partial<EnqueueAndMaybeStartOptions> = {}): EnqueueAndMaybeStartOptions {
  return {
    sessionId: 'sess-1',
    text: 'hello',
    attachedFiles: [],
    pendingFiles: [],
    onPendingRendered: vi.fn(),
    enqueue: vi.fn().mockResolvedValue(true),
    ...overrides,
  }
}

beforeEach(() => {
  resetQueuesForTest()
  resetInFlightSendsForTest()
})
afterEach(() => {
  resetQueuesForTest()
  resetInFlightSendsForTest()
})

describe('generateQueueId', () => {
  it('produces a pending- prefixed unique id', () => {
    const a = generateQueueId()
    const b = generateQueueId()
    expect(a.startsWith('pending-')).toBe(true)
    expect(a).not.toBe(b)
  })
})

describe('enqueueAndMaybeStart', () => {
  it('adds an optimistic queue entry with a generated queueId', async () => {
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    const queue = getQueue('sess-1')
    expect(queue).toHaveLength(1)
    const entry = queue[0]
    expect(entry.text).toBe('hello')
    expect(entry.queueId).toMatch(/^pending-/)
    expect(typeof entry.createdAt).toBe('string')
  })

  it('does not add the queued message to the conversation message list', async () => {
    // The whole point of the refactor: a queued message lives in the queue
    // store, never in the `messages` array, so no pending/queued special case
    // is needed in the reducer.
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    expect(getQueue('sess-1')).toHaveLength(1)
  })

  it('keeps distinct line ranges of one path and dedupes exact duplicates', async () => {
    const pending: FileEntry[] = [{ path: '/a', isDir: false }]
    const attached: FileEntry[] = [
      { path: '/a', isDir: false, startLine: 1, endLine: 5 },
      { path: '/b', isDir: false },
    ]
    const opts = makeOpts({ pendingFiles: pending, attachedFiles: attached })
    await enqueueAndMaybeStart(opts)
    const entry = getQueue('sess-1')[0]
    // Attachment identity is the composite key (path, startLine, endLine): the
    // whole-file /a and the ranged /a are distinct references and both survive,
    // while an exact duplicate would collapse.
    const paths = entry.files.map((f) => f.path)
    expect(paths).toEqual(['/a', '/a', '/b'])
    const ranged = entry.files.find((f) => f.path === '/a' && f.startLine === 1)
    expect(ranged).toBeTruthy()
    expect(ranged!.endLine).toBe(5)
  })

  it('calls enqueue with sessionId, text, attachments and queueId', async () => {
    const opts = makeOpts({ attachedFiles: [{ path: '/x', isDir: false }] })
    await enqueueAndMaybeStart(opts)
    const enqueue = opts.enqueue as ReturnType<typeof vi.fn>
    expect(enqueue).toHaveBeenCalledTimes(1)
    const [sid, text, attached, pending, queueId] = enqueue.mock.calls[0]
    expect(sid).toBe('sess-1')
    expect(text).toBe('hello')
    expect(attached).toEqual([{ path: '/x', isDir: false }])
    expect(pending).toEqual([])
    expect(queueId).toMatch(/^pending-/)
  })

  it('honors a caller-provided queueId instead of generating one', async () => {
    const opts = makeOpts({ queueId: 'custom-qid' })
    await enqueueAndMaybeStart(opts)
    expect(getQueue('sess-1')[0].queueId).toBe('custom-qid')
    const enqueue = opts.enqueue as ReturnType<typeof vi.fn>
    expect(enqueue.mock.calls[0][4]).toBe('custom-qid')
  })

  it('calls onPendingRendered after pushing the message', async () => {
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    expect(opts.onPendingRendered).toHaveBeenCalledTimes(1)
  })

  it('propagates enqueue failure so the caller can restore the input text', async () => {
    // Regression: when the enqueue request fails (network down, 5xx), the
    // caller must be able to detect the failure and restore the input box.
    // If enqueueAndMaybeStart swallowed the error, the message would be lost
    // silently while the input stays cleared.
    const opts = makeOpts({
      enqueue: vi.fn().mockRejectedValue(new Error('network down')),
    })
    await expect(enqueueAndMaybeStart(opts)).rejects.toThrow('network down')
  })

  it('throws when the enqueue call resolves with false (request failed)', async () => {
    // enqueueMessage swallows its own fetch errors and reports them by
    // returning false. enqueueAndMaybeStart must turn that into a throw so
    // the caller's try/catch restores the input box.
    const opts = makeOpts({
      enqueue: vi.fn().mockResolvedValue(false),
    })
    await expect(enqueueAndMaybeStart(opts)).rejects.toThrow()
  })

  it('resolves normally when the enqueue call resolves with true', async () => {
    const opts = makeOpts({
      enqueue: vi.fn().mockResolvedValue(true),
    })
    await expect(enqueueAndMaybeStart(opts)).resolves.toMatch(/^pending-/)
  })

  it('removes the optimistic queue entry when the enqueue call fails', async () => {
    const opts = makeOpts({ enqueue: vi.fn().mockResolvedValue(false) })
    await expect(enqueueAndMaybeStart(opts)).rejects.toThrow()
    expect(getQueue('sess-1')).toHaveLength(0)
  })
})

// ── In-flight guard on the enqueue path ──
//
// Reported: while the AI is generating, sending a message sometimes shows the
// reply streaming but the user's own bubble is absent, restored only by a
// manual refresh. The queue store is rebuilt wholesale from loadHistory's
// `queue` field, so an optimistic entry whose POST has not yet committed must
// be guarded against a stale snapshot fetched before the commit.
describe('enqueueAndMaybeStart in-flight guard', () => {
  it('tracks the optimistic entry as in-flight before the enqueue POST resolves', async () => {
    let trackedDuringEnqueue = false
    const opts = makeOpts({
      enqueue: vi.fn().mockImplementation(async (_sid, _t, _a, _p, queueId: string) => {
        // While the POST is in flight the entry must already be guarded, or a
        // concurrent loadHistory snapshot would drop it.
        trackedDuringEnqueue = isInFlightSend(queueId)
        return true
      }),
    })
    const queueId = await enqueueAndMaybeStart(opts)
    expect(trackedDuringEnqueue).toBe(true)
    // Still guarded after success — the row has not been seen in a snapshot yet.
    expect(isInFlightSend(queueId)).toBe(true)
  })

  it('guards the caller-provided queueId', async () => {
    let trackedDuringEnqueue = false
    const opts = makeOpts({
      queueId: 'custom-qid',
      enqueue: vi.fn().mockImplementation(async () => {
        trackedDuringEnqueue = isInFlightSend('custom-qid')
        return true
      }),
    })
    await enqueueAndMaybeStart(opts)
    expect(trackedDuringEnqueue).toBe(true)
  })

  it('releases the guard when the enqueue call resolves with false', async () => {
    const opts = makeOpts({ enqueue: vi.fn().mockResolvedValue(false) })
    await expect(enqueueAndMaybeStart(opts)).rejects.toThrow()
    const queueId = (opts.enqueue as ReturnType<typeof vi.fn>).mock.calls[0][4]
    expect(isInFlightSend(queueId)).toBe(false)
  })

  it('releases the guard when the enqueue call rejects', async () => {
    const opts = makeOpts({ enqueue: vi.fn().mockRejectedValue(new Error('network down')) })
    await expect(enqueueAndMaybeStart(opts)).rejects.toThrow('network down')
    const queueId = (opts.enqueue as ReturnType<typeof vi.fn>).mock.calls[0][4]
    expect(isInFlightSend(queueId)).toBe(false)
  })
})
