import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * The quote bar must be teleported OUT of `.app-container`.
 *
 * `.app-container` is `position: fixed`, and a fixed box ALWAYS establishes a
 * stacking context (CSS Positioned Layout §stacking). Every z-index inside it
 * is therefore compared only against its own siblings: the bar's
 * `--z-quote-bar` (2400) never competed with the app header (1100), which is
 * teleported to <body> by AppHeader. The bar was painted under the header
 * whenever it was anchored near the top of the viewport — and raising the
 * number could not help, because the cap is structural, not numeric (measured
 * with the real stylesheets: 2400 and 9999 both lost to the header; the same
 * element under <body> won at 2400).
 *
 * jsdom has no stacking engine (elementFromPoint returns null), so this is a
 * source guard: it pins the one property that makes the z-index meaningful.
 * The sibling assertion below keeps the reason discoverable — if someone ever
 * makes `.app-container` non-fixed, the teleport can be reconsidered.
 */
function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

describe('quote bar stacking context', () => {
  const app = readWebFile('src/App.vue')
  const layout = readWebFile('css/layout.css')
  const variables = readWebFile('css/variables.css')

  it('renders the bar inside a Teleport to body', () => {
    const at = app.indexOf('<QuoteQuestionBar')
    expect(at).toBeGreaterThan(-1)

    // Walk back to the nearest open/close Teleport marker: the bar must sit
    // INSIDE one, not merely have one somewhere in the template.
    const before = app.slice(0, at)
    const lastOpen = before.lastIndexOf('<Teleport')
    const lastClose = before.lastIndexOf('</Teleport>')
    expect(lastOpen).toBeGreaterThan(lastClose)

    const teleportOpenTag = before.slice(lastOpen, before.indexOf('>', lastOpen) + 1)
    expect(teleportOpenTag).toContain('to="body"')

    // And it must be closed after the bar, or the next sibling would be
    // teleported by accident.
    expect(app.indexOf('</Teleport>', at)).toBeGreaterThan(at)
  })

  it('is not nested in the fixed app container any more', () => {
    // `.app-container` ends before the bar's Teleport. Expressed by position
    // rather than by parsing the template: the container's own closing tag is
    // the last `</div>` before the Teleport block.
    const teleportAt = app.lastIndexOf('<Teleport', app.indexOf('<QuoteQuestionBar'))
    const containerEnd = app.lastIndexOf('</div>', teleportAt)
    expect(containerEnd).toBeLessThan(teleportAt)

    // The reason the teleport is load-bearing: a fixed ancestor caps z-index.
    expect(layout).toMatch(/\.app-container\s*\{[^}]*position:\s*fixed/)
  })

  it('keeps the bar above the app header by token, once un-capped', () => {
    const token = (name: string) => {
      const m = variables.match(new RegExp(`${name}:\\s*(\\d+)`))
      if (!m) throw new Error(`missing token ${name}`)
      return parseInt(m[1], 10)
    }
    // The bar floats next to the selection, which can be near the top of the
    // viewport; it must paint over the header there rather than under it.
    expect(token('--z-quote-bar')).toBeGreaterThan(token('--z-header'))
  })
})
