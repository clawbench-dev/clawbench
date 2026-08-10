/** npm platform package names for clawbench-desktop, mirroring internal/service/upgrade.go. */
export const DESKTOP_PLATFORM_PKG: Record<string, string> = {
  'linux/amd64': '@xulongzhe/clawbench-desktop-linux-x64',
  'linux/arm64': '@xulongzhe/clawbench-desktop-linux-arm64',
  'darwin/amd64': '@xulongzhe/clawbench-desktop-darwin-x64',
  'darwin/arm64': '@xulongzhe/clawbench-desktop-darwin-arm64',
  'win32/x64': '@xulongzhe/clawbench-desktop-win32-x64',
}

export function getDesktopPkg(platform: NodeJS.Platform, arch: string): string | undefined {
  return DESKTOP_PLATFORM_PKG[`${platform}/${arch}`]
}

export function registryBase(chinaMainland: boolean): string {
  return chinaMainland ? 'https://registry.npmmirror.com' : 'https://registry.npmjs.org'
}

/** Build the /latest query URL for a pkg. */
export function latestUrl(pkg: string, chinaMainland: boolean): string {
  return `${registryBase(chinaMainland)}/${pkg}/latest`
}

/** Rewrite an npmjs tarball URL to the npmmirror CDN, mirroring upgrade.go. */
export function rewriteTarball(url: string, chinaMainland: boolean): string {
  if (chinaMainland && url.startsWith('https://registry.npmjs.org')) {
    return url.replace('https://registry.npmjs.org', 'https://registry.npmmirror.com')
  }
  return url
}

export interface NpmLatest {
  version: string
  tarball: string
  integrity: string
}

export function parseNpmLatest(data: string): NpmLatest {
  const j = JSON.parse(data)
  if (!j || typeof j.version !== 'string' || !j.dist?.tarball) {
    throw new Error('invalid registry response')
  }
  return { version: j.version, tarball: j.dist.tarball, integrity: j.dist.integrity || '' }
}
