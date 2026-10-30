import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ShareNotesPoller } from '../liveNotesClient'
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
