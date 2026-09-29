import { describe, it, expect } from 'vitest'
import { THEMES } from '@/utils/themeMeta'
import { contrastRatio } from '@/utils/tagColor'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The running-session indicator must be visible on every theme, and must be a
 * SINGLE effect on the row's bottom edge.
 *
 * The signal is a 3px track along the bottom edge of a running row, with a
 * 38% comet travelling across it. It has to work on all 36 themes, which a
 * fixed colour cannot do: the original was a hardcoded green that read fine on
 * dark backgrounds and all but vanished on light ones (1.10:1 against the row,
 * 1.08:1 on the chat button). It is an animated *signal* — "this session is
 * running" — so being near-invisible is a functional failure, not a cosmetic
 * one.
 *
 * Four designs were tried; the three rejected ones are pinned below because
 * each looked reasonable until measured or until it shipped:
 *
 *   1. Fixed green — invisible on light themes (above).
 *   2. Accent tint across the whole row, plus a wide sweep band. This washed
 *      dark themes milky, because there the accent sits far above the row
 *      background (OKLab L +0.50, vs −0.40 on a light theme), so any alpha
 *      that showed the band also flooded the row. The band then read as a
 *      grey smudge on top of it.
 *   3. A 14px masked glow spanning the edge, PLUS an 80% band swept through it.
 *      Each layer was defensible; together they read as two effects stacked on
 *      one edge, and the tall glow smeared the band into ambient lighting. The
 *      tests below pin "exactly one layer" so it cannot come back.
 *   4. The current design: a flat 3px track with a 38% comet, no mask, no
 *      glow, no row tint. One layer, one effect.
 *
 * jsdom has no CSS engine and does not resolve `color-mix()`/`var()`, so this
 * parses variables.css directly and does the colour maths itself.
 */

// ── colour helpers ───────────────────────────────────────────────────────────

type RGB = [number, number, number]

function parseHex(hex: string): RGB {
  const m = /^#([0-9a-f]{6})$/i.exec(hex.trim())
  expect(m, `expected a 6-digit hex colour, got "${hex}"`).not.toBeNull()
  const v = m![1]
  return [
    parseInt(v.slice(0, 2), 16),
    parseInt(v.slice(2, 4), 16),
    parseInt(v.slice(4, 6), 16),
  ]
}

function toHex(c: RGB): string {
  return '#' + c.map((x) => Math.round(x).toString(16).padStart(2, '0')).join('')
}

/** Composite `fg` at `alpha` over an opaque `bg`. */
function over(fg: RGB, bg: RGB, alpha: number): RGB {
  return [
    fg[0] * alpha + bg[0] * (1 - alpha),
    fg[1] * alpha + bg[1] * (1 - alpha),
    fg[2] * alpha + bg[2] * (1 - alpha),
  ]
}

function luminance(c: RGB): number {
  const f = (x: number) => {
    const s = x / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2])
}

/** Perceptual lightness (OKLab L) — needed to compare across light/dark. */
function oklabLightness(c: RGB): number {
  const lin = (x: number) => {
    const s = x / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  const [r, g, b] = c.map(lin)
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
  return 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s
}

// ── stylesheet parsing ───────────────────────────────────────────────────────

const css = readWebFile('css/variables.css')

/** All `--name: value;` declarations inside a block body. */
function declarations(body: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/(--[a-z0-9-]+)\s*:\s*([^;]+);/g)) out[m[1]] = m[2].trim()
  return out
}

/** The body of a `selector { … }` block, or null when absent. */
function blockBody(selector: string): string | null {
  const m = css.match(
    new RegExp(selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{([\\s\\S]*?)\\n\\}'),
  )
  return m ? m[1] : null
}

const rootVars = declarations(blockBody(':root')!)

/** Per-theme colour variables, keyed by theme id. */
const themeVars: Record<string, Record<string, string>> = {}
for (const theme of THEMES) {
  const body = blockBody(`[data-theme="${theme.id}"]`)
  expect(body, `variables.css should define theme "${theme.id}"`).not.toBeNull()
  themeVars[theme.id] = declarations(body!)
}

/** The variables in effect for a theme (theme block + :root fallbacks). */
function varsFor(themeId: string): Record<string, string> {
  return { ...rootVars, ...themeVars[themeId] }
}

/**
 * Resolve a token expression to an opaque colour plus its alpha.
 *
 * Handles the shapes the tokens use: a bare reference to another token, a
 * plain colour, and `color-mix(in srgb, X N%, transparent)` — which does NOT
 * premultiply, so it yields X's exact colour at alpha N.
 */
function evalToken(
  raw: string,
  vars: Record<string, string>,
  depth = 0,
): { color: RGB; alpha: number } {
  expect(depth, `token chain too deep (cycle?): ${raw}`).toBeLessThan(5)

  const bare = raw.match(/^var\((--[a-z-]+)\)$/)
  if (bare) return evalToken(vars[bare[1]], vars, depth + 1)

  if (/^#[0-9a-f]{3,8}$/i.test(raw.trim())) {
    return { color: parseHex(raw.trim()), alpha: 1 }
  }

  const withAlpha = raw.match(/color-mix\(in srgb,\s*(.+?)\s*([\d.]+)%,\s*transparent\s*\)$/)
  if (withAlpha) {
    const inner = evalToken(withAlpha[1], vars, depth + 1)
    return { color: inner.color, alpha: Number(withAlpha[2]) / 100 }
  }

  throw new Error(`unrecognised token expression: ${raw}`)
}

const list = readWebFile('src/components/session/SessionList.vue')

/** The base rule for a selector, anchored to the start of a line. */
function baseRule(source: string, selector: string): string | undefined {
  return source.match(
    new RegExp('\\n' + selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{[^}]*\\}'),
  )?.[0]
}

// ── tests ────────────────────────────────────────────────────────────────────

/**
 * Floors measured across all 36 themes. The original fixed green scored
 * 1.10 / 1.39. The comet head is opaque, so it clears a much higher bar than
 * the old translucent band ever did.
 */
const MIN_HEAD = 1.40

describe('running-session indicator is theme-derived and visible on every theme', () => {
  it('draws the comet from the theme accent, not a literal colour', () => {
    for (const token of ['--running-track', '--running-comet', '--running-head']) {
      expect(rootVars[token], `${token} should be declared`).toBeDefined()
      expect(rootVars[token], `${token} should derive from the theme`).toMatch(
        /var\(--accent-color\)/,
      )
      expect(rootVars[token], `${token} must not hardcode a colour`).not.toMatch(
        /#[0-9a-f]{3,6}/i,
      )
      // Blending the accent away is what turned earlier versions grey.
      expect(rootVars[token]).not.toMatch(/var\(--(text-primary|text-secondary|text-muted)\)/)
      expect(rootVars[token]).not.toMatch(/,\s*(black|white)\s*\)/)
    }
  })

  it('leaves the row background alone — the light is confined to the edge', () => {
    // Design 2 tinted the whole row and washed dark themes milky. Guard the
    // property that fixed it: a running row must not set a background, and no
    // `--running-fill` token should exist to tempt it back.
    expect(rootVars['--running-fill'], '--running-fill should be gone').toBeUndefined()

    // Only rules whose subject IS the running row — `.cross-session-row.running
    // .cross-session-item:hover` is a descendant rule and legitimately sets a
    // hover background, so a looser pattern would flag it.
    const runningRules = [
      ...list.matchAll(/\.(?:session-row|cross-session-row)\.running\s*\{[^}]*\}/g),
    ].map((m) => m[0])
    expect(runningRules.length, 'running-row rules should exist').toBeGreaterThan(0)
    for (const rule of runningRules) {
      expect(rule, `a running row must not paint a background: ${rule}`).not.toMatch(
        /background(-color)?\s*:/,
      )
    }
    // The modifier must not smuggle a fill in through a descendant either.
    expect(list, 'no running-row descendant should carry the old fill').not.toMatch(
      /\.(?:session-row|cross-session-row)\.running\s+[^{]*\{[^}]*var\(--running-fill\)/,
    )
  })

  it('draws every signal surface from a token, with no hardcoded colour', () => {
    // Scoped to the rules that paint the signal — not a whole-file scan, which
    // would trip over unrelated uses (the chat input bar's context-usage gauge
    // is legitimately green).
    //
    // The two surfaces use different motifs on purpose: the session rows get a
    // bottom-edge comet (--running-track / --running-comet / --running-head),
    // the input bar's session button gets a travelling sweep (--running-sweep),
    // because the button is a small chip rather than a full-width row.
    const cases: { file: string; selector: string; token: string }[] = [
      { file: 'src/components/session/SessionList.vue', selector: '.session-running-line::before', token: '--running-track' },
      { file: 'src/components/session/SessionList.vue', selector: '.session-running-band', token: '--running-comet' },
      { file: 'src/components/session/SessionList.vue', selector: '.session-running-line.is-blocked::before', token: '--pending-track' },
      { file: 'src/components/chat/ChatInputBar.vue', selector: '.chat-action-btn.has-running::before', token: '--running-sweep' },
    ]

    for (const { file, selector, token } of cases) {
      const rule = baseRule(readWebFile(file), selector)
      expect(rule, `${file}: ${selector} should exist`).toBeTruthy()
      expect(rule, `${file}: ${selector} should use var(${token})`).toContain(`var(${token})`)
      expect(rule, `${file}: ${selector} still hardcodes the old green`)
        .not.toMatch(/rgba?\(\s*34\s*,\s*197\s*,\s*94|#22c55e/i)
    }
  })

  it('keeps the comet head visible on every theme', () => {
    // The head is the part of the comet that must never be lost — it is the
    // opaque leading edge, and it is what makes the travel readable at a
    // glance. It is measured against the row's own background, which may be
    // either surface a row can sit on.
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const { color, alpha } = evalToken(vars['--running-head'], vars)
      for (const bgToken of ['--bg-primary', '--bg-secondary']) {
        const raw = vars[bgToken]
        if (!raw) continue
        const bg = parseHex(raw)
        const ratio = contrastRatio(toHex(over(color, bg, alpha)), toHex(bg))
        if (ratio < MIN_HEAD) {
          failures.push(`${theme.id} (${bgToken}): ${ratio.toFixed(2)}:1`)
        }
      }
    }
    expect(failures, `comet head below ${MIN_HEAD}:1:\n${failures.join('\n')}`).toEqual([])
  })

  it('keeps the comet readable against its own track', () => {
    // The comet travels OVER the static track, so what matters is that the
    // head stands out from the track rather than from the row. Without this the
    // track could be darkened until the comet no longer reads as a highlight
    // moving across it.
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const head = evalToken(vars['--running-head'], vars)
      const track = evalToken(vars['--running-track'], vars)
      const bg = parseHex(vars['--bg-primary'])
      const trackOverBg = over(track.color, bg, track.alpha)
      const headRatio = contrastRatio(toHex(over(head.color, bg, head.alpha)), toHex(trackOverBg))
      // Visible at all, and clearly the highlight rather than a faint shift.
      if (headRatio < 1.25) {
        failures.push(`${theme.id}: head only ${headRatio.toFixed(2)}:1 over its track`)
      }
    }
    expect(failures, `comet vs track:\n${failures.join('\n')}`).toEqual([])
  })

  it('is exactly ONE effect on the edge — no glow, no mask, no second layer', () => {
    // The design that shipped before this one stacked a 14px masked glow under
    // an 80% band. Each layer was defensible alone; together they read as two
    // effects fighting, and the glow smeared the band into ambient lighting.
    // These assertions are the fence that keeps it from coming back.
    expect(rootVars['--running-glow'], 'the old edge glow token must be gone').toBeUndefined()
    expect(rootVars['--running-line'], 'the old symmetric band token must be gone').toBeUndefined()
    expect(rootVars['--pending-glow'], 'the old pending glow token must be gone').toBeUndefined()

    const line = baseRule(list, '.session-running-line')
    expect(line, '.session-running-line should exist').toBeTruthy()
    // Flat and short: the edge is a 3px bar, not a 14px glow field.
    const height = Number(line!.match(/height:\s*(\d+)px/)?.[1])
    expect(height, 'the track should be a thin bar').toBeGreaterThan(0)
    expect(height, `track is ${height}px — too tall, that is a glow field again`).toBeLessThanOrEqual(4)

    // The track itself must be a flat fill. A mask on the edge is how the old
    // design bled light up the row.
    const track = baseRule(list, '.session-running-line::before')
    expect(track, 'the track rule should exist').toBeTruthy()
    expect(track, 'the edge must not fade upward — that is the old glow').not.toMatch(
      /mask-image/,
    )

    // The comet must not carry a vertical mask either.
    const comet = baseRule(list, '.session-running-band')
    expect(comet, '.session-running-band should exist').toBeTruthy()
    expect(comet, 'the comet must not be vertically masked into a glow').not.toMatch(
      /mask-image/,
    )

    // Exactly one pseudo-element on the edge (the track). The comet is a real
    // element because WAAPI cannot target a pseudo-element.
    const pseudo = [...list.matchAll(/\.session-running-line::(?:before|after)/g)]
    expect(pseudo.length, 'the edge should have exactly one pseudo-element (the track)').toBe(1)
  })

  it('keeps the theme colour instead of washing it out to grey', () => {
    // Design 2's other failure mode: forcing contrast by blending the accent
    // toward black/white muted every theme (github-light #4a90d9 → #ccd7e1,
    // saturation 0.66 → 0.09). No token may blend the accent away.
    for (const token of ['--running-track', '--running-comet', '--running-head', '--running-sweep']) {
      for (const theme of THEMES) {
        const vars = varsFor(theme.id)
        const accent = parseHex(vars['--accent-color'])
        const { color } = evalToken(vars[token], vars)
        expect(toHex(color), `${theme.id}: ${token} should be the accent's own colour`).toBe(
          toHex(accent),
        )
      }
    }
    // The track and comet body are translucent — the signal is soft, not a hard
    // stripe — while the head is opaque so it stays the highlight. Resolve
    // through a full theme's vars: these reference `--accent-color`, which is
    // declared per-theme rather than in `:root`, so `rootVars` alone cannot
    // resolve the chain.
    const vars = varsFor(THEMES[0].id)
    const trackAlpha = evalToken(vars['--running-track'], vars).alpha
    const cometAlpha = evalToken(vars['--running-comet'], vars).alpha
    const headAlpha = evalToken(vars['--running-head'], vars).alpha
    const sweepAlpha = evalToken(vars['--running-sweep'], vars).alpha
    expect(trackAlpha).toBeGreaterThan(0)
    expect(trackAlpha, 'the track should be a faint bar, not solid').toBeLessThan(1)
    expect(cometAlpha).toBeGreaterThan(0)
    expect(cometAlpha, 'the comet body should be translucent').toBeLessThan(1)
    expect(headAlpha, 'the head must be opaque — it is the highlight').toBe(1)
    expect(sweepAlpha).toBeGreaterThan(0)
    expect(sweepAlpha, 'sweep should be translucent').toBeLessThan(1)
    // And the head must be the brightest of the three.
    expect(headAlpha).toBeGreaterThan(cometAlpha)
    expect(cometAlpha).toBeGreaterThan(trackAlpha)
  })

  it('keeps the session button sweep visible on every theme', () => {
    // The button's sweep sits over its own background (--bg-tertiary). It is a
    // small chip, so the bar is lower than the row comet's — but it still has
    // to be visible, which the original fixed green was not (1.08:1 there).
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      const { color, alpha } = evalToken(vars['--running-sweep'], vars)
      const bg = parseHex(vars['--bg-tertiary'] ?? vars['--bg-primary'])
      const ratio = contrastRatio(toHex(over(color, bg, alpha)), toHex(bg))
      if (ratio < 1.30) failures.push(`${theme.id}: ${ratio.toFixed(2)}:1`)
    }
    expect(failures, `button sweep below 1.30:1:\n${failures.join('\n')}`).toEqual([])
  })

  it('keeps the dark comet no brighter than the light one relative to its row', () => {
    // A dark theme's accent sits ~+0.50 OKLab L above its background (light
    // themes ~−0.40), so an opaque head steps the dark row much further. That
    // is fine for a 3px edge (it is the point — it must be visible), but it
    // should not be wildly out of scale. Guard the ratio.
    const isDark = (id: string) => oklabLightness(parseHex(varsFor(id)['--bg-primary'])) < 0.5
    const step = (id: string): number => {
      const vars = varsFor(id)
      const { color, alpha } = evalToken(vars['--running-head'], vars)
      const bg = parseHex(vars['--bg-primary'])
      return oklabLightness(over(color, bg, alpha)) - oklabLightness(bg)
    }

    const dark = THEMES.filter((t) => isDark(t.id)).map((t) => Math.abs(step(t.id)))
    const light = THEMES.filter((t) => !isDark(t.id)).map((t) => Math.abs(step(t.id)))
    expect(dark.length).toBeGreaterThan(0)
    expect(light.length).toBeGreaterThan(0)

    const avg = (xs: number[]) => xs.reduce((a, b) => a + b, 0) / xs.length
    const ratio = avg(dark) / avg(light)
    // Inherent to opaque accents on the two bases. Anything past 3x would mean
    // a token change made dark themes glare.
    expect(ratio, `dark/light comet step ratio is ${ratio.toFixed(2)}`).toBeLessThan(3)
  })

  it('uses a 38% comet whose travel is derived from that width', () => {
    // The width and the two keyframes are one design: the directive's travel is
    // expressed as a % of the comet's own width, so changing the width without
    // recomputing the keyframes would leave the comet starting or ending inside
    // the row (a visible jump at the wrap).
    const comet = baseRule(list, '.session-running-band')
    expect(comet, '.session-running-band should exist').toBeTruthy()
    const width = Number(comet!.match(/width:\s*([\d.]+)%/)?.[1])
    expect(width, 'the comet should be 38% of the track').toBe(38)

    const directive = readWebFile('src/directives/runningSweep.ts')
    const from = Number(directive.match(/SWEEP_FROM = 'translateX\((-?[\d.]+)%\)'/)?.[1])
    const to = Number(directive.match(/SWEEP_TO = 'translateX\((-?[\d.]+)%\)'/)?.[1])
    expect(Number.isFinite(from), 'SWEEP_FROM should be a translateX %').toBe(true)
    expect(Number.isFinite(to), 'SWEEP_TO should be a translateX %').toBe(true)

    // Recomputed from the design's track-space endpoints (-40% to +102%).
    expect(from).toBeCloseTo((-40 / width) * 100, 1)
    expect(to).toBeCloseTo((102 / width) * 100, 1)
    // Both extremes must be fully clear of the track, or the wrap shows a jump.
    expect(from, 'the comet must start fully off the left edge').toBeLessThanOrEqual(-100)
    expect(to, 'the comet must finish fully off the right edge').toBeGreaterThanOrEqual(200)
  })
})
