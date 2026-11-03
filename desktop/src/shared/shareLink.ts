/**
 * Share-link validation, kept free of Electron so it can be unit tested.
 *
 * The client opens these links in a window that can capture the screen and be
 * remote-controlled, so the URL is checked before a window is created rather
 * than after.
 */

/** True when the URL points at a share page of the configured server. */
export function isShareUrl(raw: string, serverUrl?: string): boolean {
  try {
    const url = new URL(raw)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return false
    if (!/^\/share\/[^/]+\/?$/.test(url.pathname)) return false
    // Origin, not just shape: the window this opens carries the app's preload
    // (native capture, remote control, stored credentials). Accepting any host
    // let a pasted link from another origin run script with that capability.
    if (!serverUrl) return false
    return url.origin === new URL(serverUrl).origin
  } catch {
    return false
  }
}
