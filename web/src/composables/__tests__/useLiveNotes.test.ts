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
    /** Mirrors the real capture: what the status bar reads while recording. */
    lastVoiceAt = 0
    dead: string[] = []

    deadChannels(): string[] {
      return this.dead
    }

    constructor(ctx: unknown, upload: (args: unknown) => Promise<unknown>, opts: Record<string, unknown>) {
      this.ctx = ctx
      this.upload = upload
      this.opts = opts
      instances.push(this)
    }

    addChannel(channel: string, speaker: () => string, stream: unknown): void {
      // The real capture resolves the label per uploaded segment; the fake
      // resolves it here so an assertion reads as "what will the lines say".
      this.channels.push({ channel, speaker: speaker(), stream })
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
  // The capture is faked, so nothing here constructs a real MediaRecorder —
  // but `start()` checks the capability before it does anything at all, and
  // jsdom has no MediaRecorder. Without this every recording test would be
  // exercising the "engine cannot record" branch.
  vi.stubGlobal('MediaRecorder', class {})
})

afterEach(() => {
  _resetLiveNotesForTest()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useLiveNotes — recording', () => {
  it('records the microphone only — system audio is not transcribed', () => {
    const notes = useLiveNotes()
    const mic = {} as MediaStream

    notes.start({ token: 'tok', ctx, micStream: mic, label: (channel) => `L:${channel}` })

    const cap = lastCapture()
    expect(notes.phase.value).toBe('recording')
    // One channel: the system loopback would duplicate every remote voice into
    // a second line and transcribe whatever else the machine is playing.
    expect(cap.channels).toEqual([{ channel: 'mic', speaker: 'L:mic', stream: mic }])
  })

  /**
   * 听记 is built on MediaRecorder, which some engines (the WebKit build
   * Playwright ships, some embedded WebViews) do not implement. Recording must
   * then simply not start — the meeting itself is not this module's business,
   * and `new MediaRecorder` throwing out of `start()` used to take the whole
   * room down with a raw ReferenceError.
   */
  it('records nothing and stays idle when the engine has no MediaRecorder', () => {
    vi.stubGlobal('MediaRecorder', undefined)
    const notes = useLiveNotes()

    expect(() => notes.start({ token: 'tok', ctx, micStream: {} as MediaStream })).not.toThrow()

    expect(h.instances).toHaveLength(0)
    expect(notes.phase.value).toBe('idle')
  })

  it('falls back to the raw channel name when no labeller is given', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })

    expect(lastCapture().channels.map((c) => c.speaker)).toEqual(['mic'])
  })

  // Putting a headset on mid-meeting re-opens the microphone; the transcript
  // must keep ONE "mic" stream of segments, not start a second channel.
  it('replaces the microphone channel when the input device changes', () => {
    const notes = useLiveNotes()
    notes.setMicStream({} as MediaStream)
    expect(h.instances).toHaveLength(0)

    const first = {} as MediaStream
    notes.start({ token: 'tok', ctx, micStream: first, label: (c) => `L:${c}` })
    const cap = lastCapture()

    const second = {} as MediaStream
    notes.setMicStream(second)

    expect(cap.channels).toEqual([
      { channel: 'mic', speaker: 'L:mic', stream: first },
      { channel: 'mic', speaker: 'L:mic', stream: second },
    ])
  })

  it('adds and removes viewer channels, ignoring empty ids and calls before start', () => {
    const notes = useLiveNotes()
    notes.addViewer('v1', {} as MediaStream)
    expect(h.instances).toHaveLength(0)

    notes.start({ token: 'tok', ctx, micStream: null })
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

    notes.start({ token: 'tok', ctx, micStream: null })
    await notes.stopCapture()
    expect(lastCapture().flushTimeout).toBe(2000)
  })

  /**
   * 听记 can be switched off for a moment and back on. The transcript of the
   * first half is the meeting's transcript — wiping it made the pane look like
   * nothing had ever been recorded — and the members already in the room must
   * be transcribed again (only a NEW join calls addViewer).
   */
  it('keeps the transcript and re-attaches the members when switched back on', async () => {
    const notes = useLiveNotes()
    const member = {} as MediaStream
    notes.start({ token: 'tok', ctx, micStream: null, label: (c) => `L:${c}` })
    notes.addViewer('v1', member)
    notes.segments.value = [
      { id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '一' },
      { id: 2, channel: 'viewer:v1', speaker: 'L:viewer:v1', startedAt: 2, durationMs: 1, text: '二' },
      { id: 3, channel: 'mic', speaker: '我', startedAt: 3, durationMs: 1, text: '三' },
    ]
    const before = lastCapture()

    await notes.stopCapture()
    await notes.finishUploads(false)
    expect(notes.phase.value).toBe('idle')

    notes.start({ token: 'tok', ctx, micStream: null, label: (c) => `L:${c}`, resume: true })

    expect(notes.phase.value).toBe('recording')
    expect(notes.segments.value.map((s) => s.text)).toEqual(['一', '二', '三'])
    const after = lastCapture()
    expect(after).not.toBe(before)
    expect(after.channels.map((c) => c.channel)).toEqual(['mic', 'viewer:v1'])
  })

  it('starts clean for another meeting even when a resume is asked for', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok-a', ctx, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '上一场' }]

    notes.start({ token: 'tok-b', ctx, micStream: null, resume: true })

    // The module is a singleton: a different meeting must not inherit the
    // previous one's transcript, whatever the caller believed.
    expect(notes.segments.value).toEqual([])
  })

  it('remembers a member who joined while the recorder was off', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    await notes.stopCapture()
    await notes.finishUploads(false)

    // 听记 is off: the stream still has to be kept, or switching it back on
    // would leave this member silent for the rest of the meeting.
    const member = {} as MediaStream
    notes.addViewer('v9', member)
    expect(h.instances).toHaveLength(1)

    notes.start({ token: 'tok', ctx, micStream: null, resume: true })
    expect(lastCapture().channels).toContainEqual({ channel: 'viewer:v9', speaker: 'viewer:v9', stream: member })
  })
})

describe('useLiveNotes — finishing', () => {
  it('merges the recording before asking for the minutes', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '内容' }]
    fetchMock.mockResolvedValue(
      jsonResponse({ ok: true, artifacts: { dir: '/p/.clawbench/notes/x', audioMeeting: '/p/.clawbench/notes/x/audio/meeting.webm' } }),
    )

    await notes.finishUploads()

    // Order matters: the merge is only correct once nothing is still uploading,
    // and the minutes link to the recording it produces.
    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls).toEqual(['/api/live-share/notes/audio/merge', '/api/live-share/notes/minutes'])
    expect(notes.artifacts.value.audioMeeting).toBe('/p/.clawbench/notes/x/audio/meeting.webm')
  })

  it('merges the recording but skips the minutes when auto-generation is off', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null, autoMinutes: false })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '内容' }]
    fetchMock.mockResolvedValue(
      jsonResponse({ ok: true, artifacts: { dir: '/p/.clawbench/notes/x', audioMeeting: '/p/.clawbench/notes/x/audio/meeting.webm' } }),
    )

    await notes.finishUploads()

    // The merge still has to happen: it is the source for "重新识别转写".
    // Only the automatic summary request is skipped.
    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls).toEqual(['/api/live-share/notes/audio/merge'])
    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('drains without merging OR summarizing when the recording is only paused', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '内容' }]
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.finishUploads(false)

    // Turning 听记 off mid-meeting must not fold the audio: whatever is
    // recorded after the toggle would end up as loose segments beside a merged
    // file that no longer covers the meeting.
    //
    // It must not summarize either. The minutes request used to fire here, from
    // the segments collected so far, while the meeting kept going — and nothing
    // regenerated them at the end, so the record covered only the first half.
    expect(fetchMock).not.toHaveBeenCalled()
    expect(lastCapture().drainCalls).toBe(1)
    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('folds and summarizes the whole meeting once it really ends, after a pause', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    notes.segments.value = [
      { id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: '前半段' },
      { id: 2, channel: 'mic', speaker: '我', startedAt: 2, durationMs: 1, text: '后半段' },
    ]
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.finishUploads(false)
    expect(fetchMock).not.toHaveBeenCalled()

    // The meeting ends with the recorder already gone: no capture, nothing to
    // drain, but the transcript is the meeting's and the minutes must cover it.
    await notes.finishUploads()

    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls).toEqual(['/api/live-share/notes/audio/merge', '/api/live-share/notes/minutes'])
    expect(urls.filter((u) => u.endsWith('/minutes'))).toHaveLength(1)
  })

  it('uploads the mix track as archive-only so it is never transcribed', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    const archive = lastCapture().opts.archiveUpload as (args: unknown) => Promise<unknown>
    expect(typeof archive).toBe('function')

    fetchMock.mockResolvedValue(jsonResponse({ id: 0, text: '' }))
    await archive({ channel: 'mix', speaker: '', startedAt: 1, durationMs: 1, blob: new Blob(['x']) })

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/live-share/notes/segment')
    const form = init.body as FormData
    expect(form.get('channel')).toBe('mix')
    expect(form.get('archive')).toBe('1')
  })

  it('drains the backlog, then asks for the minutes', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
    lastCapture().drainResult = false

    await notes.finishUploads()

    expect(notes.truncated.value).toBe(true)
    // The tail is missing, but what did land is still merged into one file.
    // The minutes are NOT requested: summarizing a transcript with a hole in it
    // would present the gap as the meeting's end.
    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls).toEqual(['/api/live-share/notes/audio/merge'])
  })

  it('does nothing when there is no capture', async () => {
    const notes = useLiveNotes()
    await expect(notes.finishUploads()).resolves.toBeUndefined()
    expect(notes.phase.value).toBe('idle')
  })

  it('finish() stops the recorders before draining', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok-a', ctx, micStream: null })
    notes.segments.value = [{ id: 1, channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, text: 'A 的内容' }]

    const capA = lastCapture()
    let releaseDrain: (v: boolean) => void = () => {}
    capA.drainUploads = () => new Promise<boolean>((resolve) => { releaseDrain = resolve })

    const finishing = notes.finishUploads()
    expect(notes.phase.value).toBe('finalizing')

    // The user closes the dialog (reset) and immediately starts another meeting.
    notes.reset()
    notes.start({ token: 'tok-b', ctx, micStream: null })
    expect(notes.phase.value).toBe('recording')

    releaseDrain(true)
    await finishing

    expect(notes.phase.value).toBe('recording', "A's drain must not reset B's phase")
    expect(fetchMock).not.toHaveBeenCalled(), 'A must not request minutes for B'
  })

  it('a poll response that arrives after reset is dropped', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok-a', ctx, micStream: null })

    fetchMock
      .mockResolvedValueOnce(jsonResponse({ status: 'generating' }))
      .mockResolvedValueOnce(jsonResponse({ minutes: { status: 'ready', markdown: '## A 的纪要' } }))

    await notes.generateMinutes()
    notes.reset()
    notes.start({ token: 'tok-b', ctx, micStream: null })

    await vi.advanceTimersByTimeAsync(2000)

    expect(notes.minutes.value).toBe('')
    expect(notes.minutesPhase.value).toBe('idle')
    expect(notes.phase.value).toBe('recording')
  })
})

describe('useLiveNotes — artifacts', () => {
  it('sends the meeting title with every segment so the folder can be named', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', title: '季度会', ctx, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ id: 1, text: '内容' }))

    // The uploader is what the capture calls for each finished segment.
    await lastCapture().upload({
      channel: 'mic',
      speaker: '我',
      startedAt: 1,
      durationMs: 2,
      blob: new Blob(['x']),
    })

    const form = (fetchMock.mock.calls[0]?.[1] as RequestInit).body as FormData
    expect(form.get('title')).toBe('季度会')
  })

  it('passes the shared voice gate and sensitivity into the capture', async () => {
    const notes = useLiveNotes()
    notes.start({
      token: 'tok',
      ctx,
      micStream: null,
      voiceGate: false,
      sensitivity: 'aggressive',
    })
    // Shared with chat dictation, so one settings change covers both — the
    // capture must receive the resolved values rather than reading settings
    // itself (liveNotes.ts stays free of the settings module).
    expect(lastCapture().opts.voiceGate).toBe(false)
    expect(lastCapture().opts.sensitivity).toBe('aggressive')
  })

  it('adopts the proofreading status and the files the meeting left behind', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(
      jsonResponse({
        status: 'ready',
        markdown: '## 概要',
        filePath: '/p/notes/m/minutes.md',
        revise: { status: 'ready' },
        artifacts: {
          dir: '/p/notes/m',
          minutes: '/p/notes/m/minutes.md',
          transcript: '/p/notes/m/transcript.vtt',
          revised: '/p/notes/m/transcript-revised.md',
          audio: '/p/notes/m/audio',
          audioFiles: 12,
        },
      }),
    )

    await notes.generateMinutes()

    expect(notes.revisePhase.value).toBe('ready')
    expect(notes.artifacts.value.transcript).toBe('/p/notes/m/transcript.vtt')
    expect(notes.artifacts.value.audioFiles).toBe(12)
  })

  it('reports a failed proofread without hiding the minutes', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(
      jsonResponse({
        status: 'ready',
        markdown: '## 概要',
        revise: { status: 'failed', error: 'model down' },
        artifacts: { dir: '/p/notes/m', minutes: '/p/notes/m/minutes.md' },
      }),
    )

    await notes.generateMinutes()

    // The summary exists — it was generated from the raw transcript — but the
    // UI has to say the proofreading did not happen.
    expect(notes.minutesPhase.value).toBe('ready')
    expect(notes.minutes.value).toBe('## 概要')
    expect(notes.revisePhase.value).toBe('failed')
    expect(notes.reviseError.value).toBe('model down')
    // No revised file: the server reports only what it wrote.
    expect(notes.artifacts.value.revised).toBeUndefined()
  })

  it('clears the artifacts when a new meeting starts', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(
      jsonResponse({
        status: 'ready',
        markdown: '## 概要',
        revise: { status: 'ready' },
        artifacts: { dir: '/p/notes/m', transcript: '/p/notes/m/transcript.vtt' },
      }),
    )
    await notes.generateMinutes()
    expect(notes.artifacts.value.dir).toBe('/p/notes/m')

    notes.start({ token: 'tok2', ctx, micStream: null })

    expect(notes.artifacts.value).toEqual({})
    expect(notes.revisePhase.value).toBe('idle')
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
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ status: 'idle' }))

    await notes.generateMinutes()

    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('surfaces a failed request instead of leaving the UI generating', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({}, false, 500))

    await notes.generateMinutes()

    expect(notes.minutesPhase.value).toBe('failed')
    expect(notes.error.value).toContain('500')
  })

  it('polls until the minutes are ready', async () => {
    vi.useFakeTimers()
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })

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
    notes.start({ token: 'tok', ctx, micStream: null })

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
    notes.start({ token: 'tok', ctx, micStream: null })

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
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({ minutes: { status: 'generating' } }))

    await notes.generateMinutes()
    await vi.advanceTimersByTimeAsync(2000 * 92)

    expect(notes.minutesPhase.value).toBe('failed')
    expect(notes.error.value).toContain('timed out')
  })

  it('regenerate clears the previous error', async () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
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

    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockRejectedValue(new Error('offline'))
    await expect(notes.refresh()).resolves.toBeUndefined()
    expect(notes.minutesPhase.value).toBe('idle')
  })

  it('reset clears every observable field', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
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
    notes.start({ token: 'tok', ctx, micStream: null })
    fetchMock.mockResolvedValue(jsonResponse({}, false, 502))

    await expect(
      lastCapture().upload({ channel: 'mic', speaker: '我', startedAt: 1, durationMs: 1, blob: new Blob(['x']) }),
    ).rejects.toThrow('502')
  })

  it('counts lost segments reported by the recorder', () => {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: null })
    const opts = lastCapture().opts as { onSegmentLost: () => void; onOverflow: () => void }

    opts.onSegmentLost()
    opts.onOverflow()

    expect(notes.lost.value).toBe(2)
  })
})

/**
 * The status bar's input.
 *
 * Every case here is a state the panel used to be unable to show: the pane said
 * "listening" whether the recorder was hearing a room, a dead headset or an
 * audio graph the system had suspended.
 */
describe('useLiveNotes — live status', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  function startRecording() {
    const notes = useLiveNotes()
    notes.start({ token: 'tok', ctx, micStream: {} as MediaStream })
    return notes
  }

  it('is plain "ok" while recording', () => {
    const notes = startRecording()
    expect(notes.health.value).toBe('ok')
    expect(notes.silentForMs.value).toBe(0)
  })

  it('reports a channel whose device disappeared', () => {
    const notes = startRecording()
    lastCapture().dead = ['mic']

    vi.advanceTimersByTime(1_000)

    expect(notes.health.value).toBe('micDead')
  })

  it('reports the audio graph being suspended', () => {
    const notes = startRecording()
    notes.setAudioSuspended(true)
    expect(notes.health.value).toBe('suspended')
  })

  it('reports the connection to the server', () => {
    const notes = startRecording()
    notes.setConnectionState('reconnecting')
    expect(notes.health.value).toBe('reconnecting')
    notes.setConnectionState('offline')
    expect(notes.health.value).toBe('offline')
  })

  it('calls out a microphone that has been silent past the threshold', () => {
    const notes = startRecording()

    // Just under two minutes: still a normal pause.
    vi.advanceTimersByTime(119_000)
    expect(notes.health.value).toBe('ok')

    vi.advanceTimersByTime(2_000)
    expect(notes.health.value).toBe('silent')
    expect(notes.silentForMs.value).toBeGreaterThanOrEqual(120_000)
  })

  it('stops counting silence once a line is heard again', () => {
    const notes = startRecording()
    vi.advanceTimersByTime(130_000)
    expect(notes.health.value).toBe('silent')

    lastCapture().lastVoiceAt = Date.now()
    vi.advanceTimersByTime(1_000)

    expect(notes.health.value).toBe('ok')
  })

  it('reports a transcription backlog', () => {
    const notes = startRecording()
    lastCapture().pending = 5

    vi.advanceTimersByTime(1_000)

    expect(notes.health.value).toBe('queued')
    expect(notes.pending.value).toBe(5)
  })

  /**
   * Priority matters, not just coverage: a dead microphone also produces no
   * speech and no backlog, so "silent" would be true and useless. The cause
   * has to win over the symptom.
   */
  it('names the cause, not the symptom, when several apply', () => {
    const notes = startRecording()
    vi.advanceTimersByTime(130_000)
    lastCapture().dead = ['mic']
    notes.setAudioSuspended(true)
    notes.setConnectionState('offline')

    vi.advanceTimersByTime(1_000)

    expect(notes.health.value).toBe('micDead')
  })

  it('says nothing about health once the recording stops', async () => {
    const notes = startRecording()
    lastCapture().dead = ['mic']
    vi.advanceTimersByTime(1_000)
    expect(notes.health.value).toBe('micDead')

    await notes.stopCapture()

    expect(notes.health.value).toBe('ok')
  })
})
