import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guards for window geometry persistence.
 *
 * `window.ts` imports `electron` at module scope, so no unit test can load it —
 * and this feature's failure mode is silent in both directions:
 *
 *  - If the close handler stops persisting (or saves `getBounds()` instead of
 *    `getNormalBounds()`), a maximized window reopens maximized-and-fullscreen
 *    sized, i.e. it "forgets" the restored rect, with no error.
 *  - If the BrowserWindow loses its `minWidth`/`minHeight`, the window can be
 *    dragged below `MIN_WINDOW_*`, `boundsToPersist` then rejects the rect on
 *    close and the geometry is silently never saved at all.
 *
 * `windowGeometry.test.ts` covers the pure decisions; these assert the wiring
 * that connects them, which is the only place a regression would show.
 */

const repoRoot = resolve(__dirname, '../../..')

function readRepoFile(rel: string): string {
  return readFileSync(resolve(repoRoot, rel), 'utf8')
}

const WINDOW = 'desktop/src/main/window.ts'

describe('window geometry persistence wiring', () => {
  it('registers the persistence handler on the main window', () => {
    const src = readRepoFile(WINDOW)
    expect(src).toMatch(/registerWindowGeometryPersistence\(mainWindow\)/)
  })

  it('saves getNormalBounds(), not the current bounds', () => {
    // While maximized/fullscreen, getBounds() is the screen rect. Persisting it
    // would restore a "maximized" window at full-screen size, losing the size
    // the user had before maximizing.
    const src = readRepoFile(WINDOW)
    const start = src.indexOf('function registerWindowGeometryPersistence')
    expect(start).toBeGreaterThan(-1)
    const body = src.slice(start, src.indexOf('\n}\n', start))
    expect(body).toContain('getNormalBounds()')
    expect(body).not.toMatch(/bounds:\s*win\.getBounds\(\)/)
  })

  it('gives the window the same minimum size that isValidBounds enforces', () => {
    // Without these, dragging the window smaller than MIN_WINDOW_* makes
    // boundsToPersist() return null on close, so the geometry is silently
    // dropped and every launch falls back to the centred default.
    const src = readRepoFile(WINDOW)
    const start = src.indexOf('new BrowserWindow({')
    expect(start).toBeGreaterThan(-1)
    const body = src.slice(start, src.indexOf('})', start))
    expect(body).toContain('minWidth: MIN_WINDOW_WIDTH')
    expect(body).toContain('minHeight: MIN_WINDOW_HEIGHT')
  })
})
