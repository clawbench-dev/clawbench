import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Theme-safety guards for the conversation-share dialog's message list.
 *
 * Why these exist: the row list shipped with three theme defects that no unit
 * test could see (jsdom has no CSS engine and the component tests assert text
 * and structure only):
 *
 *  1. A dead custom property. `--border-color-subtle` was used for the row
 *     separator but declared in NO theme (36 themes in variables.css, zero
 *     definitions). Every theme therefore silently fell through to the
 *     hardcoded light-only `#eaeef2` fallback — on a dark theme the separator
 *     was a near-white hairline.
 *  2. Hardcoded palette colours. The assistant role chip used `#eaf6ef` /
 *     `#2f6b4a` and the "cannot share yet" flag used `#9a6700`. These are
 *     light-theme values: on a dark theme the chip stayed pale green.
 *  3. Native checkbox chrome, which the OS draws and no theme can reach.
 *
 * The fix derives every colour from theme tokens (via color-mix for tints), so
 * these tests assert that no raw colour literal and no undefined custom
 * property comes back. They are source-sniffing tests by necessity — the whole
 * point is that the defect is invisible to the runtime.
 */

const DIALOG = 'src/components/session/SessionShareDialog.vue'

function read(rel: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, rel), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${rel} not found from cwd: ${process.cwd()}`)
}

/** The `<style scoped>` body of the dialog. */
function dialogCss(): string {
  const src = read(DIALOG)
  const marker = '<style scoped>'
  const i = src.indexOf(marker)
  expect(i, `${DIALOG} must have a <style scoped> block`).toBeGreaterThan(-1)
  const after = src.slice(i + marker.length)
  return after.includes('</style>') ? after.slice(0, after.indexOf('</style>')) : after
}

/** CSS with comments stripped, so prose examples are never mistaken for code. */
function code(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Every custom property name defined anywhere in the theme file. */
function definedTokens(): Set<string> {
  const vars = read('css/variables.css')
  const out = new Set<string>()
  for (const m of vars.matchAll(/(--[a-z0-9-]+)\s*:/g)) out.add(m[1])
  return out
}

/** The CSS declaration values in the dialog, keyed by the property name. */
function declarations(css: string): Array<{ prop: string; value: string }> {
  const out: Array<{ prop: string; value: string }> = []
  for (const m of code(css).matchAll(/([-a-z]+)\s*:\s*([^;{}]+);/g)) {
    out.push({ prop: m[1], value: m[2].trim() })
  }
  return out
}

describe('SessionShareDialog message list is theme-safe', () => {
  const css = dialogCss()

  it('has no bare colour literals in the message-list rules', () => {
    // A colour written literally bypasses the theme system: `#cf222e` stayed
    // bright red on a dark theme, and `#eaf6ef` left the assistant chip pale
    // green. A `var(--token, #fallback)` pair is NOT a violation — that is the
    // house convention (the token is authoritative and the fallback only
    // matters if a theme forgets it), so var() calls are stripped before the
    // scan. The one deliberate literal is the checkbox tick: it sits on the
    // accent fill, where a themed colour would be wrong on every theme.
    const offenders: Array<{ prop: string; value: string }> = []
    for (const { prop, value } of declarations(css)) {
      if (!/color|background|border|fill|outline|shadow/.test(prop)) continue
      // Remove var(...) including nested fallbacks, then look for leftovers.
      let bare = value
      let prev: string
      do {
        prev = bare
        bare = bare.replace(/var\([^()]*(?:\([^()]*\)[^()]*)*\)/g, '')
      } while (bare !== prev)
      if (!/#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(/i.test(bare)) continue
      if (prop === 'border' && value === 'solid #fff') continue
      offenders.push({ prop, value })
    }

    expect(
      offenders,
      `these declarations use a bare colour instead of a theme token: ` +
        offenders.map((o) => `${o.prop}: ${o.value}`).join('; '),
    ).toEqual([])
  })

  it('references no custom property that is undefined in every theme', () => {
    // A typo'd or invented token is not an error at runtime: the declaration
    // just falls back, or resolves to nothing. `--border-color-subtle` shipped
    // that way and every theme silently used its light-only fallback.
    const defined = definedTokens()
    const used = new Set<string>()
    for (const m of code(css).matchAll(/var\(\s*(--[a-z0-9-]+)/g)) used.add(m[1])

    const undefinedTokens = [...used].filter((t) => !defined.has(t))
    expect(
      undefinedTokens,
      `these custom properties are used but defined in no theme: ${undefinedTokens.join(', ')}`,
    ).toEqual([])
  })

  it('keeps the row separator themed rather than on a second token', () => {
    // The separator must derive from --border-color. Reintroducing a bespoke
    // token here is exactly how the undefined-property bug happened.
    const rowRule = code(css).match(/\.session-share-dialog-row\s*\{([\s\S]*?)\}/)
    expect(rowRule, '.session-share-dialog-row rule must exist').not.toBeNull()
    expect(rowRule![1]).toMatch(/border-bottom:\s*1px solid color-mix\(in srgb, var\(--border-color\)/)
  })

  it('tints the selected row from the accent token', () => {
    // Selection must read as the same accent wash the session list uses, and
    // must not be a fixed background that a running/checked state could clash
    // with.
    const checked = code(css).match(/\.session-share-dialog-row\.is-checked\s*\{([\s\S]*?)\}/)
    expect(checked, '.session-share-dialog-row.is-checked rule must exist').not.toBeNull()
    expect(checked![1]).toContain('color-mix(in srgb, var(--accent-color)')
  })

  it('styles both role chips from theme tokens', () => {
    // Both roles must be distinguishable and themed; a missing role rule would
    // leave the assistant chip inheriting the base (user) tint.
    for (const role of ['user', 'assistant']) {
      const rule = code(css).match(
        new RegExp(`\\.session-share-dialog-role\\.role-${role}\\s*\\{([\\s\\S]*?)\\}`),
      )
      expect(rule, `.role-${role} rule must exist`).not.toBeNull()
      expect(rule![1], `.role-${role} must tint from a token`).toContain('color-mix(in srgb, var(--')
    }
  })

  it('sizes the role chip as a square icon badge, not a text pill', () => {
    // The chip renders only an icon now. Without an explicit box it would
    // collapse to a sliver (an inline-flex with no content and no padding).
    const rule = code(css).match(/\.session-share-dialog-role\s*\{([\s\S]*?)\}/)
    expect(rule, '.session-share-dialog-role rule must exist').not.toBeNull()
    expect(rule![1]).toMatch(/width:\s*20px/)
    expect(rule![1]).toMatch(/height:\s*20px/)
    expect(rule![1], 'a text pill would set horizontal padding').not.toMatch(
      /padding:\s*0\s+var\(--space-3\)/,
    )
  })

  it('replaces the native checkbox so the OS chrome cannot leak in', () => {
    // Without `appearance: none` the UA paints a system checkbox whose colours
    // ignore the theme entirely.
    const check = code(css).match(/\.session-share-dialog-check\s*\{([\s\S]*?)\}/)
    expect(check, '.session-share-dialog-check rule must exist').not.toBeNull()
    expect(check![1]).toContain('appearance: none')
    // And it must actually show selection via the accent token.
    const checkedRule = code(css).match(/\.session-share-dialog-check:checked\s*\{([\s\S]*?)\}/)
    expect(checkedRule, 'the checked checkbox rule must exist').not.toBeNull()
    expect(checkedRule![1]).toContain('var(--accent-color)')
  })

  it('keeps the row dense (information density is a requirement, not a preference)', () => {
    // The restyle must not inflate the row. Assert the vertical padding stays
    // in single digits so ~9 rows still fit the 320px window.
    const rowRule = code(css).match(/\.session-share-dialog-row\s*\{([\s\S]*?)\}/)
    const padding = rowRule![1].match(/padding:\s*(\d+)px\s+var\(--space-\d+\)/)
    expect(padding, 'the row must use a literal vertical padding token pair').not.toBeNull()
    expect(Number(padding![1]), 'row vertical padding must stay under 10px').toBeLessThan(10)
  })
})
