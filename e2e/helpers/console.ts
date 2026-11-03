/**
 * Browser-level diagnostics that are NOT product errors.
 *
 * `ResizeObserver loop completed with undelivered notifications` (older
 * Chromium wording: `ResizeObserver loop limit exceeded`) is reported by the
 * browser whenever a ResizeObserver callback changes layout in a way that
 * queues another notification within the same frame. The browser aborts that
 * loop and delivers the remaining notifications on the next frame, so no
 * observation is lost and no product state is wrong — the spec explicitly
 * allows this ("deliver notifications, then run another loop").
 *
 * Chromium and Firefox surface it as a console warning; WebKit surfaces it as
 * a real `pageerror`. Specs that fail on ANY page error therefore go red on
 * WebKit only, for a reason that says nothing about the app. Filtering it here
 * keeps the "no console errors" assertion meaningful for everything else.
 */
export function isBenignPageError(text: string): boolean {
  return /ResizeObserver loop (completed with undelivered notifications|limit exceeded)/.test(text)
}
