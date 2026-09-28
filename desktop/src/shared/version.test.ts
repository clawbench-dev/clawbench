import { describe, it, expect } from 'vitest'
import { normalizeVersion, isVersionedBuild, compareVersions, shouldGate } from './version'

describe('normalizeVersion', () => {
  it('strips a leading v so the server and the app agree on one version', () => {
    // The server reports `v0.99.1` (git describe) and app.getVersion() reports
    // `0.99.1` (CI rewrites desktop/package.json without the prefix).
    expect(normalizeVersion('v0.99.1')).toBe('0.99.1')
    expect(normalizeVersion('0.99.1')).toBe('0.99.1')
    expect(normalizeVersion('v0.99.1')).toBe(normalizeVersion('0.99.1'))
  })

  it('accepts an uppercase V', () => {
    expect(normalizeVersion('V1.2.3')).toBe('1.2.3')
  })

  it('trims surrounding whitespace', () => {
    expect(normalizeVersion('  v1.2.3\n')).toBe('1.2.3')
  })

  it('leaves non-version strings alone', () => {
    // `dev` is what an untagged build reports; it must survive unchanged so the
    // dev-build guards downstream still recognize it.
    expect(normalizeVersion('dev')).toBe('dev')
    expect(normalizeVersion('')).toBe('')
  })

  it('only strips a leading v, not one inside the string', () => {
    expect(normalizeVersion('1.2.3-v2')).toBe('1.2.3-v2')
  })
})

describe('isVersionedBuild', () => {
  it('accepts clean releases with or without a v prefix', () => {
    expect(isVersionedBuild('v1.0.0')).toBe(true)
    expect(isVersionedBuild('1.0.0')).toBe(true)
    // The desktop client reports its own version WITHOUT the prefix, so the
    // optional-v form is required for the gate to ever fire.
    expect(isVersionedBuild('0.99.1')).toBe(true)
  })

  it('accepts dev builds that carry a version base', () => {
    expect(isVersionedBuild('v0.66.0-5-g7702c473')).toBe(true)
    expect(isVersionedBuild('0.66.0-5-g7702c473')).toBe(true)
  })

  it('rejects unparseable builds', () => {
    expect(isVersionedBuild('dev')).toBe(false)
    expect(isVersionedBuild('a0f87a96')).toBe(false)
    expect(isVersionedBuild('')).toBe(false)
    expect(isVersionedBuild('v1.0.0garbage')).toBe(false)
  })
})

describe('compareVersions', () => {
  it('orders by numeric segment, not lexically', () => {
    expect(compareVersions('1.10.0', '1.9.0')).toBeGreaterThan(0)
    expect(compareVersions('1.9.0', '1.10.0')).toBeLessThan(0)
  })

  it('treats equal versions as equal regardless of the v prefix', () => {
    expect(compareVersions('v0.99.1', '0.99.1')).toBe(0)
    expect(compareVersions('v1.0.0', 'v1.0.0')).toBe(0)
  })

  it('pads missing segments with zero', () => {
    expect(compareVersions('1.2', '1.2.0')).toBe(0)
    expect(compareVersions('1.2.1', '1.2')).toBeGreaterThan(0)
  })

  it('strips the mmddHHMM build-time suffix before comparing', () => {
    // The old naive comparator treated `v1.0.0-07291030` as different from
    // `1.0.0`, so a same-version pair could be reported as a mismatch.
    expect(compareVersions('v1.0.0-07291030', '1.0.0')).toBe(0)
    expect(compareVersions('v0.70.0-5-g830bb6c-07291030', '0.70.0-5-g830bb6c')).toBe(0)
    expect(compareVersions('v0.70.0-5-g830bb6c-07291030', '0.70.0')).toBeGreaterThan(0)
  })

  it('treats a pre-release build as newer than its release', () => {
    // Commits after the `0.66.0` tag are newer, not older.
    expect(compareVersions('v0.66.0-5-gabc', 'v0.66.0')).toBeGreaterThan(0)
    expect(compareVersions('v0.66.0', 'v0.66.0-5-gabc')).toBeLessThan(0)
  })

  it('compares pre-release builds by their base when the bases differ', () => {
    expect(compareVersions('v0.65.0-10-gabc', 'v0.66.0-5-g7702c473')).toBeLessThan(0)
    expect(compareVersions('v0.66.0-5-gabc', 'v0.65.0-10-g7702c473')).toBeGreaterThan(0)
  })

  it('reports a downgrade as a negative difference', () => {
    expect(compareVersions('1.0.0', '1.2.0')).toBeLessThan(0)
  })

  it('parses leading digits only, never NaN', () => {
    expect(compareVersions('1a', '1')).toBe(0)
    expect(compareVersions('2beta', '1.9')).toBeGreaterThan(0)
    expect(compareVersions('dev', '1.0.0')).toBeLessThan(0)
  })
})

describe('shouldGate', () => {
  it('gates when the client is older than the server', () => {
    expect(shouldGate('0.99.0', 'v0.99.1', { packaged: true })).toBe(true)
  })

  it('gates when the client is NEWER than the server (bidirectional)', () => {
    // The behaviour change this feature makes: Android used to only flag an
    // older client, so a newer client silently proceeded.
    expect(shouldGate('0.99.2', 'v0.99.1', { packaged: true })).toBe(true)
  })

  it('does not gate when the versions agree, even across spellings', () => {
    expect(shouldGate('0.99.1', 'v0.99.1', { packaged: true })).toBe(false)
    // Same release, one side carrying build metadata.
    expect(shouldGate('0.99.1', 'v0.99.1-07291030', { packaged: true })).toBe(false)
  })

  it('never gates an unpackaged dev build', () => {
    // The repo pins desktop/package.json to a placeholder version, so a dev
    // shell would otherwise be flagged against every server it connects to.
    expect(shouldGate('0.1.0', 'v0.99.1', { packaged: false })).toBe(false)
  })

  it('fails open when either side is unparseable', () => {
    expect(shouldGate('dev', 'v0.99.1', { packaged: true })).toBe(false)
    expect(shouldGate('a0f87a96', 'v0.99.1', { packaged: true })).toBe(false)
    expect(shouldGate('0.99.1', 'dev', { packaged: true })).toBe(false)
    expect(shouldGate('0.99.1', '', { packaged: true })).toBe(false)
  })
})
