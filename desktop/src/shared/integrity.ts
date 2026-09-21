import { createHash } from 'node:crypto'

/** Verify a buffer against a SRI integrity string of the form "sha512-<base64>". */
export function verifyIntegrity(buffer: Buffer, integrity: string): boolean {
  if (!integrity.startsWith('sha512-')) return false
  const expected = Buffer.from(integrity.slice('sha512-'.length), 'base64')
  const actual = createHash('sha512').update(buffer).digest()
  return expected.length === actual.length && expected.equals(actual)
}
