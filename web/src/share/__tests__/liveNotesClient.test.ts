import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ShareNotesPoller, fetchNotesSnapshot } from '../liveNotesClient'
import { setShareToken } from '../shareMode'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as unknown as Response
}

describe('ShareNotesPoller', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    setShareToken('tok')
  })

  afterEach(() => {
    vi.useRealTimers()
    setShareToken(null)
  })

  it('polls incrementally and merges segments across ticks', async () => {
    const calls: string[] = []
    const fetchImpl = vi.fn(async (input: RequestInfo | URL) => {
      calls.push(String(input))
      if (calls.length === 1) {
        return jsonResponse({
          segments: [{ id: 1, channel: 'system', speaker: '系统声音', startedAt: 1000, durationMs: 5000, text: '第一句' }],
          minutes: { status: 'idle' },
        })
      }
      return jsonResponse({
        segments: [{ id: 2, channel: 'mic', speaker: '我', startedAt: 500, durationMs: 4000, text: '更早说的' }],
        minutes: { status: 'idle' },
      })
    })

    const snapshots: number[] = []
    const poller = new ShareNotesPoller(
      (snapshot) => snapshots.push(snapshot.segments.length),
      1000,
      fetchImpl as unknown as typeof fetch,
    )
    poller.start()
    await vi.advanceTimersByTimeAsync(0)
    expect(snapshots).toEqual([1])
    expect(calls[0]).toContain('/api/share/tok/notes?since=0')

    await vi.advanceTimersByTimeAsync(1000)
    expect(snapshots).toEqual([1, 2])
    // The cursor follows the highest id, so the next poll asks only for new rows.
    expect(calls[1]).toContain('since=1')

    poller.stop()
  })

  it('stops polling when the share has ended (404)', async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({}, 404))
    const onUpdate = vi.fn()
    const poller = new ShareNotesPoller(onUpdate, 1000, fetchImpl as unknown as typeof fetch)

    poller.start()
    await vi.advanceTimersByTimeAsync(0)
    await vi.advanceTimersByTimeAsync(5000)

    expect(onUpdate).not.toHaveBeenCalled()
    expect(fetchImpl).toHaveBeenCalledTimes(1)
  })
})

/**
 * The one-shot read exists for a viewer who joined after 听记 was switched off:
 * the tab must still exist if the meeting recorded something. It must never
 * throw — "could not tell" is a normal answer and the caller keeps the tab
 * hidden — and it must not start a poll.
 */
describe('fetchNotesSnapshot', () => {
  beforeEach(() => {
    setShareToken('tok')
  })

  afterEach(() => {
    setShareToken(null)
  })

  it('reads the transcript once, from the beginning', async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({
        segments: [{ id: 7, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 2, text: '早' }],
        files: [{ name: 'a.ts', openedAt: 1, closedAt: 2 }],
        minutes: { status: 'done', markdown: '# 纪要' },
      }),
    )

    const snapshot = await fetchNotesSnapshot(fetchImpl as unknown as typeof fetch)

    expect(String(fetchImpl.mock.calls[0][0])).toContain('/api/share/tok/notes?since=0')
    expect(snapshot?.segments.map((s) => s.id)).toEqual([7])
    expect(snapshot?.files.map((f) => f.name)).toEqual(['a.ts'])
    expect(snapshot?.minutes.status).toBe('done')
  })

  it('answers null — not an empty snapshot — when the read fails', async () => {
    const notFound = vi.fn(async () => jsonResponse({}, 404))
    expect(await fetchNotesSnapshot(notFound as unknown as typeof fetch)).toBeNull()

    const broken = vi.fn(async () => {
      throw new Error('offline')
    })
    expect(await fetchNotesSnapshot(broken as unknown as typeof fetch)).toBeNull()
  })
})
