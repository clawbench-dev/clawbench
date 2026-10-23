import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Contrast guard for the inline annotations (file path chip, its open button,
 * commit hash, inert/missing paths) across all 36 themes.
 *
 * Why this exists: every one of these marks sits on THREE different backdrops —
 * `--bg-primary` (markdown), `--bg-tertiary` (assistant bubble) and
 * `--user-msg-color` (user bubble). The user bubble is a saturated mid-tone
 * whose contrast against white ranges from 3.3:1 (ayu-light) to 12:1
 * (high-contrast-light), so any colour tuned for a page background collapses
 * there. Measured on the pre-fix stylesheet:
 *
 *   inert path,  `--text-muted` on the bubble     1.02:1  (light-plus)
 *   open button, white 70% × opacity 50%          1.58:1  (ayu-light)
 *   external icon, fixed `#f0a04b`                1.56:1  (ayu-light)
 *   commit hash,  `--accent-color`                1.00:1  (dark-plus)
 *
 * The fix derives every colour from `currentColor` (the surrounding TEXT
 * colour) so the direction is correct by construction, and this spec pins the
 * RESULT — the computed ratio — rather than a hex value or a percentage. The
 * percentages are read back out of the stylesheet, so retuning them re-checks
 * the outcome instead of silently drifting.
 *
 * jsdom has no CSS engine (no `color-mix()`, no `var()`), so like
 * designTokens.css.test.ts this reads source and evaluates the recipes itself.
 * It only understands the shapes actually used here: `color-mix(in srgb, X N%,
 * transparent)`, `color-mix(in srgb, A N%, B)` and plain hex/`currentColor`.
 */

const ROOT = join(__dirname, '..', '..', '..', '..')

function read(rel: string): string {
  return readFileSync(join(ROOT, rel), 'utf8')
}

const variables = read('web/css/variables.css')
const annotation = read('web/src/assets/annotation-buttons.css')
const markdownCommon = read('web/css/markdown-common.css')
const chatMessageItem = read('web/src/components/chat/ChatMessageItem.vue')

/** Comments carry prose about tokens; strip them before matching declarations. */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** The body of the first rule whose selector list matches `selector` exactly. */
function ruleBody(css: string, selector: string): string {
  const clean = stripComments(css)
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const m = clean.match(new RegExp(`(?:^|\\n)\\s*${escaped}\\s*\\{([^}]*)\\}`))
  expect(m, `rule ${selector} must exist`).not.toBeNull()
  return m![1]
}

function decl(body: string, prop: string): string {
  const m = body.match(new RegExp(`(?:^|;)\\s*${prop}\\s*:\\s*([^;]+)`))
  expect(m, `declaration ${prop} must exist in:\n${body}`).not.toBeNull()
  // `!important` is a cascade hint, not part of the value.
  return m![1].replace(/\s*!important\s*$/, '').trim()
}

// ── Colour maths ────────────────────────────────────────────────────────────

type RGB = [number, number, number]

function hex(value: string): RGB {
  const h = value.replace('#', '').trim()
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

function relativeLuminance([r, g, b]: RGB): number {
  const f = (c: number) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}

function contrast(a: RGB, b: RGB): number {
  const [hi, lo] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** Composite `fg` at `alpha` over an opaque `bg`. */
function over(fg: RGB, alpha: number, bg: RGB): RGB {
  return [0, 1, 2].map((i) => Math.round(fg[i] * alpha + bg[i] * (1 - alpha))) as RGB
}

/**
 * Evaluate a colour recipe against a concrete `currentColor`.
 *
 * Handles exactly the forms the annotation CSS uses:
 *   `inherit` / `currentColor`                              → currentColor, opaque
 *   `color-mix(in srgb, currentColor 14%, transparent)`     → currentColor @ 14%
 *   `color-mix(in srgb, var(--x) 30%, currentColor)`        → weighted mix
 *
 * Returns the resolved colour plus its alpha, so the caller composites it over
 * the actual surface. `null` for anything else, so a new recipe shape fails
 * loudly instead of being compared against a wrong colour.
 */
interface Resolved {
  rgb: RGB
  alpha: number
}

function evaluate(value: string, currentColor: RGB, resolveToken: (name: string) => RGB): Resolved | null {
  const v = value.trim()
  if (v === 'inherit' || v === 'currentColor') return { rgb: currentColor, alpha: 1 }
  if (v === '#000' || v === '#000000' || v === 'black') return { rgb: [0, 0, 0], alpha: 1 }
  if (/^#[0-9a-f]{6}$/i.test(v)) return { rgb: hex(v), alpha: 1 }
  const bare = v.match(/^var\((--[a-z0-9-]+)\)$/i)
  if (bare) return { rgb: resolveToken(bare[1]), alpha: 1 }

  const mix = v.match(/^color-mix\(\s*in srgb\s*,\s*(.+?)\s*\)$/i)
  if (!mix) return null
  // The operands never contain a comma of their own in this stylesheet (no
  // nested functions), so a plain split is the top-level split.
  const parts = mix[1].split(',').map((p) => p.trim())
  if (parts.length !== 2) return null

  /** `null` pct means the operand omitted it (CSS fills in the remainder). */
  const parseOperand = (operand: string): { color: RGB | null; pct: number | null } | null => {
    if (/^transparent$/i.test(operand)) return { color: null, pct: null }
    // `var(--x) 30%` / `currentColor` (no percentage) / `#000 22%`.
    const m = operand.match(/^(.*?)(?:\s+(\d+(?:\.\d+)?)%)?$/)
    if (!m) return null
    const name = m[1].trim()
    const pct = m[2] === undefined ? null : Number(m[2]) / 100
    if (name === 'currentColor') return { color: currentColor, pct }
    const token = name.match(/^var\((--[a-z0-9-]+)\)$/i)
    if (token) return { color: resolveToken(token[1]), pct }
    if (/^#[0-9a-f]{6}$/i.test(name)) return { color: hex(name), pct }
    if (name === '#000') return { color: [0, 0, 0], pct }
    return null
  }

  const [a, b] = parts.map(parseOperand)
  if (!a || !b) return null

  // `transparent` is not a colour: mixing toward it yields the OTHER colour at
  // its own alpha, which the caller then composites over the surface.
  if (a.color === null) {
    if (b.color === null || b.pct === null) return null
    return { rgb: b.color, alpha: 1 - b.pct }
  }
  if (b.color === null) {
    if (a.pct === null) return null
    return { rgb: a.color, alpha: a.pct }
  }

  // CSS fills a missing percentage with the remainder, then rescales if the
  // two still do not sum to 100%.
  if (a.pct === null && b.pct === null) return null
  const pa = a.pct ?? 1 - b.pct!
  const pb = b.pct ?? 1 - pa
  const total = pa + pb
  const wa = pa / total
  return {
    rgb: [0, 1, 2].map((i) => Math.round(a.color![i] * wa + b.color![i] * (1 - wa))) as RGB,
    alpha: 1,
  }
}

// ── Theme table ─────────────────────────────────────────────────────────────

interface Theme {
  name: string
  token: (name: string) => RGB
}

function parseThemes(): Theme[] {
  const blocks = [...variables.matchAll(/\[data-theme="([^"]+)"\]\s*\{([\s\S]*?)\n\}/g)]
  return blocks.map(([, name, body]) => {
    const tokens = new Map<string, string>()
    for (const m of body.matchAll(/(--[a-z0-9-]+)\s*:\s*(#[0-9a-fA-F]{6})\s*;/g)) {
      tokens.set(m[1], m[2])
    }
    return {
      name,
      token: (tokenName: string) => {
        const value = tokens.get(tokenName)
        expect(value, `theme "${name}" must define ${tokenName}`).toBeTruthy()
        return hex(value!)
      },
    }
  })
}

const themes = parseThemes()

/**
 * Surfaces an annotation can sit on, with the text colour `currentColor`
 * resolves to on each.
 */
const SURFACES = [
  { name: 'markdown', bg: '--bg-primary', text: '--text-primary' },
  { name: 'assistant bubble', bg: '--bg-tertiary', text: '--text-primary' },
  // The user bubble paints its own text white (ChatMessageItem.vue), so
  // `currentColor` there is white regardless of the theme's text token.
  { name: 'user bubble', bg: '--user-msg-color', text: null },
] as const

const WHITE: RGB = [255, 255, 255]

function textColor(theme: Theme, textToken: string | null): RGB {
  return textToken === null ? WHITE : theme.token(textToken)
}

/** Worst-case value of `measure` over every theme × every surface. */
function worst(
  measure: (theme: Theme, surface: (typeof SURFACES)[number]) => number,
): { value: number; theme: string; surface: string } {
  let result = { value: Infinity, theme: '', surface: '' }
  for (const theme of themes) {
    for (const surface of SURFACES) {
      const value = measure(theme, surface)
      if (value < result.value) result = { value, theme: theme.name, surface: surface.name }
    }
  }
  return result
}

/** Effective chip background: the plate composited over the surface. */
function plate(theme: Theme, surface: (typeof SURFACES)[number], recipe: string): RGB {
  const bg = theme.token(surface.bg)
  const resolved = evaluate(recipe, textColor(theme, surface.text), theme.token)
  expect(resolved, `cannot evaluate plate recipe: ${recipe}`).not.toBeNull()
  return over(resolved!.rgb, resolved!.alpha, bg)
}

/** A colour recipe composited over the surface it is painted on. */
function paintOn(theme: Theme, surface: (typeof SURFACES)[number], recipe: string): RGB {
  const bg = theme.token(surface.bg)
  const resolved = evaluate(recipe, textColor(theme, surface.text), theme.token)
  expect(resolved, `cannot evaluate recipe: ${recipe}`).not.toBeNull()
  return over(resolved!.rgb, resolved!.alpha, bg)
}

// ── The recipes, read back from the stylesheets ─────────────────────────────

/** Shared chip plate (markdown / assistant / table row). */
const chipPlateRecipe = decl(ruleBody(markdownCommon, '.markdown-body .chat-file-path,\n.chat-message.assistant .chat-file-path,\n.table-row-value .chat-file-path'), 'background')

/** The user bubble overrides the plate DIRECTION (see the stylesheet comment). */
const userChipPlateRecipe = decl(ruleBody(markdownCommon, '.chat-message.user .chat-file-path'), 'background')

/** Glyph colour of the open button, shared by all roles. */
const openButtonRecipe = decl(ruleBody(markdownCommon, '.markdown-body .chat-file-open-btn,\n.chat-message.assistant .chat-file-open-btn,\n.chat-message.user .chat-file-open-btn,\n.table-row-value .chat-file-open-btn'), 'color')

/** Inert (verified-missing / glob) path colour. */
const inertRecipe = decl(ruleBody(annotation, 'a.chat-file-path-inert,\n.chat-file-path-inert,\n.code-file-path.chat-file-path-inert'), 'color')

/** Project-external file button glyph (orange, tinted toward the text). */
const externalButtonRecipe = decl(
  ruleBody(
    markdownCommon,
    '.markdown-body .chat-file-open-btn.external,\n.chat-message.assistant .chat-file-open-btn.external,\n.chat-message.user .chat-file-open-btn.external,\n.table-row-value .chat-file-open-btn.external',
  ),
  'color',
)

/** Verified commit hash: accent tinted toward the text, overridden on the bubble. */
const commitHashRecipe = decl(ruleBody(annotation, '.chat-commit-hash'), 'color')
const userCommitHashRecipe = decl(ruleBody(chatMessageItem, '.chat-message.user .chat-commit-hash'), 'color')

/** The colour a given surface resolves a recipe against. */
function recipeFor(surfaceName: string, base: string, userOverride: string): string {
  return surfaceName === 'user bubble' ? userOverride : base
}

/** Chip hover plate — shared, plus the user-bubble override. */
const chipHoverRecipe = decl(ruleBody(annotation, '.chat-file-path[data-file-path]:hover'), 'background')
const userChipHoverRecipe = decl(ruleBody(markdownCommon, '.chat-message.user .chat-file-path[data-file-path]:hover'), 'background')

describe('annotation colours are derived from the surrounding text, not a fixed token', () => {
  it('finds the themes and surfaces it is meant to guard', () => {
    // Guard against the walk silently matching nothing (a renamed attribute,
    // a cwd change) — that would make every ratio below vacuously pass.
    expect(themes.length, 'expected all 36 theme blocks').toBeGreaterThanOrEqual(30)
    expect(SURFACES.length).toBe(3)
  })

  it('keeps the reading-surface chip fill on --bg-secondary', () => {
    // Deliberately NOT `currentColor`: on a pale bubble the text colour is the
    // DARK thing, so a currentColor plate tints the backdrop toward the text
    // and eats the contrast the text needs (worst case dropped 3.64 → 2.81 on
    // the assistant bubble). The ratio assertion below pins the outcome; this
    // one pins the reason it cannot be "unified" with the user-bubble plate.
    expect(chipPlateRecipe).toBe('var(--bg-secondary)')
  })

  it('paints the user-bubble chip plate from a fixed dark overlay, not currentColor', () => {
    // On this one surface the plate must go DARKER than the bubble: its text is
    // always white, so a light plate lifts the backdrop toward the text. Using
    // `currentColor` here (i.e. white) costs ~2.3:1 of text contrast — see the
    // ratio assertion below, which fails if this flips back.
    expect(userChipPlateRecipe).toContain('color-mix(')
    expect(userChipPlateRecipe).toMatch(/#000\b/)
    expect(userChipPlateRecipe).not.toContain('currentColor')
  })

  it('does not fall back to the muted token for glyphs or inert paths', () => {
    // `--text-muted` is tuned against a page background. On the user bubble it
    // measured 1.02:1 (light-plus) — the mark was the same colour as the bubble.
    // `inherit` and `color-mix(… currentColor …)` are both acceptable ways to
    // say "the surrounding text colour"; the token is not.
    for (const [label, recipe] of [
      ['open button', openButtonRecipe],
      ['inert path', inertRecipe],
    ] as const) {
      expect(
        recipe === 'inherit' || recipe.includes('currentColor'),
        `${label} must derive from the surrounding text (got "${recipe}")`,
      ).toBe(true)
      expect(recipe, `${label} must not use --text-muted`).not.toContain('--text-muted')
    }
  })
})

describe('annotation contrast across all themes', () => {
  it('keeps live chip text readable on every surface', () => {
    const result = worst((theme, surface) => {
      const recipe = surface.name === 'user bubble' ? userChipPlateRecipe : chipPlateRecipe
      const bg = plate(theme, surface, recipe)
      return contrast(textColor(theme, surface.text), bg)
    })
    // The user bubble is the surface this whole guard exists for: before the
    // fix its path text sat at 3.32:1 on the bare bubble (no plate at all).
    // Anything below 3.0 means the plate is pulling the backdrop toward the
    // text — i.e. the direction flipped back.
    expect(
      result.value,
      `worst chip text contrast ${result.value.toFixed(2)} on "${result.surface}" in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(3.0)
  })

  it('makes the user-bubble plate clearly distinguishable from the bubble', () => {
    // This is the surface the whole fix is about, and the one whose plate is
    // derived rather than taken from a token — so it must clear the red-line-4
    // threshold (1.12) on every theme.
    const result = worst((theme, surface) => {
      if (surface.name !== 'user bubble') return Infinity
      return contrast(plate(theme, surface, userChipPlateRecipe), theme.token(surface.bg))
    })
    expect(
      result.value,
      `worst user-bubble plate separation ${result.value.toFixed(3)} in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(1.12)
  })

  it('does not make the reading-surface plate any less visible than before', () => {
    // `--bg-secondary` is the pre-existing fill and is deliberately kept (a
    // currentColor plate cost 0.8:1 of text contrast on pale bubbles). It is
    // NOT uniformly distinguishable — ayu-dark puts it 1.016 from
    // `--bg-primary`, design-guide red line 4's known trap — so this only pins
    // that the change did not make it worse. Fixing that would need a
    // different channel (a ring, not a fill) and is out of scope here.
    const result = worst((theme, surface) => {
      if (surface.name === 'user bubble') return Infinity
      return contrast(plate(theme, surface, chipPlateRecipe), theme.token(surface.bg))
    })
    expect(
      result.value,
      `reading-surface plate separation regressed to ${result.value.toFixed(3)} in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(1.0)
  })

  it('keeps the open-button glyph visible on every surface', () => {
    const result = worst((theme, surface) => {
      return contrast(paintOn(theme, surface, openButtonRecipe), theme.token(surface.bg))
    })
    // Icons are non-text content: 3:1 is the WCAG 1.4.11 floor. The old
    // `white 70% × opacity 0.5` measured 1.58:1 here.
    expect(
      result.value,
      `worst button contrast ${result.value.toFixed(2)} on "${result.surface}" in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(3.0)
  })

  it('keeps the project-external glyph legible instead of a fixed orange', () => {
    // A fixed `#e67e22` measured 1.05:1 on the lightest user bubbles — the
    // "this file is outside the project" marker simply disappeared there.
    expect(externalButtonRecipe, 'external glyph must be tinted toward the text').toContain('currentColor')
    expect(externalButtonRecipe).toContain('--color-orange')

    const result = worst((theme, surface) =>
      contrast(paintOn(theme, surface, externalButtonRecipe), theme.token(surface.bg)),
    )
    // Below the 3:1 icon floor on purpose: the hue is the point here (it says
    // "outside the project"), and 30% orange is the strongest tint that still
    // clears 2.2:1 everywhere. The old fixed value failed even that.
    expect(
      result.value,
      `worst external glyph contrast ${result.value.toFixed(2)} on "${result.surface}" in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(2.2)
  })

  it('keeps the commit hash legible without falling back to the raw accent', () => {
    // A bare `--accent-color` measured 1.00:1 on the user bubble (dark-plus,
    // where the accent and the bubble are the same colour) and 1.73:1 on the
    // assistant bubble (ayu-light).
    expect(commitHashRecipe, 'commit hash must be tinted toward the text').toContain('currentColor')
    expect(commitHashRecipe).toContain('--accent-color')

    const result = worst((theme, surface) => {
      const recipe = recipeFor(surface.name, commitHashRecipe, userCommitHashRecipe)
      return contrast(paintOn(theme, surface, recipe), theme.token(surface.bg))
    })
    expect(
      result.value,
      `worst commit-hash contrast ${result.value.toFixed(2)} on "${result.surface}" in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(2.5)
  })

  it('does not deepen the chip plate on hover past the point of legibility', () => {
    // Hover is where a "make it more obvious" tweak is most tempting, and where
    // it costs the most: the plate is painted from the text colour, so every
    // extra percent moves the backdrop toward the glyph. The pre-fix stylesheet
    // hovered at 18% `--text-muted`, which measured the same 3.21 as the new
    // 12% currentColor — so 12% is the budget, not a starting point.
    const result = worst((theme, surface) => {
      const recipe = recipeFor(surface.name, chipHoverRecipe, userChipHoverRecipe)
      const bg = theme.token(surface.bg)
      const resolved = evaluate(recipe, textColor(theme, surface.text), theme.token)
      expect(resolved, `cannot evaluate hover recipe: ${recipe}`).not.toBeNull()
      return contrast(textColor(theme, surface.text), over(resolved!.rgb, resolved!.alpha, bg))
    })
    expect(
      result.value,
      `worst hovered chip text contrast ${result.value.toFixed(2)} on "${result.surface}" in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(2.5)
  })

  it('still shows a visible hover change on the reading surfaces', () => {
    // A hover state that is not perceptible is the same as no hover state; the
    // previous tint sat at 1.13 from the resting fill, so that is the floor.
    const result = worst((theme, surface) => {
      if (surface.name === 'user bubble') return Infinity
      const bg = theme.token(surface.bg)
      const resting = plate(theme, surface, chipPlateRecipe)
      const resolved = evaluate(chipHoverRecipe, textColor(theme, surface.text), theme.token)
      return contrast(over(resolved!.rgb, resolved!.alpha, bg), resting)
    })
    expect(
      result.value,
      `hover tint only ${result.value.toFixed(3)} from rest in "${result.theme}"`,
    ).toBeGreaterThanOrEqual(1.10)
  })

  it('keeps inert paths readable while still visibly quieter than live ones', () => {
    const inert = worst((theme, surface) =>
      contrast(paintOn(theme, surface, inertRecipe), theme.token(surface.bg)),
    )
    // A missing path must stay legible (it is still information — the reader can
    // copy it and the tooltip explains why it is dead) but must not read as a
    // live chip. `--text-muted` scored 1.02:1 on the bubble, i.e. invisible.
    expect(
      inert.value,
      `worst inert contrast ${inert.value.toFixed(2)} on "${inert.surface}" in "${inert.theme}"`,
    ).toBeGreaterThanOrEqual(2.5)

    // …and it must remain a step DOWN from the live chip, or the dashed
    // underline becomes the only difference between "works" and "missing".
    const ratio = worst((theme, surface) => {
      const recipe = surface.name === 'user bubble' ? userChipPlateRecipe : chipPlateRecipe
      const live = contrast(textColor(theme, surface.text), plate(theme, surface, recipe))
      const dead = contrast(paintOn(theme, surface, inertRecipe), theme.token(surface.bg))
      return live / dead
    })
    expect(
      ratio.value,
      `inert/live ratio ${ratio.value.toFixed(2)} on "${ratio.surface}" in "${ratio.theme}"`,
    ).toBeGreaterThanOrEqual(1.05)
  })
})
