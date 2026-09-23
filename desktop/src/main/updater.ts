import { app } from 'electron'
import { getStore } from './store'
import { httpGetBuffer } from './install'
import { normalizeVersion } from '../shared/version'

/**
 * Desktop self-upgrade check.
 *
 * The desktop client ships in lockstep with the server, so the SERVER is the
 * source of truth for which version to run — it answers /api/desktop/latest
 * with its own version plus download URLs. That is why this no longer queries
 * the npm registry: the server already knows the answer, and routing through
 * npm only added a dependency on a third party (and on package size limits that
 * the Electron runtime no longer fits under).
 */

/**
 * Compare two dotted version strings numerically.
 *
 * Returns a negative number when a < b, 0 when equal, positive when a > b.
 * A plain `!==` check would report "update available" when the server is OLDER
 * than the running build (a downgrade) and offer to install it.
 *
 * Segments are parsed from the NORMALIZED form, because the server reports
 * `v0.99.1` while app.getVersion() reports `0.99.1`, and `parseInt('v1')` is
 * NaN (coerced to 0). Without normalization `compareVersions('v1.0.0', '0.99.1')`
 * is negative, so the update check would go permanently silent the moment the
 * project ships v1.0.0.
 */
export function compareVersions(a: string, b: string): number {
  const pa = normalizeVersion(a).split('.').map((n) => parseInt(n, 10) || 0)
  const pb = normalizeVersion(b).split('.').map((n) => parseInt(n, 10) || 0)
  const len = Math.max(pa.length, pb.length)
  for (let i = 0; i < len; i++) {
    const d = (pa[i] || 0) - (pb[i] || 0)
    if (d !== 0) return d
  }
  return 0
}

export interface UpdateInfo {
  hasUpdate: boolean
  version: string
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

export async function checkForUpdate(): Promise<UpdateInfo> {
  const none: UpdateInfo = { hasUpdate: false, version: '', urls: [], payloadUrls: [] }

  const serverUrl = getStore().get('serverUrl')
  if (!serverUrl) return none

  const key = platformKey(process.platform, process.arch)
  if (!key) return none

  let body: string
  try {
    body = (await httpGetBuffer(latestInfoUrl(serverUrl))).toString('utf8')
  } catch {
    // Server unreachable or too old to expose the endpoint — nothing to offer.
    return none
  }

  let info: DesktopLatestResponse
  try {
    info = JSON.parse(body)
  } catch {
    return none
  }

  const version = info.version || ''
  const urls = info.downloads?.[key] ?? []
  // An empty list means the server is a dev build with no matching release, so
  // there is nothing to install even if the version string differs.
  if (!version || urls.length === 0) return none

  // Absent for platforms the server publishes no payload for (macOS), and for
  // servers predating the feature — both mean "use the full package".
  const payloadUrls = info.payloads?.[key] ?? []

  const current = app.getVersion()
  return { hasUpdate: compareVersions(version, current) > 0, version, urls, payloadUrls }
}
