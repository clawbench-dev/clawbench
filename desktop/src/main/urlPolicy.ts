/** How the main window should treat a navigation target URL. */
export type UrlDisposition = 'external' | 'internal' | 'block'

/** Decide how to handle a target URL.
 *
 * - 'external' — a whitelisted scheme (http/https/mailto/tel) pointing outside
 *   the configured ClawBench server; open it in the default browser.
 * - 'internal' — a whitelisted scheme within the configured server origin;
 *   let the window navigate normally.
 * - 'block' — anything else (file:, javascript:, data:, custom schemes, or an
 *   unparseable URL); never hand it to the OS protocol handler or the window.
 */
export function classifyUrl(targetUrl: string, serverUrl?: string): UrlDisposition {
  let parsed: URL
  try {
    parsed = new URL(targetUrl)
  } catch {
    return 'block'
  }
  switch (parsed.protocol) {
    case 'http:':
    case 'https:':
    case 'mailto:':
    case 'tel:': {
      if (!serverUrl) return 'external'
      const serverOrigin = new URL(serverUrl).origin
      if (parsed.origin === serverOrigin) return 'internal'
      return 'external'
    }
    default:
      return 'block'
  }
}