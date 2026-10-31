/**
 * Whether this host can start a screen share from the page itself.
 *
 * Two independent gates decide it, and conflating them produces the wrong
 * message:
 *
 * - **Secure context.** `getDisplayMedia` is only exposed on HTTPS or
 *   localhost. The desktop shell sidesteps this with Chromium's
 *   `unsafely-treat-insecure-origin-as-secure`, a browser has no equivalent —
 *   on `http://192.168.x.x:20000` the whole `mediaDevices` object is missing.
 * - **Platform support.** Mobile browsers (Chrome for Android, Safari on iOS)
 *   expose `mediaDevices` for the microphone but have never implemented
 *   `getDisplayMedia`, so the check is on the method, not on the object.
 *
 * The desktop shell and the Android app do not need this: their capture goes
 * through native IPC (`setPendingCaptureSource` / `startScreenCapture`).
 */
export function browserScreenCaptureAvailable(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    typeof navigator.mediaDevices?.getDisplayMedia === 'function'
  )
}

/**
 * Whether the page is served from an insecure origin.
 *
 * Only used to explain *why* capture is missing: telling a user on plain HTTP
 * to update their client sends them looking in the wrong place. Defaults to
 * `false` on every uncertainty — claiming insecurity we cannot prove is worse
 * than the generic message.
 */
export function isInsecureCaptureContext(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext === false
}
