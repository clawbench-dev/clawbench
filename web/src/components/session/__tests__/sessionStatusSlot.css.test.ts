import { describe, it, expect } from 'vitest'
import { THEMES } from '@/utils/themeMeta'
import { contrastRatio } from '@/utils/tagColor'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for the session-row status slot and the bottom comet.
 *
 * The slot replaced `.session-item-badge`, which painted ONE blue dot for both
 * `unreadCount > 0` and `pendingApproval` — so "there is a reply" and "this is
 * blocked on your approval" were the same pixels. The fix has two halves, and
 * each is guarded here because each can silently rot:
 *
 *   1. Three visually distinct states, separated by MOTION (rotate / pulse /
 *      still) with hue only as a secondary channel. Motion is the primary
 *      channel because neither hue is safe on its own: a theme's accent and
 *      its orange are both theme properties and are free to sit close
 *      together, and a colour-vision-deficient reader gets nothing from hue.
 *   2. The comet stops while blocked. A travelling band means "progressing";
 *      showing it on a row that is waiting for the user claims progress that
 *      is not happening.
 *
 * The running state is a RING (a rotating arc over a faint track) while the
 * other two are filled dots — so shape carries one layer of separation and
 * motion carries the rest. The ring must stay a ring: a filled disc would make
 * it the unread dot at a larger size, and a `border` on the dot states would
 * make "waiting on you" look like a spinner.
 *
 * The component-level priority rule (pending > running > unread) is asserted
 * in SessionList.test.ts, where the real template renders. This file owns the
 * stylesheet-level invariants: token derivation, per-theme visibility, and the
 * deliberate reduced-motion EXCEPTION (this slot does not opt out — see the
 * test for why).
 *
 * jsdom has no CSS engine and does not resolve `color-mix()`/`var()`, so the
 * tokens are parsed out of variables.css and the colour maths done by hand —
 * same approach as runningSweepTheme.css.test.ts.
 */

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

const themeVars: Record<string, Record<string, string>> = {}
for (const theme of THEMES) {
  const body = blockBody(`[data-theme="${theme.id}"]`)
  expect(body, `variables.css should define theme "${theme.id}"`).not.toBeNull()
  themeVars[theme.id] = declarations(body!)
}

function varsFor(themeId: string): Record<string, string> {
  return { ...rootVars, ...themeVars[themeId] }
}

/**
 * Resolve a token expression to a colour plus its alpha.
 *
 * Handles the shapes these tokens use: a bare reference to another token, a
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

/**
 * The base rule for a selector, anchored to the start of a line.
 *
 * The anchor matters: a loose pattern also matches a descendant override such
 * as `.session-running-line.is-blocked .session-running-band { … }`, so the
 * assertion would silently target the override instead of the base rule.
 */
function baseRule(source: string, selector: string): string | undefined {
  return source.match(
    new RegExp('\\n' + selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '\\s*\\{[^}]*\\}'),
  )?.[0]
}

describe('session status slot', () => {
  it('derives every status token from the theme, never a literal colour', () => {
    // A hardcoded amber would look right on the theme it was picked on and
    // wrong on the other 35 — the exact failure the running-band tokens were
    // introduced to fix.
    const derived: Record<string, RegExp> = {
      // The running ring: the arc and its track both derive from the accent.
      '--running-ring': /var\(--accent-color\)/,
      '--running-ring-track': /var\(--accent-color\)/,
      '--status-dot': /var\(--accent-color\)/,
      '--status-dot-pending': /var\(--color-orange\)/,
      // The bottom-edge comet shares the row's "running" message, so it is
      // derived from the same accent. Its blocked state uses the same orange as
      // the pending dot, so the edge and the slot always agree.
      '--running-track': /var\(--accent-color\)/,
      '--running-comet': /var\(--accent-color\)/,
      '--running-head': /var\(--accent-color\)/,
      '--pending-track': /var\(--color-orange\)/,
    }
    for (const [token, pattern] of Object.entries(derived)) {
      expect(rootVars[token], `${token} should be declared`).toBeDefined()
      expect(rootVars[token], `${token} should derive from the theme`).toMatch(pattern)
      expect(rootVars[token], `${token} must not hardcode a colour`).not.toMatch(/#[0-9a-f]{3,6}/i)
    }
  })

  it('declares the ring tokens the running state consumes', () => {
    // The running state is a ring, and it is drawn entirely from these two
    // tokens. `--running-ring-track` is the low-alpha arc colour, not
    // `--border-color`: on a running row the track must not read as a second,
    // static ring competing with the rotating arc.
    expect(rootVars['--running-ring'], '--running-ring must exist').toBeDefined()
    expect(rootVars['--running-ring-track'], '--running-ring-track must exist').toBeDefined()
    expect(rootVars['--running-ring-track'], 'the track must be translucent').toMatch(/transparent/)

    // Only the RING tokens are required. The pending dot is a fill, not a ring,
    // so it must not have picked up a ring token of its own — that is how the
    // old pending ring would creep back in.
    expect(rootVars['--pending-ring'], '--pending-ring should not exist').toBeUndefined()
    expect(rootVars['--pending-ring-track'], '--pending-ring-track should not exist').toBeUndefined()
  })

  it('separates the live states by animation, not by hue', () => {
    // Measured: 3 of the 36 themes give `--accent-color` and `--color-orange`
    // the SAME value (ayu-light #ff9940, gruvbox-light #af3a03, gruvbox-dark
    // #fe8019). Both are theme properties, so this is not a bug to fix — it is
    // the reason the design may not lean on colour. The live states must
    // therefore differ in their ANIMATION, which is asserted here on the real
    // rules rather than on the tokens.
    const running = list.match(/\n\.session-status\.is-running\s*\{[^}]*\}/)?.[0]
    const pending = list.match(/\n\.session-status\.is-pending\s*\{[^}]*\}/)?.[0]
    expect(running, 'the running rule should exist').toBeTruthy()
    expect(pending, 'the pending rule should exist').toBeTruthy()

    const anim = (rule: string) => rule.match(/animation:\s*([\w-]+)/)?.[1]
    const a = anim(running!)
    const b = anim(pending!)
    expect(a, 'running should animate').toBeTruthy()
    expect(b, 'pending should animate').toBeTruthy()
    expect(
      a,
      'the two states must not share an animation — colour alone cannot carry it',
    ).not.toBe(b)
    // And they are not the same motion at different speeds: one rotates, one
    // scales in place.
    expect(list, '@keyframes session-status-spin should exist').toMatch(
      /@keyframes session-status-spin/,
    )
    expect(list, '@keyframes session-status-pulse should exist').toMatch(
      /@keyframes session-status-pulse/,
    )
  })

  it('keeps both dot colours visible against the row on every theme', () => {
    // The dot is the row's primary signal, so near-invisibility is a
    // functional failure rather than a cosmetic one. Measured against both
    // surfaces a row can sit on.
    const MIN = 1.9
    const failures: string[] = []
    for (const theme of THEMES) {
      const vars = varsFor(theme.id)
      for (const token of ['--status-dot', '--status-dot-pending']) {
        const { color, alpha } = evalToken(vars[token], vars)
        for (const bgToken of ['--bg-primary', '--bg-secondary']) {
          const raw = vars[bgToken]
          if (!raw) continue
          const bg = parseHex(raw)
          const ratio = contrastRatio(toHex(over(color, bg, alpha)), toHex(bg))
          if (ratio < MIN) failures.push(`${theme.id} ${token} (${bgToken}): ${ratio.toFixed(2)}:1`)
        }
      }
    }
    expect(failures, `dot below ${MIN}:1:\n${failures.join('\n')}`).toEqual([])
  })

  it('gives the comet a head brighter than its body, so travel reads as directed', () => {
    // The leading edge is opaque and the body is translucent. A symmetric
    // gradient reads as a blob drifting with no direction.
    const vars = varsFor(THEMES[0].id)
    const head = evalToken(vars['--running-head'], vars)
    const comet = evalToken(vars['--running-comet'], vars)
    const track = evalToken(vars['--running-track'], vars)

    expect(head.alpha, 'the head should be opaque').toBe(1)
    expect(comet.alpha, 'the body stays translucent').toBeLessThan(1)
    expect(track.alpha, 'the track is the faintest of the three').toBeLessThan(comet.alpha)
  })

  it('paints the comet as a one-directional ramp, not a symmetric blob', () => {
    // Line-anchored: a loose pattern also matches the descendant rule
    // `.session-running-line.is-blocked .session-running-band { … }` and would
    // assert against the override instead of the base rule.
    const band = list.match(/\n\.session-running-band\s*\{[^}]*\}/)?.[0]
    expect(band, '.session-running-band should exist').toBeTruthy()
    // Head at the trailing (right) edge — the direction of travel.
    expect(band).toMatch(/var\(--running-head\)/)
    expect(band, 'the body must be behind the head').toMatch(/var\(--running-comet\)/)
    // A symmetric ramp would fade out again on the right.
    expect(band, 'a symmetric gradient would read as a drifting blob').not.toMatch(
      /transparent\s+100%/,
    )
  })

  it('stops the comet while the row is blocked on an approval', () => {
    // The comet means "progressing". A blocked row is still `running`, so
    // without this rule it would keep sweeping and claim progress it is not
    // making — the one case where the row must not look busy.
    //
    // The blocked bar is a SEPARATE element without the directive, because the
    // directive writes `transform` through the Web Animations API and that
    // outranks a plain CSS `transform` — a single element would keep travelling
    // no matter what the stylesheet said.
    const template = list.slice(0, list.indexOf('<style'))
    expect(
      template,
      'the blocked row must render the bar WITHOUT v-running-sweep',
    ).toMatch(/v-if="row\.status === 'pending'"\s+class="session-running-band"/)
    expect(
      template,
      'the travelling comet must be the else branch',
    ).toMatch(/v-else\s+v-running-sweep\s+class="session-running-band"/)

    const blocked = baseRule(list, '.session-running-line.is-blocked .session-running-band')
    expect(blocked, 'a blocked comet should have its own rule').toBeTruthy()
    // It becomes the full-width bar, amber, with no travel and no halo.
    expect(blocked).toMatch(/width:\s*100%/)
    expect(blocked).toContain('var(--pending-comet)')
    expect(blocked, 'the halo belongs to the travelling comet only').toMatch(/filter:\s*none/)
    expect(blocked, 'the travel must be neutralised in CSS too').toMatch(/transform:\s*none/)
    expect(blocked, 'it breathes instead of travelling').toMatch(
      /animation:\s*session-comet-hold/,
    )

    // And the track turns to the amber under-bar, so the edge agrees with the
    // pending ring on the other side of the row.
    const edge = baseRule(list, '.session-running-line.is-blocked::before')
    expect(edge, 'a blocked track should be recoloured').toBeTruthy()
    expect(edge).toContain('var(--pending-track)')

    // The breathing must be opacity-only: a scaling bar would read as motion,
    // which is exactly what "blocked" must not look like.
    const hold = list.match(/@keyframes session-comet-hold\s*\{[^}]*\}[^}]*\}/)?.[0]
    expect(hold, '@keyframes session-comet-hold should exist').toBeTruthy()
    expect(hold).toMatch(/opacity/)
    expect(hold, 'a scaling bar would read as movement').not.toMatch(/scale|translate/)
  })

  it('uses motion, not just colour, to separate the states', () => {
    // Motion is the load-bearing channel: it survives every theme and every
    // reader. Assert each state's animation is the right SHAPE — a rotating
    // ring for progress, an in-place pulse for "waiting on you", and nothing
    // at all for unread.
    const running = list.match(/\n\.session-status\.is-running\s*\{[^}]*\}/)?.[0]
    expect(running, '.session-status.is-running should exist').toBeTruthy()
    expect(running, 'running must rotate').toMatch(/animation:\s*session-status-spin/)
    expect(running).toContain('var(--running-ring)')

    const pending = list.match(/\n\.session-status\.is-pending\s*\{[^}]*\}/)?.[0]
    expect(pending, '.session-status.is-pending should exist').toBeTruthy()
    expect(pending, 'pending must pulse in place').toMatch(/animation:\s*session-status-pulse/)
    expect(pending).toContain('var(--status-dot-pending)')
    // The whole distinction: a blocked row must NOT look like a busy one.
    expect(pending, 'pending must not rotate — that would read as busy').not.toMatch(
      /session-status-spin/,
    )

    const unread = list.match(/\n\.session-status\.is-unread\s*\{[^}]*\}/)?.[0]
    expect(unread, '.session-status.is-unread should exist').toBeTruthy()
    expect(unread, 'unread is the quiet one — no animation').not.toMatch(/animation\s*:/)
    expect(unread).toContain('var(--status-dot)')

    // The ring must be a ring: an arc over a faint track, not a filled disc.
    // A fill would make running look like the unread dot at a larger size.
    expect(running, 'the running state must be a ring (border)').toMatch(/border:\s*2px solid/)
    expect(running, 'the arc must be distinguished from its track').toMatch(
      /border-top-color:\s*var\(--running-ring\)/,
    )

    // The pulse must scale in place; a translate would read as movement.
    const pulse = list.match(/@keyframes session-status-pulse\s*\{[^}]*\}[^}]*\}/)?.[0]
    expect(pulse, '@keyframes session-status-pulse should exist').toBeTruthy()
    expect(pulse).toMatch(/scale\(/)
    expect(pulse, 'a translating pulse would read as travel').not.toMatch(/translate/)
  })

  it('renders the running state as a single ring element, not child dots', () => {
    // The running state is the slot itself (a `border` ring), so the template
    // must NOT emit child elements. A leftover `v-for` dot container here would
    // render three unstyled children inside a 14px ring — invisible in most
    // themes but a real regression, and exactly the kind of leftover that
    // survives a refactor unnoticed.
    const template = list.slice(0, list.indexOf('<style'))
    expect(
      template,
      'the running state must not render child dots — it is a ring on the slot',
    ).not.toMatch(/session-status-dot/)
    expect(template, 'no dot v-for should remain').not.toMatch(
      /v-for="n in \((row|crossStatus)\.?[^)]*running[^)]*\)"/,
    )
  })

  it('deliberately does NOT opt out under prefers-reduced-motion', () => {
    // This inverts what this file used to assert, so read the reason before
    // "fixing" it back.
    //
    // The project rule is that animations should respect the preference, and
    // this slot is an intentional exception. Motion is a load-bearing channel
    // here, and freezing collapses states:
    //
    //   - "pending" (pulsing dot) and "unread" (still dot) are the same shape
    //     and size; only the pulse and the hue separate them. Frozen, they
    //     differ by hue alone — and a theme's accent and its orange may be
    //     identical (3 of the 36 themes), making them literally the same.
    //   - a stopped ring is also the universal "stalled / broken" glyph, so
    //     running would misread as stuck rather than quiet.
    //
    // It also has to match the rest of the app, which was the actual bug
    // report: every other loading indicator (RefreshButton, the bottom-edge
    // comet) is WAAPI-driven, and WAAPI never consults the preference. Opting
    // out only here made this the single indicator that froze — "why does only
    // this one stop?".
    //
    // If the states ever become distinguishable without motion, this exception
    // can be removed. The media query must not come first.
    const m = list.match(/@media \(prefers-reduced-motion: reduce\)\s*\{([\s\S]*?)\n\}/)
    expect(
      m,
      'the status slot must not opt out of reduced-motion — see the CSS comment',
    ).toBeNull()

    // And the animations it would have disabled must still be present, so this
    // cannot pass merely because the animations were deleted outright.
    const running = list.match(/\n\.session-status\.is-running\s*\{[^}]*\}/)?.[0]
    expect(running, 'the running spin must still be declared').toMatch(
      /animation:\s*session-status-spin/,
    )
    const pending = list.match(/\n\.session-status\.is-pending\s*\{[^}]*\}/)?.[0]
    expect(pending, 'the pending pulse must still be declared').toMatch(
      /animation:\s*session-status-pulse/,
    )
  })

  it('no longer paints one shared dot for two different states', () => {
    // The regression this whole change exists to prevent: a single element
    // keyed off `unreadCount > 0 || pendingApproval`.
    expect(list, 'the shared badge must be gone').not.toMatch(/class="session-item-badge"/)
    expect(list, 'its keyframes should be gone too').not.toMatch(/@keyframes badge-breathe/)
    expect(
      list,
      'no element may merge unread and pending into one condition',
    ).not.toMatch(/unreadCount\s*>\s*0\s*\|\|\s*[\w.]*pendingApproval/)
  })

  it('sizes the slot so it cannot collide with the pinned wedge', () => {
    // The old badge was absolutely positioned at the row's top-right, where
    // the pinned wedge lives. The slot is a flex child instead, so it must
    // stay a bounded size or it would push the title's ellipsis around.
    const rule = list.match(/\n\.session-status\s*\{[^}]*\}/)?.[0]
    expect(rule, '.session-status should exist').toBeTruthy()
    const w = Number(rule!.match(/width:\s*(\d+)px/)?.[1])
    expect(w, 'slot width should be a small px value').toBeGreaterThan(0)
    expect(w, `slot is ${w}px — too wide for a row's trailing signal`).toBeLessThanOrEqual(20)
    expect(rule, 'the slot is a flex child, not an absolute overlay').not.toMatch(
      /position:\s*absolute/,
    )
  })

  it('keeps the status slot off the title, with the gap paid for out of the trailing edge', () => {
    // The bug this fixes: `.session-item` had `padding-right: 12px` and the slot
    // had no `margin-left`, so the slot's left edge landed EXACTLY on the
    // title's ellipsis boundary — measured 0px on the live 1440px layout. The
    // dot was flush against the text while ~10px sat unused between the slot and
    // the ⋮ button. The dot read as crammed into the title for no reason.
    //
    // The fix moves the slot right by shrinking the item's right padding and
    // giving the slot its own margin, so both gaps end up ~11px.
    const slot = list.match(/\n\.session-status\s*\{[^}]*\}/)?.[0]
    expect(slot, '.session-status should exist').toBeTruthy()
    expect(
      slot,
      'the slot needs its own left margin, or it sits flush on the title',
    ).toMatch(/margin-left:\s*var\(--space-4\)/)

    // And the row's own right inset must be the matching 8px, not the 12px the
    // other sides use — otherwise the gap only moves from one side to the other.
    for (const sel of ['\\.session-item', '\\.cross-session-item']) {
      const rule = list.match(new RegExp(`\\n${sel}\\s*\\{[^}]*\\}`))?.[0]
      expect(rule, `${sel} should exist`).toBeTruthy()
      // Shorthand padding: top | right | bottom | left.
      expect(
        rule,
        `${sel} should inset its right edge by 8px so the slot can move right`,
      ).toMatch(/padding:\s*var\(--space-5\)\s+var\(--space-4\)\s+var\(--space-5\)\s+var\(--space-6\)/)
    }
  })

  it('paints the state dots as dots, not as full-slot discs', () => {
    // A solid disc at the slot's full size carries far more visual weight than
    // a small dot, so the two read as unrelated indicators. Both dot states are
    // therefore an 8px paint inside the 14px slot.
    //
    // The dot is painted by a radial-gradient INSIDE the 14px slot rather than
    // by shrinking the element: the slot must keep its footprint, or the title
    // would run 6px further right on unread rows than on running ones and the
    // list's right edge would look ragged.
    const unread = list.match(/\n\.session-status\.is-unread\s*\{[^}]*\}/)?.[0]
    expect(unread, '.session-status.is-unread should exist').toBeTruthy()

    // The dot's radius is the gradient's stop, not a width/height.
    const stop = Number(unread!.match(/(\d+(?:\.\d+)?)px,\s*transparent/)?.[1])
    expect(Number.isFinite(stop), 'the dot should be a radial-gradient with a px stop').toBe(true)
    const dotDiameter = stop * 2
    expect(dotDiameter, 'the dot should be visibly smaller than the slot').toBeLessThan(12)
    expect(dotDiameter, 'the dot should still be a readable dot').toBeGreaterThanOrEqual(6)

    // And it must NOT be a flat fill spanning the whole slot.
    expect(
      unread,
      'a flat background paints a full-slot disc — the oversized look being fixed',
    ).not.toMatch(/background:\s*var\(--status-dot\)/)

    // Nor may it shrink the ELEMENT to get a small dot: the slot must keep its
    // 14px footprint, or the title runs further right on unread rows than on
    // running ones and the list's right edge looks ragged. The dot's size comes
    // from the gradient stop, never from width/height on this rule.
    expect(
      unread,
      'the unread rule must not resize the slot — size the gradient stop instead',
    ).not.toMatch(/(?:^|[;{]\s*)(width|height)\s*:/)

    // The pending dot uses the same 8px paint, so the two dot states are the
    // same size and only their motion differs.
    const pending = list.match(/\n\.session-status\.is-pending\s*\{[^}]*\}/)?.[0]
    expect(pending, '.session-status.is-pending should exist').toBeTruthy()
    const pendingStop = Number(pending!.match(/(\d+(?:\.\d+)?)px,\s*transparent/)?.[1])
    expect(pendingStop, 'the pending dot should be the same size as the unread one').toBe(stop)

    // The dot states must be FILLS, not rings — the ring belongs to running
    // alone. A border here would make a "waiting" state look like a spinner.
    for (const cls of ['is-pending', 'is-unread']) {
      const rule = list.match(new RegExp(`\\n\\.session-status\\.${cls}\\s*\\{[^}]*\\}`))?.[0]
      expect(rule, `.session-status.${cls} should exist`).toBeTruthy()
      expect(rule, `${cls} must be a fill, not a ring`).not.toMatch(/border:/)
    }

    // And running must be the opposite: a ring, never a gradient fill (a fill
    // would make it the unread dot at a larger size).
    const running = list.match(/\n\.session-status\.is-running\s*\{[^}]*\}/)?.[0]
    expect(running, '.session-status.is-running should exist').toBeTruthy()
    expect(running, 'running must be a ring, not a fill').not.toMatch(/radial-gradient/)
  })
})
