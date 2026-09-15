import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

/**
 * Both roles' meta bars must share the same right edge.
 *
 * .chat-message is the flex ROW holding .msg-card (the bubble) and
 * .chat-meta-bar (outside the bubble). The user bubble is inset from the right
 * by 10px (--space-5) and capped at `calc(100% - 20px)`.
 *
 * Regression: those two insets used to live on the ROW. That shifted the whole
 * row — meta bar included — inward by 10px, so the user bar's right edge sat
 * left of the assistant bar's (measured: user actions ended ~10px short of the
 * assistant ones). The inset belongs on the BUBBLE only; the row must span the
 * full column so both bars line up.
 *
 * jsdom has no CSS engine, so this is a source-contract check (the same pattern
 * chatLeakedControlWrap.test.ts / chatBoldStyle.test.ts use).
 */
describe('chat meta bar: user and assistant rows align on the right edge', () => {
  const source = readFileSync(resolve(__dirname, '../ChatMessageItem.vue'), 'utf8')
  // The non-scoped <style> block — the role rules live there so they penetrate
  // the v-html message body.
  const globalStyle = source.slice(source.lastIndexOf('<style>'))

  /** Declarations of the first rule whose selector matches `pattern`. */
  function declsIn(text: string, pattern: RegExp): string {
    const m = text.match(pattern)
    expect(m, `${pattern} rule must exist`).not.toBeNull()
    return m![1]
  }

  /** Declarations of a global-block rule. */
  function declsOf(pattern: RegExp): string {
    return declsIn(globalStyle, pattern)
  }

  it('keeps the user row full-width so its meta bar reaches the same right edge', () => {
    const row = declsOf(/\.chat-message\.user\s*\{([^}]*)\}/)
    // A right inset on the row would pull the meta bar inward too.
    expect(row, 'row must not inset itself from the right').not.toMatch(/margin-right/)
    // `align-self: stretch` (not flex-end) so the row spans the column and the
    // bar's own justify/padding decides the edge.
    expect(row).toMatch(/align-self:\s*stretch/)
  })

  it('moves the user bubble inset onto the bubble itself', () => {
    const bubble = declsOf(/\.chat-message\.user\s+\.msg-card\s*\{([^}]*)\}/)
    expect(bubble, 'bubble keeps its 10px right inset').toMatch(
      /margin-right:\s*var\(--space-5\)/,
    )
    expect(bubble, 'bubble keeps its width cap').toMatch(
      /max-width:\s*calc\(100%\s*-\s*20px\)/,
    )
  })

  it('does not inset the assistant row (its bubble is full-bleed)', () => {
    const row = declsOf(/\.chat-message\.assistant\s*\{([^}]*)\}/)
    expect(row).not.toMatch(/margin-right/)
    expect(row).toMatch(/align-self:\s*stretch/)
  })

  it('gives both roles the same meta bar padding', () => {
    // The shared .chat-meta-bar (scoped block) carries the horizontal padding;
    // neither role may override it, or the two bars would disagree on their
    // inner edge.
    const bar = declsIn(source, /\.chat-meta-bar\s*\{([^}]*)\}/)
    expect(bar).toMatch(/padding:\s*0\s+var\(--space-6\)/)
    expect(source).not.toMatch(/\.chat-meta-bar-(?:user|assistant)\s*\{[^}]*padding/)
  })
})
