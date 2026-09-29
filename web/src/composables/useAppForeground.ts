import { ref } from 'vue'

// Module-level singleton — all consumers share the same state.
const appInForeground = ref(true)
let initialized = false

// Resume listeners. These fire on every entry into the foreground — including
// the case where the foreground STATE never changed because the background
// signal was lost (see notifyAppResume below). A consumer that must re-sync on
// return (e.g. reload the current session) cannot rely on a state transition,
// which is why this is a separate signal from the `appInForeground` ref.
const resumeListeners: Array<() => void> = []

/**
 * Subscribe to app-resume events. The callback fires every time the app enters
 * the foreground, with no arguments and with no dedup against the previous
 * state. Returns an unsubscribe function.
 *
 * This is the correct signal for "re-sync the current session" work, because
 * the foreground state may never have flipped to background: Android WebView
 * freezes the page while paused, so the JS call that reports the background
 * transition can be dropped entirely. When that happens the state is still
 * `true` on return, a state-transition signal would never fire, and the session
 * would stay stale until the user manually switched away and back. The native
 * onResume hook (window.__clawbenchAppResume) fires unconditionally, so the
 * resume signal does too.
 *
 * Consumers read the current state from `appInForeground` when they need it;
 * there is deliberately no state-transition listener anymore (the one
 * consumer, the chat session re-sync, needs the unconditional signal).
 */
export function onAppResume(cb: () => void): () => void {
  resumeListeners.push(cb)
  return () => {
    const idx = resumeListeners.indexOf(cb)
    if (idx !== -1) resumeListeners.splice(idx, 1)
  }
}

// Timestamp of the last fired resume. Used to collapse the redundant pair of
// signals Android can produce for one resume (the native bridge AND a
// visibilitychange, when the WebView did manage to flip to hidden). A genuine
// second resume within this window would re-run idempotent work only, so
// collapsing it is free; it also prevents a visible double refresh.
const RESUME_DEDUPE_MS = 1000
let lastResumeAt = 0

function setForeground(fg: boolean) {
  if (appInForeground.value === fg) return
  appInForeground.value = fg
}

function fireResume() {
  const now = Date.now()
  // Math.max guards against a backward wall-clock jump (NTP): a negative delta
  // would otherwise read as "within the dedupe window" and suppress resumes
  // until the clock caught up.
  if (Math.max(0, now - lastResumeAt) < RESUME_DEDUPE_MS) return
  lastResumeAt = now
  for (const cb of resumeListeners) {
    try {
      cb()
    } catch {
      // A listener error must never break the resume signal chain.
    }
  }
}

/**
 * The app returned to the foreground. Authoritative and unconditional — the
 * native host calls this on every onResume, whether or not the matching
 * onPause notification ever reached JS.
 */
function notifyAppResume() {
  setForeground(true)
  fireResume()
}

/**
 * Reliable app foreground/background signal for the WebView frontend.
 *
 * Background signal sources (both are installed; they can fire for the same
 * physical resume, which is what RESUME_DEDUPE_MS collapses):
 * 1. Native host push: Android MainActivity.onPause calls the injected global
 *    `window.__setAppForeground(false)`, and onResume calls
 *    `window.__clawbenchAppResume()`. This is authoritative —
 *    document.visibilityState is unreliable in Android WebView (onPause() does
 *    not reliably flip it to 'hidden').
 * 2. The Page Visibility API: the only source on desktop browsers and in the
 *    Electron shell, which have no native bridge.
 *
 * The completion paths in the chat UI read the foreground STATE to decide
 * whether marking a just-completed session read is a genuine user action (app
 * is visible) or a background auto-refresh that must NOT clear the unread badge
 * — the floating status window shows unread sessions only while the app is in
 * the background. The RESUME signal drives the "re-sync the current session"
 * work on return.
 *
 * Both globals are installed unconditionally. They are function *hooks the
 * host calls*, so the host cannot be the one to define them: the previous
 * `if (typeof window.__setAppForeground === 'function')` guard meant neither
 * side ever created it (the host only ever calls it, guarded by the same
 * typeof check), so the bridge silently never worked and the foreground
 * re-sync never ran on Android. The host-side globals it *does* define itself
 * (e.g. __clawbenchBackHandled, JSErrorInjector) follow the same
 * install-then-call shape.
 */
export function useAppForeground() {
  if (!initialized) {
    initialized = true
    const w = window as unknown as {
      __setAppForeground?: (fg: boolean) => void
      __clawbenchAppResume?: () => void
    }
    w.__setAppForeground = setForeground
    w.__clawbenchAppResume = notifyAppResume
    // `visible` is treated as a resume (not merely a state change) so desktop
    // browsers and the Electron shell still re-sync on tab/window return.
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') notifyAppResume()
      else setForeground(false)
    })
  }
  return { appInForeground }
}
