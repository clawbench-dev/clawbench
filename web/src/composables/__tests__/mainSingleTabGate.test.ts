import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Guards against a regression that silently removes the single-tab gate.
//
// The gate is the only thing preventing a second tab from opening a WebSocket.
// Without it, two tabs share one server-side subscription slot and repeatedly
// replace each other's connection, and each replacement triggers a full state
// resync (measured: 1,402 reconnects / 17 min → ~2,900 requests/min, making
// every page slow). main.ts mounts a Vue app, so it has no importable surface
// to unit-test directly — these assertions read the source instead.

// The web source root differs by invocation (repo root vs web/), so probe both
// rather than assuming process.cwd().
function readMain(): string {
  const candidates = [
    resolve(process.cwd(), 'src/main.ts'),
    resolve(process.cwd(), 'web/src/main.ts'),
  ]
  for (const p of candidates) {
    try {
      return readFileSync(p, 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error(`could not locate main.ts from cwd=${process.cwd()}`)
}

describe('main.ts single-tab gate', () => {
  const src = readMain()

  it('acquires the single-tab guard before mounting the app', () => {
    expect(src).toContain('createSingleTabGuard')
    const acquireAt = src.indexOf('guard.acquire()')
    const mountAt = src.indexOf("app.mount('#app')")
    expect(acquireAt).toBeGreaterThan(-1)
    expect(mountAt).toBeGreaterThan(-1)
    expect(acquireAt).toBeLessThan(mountAt)
  })

  it('mounts only the blocking screen when another tab owns the app', () => {
    expect(src).toContain('SingleTabBlocked')
    expect(src).toContain('if (!ownsTab)')
  })

  it('releases ownership on page unload so a waiting tab can take over', () => {
    expect(src).toContain('guard.release()')
    expect(src).toContain('pagehide')
  })

  it('fails open when the guard throws, so the app never hard-blocks on an error', () => {
    // A guard bug must not lock the user out of the app entirely.
    expect(src).toMatch(/catch[\s\S]{0,200}ownsTab = true/)
  })
})
