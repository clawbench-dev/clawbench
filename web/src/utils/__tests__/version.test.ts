import { describe, it, expect } from 'vitest'
import { compareVersions } from '../version'

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

  it('strips build-time suffix before comparison', () => {
    expect(compareVersions('v1.0.0 (2026-07-24 10:30:00)', '1.0.0')).toBe(0)
    expect(compareVersions('v1.0.0 (2026-07-24 10:30:00)', '1.0.1')).toBe(-1)
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

  it('dev build with build-time suffix strips correctly', () => {
    expect(compareVersions('v0.66.0-5-gabc (2026-07-24)', 'v0.66.0-5-gabc')).toBe(0)
  })
})
