import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The Mermaid loading placeholder is the standard picture glyph on a
 * shimmering skeleton tile.
 *
 * Why this is pinned at the source level
 * --------------------------------------
 * The placeholder markup is injected with `innerHTML` from a util and is
 * replaced the moment `mermaid.render()` resolves, so a component test can only
 * observe it for one tick — and jsdom has no layout engine, so the properties
 * that matter (does the sweep have enough surface to be seen; is it the TILE
 * that animates rather than the glyph) cannot be measured at all.
 *
 * What this replaced, and why
 * ---------------------------
 * First a lone 20px ring in a 660×68 empty box: it said "waiting" but nothing
 * about what was coming. Then a hand-drawn bar skeleton in the shape of a fork:
 * a diagram's real shape is unknowable before it renders, so the invented
 * layout was a guess, and drawn out of grey bars it read as a chart-ish blob
 * rather than as a diagram. Then Lucide's `network` glyph, which over-specified
 * the content — it promised a node graph, and a sequence diagram or a pie chart
 * is not one. What the placeholder actually stands for is a MEDIA BLOCK that
 * has not arrived, and the plain picture glyph is exactly that.
 */

const css = readWebFile('src/assets/mermaid.css').replace(/\/\*[\s\S]*?\*\//g, '')
const mermaidTs = readWebFile('src/utils/mermaid.ts')

/** The declaration block of the first rule whose selector ends with `selector`. */
function blockOf(source: string, selector: string): string | null {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`(?:^|\\n)[^\\n{}]*${escaped}\\s*\\{([\\s\\S]*?)\\}`)
  const m = re.exec(source)
  return m ? m[1] : null
}

describe('mermaid loading placeholder markup', () => {
  it('renders the picture glyph, not a hand-drawn skeleton', () => {
    expect(mermaidTs, 'the placeholder must use the picture glyph').toContain('mermaid-loading-icon')
    // The bar skeleton is gone entirely — no half-migrated state where both
    // shapes could render.
    expect(mermaidTs, 'the invented bar skeleton must be gone').not.toContain('mermaid-skeleton')
  })

  it('copies the glyph path data verbatim from lucide (no drift)', () => {
    // The markup is a string for innerHTML, so the icon cannot be imported (and
    // several test files mock lucide with a narrow name set). Copying is the
    // trade — this assertion is what keeps the copy honest: it must match
    // lucide-vue-next's `network.js` exactly.
    const lucide = readWebFile('../node_modules/lucide-vue-next/dist/esm/icons/image.js')
    // The frame, the sun and the mountain — the three parts of the picture glyph.
    const parts = [
      { inLucide: /width:\s*"18",\s*height:\s*"18",\s*x:\s*"3",\s*y:\s*"3"/, inMarkup: /<rect width="18" height="18" x="3" y="3"/ },
      { inLucide: /cx:\s*"9",\s*cy:\s*"9",\s*r:\s*"2"/, inMarkup: /<circle cx="9" cy="9" r="2"/ },
      { inLucide: /d:\s*"m21 15-3\.086-3\.086a2 2 0 0 0-2\.828 0L6 21"/, inMarkup: /d="m21 15-3\.086-3\.086a2 2 0 0 0-2\.828 0L6 21"/ },
    ]
    for (const part of parts) {
      expect(lucide, 'lucide must still describe this part of the glyph').toMatch(part.inLucide)
      expect(mermaidTs, 'the inline glyph must match lucide').toMatch(part.inMarkup)
    }
  })

  it('announces the load and hides the glyph', () => {
    // The glyph is aria-hidden, so without the one status line a screen reader
    // walking into the diagram position hears nothing for those seconds.
    expect(mermaidTs).toMatch(/role="status"/)
    expect(mermaidTs).toContain('media.diagramLoading')
    expect(mermaidTs).toMatch(/aria-hidden="true"/)
  })

  it('does not pair the glyph with a spinner', () => {
    // One mark, one motion: the sweep is the signal, a ring beside it would say
    // the same thing twice.
    expect(mermaidTs).not.toContain('li-spinner')
  })
})

describe('mermaid loading placeholder (mermaid.css contract)', () => {
  it('shimmers a TILE, not the glyph', () => {
    // A sweep needs surface. Pulsing the bare glyph was measured as a no-op at
    // 56dp: the stroke is ~1.5px, so a brightness change on that little ink is
    // invisible at reading distance and the block looked frozen. The tile gives
    // the band somewhere to travel.
    const tile = blockOf(css, '.mermaid-loading-tile')
    expect(tile, 'the tile rule must exist').not.toBeNull()
    expect(tile!, 'the tile must run the shared shimmer').toContain('skeleton-shimmer')

    const icon = blockOf(css, '.mermaid-loading-icon')
    expect(icon, 'the glyph rule must exist').not.toBeNull()
    expect(icon!, 'the glyph itself must not animate — it is the watermark').not.toMatch(/animation:/)
  })

  it('reuses the shared skeleton variables instead of declaring them', () => {
    // Same rule as `.skeleton-block`: declaring them here makes every ancestor
    // override a silent no-op.
    const tile = blockOf(css, '.mermaid-loading-tile')
    expect(tile!).toMatch(/var\(--skeleton-fill/)
    expect(tile!).toMatch(/var\(--skeleton-sheen/)
    expect(tile!, 'must not declare the variables it consumes').not.toMatch(/--skeleton-(fill|sheen)\s*:/)
    expect(tile!, 'must not fork the shimmer definition').not.toContain('@keyframes')
  })

  it('gives the sweep enough horizontal travel to read as motion', () => {
    // The gradient is horizontal, so a narrow tile leaves it almost no travel
    // and it degrades into a soft glow — a 96x96 tile measured exactly that
    // way (indistinguishable from a pulse). 200x120 is the smallest size where
    // the band visibly MOVES while the glyph still holds the eye.
    const tile = blockOf(css, '.mermaid-loading-tile')
    const w = Number(/width:\s*calc\((\d+)\s*\*/.exec(tile!)?.[1])
    const h = Number(/height:\s*calc\((\d+)\s*\*/.exec(tile!)?.[1])
    expect(w, 'the tile must declare a width').toBeGreaterThan(0)
    expect(h, 'the tile must declare a height').toBeGreaterThan(0)
    expect(w / h, `tile ${w}x${h} must be landscape enough for a horizontal sweep`)
      .toBeGreaterThanOrEqual(1.4)
    expect(w, 'below ~160dp the sweep reads as a glow, not a band').toBeGreaterThanOrEqual(160)
  })

  it('keeps the glyph muted so it reads as a watermark on the tile', () => {
    const icon = blockOf(css, '.mermaid-loading-icon')
    expect(icon!).toMatch(/color:\s*var\(--text-muted/)
    expect(icon!).toMatch(/opacity:/)
  })

  it('freezes the shimmer but keeps the tile visible under reduced motion', () => {
    const block = /@media \(prefers-reduced-motion: reduce\) \{([\s\S]*?)\n\}/.exec(css)
    expect(block, 'a reduced-motion block must exist').not.toBeNull()
    expect(block![1]).toMatch(/animation:\s*none/)
    // It must target the TILE — freezing the (non-animating) glyph would pass
    // this assertion while leaving the sweep running.
    expect(block![1]).toContain('mermaid-loading-tile')
  })
})
