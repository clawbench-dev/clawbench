import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guard for the main window's popup policy.
 *
 * `window.ts` imports `electron` at module scope, so no unit test can load it
 * (same convention as `certificateWiring.test.ts`). The decision itself —
 * "which URL is the app's own popup" — is covered by `urlPolicy.test.ts`; what
 * is pinned here is that the handler actually consults it, and that the one
 * allowed window is a normal, movable window rather than a frameless clone of
 * the app shell.
 *
 * Failure modes this catches: the popup silently denied again (the button looks
 * dead), and the picture window inheriting `frame: false` from the frameless app
 * window (it cannot be moved or resized by hand).
 */

const repoRoot = resolve(__dirname, '../../..')
const WINDOW = 'desktop/src/main/window.ts'

function handlerBlock(): string {
  const src = readFileSync(resolve(repoRoot, WINDOW), 'utf8')
  const start = src.indexOf('setWindowOpenHandler')
  expect(start, 'no setWindowOpenHandler in window.ts').toBeGreaterThan(-1)
  const end = src.indexOf("mainWindow.on('closed'", start)
  expect(end, 'handler block is not terminated').toBeGreaterThan(start)
  return src.slice(start, end)
}

describe('main window popup policy', () => {
  it('allows the app popup before falling through to the deny', () => {
    const block = handlerBlock()
    expect(block).toContain('isAppPopup(target)')
    expect(block).toContain("action: 'allow'")
    expect(block).toContain("action: 'deny'")
    // Order matters: an early deny would make the allow branch unreachable.
    expect(block.indexOf("action: 'allow'")).toBeLessThan(block.indexOf("action: 'deny'"))
  })

  it('gives the picture window a real frame', () => {
    const block = handlerBlock()
    // The app window is frameless; the popup must not inherit that, or it has
    // no title bar to drag and no resize border.
    expect(block).toMatch(/frame:\s*true/)
  })

  it('still sends external links to the browser instead of opening a window', () => {
    const block = handlerBlock()
    expect(block).toContain('openExternal(target)')
  })
})
