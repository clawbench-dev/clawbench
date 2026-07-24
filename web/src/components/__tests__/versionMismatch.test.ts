import { describe, expect, it } from 'vitest'
import { normalizeVersion, isReleaseVersion, isVersionedBuild, extractBaseVersion, shouldShowMismatch, compareVersions } from '@/utils/version'

describe('version utils', () => {
  describe('normalizeVersion', () => {
    it('strips build time suffix', () => {
      expect(normalizeVersion('v1.0.0 (2026-05-21 10:30:00)')).toBe('v1.0.0')
    })

    it('strips build time suffix with dev version', () => {
      expect(normalizeVersion('v0.30.0-30-g830bb6c (2026-05-21 10:30:00)')).toBe('v0.30.0-30-g830bb6c')
    })

    it('returns version unchanged when no suffix', () => {
      expect(normalizeVersion('v1.0.0')).toBe('v1.0.0')
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
  })

  describe('shouldShowMismatch', () => {
    it('returns true when release versions are different and APK is older', () => {
      expect(shouldShowMismatch('v1.0.0', 'v2.0.0')).toBe(true)
    })

    it('returns false when release versions match', () => {
      expect(shouldShowMismatch('v1.0.0', 'v1.0.0')).toBe(false)
    })

    it('returns false when server has build time suffix but release versions match', () => {
      expect(shouldShowMismatch('v1.0.0', 'v1.0.0 (2026-05-21 10:30:00)')).toBe(false)
    })

    it('returns true when server with build time is a different (newer) release', () => {
      expect(shouldShowMismatch('v1.0.0', 'v2.0.0 (2026-05-21 10:30:00)')).toBe(true)
    })

    it('returns false when APK is newer than server', () => {
      expect(shouldShowMismatch('v2.0.0', 'v1.0.0')).toBe(false)
    })

    it('returns true when APK dev build is older than server dev build', () => {
      expect(shouldShowMismatch('v0.65.0-10-gabc', 'v0.66.0-5-g7702c473')).toBe(true)
    })

    it('returns false when APK dev build is same base as server', () => {
      // Same base version (0.66.0), just different commit counts — no upgrade needed
      expect(shouldShowMismatch('v0.66.0-3-gabc', 'v0.66.0-5-g7702c473')).toBe(false)
    })

    it('returns true when APK dev build is same base but APK release is older', () => {
      // APK v0.65.0 (release) vs Server v0.66.0 (dev): different base, APK older
      expect(shouldShowMismatch('v0.65.0', 'v0.66.0-5-g7702c473')).toBe(true)
    })

    it('returns true for APK dev build vs server release with higher version', () => {
      expect(shouldShowMismatch('v0.66.0-5-g7702c473', 'v1.0.0')).toBe(true)
    })

    it('returns false when APK release is newer than server dev build', () => {
      expect(shouldShowMismatch('v1.0.0', 'v0.66.0-5-g7702c473')).toBe(false)
    })

    it('handles server version with build time suffix', () => {
      expect(shouldShowMismatch('v0.65.0-10-gabc', 'v0.66.0-5-g7702c473 (2026-07-24 22:42:18)')).toBe(true)
    })

    it('skips check for short hash server version', () => {
      expect(shouldShowMismatch('v1.0.0', 'a0f87a96')).toBe(false)
    })

    it('skips check for short hash app version', () => {
      expect(shouldShowMismatch('a0f87a96', 'v1.0.0')).toBe(false)
    })

    it('skips check when app version is empty', () => {
      expect(shouldShowMismatch('', 'v1.0.0')).toBe(false)
    })

    it('skips check when server version is empty', () => {
      expect(shouldShowMismatch('v1.0.0', '')).toBe(false)
    })
  })
})
