import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Guard for the "other project" card's two shapes.
 *
 * A card either has active sessions (header + rows) or only a recency stamp
 * (header alone until opened). Three rules make the idle shape read correctly,
 * and each is easy to lose in a later restyle:
 *
 *   1. The card carries `.is-idle` so the header-only shape is addressable at
 *      all — without it the collapsed card is indistinguishable in CSS from an
 *      expanded one that happens to be empty.
 *   2. While collapsed, the header's bottom border is dropped. It would
 *      otherwise sit on the card's own border and read as a double line, since
 *      there are no rows to separate it from.
 *   3. The recency label is plain text, not the fixed-size count badge: a
 *      18dp circle would clip "23 小时前" or leave a hole.
 *
 * jsdom does not apply scoped styles, so this is a source guard. The cards live
 * in their own component (both layouts render it), so the guard reads that file.
 */
const src = readFileSync(join(__dirname, '..', 'OtherProjectCards.vue'), 'utf8')

/** The declaration block for one selector, or '' when absent. */
function block(selector: string): string {
  const i = src.indexOf(`${selector} {`)
  if (i === -1) return ''
  return src.slice(i, src.indexOf('}', i))
}

describe('other-project card — idle shape', () => {
  it('marks a session-less card with .is-idle', () => {
    expect(src).toContain(`'is-idle': group.sessions.length === 0`)
  })

  it('drops the header divider only while the idle card is collapsed', () => {
    const rule = block('.pcard.is-idle:not(.is-expanded) .pcard-head')
    expect(rule, 'the collapsed-idle divider rule must exist').not.toBe('')
    expect(rule).toContain('border-bottom: 0')
    // The rule must be scoped to the COLLAPSED state: an open idle card shows
    // rows, and there the header border is the divider between them.
    expect(src).not.toMatch(/\.pcard\.is-idle\s+\.pcard-head\s*\{[^}]*border-bottom:\s*0/)
  })

  it('renders the recency as text rather than the count badge', () => {
    const rule = block('.pcard-recent')
    expect(rule, '.pcard-recent must be styled').not.toBe('')
    // A fixed circle is exactly what the count badge is; the label must not
    // copy it or long relative times get clipped.
    expect(rule).not.toMatch(/width:\s*calc\(/)
    expect(rule).not.toContain('border-radius: 50%')
  })

  it('shows the count badge only when the card has sessions', () => {
    expect(src).toContain('v-if="group.sessions.length > 0" class="pcard-count"')
    expect(src).toContain('v-else class="pcard-recent"')
  })
})
