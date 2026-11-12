/**
 * Capability probe for the phone's own speech engine.
 *
 * The settings switch must not simply flip on: a phone may have no TTS engine
 * at all (common on AOSP builds, TV boxes and dev boards), or an engine with
 * no Chinese voice. Either way the feature would silently do nothing and every
 * read would fall back to the server — the user would think they had chosen a
 * voice they never hear. Checking at toggle time turns that into an explicit
 * answer.
 *
 * Leaf module on purpose: it only reads the native bridge, so it can be used
 * from the settings page without dragging in playback code.
 */
import { getNative } from '@/utils/clawbenchNative'

export type SystemSpeechFailure =
  /** Not the Android app, or an older build without the bridge methods. */
  | 'no_bridge'
  /** No TextToSpeech engine installed on the device. */
  | 'no_engine'
  /** An engine exists, but no Chinese voice is installed. */
  | 'no_chinese'
  /** Initialization never reported back. */
  | 'timeout'

export type SystemSpeechProbe = { ok: true } | { ok: false; reason: SystemSpeechFailure }

/** i18n key explaining each failure to the user. */
export function systemSpeechErrorKey(reason: SystemSpeechFailure): string {
  switch (reason) {
    case 'no_bridge':
      return 'settings.items.systemSpeechNoBridge'
    case 'no_chinese':
      return 'settings.items.systemSpeechNoChinese'
    case 'timeout':
      return 'settings.items.systemSpeechTimeout'
    default:
      return 'settings.items.systemSpeechNoEngine'
  }
}

/**
 * Ask the device whether it can read Chinese aloud.
 *
 * `ttsStatus()` starts initialization and answers immediately; when it reports
 * `initializing` this waits for the one-shot `clawbench-tts-init` event instead
 * of polling. The timeout exists because that event crosses the WebView bridge
 * and could be lost on a page reload — without it the switch would hang.
 */
export async function probeSystemSpeech(timeoutMs = 5000): Promise<SystemSpeechProbe> {
  const native = getNative()
  if (typeof native?.ttsSpeak !== 'function' || typeof native?.ttsStatus !== 'function') {
    return { ok: false, reason: 'no_bridge' }
  }

  const status = native.ttsStatus()
  if (status === 'ready') return { ok: true }
  if (status === 'no_chinese') return { ok: false, reason: 'no_chinese' }
  if (status === 'unavailable') return { ok: false, reason: 'no_engine' }
  if (status !== 'initializing') return { ok: false, reason: 'no_engine' }

  return await new Promise<SystemSpeechProbe>((resolve) => {
    let settled = false
    const finish = (result: SystemSpeechProbe) => {
      if (settled) return
      settled = true
      window.removeEventListener('clawbench-tts-init', onInit)
      clearTimeout(timer)
      resolve(result)
    }
    const onInit = (e: Event) => {
      const state = (e as CustomEvent).detail?.state
      if (state === 'ready') finish({ ok: true })
      else if (state === 'no_chinese') finish({ ok: false, reason: 'no_chinese' })
      else if (state === 'unavailable') finish({ ok: false, reason: 'no_engine' })
    }
    const timer = setTimeout(() => finish({ ok: false, reason: 'timeout' }), timeoutMs)
    window.addEventListener('clawbench-tts-init', onInit)
  })
}
