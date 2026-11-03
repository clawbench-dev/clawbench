import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Wiring guard for the desktop's "leave the page?" prompt.
 *
 * `window.ts` imports `electron` at module scope, so no unit test can load it.
 * The failure mode here is silent in the worst direction: without a
 * `will-prevent-unload` handler Electron drops the unload with NO dialog at
 * all, so the renderer's meeting guard reads as a dead F5 key and a close
 * button that does nothing. Pinned at the source, following
 * `certificateWiring.test.ts`.
 */

const repoRoot = resolve(__dirname, '../../..')
const WINDOW = 'desktop/src/main/window.ts'

function windowSrc(): string {
  return readFileSync(resolve(repoRoot, WINDOW), 'utf8')
}

describe('desktop unload guard', () => {
  it('answers will-prevent-unload instead of dropping the unload silently', () => {
    const src = windowSrc()
    expect(src).toContain("on('will-prevent-unload'")
    expect(src).toContain('dialog.showMessageBoxSync')
  })

  it('registers the handler when the window is created', () => {
    expect(windowSrc()).toContain('registerUnloadGuard(mainWindow)')
  })

  it('lets the unload through only when the user chooses to leave', () => {
    const src = windowSrc()
    // Esc / closing the dialog must keep the meeting: cancelId is the stay button.
    expect(src).toMatch(/cancelId:\s*1/)
    expect(src).toMatch(/if \(choice === 0\) event\.preventDefault\(\)/)
  })
})
