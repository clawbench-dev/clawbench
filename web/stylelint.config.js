/**
 * Stylelint config — CSS and Vue `<style>` linting for the web front end.
 *
 * Scope: this repo's styles live in two places — plain `.css` files
 * (`web/css`, `web/src/assets`) and the `<style>` blocks of `.vue` SFCs.
 * Both are linted (see the `lint:css` npm script).
 *
 * Philosophy: this codebase predates any CSS linting, so the goal is to catch
 * *bugs* on day one, not to reformat 166 files. The standard config is
 * therefore relaxed in three groups:
 *
 *   1. Cosmetic/formatting rules — off. The repo already uses 4-space indent
 *      and single-line rules deliberately; enforcing the standard config's
 *      preferences would only create churn.
 *   2. Vendor prefixes — off. This is an Android WebView + desktop shell app;
 *      `-webkit-user-select`, `-webkit-backdrop-filter`, `-webkit-mask-*` and
 *      `-moz-tab-size` are hand-written on purpose (no autoprefixer in the
 *      build), so flagging them would be wrong.
 *   3. Deprecation notices — downgraded to `warning` severity. Notably the 72
 *      `word-break: break-word` uses are deprecated but *not* interchangeable
 *      with `overflow-wrap: break-word` (the former also affects min-content
 *      sizing), so they are surfaced without blocking the build.
 *
 * Everything else — duplicate properties, empty blocks, unknown properties,
 * invalid selectors, `0px` units — stays on at error severity.
 */
export default {
  extends: ['stylelint-config-standard', 'stylelint-config-recommended-vue'],

  rules: {
    // ── 1. Cosmetic / formatting — off ───────────────────────────────
    // NOTE: `indentation` and the other stylistic rules were REMOVED from
    // stylelint 17 (moved to @stylistic/stylelint-plugin). Do not re-add them
    // here — an unknown rule name is itself reported as an error.
    'rule-empty-line-before': null,
    'comment-empty-line-before': null,
    'declaration-empty-line-before': null,
    'custom-property-empty-line-before': null,
    'at-rule-empty-line-before': null,
    'declaration-block-single-line-max-declarations': null,
    'no-descending-specificity': null,
    'no-duplicate-selectors': null,
    'declaration-block-no-redundant-longhand-properties': null,
    'shorthand-property-no-redundant-values': null,
    'selector-class-pattern': null,
    'keyframes-name-pattern': null,
    'custom-property-pattern': null,
    'alpha-value-notation': null,
    'color-function-notation': null,
    'color-function-alias-notation': null,
    'value-keyword-case': null,
    'font-family-name-quotes': null,
    'number-max-precision': null,
    'media-feature-range-notation': null,
    'selector-not-notation': null,
    'import-notation': null,
    // Hex length is mixed: 6-digit dominates (~3400) but 3-digit is also used
    // in real declarations (~100) and far more often inside `var()` fallbacks.
    // Both forms are valid and equivalent; normalising is not a bug fix.
    'color-hex-length': null,

    // ── 2. Intentional vendor prefixes (Android WebView / desktop shell) ──
    'property-no-vendor-prefix': null,

    // ── 3. Deprecations: visible but non-blocking ────────────────────
    'declaration-property-value-keyword-no-deprecated': [
      true,
      { severity: 'warning' },
    ],
    'property-no-deprecated': [true, { severity: 'warning' }],

    // ── Kept on (inherited from the standard config) ─────────────────
    // block-no-empty, declaration-block-no-duplicate-properties,
    // property-no-unknown, selector-pseudo-class-no-unknown,
    // font-family-no-missing-generic-family-keyword, length-zero-no-unit,
    // no-duplicate-at-import-rules, named-grid-areas-no-invalid, …
  },

  ignoreFiles: [
    'dist/**',
    'node_modules/**',
    'coverage/**',
    '.clawbench-web/vendor/**',
  ],
}
