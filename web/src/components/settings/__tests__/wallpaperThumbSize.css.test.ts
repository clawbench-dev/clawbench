import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard test for the wallpaper preview thumbnail size.
 *
 * Regression: the Bing preview and the uploaded-gallery tiles were sized
 * independently — the Bing preview was a hard-coded 44x30 while a gallery tile
 * was a 72px-wide grid column. The same wallpaper therefore appeared at two
 * different sizes depending on its source, which read as two unrelated
 * controls. Both now derive from one shared custom property.
 *
 * Source-sniffing test (jsdom has no CSS engine), matching the pattern in
 * wallpaperSurfaceTransparency.css.test.ts. The SFC lives under the vitest
 * source root, but cwd differs between a bare `vitest` run (web/) and the
 * scripts/vitest-run.sh wrapper (repo root), so probe both.
 */

function readComponent(): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(
        join(base, 'src/components/settings/WallpaperSetting.vue'),
        'utf8',
      )
    } catch {
      // try the next candidate
    }
  }
  throw new Error('WallpaperSetting.vue not found from cwd: ' + process.cwd())
}

const src = readComponent()
const style = src.slice(src.indexOf('<style scoped>'))

/** The declaration block of the first rule whose selector matches exactly. */
function blockFor(selector: string): string {
  const idx = style.indexOf(selector + ' {')
  expect(idx, `rule "${selector}" should exist`).toBeGreaterThan(-1)
  const open = style.indexOf('{', idx)
  const close = style.indexOf('}', open)
  return style.slice(open + 1, close)
}

describe('wallpaper preview thumbnail sizing', () => {
  it('defines the thumbnail size once as shared custom properties', () => {
    const root = blockFor('.wallpaper-setting')
    expect(root).toMatch(/--wallpaper-thumb-w:\s*\d+px;/)
    expect(root).toMatch(/--wallpaper-thumb-h:\s*\d+px;/)
  })

  it('sizes the Bing preview from the shared properties', () => {
    const thumb = blockFor('.wallpaper-thumb')
    expect(thumb).toContain('width: var(--wallpaper-thumb-w);')
    expect(thumb).toContain('height: var(--wallpaper-thumb-h);')
  })

  it('sizes gallery columns from the shared width', () => {
    const gallery = blockFor('.wallpaper-gallery')
    expect(gallery).toContain('var(--wallpaper-thumb-w)')
    // minmax(shared-width, 1fr): the shared width is the floor (so a tile is
    // never smaller than the Bing preview) while the fr lets columns absorb the
    // leftover width so the row reaches the right edge. A bare fixed track left
    // a ragged gap whenever the panel width was not a multiple of tile + gap.
    expect(gallery).toContain('minmax(var(--wallpaper-thumb-w), 1fr)')
    expect(gallery).toContain('repeat(auto-fill')
  })

  it('no longer hard-codes the old Bing preview size', () => {
    expect(style).not.toMatch(/\.wallpaper-thumb\s*\{[^}]*width:\s*44px/)
    expect(style).not.toMatch(/\.wallpaper-thumb\s*\{[^}]*height:\s*30px/)
  })

  it('keeps the preview aspect ratio equal to the gallery tile ratio', () => {
    // The gallery tile pins aspect-ratio: 4 / 3; the explicit preview size must
    // match it, or the two previews would differ in shape as well as scale.
    const root = blockFor('.wallpaper-setting')
    const w = Number(root.match(/--wallpaper-thumb-w:\s*(\d+)px/)?.[1])
    const h = Number(root.match(/--wallpaper-thumb-h:\s*(\d+)px/)?.[1])
    expect(w).toBeGreaterThan(0)
    expect(h).toBeGreaterThan(0)
    expect(w / h).toBeCloseTo(4 / 3, 5)

    expect(blockFor('.wallpaper-gallery__item')).toContain('aspect-ratio: 4 / 3;')
  })

  it('draws the gallery selection ring without consuming layout space', () => {
    // Measured in a real browser: a 2px border shrank the gallery photo to
    // 68x50 while the Bing preview rendered 72x54. The ring must be an inset
    // shadow so both photos occupy the identical box.
    const item = blockFor('.wallpaper-gallery__item')
    expect(item).toContain('box-shadow: inset 0 0 0 2px transparent;')
    expect(item).not.toMatch(/border:\s*2px/)

    const active = blockFor('.wallpaper-gallery__item--active')
    expect(active).toContain('box-shadow: inset 0 0 0 2px var(--accent-color);')
    expect(active).not.toContain('border-color')
  })

  it('keeps the preview image left and pushes only the action button right', () => {
    // Measured in a real browser (settings panel 445px wide): the Bing text
    // block (label + date + title + credit) is ~413px, so this group always
    // wraps onto its own line. The image must stay at the LEFT edge, aligned
    // with the text above it; only the action button is pushed to the right
    // (`margin-left: auto` on the BUTTON — putting it on the wrapper drags the
    // thumbnail along and strands the image at the right edge).
    const wrap = blockFor('.wallpaper-thumb-wrap')
    expect(wrap, 'the wrapper must span the row so the button can reach its edge').toContain('width: 100%;')
    expect(wrap, 'the wrapper must NOT carry the right-push').not.toContain('margin-left: auto;')

    // The push lives on the button inside the wrapper.
    expect(style).toMatch(/\.wallpaper-thumb-wrap\s+\.settings-item__action\s*\{[^}]*margin-left:\s*auto/)
  })

  it('keeps the tile Apply button always visible', () => {
    // It shipped hover-revealed (opacity 0 until the tile was hovered), which
    // the user reported as "I can't see the Apply button" — the affordance is
    // undiscoverable if it only appears once you already found the tile. Like
    // the delete button in the opposite corner, it must be permanently drawn;
    // only the DISABLED state may dim it.
    const apply = blockFor('.wallpaper-gallery__apply')
    expect(apply, 'the Apply button must not start hidden').not.toMatch(/opacity:\s*0\s*;/)
    // A hover rule that turns it ON is the hover-reveal pattern — reject it.
    expect(style).not.toMatch(/\.wallpaper-gallery__item:hover\s+\.wallpaper-gallery__apply/)
  })
})
