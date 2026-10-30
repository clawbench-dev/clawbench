/**
 * Publisher-side AI 听记 state machine.
 *
 * The recorder itself is replaced by a fake: what matters here is the
 * orchestration — which channels get registered, when the upload backlog is
 * drained, how the minutes status is polled, and that a failed request is
 * surfaced instead of leaving the UI spinning.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const h = vi.hoisted(() => {
  interface FakeCapture {
    ctx: unknown
    upload: (args: unknown) => Promise<unknown>
    opts: Record<string, unknown>
    channels: Array<{ channel: string; speaker: string; stream: unknown }>
    removed: string[]
    pending: number
    drainResult: boolean
    drainCalls: number
    flushTimeout: number | null
    stopAndFlush: (ms: number) => Promise<void>
    drainUploads: (ms: number) => Promise<boolean>
  }

  const instances: FakeCapture[] = []

  class FakeLiveNotesCapture implements FakeCapture {
    ctx: unknown
    upload: (args: unknown) => Promise<unknown>
    opts: Record<string, unknown>
    channels: Array<{ channel: string; speaker: string; stream: unknown }> = []
    removed: string[] = []
    pending = 0
    drainResult = true
    drainCalls = 0
    flushTimeout: number | null = null

    constructor(ctx: unknown, upload: (args: unknown) => Promise<unknown>, opts: Record<string, unknown>) {
      this.ctx = ctx
      this.upload = upload
      this.opts = opts
      instances.push(this)
    }

    addChannel(channel: string, speaker: string, stream: unknown): void {
      this.channels.push({ channel, speaker, stream })
    }

    removeChannel(channel: string): void {
      this.removed.push(channel)
    }

    async stopAndFlush(ms: number): Promise<void> {
      this.flushTimeout = ms
    }

    async drainUploads(): Promise<boolean> {
      this.drainCalls += 1
      return this.drainResult
    }
  }

  return { instances, FakeLiveNotesCapture }
})

vi.mock('@/composables/liveNotes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/liveNotes')>()
  return { ...actual, LiveNotesCapture: h.FakeLiveNotesCapture }
})

import { useLiveNotes, _resetLiveNotesForTest } from '../useLiveNotes'

const ctx = {} as AudioContext

function jsonResponse(body: unknown, ok = true, status = 200): Response {
  return {
    ok,
    status,
    json: async () => body,
  } as unknown as Response
}

function lastCapture() {
  const inst = h.instances[h.instances.length - 1]
  if (!inst) throw new Error('no capture was created')
  return inst
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  h.instances.length = 0
  _resetLiveNotesForTest()
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  _resetLiveNotesForTest()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useLiveNotes — recording', () => {
  it('registers one channel per source and reports recording', () => {
    const notes = useLiveNotes()
    const system = {} as MediaStream
    const mic = {} as MediaStream

    notes.start({
      token: 'tok',
      ctx,
      systemStream: system,
      micStream: mic,
      label: (channel) => `L:${channel}`,
    })

    const cap = lastCapture()
    expect(notes.phase.value).toBe('recording')
    expect(cap.channels).toEqual([
      { channel: 'system', speaker: 'L:system', stream: system },
      { channel: 'mic', speaker: 'L:mic', stream: mic },
    ])
  })

  it('falls back to the raw channel name when no labeller is given', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })

    expect(lastCapture().channels.map((c) => c.speaker)).toEqual(['system', 'mic'])
  })

  it('adds and removes viewer channels, ignoring empty ids and calls before start', () => {
    const notes = useLiveNotes()
    notes.addViewer('v1', {} as MediaStream)
    expect(h.instances).toHaveLength(0)

    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    const cap = lastCapture()

    notes.addViewer('v1', {} as MediaStream)
    notes.addViewer('', {} as MediaStream)
    notes.removeViewer('v1')

    expect(cap.channels.map((c) => c.channel)).toContain('viewer:v1')
    expect(cap.removed).toEqual(['viewer:v1'])
  })

  it('stopCapture waits for the final blobs and is a no-op without a capture', async () => {
    const notes = useLiveNotes()
    await expect(notes.stopCapture()).resolves.toBeUndefined()

    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    await notes.stopCapture()
    expect(lastCapture().flushTimeout).toBe(2000)
  })
})

describe('useLiveNotes — finishing', () => {
  it('drains the backlog, then asks for the minutes', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    // Pretend a segment already landed through the upload response.
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '内容' }]
    fetchMock.mockResolvedValue(jsonResponse({ status: 'generating' }))

    await notes.finishUploads()

    const cap = lastCapture()
    expect(cap.drainCalls).toBe(1)
    expect(notes.phase.value).toBe('idle')
    expect(notes.pending.value).toBe(0)
    expect(fetchMock).toHaveBeenCalledWith('/api/live-share/notes/minutes', expect.objectContaining({ method: 'POST' }))
  })

  it('flags a truncated transcript when the drain times out', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    lastCapture().drainResult = false

    await notes.finishUploads()

    expect(notes.truncated.value).toBe(true)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('does nothing when there is no capture', async () => {
    const notes = useLiveNotes()
    await expect(notes.finishUploads()).resolves.toBeUndefined()
    expect(notes.phase.value).toBe('idle')
  })

  it('finish() stops the recorders before draining', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.finish()

    const cap = lastCapture()
    expect(cap.flushTimeout).toBe(2000)
    expect(cap.drainCalls).toBe(1)
  })
})

describe('useLiveNotes — session isolation', () => {
  // The state is a module-level singleton: closing the dialog and starting a new
  // meeting must not let the previous meeting's background drain write into it.
  it('a drain finishing after a new session started leaves the new one alone', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok-a', ctx, systemStream: null, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: 'A 的内容' }]

    const capA = lastCapture()
    let releaseDrain: (v: boolean) => void = () => {}
    capA.drainUploads = () => new Promise<boolean>((resolve) => { releaseDrain = resolve })

    const finishing = notes.finishUploads()
    expect(notes.phase.value).toBe('finalizing')

    // The user closes the dialog (reset) and immediately starts another meeting.
    notes.reset()
    notes.start({ token: 'tok-b', ctx, systemStream: null, micStream: null })
    expect(notes.phase.value).toBe('recording')

    releaseDrain(true)
    await finishing

    expect(notes.phase.value).toBe('recording', "A's drain must not reset B's phase")
    expect(fetchMock).not.toHaveBeenCalled(), 'A must not request minutes for B'
  })

  it('a poll response that arrives after reset is dropped', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok-a', ctx, systemStream: null, micStream: null })

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'generating' }))
      .mockResolvedValueOnce(jsonResponse({ minutes: { status: 'ready', markdown: '## A 的纪要' } }))

    await notes.generateMinutes()
    notes.reset()
    notes.start({ token: 'tok-b', ctx, systemStream: null, micStream: null })

    await vi.advanceTimersByTimeAsync(2000)

    expect(notes.minutes.value).toBe('')
    expect(notes.minutesPhase.value).toBe('idle')
    expect(notes.phase.value).toBe('recording')
  })
})

describe('useLiveNotes — minutes', () => {
  it('is a no-op without a session token', async () => {
    const notes = useLiveNotes()
    await notes.generateMinutes()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('adopts minutes returned inline', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(
      jsonResponse({ status: 'ready', markdown: '## 概要', filePath: '/p/notes.md' }),
    )

    await notes.generateMinutes()

    expect(notes.minutesPhase.value).toBe('ready')
    expect(notes.minutes.value).toBe('## 概要')
    expect(notes.minutesFile.value).toBe('/p/notes.md')
  })

  it('stays idle when nothing was transcribed', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.generateMinutes()

    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('surfaces a failed request instead of leaving the UI generating', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({}, false, 500))

    await notes.generateMinutes()

    expect(notes.minutesPhase.value).toBe('failed')
    expect(notes.error.value).toContain('500')
  })

  it('polls until the minutes are ready', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'generating' }))
      .mockResolvedValueOnce(jsonResponse({ minutes: { status: 'generating' } }))
      .mockResolvedValueOnce(
        jsonResponse({
          segments: [{ id: 7, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '后到的' }],
          minutes: { status: 'ready', markdown: '## 概要', filePath: '/p/x.md' },
        }),
      )

    await notes.generateMinutes()
    expect(notes.minutesPhase.value).toBe('generating')

    await vi.advanceTimersByTimeAsync(2000)
    expect(notes.minutesPhase.value).toBe('generating')

    await vi.advanceTimersByTimeAsync(2000)
    expect(notes.minutesPhase.value).toBe('ready')
    expect(notes.minutes.value).toBe('## 概要')
    expect(notes.minutesFile.value).toBe('/p/x.md')
    expect(notes.segments.value.map((s) => s.id)).toEqual([7])
  })

  it('records the server-side failure reason', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'generating' }))
      .mockResolvedValueOnce(jsonResponse({ minutes: { status: 'failed', error: 'model down' } }))

    await notes.generateMinutes()
    await vi.advanceTimersByTimeAsync(2000)

    expect(notes.minutesPhase.value).toBe('failed')
    expect(notes.error.value).toBe('model down')
  })

  it('keeps polling after a transport error', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'generating' }))
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce(jsonResponse({ minutes: { status: 'ready', markdown: 'ok' } }))

    await notes.generateMinutes()
    await vi.advanceTimersByTimeAsync(2000)
    await vi.advanceTimersByTimeAsync(2000)

    expect(notes.minutesPhase.value).toBe('ready')
    expect(notes.minutes.value).toBe('ok')
  })

  it('gives up after too many attempts', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ minutes: { status: 'generating' } }))

    await notes.generateMinutes()
    await vi.advanceTimersByTimeAsync(2000 * 92)

    expect(notes.minutesPhase.value).toBe('failed')
    expect(notes.error.value).toContain('timed out')
  })

  it('regenerate clears the previous error', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    notes.error.value = 'old'
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.generateMinutes(true)

    expect(notes.error.value).toBe('')
    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.regenerate).toBe(true)
  })
})

describe('useLiveNotes — refresh and reset', () => {
  it('refresh merges what was missed and adopts the server status', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '本地' }]
    fetchMock.mockResolvedValue(
      jsonResponse({
        segments: [{ id: 2, channel: 'system', speaker: '系统', startedAt: 2, durationMs: 1, text: '远端' }],
        minutes: { status: 'ready', markdown: '## 纪要', filePath: '/p/y.md' },
      }),
    )

    await notes.refresh()

    expect(notes.segments.value.map((s) => s.text)).toEqual(['本地', '远端'])
    expect(notes.minutesPhase.value).toBe('ready')
    expect(notes.minutes.value).toBe('## 纪要')
    expect(notes.minutesFile.value).toBe('/p/y.md')
  })

  it('refresh ignores a failing request and an empty session', async () => {
    const notes = useLiveNotes()
    await expect(notes.refresh()).resolves.toBeUndefined()
    expect(fetchMock).not.toHaveBeenCalled()

    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockRejectedValue(new Error('offline'))
    await expect(notes.refresh()).resolves.toBeUndefined()
    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('reset clears every observable field', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: 'x' }]
    notes.minutes.value = '## x'
    notes.minutesFile.value = '/p/x.md'
    notes.minutesPhase.value = 'ready'
    notes.error.value = 'boom'
    notes.pending.value = 3
    notes.lost.value = 2
    notes.truncated.value = true

    notes.reset()

    expect(notes.phase.value).toBe('idle')
    expect(notes.segments.value).toEqual([])
    expect(notes.minutes.value).toBe('')
    expect(notes.minutesFile.value).toBe('')
    expect(notes.minutesPhase.value).toBe('idle')
    expect(notes.error.value).toBe('')
    expect(notes.pending.value).toBe(0)
    expect(notes.lost.value).toBe(0)
    expect(notes.truncated.value).toBe(false)
  })
})

describe('useLiveNotes — upload callback', () => {
  it('merges a transcribed segment into the timeline', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(
      jsonResponse({
        id: 5,
        channel: 'mic',
        speaker: '我',
        startedAt: 1000,
        durationMs: 8000,
        text: '  你好  ',
      }),
    )

    const seg = await lastCapture().upload({
      channel: 'mic',
      speaker: '我',
      startedAt: 1000,
      durationMs: 8000,
      blob: new Blob(['x']),
    })

    expect(seg).toMatchObject({ id: 5, text: '  你好  ' })
    expect(notes.segments.value.map((s) => s.id)).toEqual([5])
  })

  it('returns null for silence so nothing is added to the timeline', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ id: 0, text: '' }))

    const seg = await lastCapture().upload({
      channel: 'mic',
      speaker: '我',
      startedAt: 1,
      durationMs: 1,
      blob: new Blob(['x']),
    })

    expect(seg).toBeNull()
    expect(notes.segments.value).toEqual([])
  })

  it('throws when the server rejects the segment', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({}, false, 502))

    await expect(
      lastCapture().upload({ channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, blob: new Blob(['x']) }),
    ).rejects.toThrow('502')
  })

  it('counts lost segments reported by the recorder', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, systemStream: null, micStream: null })
    const opts = lastCapture().opts as { onSegmentLost: () => void; onOverflow: () => void }

    opts.onSegmentLost()
    opts.onOverflow()

    expect(notes.lost.value).toBe(2)
  })
})
