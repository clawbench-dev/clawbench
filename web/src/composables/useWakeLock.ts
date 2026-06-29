/**
 * useWakeLock
 *
 * Manages a screen wake lock to prevent the display from turning off.
 * Uses two complementary mechanisms:
 *   1. Web Wake Lock API (navigator.wakeLock) — standard, works in all modern browsers
 *   2. Android native bridge (AndroidNative.setKeepScreenOn) — extra safety in WebView
 *
 * The wake lock is automatically released when the page becomes hidden
 * (e.g. user switches tabs). We re-acquire on visibility change if the
 * lock should still be held.
 *
 * Singleton pattern — all consumers share the same lock state.
 */

import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'WakeLock'

// --- Singleton state ---
const held = ref(false)

/** True when we *want* the lock held (even if the tab is hidden and the browser auto-released) */
let shouldHold = false

let webWakeLock: WakeLockSentinel | null = null

/**
 * Internal: actually acquire the wake lock via Web API and/or Android bridge.
 * Does NOT check shouldHold — callers must handle that.
 */
async function _doAcquire() {
  // 1. Web Wake Lock API
  try {
    if ('wakeLock' in navigator) {
      webWakeLock = await navigator.wakeLock.request('screen')
      webWakeLock.onrelease = () => {
        appLog.i(TAG, 'Web Wake Lock released by browser')
        webWakeLock = null
        // If we still want the lock (e.g. browser auto-released on visibility change),
        // don't clear held — the visibilitychange handler will re-acquire.
        if (!shouldHold) {
          held.value = false
        }
      }
      held.value = true
      appLog.i(TAG, 'Web Wake Lock acquired')
    } else {
      appLog.d(TAG, 'Web Wake Lock API not available')
    }
  } catch (e) {
    appLog.w(TAG, 'Web Wake Lock acquire failed:', e)
  }

  // 2. Android native bridge (extra safety — works even when WebView doesn't focus)
  try {
    const native = (window as any).AndroidNative
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(true)
      appLog.i(TAG, 'Android setKeepScreenOn(true)')
      // Even if Web API failed, Android bridge may succeed
      if (!held.value) held.value = true
    } else if (native) {
      appLog.w(TAG, 'Android bridge exists but setKeepScreenOn method missing — APP needs update')
    }
  } catch { /* not in app mode */ }
}

/** Acquire the screen wake lock. No-op if already held. */
async function acquire() {
  if (shouldHold) {
    appLog.d(TAG, 'acquire skipped: already held')
    return
  }
  shouldHold = true
  await _doAcquire()
}

/** Release the screen wake lock via all acquired mechanisms. */
function release() {
  shouldHold = false

  // Web Wake Lock
  if (webWakeLock && !webWakeLock.released) {
    try {
      webWakeLock.release()
    } catch { /* already released */ }
    webWakeLock = null
  }

  // Android native bridge
  try {
    const native = (window as any).AndroidNative
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(false)
      appLog.i(TAG, 'Android setKeepScreenOn(false)')
    }
  } catch { /* not in app mode */ }

  if (held.value) {
    appLog.i(TAG, 'Wake lock released')
  }
  held.value = false
}

// Re-acquire wake lock when page becomes visible again.
// The Web Wake Lock API auto-releases when the page is hidden,
// so we need to re-acquire if we still want it held.
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && shouldHold && !webWakeLock) {
      _doAcquire()
    }
  })
}

export function useWakeLock() {
  return { held, acquire, release }
}
