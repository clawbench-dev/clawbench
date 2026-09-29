/**
 * Window chrome decision for the desktop shell.
 *
 * Kept free of electron imports so the decision table is unit-testable, the
 * same way `zoom.ts` and `shortcuts.ts` are.
 *
 * Why Windows/Linux only
 * ----------------------
 * On macOS the window controls are the "traffic lights" in the TOP-LEFT, drawn
 * by the system, and users have decades of muscle memory for them. The app's
 * own controls live in the TOP-RIGHT (the Windows/Linux convention), so the two
 * platforms want opposite things: macOS keeps its native frame, while
 * Windows/Linux drop it and let the web UI draw the buttons.
 *
 * macOS could hide just the title bar while keeping the traffic lights
 * (`titleBarStyle: 'hidden'`), but then the header's right side would hold no
 * controls at all — an empty cluster, or a header that silently differs by
 * platform. Keeping the plain native frame is the smaller, more predictable
 * surface.
 */

/** Platforms that get a frameless window with app-drawn controls. */
const FRAMELESS_PLATFORMS = ['win32', 'linux']

/**
 * Whether the shell should create a frameless window, which is also exactly
 * when the renderer shows the app-drawn minimize/maximize/close cluster.
 *
 * Defaults to `false` for anything unrecognised: a frameless window without a
 * working control cluster is unusable (no way to close or move it), so the
 * unknown case must fail toward the native frame.
 */
export function shouldUseFramelessWindow(platform: NodeJS.Platform | string): boolean {
  return FRAMELESS_PLATFORMS.includes(platform)
}
