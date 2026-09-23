import { describe, it, expect } from 'vitest'
import { normalizeVersion } from './version'

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
