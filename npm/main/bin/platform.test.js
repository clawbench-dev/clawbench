import { describe, it, expect } from 'vitest'
import {
  PLATFORM_MAP,
  resolvePlatformPackage,
  resolveBinName,
} from './platform.js'

describe('resolvePlatformPackage', () => {
  it('maps android/arm64 to the PIE android binary package', () => {
    expect(resolvePlatformPackage('android', 'arm64')).toEqual({
      key: 'android-arm64',
      pkg: '@xulongzhe/clawbench-android-arm64',
    })
  })

  it('keeps linux/arm64 mapped to the linux-arm64 package', () => {
    expect(resolvePlatformPackage('linux', 'arm64')).toEqual({
      key: 'linux-arm64',
      pkg: '@xulongzhe/clawbench-linux-arm64',
    })
  })

  it('maps darwin/arm64 and linux/x64 to their packages', () => {
    expect(resolvePlatformPackage('darwin', 'arm64').pkg).toBe('@xulongzhe/clawbench-darwin-arm64')
    expect(resolvePlatformPackage('linux', 'x64').pkg).toBe('@xulongzhe/clawbench-linux-x64')
  })

  it('maps win32/x64 to the win32 package', () => {
    expect(resolvePlatformPackage('win32', 'x64').pkg).toBe('@xulongzhe/clawbench-win32-x64')
  })

  it('returns null for unsupported platform/arch combos', () => {
    expect(resolvePlatformPackage('win32', 'ia32')).toBeNull()
    expect(resolvePlatformPackage('freebsd', 'x64')).toBeNull()
    expect(resolvePlatformPackage('android', 'x64')).toBeNull()
  })
})

describe('resolveBinName', () => {
  it('uses clawbench.exe for win32', () => {
    expect(resolveBinName('win32')).toBe('clawbench.exe')
  })

  it('uses the plain name for every other platform including android', () => {
    expect(resolveBinName('android')).toBe('clawbench')
    expect(resolveBinName('linux')).toBe('clawbench')
    expect(resolveBinName('darwin')).toBe('clawbench')
  })
})

describe('PLATFORM_MAP', () => {
  it('contains an android-arm64 entry so Termux selects the PIE binary', () => {
    expect(PLATFORM_MAP['android-arm64']).toBe('@xulongzhe/clawbench-android-arm64')
  })
})
