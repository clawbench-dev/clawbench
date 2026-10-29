/**
 * Share links across every address this server can be reached on.
 *
 * WHY THIS EXISTS
 * ---------------
 * Every share dialog used to build its link with `window.location.origin`,
 * which only knows the ONE address the browser happened to use. On a machine
 * with Wi-Fi (192.168.100.104), a USB Ethernet adapter (192.168.100.142) and a
 * Docker bridge (172.17.0.1), a colleague on another subnet cannot open a link
 * built from whichever address the sharer's own browser picked.
 *
 * So the server enumerates its addresses (GET /api/server/addresses) and the
 * dialog offers: ONE primary link by default, expandable to every address.
 *
 * The primary is deliberately NOT "the first address in the list". It is the
 * address the viewer's own browser is currently using, when that address is in
 * the list — because the sharer can reach the server on it right now, which is
 * the best available evidence that it works on this network. Only when the
 * current origin is not a listed address (localhost, a hostname, a reverse
 * proxy) does it fall back to the server's ordering.
 */
import { ref } from 'vue'
import { appLog } from '@/utils/appLog'

const TAG = 'ShareAddresses'

/** One reachable address, as reported by the server. */
export interface ShareAddress {
  ip: string
  /** OS interface name (e.g. "wlp1s0"); '' when unknown. */
  interface: string
  /** 'lan' | 'virtual' | 'public' | 'other' */
  kind: string
}

/** A ready-to-copy link plus the address it points at. */
export interface ShareLink {
  url: string
  address: ShareAddress
}

interface AddressResponse {
  port?: string
  addresses?: ShareAddress[]
}

/**
 * Cached per page load. The address list changes only when a NIC comes up or
 * goes down, and the dialogs are opened rarely; re-fetching on every open would
 * add a round trip to a button that should feel instant.
 *
 * A failed fetch is NOT cached, so a transient error does not pin an empty list
 * for the rest of the session.
 */
let cache: { port: string; addresses: ShareAddress[] } | null = null
let inflight: Promise<{ port: string; addresses: ShareAddress[] }> | null = null

/** Test seam: forget the cached address list. */
export function _resetShareAddressesForTest(): void {
  cache = null
  inflight = null
}

async function loadAddresses(): Promise<{ port: string; addresses: ShareAddress[] }> {
  if (cache) return cache
  if (inflight) return inflight

  inflight = (async () => {
    try {
      const resp = await fetch('/api/server/addresses', { credentials: 'same-origin' })
      if (!resp.ok) throw new Error(`addresses: ${resp.status}`)
      const data = (await resp.json()) as AddressResponse
      const out = {
        port: data.port ?? '',
        addresses: Array.isArray(data.addresses) ? data.addresses : [],
      }
      cache = out
      return out
    } catch (e) {
      // Fall back to "no extra addresses" rather than failing the dialog: the
      // primary link still works, it is just the only one offered.
      appLog.w(TAG, 'address list unavailable', e)
      return { port: '', addresses: [] }
    } finally {
      inflight = null
    }
  })()
  return inflight
}

/** Scheme + host[:port] for an address. Port is omitted when it is the default. */
function originFor(addr: ShareAddress, port: string, scheme: string): string {
  const host = addr.ip.includes(':') ? `[${addr.ip}]` : addr.ip
  const isDefault =
    (scheme === 'http' && port === '80') || (scheme === 'https' && port === '443')
  return port && !isDefault ? `${scheme}://${host}:${port}` : `${scheme}://${host}`
}

/**
 * Rank the addresses for display.
 *
 * Order: LAN/public first (those are what a colleague can actually use), then
 * virtual adapters, then IPv4 before IPv6. The server already sorts this way;
 * re-applying it here keeps the result stable when the current origin is
 * promoted to the front.
 */
function rank(a: ShareAddress): number {
  if (a.kind === 'lan' || a.kind === 'public') return 0
  if (a.kind === 'virtual') return 1
  return 2
}

function isV4(ip: string): boolean {
  return !ip.includes(':')
}

/**
 * Build every link for a share path.
 *
 * `path` is the server-relative share path, e.g. `/share/abc123`.
 *
 * Returns the primary link first (the one the copy button uses) followed by the
 * remaining addresses. When the server reports no addresses, this degrades to
 * exactly the old behaviour: a single link built from `window.location.origin`.
 */
export async function buildShareLinks(path: string): Promise<ShareLink[]> {
  const origin = window.location.origin
  const { port, addresses } = await loadAddresses()

  if (addresses.length === 0) {
    return [
      {
        url: origin + path,
        address: { ip: window.location.hostname, interface: '', kind: 'current' },
      },
    ]
  }

  const scheme = window.location.protocol === 'https:' ? 'https' : 'http'
  // The port the browser is using is authoritative: it is the one that works
  // right now, and it survives a port-forward where the server's own port
  // differs from the public one.
  const effectivePort = window.location.port || port

  const currentHost = window.location.hostname
  const links: ShareLink[] = addresses.map((a) => ({
    url: originFor(a, effectivePort, scheme) + path,
    address: a,
  }))

  // Promote the address the browser itself used. Matching on the bare host
  // (not origin) keeps this working behind a proxy that terminates TLS.
  const currentIdx = links.findIndex((l) => l.address.ip === currentHost)
  if (currentIdx > 0) {
    const [primary] = links.splice(currentIdx, 1)
    links.unshift(primary)
  } else if (currentIdx < 0) {
    // The browser is on localhost or a hostname that is not one of the
    // enumerated addresses. Keep the browser's own origin as the primary: the
    // sharer can definitely open it, and the enumerated list follows.
    links.unshift({
      url: origin + path,
      address: { ip: currentHost, interface: '', kind: 'current' },
    })
  }

  // Stable secondary ordering: LAN/public, then virtual, IPv4 before IPv6.
  // The primary is pinned at index 0 regardless.
  const [primary, ...rest] = links
  rest.sort((a, b) => {
    const ra = rank(a.address)
    const rb = rank(b.address)
    if (ra !== rb) return ra - rb
    const va = isV4(a.address.ip)
    const vb = isV4(b.address.ip)
    if (va !== vb) return va ? -1 : 1
    return a.address.ip.localeCompare(b.address.ip)
  })

  return [primary, ...rest]
}

/**
 * Reactive wrapper for dialogs: `primary` is what the copy button uses, `all`
 * is everything (including the primary) for the expandable list.
 */
export function useShareLinks() {
  const all = ref<ShareLink[]>([])
  const loading = ref(false)

  async function load(path: string): Promise<void> {
    loading.value = true
    try {
      all.value = await buildShareLinks(path)
    } finally {
      loading.value = false
    }
  }

  return {
    all,
    loading,
    primary: () => all.value[0]?.url ?? '',
    load,
  }
}
