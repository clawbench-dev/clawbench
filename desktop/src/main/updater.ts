import { httpGetBuffer } from './httpGet'
import { recordError } from './clientLog'

/**
 * Desktop release info fetched from the server.
 *
 * The desktop client ships in lockstep with the server, so the SERVER is the
 * source of truth for which version to run — it answers /api/desktop/latest
 * with its own version plus download URLs. That is why this does not query the
 * npm registry: the server already knows the answer, and routing through npm
 * only added a dependency on a third party (and on package size limits that the
 * Electron runtime no longer fits under).
 */

/** Platform key matching the server's `downloads` map (see detectPlatformKey in the web UI). */
export function platformKey(platform: NodeJS.Platform, arch: string): string {
  if (platform === 'win32') return 'win32-x64'
  if (platform === 'darwin') return arch === 'arm64' ? 'darwin-arm64' : 'darwin-x64'
  if (platform === 'linux') return arch === 'arm64' ? 'linux-arm64' : 'linux-x64'
  return ''
}

/** Build the /api/desktop/latest URL from the configured server. */
export function latestInfoUrl(serverUrl: string): string {
  return `${serverUrl.replace(/\/+$/, '')}/api/desktop/latest`
}

export interface DesktopLatest {
  /** The version the server reports for itself — the target desktop version. */
  version: string
  /**
   * Version of the package these URLs actually ship. Equal to `version` for an
   * ordinary release, and the PREVIOUS release's version when this release
   * reused that build untouched. The installer keys the side-by-side directory
   * off it: installing a v0.129 app under app-0.130 makes app.getVersion()
   * disagree with the pointer, and the startup handoff then deletes the
   * pointer, silently reverting the upgrade.
   */
  clientVersion: string
  /**
   * Client/server compatibility number. 0 means the server predates the field,
   * which makes the gate fall back to comparing version strings.
   */
  protocol: number
  /** GitHub release tag, or '' on a dev/untagged server (urls are then empty). */
  tag: string
  /** Candidate download URLs for the FULL package, best-first (mirror first in China, github.com last). */
  urls: string[]
  /**
   * Candidate URLs for the small payload-only archive, best-first. Empty when
   * the server publishes none for this platform — macOS has no payload, and a
   * server older than this feature reports no `payloads` field at all. An empty
   * list means "download the full package", not an error.
   */
  payloadUrls: string[]
}

interface DesktopLatestResponse {
  version?: string
  clientVersion?: string
  protocol?: number
  tag?: string
  downloads?: Record<string, string[]>
  payloads?: Record<string, string[]>
}

/**
 * GET the release info, falling back to the pinned certificate.
 *
 * Verification runs normally first, so a server with a publicly trusted
 * certificate keeps working even when a fingerprint from an earlier
 * self-signed setup is still stored for its origin. Only when that fails is the
 * remembered certificate tried — and then the peer must present EXACTLY that
 * certificate, or the request fails (see `httpGet.ts`).
 *
 * The cost for a self-signed server is one extra failed handshake per check,
 * which is what "verify, then pin" buys over "never verify".
 */
async function fetchLatestBody(url: string, pinnedFingerprint: string): Promise<Buffer> {
  try {
    return await httpGetBuffer(url)
  } catch (err) {
    if (!pinnedFingerprint) throw err
    return await httpGetBuffer(url, { pinnedFingerprint })
  }
}

/**
 * One line naming WHY the release info could not be fetched.
 *
 * All three cases end in the same user-visible outcome ("no update offered"),
 * so the log is the only place the difference is visible. The three lead to
 * very different actions: a pinned-certificate mismatch means the server's
 * certificate changed (re-trust it in Settings → Security), a TLS failure means
 * nothing is remembered for this origin yet, and a network error means the
 * server is simply not reachable right now.
 */
export function classifyFetchFailure(err: unknown): string {
  const name = (err as { name?: string })?.name ?? ''
  const message = String((err as Error)?.message ?? err)
  if (name === 'CertificatePinMismatchError' || /does not match the pinned/.test(message)) {
    return `pinned certificate mismatch (accept it again in Settings → Security): ${message}`
  }
  if (/certificate|self[- ]signed|ERR_TLS|CERT_|UNABLE_TO_VERIFY|unable to verify/i.test(message)) {
    return `TLS verification failed (no accepted certificate for this server): ${message}`
  }
  if (/ECONNREFUSED|ENOTFOUND|EAI_AGAIN|ETIMEDOUT|EHOSTUNREACH|socket hang up/i.test(message)) {
    return `server unreachable: ${message}`
  }
  return message
}

/**
 * Fetch and parse /api/desktop/latest for the running platform.
 *
 * Returns null when the server is unreachable, the response is malformed, the
 * platform has no desktop build, or the server is a dev/untagged build with no
 * release to point at — all of which mean "nothing to offer", not an error.
 *
 * `pinnedFingerprint` is the certificate the user accepted for this server
 * (`trustedCertificates[origin]`). The answer decides which version runs and
 * where it is downloaded from, so it must not be readable by anyone who can
 * answer on the server's address: without a pin, a self-signed deployment
 * simply gets no upgrade offered, exactly as before the pinning existed.
 */
export async function fetchDesktopLatest(
  serverUrl: string,
  pinnedFingerprint = '',
): Promise<DesktopLatest | null> {
  if (!serverUrl) return null

  const key = platformKey(process.platform, process.arch)
  if (!key) return null

  let body: string
  try {
    body = (await fetchLatestBody(latestInfoUrl(serverUrl), pinnedFingerprint)).toString('utf8')
  } catch (err) {
    // Server unreachable, too old to expose the endpoint, or presenting a
    // certificate that is not the pinned one — all of which mean "nothing to
    // offer", not an error the user should be shown. Logged anyway (and
    // classified): without this line a certificate change is indistinguishable
    // from "this server publishes no release", and the only symptom is that
    // upgrades silently stop being offered.
    recordError('Updater', classifyFetchFailure(err))
    return null
  }

  let info: DesktopLatestResponse
  try {
    info = JSON.parse(body)
  } catch {
    return null
  }

  const version = info.version || ''
  const urls = info.downloads?.[key] ?? []
  // An empty list means the server is a dev build with no matching release, so
  // there is nothing to install even if the version string differs.
  if (!version || urls.length === 0) return null

  // Absent for platforms the server publishes no payload for (macOS), and for
  // servers predating the feature — both mean "use the full package".
  const payloadUrls = info.payloads?.[key] ?? []

  // Both fields are absent on servers predating the protocol guard. 0 makes the
  // gate fall back to comparing versions, which is exactly the old behaviour;
  // an absent clientVersion means the download really is this release's build.
  const protocol = typeof info.protocol === 'number' ? info.protocol : 0
  const clientVersion = info.clientVersion || version

  return { version, clientVersion, protocol, tag: info.tag || '', urls, payloadUrls }
}
