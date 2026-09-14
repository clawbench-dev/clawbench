import { describe, expect, it } from 'vitest'
import { readFileSync } from 'fs'
import { resolve } from 'path'

/**
 * Stacking contract for the docked code-preview pane.
 *
 * Regression: hovering the divider above the docked preview pane showed only
 * the UPPER half of the highlight band ("half glow"). The floating preview card
 * sets `z-index: var(--z-sheet)` (1200) so it sits above the app; docked, the
 * pane is an ordinary layout pane, but `.is-docked` reset `position`, `inset`,
 * `border`, `border-radius` and `box-shadow` — and forgot `z-index`. The split
 * divider is `z-index: 2`, and on hover/drag it expands from 1px to 12px,
 * growing ~5.5px past its own box on each side. The lower half landed inside the
 * pane's box, so at 1200 the opaque pane painted over it.
 *
 * Measured live (elementFromPoint sweep down the expanded band):
 *   before → rows 538..543 visible, 544..549 hit `div.code-preview-header`
 *   after  → all 13 rows hit the divider
 *
 * jsdom has no CSS engine, so this is a source-contract check — the same
 * pattern MarkdownPreviewWideLayout.css.test.ts uses.
 */
describe('docked code-preview pane must not out-stack the split divider', () => {
  const css = readFileSync(
    resolve(__dirname, '../../../assets/code-link-preview.css'),
    'utf8',
  )

  /** Declarations of the first rule matching `selector {`. */
  function declsOf(selector: string): string {
    const m = css.match(
      new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([^}]*)\\}'),
    )
    expect(m, `${selector} rule must exist`).not.toBeNull()
    return m![1]
  }

  it('resets z-index on the docked pane', () => {
    // The pane must leave the stacking game entirely. `auto` (not a low number)
    // matches its `position: relative` layout role and cannot be beaten by a
    // later-painted sibling.
    const docked = declsOf('.code-link-preview-floating.is-docked')
    expect(docked, 'docked pane must reset the floating z-index').toMatch(
      /z-index:\s*auto/,
    )
  })

  it('still sets a high z-index while floating', () => {
    // The reset must be scoped to the docked variant — a floating card still
    // has to sit above the app, or the fix trades one bug for another.
    const floating = declsOf('.code-link-preview-floating')
    expect(floating).toMatch(/z-index:\s*var\(--z-sheet\)/)
  })

  it('keeps the divider below the pane unless the pane opts out', () => {
    // The divider's own z-index is what makes it vulnerable: it is low (2) and
    // relies on nothing above it competing. Documenting the number here means a
    // future bump of the divider does not silently mask a re-broken pane.
    const divider = readFileSync(
      resolve(__dirname, '../../common/SplitDivider.vue'),
      'utf8',
    )
    const style = divider.slice(divider.indexOf('<style scoped>'))
    expect(style).toMatch(/\.split-view__divider\s*\{[^}]*z-index:\s*2/)
  })
})
