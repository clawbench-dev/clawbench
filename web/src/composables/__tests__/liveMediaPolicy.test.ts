import { describe, it, expect, vi, afterEach } from 'vitest'
import {
  CAMERA_BUDGET,
  MAX_ENCODE_LEVEL,
  RELAY_BUDGET,
  SCREEN_BUDGET,
  VideoLoadGovernor,
  applyCodecPreference,
  applyContentHint,
  applyEncodeBudget,
  pickPreferredVideoMime,
  reorderVideoCodecs,
  resolvePreferredVideoMime,
  screenCaptureConstraints,
} from '../liveMediaPolicy'

/**
 * Encode-side policy for live sharing.
 *
 * The failures worth guarding here are all silent: a budget that is never
 * pushed, a governor that never recovers, a codec preference that reorders RTX
 * away from its codec and makes `setCodecPreferences` throw — each one leaves a
 * share that *works*, just expensively.
 */

interface SendParams {
  encodings: Array<Record<string, unknown>>
  degradationPreference?: string
}

/** Minimal RTCRtpSender: only the three members the policy touches. */
class Sender {
  params: SendParams = { encodings: [{}] }
  /** What `getStats()` reports; 'cpu' is the browser saying it is behind. */
  reason: string | null = 'none'
  /** Mimic a platform that refuses to change the scale on a live sender. */
  rejectScale = false
  applied: SendParams[] = []

  getParameters(): SendParams {
    return this.params
  }

  async setParameters(p: SendParams): Promise<void> {
    if (this.rejectScale && p.encodings[0]?.scaleResolutionDownBy !== undefined) {
      throw new Error('scaleResolutionDownBy not supported')
    }
    this.params = p
    this.applied.push(p)
  }

  async getStats(): Promise<RTCStatsReport> {
    const report = new Map<string, Record<string, unknown>>()
    report.set('out-1', {
      type: 'outbound-rtp',
      qualityLimitationReason: this.reason ?? 'none',
    })
    return report as unknown as RTCStatsReport
  }
}

/** A sender whose getStats() rejects — an older browser, or a closed peer. */
class MuteSender extends Sender {
  async getStats(): Promise<RTCStatsReport> {
    throw new Error('no stats')
  }
}

function asSender(s: Sender): RTCRtpSender {
  return s as unknown as RTCRtpSender
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('screenCaptureConstraints', () => {
  it('caps both dimensions and the frame rate instead of taking the native surface', () => {
    expect(screenCaptureConstraints()).toEqual({
      width: { max: 1920 },
      height: { max: 1200 },
      frameRate: { max: 30 },
    })
  })
})

describe('applyContentHint', () => {
  it('sets the hint', () => {
    const track = {} as { contentHint?: string }
    applyContentHint(track as unknown as MediaStreamTrack, 'text')
    expect(track.contentHint).toBe('text')
  })

  it('ignores a missing track', () => {
    expect(() => applyContentHint(null, 'text')).not.toThrow()
  })

  it('survives a track that refuses the assignment', () => {
    const track = {}
    Object.defineProperty(track, 'contentHint', {
      configurable: true,
      get: () => '',
      set: () => {
        throw new Error('read-only')
      },
    })
    expect(() => applyContentHint(track as unknown as MediaStreamTrack, 'motion')).not.toThrow()
  })
})

describe('reorderVideoCodecs', () => {
  const available = [
    { mimeType: 'video/VP8', clockRate: 90000 },
    { mimeType: 'video/rtx', clockRate: 90000, sdpFmtpLine: 'apt=96' },
    { mimeType: 'video/H264', clockRate: 90000, sdpFmtpLine: 'profile-level-id=42e01f' },
    { mimeType: 'video/rtx', clockRate: 90000, sdpFmtpLine: 'apt=102' },
  ] as RTCRtpCodec[]

  it('moves a codec family to the front with its own RTX', () => {
    // Each RTX entry belongs to the codec above it (`apt`). Moving H264 to the
    // front without its RTX makes Chrome reject the whole list.
    const out = reorderVideoCodecs(available, 'video/H264')
    expect(out).toEqual([available[2], available[3], available[0], available[1]])
  })

  it('keeps the original order when the preferred codec is absent', () => {
    expect(reorderVideoCodecs(available, 'video/AV1')).toEqual(available)
  })
})

describe('pickPreferredVideoMime', () => {
  it('prefers H.264 when the platform says it is hardware accelerated', () => {
    const mime = pickPreferredVideoMime(['video/h264', 'video/vp8'], (m) => m === 'video/h264')
    expect(mime).toBe('video/h264')
  })

  it('falls through to VP9 when H.264 has no hardware encoder', () => {
    const mime = pickPreferredVideoMime(['video/h264', 'video/vp9', 'video/vp8'], (m) => m === 'video/vp9')
    expect(mime).toBe('video/vp9')
  })

  it('returns null when nothing is known to be hardware accelerated', () => {
    // A software H.264 is not cheaper than VP8, so guessing would make things
    // worse on exactly the machines this policy exists to help.
    expect(pickPreferredVideoMime(['video/h264', 'video/vp8'], () => false)).toBeNull()
    expect(pickPreferredVideoMime(['video/h264', 'video/vp8'], () => null)).toBeNull()
  })

  it('ignores codecs the browser did not advertise', () => {
    expect(pickPreferredVideoMime(['video/vp8'], (m) => m === 'video/h264')).toBeNull()
  })
})

describe('resolvePreferredVideoMime', () => {
  function stubCaps(mimes: string[]): void {
    vi.stubGlobal('RTCRtpSender', {
      getCapabilities: () => ({ codecs: mimes.map((m) => ({ mimeType: m, clockRate: 90000 })) }),
    })
  }

  it('answers from the platform hardware-encoder report', async () => {
    stubCaps(['video/VP8', 'video/rtx', 'video/H264'])
    vi.stubGlobal('navigator', {
      mediaCapabilities: {
        encodingInfo: async (cfg: { video: { contentType: string } }) => ({
          powerEfficient: cfg.video.contentType.includes('H264'),
        }),
      },
    })

    await expect(resolvePreferredVideoMime()).resolves.toBe('video/h264')
  })

  it('returns null when mediaCapabilities is missing', async () => {
    stubCaps(['video/VP8', 'video/H264'])
    vi.stubGlobal('navigator', {})

    await expect(resolvePreferredVideoMime()).resolves.toBeNull()
  })

  it('returns null when the browser advertises no video codecs', async () => {
    vi.stubGlobal('RTCRtpSender', { getCapabilities: () => null })
    await expect(resolvePreferredVideoMime()).resolves.toBeNull()
  })

  it('treats a rejected probe as no opinion rather than a hard no', async () => {
    stubCaps(['video/H264'])
    vi.stubGlobal('navigator', {
      mediaCapabilities: {
        encodingInfo: async () => {
          throw new TypeError('unsupported contentType')
        },
      },
    })

    await expect(resolvePreferredVideoMime()).resolves.toBeNull()
  })
})

describe('applyCodecPreference', () => {
  function tx(setCodecPreferences?: (c: RTCRtpCodec[]) => void): RTCRtpTransceiver {
    return { setCodecPreferences } as unknown as RTCRtpTransceiver
  }
  const available = [
    { mimeType: 'video/VP8', clockRate: 90000 },
    { mimeType: 'video/H264', clockRate: 90000 },
  ] as RTCRtpCodec[]

  it('pushes the reordered list onto the transceiver', () => {
    const setCodecPreferences = vi.fn()
    expect(applyCodecPreference(tx(setCodecPreferences), available, 'video/H264')).toBe(true)
    expect(setCodecPreferences).toHaveBeenCalledWith([available[1], available[0]])
  })

  it('does nothing without a preference, a list, or the method', () => {
    const setCodecPreferences = vi.fn()
    expect(applyCodecPreference(tx(setCodecPreferences), available, null)).toBe(false)
    expect(applyCodecPreference(tx(setCodecPreferences), [], 'video/H264')).toBe(false)
    expect(applyCodecPreference(tx(undefined), available, 'video/H264')).toBe(false)
    expect(setCodecPreferences).not.toHaveBeenCalled()
  })

  it('falls back to the browser default when the list is rejected', () => {
    const setCodecPreferences = vi.fn(() => {
      throw new Error('InvalidModificationError')
    })
    expect(applyCodecPreference(tx(setCodecPreferences), available, 'video/H264')).toBe(false)
  })
})

describe('applyEncodeBudget', () => {
  it('writes bitrate, frame rate, scale and degradation preference', async () => {
    const sender = new Sender()
    await expect(applyEncodeBudget(asSender(sender), SCREEN_BUDGET)).resolves.toBe(true)

    expect(sender.params.encodings[0]).toMatchObject({
      maxBitrate: 4_000_000,
      maxFramerate: 30,
      scaleResolutionDownBy: 1,
    })
    expect(sender.params.degradationPreference).toBe('maintain-resolution')
  })

  it('creates an encoding slot when the sender has none yet', async () => {
    const sender = new Sender()
    sender.params = { encodings: [] }
    await applyEncodeBudget(asSender(sender), CAMERA_BUDGET)
    expect(sender.params.encodings).toHaveLength(1)
    expect(sender.params.encodings[0].maxBitrate).toBe(1_500_000)
  })

  it('retries without the scale when the platform refuses it', async () => {
    const sender = new Sender()
    sender.rejectScale = true
    await expect(applyEncodeBudget(asSender(sender), RELAY_BUDGET)).resolves.toBe(true)

    // Bitrate and frame rate still bound the encoder, so dropping only the
    // rejected field is better than dropping the whole budget.
    expect(sender.applied).toHaveLength(1)
    expect(sender.params.encodings[0].maxBitrate).toBe(2_500_000)
    expect(sender.params.encodings[0].scaleResolutionDownBy).toBeUndefined()
  })

  it('reports failure instead of throwing when the sender cannot take parameters', async () => {
    await expect(applyEncodeBudget(null, SCREEN_BUDGET)).resolves.toBe(false)
    await expect(applyEncodeBudget({} as RTCRtpSender, SCREEN_BUDGET)).resolves.toBe(false)
  })
})

describe('VideoLoadGovernor', () => {
  function makeGovernor(senders: Sender[]): { governor: VideoLoadGovernor; keys: string[] } {
    const governor = new VideoLoadGovernor()
    const keys = senders.map((s, i) => {
      const key = `s${i}`
      governor.register(key, asSender(s), SCREEN_BUDGET)
      return key
    })
    return { governor, keys }
  }

  it('applies the base budget on registration', async () => {
    const sender = new Sender()
    const { governor } = makeGovernor([sender])
    await vi.waitFor(() => expect(sender.applied).toHaveLength(1))
    expect(governor.currentLevel).toBe(0)
    expect(governor.size).toBe(1)
  })

  it('needs two consecutive cpu samples before stepping down', async () => {
    const sender = new Sender()
    const { governor } = makeGovernor([sender])
    sender.reason = 'cpu'

    await governor.sample()
    // A single busy window — a scroll, a keyframe burst — must not cost every
    // viewer a resolution drop.
    expect(governor.currentLevel).toBe(0)

    await governor.sample()
    expect(governor.currentLevel).toBe(1)
    expect(sender.params.encodings[0].scaleResolutionDownBy).toBe(2)
    expect(sender.params.encodings[0].maxBitrate).toBe(2_000_000)
    expect(sender.params.encodings[0].maxFramerate).toBe(15)
  })

  it('steps every registered sender together', async () => {
    const a = new Sender()
    const b = new Sender()
    const { governor } = makeGovernor([a, b])
    a.reason = 'cpu'

    await governor.sample()
    await governor.sample()

    // CPU is machine-wide: one saturated encoder means the others are queued
    // behind it, so they all move.
    expect(b.params.encodings[0].scaleResolutionDownBy).toBe(2)
  })

  it('never descends past the configured depth', async () => {
    const sender = new Sender()
    const { governor } = makeGovernor([sender])
    sender.reason = 'cpu'
    for (let i = 0; i < 20; i++) await governor.sample()
    expect(governor.currentLevel).toBe(MAX_ENCODE_LEVEL)
    expect(sender.params.encodings[0].scaleResolutionDownBy).toBe(4)
  })

  it('recovers only after four clean samples', async () => {
    const sender = new Sender()
    const { governor } = makeGovernor([sender])
    sender.reason = 'cpu'
    await governor.sample()
    await governor.sample()
    expect(governor.currentLevel).toBe(1)

    sender.reason = 'none'
    for (let i = 0; i < 3; i++) await governor.sample()
    // Still degraded: recovering on the first clean sample flaps on a machine
    // hovering at its limit.
    expect(governor.currentLevel).toBe(1)

    await governor.sample()
    expect(governor.currentLevel).toBe(0)
    expect(sender.params.encodings[0].scaleResolutionDownBy).toBe(1)
  })

  it('holds the level when no sender can report a reason', async () => {
    const sender = new MuteSender()
    const { governor } = makeGovernor([sender])
    for (let i = 0; i < 10; i++) await governor.sample()
    expect(governor.currentLevel).toBe(0)
  })

  it('stops managing a sender that was unregistered', async () => {
    const sender = new Sender()
    const { governor, keys } = makeGovernor([sender])
    sender.reason = 'cpu'
    governor.unregister(keys[0])
    expect(governor.size).toBe(0)

    await governor.sample()
    await governor.sample()
    // A departed viewer's encoder must not hold the room down.
    expect(governor.currentLevel).toBe(0)
  })

  it('re-applies the new budget when the picture changes kind', async () => {
    const sender = new Sender()
    const { governor, keys } = makeGovernor([sender])
    governor.setBudget(keys[0], CAMERA_BUDGET)
    await vi.waitFor(() => expect(sender.params.encodings[0].maxBitrate).toBe(1_500_000))
    expect(sender.params.encodings[0].maxFramerate).toBe(20)
  })

  it('resets everything on stop', async () => {
    const sender = new Sender()
    const { governor } = makeGovernor([sender])
    sender.reason = 'cpu'
    await governor.sample()
    await governor.sample()
    expect(governor.currentLevel).toBe(1)

    governor.stop()
    expect(governor.currentLevel).toBe(0)
    expect(governor.size).toBe(0)
  })
})
