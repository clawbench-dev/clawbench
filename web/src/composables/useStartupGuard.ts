// Startup guard: run app initialization and guarantee the native splash is
// dismissed, whatever happens.
//
// Background (issue #449): the Android app could stay on the native splash
// ("正在初始化应用…") forever. The splash is dismissed by JS via
// ClawBenchNative.dismissSplash(), and the only two call sites sat *after*
// `await initializeApp()` in the mount/login handlers. Any exception thrown
// inside initialization rejected the async handler before reaching
// dismissSplash(), so nothing ever removed the overlay — the connection-timeout
// guard in the native layer is already disarmed once the page loads.
//
// This guard centralizes the invariant: no matter whether initialization
// succeeds, returns false, or throws, the splash comes down and the user is
// left on a usable screen (the app, or the login page with an error toast).

export interface StartupGuardOptions {
  /** Run the real initialization. Returns false on a handled fatal error. */
  initialize: () => Promise<boolean>
  /** Dismiss the native splash overlay. Must be idempotent; no-op outside app mode. */
  dismissSplash: () => void
  /** Report a thrown error to the user (toast) and to the log relay. */
  onError: (err: unknown) => void
  /**
   * Runs only when initialization succeeded, immediately before the splash is
   * dismissed. Callers flip `isAuthenticated` here so the app UI is already
   * mounted behind the fading splash — dismissing first would briefly expose
   * the login view during the fade-out.
   */
  onReady?: () => void
}

/**
 * Run `initialize()` and always dismiss the splash afterwards.
 *
 * Returns true when initialization succeeded (callers may then continue with
 * the post-init UI work). Returns false when it failed or threw — the splash is
 * already dismissed, so the caller must NOT keep waiting for it.
 *
 * Never throws: an exception from `initialize()` is reported via `onError`.
 */
export async function guardStartupWithSplash(opts: StartupGuardOptions): Promise<boolean> {
  const { initialize, dismissSplash, onError, onReady } = opts
  let ok = false
  try {
    if (await initialize()) {
      // onReady runs inside the try so a throw from the ready hook is treated
      // as a startup failure (reported + splash still dismissed) rather than
      // escaping and stranding the overlay.
      onReady?.()
      ok = true
    }
  } catch (err) {
    onError(err)
  } finally {
    // Single exit point: the splash must never outlive startup, whether
    // initialization succeeded, returned false, or threw. dismissSplash() is
    // idempotent, so a duplicate call is harmless.
    dismissSplash()
  }
  return ok
}
