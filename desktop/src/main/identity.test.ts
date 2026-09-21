import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { APP_USER_MODEL_ID } from './identity'

/**
 * The Windows toast identity is declared in two places that cannot see each
 * other at runtime:
 *   - desktop/electron-builder.yml `appId` — baked into the Start Menu shortcut
 *     and the exe's version resource at build time
 *   - APP_USER_MODEL_ID — passed to app.setAppUserModelId() at startup
 *
 * electron-builder.yml is NOT shipped inside app.asar, so the app cannot read
 * it back; and Windows only shows a toast for an AUMID that resolves to an
 * installed shortcut. If the two drift, notifications silently stop appearing
 * on Windows with no error anywhere. Hence this guard.
 */
describe('Windows toast identity', () => {
  it('APP_USER_MODEL_ID matches the electron-builder appId', () => {
    const yml = readFileSync(resolve(__dirname, '../../electron-builder.yml'), 'utf8')
    const m = yml.match(/^appId:\s*(\S+)\s*$/m)
    expect(m, 'electron-builder.yml must declare a top-level appId').not.toBeNull()

    expect(m![1]).toBe(APP_USER_MODEL_ID)
  })

  it('is a reverse-DNS style id (a bare word would collide across apps)', () => {
    // Windows uses the AUMID to attribute and group notifications; a generic id
    // like "ClawBench" is not unique and can collide with another install.
    expect(APP_USER_MODEL_ID).toMatch(/^[a-z0-9]+(\.[a-z0-9-]+)+$/)
    expect(APP_USER_MODEL_ID).toContain('.')
  })
})
