import { describe, expect, it } from 'vitest'

/**
 * Width contract for the two shared shells.
 *
 * ModalDialog renders ONE fixed-width card for every dialog (900px), so no
 * call site can make its dialog wider or narrower than its neighbours. The cap
 * lives in modal-card.css instead of a `maxWidth` prop for exactly that reason:
 * a prop invites per-dialog drift, which is how this codebase ended up with six
 * different dialog widths (380/420/480/520/560/720, each silently multiplied by
 * a 1.3 fudge factor at render time).
 *
 * BottomSheet deliberately keeps the narrower 624px default — those are
 * drawers shown as a centered card on wide screens, and a full 900px column
 * reads as a page rather than a sheet. A handful of them override the variable
 * locally (.tool-detail-sheet etc.).
 *
 * jsdom has no CSS engine, so this reads the stylesheet. The narrow-viewport
 * half is a layout property (`.modal-dialog` is `width: 100%` inside an overlay
 * with side padding) and cannot bind below ~940px, which is why mobile is
 * unaffected by the cap and needs no separate rule.
 */

async function rawCss(): Promise<string> {
  const mod = await import('@/assets/modal-card.css?raw')
  return String(mod.default).replace(/\/\*[\s\S]*?\*\//g, '')
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/** Declarations of the first rule whose selector list is exactly `selector`. */
function declsOf(css: string, selector: string): string | null {
  const pattern = selector.split(',').map((s) => escapeRe(s.trim())).join('\\s*,\\s*')
  const m = css.match(new RegExp(`(?:^|[},])\\s*${pattern}\\s*\\{([^}]*)\\}`))
  return m ? m[1] : null
}

describe('shared shell width', () => {
  it('gives every ModalDialog the same 900px cap', async () => {
    const css = await rawCss()
    const decls = declsOf(css, '.modal-dialog')
    expect(decls, '.modal-dialog rule must exist').not.toBeNull()
    expect(decls).toMatch(/--modal-max-width:\s*900px/)
  })

  it('leaves BottomSheet on the narrower 624px default', async () => {
    const css = await rawCss()
    const decls = declsOf(css, '.modal-dialog, .bs-panel.bs-wide-auto')
    expect(decls, 'the shared card rule must exist').not.toBeNull()
    expect(decls).toMatch(/max-width:\s*var\(--modal-max-width,\s*624px\)/)
  })

  it('does not let the dialog cap leak into the drawer cap', async () => {
    const css = await rawCss()
    // A `.bs-panel.bs-wide-auto { --modal-max-width: 900px }` rule would widen
    // all 35 drawers; the dialog's 900 must stay scoped to `.modal-dialog`.
    expect(css).not.toMatch(/\.bs-panel\.bs-wide-auto\s*\{[^}]*--modal-max-width:\s*900px/)
  })
})
