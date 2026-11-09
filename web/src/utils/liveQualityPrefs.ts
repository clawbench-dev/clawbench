/**
 * Picture-quality preferences for meetings, as the user sees them in
 * 设置 → 会议 → 画面质量.
 *
 * These are per-device, not per-server: the right ceiling depends on the
 * machine that has to encode the picture and the link it uploads over, so a
 * phone and a workstation sharing one ClawBench server must be able to choose
 * differently.
 *
 * The stored values are plain settings keys (see `settingsStorage`), so the
 * settings panel writes them and this module reads them back — there is no
 * second copy to keep in sync. Every read is normalised against a whitelist:
 * a hand-edited or stale value falls back to the default instead of reaching
 * `getDisplayMedia` as nonsense.
 *
 * `liveMediaPolicy.ts` turns these into capture constraints and encoder
 * budgets. Changes apply the next time a picture starts (a new share, a new
 * viewer joining); an already-running capture keeps the settings it started
 * with, which is what the description text in the panel says.
 */
import { readStoredSetting } from './settingsStorage'

/** Resolutions offered for a shared screen, in CSS pixels. */
export const SCREEN_RESOLUTIONS = {
  '1280x720': { width: 1280, height: 720 },
  '1920x1200': { width: 1920, height: 1200 },
  '2560x1600': { width: 2560, height: 1600 },
  '3840x2160': { width: 3840, height: 2160 },
} as const
export type ScreenResolutionKey = keyof typeof SCREEN_RESOLUTIONS

/** Resolutions offered for the camera tile. */
export const CAMERA_RESOLUTIONS = {
  '640x480': { width: 640, height: 480 },
  '1280x720': { width: 1280, height: 720 },
  '1920x1080': { width: 1920, height: 1080 },
} as const
export type CameraResolutionKey = keyof typeof CAMERA_RESOLUTIONS

export const SCREEN_FPS_CHOICES = [15, 24, 30, 60] as const
export const SCREEN_BITRATE_CHOICES = [2, 4, 6, 8, 12] as const
export const CAMERA_FPS_CHOICES = [15, 24, 30] as const
export const CAMERA_BITRATE_CHOICES = [1, 1.5, 2.5, 4] as const

/**
 * Defaults.
 *
 * 1080p-class screen at 30 fps / 4 Mbps was the old hard-coded policy and
 * stays the screen default: it is the most a four-viewer meeting on a laptop
 * can carry without the governor stepping in. The camera default moved up from
 * 480p/15 fps to 720p/24 fps — 480p was visibly soft even in a small tile, and
 * a camera is one encode per viewer, not the screen's full-frame one.
 */
export const LIVE_QUALITY_DEFAULTS = {
  screenResolution: '1920x1200' as ScreenResolutionKey,
  screenFps: 30,
  screenBitrate: 4,
  cameraResolution: '1280x720' as CameraResolutionKey,
  cameraFps: 24,
  cameraBitrate: 2.5,
  autoDowngrade: true,
}

export interface LiveQuality {
  screenWidth: number
  screenHeight: number
  screenFps: number
  /** Megabits per second. */
  screenBitrate: number
  cameraWidth: number
  cameraHeight: number
  cameraFps: number
  cameraBitrate: number
  /** Whether the encoder may step resolution down under CPU pressure. */
  autoDowngrade: boolean
}

function pickNumber(raw: unknown, choices: readonly number[], fallback: number): number {
  const n = typeof raw === 'number' ? raw : Number(raw)
  return choices.includes(n) ? n : fallback
}

function pickKey<T extends string>(raw: unknown, table: Record<T, unknown>, fallback: T): T {
  return typeof raw === 'string' && raw in table ? (raw as T) : fallback
}

/**
 * The preferences in effect, resolved to concrete numbers.
 *
 * Read on demand rather than cached: a capture or a viewer join is a rare
 * event, and reading fresh means the settings panel needs no invalidation
 * hook to make a change take effect.
 */
export function readLiveQuality(): LiveQuality {
  const screenKey = pickKey(
    readStoredSetting('liveScreenResolution', LIVE_QUALITY_DEFAULTS.screenResolution),
    SCREEN_RESOLUTIONS,
    LIVE_QUALITY_DEFAULTS.screenResolution,
  )
  const cameraKey = pickKey(
    readStoredSetting('liveCameraResolution', LIVE_QUALITY_DEFAULTS.cameraResolution),
    CAMERA_RESOLUTIONS,
    LIVE_QUALITY_DEFAULTS.cameraResolution,
  )
  const screen = SCREEN_RESOLUTIONS[screenKey]
  const camera = CAMERA_RESOLUTIONS[cameraKey]
  const autoDowngrade = readStoredSetting('liveAutoDowngrade', LIVE_QUALITY_DEFAULTS.autoDowngrade)
  return {
    screenWidth: screen.width,
    screenHeight: screen.height,
    screenFps: pickNumber(
      readStoredSetting('liveScreenFps', LIVE_QUALITY_DEFAULTS.screenFps),
      SCREEN_FPS_CHOICES,
      LIVE_QUALITY_DEFAULTS.screenFps,
    ),
    screenBitrate: pickNumber(
      readStoredSetting('liveScreenBitrate', LIVE_QUALITY_DEFAULTS.screenBitrate),
      SCREEN_BITRATE_CHOICES,
      LIVE_QUALITY_DEFAULTS.screenBitrate,
    ),
    cameraWidth: camera.width,
    cameraHeight: camera.height,
    cameraFps: pickNumber(
      readStoredSetting('liveCameraFps', LIVE_QUALITY_DEFAULTS.cameraFps),
      CAMERA_FPS_CHOICES,
      LIVE_QUALITY_DEFAULTS.cameraFps,
    ),
    cameraBitrate: pickNumber(
      readStoredSetting('liveCameraBitrate', LIVE_QUALITY_DEFAULTS.cameraBitrate),
      CAMERA_BITRATE_CHOICES,
      LIVE_QUALITY_DEFAULTS.cameraBitrate,
    ),
    autoDowngrade: autoDowngrade !== false,
  }
}

/**
 * Capture options for the Android projection bridge.
 *
 * The phone hands over JPEG frames through the WebView bridge and the page
 * decodes each one before WebRTC re-encodes it, so this path is far more
 * expensive per pixel than `getDisplayMedia`. Both caps therefore follow the
 * user's screen choice but stay below it: 15 fps and 1920 px are what a
 * mid-range phone sustains, and the quality bump (55 → 68) is what removes the
 * blockiness that made phone shares look worse than their resolution suggests.
 */
export function androidCaptureOptions(quality: LiveQuality = readLiveQuality()): {
  fps: number
  maxWidth: number
  quality: number
} {
  return {
    fps: Math.min(quality.screenFps, 15),
    maxWidth: Math.min(quality.screenWidth, 1920),
    quality: 68,
  }
}
