/**
 * useWakeLock
 *
 * Manages a screen wake lock to prevent the display from turning off.
 * Uses two complementary mechanisms:
 *   1. Web Wake Lock API (navigator.wakeLock) — standard, works in all modern browsers
 *   2. Native bridge (ClawBenchNative.setKeepScreenOn) — extra safety in WebView
 *
 * The wake lock is automatically released when the page becomes hidden
 * (e.g. user switches tabs). We re-acquire on visibility change if the
 * lock should still be held.
 *
 * Singleton pattern — all consumers share the same lock state.
 */

import { ref } from 'vue'
import { appLog } from '@/utils/appLog'
import { getNative } from '@/utils/clawbenchNative'

const TAG = 'WakeLock'

// --- Singleton state ---
const held = ref(false)

/** True when we *want* the lock held (even if the tab is hidden and the browser auto-released) */
let shouldHold = false

/**
 * Who is currently asking for the lock.
 *
 * A Set of reasons rather than a boolean because two unrelated features want it
 * at the same time: AI output + auto-speech, and a running meeting. With one
 * flag, whichever finished first would turn the screen back off while the other
 * still needed it. `acquire`/`release` are idempotent per reason, so a watcher
 * that fires repeatedly is harmless.
 */
const holders = new Set<string>()

let webWakeLock: WakeLockSentinel | null = null

/**
 * Bumped by every acquisition attempt.
 *
 * `navigator.wakeLock.request()` is async, and a caller may release (or
 * re-acquire) while it is still pending. An older request that resolves later
 * must not install its sentinel: it would leave a lock held with no entry in
 * `holders`, and `release()` short-circuits on `holders.delete` returning
 * false — so nothing would ever release it again and the screen would stay on
 * until the page goes away. The same applies to the native flag, which is why
 * the stale branch returns before touching it.
 */
let acquireGen = 0

/**
 * Internal: actually acquire the wake lock via Web API and/or Android bridge.
 * Does NOT check shouldHold — callers must handle that.
 */
async function _doAcquire() {
  const myGen = ++acquireGen

  // 1. Web Wake Lock API
  if ('wakeLock' in navigator) {
    try {
      const sentinel = await navigator.wakeLock.request('screen')
      if (myGen !== acquireGen || !shouldHold) {
        // Released, or superseded by a newer attempt, while we were waiting.
        try { sentinel.release() } catch { /* already gone */ }
        return
      }
      webWakeLock = sentinel
      sentinel.onrelease = () => {
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
    } catch (e) {
      appLog.w(TAG, 'Web Wake Lock acquire failed:', e)
    }
  }

  // 2. Native bridge (setKeepScreenOn) — extra safety in WebView
  try {
    const native = getNative()
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(true)
      appLog.i(TAG, 'Native setKeepScreenOn(true)')
      if (!held.value) held.value = true
    } else if (native) {
      appLog.w(TAG, 'Native bridge exists but setKeepScreenOn method missing — host needs update')
    }
  } catch { /* not in app mode */ }
}

/** Acquire the screen wake lock on behalf of `reason`. No-op if that reason already holds it. */
async function acquire(reason = 'default') {
  if (holders.has(reason)) {
    return
  }
  const first = holders.size === 0
  holders.add(reason)
  shouldHold = true
  if (!first) return
  await _doAcquire()
}

/** Release the lock on behalf of `reason`; the screen is only released once nobody else holds it. */
function release(reason = 'default') {
  if (!holders.delete(reason)) {
    return
  }
  if (holders.size > 0) {
    return
  }
  shouldHold = false

  // Web Wake Lock
  if (webWakeLock && !webWakeLock.released) {
    try {
      webWakeLock.release()
    } catch { /* already released */ }
    webWakeLock = null
  }

  // Native bridge
  try {
    const native = getNative()
    if (native?.setKeepScreenOn) {
      native.setKeepScreenOn(false)
      appLog.i(TAG, 'Native setKeepScreenOn(false)')
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

/** Test-only: module state outlives a single case, and the reasons are global. */
export function _resetWakeLockForTest() {
  holders.clear()
  shouldHold = false
  webWakeLock = null
  held.value = false
}
