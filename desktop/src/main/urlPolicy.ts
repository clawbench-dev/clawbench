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
/**
 * The app's own popup window — the member's shared screen opened in a window of
 * its own.
 *
 * `about:blank` is deliberate: the popup must stay a SECOND DOCUMENT IN THE SAME
 * RENDERER, because the picture is a live MediaStream that belongs to the
 * opener's context and is rendered into the popup's `<body>` (Teleport). A
 * navigation to a real URL would get its own process and could not see the
 * track at all.
 *
 * Only the main window's `window.open('')` produces this URL; it is exempted
 * from the "deny every popup" rule for that reason and nothing else.
 */
export function isAppPopup(targetUrl: string): boolean {
  return targetUrl === '' || targetUrl === 'about:blank'
}

/**
 * Whether a navigation inside the app popup must be blocked.
 *
 * The popup inherits this window's `preload`, so a page that opens
 * `about:blank` and then assigns `location` would run the whole native bridge
 * (`ClawBenchNative`: server list, SSH password, file download) on a foreign
 * origin. The popup only ever needs its blank document — the picture is drawn
 * into it through Teleport from the opener — so anything else is refused.
 */
export function shouldBlockPopupNavigation(targetUrl: string): boolean {
  return !isAppPopup(targetUrl)
}

