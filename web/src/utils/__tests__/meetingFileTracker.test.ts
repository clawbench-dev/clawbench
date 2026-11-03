import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  flushMeetingFiles,
  meetingFileEntries,
  noteMeetingFileClosed,
  noteMeetingFileOpened,
  startMeetingFileTracking,
  stopMeetingFileTracking,
  _meetingFileSnapshotForTest,
  _resetMeetingFileTrackerForTest,
} from '@/utils/meetingFileTracker'

interface Call {
  token: string
  files: Array<Record<string, unknown>>
}

let calls: Call[] = []
let failNext = false

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-30T14:30:00Z'))
  calls = []
  failNext = false
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url: string, init: { body: string }) => {
      if (failNext) {
        failNext = false
        return { ok: false, status: 500 }
      }
      calls.push(JSON.parse(init.body) as Call)
      return { ok: true, status: 200 }
    }),
  )
  _resetMeetingFileTrackerForTest()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('meetingFileTracker', () => {
  it('records an interval and flushes it when the meeting stops', async () => {
    const t0 = Date.now()
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('docs/a.md')
    vi.advanceTimersByTime(60_000)
    noteMeetingFileClosed()
    await stopMeetingFileTracking()

    expect(calls).toHaveLength(1)
    expect(calls[0].token).toBe('tok')
    expect(calls[0].files).toEqual([
      {
        owner: 'host',
        ownerName: '',
        path: 'docs/a.md',
        openedAt: t0,
        closedAt: t0 + 60_000,
        source: 'preview',
      },
    ])
  })

  it('closes the previous file when another one is opened', async () => {
    const t0 = Date.now()
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('a.md')
    vi.advanceTimersByTime(5000)
    noteMeetingFileOpened('b.md')

    const snap = _meetingFileSnapshotForTest()
    expect(snap.pending).toHaveLength(1)
    expect(snap.pending[0]).toMatchObject({ path: 'a.md', openedAt: t0, closedAt: t0 + 5000 })
    expect(snap.open).toMatchObject({ path: 'b.md', closedAt: 0 })

    vi.advanceTimersByTime(2000)
    await stopMeetingFileTracking()
    expect(calls[0].files).toHaveLength(2)
  })

  it('drops a visit shorter than a second', async () => {
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('a.md')
    vi.advanceTimersByTime(500)
    noteMeetingFileClosed()

    expect(_meetingFileSnapshotForTest().pending).toHaveLength(0)
    expect(meetingFileEntries()).toHaveLength(0)
    await stopMeetingFileTracking()
    expect(calls).toHaveLength(0)
  })

  it('reopening the same file within the coalesce window extends the interval', async () => {
    const t0 = Date.now()
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('a.md')
    vi.advanceTimersByTime(5000)
    noteMeetingFileClosed()
    vi.advanceTimersByTime(10_000)
    noteMeetingFileOpened('a.md')

    const snap = _meetingFileSnapshotForTest()
    expect(snap.pending).toHaveLength(1)
    expect(snap.pending[0]).toMatchObject({ path: 'a.md', openedAt: t0, closedAt: 0 })
  })

  it('keeps the batch when a flush fails, and retries it', async () => {
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('a.md')
    // Stay below the 5s auto-flush so this test owns every request.
    vi.advanceTimersByTime(2000)
    noteMeetingFileClosed()

    failNext = true
    await flushMeetingFiles()
    expect(calls).toHaveLength(0)
    expect(_meetingFileSnapshotForTest().pending).toHaveLength(1)

    await flushMeetingFiles()
    expect(calls).toHaveLength(1)
    expect(_meetingFileSnapshotForTest().pending).toHaveLength(0)
  })

  it('flushes on its own timer so a crash loses at most one interval', async () => {
    startMeetingFileTracking({ token: 'tok' })
    noteMeetingFileOpened('a.md')
    vi.advanceTimersByTime(4000)
    noteMeetingFileOpened('b.md')

    await vi.advanceTimersByTimeAsync(5000)
    expect(calls.length).toBeGreaterThanOrEqual(1)
    expect(calls[0].files[0]).toMatchObject({ path: 'a.md' })
  })

  it('does nothing outside a meeting', async () => {
    noteMeetingFileOpened('a.md')
    noteMeetingFileClosed()
    await flushMeetingFiles()
    expect(calls).toHaveLength(0)
    expect(meetingFileEntries()).toHaveLength(0)
  })
})
