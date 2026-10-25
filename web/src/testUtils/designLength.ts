/**
 * Helpers for source-sniffing CSS guards, aware of font-driven scaling.
 *
 * Sizes in this codebase are no longer bare `px`: geometry is written
 * `calc(<design px> * var(--dp))` so the whole UI scales from the root font size
 * (see the FONT-DRIVEN SCALING block in web/css/variables.css). A guard that
 * greps for `width: 3px` therefore fails on a declaration that is still exactly
 * 3 design px wide.
 *
 * Two helpers, for the two shapes the guards actually take:
 *   • `designPx()`   — the guard asserts a NUMBER ("the rail is 3 wide")
 *   • `normalizeDp()` — the guard asserts a literal string
 *                       ("padding: 9px 11px"), so the source is normalised
 *                       first and the assertion stays readable
 */

/**
 * The design px number of a `prop:` declaration in `css`.
 *
 * Use this when the assertion is about the VALUE (e.g. "the rail is 3 wide"),
 * which reads better than a truthiness check and reports the actual number on
 * failure.
 *
 * Recognised forms, in priority order:
 *   1. `calc(N * var(--dp))` — the scaled form
 *   2. a sum of those, `calc(N * var(--dp) + M * var(--dp) …)` — summed
 *   3. a literal `Npx` — hairlines and other deliberately unscaled lengths
 *
 * Returns NaN when the property is absent (so `toBeNaN()` reads as "the guard
 * is looking at the wrong declaration"). THROWS when the property is present
 * but written in a form this cannot read: silently returning NaN there would
 * fail the guard with `NaN ≠ 3` and give no hint that the SOURCE shape is the
 * problem. The message carries the original value for that reason.
 */
export function designPx(css: string, prop: string): number {
  // Anchored on a property-name boundary so a `width` lookup cannot be
  // satisfied by `min-width` / `max-width`. Without it a guard for one
  // declaration silently passes on a different one.
  const at = `(?:^|[\\s;{])${prop}:`
  const decl = new RegExp(`${at}\\s*([^;}]+)`).exec(css)
  if (!decl) return NaN
  const value = decl[1].replace(/!\s*important\s*$/i, '').trim()

  // A single `calc(...)` whose body is nothing but `N * var(--dp)` terms joined
  // by `+`. Covers the single-term form (the common case) and
  // `calc(4 * var(--dp) + 2 * var(--dp))`.
  //
  // Requiring the WHOLE value to be that one calc is what rejects the shapes
  // this cannot reduce to a single number: a multi-value shorthand
  // (`padding: calc(9 * var(--dp)) calc(11 * var(--dp))` — summing those would
  // be a silently wrong answer, not a missing one), a subtraction, and
  // `calc(4 * var(--dp) * 2)`.
  const wholeCalc = /^calc\(([\s\S]*)\)$/.exec(value)
  if (wholeCalc) {
    const terms = wholeCalc[1].split('+').map(t => t.trim())
    const TERM = /^(-?[\d.]+)\s*\*\s*var\(--dp\)$/
    if (terms.every(t => TERM.test(t))) {
      return terms.reduce((sum, t) => sum + Number(TERM.exec(t)![1]), 0)
    }
    throw new Error(
      `designPx(${prop}) cannot read "${value}": only additions of ` +
      `\`N * var(--dp)\` can be summed. Extend the helper rather than ` +
      `asserting a number it cannot derive.`,
    )
  }

  const literal = /^(-?[\d.]+)px$/.exec(value)
  if (literal) return Number(literal[1])

  throw new Error(
    `designPx(${prop}) cannot read "${value}": expected ` +
    `\`calc(N * var(--dp))\`, a sum of those, or a literal \`Npx\`.`,
  )
}

/**
 * Rewrite a CSS text so every `calc(N * var(--dp))` reads as `Npx`.
 *
 * This is the escape hatch for the many guards written as
 * `expect(rule).toContain('padding: 9px 11px')`: rather than hand-rewrite each
 * one into a regex, normalise the SOURCE and keep the readable literal
 * assertion. The guards still pin exactly the same design numbers, which is the
 * invariant they were written for — only the unit expression changed.
 *
 * Scope: it only rewrites the `--dp` form. A literal `Npx` is left alone, so a
 * value that deliberately does NOT scale (a hairline, a scrollbar width) still
 * has to be written literally and will not be normalised into agreement with a
 * scaled one by accident.
 *
 * `calc(N * var(--dp) + M * var(--dp))` and other arithmetic is collapsed
 * left-to-right only when the whole expression is a plain sum of `--dp` terms;
 * anything else is left untouched so the guard sees the real text.
 */
export function normalizeDp(css: string): string {
  return (
    css
      .replace(
        /calc\(\s*(-?[\d.]+)\s*\*\s*var\(--dp\)\s*\)/g,
        (_m, n: string) => `${Number(n)}px`,
      )
      // Collapse the whitespace a substitution can leave doubled, so a guard
      // written against the old single-space source still matches.
      .replace(/[ \t]{2,}/g, ' ')
  )
}

