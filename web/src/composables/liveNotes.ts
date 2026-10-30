/**
 * AI 听记 capture: one recorder per audio channel.
 *
 * A live share is P2P — the server never sees the media — so transcription has
 * to start here, on the publisher. That is also the only place where the
 * channels are still separate: system audio, the publisher's microphone and
 * each viewer's microphone are three (or N) distinct tracks. Recording them
 * separately is what gives every transcript line a speaker without a
 * diarisation model.
 *
 * Segments are cut by restarting the recorder every `segmentMs`. A
 * `timeslice` MediaRecorder would be cheaper, but only the first slice of a
 * WebM stream carries the container header, so slices 2..N are not decodable
 * on their own. Restarting produces one complete, standalone file per segment
 * at the cost of a few milliseconds of audio at each boundary.
 *
 * No Vue, no DOM events beyond MediaRecorder: the reactive wrapper lives in
 * useLiveNotes.ts so this part is testable with fakes.
 */
import { appLog } from '@/utils/appLog'

const TAG = 'LiveNotes'

/**
 * Hard ceiling for one uploaded segment.
 *
 * Segments are normally cut at a pause (see SILENCE_* below); this only bounds
 * someone who talks without stopping. 20s is also within Whisper's native
 * window, so a forced cut still transcribes well.
 */
export const NOTES_SEGMENT_MS = 20_000

/**
 * Minimum segment length before a pause may end it.
 *
 * Cutting on every micro-pause would produce hundreds of two-word segments —
 * each one a separate STT round-trip with no sentence context, which is worse
 * than the cut we are avoiding.
 */
export const NOTES_MIN_SEGMENT_MS = 4_000

/** How long a pause must last before it counts as the end of a sentence. */
export const NOTES_SILENCE_HOLD_MS = 450

/**
 * RMS below which a 100ms window counts as silence (≈ -40 dBFS).
 *
 * Speech in a normal microphone or system-audio capture sits well above this;
 * room tone and codec noise sit below it.
 */
export const NOTES_SILENCE_RMS = 0.01

/** How often the level is sampled while a segment is recording. */
const NOTES_VAD_INTERVAL_MS = 100

/**
 * Hard memory guard, NOT a backpressure policy.
 *
 * Losing a segment loses part of the meeting forever, so the queue is allowed
 * to grow as far as the transcription backend is behind. The only thing this
 * cap prevents is an unbounded leak when the backend is down for a very long
 * time: 180 × 10-20s ≈ 30-60 minutes of audio per channel. Hitting it is
 * reported to the UI as "transcript incomplete" — silently dropping is exactly
 * what this feature must not do.
 */
export const NOTES_MAX_PENDING = 180

/** Retry backoff for one segment. A transient network blip must not leave a
 *  hole in the transcript; after these attempts the segment is counted as lost
 *  and the UI says so. */
export const NOTES_RETRY_DELAYS_MS = [1_000, 3_000, 9_000]

export const NOTES_MIME = 'audio/webm;codecs=opus'

/** 32 kbps mono Opus is transparent for speech and keeps a 10s segment ~40 KB,
 *  which keeps both the upload and the STT round-trip short. */
export const NOTES_AUDIO_BITRATE = 32_000

/** One transcribed line, as returned by the server. */
export interface LiveNoteSegment {
  id: number
  channel: string
  speaker: string
  startedAt: number
  durationMs: number
  text: string
}

/** One rendered line: consecutive segments from the same speaker merged. */
export interface NoteBlock {
  key: string
  channel: string
  speaker: string
  startedAt: number
  text: string
}

/** Merges incoming segments into the timeline, de-duplicated by id. */
export function mergeNoteSegments(
  existing: LiveNoteSegment[],
  incoming: LiveNoteSegment[],
): LiveNoteSegment[] {
  const byId = new Map<number, LiveNoteSegment>()
  for (const s of existing) byId.set(s.id, s)
  for (const s of incoming) {
    if (s.id > 0) byId.set(s.id, s)
  }
  return [...byId.values()].sort((a, b) => a.startedAt - b.startedAt || a.id - b.id)
}

/** Gap under which two utterances by the same speaker render as one line. */
const BLOCK_MERGE_GAP_MS = 4_000

/**
 * Groups the timeline into speaker turns for display.
 *
 * The channels are recorded independently, so one sentence can arrive as two
 * segments; merging consecutive same-speaker lines keeps the panel readable
 * without hiding who said what.
 */
export function groupNoteSegments(list: LiveNoteSegment[]): NoteBlock[] {
  const out: NoteBlock[] = []
  let lastEnd = 0
  for (const s of list) {
    if (!s.text.trim()) continue
    const last = out[out.length - 1]
    if (last && last.channel === s.channel && s.startedAt - lastEnd <= BLOCK_MERGE_GAP_MS) {
      last.text = `${last.text} ${s.text}`.trim()
    } else {
      out.push({
        key: `n${s.id}`,
        channel: s.channel,
        speaker: s.speaker,
        startedAt: s.startedAt,
        text: s.text,
      })
    }
    lastEnd = s.startedAt + Math.max(0, s.durationMs)
  }
  return out
}

export interface NotesUploadArgs {
  channel: string
  speaker: string
  startedAt: number
  durationMs: number
  blob: Blob
}

/** Uploads one segment and resolves to the stored line (null when silent). */
export type NotesUploader = (args: NotesUploadArgs) => Promise<LiveNoteSegment | null>

export interface LiveNotesCaptureOptions {
  /** Hard ceiling for one segment (a pause normally ends it earlier). */
  segmentMs?: number
  /** A pause may end a segment only after this long. */
  minSegmentMs?: number
  maxPending?: number
  mimeType?: string
  /** Called when a segment is lost after exhausting its retries. */
  onSegmentLost?: (channel: string, reason: string) => void
  /** Called when the memory guard drops a segment (backend down for ~30 min). */
  onOverflow?: (channel: string) => void
}

interface QueuedSegment {
  blob: Blob
  startedAt: number
  durationMs: number
}

/**
 * Records one channel as a series of independently decodable segments.
 *
 * Segments end at a PAUSE, not on a fixed clock: a cut in the middle of a word
 * makes Whisper drop or invent the boundary word, and a meeting has hundreds of
 * boundaries. A hard ceiling still applies so a monologue cannot grow without
 * bound (and 20s stays inside Whisper's native window).
 */
class ChannelRecorder {
  private readonly source: MediaStreamAudioSourceNode
  private readonly dest: MediaStreamAudioDestinationNode
  private readonly analyser: AnalyserNode | null
  private readonly segmentMs: number
  private readonly minSegmentMs: number
  private readonly maxPending: number
  private readonly mimeType: string

  private recorder: MediaRecorder | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private vadTimer: ReturnType<typeof setInterval> | null = null
  private stopped = false
  private flushWaiters: Array<() => void> = []

  private queue: QueuedSegment[] = []
  private pumping = false
  private waiters: Array<() => void> = []
  private readonly opts: LiveNotesCaptureOptions

  constructor(
    ctx: AudioContext,
    stream: MediaStream,
    private readonly channel: string,
    private readonly speaker: string,
    private readonly upload: NotesUploader,
    opts: LiveNotesCaptureOptions,
  ) {
    this.opts = opts
    this.segmentMs = opts.segmentMs ?? NOTES_SEGMENT_MS
    this.minSegmentMs = Math.min(opts.minSegmentMs ?? NOTES_MIN_SEGMENT_MS, this.segmentMs)
    this.maxPending = opts.maxPending ?? NOTES_MAX_PENDING
    this.mimeType = opts.mimeType ?? NOTES_MIME
    // A per-channel destination is what makes the channel separation real: the
    // recorder only ever sees this one source.
    this.source = ctx.createMediaStreamSource(stream)
    this.dest = ctx.createMediaStreamDestination()
    this.source.connect(this.dest)

    // Level metering for pause detection. The analyser is pulled through a
    // muted gain so it keeps being processed without being heard (the
    // publisher's speakers are already fed by the viewer monitor edge).
    this.analyser = null
    if (typeof ctx.createAnalyser === 'function' && typeof ctx.createGain === 'function') {
      const analyser = ctx.createAnalyser()
      analyser.fftSize = 512
      const mute = ctx.createGain()
      mute.gain.value = 0
      this.source.connect(analyser)
      analyser.connect(mute)
      mute.connect(ctx.destination)
      this.analyser = analyser
    }
  }

  /** Segments still waiting to be uploaded (the in-flight one included). */
  get pending(): number {
    return this.queue.length + (this.pumping ? 1 : 0)
  }

  start(): void {
    if (this.stopped || this.recorder) return
    this.recordOnce()
  }

  stop(): void {
    if (this.stopped) return
    this.stopped = true
    this.stopVad()
    if (this.timer !== null) {
      clearTimeout(this.timer)
      this.timer = null
    }
    try {
      if (this.recorder && this.recorder.state !== 'inactive') this.recorder.stop()
    } catch {
      /* already stopped */
    }
    try {
      this.source.disconnect()
      this.dest.disconnect()
    } catch {
      /* already disconnected */
    }
  }

  /** Resolves when the final segment has been handed to the uploader (or the
   *  timeout). Callers that are about to tear down the AudioContext must wait
   *  for this: `onstop` is what produces the last blob. */
  async flushed(timeoutMs = 2000): Promise<void> {
    if (!this.recorder) return
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, timeoutMs)
      this.flushWaiters.push(() => {
        clearTimeout(timer)
        resolve()
      })
    })
  }

  /** Resolves when every queued segment has been uploaded (or the timeout). */
  async idle(timeoutMs: number): Promise<void> {
    if (!this.pumping && this.queue.length === 0) return
    await new Promise<void>((resolve) => {
      const timer = setTimeout(resolve, timeoutMs)
      this.waiters.push(() => {
        clearTimeout(timer)
        resolve()
      })
    })
  }

  private recordOnce(): void {
    if (this.stopped) return
    const chunks: BlobPart[] = []
    const startedAt = Date.now()
    let rec: MediaRecorder
    try {
      rec = new MediaRecorder(this.dest.stream, {
        mimeType: this.mimeType,
        audioBitsPerSecond: NOTES_AUDIO_BITRATE,
      })
    } catch (e) {
      // Older engines without Opus/WebM: fall back to the default codec rather
      // than losing the whole feature.
      appLog.w(TAG, 'requested mime unsupported, using default', e)
      rec = new MediaRecorder(this.dest.stream)
    }
    this.recorder = rec
    // Without a meter (a context that cannot create an analyser) nothing is
    // discarded: not knowing whether a segment has speech must fail towards
    // keeping it.
    let speechSeen = this.analyser === null
    let vadSamples = 0
    let lastVoiceAt = startedAt
    const cut = () => {
      try {
        if (rec.state !== 'inactive') rec.stop()
      } catch {
        /* already stopped */
      }
    }
    rec.ondataavailable = (ev: BlobEvent) => {
      if (ev.data && ev.data.size > 0) chunks.push(ev.data)
    }
    rec.onstop = () => {
      this.recorder = null
      this.stopVad()
      if (this.timer !== null) {
        clearTimeout(this.timer)
        this.timer = null
      }
      const durationMs = Date.now() - startedAt
      // A segment with no speech is pure overhead: one STT round-trip that
      // returns nothing. Discarding it loses no content — unlike dropping a
      // segment that does contain speech.
      // A segment that was never metered (shorter than one sample, or no
      // analyser at all) is kept: "unknown" must not mean "silent".
      const silent = vadSamples > 0 && !speechSeen
      if (chunks.length > 0 && !silent) {
        this.enqueue({
          blob: new Blob(chunks, { type: rec.mimeType || this.mimeType }),
          startedAt,
          durationMs,
        })
      }
      // Chain the next segment from onstop: starting a second recorder before
      // the first has flushed would overlap two writers on the same stream.
      if (!this.stopped) this.recordOnce()
      const waiters = this.flushWaiters
      this.flushWaiters = []
      for (const resolve of waiters) resolve()
    }
    rec.start()

    // Pause-aware cut: once the minimum length is reached, a long enough
    // silence ends the segment, so sentences stay whole.
    this.vadTimer = this.startVad((rms) => {
      vadSamples += 1
      const now = Date.now()
      if (rms >= NOTES_SILENCE_RMS) {
        speechSeen = true
        lastVoiceAt = now
      }
      if (now - startedAt >= this.segmentMs) {
        cut()
        return
      }
      if (now - startedAt >= this.minSegmentMs && now - lastVoiceAt >= NOTES_SILENCE_HOLD_MS) {
        cut()
      }
    })

    // Safety net for a context that cannot meter (or a channel that never
    // pauses): the segment still ends, it just ends mid-sentence.
    this.timer = setTimeout(() => {
      this.timer = null
      cut()
    }, this.segmentMs)
  }

  /** Starts level sampling; returns a stop function. */
  private startVad(onLevel: (rms: number) => void): ReturnType<typeof setInterval> | null {
    if (!this.analyser) return null
    const buf = new Uint8Array(this.analyser.fftSize)
    return setInterval(() => {
      if (!this.analyser) return
      this.analyser.getByteTimeDomainData(buf)
      let sum = 0
      for (let i = 0; i < buf.length; i++) {
        const v = (buf[i] - 128) / 128
        sum += v * v
      }
      onLevel(Math.sqrt(sum / buf.length))
    }, NOTES_VAD_INTERVAL_MS)
  }

  private stopVad(): void {
    if (this.vadTimer !== null) {
      clearInterval(this.vadTimer)
      this.vadTimer = null
    }
  }

  private enqueue(seg: QueuedSegment): void {
    // The queue exists to absorb a backend that is slower than real time. It is
    // NOT a place to throw audio away: every segment carries part of the
    // meeting, and a missing segment is a permanent hole in the transcript.
    // Captions are allowed to lag; they are not allowed to cost content.
    if (this.queue.length >= this.maxPending) {
      // Only reachable when the backend has been unreachable for ~30 minutes
      // per channel. Reported to the UI rather than hidden.
      appLog.e(TAG, 'segment queue overflow, audio lost', this.channel)
      this.opts.onOverflow?.(this.channel)
      return
    }
    this.queue.push(seg)
    void this.pump()
  }

  private async pump(): Promise<void> {
    if (this.pumping) return
    this.pumping = true
    while (this.queue.length > 0) {
      const seg = this.queue.shift() as QueuedSegment
      await this.uploadWithRetry(seg)
    }
    this.pumping = false
    const waiters = this.waiters
    this.waiters = []
    for (const resolve of waiters) resolve()
  }

  /**
   * Uploads one segment, retrying transient failures with backoff.
   *
   * A network blip must not punch a hole in the transcript, so the same segment
   * is retried instead of being skipped. Only after every attempt fails is it
   * counted as lost — and the UI is told, because a silently incomplete
   * transcript is worse than a visibly incomplete one.
   */
  private async uploadWithRetry(seg: QueuedSegment): Promise<void> {
    const args = {
      channel: this.channel,
      speaker: this.speaker,
      startedAt: seg.startedAt,
      durationMs: seg.durationMs,
      blob: seg.blob,
    }
    for (let attempt = 0; ; attempt++) {
      try {
        await this.upload(args)
        return
      } catch (e) {
        if (attempt >= NOTES_RETRY_DELAYS_MS.length) {
          appLog.e(TAG, 'segment lost after retries', this.channel, e)
          this.opts.onSegmentLost?.(this.channel, e instanceof Error ? e.message : String(e))
          return
        }
        appLog.w(TAG, 'segment upload failed, retrying', this.channel, attempt + 1, e)
        await new Promise<void>((resolve) => {
          setTimeout(resolve, NOTES_RETRY_DELAYS_MS[attempt])
        })
      }
    }
  }
}

/** Owns one ChannelRecorder per audio source. */
export class LiveNotesCapture {
  private readonly channels = new Map<string, ChannelRecorder>()
  /** Recorders that are no longer live but may still have uploads queued. */
  private readonly retired: ChannelRecorder[] = []
  private stopped = false

  constructor(
    private readonly ctx: AudioContext,
    private readonly upload: NotesUploader,
    private readonly opts: LiveNotesCaptureOptions = {},
  ) {}

  get size(): number {
    return this.channels.size
  }

  /** Segments recorded but not yet transcribed, across every channel. */
  get pending(): number {
    let n = 0
    for (const rec of this.channels.values()) n += rec.pending
    for (const rec of this.retired) n += rec.pending
    return n
  }

  /** Adds (or replaces) a channel. A null/empty stream is ignored: a muted
   *  microphone is normal and must not create a silent recorder. */
  addChannel(channel: string, speaker: string, stream: MediaStream | null): void {
    if (this.stopped || !stream || stream.getAudioTracks().length === 0) return
    this.removeChannel(channel)
    const rec = new ChannelRecorder(this.ctx, stream, channel, speaker, this.upload, this.opts)
    this.channels.set(channel, rec)
    rec.start()
  }

  removeChannel(channel: string): void {
    const rec = this.channels.get(channel)
    if (!rec) return
    rec.stop()
    this.channels.delete(channel)
    // Keep it so the final drain still waits for the segment that was in
    // flight when the viewer left.
    this.retired.push(rec)
  }

  /** Stops every recorder; in-flight segments are still uploaded. */
  stop(): void {
    if (this.stopped) return
    this.stopped = true
    for (const rec of this.channels.values()) {
      rec.stop()
      this.retired.push(rec)
    }
    this.channels.clear()
  }

  /** Stops everything, then waits for the final blobs to be produced. Does NOT
   *  wait for uploads — callers close the AudioContext right after. */
  async stopAndFlush(timeoutMs = 2000): Promise<void> {
    this.stop()
    await Promise.all(this.retired.map((rec) => rec.flushed(timeoutMs)))
  }

  /**
   * Waits for the upload queues to drain (bounded by `timeoutMs`).
   *
   * Returns whether everything was transcribed. `false` means the caller is
   * about to summarise a transcript that is still missing its tail, which the
   * UI must say out loud.
   */
  async drainUploads(timeoutMs = 20_000): Promise<boolean> {
    await Promise.all(this.retired.map((rec) => rec.idle(timeoutMs)))
    const drained = this.pending === 0
    this.retired.length = 0
    return drained
  }
}
