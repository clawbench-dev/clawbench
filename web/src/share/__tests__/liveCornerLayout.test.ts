import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The viewer page has two floating notices that answer unrelated questions:
 *
 *   - "who is speaking"  — changes every few seconds, wants attention;
 *   - connection health  — an ambient dot, interesting only when the picture
 *     stutters.
 *
 * They were both anchored to the top-left corner, so the speaker pill (rendered
 * later, same z-index) covered the quality chip exactly while somebody talked.
 * The fix is a corner split: speaker owns the left, the quality chip shares the
 * right column with the browser capture hint.
 *
 * Asserted on the template and the stylesheet rather than a computed box: jsdom
 * applies neither layout nor scoped CSS, so a rendered measurement would pass
 * no matter where the rules put the chips.
 */
const FILE = 'src/share/LiveShareView.vue'

function template(): string {
  const src = readWebFile(FILE)
  const start = src.indexOf('<template>')
  const end = src.indexOf('<script setup')
  expect(start, 'template start not found').toBeGreaterThanOrEqual(0)
  expect(end, 'script setup not found').toBeGreaterThan(start)
  return src.slice(start, end)
}

/** The whole `<div>` whose opening tag carries `marker`, nested divs included. */
function divBlockAt(marker: string): string {
  const tpl = template()
  const at = tpl.indexOf(marker)
  expect(at, `${marker} not found in template`).toBeGreaterThanOrEqual(0)
  const open = tpl.lastIndexOf('<div', at)
  const re = /<div\b|<\/div>/g
  re.lastIndex = open
  let depth = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(tpl))) {
    depth += m[0] === '</div>' ? -1 : 1
    if (depth === 0) return tpl.slice(open, m.index + m[0].length)
  }
  throw new Error(`unbalanced <div> around ${marker}`)
}

/** Declarations of the last rule whose selector is exactly `selector`. */
function ruleBody(selector: string): string {
  const src = readWebFile(FILE)
  const css = (src.match(/<style scoped>([\s\S]*)<\/style>/) as RegExpMatchArray)[1].replace(
    /\/\*[\s\S]*?\*\//g,
    '',
  )
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const matches = [...css.matchAll(new RegExp(`(?:^|[}\\s])${escaped}\\s*\\{([^{}]*)\\}`, 'g'))]
  expect(matches.length, `no rule for ${selector}`).toBeGreaterThan(0)
  return matches[matches.length - 1][1]
}

describe('viewer corner split: speaking vs connection health', () => {
  it('puts the connection chip in the top-right column, not under the speaker pill', () => {
    const corner = divBlockAt('class="live-top-right"')
    expect(corner).toContain('data-role="quality"')
    expect(corner).toContain('data-role="client-hint"')
    // The speaker pill must NOT be in this column: it is the whole point.
    expect(corner).not.toContain('data-role="speaker"')
  })

  it('keeps the speaker pill out of the top-right column and on the left', () => {
    const speaker = ruleBody('.live-speaker')
    expect(speaker).toMatch(/left:\s*calc\(/)
    expect(speaker).not.toMatch(/right:/)
  })

  it('anchors the column itself on the right edge', () => {
    const corner = ruleBody('.live-top-right')
    expect(corner).toMatch(/right:\s*calc\(/)
    expect(corner).not.toMatch(/(^|[;\s])left:/)
    expect(corner).toMatch(/position:\s*absolute/)
  })

  it('does not let the quality chip position itself (it is a flex item of the column)', () => {
    const chip = ruleBody('.live-quality')
    expect(chip).not.toMatch(/position:\s*absolute/)
    expect(chip).not.toMatch(/(^|[;\s])(top|left|right|bottom):/)
  })
})
