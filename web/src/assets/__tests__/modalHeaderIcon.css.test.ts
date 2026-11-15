import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * Guard for the shared drawer/dialog header icon tiles (`.bs-header-icon` in
 * BottomSheet.vue, `.modal-header-icon` in modal-card.css).
 *
 * Both classes are put DIRECTLY on lucide `<svg>` elements at ~40 of 45 call
 * sites (`<Search :size="16" class="bs-header-icon" />`). The tile is 24dp;
 * without inner padding, CSS `width/height` override the svg's width ATTRIBUTE
 * and stretch the 16dp glyph to fill the whole 24dp box — the icon reads ~1.5×
 * larger than every neighbour (measured on screenshots: glyph bbox 22px for a
 * lucide icon whose native draw is ~14.7px at the same scale).
 *
 * The fix is `padding: calc(4 * var(--dp))` on the tile: box-sizing is
 * border-box globally (css/base.css), so the padding shrinks the svg's own
 * viewport back to 16dp and the viewBox scales into it. On the <span>-wrapper
 * call sites the padding is a visual no-op (child already centered by
 * inline-flex).
 */

function readWebFile(rel: string): string {
  return readFileSync(resolve(__dirname, '../../../..', rel), 'utf8')
}

function ruleBody(css: string, selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = css.match(new RegExp(`${escaped}\\s*\\{([\\s\\S]*?)\\}`))
  expect(match, `${selector} rule must exist`).not.toBeNull()
  return match![1]
}

const SELECTORS: Array<[string, string]> = [
  ['web/src/components/common/BottomSheet.vue', '.bs-header-icon'],
  ['web/src/assets/modal-card.css', '.modal-header-icon'],
]

describe('drawer/dialog header icon tiles keep glyph breathing room', () => {
  it.each(SELECTORS)('%s pins %s geometry', (file, selector) => {
    const body = ruleBody(readWebFile(file), selector)

    // Tile stays 24dp.
    expect(body, `${selector} must keep its 24dp tile`).toContain('width: calc(24 * var(--dp))')
    expect(body).toContain('height: calc(24 * var(--dp))')

    // The breathing room: 4dp padding. Without it a lucide svg carrying this
    // class directly stretches its glyph to 24dp and the header icon reads
    // ~1.5× larger than its neighbours.
    expect(body, `${selector} must declare 4dp padding so the 16dp glyph is not stretched to the 24dp tile`).toContain(
      'padding: calc(4 * var(--dp))',
    )
  })

  it('bottom-sheet tile keeps modal-card tile geometry (two declarations, one recipe)', () => {
    const bs = ruleBody(readWebFile('web/src/components/common/BottomSheet.vue'), '.bs-header-icon')
    const modal = ruleBody(readWebFile('web/src/assets/modal-card.css'), '.modal-header-icon')
    const geometry = (body: string) =>
      ['width', 'height', 'padding'].map((prop) => {
        const line = body.split('\n').find((l) => l.trim().startsWith(`${prop}:`))
        return line?.trim().replace(/\s*;.*/, ';')
      })
    expect(geometry(bs)).toEqual(geometry(modal))
  })
})
