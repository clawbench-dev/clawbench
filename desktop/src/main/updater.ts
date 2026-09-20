import { app } from 'electron'
import { getDesktopPkg, latestUrl, rewriteTarball, parseNpmLatest } from '../shared/registry'
import { httpGetBuffer } from './install'

export function isChinaMainland(): boolean {
  const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  return ['Asia/Shanghai', 'Asia/Chongqing', 'Asia/Urumqi', 'Asia/Harbin'].includes(tz)
}

/**
 * Compare two dotted version strings numerically.
 *
 * Returns a negative number when a < b, 0 when equal, positive when a > b.
 * The previous check was `current !== info.version`, which reports "update
 * available" when the registry is OLDER than the running build (a downgrade,
 * or a locally-built newer version) and would offer to install it.
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
  tarball: string
  integrity: string
}

export async function checkForUpdate(): Promise<UpdateInfo> {
  const pkg = getDesktopPkg(process.platform, process.arch)
  if (!pkg) return { hasUpdate: false, version: '', tarball: '', integrity: '' }
  const china = isChinaMainland()
  const json = (await httpGetBuffer(latestUrl(pkg, china))).toString('utf8')
  const info = parseNpmLatest(json)
  const current = app.getVersion()
  const hasUpdate = compareVersions(info.version, current) > 0
  return { hasUpdate, version: info.version, tarball: rewriteTarball(info.tarball, china), integrity: info.integrity }
}
