/**
 * useNotificationSound
 *
 * Plays a short notification chime and triggers haptic vibration
 * when the AI finishes replying.
 * Uses Web Audio API to synthesize the sound — no external audio file needed.
 * Uses Vibration API for haptic feedback (Android only; iOS Safari ignores it).
 * The AudioContext is lazily created on first playback to comply with
 * browser autoplay policies (requires user gesture before first use).
 */

import { appLog } from '@/utils/appLog'

const TAG = 'NotificationSound'

let audioCtx: AudioContext | null = null

function getAudioContext(): AudioContext {
  if (!audioCtx) {
    audioCtx = new AudioContext()
  }
  return audioCtx
}

/**
 * Trigger a short haptic vibration pattern.
 * Pattern: 100ms vibrate — 50ms pause — 80ms vibrate
 * Silently ignored on browsers that don't support the Vibration API (e.g. iOS Safari).
 */
function vibrateNotification() {
  try {
    if (navigator.vibrate) {
      navigator.vibrate([100, 50, 80])
    }
  } catch {
    // Silently fail — vibration is non-critical
  }
}

/**
 * Play a short two-tone descending chime (C5 → G4)
 * and trigger haptic vibration.
 * Resumes the AudioContext if it was suspended (browser autoplay policy).
 */
export function playNotificationSound() {
  try {
    const ctx = getAudioContext()
    if (ctx.state === 'suspended') {
      ctx.resume()
    }

    const now = ctx.currentTime

    // First tone: C5 (523 Hz)
    const osc1 = ctx.createOscillator()
    const gain1 = ctx.createGain()
    osc1.type = 'sine'
    osc1.frequency.value = 523.25
    gain1.gain.setValueAtTime(0.3, now)
    gain1.gain.exponentialRampToValueAtTime(0.01, now + 0.15)
    osc1.connect(gain1)
    gain1.connect(ctx.destination)
    osc1.start(now)
    osc1.stop(now + 0.15)

    // Second tone: G4 (392 Hz), starts shortly after
    const osc2 = ctx.createOscillator()
    const gain2 = ctx.createGain()
    osc2.type = 'sine'
    osc2.frequency.value = 392.0
    gain2.gain.setValueAtTime(0.3, now + 0.12)
    gain2.gain.exponentialRampToValueAtTime(0.01, now + 0.35)
    osc2.connect(gain2)
    gain2.connect(ctx.destination)
    osc2.start(now + 0.12)
    osc2.stop(now + 0.35)

    // Trigger haptic vibration alongside the sound
    vibrateNotification()
  } catch (err) {
    // Silently fail — sound is non-critical
    appLog.w(TAG, 'Failed to play notification sound:', err)
  }
}

export function useNotificationSound() {
  return { play: playNotificationSound }
}
