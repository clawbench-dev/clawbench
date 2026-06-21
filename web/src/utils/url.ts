/** Format a URL for display, showing host:port (omitting default HTTP/HTTPS ports). */
export function formatServerHost(url: string): string {
  try {
    const u = new URL(url)
    return u.host + (u.port && ![80, 443].includes(Number(u.port)) ? ':' + u.port : '')
  } catch {
    return url
  }
}
