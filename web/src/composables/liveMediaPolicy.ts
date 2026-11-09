/**
 * Encode-side policy for live screen sharing.
 *
 * The publisher owns one RTCPeerConnection per viewer and WebRTC never shares an
 * encoder between connections, so N viewers means N simultaneous encodes of the
 * same picture. Nothing in WebRTC bounds that cost by itself: without an
 * explicit budget each sender encodes at the capture's native resolution with
 * the codec the browser happens to prefer. On a 2K/4K desktop that is the
 * difference between four viewers being routine and four viewers pinning every
 * core.
 *
 * Three separate levers, because they fail independently:
 *
 * 1. **Capture caps** — the cheapest win. Halving each dimension is 4× fewer
 *    pixels for every encoder at once, and it happens before any encoder runs.
 * 2. **Codec choice** — only when the platform reports a hardware encoder. A
 *    software H.264 is not obviously cheaper than VP8, so we never gamble on a
 *    guess: no `powerEfficient` verdict means we leave the browser's default
 *    alone.
 * 3. **A CPU governor** — `qualityLimitationReason === 'cpu'` is the browser
 *    telling us the encoder is behind. Stepping resolution down when that
 *    persists (and back up when it stops) keeps a slow machine usable instead of
 *    freezing, without capping a fast one.
 *
 * Everything here is defensive: jsdom, Safari and older Chromium all lack parts
 * of the API surface, and a missing optional call must never break a share.
 *
 * The numbers behind levers 1 and 3 are the user's 设置 → 会议 → 画面质量
 * choices (`utils/liveQualityPrefs`), not constants. This module owns how a
 * choice turns into constraints and budgets; the panel owns what the choices
 * are. A capture reads them when it starts, so a change applies to the next
 * share or the next viewer rather than retroactively to a running encoder.
 */
import { appLog } from '@/utils/appLog'
import { readLiveQuality } from '@/utils/liveQualityPrefs'

const TAG = 'LiveMedia'

/**
 * One sender's encode budget, before the governor scales it down.
 *
 * Built from the user's 画面质量 choices (`utils/liveQualityPrefs`) rather than
 * from constants: how much detail a machine can carry is a property of the
 * machine and its link, which is exactly what the settings panel asks about.
 */
export interface VideoEncodeBudget {
  /** Ceiling for this sender, in bits per second. */
  maxBitrate: number
  maxFramerate: number
  /** 1 = capture resolution; 2 = half each dimension. */
  scaleResolutionDownBy: number
}

/** The publisher's own screen: the main picture, encoded once per viewer. */
export function screenBudget(): VideoEncodeBudget {
  const q = readLiveQuality()
  return {
    maxBitrate: Math.round(q.screenBitrate * 1_000_000),
    maxFramerate: q.screenFps,
    scaleResolutionDownBy: 1,
  }
}

/**
 * A relayed member screen.
 *
 * This path is already decode-then-re-encode, so it starts a step below the
 * host's own capture: the picture has been through one encoder already and the
 * extra fidelity would be spent re-encoding artifacts. Three quarters of the
 * screen bitrate is that step, and it follows the user's choice instead of
 * being pinned, so raising 画面质量 lifts this path too.
 */
export function relayBudget(): VideoEncodeBudget {
  const q = readLiveQuality()
  return {
    maxBitrate: Math.round(q.screenBitrate * 750_000),
    maxFramerate: Math.min(q.screenFps, 30),
    scaleResolutionDownBy: 1,
  }
}

/** The camera tile, or the whole picture in a camera-only meeting. */
export function cameraBudget(): VideoEncodeBudget {
  const q = readLiveQuality()
  return {
    maxBitrate: Math.round(q.cameraBitrate * 1_000_000),
    maxFramerate: q.cameraFps,
    scaleResolutionDownBy: 1,
  }
}

/**
 * A member's own screen, on its way up to the host.
 *
 * The host decodes this and re-encodes it for everyone else, so every extra bit
 * and frame here is paid twice. The host cannot bound this from its side — the
 * encoder that needs bounding is on the member's machine — so it sits at half
 * the host's screen budget and never above 20 fps.
 */
export function guestScreenBudget(): VideoEncodeBudget {
  const q = readLiveQuality()
  return {
    maxBitrate: Math.round(q.screenBitrate * 500_000),
    maxFramerate: Math.min(q.screenFps, 20),
    scaleResolutionDownBy: 1,
  }
}

/**
 * Capture constraints for `getDisplayMedia`.
 *
 * Expressed as `max`, not `ideal`: `ideal` lets the browser hand back a 4K
 * surface when it cannot satisfy the request, which is exactly the case this
 * exists to prevent.
 */
export function screenCaptureConstraints(): MediaTrackConstraints {
  const q = readLiveQuality()
  return {
    width: { max: q.screenWidth },
    height: { max: q.screenHeight },
    frameRate: { max: q.screenFps },
  }
}

/**
 * Capture constraints for the camera.
 *
 * `ideal` here (unlike the screen): a camera that cannot do 720p should hand
 * back its best mode, not fail or be padded up to a size it does not have.
 */
export function cameraCaptureConstraints(): MediaTrackConstraints {
  const q = readLiveQuality()
  return {
    width: { ideal: q.cameraWidth },
    height: { ideal: q.cameraHeight },
    frameRate: { ideal: q.cameraFps, max: q.cameraFps * 1.5 },
  }
}

/**
 * Hint the encoder about what the picture is.
 *
 * `text` is what makes Chromium use its screen-content path (skip unchanged
 * blocks, preserve edges); `motion` keeps a camera from being treated as a
 * mostly-static desktop and smeared.
 */
export function applyContentHint(track: MediaStreamTrack | null | undefined, hint: 'text' | 'motion'): void {
  if (!track) return
  try {
    track.contentHint = hint
  } catch (e) {
    appLog.d(TAG, 'contentHint rejected', e)
  }
}

/**
 * Move one codec family to the front, keeping RTX attached to its codec.
 *
 * `setCodecPreferences` requires every codec in the list to be one the browser
 * advertised, and an RTX entry is only valid next to the codec it repairs — so
 * this reorders whole families instead of filtering. Anything we do not
 * recognise keeps its original relative position after the preferred family,
 * which leaves a working fallback when the far end cannot do the preferred one.
 */
export function reorderVideoCodecs(
  available: readonly RTCRtpCodec[],
  preferredMime: string,
): RTCRtpCodec[] {
  const families: RTCRtpCodec[][] = []
  for (const codec of available) {
    const mime = (codec.mimeType ?? '').toLowerCase()
    // RTX/ RED / FEC belong to the codec that precedes them.
    if (mime === 'video/rtx' || mime === 'video/red' || mime === 'video/ulpfec') {
      if (families.length > 0) families[families.length - 1].push(codec)
      else families.push([codec])
      continue
    }
    families.push([codec])
  }
  const wanted = preferredMime.toLowerCase()
  const preferred = families.filter((f) => (f[0].mimeType ?? '').toLowerCase() === wanted)
  const rest = families.filter((f) => (f[0].mimeType ?? '').toLowerCase() !== wanted)
  return [...preferred, ...rest].flat()
}

/**
 * The codec to prefer, given what the platform advertises and what it says
 * about hardware acceleration.
 *
 * Returns null when nothing is known to be hardware-accelerated — the browser's
 * own choice is a better default than a guess, because software H.264 encodes
 * screen content worse *and* slower than VP8 on most machines.
 */
export function pickPreferredVideoMime(
  availableMimes: readonly string[],
  isPowerEfficient: (mime: string) => boolean | null,
): string | null {
  const have = new Set(availableMimes.map((m) => m.toLowerCase()))
  for (const mime of ['video/h264', 'video/vp9', 'video/vp8']) {
    if (!have.has(mime)) continue
    if (isPowerEfficient(mime) === true) return mime
  }
  return null
}

/**
 * Content types used to ask `mediaCapabilities` about one MIME.
 *
 * `encodingInfo` needs a concrete codec string; the profile is only a probe, so
 * the widely-supported baseline/H.264 3.1 and the VP9 profile-0 8-bit form are
 * enough to learn whether the platform has *any* hardware encoder for the
 * family.
 */
const MIME_PROBE_CODEC: Record<string, string> = {
  'video/h264': 'video/H264;codecs=avc1.42E01E',
  'video/vp9': 'video/VP9;codecs=vp09.00.10.08',
  'video/vp8': 'video/VP8;codecs=vp8',
}

async function probePowerEfficient(mime: string): Promise<boolean | null> {
  const contentType = MIME_PROBE_CODEC[mime]
  const mc = typeof navigator === 'undefined' ? undefined : navigator.mediaCapabilities
  if (!contentType || !mc?.encodingInfo) return null
  try {
    const info = await mc.encodingInfo({
      type: 'record',
      video: { contentType, width: 1920, height: 1080, bitrate: 4_000_000, framerate: 30 },
    })
    return info.powerEfficient === true
  } catch {
    // An unsupported contentType throws; "no opinion" is the honest answer.
    return null
  }
}

/**
 * Ask the platform which video codec to prefer for this machine.
 *
 * Returns null when the answer is "no hardware encoder" or "cannot tell", which
 * callers must treat as "leave the browser default alone".
 */
export async function resolvePreferredVideoMime(): Promise<string | null> {
  const caps = typeof RTCRtpSender === 'undefined' ? null : RTCRtpSender.getCapabilities?.('video')
  const codecs = caps?.codecs
  if (!codecs?.length) return null
  const mimes = [...new Set(codecs.map((c) => (c.mimeType ?? '').toLowerCase()))].filter(
    (m) => MIME_PROBE_CODEC[m] !== undefined,
  )
  const verdict = new Map<string, boolean | null>()
  for (const mime of mimes) verdict.set(mime, await probePowerEfficient(mime))
  return pickPreferredVideoMime(mimes, (m) => verdict.get(m) ?? null)
}

/**
 * Apply a codec preference to one transceiver.
 *
 * Must run before `createAnswer`/`createOffer`: the preference only affects
 * negotiation that has not happened yet, and an answer already sent keeps the
 * codec it named.
 */
export function applyCodecPreference(
  tx: RTCRtpTransceiver | null | undefined,
  available: readonly RTCRtpCodec[],
  preferredMime: string | null,
): boolean {
  if (!tx || !preferredMime || available.length === 0) return false
  if (typeof tx.setCodecPreferences !== 'function') return false
  try {
    tx.setCodecPreferences(reorderVideoCodecs(available, preferredMime))
    return true
  } catch (e) {
    // Chrome rejects a list that is not exactly a reordering of its own; the
    // browser default is a fine fallback.
    appLog.d(TAG, 'setCodecPreferences rejected', e)
    return false
  }
}

/**
 * Push one budget onto a sender.
 *
 * `scaleResolutionDownBy` is the only field that changes encoder *cost* —
 * bitrate alone changes how hard the encoder works per pixel but not how many
 * pixels there are — and it is also the field some platforms refuse to change
 * on a live sender. The retry without it keeps bitrate and frame rate bounded
 * instead of dropping the whole budget.
 */
export async function applyEncodeBudget(
  sender: RTCRtpSender | null | undefined,
  budget: VideoEncodeBudget,
): Promise<boolean> {
  if (!sender || typeof sender.getParameters !== 'function' || typeof sender.setParameters !== 'function') {
    return false
  }
  let params: RTCRtpSendParameters
  try {
    params = sender.getParameters()
  } catch {
    return false
  }
  if (!params) return false
  if (!Array.isArray(params.encodings) || params.encodings.length === 0) params.encodings = [{}]
  const encoding = params.encodings[0]
  encoding.maxBitrate = Math.max(1, Math.round(budget.maxBitrate))
  encoding.maxFramerate = Math.max(1, Math.round(budget.maxFramerate))
  encoding.scaleResolutionDownBy = Math.max(1, budget.scaleResolutionDownBy)
  // Screen content should lose frame rate, not legibility, when the pipe
  // narrows. (This does not affect CPU pressure, which the governor handles.)
  params.degradationPreference = 'maintain-resolution'
  try {
    await sender.setParameters(params)
    return true
  } catch (e) {
    appLog.d(TAG, 'setParameters with scaling rejected, retrying without', e)
  }
  delete encoding.scaleResolutionDownBy
  try {
    await sender.setParameters(params)
    return true
  } catch (e) {
    // Best effort by design: a sender that refuses every parameter still
    // carries a working picture, it is just not bounded. Debug, not warning —
    // a voice-only meeting has senders with no track yet and would otherwise
    // fill the log with noise that looks like a failure.
    appLog.d(TAG, 'setParameters rejected', e)
    return false
  }
}

/** How many levels the governor may descend: 1× → 2× → 4× downscale. */
export const MAX_ENCODE_LEVEL = 2

/** Poll period. Slow enough to be free, fast enough to react inside a scroll. */
const GOVERNOR_POLL_MS = 3000
/** Consecutive CPU-limited samples before stepping down. */
const DOWN_AFTER = 2
/** Consecutive clean samples before stepping back up (avoids flapping). */
const UP_AFTER = 4

interface GovernorEntry {
  sender: RTCRtpSender
  budget: VideoEncodeBudget
}

/** Read the browser's own verdict on why it is not sending faster. */
async function cpuLimitationOf(sender: RTCRtpSender): Promise<string | null> {
  if (typeof sender.getStats !== 'function') return null
  try {
    const report = await sender.getStats()
    let reason: string | null = null
    report.forEach((stat: RTCStats) => {
      const s = stat as RTCStats & { type?: string; qualityLimitationReason?: string }
      if (s.type === 'outbound-rtp' && typeof s.qualityLimitationReason === 'string') {
        reason = s.qualityLimitationReason
      }
    })
    return reason
  } catch {
    return null
  }
}

/**
 * Machine-wide encode governor.
 *
 * CPU is a shared resource, so one governor owns every sender instead of one
 * controller per peer: independent controllers would each see "cpu" from the
 * same saturated machine and step down at different times, producing viewers
 * with mismatched quality for no benefit.
 *
 * It only ever reacts to the browser's own `qualityLimitationReason`, never to a
 * JS-side guess about how much work an encode is.
 *
 * `autoDowngrade` is the user's switch (设置 → 会议 → 画面质量). Turning it off
 * keeps every sender at its base budget and stops the sampling rounds — it does
 * NOT remove the budgets themselves, because those are the only thing bounding
 * the encoders at all. The distinction matters: "no governor" would mean "no
 * bitrate cap", which is the CPU blow-up this file exists to prevent.
 */
export class VideoLoadGovernor {
  private readonly entries = new Map<string, GovernorEntry>()
  private readonly autoDowngrade: boolean
  private level = 0
  private downStreak = 0
  private upStreak = 0
  private timer: ReturnType<typeof setInterval> | null = null
  /** True while a sampling round is in flight (see sample()). */
  private sampling = false

  constructor(autoDowngrade = true) {
    this.autoDowngrade = autoDowngrade
  }

  /** Current downscale level: 0 = as captured, 1 = 2×, 2 = 4×. */
  get currentLevel(): number {
    return this.level
  }

  /** Number of senders under management (tests and diagnostics). */
  get size(): number {
    return this.entries.size
  }

  /** Start governing one sender. Idempotent per key. */
  register(key: string, sender: RTCRtpSender, budget: VideoEncodeBudget): void {
    this.entries.set(key, { sender, budget })
    void this.applyTo(this.entries.get(key) as GovernorEntry)
  }

  /** Swap a sender's base budget, e.g. when the picture becomes a camera. */
  setBudget(key: string, budget: VideoEncodeBudget): void {
    const entry = this.entries.get(key)
    if (!entry) return
    entry.budget = budget
    void this.applyTo(entry)
  }

  unregister(key: string): void {
    this.entries.delete(key)
  }

  start(): void {
    if (this.timer !== null) return
    this.timer = setInterval(() => {
      void this.sample()
    }, GOVERNOR_POLL_MS)
  }

  stop(): void {
    if (this.timer !== null) clearInterval(this.timer)
    this.timer = null
    this.entries.clear()
    this.level = 0
    this.downStreak = 0
    this.upStreak = 0
  }

  /**
   * One polling round.
   *
   * Public so tests can drive it directly: a 3 s timer is not something a unit
   * test should wait for, and faking timers around an async stats read is how
   * these tests become flaky.
   *
   * Re-entrant calls are dropped. The interval does not wait for the previous
   * round, and a round costs one `getStats()` per sender — on the slow, loaded
   * machine this governor exists for, two rounds can overlap, and then each one
   * counts the same busy window: `DOWN_AFTER` would be reached in a single
   * sample and `applyAll()` would run twice on the same senders.
   */
  async sample(): Promise<void> {
    if (this.sampling) return
    if (this.entries.size === 0) return
    this.sampling = true
    try {
      await this.sampleOnce()
    } finally {
      this.sampling = false
    }
  }

  private async sampleOnce(): Promise<void> {
    // The user turned automatic downgrading off: hold the base budget and skip
    // the per-sender stats read entirely (one getStats per sender per round is
    // not free on the machines this setting exists for).
    if (!this.autoDowngrade) return
    let limited = false
    let sampled = false
    for (const entry of this.entries.values()) {
      const reason = await cpuLimitationOf(entry.sender)
      if (reason === null) continue
      sampled = true
      if (reason === 'cpu') limited = true
    }
    // No sender answered with a reason (older Firefox, or nothing on the wire
    // yet): hold the current level rather than drifting back to full quality.
    if (!sampled) return

    if (limited) {
      this.upStreak = 0
      this.downStreak += 1
      if (this.downStreak >= DOWN_AFTER && this.level < MAX_ENCODE_LEVEL) {
        this.downStreak = 0
        this.level += 1
        await this.applyAll()
      }
      return
    }
    this.downStreak = 0
    this.upStreak += 1
    if (this.upStreak >= UP_AFTER && this.level > 0) {
      this.upStreak = 0
      this.level -= 1
      await this.applyAll()
    }
  }

  private async applyAll(): Promise<void> {
    for (const entry of this.entries.values()) await this.applyTo(entry)
    appLog.i(TAG, `encode level ${this.level} (${2 ** this.level}× downscale) on ${this.entries.size} senders`)
  }

  private async applyTo(entry: GovernorEntry): Promise<void> {
    const factor = 2 ** this.level
    await applyEncodeBudget(entry.sender, {
      maxBitrate: entry.budget.maxBitrate / factor,
      maxFramerate: Math.max(10, Math.round(entry.budget.maxFramerate / factor)),
      scaleResolutionDownBy: entry.budget.scaleResolutionDownBy * factor,
    })
  }
}
