import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { PcmJitterBuffer, startAndroidScreenCapture } from '../androidScreenCapture'

/**
 * Android screen capture: the bridge between a native MediaProjection session
 * and a MediaStream the rest of the sharing stack understands.
 *
 * The failure modes worth pinning are the ones that look like "it works" until
 * a real phone is used: a tick that never reaches the page, a frame that is
 * written after the share was stopped, audio that starts before it has been
 * prefilled (and therefore stutters forever), and a stop() that leaves the
 * native projection running.
 */

const bridge = vi.hoisted(() => ({
  startScreenCapture: vi.fn(() => '{"state":"requesting"}'),
  stopScreenCapture: vi.fn(),
  getScreenCaptureState: vi.fn(() => '{"state":"requesting"}'),
  takeScreenFrame: vi.fn(() => ''),
  takeSystemAudio: vi.fn(() => ''),
}))

vi.mock('@/utils/clawbenchNative', () => ({
  getNative: () => bridge,
}))

/** jsdom has neither MediaStream nor WebCodecs; only the shapes we touch matter. */
class FakeTrack {
  kind = 'video'
  stopped = false
  stop() {
    this.stopped = true
  }
}

class FakeMediaStream {
  tracks: FakeTrack[]
  constructor(tracks: FakeTrack[] = []) {
    this.tracks = tracks
  }
  addTrack(t: FakeTrack) {
    this.tracks.push(t)
  }
  getTracks() {
    return this.tracks
  }
  getAudioTracks() {
    return this.tracks.filter((t) => t.kind === 'audio')
  }
  getVideoTracks() {
    return this.tracks.filter((t) => t.kind === 'video')
  }
}

const writtenFrames: unknown[] = []

class FakeVideoFrame {
  constructor(
    public source: unknown,
    public init: { timestamp: number },
  ) {}
  close() {
    /* no-op */
  }
}

class FakeVideoTrackGenerator extends FakeTrack {
  kind = 'video'
  written: unknown[] = []
  writable = {
    getWriter: () => ({
      write: async (frame: unknown) => {
        this.written.push(frame)
        writtenFrames.push(frame)
      },
      close: async () => undefined,
    }),
  }
}

/** Interleaved little-endian 16-bit stereo from per-channel sample arrays. */
function pcm(left: number[], right: number[]): Uint8Array {
  const bytes = new Uint8Array(left.length * 4)
  for (let i = 0; i < left.length; i++) {
    const l = left[i] & 0xffff
    const r = right[i] & 0xffff
    bytes[i * 4] = l & 0xff
    bytes[i * 4 + 1] = (l >> 8) & 0xff
    bytes[i * 4 + 2] = r & 0xff
    bytes[i * 4 + 3] = (r >> 8) & 0xff
  }
  return bytes
}

function base64(bytes: Uint8Array): string {
  let s = ''
  for (const b of bytes) s += String.fromCharCode(b)
  return btoa(s)
}

const tick = (): void => {
  const fn = (window as unknown as Record<string, unknown>).__clawbenchScreenTick
  if (typeof fn === 'function') (fn as () => void)()
}

/** Let the tick's promise chain settle. */
const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('PcmJitterBuffer', () => {
  it('stays silent until the prefill is reached', () => {
    const buffer = new PcmJitterBuffer()
    // 0.1 s — below the 0.2 s prefill.
    buffer.push(pcm(new Array(4800).fill(1000), new Array(4800).fill(1000)))
    const left = new Float32Array(1024)
    const right = new Float32Array(1024)
    buffer.pull(left, right)
    expect(left.every((v) => v === 0)).toBe(true)
    expect(right.every((v) => v === 0)).toBe(true)
  })

  it('plays buffered audio once primed, scaling 16-bit to [-1, 1]', () => {
    const buffer = new PcmJitterBuffer()
    const samples = new Array(9600).fill(16384)
    buffer.push(pcm(samples, samples))
    const left = new Float32Array(256)
    const right = new Float32Array(256)
    buffer.pull(left, right)
    expect(left[0]).toBeCloseTo(0.5, 5)
    expect(right[0]).toBeCloseTo(0.5, 5)
  })

  it('drops the oldest audio when the backlog exceeds the cap', () => {
    const buffer = new PcmJitterBuffer()
    // 0.7 s: over the 0.6 s cap, so it must be cut back to the 0.2 s prefill.
    const ramp = Array.from({ length: 33600 }, (_, i) => i % 30000)
    buffer.push(pcm(ramp, ramp))
    expect(buffer.size).toBe(9600)
    const left = new Float32Array(1)
    const right = new Float32Array(1)
    buffer.pull(left, right)
    // The surviving window starts at frame 24000, not at frame 0.
    expect(left[0]).toBeCloseTo(24000 / 32768, 5)
  })

  it('zero-fills and re-arms the prefill after an underrun', () => {
    const buffer = new PcmJitterBuffer()
    buffer.push(pcm(new Array(9600).fill(32767), new Array(9600).fill(32767)))
    const left = new Float32Array(4096)
    const right = new Float32Array(4096)
    // 9600 buffered, 4096 per pull: the third pull drains 1408 real frames and
    // zero-fills the rest.
    buffer.pull(left, right)
    buffer.pull(left, right)
    buffer.pull(left, right)
    expect(left[0]).toBeCloseTo(1, 3)
    expect(left[1407]).toBeCloseTo(1, 3)
    expect(left[1408]).toBe(0)
    expect(left[4095]).toBe(0)

    // Having run dry, the buffer stays silent until it is primed again.
    const next = new Float32Array(16)
    buffer.pull(next, next)
    expect(next.every((v) => v === 0)).toBe(true)
  })
})

describe('startAndroidScreenCapture', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    writtenFrames.length = 0
    bridge.startScreenCapture.mockClear()
    bridge.stopScreenCapture.mockClear()
    bridge.takeScreenFrame.mockReset().mockReturnValue('')
    bridge.takeSystemAudio.mockReset().mockReturnValue('')
    bridge.getScreenCaptureState.mockReset().mockReturnValue('{"state":"requesting"}')
    vi.stubGlobal('MediaStream', FakeMediaStream as unknown as typeof MediaStream)
    vi.stubGlobal('VideoFrame', FakeVideoFrame as unknown as typeof VideoFrame)
    vi.stubGlobal('VideoTrackGenerator', FakeVideoTrackGenerator as unknown as typeof VideoTrackGenerator)
    vi.stubGlobal('createImageBitmap', vi.fn(async () => ({ width: 8, height: 8, close: vi.fn() })))
  })

  afterEach(() => {
    delete (window as unknown as Record<string, unknown>).__clawbenchScreenTick
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('sends the capture options and resolves once the projection is running', async () => {
    bridge.getScreenCaptureState.mockReturnValue(
      '{"state":"running","width":1080,"height":2400,"systemAudio":false}',
    )
    const handle = await startAndroidScreenCapture({ fps: 8, maxWidth: 720, quality: 40 })
    expect(bridge.startScreenCapture).toHaveBeenCalledTimes(1)
    expect(JSON.parse(bridge.startScreenCapture.mock.calls[0][0] as unknown as string)).toEqual({
      fps: 8,
      maxWidth: 720,
      quality: 40,
      systemAudio: false,
    })
    expect(handle.width).toBe(1080)
    expect(handle.height).toBe(2400)
    expect(handle.systemAudio).toBe(false)
    handle.stop()
  })

  it('rejects when the user denies the consent dialog', async () => {
    bridge.getScreenCaptureState.mockReturnValue('{"state":"denied"}')
    await expect(startAndroidScreenCapture({})).rejects.toThrow()
  })

  it('rejects with the native error message', async () => {
    bridge.getScreenCaptureState.mockReturnValue('{"state":"error","error":"boom"}')
    await expect(startAndroidScreenCapture({})).rejects.toThrow('boom')
  })

  it('writes a decoded frame on each tick and ignores ticks after stop()', async () => {
    bridge.getScreenCaptureState.mockReturnValue(
      '{"state":"running","width":1080,"height":2400,"systemAudio":false}',
    )
    const handle = await startAndroidScreenCapture({})
    bridge.takeScreenFrame.mockReturnValue(base64(new Uint8Array([1, 2, 3])))

    tick()
    await flush()
    expect(writtenFrames).toHaveLength(1)

    handle.stop()
    tick()
    await flush()
    // The global was removed, so this is a no-op; even if it fired, `stopped`
    // must keep the frame out of the (already closed) generator.
    expect(writtenFrames).toHaveLength(1)
    expect(bridge.stopScreenCapture).toHaveBeenCalledTimes(1)
  })

  it('feeds system audio into the stream when the host reports it', async () => {
    bridge.getScreenCaptureState.mockReturnValue(
      '{"state":"running","width":1080,"height":2400,"systemAudio":true}',
    )
    const ctx = new FakeAudioContext()
    vi.stubGlobal('AudioContext', function () {
      return ctx
    } as unknown as typeof AudioContext)

    const handle = await startAndroidScreenCapture({ systemAudio: true })
    expect(handle.systemAudio).toBe(true)
    expect(handle.stream.getAudioTracks()).toHaveLength(1)
    expect(ctx.scriptProcessors).toHaveLength(1)

    // A tick with PCM must not throw and must keep the buffer armed.
    bridge.takeSystemAudio.mockReturnValue(
      base64(pcm(new Array(9600).fill(1000), new Array(9600).fill(1000))),
    )
    tick()
    await flush()

    handle.stop()
    expect(ctx.closed).toBe(true)
  })

  it('ends the share when the projection disappears on the native side', async () => {
    bridge.getScreenCaptureState.mockReturnValue(
      '{"state":"running","width":1080,"height":2400,"systemAudio":false}',
    )
    const onEnded = vi.fn()
    const handle = await startAndroidScreenCapture({ onEnded })
    bridge.getScreenCaptureState.mockReturnValue('{"state":"idle"}')
    await vi.advanceTimersByTimeAsync(2100)
    expect(onEnded).toHaveBeenCalledTimes(1)
    handle.stop()
  })
})

/**
 * Minimal AudioContext: the capture module only creates a destination, a
 * ScriptProcessor and a ConstantSource, then closes the context.
 */
class FakeAudioContext {
  sampleRate = 48000
  closed = false
  scriptProcessors: FakeScriptProcessor[] = []
  createMediaStreamDestination() {
    const track = new FakeTrack()
    track.kind = 'audio'
    return {
      stream: new FakeMediaStream([track]),
      connect: () => undefined,
      disconnect: () => undefined,
    }
  }
  createScriptProcessor() {
    const processor = new FakeScriptProcessor()
    this.scriptProcessors.push(processor)
    return processor
  }
  createConstantSource() {
    return {
      offset: { value: 0 },
      connect: () => undefined,
      disconnect: () => undefined,
      start: () => undefined,
      stop: () => undefined,
    }
  }
  resume() {
    return Promise.resolve()
  }
  close() {
    this.closed = true
    return Promise.resolve()
  }
}

class FakeScriptProcessor {
  onaudioprocess: ((e: { outputBuffer: { getChannelData(n: number): Float32Array } }) => void) | null =
    null
  connect() {}
  disconnect() {}
}
