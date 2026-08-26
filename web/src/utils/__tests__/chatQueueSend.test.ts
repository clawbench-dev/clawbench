import { describe, expect, it, vi } from 'vitest'
import { enqueueAndMaybeStart, generateQueueId } from '@/utils/chatQueueSend'
import type { FileEntry } from '@/utils/fileAttachmentUtils'
import type { EnqueueAndMaybeStartOptions } from '@/utils/chatQueueSend'

function makeOpts(overrides: Partial<EnqueueAndMaybeStartOptions> = {}): EnqueueAndMaybeStartOptions {
  return {
    sessionId: 'sess-1',
    text: 'hello',
    attachedFiles: [],
    pendingFiles: [],
    pushMessage: vi.fn(),
    onPendingRendered: vi.fn(),
    enqueue: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  }
}

describe('generateQueueId', () => {
  it('produces a pending- prefixed unique id', () => {
    const a = generateQueueId()
    const b = generateQueueId()
    expect(a.startsWith('pending-')).toBe(true)
    expect(a).not.toBe(b)
  })
})

describe('enqueueAndMaybeStart', () => {
  it('pushes a pending user message with a generated queueId', async () => {
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    const push = opts.pushMessage as ReturnType<typeof vi.fn>
    expect(push).toHaveBeenCalledTimes(1)
    const msg = push.mock.calls[0][0]
    expect(msg.role).toBe('user')
    expect(msg.pending).toBe(true)
    expect(msg.content).toBe('hello')
    expect(msg.blocks).toEqual([{ type: 'text', text: 'hello' }])
    expect(msg.id).toMatch(/^pending-/)
  })

  it('assigns a monotonic seq to the pushed pending message so it sorts among transients', async () => {
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    const msg = (opts.pushMessage as ReturnType<typeof vi.fn>).mock.calls[0][0]
    // Without a seq, messageSortValue yields TRANSIENT_BASE + 0, jumping the
    // queued message above every earlier still-transient question. A number
    // guarantees a correct position in the transient ordering domain.
    expect(typeof msg.seq).toBe('number')
    expect(Number.isFinite(msg.seq)).toBe(true)
  })

  it('dedupes pending and attached files into the pending message files', async () => {
    const pending: FileEntry[] = [{ path: '/a', isDir: false }]
    const attached: FileEntry[] = [
      { path: '/a', isDir: false, startLine: 1, endLine: 5 },
      { path: '/b', isDir: false },
    ]
    const opts = makeOpts({ pendingFiles: pending, attachedFiles: attached })
    await enqueueAndMaybeStart(opts)
    const msg = (opts.pushMessage as ReturnType<typeof vi.fn>).mock.calls[0][0]
    // dedupeFiles keeps the richer entry (with line range) for /a
    const paths = msg.files.map((f: FileEntry) => f.path)
    expect(paths).toEqual(['/a', '/b'])
    const a = msg.files.find((f: FileEntry) => f.path === '/a')
    expect(a.startLine).toBe(1)
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

  it('enqueues the message for delivery', async () => {
    const opts = makeOpts()
    await enqueueAndMaybeStart(opts)
    expect(opts.enqueue).toHaveBeenCalledTimes(1)
  })

  it('honors a caller-provided queueId instead of generating one', async () => {
    const opts = makeOpts({ queueId: 'custom-qid' })
    await enqueueAndMaybeStart(opts)
    const msg = (opts.pushMessage as ReturnType<typeof vi.fn>).mock.calls[0][0]
    expect(msg.id).toBe('custom-qid')
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
})
