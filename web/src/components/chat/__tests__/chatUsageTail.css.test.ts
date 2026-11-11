import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * The consumption figures live INSIDE the reply bubble (bottom right), not in
 * the row below it. Two things follow, and jsdom has no CSS engine so neither
 * can be caught by a component test:
 *
 *   1. `user-select: none` — otherwise selecting a reply to copy appends
 *      "↑24.6M ↓66.5K 20.7" to the copied text. This is the whole reason the
 *      figures are allowed inside the bubble at all.
 *   2. A stronger text tone than the meta row uses: the bubble paints its own
 *      grey (and a translucent one under a wallpaper), so the muted meta colour
 *      would wash out there.
 */
function source(): string {
  const candidates = [
    resolve(process.cwd(), 'web/src/components/chat/ChatMessageItem.vue'),
    resolve(process.cwd(), 'src/components/chat/ChatMessageItem.vue'),
  ]
  for (const p of candidates) {
    try {
      return readFileSync(p, 'utf8')
    } catch {
      /* try the next root */
    }
  }
  throw new Error(`ChatMessageItem.vue not found from cwd: ${process.cwd()}`)
}

/** Extract the declaration block of a top-level class rule. */
function rule(css: string, selector: string): string {
  const start = css.indexOf(`\n${selector} {`)
  if (start === -1) throw new Error(`rule ${selector} not found`)
  const open = css.indexOf('{', start)
  const close = css.indexOf('}', open)
  return css.slice(open + 1, close)
}

describe('in-bubble consumption figures', () => {
  const css = source()

  it('is excluded from text selection so copying a reply stays clean', () => {
    const body = rule(css, '.chat-usage-inline')
    expect(body).toMatch(/user-select:\s*none/)
    // Safari needs the prefixed form; the repo ships both everywhere else too.
    expect(body).toMatch(/-webkit-user-select:\s*none/)
  })

  it('uses a stronger tone than the meta row below the bubble', () => {
    const inline = rule(css, '.chat-usage-inline')
    const metaRow = rule(css, '.chat-meta-info')
    const pct = (s: string) => Number((s.match(/var\(--text-secondary\)\s*(\d+)%/) || [])[1])
    expect(pct(inline)).toBeGreaterThan(pct(metaRow))
  })

  it('sits flush right inside the bubble', () => {
    const body = rule(css, '.chat-usage-inline')
    expect(body).toMatch(/justify-content:\s*flex-end/)
  })

  it('sizes both glyphs in --dp so they scale with the numbers', () => {
    for (const sel of ['.chat-usage-arrow', '.chat-usage-money svg']) {
      const body = rule(css, sel)
      expect(body, sel).toMatch(/width:\s*calc\(12 \* var\(--dp\)\)/)
      expect(body, sel).toMatch(/height:\s*calc\(12 \* var\(--dp\)\)/)
    }
  })

  it('leaves the credit figure uncoloured so it inherits the row tone', () => {
    const body = rule(css, '.chat-usage-money')
    expect(body).not.toMatch(/\bcolor\s*:/)
  })

  it('paints the direction arrows at the same strength as the money glyph', () => {
    // They share one row, so a dimmed arrow next to a full-strength coin reads
    // as two different tones rather than as one group.
    const body = rule(css, '.chat-usage-arrow')
    expect(body).not.toMatch(/opacity\s*:/)
  })

  it('pins the model to the left edge while the figures stay flush right', () => {
    // `margin-right: auto` (NOT `justify-content: space-between`): a row that
    // has no model must keep its figures flush right exactly as before.
    const model = rule(css, '.chat-usage-model')
    const row = rule(css, '.chat-usage-inline')
    expect(model).toMatch(/margin-right:\s*auto/)
    expect(row).toMatch(/justify-content:\s*flex-end/)
  })

  it('ellipsizes a long model id instead of squeezing the figures', () => {
    // A flex item refuses to shrink below its content without `min-width: 0`,
    // so `text-overflow` alone would never fire and long provider/model pairs
    // would push the numbers out of the bubble.
    const model = rule(css, '.chat-usage-model')
    expect(model).toMatch(/min-width:\s*0/)
    expect(model).toMatch(/overflow:\s*hidden/)
    expect(model).toMatch(/text-overflow:\s*ellipsis/)
    expect(model).toMatch(/white-space:\s*nowrap/)
  })
})
