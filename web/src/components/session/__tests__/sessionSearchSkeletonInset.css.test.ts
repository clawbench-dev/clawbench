import { describe, expect, it } from 'vitest'
import { normalizeDp } from '@/testUtils/designLength'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Regression guard: the drill-down preview skeleton must sit on the chunk row's
 * inset.
 *
 * `PanelSkeleton`'s text variant carries no horizontal padding of its own (the
 * list/grid variants pad themselves; the text variant is meant to be dropped
 * into a body that already has one). `.detail-page` has none either, and
 * `.detail-chunk` carries the 14dp itself — so the placeholder ran flush to both
 * edges while the real chunks are inset, and the page shifted sideways the
 * moment the preview arrived.
 *
 * jsdom does no layout and does not apply the component's scoped styles, so
 * this has to be a source guard. The expected inset is derived FROM
 * `.detail-chunk` rather than repeated as a literal, so the two cannot drift.
 */
const src = normalizeDp(
  readFileSync(join(__dirname, '..', 'SessionSearchDrawer.vue'), 'utf8'),
)

/** The declaration block for one selector, or '' when absent. */
function block(selector: string): string {
  const i = src.indexOf(`${selector} {`)
  if (i === -1) return ''
  return src.slice(i, src.indexOf('}', i))
}

/** The horizontal component of a (already normalised) `padding` shorthand. */
function horizontalInset(rule: string): string | undefined {
  const value = rule.match(/(?:^|[\s;])padding:\s*([^;]+);/)?.[1].trim()
  if (!value) return undefined
  // A single value means all four sides; otherwise the LAST token is the
  // horizontal one (top / right / bottom / left and the 2- and 3-value forms).
  return value.split(/\s+/).pop()
}

describe('session search preview skeleton inset', () => {
  it('gives the preview skeleton its own inset class in the template', () => {
    expect(src).toMatch(/class="detail-skeleton"\s+variant="text"/)
  })

  it('insets it exactly like a chunk row', () => {
    const chunk = block('.detail-chunk')
    const skeleton = block('.detail-skeleton')
    expect(chunk, '.detail-chunk rule must exist').not.toBe('')
    expect(skeleton, '.detail-skeleton rule must exist').not.toBe('')

    const expected = horizontalInset(chunk)
    expect(expected, '.detail-chunk must declare a horizontal padding').toBeDefined()
    // The inset is expressed through the skeleton primitive's variable, so the
    // number itself is what has to match the row.
    expect(skeleton.match(/--panel-skeleton-inset:\s*([^;]+);/)?.[1].trim()).toBe(expected)
  })
})
