/** Format a URL for display, showing host:port (omitting default HTTP/HTTPS ports). */
export function formatServerHost(url: string): string {
  try {
    const u = new URL(url)
    return u.host
  } catch {
    return url
  }
}
