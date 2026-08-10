import { describe, it, expect } from 'vitest'
import { getDesktopPkg, registryBase, latestUrl, rewriteTarball, parseNpmLatest } from './registry'

describe('registry', () => {
  it('maps platform+arch to npm package', () => {
    expect(getDesktopPkg('win32', 'x64')).toBe('@xulongzhe/clawbench-desktop-win32-x64')
    expect(getDesktopPkg('darwin', 'arm64')).toBe('@xulongzhe/clawbench-desktop-darwin-arm64')
    expect(getDesktopPkg('freebsd', 'x64')).toBeUndefined()
  })
  it('selects registry base by region', () => {
    expect(registryBase(true)).toContain('npmmirror')
    expect(registryBase(false)).toContain('npmjs')
  })
  it('builds latest url', () => {
    expect(latestUrl('@xulongzhe/clawbench-desktop-win32-x64', false))
      .toBe('https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/latest')
  })
  it('rewrites tarball to npmmirror in China', () => {
    const u = 'https://registry.npmjs.org/@xulongzhe/clawbench-desktop-win32-x64/-/x-0.1.0.tgz'
    expect(rewriteTarball(u, true)).toContain('npmmirror')
    expect(rewriteTarball(u, false)).toBe(u)
  })
  it('parses latest metadata', () => {
    const p = parseNpmLatest('{"version":"0.1.0","dist":{"tarball":"t.tgz","integrity":"sha512-x"}}')
    expect(p.version).toBe('0.1.0')
    expect(p.tarball).toBe('t.tgz')
    expect(() => parseNpmLatest('{}')).toThrow()
  })
})
