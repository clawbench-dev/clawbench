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
import { localConfig } from '@/composables/useSettingsConfig'

const TAG = 'NotificationSound'

let audioCtx: AudioContext | null = null

/**
 * Minimum gap between two chimes, in ms.
 *
 * The chime itself is ~350ms long, so anything arriving inside this window
 * would overlap it and turn into noise rather than an alert. One forge poll
 * derives events in a loop (forge_syncer.go drains a batch per repo), so a
 * sync touching 20 items used to fire 20 overlapping chimes. The
 * notifications themselves are already deduped per item — this only stops the
 * sound from stacking. A genuinely separate notification arriving after the
 * window still plays.
 */
const SOUND_DEDUPE_WINDOW_MS = 800

let lastPlayedAt = 0

/**
 * Test-only: reset the module's cached state (the coalescing window and the
 * lazily-created AudioContext) so each case starts from a clean slate. Without
 * this, the first case that plays also caches the context, and later cases see
 * a constructor that is never called again.
 */
export function _resetSoundThrottleForTesting() {
  lastPlayedAt = 0
  audioCtx = null
}

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
  // Respect the notification sound setting — skip if user disabled it
  if (localConfig.notificationSound === false) return

  // Coalesce a burst into one chime. Checked before touching the AudioContext
  // so a suppressed call does no work at all. Vibration is skipped with it —
  // a burst of 20 haptic pulses is the same problem.
  const nowMs = Date.now()
  if (nowMs - lastPlayedAt < SOUND_DEDUPE_WINDOW_MS) return
  lastPlayedAt = nowMs

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
  } catch (err: unknown) {
    // Silently fail — sound is non-critical
    appLog.w(TAG, 'Failed to play notification sound:', err)
  }
}

/**
 * Tones for the continuous-conversation cues, as [frequency Hz, start offset s,
 * duration s]. Deliberately different from the notification chime in both shape
 * and pitch: "sent" rises, "closed" falls, "interrupted" is a flat double
 * tick — so all three are distinguishable without looking at the screen, which
 * is the whole point of the mode.
 */
const VOICE_CUE_TONES: Record<'send' | 'interrupt' | 'exit', ReadonlyArray<readonly [number, number, number]>> = {
  send: [[880, 0, 0.07], [1244.5, 0.07, 0.09]],
  // Flat, not a glide: "I heard you, it stopped." The two pulses are identical
  // on purpose — an upward or downward contour would be read as "sent" or
  // "closed", the two things this is NOT. Pitched at G5 and held for 80 ms
  // each: the first version (587 Hz, 55 ms) was so short and low that it could
  // pass for a click, especially right after the speaker stops.
  interrupt: [[784, 0, 0.08], [784, 0.1, 0.08]],
  exit: [[659.25, 0, 0.1], [440, 0.11, 0.18]],
}

/**
 * Creates/resumes the cue AudioContext from inside a user gesture.
 *
 * Mobile browsers gate Web Audio on a gesture. The cues themselves fire from
 * speech recognition and turn events — never a gesture — so under a strict
 * autoplay policy the context would be created `suspended` and the later
 * `resume()` calls would be rejected, leaving the whole mode silent while
 * everything else still looks fine. Called when the user turns the mode on,
 * which is a tap.
 */
export function unlockVoiceAudio() {
  try {
    const ctx = getAudioContext()
    if (ctx.state !== 'suspended') return
    void ctx.resume().catch((err: unknown) => {
      appLog.w(TAG, 'Failed to unlock audio context for voice cues:', err)
    })
  } catch (err: unknown) {
    appLog.w(TAG, 'Failed to create audio context for voice cues:', err)
  }
}

/**
 * Audible feedback for continuous conversation (连续对话).
 *
 * The user is not looking at the screen while talking, so "the loop took my
 * sentence", "the interruption landed" and "the mode is off now" all need an
 * answer that is not visual. Played at a lower gain than the completion chime:
 * these fire mid-conversation and must not sound like an alert.
 *
 * Deliberately NOT gated on `notificationSound`: that switch is about task
 * completion alerts (its description is "avoid interrupting Bluetooth audio"),
 * while these are part of the voice mode the user just turned on. Silently
 * dropping them would leave the mode with no feedback at all.
 */
export function playVoiceModeCue(kind: 'send' | 'interrupt' | 'exit') {
  try {
    const ctx = getAudioContext()
    if (ctx.state === 'suspended') {
      // Reported rather than ignored: a rejected resume leaves the mode silent
      // with nothing else on screen saying so, which is exactly the kind of
      // report this line answers.
      void ctx.resume().catch((err: unknown) => {
        appLog.w(TAG, 'Failed to resume audio context for voice cue:', err)
      })
    }
    const now = ctx.currentTime
    // "The interruption landed" is the one cue that fires over a reply that was
    // just cut short, so it carries a little more gain; the other two already
    // stand out because nothing else is playing.
    const gainValue = kind === 'interrupt' ? 0.26 : 0.18
    appLog.d(TAG, `voice cue=${kind} ctx=${ctx.state} at=${now.toFixed(2)}`)
    for (const [freq, offset, duration] of VOICE_CUE_TONES[kind]) {
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = 'sine'
      osc.frequency.value = freq
      gain.gain.setValueAtTime(gainValue, now + offset)
      gain.gain.exponentialRampToValueAtTime(0.01, now + offset + duration)
      osc.connect(gain)
      gain.connect(ctx.destination)
      osc.start(now + offset)
      osc.stop(now + offset + duration)
    }
  } catch (err: unknown) {
    // Silently fail — the cue is non-critical
    appLog.w(TAG, 'Failed to play voice mode cue:', err)
  }
}

export function useNotificationSound() {
  return { play: playNotificationSound }
}
