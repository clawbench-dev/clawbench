/** Format a URL for display, showing protocol://host:port. */
export function formatServerHost(url: string): string {
  try {
    const u = new URL(url)
    return u.origin
  } catch {
    return url
  }
}
