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
 * The design px number of a `prop:` declaration in `css`, or NaN.
 *
 * Use this when the assertion is about the VALUE (e.g. "the rail is 3 wide"),
 * which reads better than a truthiness check and reports the actual number on
 * failure.
 */
export function designPx(css: string, prop: string): number {
  const viaDp = css.match(
    new RegExp(`${prop}:\\s*calc\\(\\s*(-?[\\d.]+)\\s*\\*\\s*var\\(--dp\\)\\s*\\)`),
  )
  if (viaDp) return Number(viaDp[1])
  const literal = css.match(new RegExp(`${prop}:\\s*(-?[\\d.]+)px`))
  return literal ? Number(literal[1]) : NaN
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

