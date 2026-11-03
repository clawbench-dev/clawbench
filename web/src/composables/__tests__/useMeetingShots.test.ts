import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import {
  _resetMeetingShotsForTest,
  _setShotGrabberForTest,
  useMeetingShots,
} from '../useMeetingShots'
import { useLiveNotes, _resetLiveNotesForTest } from '../useLiveNotes'
import {
  _resetMeetingFileTrackerForTest,
  noteMeetingFileOpened,
  startMeetingFileTracking,
} from '@/utils/meetingFileTracker'

function stream(): MediaStream {
  return { getVideoTracks: () => [{}] } as unknown as MediaStream
}

function sample(hash = '0'.repeat(64), encode = vi.fn(async () => new Blob(['x']))) {
  return { hash, width: 1280, height: 720, encode }
}

function uploads(): Array<Record<string, unknown>> {
  const calls = (fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls
  return calls
    .filter(([url]) => url === '/api/live-share/notes/shot')
    .map(([, init]) => {
      const form = (init as RequestInit).body as FormData
      const out: Record<string, unknown> = {}
      form.forEach((value, key) => {
        out[key] = value
      })
      return out
    })
}

/** Sequence for the stubbed upload responses (one id per stored frame). */
let shotSeq = 0

describe('useMeetingShots', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    shotSeq = 0
    // The upload response carries the stored row: the id is what the transcript
    // row addresses, so the stub must answer like the real endpoint.
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ id: ++shotSeq, at: 1_700_000_000_000 }) })),
    )
    _resetMeetingShotsForTest()
    _resetLiveNotesForTest()
    _resetMeetingFileTrackerForTest()
    _setShotGrabberForTest(async () => sample())
  })

  afterEach(() => {
    _resetMeetingShotsForTest()
    _resetMeetingFileTrackerForTest()
    _resetLiveNotesForTest()
    _setShotGrabberForTest(null)
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  function start() {
    const shots = useMeetingShots()
    shots.start({ token: 'tok', getSource: () => stream(), getStrokes: () => [] })
    return shots
  }

  it('uploads a frame with its trigger and reference', async () => {
    const shots = start()
    await shots.captureNow('manual')
    await vi.advanceTimersByTimeAsync(0)

    const sent = uploads()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({ token: 'tok', reason: 'manual', width: '1280', height: '720' })
    expect(shots.count.value).toBe(1)
    // The stored row is kept locally so the live transcript can show the frame
    // immediately, without waiting for a history read.
    expect(shots.items.value).toHaveLength(1)
    expect(shots.items.value[0]).toMatchObject({ id: 1, at: 1_700_000_000_000, reason: 'manual' })
  })

  it('does not upload the same picture twice', async () => {
    const shots = start()
    await shots.captureNow('change')
    await vi.advanceTimersByTimeAsync(9_000)
    // Past the interval, so this one is rejected by the hash — the rule the
    // scheduler exists for.
    await shots.captureNow('change')
    await vi.advanceTimersByTimeAsync(0)
    expect(uploads()).toHaveLength(1)
  })

  it('keeps a manual capture even when the picture is unchanged', async () => {
    const shots = start()
    await shots.captureNow('change')
    await shots.captureNow('manual')
    await vi.advanceTimersByTimeAsync(0)
    expect(uploads()).toHaveLength(2)
  })

  it('captures nothing while paused', async () => {
    const shots = start()
    shots.pause()
    await shots.captureNow('file', 'docs/a.md')
    await vi.advanceTimersByTimeAsync(0)
    expect(uploads()).toHaveLength(0)
  })

  it('drops a frame that finished decoding after the meeting ended', async () => {
    let release: ((value: ReturnType<typeof sample>) => void) | null = null
    const encode = vi.fn(async () => new Blob(['x']))
    _setShotGrabberForTest(() => new Promise((resolve) => (release = resolve)))

    const shots = start()
    const pending = shots.captureNow('file', 'docs/a.md')
    shots.stop()
    release?.(sample('0'.repeat(64), encode))
    await pending
    await vi.advanceTimersByTimeAsync(0)
    expect(uploads()).toHaveLength(0)
    expect(encode).not.toHaveBeenCalled()
  })

  it('does not encode a frame the scheduler drops', async () => {
    const encode = vi.fn(async () => new Blob(['x']))
    _setShotGrabberForTest(async () => sample('0'.repeat(64), encode))

    const shots = start()
    await shots.captureNow('change')
    await vi.advanceTimersByTimeAsync(9_000)
    // Same picture past the interval: rejected by the hash, before encoding.
    await shots.captureNow('change')

    expect(encode).toHaveBeenCalledTimes(1)
    expect(uploads()).toHaveLength(1)
  })

  it('takes a frame 1.2s after a file opens, with the path as reference', async () => {
    startMeetingFileTracking({ token: 'tok' })
    const shots = start()
    noteMeetingFileOpened('docs/arch.md')

    await vi.advanceTimersByTimeAsync(1_000)
    expect(uploads()).toHaveLength(0)

    await vi.advanceTimersByTimeAsync(300)
    const sent = uploads()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({ reason: 'file', ref: 'docs/arch.md' })
    expect(shots.count.value).toBe(1)
  })

  it('does not capture a file that was closed before the delay elapsed', async () => {
    startMeetingFileTracking({ token: 'tok' })
    start()
    noteMeetingFileOpened('docs/arch.md')
    // A visit under a second is a mis-click; the tracker drops it from the
    // timeline, so a frame would have nothing to sit next to.
    noteMeetingFileOpened('docs/other.md')
    await vi.advanceTimersByTimeAsync(1_500)
    const refs = uploads().map((u) => u.ref)
    expect(refs).not.toContain('docs/arch.md')
  })

  it('captures when a transcript line hits a cue', async () => {
    const shots = start()
    const notes = useLiveNotes()
    notes.segments.value = [
      { id: 1, channel: 'mic', speaker: '我', startedAt: Date.now() - 500, durationMs: 400, text: '大家看这个页面' },
    ]
    await nextTick()
    await vi.advanceTimersByTimeAsync(0)

    const sent = uploads()
    expect(sent).toHaveLength(1)
    expect(sent[0]).toMatchObject({ reason: 'speech', ref: '1' })
    expect(shots.count.value).toBe(1)
  })

  it('does not backfill frames for lines transcribed before the toggle', async () => {
    const notes = useLiveNotes()
    // Already in the transcript when the host turns 关键画面 on: it was not on
    // screen for this pipeline, and re-scanning it would fire a retroactive shot.
    notes.segments.value = [
      { id: 7, channel: 'mic', speaker: '我', startedAt: Date.now() - 60_000, durationMs: 1_000, text: '大家看这个页面' },
    ]
    const shots = start()
    await vi.advanceTimersByTimeAsync(5_000)
    expect(uploads()).toHaveLength(0)
    expect(shots.count.value).toBe(0)
  })
})
