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
})
