import { describe, it, expect, afterEach } from 'vitest'
import {
  DEFAULT_INSTANCE,
  INSTANCE_FLAG,
  _resetInstanceForTests,
  appRootFor,
  currentInstanceArgs,
  instanceIdFor,
  isInsideDir,
  normalizeRoot,
  parseInstanceFlag,
  profileUserData,
  resolveInstanceProfile,
  setActiveInstanceId,
  type InstanceInput,
} from './instanceProfile'

/**
 * The instance rules, without Electron.
 *
 * Getting these wrong is silent: a copy either shares the original's settings
 * (and the single-instance lock makes it inert), or it loses the user's
 * settings after an upgrade because the id was derived from a path that moved.
 */

afterEach(() => _resetInstanceForTests())

describe('parseInstanceFlag', () => {
  it('accepts both spellings', () => {
    expect(parseInstanceFlag(['exe', '--instance', 'work'])).toBe('work')
    expect(parseInstanceFlag(['exe', '--instance=work'])).toBe('work')
  })

  it('is empty when absent, blank, or unsafe', () => {
    expect(parseInstanceFlag(['exe'])).toBe('')
    expect(parseInstanceFlag(['exe', '--instance'])).toBe('')
    expect(parseInstanceFlag(['exe', '--instance', ''])).toBe('')
    // The id becomes a directory name: these must never reach the filesystem.
    expect(parseInstanceFlag(['exe', '--instance', '../evil'])).toBe('')
    expect(parseInstanceFlag(['exe', '--instance', 'a/b'])).toBe('')
    expect(parseInstanceFlag(['exe', '--instance', 'x'.repeat(33)])).toBe('')
  })
})

describe('appRootFor', () => {
  it('is the directory holding the executable', () => {
    expect(appRootFor('/opt/ClawBench/clawbench-desktop', 'linux')).toBe('/opt/ClawBench')
    expect(appRootFor('C:\\Apps\\ClawBench\\clawbench-desktop.exe', 'win32')).toBe('C:\\Apps\\ClawBench')
  })

  it('identifies a macOS install by its .app bundle', () => {
    // Two copies normally sit side by side in /Applications, so the containing
    // directory is shared and cannot be the identity.
    expect(appRootFor('/Applications/ClawBench.app/Contents/MacOS/clawbench-desktop', 'darwin')).toBe(
      '/Applications/ClawBench.app',
    )
    expect(appRootFor('/Users/x/Applications/ClawBench.app/Contents/MacOS/clawbench-desktop', 'darwin')).toBe(
      '/Users/x/Applications/ClawBench.app',
    )
  })

  it('falls back to the executable directory for a non-bundle macOS binary', () => {
    expect(appRootFor('/Users/x/bin/clawbench-desktop', 'darwin')).toBe('/Users/x/bin')
  })
})

describe('normalizeRoot', () => {
  it('folds case on the case-insensitive platforms only', () => {
    expect(normalizeRoot('C:\\Apps\\ClawBench', 'win32')).toBe('c:\\apps\\clawbench')
    expect(normalizeRoot('/Users/x/Applications', 'darwin')).toBe('/users/x/applications')
    expect(normalizeRoot('/opt/ClawBench', 'linux')).toBe('/opt/ClawBench')
  })

  it('drops a trailing separator so two spellings match', () => {
    expect(normalizeRoot('/opt/ClawBench/', 'linux')).toBe('/opt/ClawBench')
  })
})

describe('isInsideDir', () => {
  it('matches the directory itself and anything below it', () => {
    expect(isInsideDir('/home/u/.clawbench-desktop', '/home/u/.clawbench-desktop', 'linux')).toBe(true)
    expect(isInsideDir('/home/u/.clawbench-desktop/app-1.0.0', '/home/u/.clawbench-desktop', 'linux')).toBe(true)
    expect(isInsideDir('/home/u/.clawbench-desktop-copy', '/home/u/.clawbench-desktop', 'linux')).toBe(false)
  })
})

describe('instanceIdFor', () => {
  it('is stable for a directory and different for a copy', () => {
    const a = instanceIdFor('/opt/ClawBench', 'linux')
    expect(a).toBe(instanceIdFor('/opt/ClawBench', 'linux'))
    expect(a).toMatch(/^[0-9a-f]{8}$/)
    expect(instanceIdFor('/opt/ClawBench - 副本', 'linux')).not.toBe(a)
  })

  it('ignores case on Windows so the same folder cannot become two copies', () => {
    expect(instanceIdFor('C:\\Apps\\ClawBench', 'win32')).toBe(instanceIdFor('c:\\apps\\clawbench', 'win32'))
  })
})

describe('profileUserData', () => {
  it('leaves the default profile where it was', () => {
    expect(profileUserData('/home/u/.config/ClawBench', DEFAULT_INSTANCE)).toBe('/home/u/.config/ClawBench')
  })

  it('puts a copy beside it, not inside it', () => {
    expect(profileUserData('/home/u/.config/ClawBench', 'abcd1234')).toBe('/home/u/.config/ClawBench-abcd1234')
  })
})

const base: InstanceInput = {
  flagId: '',
  isPackaged: true,
  execPath: '/opt/ClawBench/clawbench-desktop',
  platform: 'linux',
  defaultUserData: '/home/u/.config/ClawBench',
  installRoot: '/home/u/.clawbench-desktop',
  primaryRoot: '',
  primaryRootExists: false,
}

describe('resolveInstanceProfile', () => {
  it('claims the primary slot on the first run of an install', () => {
    const p = resolveInstanceProfile(base)
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.userData).toBe(base.defaultUserData)
    expect(p.claimPrimaryRoot).toBe('/opt/ClawBench')
    expect(p.isCopy).toBe(false)
  })

  it('keeps the default profile for the recorded primary install', () => {
    const p = resolveInstanceProfile({
      ...base,
      primaryRoot: '/opt/ClawBench',
      primaryRootExists: true,
    })
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.claimPrimaryRoot).toBe('')
  })

  it('gives a copied directory its own profile', () => {
    const p = resolveInstanceProfile({
      ...base,
      execPath: '/opt/ClawBench - 副本/clawbench-desktop',
      primaryRoot: '/opt/ClawBench',
      primaryRootExists: true,
    })
    expect(p.id).toBe(instanceIdFor('/opt/ClawBench - 副本', 'linux'))
    expect(p.userData).toBe(`${base.defaultUserData}-${p.id}`)
    expect(p.isCopy).toBe(true)
    expect(p.claimPrimaryRoot).toBe('')
  })

  it('treats a vanished primary directory as a MOVE, keeping the settings', () => {
    const p = resolveInstanceProfile({
      ...base,
      execPath: '/opt/Moved/clawbench-desktop',
      primaryRoot: '/opt/ClawBench',
      primaryRootExists: false,
    })
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.userData).toBe(base.defaultUserData)
    expect(p.claimPrimaryRoot).toBe('/opt/Moved')
  })

  it('treats the shared version store as the same installation', () => {
    const p = resolveInstanceProfile({
      ...base,
      execPath: '/home/u/.clawbench-desktop/app-0.150.0/clawbench-desktop',
      primaryRoot: '/opt/ClawBench',
      primaryRootExists: true,
    })
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.claimPrimaryRoot).toBe('')
  })

  it('never forks a profile for a development run', () => {
    const p = resolveInstanceProfile({
      ...base,
      isPackaged: false,
      primaryRoot: '/opt/ClawBench',
      primaryRootExists: true,
    })
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.claimPrimaryRoot).toBe('')
  })

  it('lets an explicit id win — a handoff runs from the shared store', () => {
    const p = resolveInstanceProfile({
      ...base,
      flagId: 'abcd1234',
      execPath: '/home/u/.clawbench-desktop/app-0.150.0/clawbench-desktop',
    })
    expect(p.id).toBe('abcd1234')
    expect(p.userData).toBe(`${base.defaultUserData}-abcd1234`)
    expect(p.claimPrimaryRoot).toBe('')
    expect(p.isCopy).toBe(true)
  })

  it('treats --instance default as the default profile', () => {
    const p = resolveInstanceProfile({ ...base, flagId: DEFAULT_INSTANCE })
    expect(p.id).toBe(DEFAULT_INSTANCE)
    expect(p.userData).toBe(base.defaultUserData)
    expect(p.isCopy).toBe(false)
  })

  it('resolves a macOS copy from its own .app bundle', () => {
    const p = resolveInstanceProfile({
      ...base,
      platform: 'darwin',
      execPath: '/Applications/ClawBench 副本.app/Contents/MacOS/clawbench-desktop',
      primaryRoot: '/Applications/ClawBench.app',
      primaryRootExists: true,
    })
    expect(p.isCopy).toBe(true)
    expect(p.userData).toBe(`${base.defaultUserData}-${p.id}`)
  })
})

describe('currentInstanceArgs', () => {
  it('adds nothing for the default instance', () => {
    expect(currentInstanceArgs()).toEqual([])
  })

  it('carries a copy id into the next process', () => {
    setActiveInstanceId('abcd1234')
    expect(currentInstanceArgs()).toEqual([INSTANCE_FLAG, 'abcd1234'])
  })
})
