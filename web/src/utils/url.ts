/** Format a URL for display, showing protocol://host:port. */
export function formatServerHost(url: string): string {
  try {
    const u = new URL(url)
    return u.origin
  } catch {
    return url
  }
}

/**
 * Shaped like an IPv6 literal (hex digits, dots, colons) and with at least two
 * colons — a dotted quad like `192.168.1.100` is hex-plus-dots too and is
 * checked for a port first. Full validation is `new URL()`'s job below.
 */
function isIPv6Literal(text: string): boolean {
  return (text.match(/:/g) || []).length >= 2 && /^[0-9A-Fa-f:.]+$/.test(text)
}

/**
 * Return `text` as a URL the platform can actually load, or '' when it is not
 * an address we understand.
 *
 * The rules mirror buildServerUrl() in the clients' url-utils.js, because the
 * address saved here is later handed to the native connectToServer() and to the
 * clients' own login page. IPv6 is the reason this exists: `new URL()` needs
 * the literal bracketed (`http://[240e:...::1]:20000`), and a raw
 * `https://240e:...::1:20000` is not a URL at all.
 *
 * An IPv6 address must be written in full — scheme, brackets AND port:
 *
 *   https://host:20000            -> unchanged (path/query dropped)
 *   http://[240e:...::123]:20000  -> unchanged
 *   https://[::1]:8443            -> unchanged
 *   [240e:...::123]:20000         -> '' (no scheme)
 *   http://[240e:...::123]        -> '' (no port)
 *   240e:390:...::123             -> '' (no brackets: no way to add a port)
 *   192.168.1.100 / example.com   -> https://… (port defaults to 20000)
 */
export function normalizeServerUrl(text: string): string {
  let rest = text.trim()
  if (!rest) return ''

  let protocol = 'https'
  let hasScheme = false
  const scheme = rest.match(/^([A-Za-z][A-Za-z0-9+.-]*):\/\//)
  if (scheme) {
    const name = scheme[1].toLowerCase()
    if (name !== 'http' && name !== 'https') return ''
    protocol = name
    hasScheme = true
    rest = rest.slice(scheme[0].length)
  }

  // Path, query and fragment are meaningless for a server address.
  rest = rest.split(/[/?#]/)[0]

  let host = rest
  let port = '20000'

  const bracketed = rest.match(/^\[([^\]]+)\](?::(\d+))?$/)
  if (bracketed) {
    // IPv6 is never guessed at: all three parts must be there, because a
    // half-written literal is exactly the address that cannot be loaded.
    if (!hasScheme || !isIPv6Literal(bracketed[1]) || !bracketed[2]) return ''
    host = `[${bracketed[1]}]`
    port = bracketed[2]
  } else if (isIPv6Literal(rest)) {
    // A bare literal has nowhere to attach a port, so it is never complete.
    return ''
  } else {
    const portMatch = rest.match(/^(.*):(\d+)$/)
    if (portMatch) {
      host = portMatch[1]
      port = portMatch[2]
    }
    // Digits only: `new URL('https://20800:20000')` silently reads '20800' as
    // a compressed IPv4 address, which is never what was typed.
    if (!/^[A-Za-z0-9._-]+$/.test(host) || /^[0-9]+$/.test(host)) return ''
  }

  // `new URL()` is the validity check: it rejects malformed IPv6 literals and
  // empty hosts. Its `origin` is what gets saved (scheme lowercased, IPv6
  // normalised), and is exactly the shape the clients' parser accepts.
  try {
    return new URL(`${protocol}://${host}:${port}`).origin
  } catch {
    return ''
  }
}
