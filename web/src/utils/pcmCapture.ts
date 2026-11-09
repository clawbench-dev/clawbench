/**
 * Microphone → 16 kHz mono Int16 PCM, with the shared preprocessing applied.
 *
 * Two consumers need exactly this and nothing more:
 *   - chat dictation (`useVoiceInput`), which forwards frames to the streaming
 *     STT socket;
 *   - free-dialog mode (`useFreeDialog`), which additionally keeps the stream
 *     open across turns and watches the level.
 *
 * Keeping it in one place is what guarantees the band filter and the voice gate
 * cannot drift between them — the failure mode would be "dictation hears me but
 * free mode does not", which is indistinguishable from a device problem.
 *
 * No Vue, no settings access: the caller passes a resolved tuning object.
 */
import { buildBandFilter, rmsFromInt16, rmsThreshold, type VoiceTuning } from '@/utils/audioTuning'

/** PCM rate the streaming engine is told to expect. */
export const PCM_SAMPLE_RATE = 16000

/**
 * AudioWorklet processor: mixes to mono, resamples to `targetRate` with a box
 * filter and posts 100 ms Int16 frames.
 *
 * Kept as source text rather than a separate module so no bundler asset wiring
 * (and no CSP surprise) is involved: it is loaded from a Blob URL.
 */
const PCM_WORKLET_SOURCE = `
class ClawbenchPcm extends AudioWorkletProcessor {
  constructor(options) {
    super()
    const target = (options.processorOptions && options.processorOptions.targetRate) || 16000
    this.ratio = sampleRate / target
    this.acc = 0
    this.count = 0
    this.pos = 0
    this.out = []
    this.frameSize = Math.round(target / 10)
  }
  process(inputs) {
    const input = inputs[0]
    if (!input || !input[0]) return true
    const ch = input[0]
    for (let i = 0; i < ch.length; i++) {
      this.acc += ch[i]
      this.count++
      this.pos += 1
      if (this.pos >= this.ratio) {
        const v = this.acc / this.count
        this.out.push(Math.max(-32768, Math.min(32767, Math.round(v < 0 ? v * 0x8000 : v * 0x7fff))))
        this.acc = 0
        this.count = 0
        this.pos -= this.ratio
      }
    }
    if (this.out.length >= this.frameSize) {
      const pcm = Int16Array.from(this.out)
      this.out = []
      this.port.postMessage(pcm.buffer, [pcm.buffer])
    }
    return true
  }
}
registerProcessor('clawbench-pcm', ClawbenchPcm)
`

/**
 * How long the gate may withhold audio before one frame is forwarded anyway.
 *
 * Free-dialog mode keeps one recogniser session open for minutes. Sending
 * nothing at all for that long is what gets an idle stream closed by the
 * server, and a closed stream silently ends the mode. One 100 ms frame every
 * few seconds keeps the session alive at roughly 1% of the cost of streaming
 * silence continuously.
 */
const KEEPALIVE_MS = 2000

/**
 * How long the gate stays open after the last loud frame.
 *
 * Word endings are quieter than word middles: "…好的" trails off well below the
 * threshold. Cutting the moment the level drops clips the tail of the last
 * syllable, and a recogniser with a clipped ending guesses.
 *
 * The span itself comes from the tuning, because it has a second job: it has to
 * outlast the engine's own end window (800 ms for the streaming engines) so the
 * engine closes the sentence while the user is still listening, instead of
 * leaving the last characters stuck in a revision until the send flushes it.
 */
const HANGOVER_FALLBACK_MS = 600

/**
 * How much gated-out audio is kept to prepend when speech starts.
 *
 * The gate can only react to a frame that is ALREADY loud, so without this the
 * first syllable is always lost — "帮我看下" arrives as "看下". Three frames is
 * 300 ms, enough to cover the attack of a word.
 */
const PREROLL_FRAMES = 3

export interface PcmStreamerOptions {
  /** Microphone to capture from. The streamer does NOT stop its tracks. */
  stream: MediaStream
  tuning: VoiceTuning
  /** Called for every frame that passed the voice gate. */
  onFrame: (frame: ArrayBuffer) => void
  /**
   * Called for every frame with its RMS, gate or no gate.
   *
   * Not a silence detector: room tone never reaches zero, so anything that
   * restarts a "user stopped talking" timer from this will restart it forever.
   * Free-dialog mode measures its pause from recognition output instead.
   */
  onLevel?: (rms: number) => void
}

/**
 * Owns one capture graph: source → band filter → worklet → destination.
 *
 * `start()` resolves once the worklet is loaded and the graph is running, so a
 * caller that opens a socket first can be sure no frame is produced before it
 * is ready to send.
 */
export class PcmStreamer {
  private ctx: AudioContext | null = null
  private node: AudioWorkletNode | null = null
  private source: MediaStreamAudioSourceNode | null = null
  private filterTail: AudioNode | null = null
  private stopped = false
  /** When a frame was last handed to the sink (epoch ms). */
  private lastSentAt = 0
  /** Gate stays open until this time (epoch ms) — see `HANGOVER_FALLBACK_MS`. */
  private gateOpenUntil = 0
  /** Frames withheld while the gate was shut, oldest first — see PREROLL_FRAMES. */
  private preroll: ArrayBuffer[] = []

  constructor(private readonly opts: PcmStreamerOptions) {}

  async start(): Promise<void> {
    if (this.stopped) return
    const ctx = new AudioContext({ sampleRate: PCM_SAMPLE_RATE })
    this.ctx = ctx
    // A context created outside a user gesture starts suspended and would feed
    // nothing to the worklet.
    if (ctx.state === 'suspended') await ctx.resume()
    const url = URL.createObjectURL(new Blob([PCM_WORKLET_SOURCE], { type: 'application/javascript' }))
    try {
      await ctx.audioWorklet.addModule(url)
    } finally {
      URL.revokeObjectURL(url)
    }
    if (this.stopped) {
      void ctx.close().catch(() => { /* already closed */ })
      return
    }

    // Seed the keepalive clock: starting at 0 would make the very first silent
    // frame look like it had been withheld for a full interval and leak it
    // through.
    this.lastSentAt = Date.now()
    this.gateOpenUntil = 0
    this.preroll = []
    const source = ctx.createMediaStreamSource(this.opts.stream)
    let tail: AudioNode = source
    if (this.opts.tuning.bandFilter) {
      try {
        tail = buildBandFilter(ctx, source)
      } catch {
        // A graph that cannot build a biquad must not take the capture down —
        // the microphone still works, it is just unfiltered.
        tail = source
      }
    }
    this.source = source
    this.filterTail = tail === source ? null : tail

    const node = new AudioWorkletNode(ctx, 'clawbench-pcm', {
      numberOfInputs: 1,
      numberOfOutputs: 1,
      channelCount: 1,
      processorOptions: { targetRate: PCM_SAMPLE_RATE },
    })
    this.node = node
    const threshold = rmsThreshold(this.opts.tuning)
    const gate = this.opts.tuning.voiceGate
    const hangoverMs = this.opts.tuning.hangoverMs || HANGOVER_FALLBACK_MS
    node.port.onmessage = (e: MessageEvent<ArrayBuffer>) => {
      if (this.stopped) return
      const buf = e.data
      if (buf.byteLength < 2) {
        // "Cannot tell" must never mean "discard".
        this.lastSentAt = Date.now()
        this.opts.onFrame(buf)
        return
      }
      const rms = rmsFromInt16(new Int16Array(buf))
      this.opts.onLevel?.(rms)
      const now = Date.now()
      if (gate && rms >= threshold) {
        // Speech (re)starts: hand over the frames that led into it, then this
        // one, and hold the gate open so the tail is not clipped either.
        for (const pending of this.preroll) this.opts.onFrame(pending)
        this.preroll = []
        this.gateOpenUntil = now + hangoverMs
      } else if (gate && now <= this.gateOpenUntil) {
        // Inside the hangover window: keep sending, this is the end of a word.
      } else if (gate && now - this.lastSentAt < KEEPALIVE_MS) {
        // Shut, and not yet time for the keepalive: hold the frame in case the
        // next one is speech (see PREROLL_FRAMES).
        this.preroll.push(buf)
        if (this.preroll.length > PREROLL_FRAMES) this.preroll.shift()
        return
      }
      this.lastSentAt = now
      this.opts.onFrame(buf)
    }
    tail.connect(node)
    // The worklet emits silence, but the graph only runs while it reaches the
    // destination.
    node.connect(ctx.destination)
  }

  /**
   * Drops audio withheld while the gate was shut, and re-opens the keepalive
   * clock.
   *
   * The gate holds the last few frames so the attack of a word is not lost.
   * When the consumer changes — dictation → interrupt listener → dictation, as
   * free-dialog mode swaps who reads the same microphone — those withheld
   * frames belong to whatever was playing before. Flushing them into the new
   * consumer would hand the assistant's tail to the recogniser as if the user
   * had said it.
   */
  resetGate(): void {
    this.preroll = []
    this.gateOpenUntil = 0
    this.lastSentAt = Date.now()
  }

  /** Tears the graph down. Safe to call more than once, and before `start()`. */
  stop(): void {
    this.stopped = true
    if (this.node) {
      this.node.port.onmessage = null
      try { this.node.disconnect() } catch { /* already disconnected */ }
      this.node = null
    }
    if (this.filterTail) {
      try { this.filterTail.disconnect() } catch { /* already disconnected */ }
      this.filterTail = null
    }
    if (this.source) {
      try { this.source.disconnect() } catch { /* already disconnected */ }
      this.source = null
    }
    if (this.ctx) {
      void this.ctx.close().catch(() => { /* already closed */ })
      this.ctx = null
    }
  }
}
