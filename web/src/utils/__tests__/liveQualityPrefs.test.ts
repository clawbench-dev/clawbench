import { describe, it, expect, afterEach } from 'vitest'
import { androidCaptureOptions, readLiveQuality } from '../liveQualityPrefs'
import { SETTINGS_KEY_PREFIX } from '../settingsStorage'

/**
 * The panel and the capture path agree only because they share storage keys.
 * These tests read through the same prefix the panel writes, so a renamed key
 * shows up here instead of as "the setting does nothing".
 */
function store(key: string, value: unknown): void {
  localStorage.setItem(SETTINGS_KEY_PREFIX + key, JSON.stringify(value))
}

afterEach(() => {
  localStorage.clear()
})

describe('readLiveQuality', () => {
  it('uses the shipped defaults on a device that never opened the panel', () => {
    expect(readLiveQuality()).toEqual({
      screenWidth: 1920,
      screenHeight: 1200,
      screenFps: 30,
      screenBitrate: 4,
      cameraWidth: 1280,
      cameraHeight: 720,
      cameraFps: 24,
      cameraBitrate: 2.5,
      autoDowngrade: true,
    })
  })

  it('reads what the settings panel stores', () => {
    store('liveScreenResolution', '3840x2160')
    store('liveScreenFps', '60')
    store('liveScreenBitrate', '12')
    store('liveCameraResolution', '1920x1080')
    store('liveCameraFps', '30')
    store('liveCameraBitrate', '4')
    store('liveAutoDowngrade', false)

    const q = readLiveQuality()
    expect(q.screenWidth).toBe(3840)
    expect(q.screenHeight).toBe(2160)
    expect(q.screenFps).toBe(60)
    expect(q.screenBitrate).toBe(12)
    expect(q.cameraWidth).toBe(1920)
    expect(q.cameraHeight).toBe(1080)
    expect(q.cameraFps).toBe(30)
    expect(q.cameraBitrate).toBe(4)
    expect(q.autoDowngrade).toBe(false)
  })

  // A hand-edited or stale value must not reach getDisplayMedia as nonsense;
  // falling back is the whole point of the whitelist.
  it('falls back per field instead of throwing on a bad value', () => {
    store('liveScreenResolution', 'not-a-size')
    store('liveScreenFps', 999)
    store('liveScreenBitrate', 'wat')
    store('liveAutoDowngrade', 'yes')

    const q = readLiveQuality()
    expect(q.screenWidth).toBe(1920)
    expect(q.screenFps).toBe(30)
    expect(q.screenBitrate).toBe(4)
    // Only an explicit false turns automatic downgrading off.
    expect(q.autoDowngrade).toBe(true)
  })
})

describe('androidCaptureOptions', () => {
  it('stays below the desktop caps the user chose', () => {
    store('liveScreenResolution', '3840x2160')
    store('liveScreenFps', '60')
    expect(androidCaptureOptions()).toEqual({ fps: 15, maxWidth: 1920, quality: 68 })
  })

  it('never asks a phone for more frames than the screen setting', () => {
    store('liveScreenFps', '15')
    expect(androidCaptureOptions().fps).toBe(15)
  })
})
