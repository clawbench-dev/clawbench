import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Chrome contract for the git-history diff hunks.
 *
 * Two deliberate choices, both easy to "tidy away" by accident:
 *
 *   - The hunk is SQUARE. The diff is a stack of full-width code blocks; a
 *     rounded corner on each one reads as a set of detached cards rather than
 *     one continuous diff, and the gutter tint runs to the edge so a radius
 *     would clip it mid-column.
 *   - The hunk keeps a SMALL horizontal margin (one --space-2), so the block
 *     edges are not welded to the panel border. It must stay small: the hunk
 *     body already scrolls horizontally, and a large inset steals width from
 *     the code.
 *
 * jsdom has no CSS engine, so this is a source-contract check — the same
 * pattern the other *.css.test.ts guards in this repo use.
 */
describe('git-history diff hunk chrome', () => {
  const src = readFileSync(resolve(__dirname, '../GitDiffView.vue'), 'utf8')

  /** Declarations of the `.git-diff-scroll :deep(.diff-hunk)` rule. */
  function hunkRule(): string {
    const m = src.match(
      /\.git-diff-scroll\s*:deep\(\.diff-hunk\)\s*\{([^}]*)\}/,
    )
    expect(m, '.diff-hunk rule must exist').not.toBeNull()
    return m![1]
  }

  it('renders the hunk square', () => {
    // Assert the zero explicitly: a removed declaration would fall back to the
    // UA default (0) and pass a "no radius" check, so pin the property itself.
    expect(hunkRule()).toMatch(/border-radius:\s*0(?:px)?\s*;/)
  })

  it('keeps a small horizontal margin only', () => {
    const rule = hunkRule()
    // Horizontal inset, driven by the shared spacing scale.
    expect(rule).toMatch(
      /margin:\s*0\s+var\(--space-2(?:,\s*4px)?\)\s*;/,
    )
    // No vertical margin: the hunk stack is already spaced by the parent's
    // `gap`, and adding to it would open the blocks up unevenly.
    expect(rule).not.toMatch(/margin:\s*var\(--space-\d\)\s+0\s*;/)
  })
})
