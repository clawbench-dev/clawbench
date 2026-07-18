import { describe, expect, it } from 'vitest'
import { normalizeVersion, isReleaseVersion, shouldShowMismatch } from '@/utils/version'

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

  describe('shouldShowMismatch', () => {
    it('returns true when release versions are different', () => {
      expect(shouldShowMismatch('v1.0.0', 'v2.0.0')).toBe(true)
    })

    it('returns false when release versions match', () => {
      expect(shouldShowMismatch('v1.0.0', 'v1.0.0')).toBe(false)
    })

    it('returns false when server has build time suffix but release versions match', () => {
      expect(shouldShowMismatch('v1.0.0', 'v1.0.0 (2026-05-21 10:30:00)')).toBe(false)
    })

    it('returns true when server with build time is a different release', () => {
      expect(shouldShowMismatch('v1.0.0', 'v2.0.0 (2026-05-21 10:30:00)')).toBe(true)
    })

    it('skips check for dev server version', () => {
      expect(shouldShowMismatch('v1.0.0', 'v0.30.0-30-g830bb6c')).toBe(false)
    })

    it('skips check for dev server version with build time', () => {
      expect(shouldShowMismatch('v1.0.0', 'v0.30.0-30-g830bb6c (2026-05-21 10:30:00)')).toBe(false)
    })

    it('skips check for dev app version', () => {
      expect(shouldShowMismatch('v0.30.0-30-g830bb6c', 'v1.0.0')).toBe(false)
    })

    it('skips check for short hash versions', () => {
      expect(shouldShowMismatch('a0f87a96', 'b1f88b07')).toBe(false)
    })

    it('skips check when app version is empty', () => {
      expect(shouldShowMismatch('', 'v1.0.0')).toBe(false)
    })

    it('skips check when server version is empty', () => {
      expect(shouldShowMismatch('v1.0.0', '')).toBe(false)
    })
  })
})
