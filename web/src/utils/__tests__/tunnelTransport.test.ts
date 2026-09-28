import { describe, expect, it } from 'vitest'
import { transportLabelKey } from '@/utils/tunnelTransport'

/**
 * The tunnel status copy is transport-neutral; a parenthesized annotation is
 * only appended once a single concrete wire is known. This helper decides
 * whether a value is annotatable at all — `''` (unknown) and `'both'` (a
 * preference, not a wire) must NOT resolve to a label, so the caller falls back
 * to the neutral wording instead of guessing.
 */
describe('transportLabelKey', () => {
  it('maps ssh to the SSH label key', () => {
    expect(transportLabelKey('ssh')).toBe('proxy.transportSsh')
  })

  it('maps h2 to the HTTP/2 label key', () => {
    expect(transportLabelKey('h2')).toBe('proxy.transportH2')
  })

  it('returns empty for an unknown transport', () => {
    // Web mode, or a host that predates the bridge methods.
    expect(transportLabelKey('')).toBe('')
  })

  it('returns empty for the both preference', () => {
    // 'both' is a preference, not a wire: the winner is not determinable here.
    expect(transportLabelKey('both')).toBe('')
  })

  it('returns empty for an unexpected value', () => {
    expect(transportLabelKey('tls')).toBe('')
    expect(transportLabelKey('h2c')).toBe('')
  })
})
