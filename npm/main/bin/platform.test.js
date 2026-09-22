import { describe, it, expect } from 'vitest'
import {
  PLATFORM_MAP,
  resolvePlatformPackage,
  resolveBinName,
  resolveSpawnOptions,
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

describe('resolveSpawnOptions', () => {
  // Regression guard: without detached the server joins the launcher's process
  // group and dies on terminal hangup (SSH disconnect), because `nohup` only
  // shields the launcher — Node resets SIG_IGN to SIG_DFL for itself, so the
  // SIGHUP still reaches the Go binary. The directly-installed binary has no
  // launcher layer, which is why only the npm install was affected.
  it('detaches the server on POSIX so a terminal hangup cannot kill it', () => {
    expect(resolveSpawnOptions({}, 'linux').detached).toBe(true)
    expect(resolveSpawnOptions({}, 'darwin').detached).toBe(true)
    expect(resolveSpawnOptions({}, 'android').detached).toBe(true)
  })

  // Regression guard: on Windows detached maps to DETACHED_PROCESS, which
  // strips the child's console while `stdio: "inherit"` still passes the
  // launcher's console handles. The server writes all of its console output
  // (slog -> stderr, startup banner -> stdout) through those handles, so the
  // combination silently discarded every line — including the first-run
  // password banner. Windows must spawn without detached.
  it('does not detach on Windows, where it would silently discard all output', () => {
    expect(resolveSpawnOptions({}, 'win32').detached).toBe(false)
  })

  it('defaults to the running platform', () => {
    expect(resolveSpawnOptions({}).detached).toBe(process.platform !== 'win32')
  })

  it('passes the environment through to the server', () => {
    const env = { PATH: '/usr/bin', CLAWBENCH_CHILD: '1' }
    expect(resolveSpawnOptions(env, 'linux').env).toEqual(env)
  })

  it('keeps stdio inherited so the server logs stay visible', () => {
    expect(resolveSpawnOptions({}, 'linux').stdio).toBe('inherit')
    expect(resolveSpawnOptions({}, 'win32').stdio).toBe('inherit')
  })

  it('returns a fresh object so callers cannot mutate shared state', () => {
    const first = resolveSpawnOptions({ A: '1' }, 'linux')
    const second = resolveSpawnOptions({ A: '2' }, 'linux')
    expect(first).not.toBe(second)
    expect(first.env).not.toBe(second.env)
  })
})
