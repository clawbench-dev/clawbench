import { describe, it, expect } from 'vitest'
import { createHash } from 'node:crypto'
import { verifyIntegrity } from './integrity'

describe('verifyIntegrity', () => {
  const data = Buffer.from('hello clawbench')
  const b64 = createHash('sha512').update(data).digest('base64')
  it('accepts correct sha512 integrity', () => {
    expect(verifyIntegrity(data, `sha512-${b64}`)).toBe(true)
  })
  it('rejects wrong hash', () => {
    expect(verifyIntegrity(data, `sha512-${createHash('sha512').update('other').digest('base64')}`)).toBe(false)
  })
  it('rejects non-sha512 algorithm', () => {
    expect(verifyIntegrity(data, `sha256-${b64}`)).toBe(false)
  })
  it('rejects empty/malformed', () => {
    expect(verifyIntegrity(data, '')).toBe(false)
  })
})
