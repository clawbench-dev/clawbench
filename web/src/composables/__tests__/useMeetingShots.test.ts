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

function frame(hash = '0'.repeat(64)) {
  return { blob: new Blob(['x']), width: 1280, height: 720, hash }
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

describe('useMeetingShots', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200 })))
    _resetMeetingShotsForTest()
    _resetLiveNotesForTest()
    _resetMeetingFileTrackerForTest()
    _setShotGrabberForTest(async () => frame())
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
    let release: ((value: ReturnType<typeof frame>) => void) | null = null
    _setShotGrabberForTest(() => new Promise((resolve) => (release = resolve)))

    const shots = start()
    const pending = shots.captureNow('file', 'docs/a.md')
    shots.stop()
    release?.(frame())
    await pending
    await vi.advanceTimersByTimeAsync(0)
    expect(uploads()).toHaveLength(0)
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
})
