import { describe, it, expect, vi } from 'vitest'

// updater.ts imports electron for app.getVersion(); only compareVersions is
// under test here.
vi.mock('electron', () => ({
  app: { getVersion: () => '1.0.0' },
}))

import { compareVersions } from './updater'

describe('compareVersions', () => {
  it('orders by each numeric segment, not lexically', () => {
    // The whole point: a naive string compare puts "1.10.0" before "1.9.0".
    expect(compareVersions('1.10.0', '1.9.0')).toBeGreaterThan(0)
    expect(compareVersions('1.9.0', '1.10.0')).toBeLessThan(0)
  })

  it('treats equal versions as equal', () => {
    expect(compareVersions('1.2.3', '1.2.3')).toBe(0)
  })

  it('pads missing segments with zero', () => {
    expect(compareVersions('1.2', '1.2.0')).toBe(0)
    expect(compareVersions('1.2.1', '1.2')).toBeGreaterThan(0)
  })

  it('reports a downgrade as NOT an update', () => {
    // Regression guard: the old `current !== latest` check reported
    // "update available" when the registry was older than the running build.
    expect(compareVersions('1.0.0', '1.2.0')).toBeLessThan(0)
  })

  it('handles major/minor/patch boundaries', () => {
    expect(compareVersions('2.0.0', '1.99.99')).toBeGreaterThan(0)
    expect(compareVersions('1.0.1', '1.0.0')).toBeGreaterThan(0)
  })
})
