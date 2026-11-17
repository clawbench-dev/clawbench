import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards the hierarchy inside the tool-output header: `output` is a SECTION
 * LABEL (it names the block below), `ok` / `error` is a STATUS (it carries the
 * verdict). Both used to be the exact same green pill — same font size, same
 * padding, same radius, same `rgba(34,197,94,.12)` background — so the row read
 * as two equal statuses, and the label looked like a second success badge
 * beside the real one.
 *
 * The settled split: the label is typographic only (muted, uppercase,
 * letter-spaced, NO pill); the pill belongs to the status chip, which is the
 * only thing in that row allowed to be coloured.
 *
 * Two traps this file pins down:
 *   1. The green was a literal hex + rgba, so it could not follow the other 35
 *      themes. The label now takes `--text-muted`; a hex creeping back in fails.
 *   2. The same six rules also lived in ContentBlocks.vue, where NOTHING
 *      produces the markup (the inline card renders tool input only and opens
 *      the drawer for output). A stale copy there is invisible until someone
 *      edits "the other one" — so its absence is asserted.
 *
 * jsdom has no CSS engine, so this is a source contract.
 */

const OVERLAY = 'src/components/chat/ToolDetailDrawer.vue'
const INLINE = 'src/components/chat/ContentBlocks.vue'

/** The single rule whose selector is exactly `selector` (anchored on `{`). */
function rule(src: string, selector: string): string | undefined {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  for (const block of src.matchAll(/<style([^>]*)>([\s\S]*?)<\/style>/g)) {
    // `[^}]*` is safe: no rule in these blocks nests braces.
    const m = block[2].match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`))
    if (m) return m[1]
  }
  return undefined
}

const decls = (selector: string) => {
  const found = rule(readWebFile(OVERLAY), selector)
  expect(found, `${OVERLAY}: missing "${selector}"`).toBeDefined()
  return found!
}

describe('tool-output header: label and status are different tiers', () => {
  it('the label is typographic, not a pill', () => {
    const label = decls('.tool-detail-body .tool-output-label')
    // A pill is exactly what made it read as a peer of the status chip.
    expect(label, 'a section label must not paint a chip background').not.toMatch(
      /\bbackground(-color)?\s*:/,
    )
    expect(label, 'a section label must not be a rounded chip').not.toMatch(
      /border-radius\s*:/,
    )
    // Padding on a pill insets its text; on a label it just misaligns it with
    // the output box below.
    expect(label, 'no padding on a plain label').not.toMatch(/\bpadding\s*:/)
  })

  it('the label is muted + uppercase so it reads as a caption', () => {
    const label = decls('.tool-detail-body .tool-output-label')
    expect(label, 'colour must come from a theme token').toMatch(
      /color:\s*var\(--text-muted\)/,
    )
    expect(label, 'no literal colours — 36 themes must adapt').not.toMatch(
      /#[0-9a-f]{3,8}\b|\brgba?\(/i,
    )
    expect(label).toMatch(/text-transform:\s*uppercase/)
    expect(label).toMatch(/letter-spacing:/)
  })

  it('the status chip keeps the pill', () => {
    const chip = decls('.tool-detail-body .tool-output-status')
    expect(chip, 'status must stay a chip (radius + padding)').toMatch(
      /border-radius\s*:/,
    )
    expect(chip).toMatch(/\bpadding\s*:/)
    expect(decls('.tool-detail-body .tool-output-success')).toMatch(/\bbackground\s*:/)
    expect(decls('.tool-detail-body .tool-output-error')).toMatch(/\bbackground\s*:/)
  })

  it('the two tiers cannot collapse back into the same declaration set', () => {
    const label = decls('.tool-detail-body .tool-output-label')
    const chip = decls('.tool-detail-body .tool-output-status')
    expect(label.trim(), 'label and chip must not be identical').not.toBe(chip.trim())
    // The decisive difference: only the chip is allowed to paint.
    expect(/\bbackground\s*:/.test(label)).toBe(false)
    expect(/\bbackground\s*:/.test(chip) || /background\s*:/.test(
      decls('.tool-detail-body .tool-output-success'),
    )).toBe(true)
  })
})

describe('tool-output header: no stale duplicate in the inline card', () => {
  it('ContentBlocks does not style a header it never renders', () => {
    const src = readWebFile(INLINE)
    for (const cls of [
      'tool-output-header',
      'tool-output-label',
      'tool-output-status',
      'tool-output-success',
      'tool-output-error',
    ]) {
      expect(
        rule(src, `.content-blocks .tool-detail .${cls}`),
        `${cls} has no producer under .content-blocks — a copy here drifts silently`,
      ).toBeUndefined()
    }
  })
})
