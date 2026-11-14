import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Guard: the share page must disable page-level pinch zoom, exactly like the
 * app shell does.
 *
 * Why this matters: `.share-topbar` is the first row of a fixed flex column.
 * A browser-level pinch (or the pan that follows it) moves the whole document
 * inside the visual viewport, so the top bar and the page background leave the
 * screen and the reader is left with nothing but the artwork. On a phone this
 * happens without meaning to — the top bar sits 44px above the draw.io canvas,
 * so one finger of a two-finger pinch lands on the bar while the other lands on
 * the diagram. (Reported as "整个 Header 和背景全消失，只剩那张图".)
 *
 * Every zoomable surface on the share page owns its own zoom (image lightbox,
 * draw.io canvas, Excalidraw canvas, PDF viewer), so nothing user-visible is
 * lost by mirroring index.html here. This is a source-sniffing test because the
 * meta tag runs in the browser before any module — the same pattern
 * themeBootstrap.test.ts uses for the inline theme scripts.
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
