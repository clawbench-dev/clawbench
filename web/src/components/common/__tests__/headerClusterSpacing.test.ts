import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The header's right-hand icon cluster is exactly two buttons: theme → server.
 *
 * Screen sharing used to have a third button here (and later a voice-notes one
 * took its slot). Both moved into the dock, so the cluster is back to the pair
 * it had before the sharing feature existed: the theme button pushes the group
 * against the right edge, the server button tucks 8dp under it.
 *
 * Two invariants are worth pinning, because both were broken by the removed
 * buttons:
 *
 *  1. Spacing. Every button carrying `.theme-quick-toggle` inherits
 *     `margin-left: auto`. A second such button splits the slack with the theme
 *     button, which opens a visible gap between them on any screen where the
 *     usage capsule is absent (narrow, or before the first fetch lands). One
 *     `.theme-quick-toggle` in the markup is therefore the spacing contract, not
 *     just a tidiness rule.
 *  2. No stray entry. The recording entry must not come back as a header button:
 *     the notes dock tab is its only home now.
 *
 * jsdom has no layout, so a mount test cannot measure this — the guard reads the
 * component source instead.
 */

function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, '')
}

const source = stripComments(readWebFile('src/components/common/AppHeader.vue'))
const template = source.slice(0, source.indexOf('<style'))

/** Declarations of the first rule whose selector is exactly `selector`. */
function declsOf(selector: string): string {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = source.match(new RegExp(`(?:^|\\n)${escaped}\\s*\\{([^}]*)\\}`))
  expect(match, `expected a rule for ${selector}`).not.toBeNull()
  return match![1]
}

function marginLeftOf(decls: string): string {
  return decls.match(/margin-left\s*:\s*([^;]+);/)?.[1].trim() ?? ''
}

describe('header icon cluster — theme → server', () => {
  it('has exactly one .theme-quick-toggle button', () => {
    // A second one would inherit `margin-left: auto` and split the slack with
    // the theme button, leaving a gap between them.
    const count = template.match(/class="theme-quick-toggle"/g)?.length ?? 0
    expect(count).toBe(1)
  })

  it('does not render a voice-notes entry', () => {
    // The dictaphone lives in the AI notes dock tab; a header shortcut would be
    // a second entry point with its own spacing problem.
    expect(template).not.toContain('solo-notes')
  })

  it('pushes the cluster right from the theme button', () => {
    // The usage capsule owns the auto margin while it exists
    // (`.header--has-meter .theme-quick-toggle`); this is the fallback for the
    // case where no capsule is rendered at all.
    expect(marginLeftOf(declsOf('.theme-quick-toggle'))).toBe('auto')
  })

  it('tucks the server button 8dp under the theme button', () => {
    // Same negative margin the cluster has always used; it is what makes the
    // icon rhythm tight instead of one `gap` wider than the rest of the header.
    expect(marginLeftOf(declsOf('.server-toggle'))).toBe('calc(-8 * var(--dp))')
  })
})
