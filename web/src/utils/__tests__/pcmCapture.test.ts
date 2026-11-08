import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { PcmStreamer } from '../pcmCapture'
import { VOICE_TUNING_DEFAULTS } from '../audioTuning'

/**
 * The capture graph is built out of Web Audio nodes, which jsdom does not
 * implement. The fakes below record the wiring so the tests can assert on the
 * shape of the graph — that is the part with behaviour (band filter applied or
 * skipped, frames gated by level, teardown complete) — while the resampling
 * itself stays inside the worklet and is not re-implemented here.
 */

interface FakeNode {
  type?: string
  frequency: { value: number }
  Q: { value: number }
  connect: ReturnType<typeof vi.fn>
  disconnect: ReturnType<typeof vi.fn>
}

let createdFilters: FakeNode[] = []
/** Set to make the fake context refuse to build a biquad (degradation path). */
let failBiquad = false
let workletNode: { port: { onmessage: ((e: { data: ArrayBuffer }) => void) | null }; connect: ReturnType<typeof vi.fn>; disconnect: ReturnType<typeof vi.fn> }
let audioCtx: { state: string; sampleRate: number; destination: unknown; audioWorklet: { addModule: ReturnType<typeof vi.fn> }; createMediaStreamSource: ReturnType<typeof vi.fn>; createBiquadFilter: ReturnType<typeof vi.fn>; resume: ReturnType<typeof vi.fn>; close: ReturnType<typeof vi.fn> }

function installAudioFakes() {
  createdFilters = []
  failBiquad = false
  workletNode = { port: { onmessage: null }, connect: vi.fn(), disconnect: vi.fn() }
  const source = { connect: vi.fn(), disconnect: vi.fn() }

  vi.stubGlobal('AudioWorkletNode', class {
    port = workletNode.port
    connect = workletNode.connect
    disconnect = workletNode.disconnect
  })
  vi.stubGlobal('AudioContext', class {
    state = 'running'
    sampleRate = 16000
    destination = {}
    audioWorklet = { addModule: vi.fn().mockResolvedValue(undefined) }
    createMediaStreamSource = vi.fn(() => source)
    createBiquadFilter = vi.fn(() => {
      if (failBiquad) throw new Error('no biquad')
      const f: FakeNode = {
        frequency: { value: 0 },
        Q: { value: 0 },
        connect: vi.fn(),
        disconnect: vi.fn(),
      }
      createdFilters.push(f)
      return f
    })
    resume = vi.fn().mockResolvedValue(undefined)
    close = vi.fn().mockResolvedValue(undefined)
  })
  vi.stubGlobal('URL', Object.assign(URL, {
    createObjectURL: vi.fn(() => 'blob:worklet'),
    revokeObjectURL: vi.fn(),
  }))
  audioCtx = (globalThis as unknown as { AudioContext: { prototype: typeof audioCtx } }).AudioContext
    .prototype as unknown as typeof audioCtx
}

/** A 100 ms frame at the given amplitude, in Int16. */
function frameAt(amplitude: number): ArrayBuffer {
  const arr = new Int16Array(160)
  arr.fill(amplitude)
  return arr.buffer
}

const stream = { getTracks: () => [], getAudioTracks: () => [] } as unknown as MediaStream

describe('PcmStreamer', () => {
  beforeEach(() => {
    vi.resetModules()
    installAudioFakes()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('forwards a loud frame to the sink', async () => {
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(8000) })
    expect(onFrame).toHaveBeenCalledTimes(1)
    s.stop()
  })

  it('drops a silent frame when the voice gate is on', async () => {
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(0) })
    expect(onFrame).not.toHaveBeenCalled()
    s.stop()
  })

  it('forwards a silent frame when the voice gate is off', async () => {
    const onFrame = vi.fn()
    const s = new PcmStreamer({
      stream,
      tuning: { ...VOICE_TUNING_DEFAULTS, voiceGate: false },
      onFrame,
    })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(0) })
    expect(onFrame).toHaveBeenCalledTimes(1)
    s.stop()
  })

  it('reports the level for every frame, gated or not', async () => {
    // Free-dialog mode uses the level to restart its pause timer, so a silent
    // frame must still be reported even though it is not forwarded.
    const onLevel = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame: vi.fn(), onLevel })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(0) })
    workletNode.port.onmessage!({ data: frameAt(8000) })
    expect(onLevel).toHaveBeenCalledTimes(2)
    expect(onLevel.mock.calls[0][0]).toBe(0)
    expect(onLevel.mock.calls[1][0]).toBeGreaterThan(0.2)
    s.stop()
  })

  it('honours the sensitivity setting when deciding what is speech', async () => {
    // ~0.0046 RMS: above the conservative floor, below the aggressive one.
    const onFrame = vi.fn()
    const s = new PcmStreamer({
      stream,
      tuning: { ...VOICE_TUNING_DEFAULTS, sensitivity: 'aggressive' },
      onFrame,
    })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(150) })
    expect(onFrame).not.toHaveBeenCalled()
    s.stop()

    const onFrame2 = vi.fn()
    const s2 = new PcmStreamer({
      stream,
      tuning: { ...VOICE_TUNING_DEFAULTS, sensitivity: 'conservative' },
      onFrame: onFrame2,
    })
    await s2.start()
    workletNode.port.onmessage!({ data: frameAt(150) })
    expect(onFrame2).toHaveBeenCalledTimes(1)
    s2.stop()
  })

  it('sends the frames that led into the first loud one', async () => {
    // The gate can only react to a frame that is already loud, so without a
    // pre-roll the first syllable is always lost ("帮我看下" arrives as "看下").
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(0) })
    workletNode.port.onmessage!({ data: frameAt(0) })
    workletNode.port.onmessage!({ data: frameAt(0) })
    expect(onFrame).not.toHaveBeenCalled()
    workletNode.port.onmessage!({ data: frameAt(8000) })
    // Three withheld frames plus the loud one.
    expect(onFrame).toHaveBeenCalledTimes(4)
    s.stop()
  })

  it('keeps sending for a moment after the level drops', async () => {
    // Word endings trail off well below the threshold; cutting immediately
    // clips the last syllable.
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(8000) })
    workletNode.port.onmessage!({ data: frameAt(0) })
    // Second frame is inside the hangover window.
    expect(onFrame).toHaveBeenCalledTimes(2)
    s.stop()
  })

  it('does not grow the pre-roll buffer without bound', async () => {
    // A long silence must not accumulate every withheld frame in memory.
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    for (let i = 0; i < 50; i++) workletNode.port.onmessage!({ data: frameAt(0) })
    workletNode.port.onmessage!({ data: frameAt(8000) })
    // At most PREROLL_FRAMES (3) plus the loud one — not 51.
    expect(onFrame.mock.calls.length).toBeLessThanOrEqual(4)
    s.stop()
  })

  it('drops withheld frames when the consumer changes', async () => {
    // Free-dialog mode swaps who reads the same microphone between phases. The
    // frames withheld before the swap belong to whatever was playing then —
    // flushing them into the next recogniser would hand it the assistant's
    // tail as if the user had said it.
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: frameAt(0) })
    workletNode.port.onmessage!({ data: frameAt(0) })
    s.resetGate()
    workletNode.port.onmessage!({ data: frameAt(8000) })
    // Only the loud frame: the two withheld ones were dropped by the reset.
    expect(onFrame).toHaveBeenCalledTimes(1)
    s.stop()
  })

  it('builds a high-pass and a low-pass when the band filter is on', async () => {
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame: vi.fn() })
    await s.start()
    expect(createdFilters.map((f) => f.type)).toEqual(['highpass', 'lowpass'])
    expect(createdFilters[0].frequency.value).toBe(100)
    s.stop()
  })

  it('clamps the low-pass below Nyquist', async () => {
    // At 16 kHz the Nyquist limit is 8000, so the corner must sit below it —
    // setting it AT Nyquist is a no-op or an error depending on the engine.
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame: vi.fn() })
    await s.start()
    expect(createdFilters[1].frequency.value).toBeLessThan(8000)
    s.stop()
  })

  it('skips the filters entirely when the band filter is off', async () => {
    const s = new PcmStreamer({
      stream,
      tuning: { ...VOICE_TUNING_DEFAULTS, bandFilter: false },
      onFrame: vi.fn(),
    })
    await s.start()
    expect(createdFilters).toHaveLength(0)
    s.stop()
  })

  it('keeps capturing when the filter graph cannot be built', async () => {
    // A context whose filter cannot be built must degrade to an unfiltered
    // graph, not take the whole capture down.
    failBiquad = true
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await expect(s.start()).resolves.toBeUndefined()
    expect(createdFilters).toHaveLength(0)
    // The graph still runs: a loud frame reaches the sink.
    workletNode.port.onmessage!({ data: frameAt(8000) })
    expect(onFrame).toHaveBeenCalledTimes(1)
    s.stop()
    failBiquad = false
  })

  it('stops delivering frames after stop()', async () => {
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    s.stop()
    // The worklet's handler is detached, so a late frame cannot reach the sink.
    expect(workletNode.port.onmessage).toBeNull()
    expect(onFrame).not.toHaveBeenCalled()
  })

  it('can be stopped more than once', async () => {
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame: vi.fn() })
    await s.start()
    expect(() => { s.stop(); s.stop() }).not.toThrow()
  })

  it('can be stopped before it ever started', () => {
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame: vi.fn() })
    expect(() => s.stop()).not.toThrow()
  })

  it('does not build a graph when it was stopped while the worklet loaded', async () => {
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    const started = s.start()
    s.stop()
    await started
    expect(workletNode.connect).not.toHaveBeenCalled()
  })

  it('treats an unreadable frame as speech rather than dropping it', async () => {
    // "Cannot tell" must never mean "discard", or a broken meter would swallow
    // everything the user says.
    const onFrame = vi.fn()
    const s = new PcmStreamer({ stream, tuning: VOICE_TUNING_DEFAULTS, onFrame })
    await s.start()
    workletNode.port.onmessage!({ data: new ArrayBuffer(1) })
    expect(onFrame).toHaveBeenCalledTimes(1)
    s.stop()
  })
})
