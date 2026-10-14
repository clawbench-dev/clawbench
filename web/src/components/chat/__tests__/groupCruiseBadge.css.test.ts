import { describe, it, expect } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the chat-header auto-cruise badge (design §15).
 *
 * The badge marks the CURRENT group as auto-cruising. Two things can silently
 * rot and neither is caught by the DOM test in GroupAvatarStack.test.ts:
 *
 *   1. Its colours must derive from the theme. A hardcoded blue looks right on
 *      the theme it was picked on and wrong on the other 35 — the exact failure
 *      the accent-derived running-band tokens were introduced to fix.
 *   2. Its reduced-motion handling. Unlike the session status slot (where motion
 *      is the only channel and opting out would collapse two states), the badge
 *      glow is DECORATIVE — the pill and its "cruising" label already carry the
 *      meaning — so it MAY stop under the preference. The guard asserts the
 *      opt-out exists AND that the animation is still declared, so it cannot
 *      pass merely because someone deleted the animation.
 */
const src = readWebFile('src/components/chat/GroupAvatarStack.vue')
const style = src.slice(src.indexOf('<style'))

/** The base rule for a selector, anchored to the start of a line. */
function baseRule(selector: string): string | undefined {
  return style.match(
    new RegExp('\\n' + selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{[^}]*\\}'),
  )?.[0]
}

describe('group auto-cruise badge', () => {
  it('derives its colours from the theme, never a literal colour', () => {
    const badge = baseRule('.group-cruise-badge')
    expect(badge, '.group-cruise-badge should exist').toBeTruthy()
    expect(badge, 'the pill background should derive from the accent').toMatch(
      /background:\s*color-mix\(in srgb,\s*var\(--accent-color\)/,
    )
    expect(badge, 'the pill text should be the accent').toMatch(/color:\s*var\(--accent-color\)/)
    // No hardcoded colour anywhere in the rule (the `#` fallback in the focus
    // ring belongs to a different rule and is not reached here).
    expect(badge, 'a hardcoded colour would be wrong on most themes').not.toMatch(
      /#[0-9a-f]{3,6}/i,
    )
  })

  it('animates the glow so the badge reads as "live"', () => {
    const badge = baseRule('.group-cruise-badge')
    expect(badge, 'the badge should breathe').toMatch(/animation:\s*group-cruise-breathe/)
    expect(style, '@keyframes group-cruise-breathe should exist').toMatch(
      /@keyframes group-cruise-breathe/,
    )
  })

  it('opts the decorative glow out under prefers-reduced-motion, keeping the pill', () => {
    // The glow is decorative: the pill + its label already say "cruising", so
    // stopping it loses no information (unlike the session status dots, which
    // are a deliberate no-opt-out exception). The opt-out must target ONLY the
    // animation, so the pill stays visible when motion is reduced.
    const m = style.match(/@media \(prefers-reduced-motion: reduce\)\s*\{([\s\S]*?)\n\}/)
    expect(m, 'the badge should opt out of reduced-motion — see the CSS comment').not.toBeNull()
    expect(m![1], 'the opt-out must disable the animation').toMatch(
      /\.group-cruise-badge\s*\{[^}]*animation:\s*none/,
    )

    // And the animation it disables must still be declared, so this cannot pass
    // merely because the animation was deleted outright.
    const badge = baseRule('.group-cruise-badge')
    expect(badge, 'the breathing animation must still be declared').toMatch(
      /animation:\s*group-cruise-breathe/,
    )
  })
})
