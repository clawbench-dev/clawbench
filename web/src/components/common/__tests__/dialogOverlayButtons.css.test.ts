import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Spec guard for the app's dialog footer buttons.
 *
 * Two dialogs shipped their own bespoke `.dlg-btn` button language instead of
 * the shared pill spec (`assets/modal-footer-btn.css`): a 6px rounded rect with
 * hardcoded reds (`#d32f2f` for a destructive confirm, `#ef4444` in a
 * `[data-theme-base="dark"]` override). That diverged from the rest of the app
 * in two ways:
 *
 *  1. **Shape** — dialog footers use the `.fbtn` pill language: 30px tall,
 *     radius 15, neutral / `.fbtn-primary` / `.fbtn-danger` variants. The two
 *     `.dlg-btn` copies were the last bespoke footer-button implementations.
 *  2. **Colour** — `#d32f2f` matches no theme token, so the destructive button
 *     ignored all 36 themes' own reds. The convention (see GitTagList.vue,
 *     SharedSessionsDrawer.vue) is the hue-named `var(--color-red)` token.
 *
 * Both files carried a byte-identical copy of the button block, so fixing one
 * and not the other is exactly how they drifted apart — the guard therefore
 * runs against both.
 *
 * jsdom has no CSS engine and `var()`/`color-mix()` do not resolve, so none of
 * this is observable from a mounted component — it is read off the source, the
 * same pattern as sharedDrawerDestructiveColor.test.ts.
 */

interface Dialog {
  name: string
  file: string
  /** The class attribute the dialog's primary footer button must carry. */
  footerButton: RegExp
}

const DIALOGS: Dialog[] = [
  {
    name: 'DialogOverlay (confirm/alert/prompt)',
    file: join(__dirname, '..', 'DialogOverlay.vue'),
    // Four possible actions; each must take the shared base class.
    footerButton: /class="fbtn dlg-(generate|extra|cancel|ok)"/,
  },
  {
    name: 'AgentInstallDialog',
    file: join(__dirname, '..', '..', 'AgentInstallDialog.vue'),
    footerButton: /class="fbtn"/,
  },
]

/** Declaration block of the rule whose selector LIST contains `sel`.
 *
 *  A selector may be a comma group (`.a,\n.a:hover { … }`), so matching the
 *  selector as a literal prefix against the `{` misses every member but the
 *  last — the exact trap the design guide warns about. Split each rule's
 *  prelude on commas and compare members instead. */
function ruleDecls(css: string, sel: string): string | null {
  const ruleRe = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = ruleRe.exec(css))) {
    const members = m[1].split(',').map((s) => s.trim())
    if (members.includes(sel)) return m[2]
  }
  return null
}

/** Red hues that are not theme tokens. `#fff` is deliberately absent: a white
 *  label on a solid fill is the same convention `.fbtn-primary` uses. */
const RED_LITERAL = /#(?:d32f2f|ef4444|dc2626|b91c1c|f87171|fca5a5|cf222e|e5484d)\b/i

for (const { name, file, footerButton } of DIALOGS) {
  const source = readFileSync(file, 'utf8')
  const template = source.slice(0, source.indexOf('<script'))
  const style = source.slice(source.indexOf('<style'))
  const styleNoComments = style.replace(/\/\*[\s\S]*?\*\//g, '')

  describe(`${name} footer buttons use the shared .fbtn spec`, () => {
    it('imports the shared footer-button stylesheet', () => {
      // Without this the .fbtn classes render with no geometry at all — the
      // "displays but has no shape" failure mode the design guide calls out.
      expect(source).toMatch(/import '@\/assets\/modal-footer-btn\.css'/)
    })

    it('gives the footer button the .fbtn base class', () => {
      expect(template, 'footer button must carry .fbtn').toMatch(footerButton)
    })

    it('has no leftover bespoke .dlg-btn rule', () => {
      // The old button language must be fully gone, or a stray rule could still
      // win over the shared pill on shape.
      expect(styleNoComments).not.toMatch(/\.dlg-btn\b/)
    })

    it('carries no hardcoded red anywhere in its own styles', () => {
      // A red literal cannot adapt across the 36 themes — the destructive
      // colour must come from `var(--color-red)`. Scanned across the whole
      // style block rather than a hand-listed set of selectors: a filter keyed
      // on class names silently matched nothing once the bespoke `.dlg-btn`
      // rules were deleted, making the check pass vacuously. Both files are
      // dialog chrome, where every colour should be token-driven.
      expect(
        styleNoComments.length,
        'the style block must be non-trivial, or this guard is vacuous',
      ).toBeGreaterThan(200)
      expect(styleNoComments, `${name} must not hardcode a red`).not.toMatch(RED_LITERAL)
    })
  })
}

// ── DialogOverlay-specific structure ─────────────────────────────────────────

const overlayFile = join(__dirname, '..', 'DialogOverlay.vue')
const overlaySource = readFileSync(overlayFile, 'utf8')
const overlayTemplate = overlaySource.slice(0, overlaySource.indexOf('<script'))
const overlayStyle = overlaySource.slice(overlaySource.indexOf('<style'))
const overlayStyleNoComments = overlayStyle.replace(/\/\*[\s\S]*?\*\//g, '')

/** The `class="…"` attribute value of the element carrying `anchor`. */
function classesOf(anchor: string): string {
  const m = overlayTemplate.match(new RegExp(`class="([^"]*\\b${anchor}\\b[^"]*)"`))
  expect(m, `${anchor} must exist in the template`).not.toBeNull()
  return m![1]
}

describe('DialogOverlay button variants', () => {
  it('gives every action button the .fbtn base class', () => {
    for (const anchor of ['dlg-generate', 'dlg-extra', 'dlg-cancel', 'dlg-ok']) {
      expect(classesOf(anchor), `${anchor} must carry .fbtn`).toMatch(/\bfbtn\b/)
    }
  })

  it('uses the primary variant for a normal confirm and danger for a destructive one', () => {
    // The variant is bound, not written as a literal class, so assert on the
    // binding expression rather than the static attribute.
    const ok = overlayTemplate.match(/class="fbtn dlg-ok"[\s\S]*?:class="([^"]+)"/)
    expect(ok, 'the confirm button must bind a variant class').not.toBeNull()
    expect(ok![1]).toContain('fbtn-danger')
    expect(ok![1]).toContain('fbtn-primary')
    expect(ok![1]).toContain('dangerous')
  })

  it('renders the two-step destructive action as .fbtn-danger', () => {
    expect(classesOf('dlg-extra')).toMatch(/\bfbtn-danger\b/)
  })

  it('derives the primed destructive fill from the theme red token, not a literal', () => {
    const decls = ruleDecls(overlayStyleNoComments, '.dlg-actions .dlg-extra-primed')
    expect(decls, '.dlg-actions .dlg-extra-primed rule must exist').not.toBeNull()
    expect(decls).toMatch(/background:\s*var\(--color-red\)/)
  })

  it('does not hand-write dark overrides for the neutral or variant buttons', () => {
    // The shared sheet already lifts .fbtn / .fbtn-danger / … for dark themes.
    // A local override would be dead weight that can drift from the token.
    // The single legitimate exception is pinning the primed solid fill's label
    // back to white (the shared dark rule would otherwise pastel it).
    expect(overlayStyleNoComments).not.toMatch(
      /\[data-theme-base="dark"\][^{]*\.dlg-(cancel|ok|extra)\b(?!-primed)/,
    )
  })
})
