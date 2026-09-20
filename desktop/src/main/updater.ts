import { app } from 'electron'
import { getStore } from './store'
import { httpGetBuffer } from './install'

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
 */
export function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map((n) => parseInt(n, 10) || 0)
  const pb = b.split('.').map((n) => parseInt(n, 10) || 0)
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
  /** Candidate download URLs, best-first (mirror first in China, github.com last). */
  urls: string[]
}

interface DesktopLatestResponse {
  version?: string
  tag?: string
  downloads?: Record<string, string[]>
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
  const none: UpdateInfo = { hasUpdate: false, version: '', urls: [] }

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

  const current = app.getVersion()
  return { hasUpdate: compareVersions(version, current) > 0, version, urls }
}
