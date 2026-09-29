import { httpGetBuffer } from './install'

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
  tag?: string
  downloads?: Record<string, string[]>
  payloads?: Record<string, string[]>
}

/**
 * Fetch and parse /api/desktop/latest for the running platform.
 *
 * Returns null when the server is unreachable, the response is malformed, the
 * platform has no desktop build, or the server is a dev/untagged build with no
 * release to point at — all of which mean "nothing to offer", not an error.
 */
export async function fetchDesktopLatest(serverUrl: string): Promise<DesktopLatest | null> {
  if (!serverUrl) return null

  const key = platformKey(process.platform, process.arch)
  if (!key) return null

  let body: string
  try {
    body = (await httpGetBuffer(latestInfoUrl(serverUrl))).toString('utf8')
  } catch {
    // Server unreachable or too old to expose the endpoint — nothing to offer.
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

  return { version, tag: info.tag || '', urls, payloadUrls }
}
