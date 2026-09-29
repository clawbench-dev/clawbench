import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guard: the viewport meta must opt in to `interactive-widget=resizes-content`.
 *
 * Chrome 108+ defaults to `resizes-visual`, which shrinks only the *visual*
 * viewport when the on-screen keyboard opens and leaves the *layout* viewport at
 * full height. The app shell is `position: fixed; inset: 0`, so it stays
 * full-height too, the visual viewport can still be panned inside it, and
 * panning reveals a blank band exactly as tall as the keyboard (reported on
 * Android Chrome browser + PWA; Android WebView was unaffected because
 * `adjustResize` really shrinks the layout viewport).
 *
 * `resizes-content` makes the layout viewport shrink as well — i.e. the viewport
 * behaves as the user expects, with no scrollable blank area. It also makes
 * `innerHeight` shrink, which turns the JS keyboard compensation in
 * useChatKeyboard / useTerminalViewport into a no-op instead of double-counting.
 *
 * iOS Safari does not support the keyword and ignores it, keeping its previous
 * behaviour; the meta must therefore not be gated on a UA.
 */
describe('viewport meta', () => {
  function viewportContent(): string {
    const html = readWebFile('index.html')
    const m = html.match(/<meta\s+name="viewport"\s+content="([^"]*)"/)
    if (!m) throw new Error('viewport meta not found in index.html')
    return m[1]
  }

  it('opts in to resizes-content so the layout viewport shrinks with the keyboard', () => {
    expect(viewportContent()).toMatch(/interactive-widget=resizes-content/)
  })

  it('does not fall back to resizes-visual or overlays-content', () => {
    // Either would leave the layout viewport full-height and restore the bug.
    const content = viewportContent()
    expect(content).not.toMatch(/interactive-widget=resizes-visual/)
    expect(content).not.toMatch(/interactive-widget=overlays-content/)
  })

  it('keeps the pre-existing viewport behaviour intact', () => {
    // The keyword is additive — dropping viewport-fit=cover would re-break the
    // safe-area layout, and the scale locks are relied on elsewhere.
    const content = viewportContent()
    expect(content).toMatch(/width=device-width/)
    expect(content).toMatch(/initial-scale=1\.0/)
    expect(content).toMatch(/maximum-scale=1\.0/)
    expect(content).toMatch(/user-scalable=no/)
    expect(content).toMatch(/viewport-fit=cover/)
  })
})
