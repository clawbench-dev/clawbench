import { describe, it, expect } from 'vitest'
import config from '../../../../stylelint.config.js'

/**
 * Guards for the stylelint setup itself.
 *
 * Regression: stylelint 17 moved every stylistic rule (indentation,
 * declaration-colon-space-after, …) out to @stylistic/stylelint-plugin. A
 * config that still names them does not merely no-op — stylelint reports
 * "Unknown rule" *as a violation on every file*, so the lint gate fails for a
 * reason that has nothing to do with the CSS under test. Turning such a rule
 * `off` (`null`) does not help; the rule name must not appear at all.
 *
 * NOTE: this test deliberately does NOT `import stylelint` to enumerate the
 * live rule set. `stylelint` is a lint-only dependency installed in
 * `web/node_modules`, but the frontend coverage job runs vitest from the repo
 * root against the root `node_modules` — importing it there fails to resolve
 * and takes the whole suite down. The authoritative "unknown rule" check
 * happens in `npm run lint:css` itself (stylelint validates its own config and
 * exits non-zero); the assertions below cover the policy choices that
 * stylelint cannot know about.
 *
 * The other half is scope: this repo's CSS lives in both `web/css` and
 * `web/src`, so a config that ignores either directory silently stops linting
 * half the styles.
 */

/**
 * Stylistic rules removed from stylelint core in v16/v17 (now provided by
 * @stylistic/stylelint-plugin). Naming any of these in `rules` is the exact
 * footgun described above.
 */
const REMOVED_STYLISTIC_RULES = [
  'indentation',
  'declaration-colon-space-after',
  'declaration-colon-space-before',
  'declaration-block-semicolon-newline-after',
  'declaration-block-trailing-semicolon',
  'block-closing-brace-newline-after',
  'block-opening-brace-space-before',
  'color-hex-case',
  'function-comma-space-after',
  'function-comma-space-before',
  'number-leading-zero',
  'number-no-trailing-zeros',
  'property-case',
  'selector-combinator-space-after',
  'selector-list-comma-newline-after',
  'string-quotes',
  'unit-case',
  'value-list-comma-space-after',
]

describe('stylelint configuration', () => {
  it('does not name stylistic rules removed from stylelint core', () => {
    const declared = Object.keys(config.rules ?? {})
    const offenders = declared.filter((rule) => REMOVED_STYLISTIC_RULES.includes(rule))
    expect(
      offenders,
      'these rules were moved to @stylistic/stylelint-plugin; naming them makes ' +
        'stylelint report "Unknown rule" on every file',
    ).toEqual([])
  })

  it('extends the shared configs for plain CSS and Vue SFC <style> blocks', () => {
    const extendsList = ([] as string[]).concat(config.extends as string | string[])
    expect(extendsList).toContain('stylelint-config-standard')
    expect(extendsList).toContain('stylelint-config-recommended-vue')
  })

  it('keeps the bug-catching rules enabled', () => {
    const rules = config.rules ?? {}
    // Explicitly configured to `null` would mean "off"; these must stay on.
    for (const rule of [
      'block-no-empty',
      'declaration-block-no-duplicate-properties',
    ]) {
      expect(rules[rule], `${rule} must not be disabled`).not.toBe(null)
    }
  })

  it('does not silence real problems via ignoreFiles', () => {
    const ignored = ([] as string[]).concat(config.ignoreFiles as string | string[])
    // Only build output / vendored bundles may be ignored — never source.
    expect(ignored.some((p) => p.startsWith('src') || p.startsWith('css'))).toBe(false)
    expect(ignored).toContain('public/vendor/**')
  })
})
