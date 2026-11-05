import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { CLIENT_PROTOCOL } from './protocol'

/**
 * The desktop constant must match `internal/version/client_protocol.txt`.
 *
 * Three sides read that one file (Go embeds it, Android's Gradle reads it
 * directly, the desktop mirrors it here). A drift is silent and one-directional:
 * the desktop would keep accepting servers it should reject (or vice versa) and
 * nothing in the build would notice.
 */
function repoFile(rel: string): string {
  for (const base of [process.cwd(), resolve(process.cwd(), '..')]) {
    try {
      return readFileSync(resolve(base, rel), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

describe('CLIENT_PROTOCOL', () => {
  it('matches the repository-wide protocol file', () => {
    const raw = repoFile('internal/version/client_protocol.txt').trim()
    expect(raw).toMatch(/^[0-9]+$/)
    expect(Number(raw)).toBe(CLIENT_PROTOCOL)
  })

  it('is a positive integer', () => {
    // Zero would make every server look "unknown" and silently fall back to the
    // strict version comparison this number exists to replace.
    expect(Number.isInteger(CLIENT_PROTOCOL)).toBe(true)
    expect(CLIENT_PROTOCOL).toBeGreaterThan(0)
  })
})
