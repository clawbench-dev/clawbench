/**
 * Share-link validation, kept free of Electron so it can be unit tested.
 *
 * The client opens these links in a window that can capture the screen and be
 * remote-controlled, so the URL is checked before a window is created rather
 * than after.
 */

/** True when the URL points at a share page this app can host. */
export function isShareUrl(raw: string): boolean {
  try {
    const url = new URL(raw)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return false
    return /^\/share\/[^/]+\/?$/.test(url.pathname)
  } catch {
    return false
  }
}
