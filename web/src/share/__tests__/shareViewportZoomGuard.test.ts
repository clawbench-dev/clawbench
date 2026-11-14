import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard: the share page must not let the BROWSER move the page around.
 *
 * Two ways it can, both reported as "一拖动/放大，整个 Header 和背景全消失，就只剩
 * 下那张图":
 *
 *   1. Page-level pinch zoom. `.share-topbar` is the first row of a fixed flex
 *      column, so a browser pinch (and the pan that follows) carries it off the
 *      screen. On a phone this happens without meaning to — the bar sits 44px
 *      above the draw.io canvas, so one finger of a two-finger pinch lands on
 *      the bar while the other lands on the diagram.
 *   2. Document scrolling. `base.css` sizes `html`/`body` with `100vh`, which on
 *      a phone means the LARGE viewport (browser address bar hidden), so the
 *      document is ~50px taller than the visible area and a 50px drag scrolls
 *      the 44px top bar clean off the screen.
 *
 * Every zoomable surface on the share page owns its own zoom (image lightbox,
 * draw.io canvas, Excalidraw canvas, PDF viewer) and every list scrolls inside
 * `.share-body`, so nothing user-visible is lost by pinning both. These are
 * source-sniffing tests because the meta tag and the shell height run before any
 * module — the same pattern themeBootstrap.test.ts uses for the inline theme
 * scripts.
 */

const webRoot = resolve(__dirname, '../../..')

function viewportMeta(file: string): string {
  const html = readFileSync(resolve(webRoot, file), 'utf8')
  const m = html.match(/<meta name="viewport" content="([^"]*)"/)
  if (!m) throw new Error(`${file}: viewport meta not found`)
  return m[1]
}

describe('share page viewport zoom guard', () => {
  it('disables page-level pinch zoom on the share page', () => {
    const content = viewportMeta('share.html')
    expect(content).toContain('maximum-scale=1.0')
    expect(content).toContain('user-scalable=no')
  })

  it('matches the app shell (index.html) so the two entries cannot drift', () => {
    // index.html additionally carries `interactive-widget=resizes-content`,
    // which is about the on-screen keyboard and deliberately NOT mirrored:
    // the share page has no keyboard-driven shell resize to keep honest.
    for (const flag of ['maximum-scale=1.0', 'user-scalable=no']) {
      expect(viewportMeta('index.html')).toContain(flag)
      expect(viewportMeta('share.html')).toContain(flag)
    }
  })
})

describe('share page document scroll guard', () => {
  const html = readFileSync(resolve(webRoot, 'share.html'), 'utf8')

  it('sizes the document to the VISIBLE viewport and forbids page scrolling', () => {
    // `100vh` is the large viewport on a phone, i.e. ~50px taller than what the
    // reader can see — enough to scroll the 44px top bar away. The declarations
    // need `!important` because css/base.css (loaded later from the JS entry)
    // sizes html/body with `100vh` for the app.
    expect(html).toMatch(/height:\s*100dvh\s*!important/)
    expect(html).toMatch(/overflow:\s*hidden\s*!important/)
    // Fallback for browsers that do not know `dvh`.
    expect(html).toMatch(/height:\s*100%/)
  })
})
