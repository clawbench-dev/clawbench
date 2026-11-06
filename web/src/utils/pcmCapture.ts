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

export interface PcmStreamerOptions {
  /** Microphone to capture from. The streamer does NOT stop its tracks. */
  stream: MediaStream
  tuning: VoiceTuning
  /** Called for every frame that passed the voice gate. */
  onFrame: (frame: ArrayBuffer) => void
  /**
   * Called for every frame with its RMS, gate or no gate. Free-dialog mode uses
   * this to notice "the user stopped talking"; dictation does not need it.
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
    node.port.onmessage = (e: MessageEvent<ArrayBuffer>) => {
      if (this.stopped) return
      const buf = e.data
      if (buf.byteLength < 2) {
        // "Cannot tell" must never mean "discard".
        this.opts.onFrame(buf)
        return
      }
      const rms = rmsFromInt16(new Int16Array(buf))
      this.opts.onLevel?.(rms)
      if (gate && rms < threshold) return
      this.opts.onFrame(buf)
    }
    tail.connect(node)
    // The worklet emits silence, but the graph only runs while it reaches the
    // destination.
    node.connect(ctx.destination)
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
