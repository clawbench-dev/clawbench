import { describe, it, expect } from 'vitest'
import { compareVersions, normalizeVersion, isReleaseVersion, isVersionedBuild, extractBaseVersion, shouldShowMismatch } from '../version'

describe('normalizeVersion', () => {
  it('strips build time suffix (mmddHHMM format)', () => {
    expect(normalizeVersion('v1.0.0-07291030')).toBe('v1.0.0')
  })

  it('strips build time suffix with dev version', () => {
    expect(normalizeVersion('v0.30.0-30-g830bb6c-07291030')).toBe('v0.30.0-30-g830bb6c')
  })

  it('returns version unchanged when no suffix', () => {
    expect(normalizeVersion('v1.0.0')).toBe('v1.0.0')
  })

  it('does not strip short numeric suffix that is not mmddHHMM', () => {
    expect(normalizeVersion('v1.0.0-5')).toBe('v1.0.0-5')
  })

  it('handles short hash version', () => {
    expect(normalizeVersion('a0f87a96')).toBe('a0f87a96')
  })

  it('handles empty string', () => {
    expect(normalizeVersion('')).toBe('')
  })
})

describe('isReleaseVersion', () => {
  it('matches formal release versions', () => {
    expect(isReleaseVersion('v1.0.0')).toBe(true)
    expect(isReleaseVersion('v2.3.1')).toBe(true)
    expect(isReleaseVersion('v10.20.30')).toBe(true)
  })

  it('rejects pre-release / dev versions with suffix', () => {
    expect(isReleaseVersion('v0.30.0-30-g830bb6c')).toBe(false)
    expect(isReleaseVersion('v1.0.0-alpha')).toBe(false)
    expect(isReleaseVersion('v1.0.0-rc.1')).toBe(false)
  })

  it('rejects short hash', () => {
    expect(isReleaseVersion('a0f87a96')).toBe(false)
  })

  it('rejects plain dev', () => {
    expect(isReleaseVersion('dev')).toBe(false)
  })

  it('rejects empty string', () => {
    expect(isReleaseVersion('')).toBe(false)
  })
})

describe('isVersionedBuild', () => {
  it('accepts clean release versions', () => {
    expect(isVersionedBuild('v1.0.0')).toBe(true)
    expect(isVersionedBuild('v2.3.1')).toBe(true)
  })

  it('accepts dev builds with vX.Y.Z base', () => {
    expect(isVersionedBuild('v0.30.0-30-g830bb6c')).toBe(true)
    expect(isVersionedBuild('v0.66.0-5-g7702c473')).toBe(true)
  })

  it('accepts version with mmddHHMM build-time suffix', () => {
    expect(isVersionedBuild('v0.66.0-07291030')).toBe(true)
  })

  it('rejects short hash', () => {
    expect(isVersionedBuild('a0f87a96')).toBe(false)
  })

  it('rejects plain dev', () => {
    expect(isVersionedBuild('dev')).toBe(false)
  })

  it('rejects empty string', () => {
    expect(isVersionedBuild('')).toBe(false)
  })

  it('rejects versions without v prefix', () => {
    expect(isVersionedBuild('1.0.0')).toBe(false)
  })

  it('rejects garbage after patch number without dash', () => {
    expect(isVersionedBuild('v1.0.0garbage')).toBe(false)
  })
})

describe('extractBaseVersion', () => {
  it('extracts base from dev build', () => {
    expect(extractBaseVersion('v0.66.0-5-g7702c473')).toBe('v0.66.0')
  })

  it('returns clean release unchanged', () => {
    expect(extractBaseVersion('v1.0.0')).toBe('v1.0.0')
  })

  it('handles version without match', () => {
    expect(extractBaseVersion('dev')).toBe('dev')
  })

  it('extracts base from build with mmddHHMM build-time suffix', () => {
    expect(extractBaseVersion('v0.66.0-5-g7702c473-07291030')).toBe('v0.66.0')
  })

  it('rejects garbage after patch number', () => {
    expect(extractBaseVersion('v1.0.0garbage')).toBe('v1.0.0garbage')
  })
})

describe('shouldShowMismatch', () => {
  it('returns true when APK is older than server', () => {
    expect(shouldShowMismatch('v1.0.0', 'v2.0.0')).toBe(true)
  })

  it('returns false when versions match', () => {
    expect(shouldShowMismatch('v1.0.0', 'v1.0.0')).toBe(false)
  })

  it('returns false when APK is newer than server', () => {
    expect(shouldShowMismatch('v2.0.0', 'v1.0.0')).toBe(false)
  })

  it('returns true for dev build APK older than server', () => {
    expect(shouldShowMismatch('v0.65.0-10-gabc', 'v0.66.0-5-g7702c473')).toBe(true)
  })

  it('returns false when same base dev builds', () => {
    expect(shouldShowMismatch('v0.66.0-3-gabc', 'v0.66.0-5-g7702c473')).toBe(false)
  })

  it('returns false when app version is empty', () => {
    expect(shouldShowMismatch('', 'v1.0.0')).toBe(false)
  })

  it('returns false when server version is empty', () => {
    expect(shouldShowMismatch('v1.0.0', '')).toBe(false)
  })

  it('skips check for short hash versions', () => {
    expect(shouldShowMismatch('a0f87a96', 'v1.0.0')).toBe(false)
    expect(shouldShowMismatch('v1.0.0', 'a0f87a96')).toBe(false)
  })

  it('handles versions with mmddHHMM build time suffix', () => {
    expect(shouldShowMismatch('v1.0.0', 'v1.0.0-07291030')).toBe(false)
    expect(shouldShowMismatch('v1.0.0', 'v2.0.0-07291030')).toBe(true)
  })
})

describe('compareVersions', () => {
  it('returns 0 for equal versions', () => {
    expect(compareVersions('1.0.0', '1.0.0')).toBe(0)
    expect(compareVersions('v1.0.0', '1.0.0')).toBe(0)
    expect(compareVersions('v1.0.0', 'v1.0.0')).toBe(0)
  })

  it('returns -1 when a < b', () => {
    expect(compareVersions('1.0.0', '1.0.1')).toBe(-1)
    expect(compareVersions('1.0.0', '1.1.0')).toBe(-1)
    expect(compareVersions('1.0.0', '2.0.0')).toBe(-1)
    expect(compareVersions('v1.0.0', '1.0.1')).toBe(-1)
  })

  it('returns 1 when a > b', () => {
    expect(compareVersions('1.0.1', '1.0.0')).toBe(1)
    expect(compareVersions('1.1.0', '1.0.0')).toBe(1)
    expect(compareVersions('2.0.0', '1.99.99')).toBe(1)
  })

  it('handles different segment counts', () => {
    expect(compareVersions('1.0', '1.0.0')).toBe(0)
    expect(compareVersions('1.0.1', '1.0')).toBe(1)
    expect(compareVersions('1.0', '1.0.1')).toBe(-1)
  })

  it('handles dev/non-release versions', () => {
    expect(compareVersions('0', '1.0.0')).toBe(-1)
  })

  it('handles non-numeric version parts (NaN → 0, matching Go parseVersionPart)', () => {
    expect(compareVersions('dev', '1.0.0')).toBe(-1)
    expect(compareVersions('a0f87a96', '1.0.0')).toBe(-1)
    expect(compareVersions('1.0.0', 'dev')).toBe(1)
  })

  it('strips mmddHHMM build-time suffix before comparison', () => {
    expect(compareVersions('v1.0.0-07291030', '1.0.0')).toBe(0)
    expect(compareVersions('v0.70.0-5-g830bb6c-07291030', '0.70.0-5-g830bb6c')).toBe(0)
    expect(compareVersions('v0.70.0-5-g830bb6c-07291030', '0.70.0')).toBe(1)
  })

  it('pre-release builds are newer than the same release version', () => {
    expect(compareVersions('v0.66.0-5-gabc', 'v0.66.0')).toBe(1)
    expect(compareVersions('v0.66.0', 'v0.66.0-5-gabc')).toBe(-1)
  })

  it('same base with different pre-release suffixes are equal', () => {
    expect(compareVersions('v0.66.0-3-gabc', 'v0.66.0-5-g7702c473')).toBe(0)
  })

  it('pre-release builds with different base versions compare by base', () => {
    expect(compareVersions('v0.65.0-10-gabc', 'v0.66.0-5-g7702c473')).toBe(-1)
    expect(compareVersions('v0.66.0-5-gabc', 'v0.65.0-10-g7702c473')).toBe(1)
  })

  it('dev build with mmddHHMM build-time suffix strips correctly', () => {
    expect(compareVersions('v0.66.0-5-gabc-07291030', 'v0.66.0-5-gabc')).toBe(0)
  })

  it('short hash with mmddHHMM suffix strips correctly', () => {
    expect(compareVersions('a0f87a96-07291030', 'a0f87a96')).toBe(0)
  })

  it('dev with mmddHHMM suffix strips correctly', () => {
    expect(compareVersions('dev-07291030', 'dev')).toBe(0)
  })
})
