import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Two mechanical rules the floating meeting controls kept breaking.
 *
 * 1. Touch targets. The dock buttons are the ONLY way to mute, chat or go
 *    fullscreen while a phone viewer is watching a share, and the join gate's
 *    field/button is the only way past the gate. They were 24–30dp tall (and
 *    the icon-only variant at ≤520px was about 24dp wide) — so a thumb pressed
 *    the neighbouring control instead.
 *
 *    The dock's floor is 28dp, not 44: it floats over the picture and every dp
 *    of height comes out of the video, so the bar is deliberately compact
 *    (28dp button + 2dp pill padding = 32dp). Do not "restore" 44dp without
 *    also accepting a bar that is half again as tall as the picture's own
 *    controls. The join gate keeps 44dp — it is a form, not an overlay.
 *
 * 2. Interface scaling. `--ui-scale` only scales the root font size, so every
 *    geometric length has to be written as `calc(N * var(--dp))`. A bare `6px`
 *    status dot stays 6px while the text around it grows, which at 1.25–1.5
 *    makes the dot read as a smudge. Hairlines (1px) are the one exception.
 *
 * Asserted on the stylesheet rather than a computed box: jsdom does not apply
 * scoped component CSS, so a rendered measurement would pass no matter what the
 * rule says.
 */
function styleBlock(file: string): string {
  const src = readWebFile(file)
  const m = src.match(/<style scoped>([\s\S]*)<\/style>/)
  expect(m, `${file} has no scoped style block`).not.toBeNull()
  return (m as RegExpMatchArray)[1].replace(/\/\*[\s\S]*?\*\//g, '')
}

/** Every `selector { ... }` block; nested at-rules are flattened. */
function rules(css: string): Array<{ selector: string; body: string }> {
  const out: Array<{ selector: string; body: string }> = []
  const re = /([^{}]+)\{([^{}]*)\}/g
  let m: RegExpExecArray | null
  while ((m = re.exec(css))) {
    out.push({ selector: m[1].trim().replace(/\s+/g, ' '), body: m[2] })
  }
  return out
}

function bodiesOf(file: string, selector: string): string[] {
  return rules(styleBlock(file))
    .filter((r) => r.selector === selector)
    .map((r) => r.body)
}

/**
 * The raw text of one `@media (...) { ... }` block.
 *
 * `rules()` flattens at-rules, so a rule's membership in a breakpoint is lost
 * and could only be guessed from one of its declarations — which breaks the
 * moment somebody tunes a padding. Extracting the block keeps the query itself
 * as the anchor.
 */
function mediaBlock(css: string, query: string): string {
  const at = css.indexOf(query)
  if (at < 0) return ''
  const open = css.indexOf('{', at)
  if (open < 0) return ''
  let depth = 0
  for (let i = open; i < css.length; i++) {
    if (css[i] === '{') depth++
    else if (css[i] === '}') {
      depth--
      if (depth === 0) return css.slice(open + 1, i)
    }
  }
  return ''
}

const DOCK_BUTTON = 'src/share/LiveDockButton.vue'
const JOIN_GATE = 'src/share/LiveJoinGate.vue'

describe('touch targets', () => {
  it('gives every dock button a 28dp minimum in both axes', () => {
    const css = styleBlock(DOCK_BUTTON)
    const bodies = bodiesOf(DOCK_BUTTON, '.live-dock-btn')
    expect(bodies.length).toBeGreaterThan(0)

    // Height: one rule is enough, it applies everywhere.
    expect(bodies.some((b) => /min-height:\s*calc\(28 \* var\(--dp\)\)/.test(b))).toBe(true)

    // Width: only needed once the label is dropped, so it must live in THAT
    // state — a min-width on the labelled variant is dead weight and would
    // prove nothing about the icon-only case.
    //
    // The state is a class driven by the design-width composable, not a media
    // query: the threshold has to follow the interface scale (a 640px window at
    // 1.33× has only ~480 design px of room). See useDesignWidth.test.ts for
    // the measurement itself.
    const compactRules = rules(css).filter((r) => r.selector.includes('--compact'))
    expect(compactRules.length, 'no --compact rules').toBeGreaterThan(0)

    // It really is the state that drops the label...
    expect(
      compactRules.some(
        (r) => /\.live-dock-label$/.test(r.selector) && /display:\s*none/.test(r.body),
      ),
      'the compact state must hide the label',
    ).toBe(true)

    // ...and the button itself becomes square there.
    const compactButton = compactRules.filter((r) => /\.live-dock-btn--compact$/.test(r.selector))
    expect(compactButton, 'no .live-dock-btn--compact rule').toHaveLength(1)
    expect(compactButton[0].body).toMatch(/min-width:\s*calc\(28 \* var\(--dp\)\)/)

    // A leftover media query would mean two competing sources of truth.
    expect(css).not.toMatch(/@media[^{]*max-width:\s*520px/)
  })

  it('raises the join gate field and button to 44dp', () => {
    for (const selector of ['.join-gate__input', '.join-gate__btn']) {
      const bodies = bodiesOf(JOIN_GATE, selector)
      expect(bodies.length, `${selector} is missing`).toBeGreaterThan(0)
      const joined = bodies.join('\n')
      expect(joined).toMatch(/min-height:\s*calc\(44 \* var\(--dp\)\)/)
      // The old fixed heights (32dp field, 30dp button) must be gone: they
      // would win over nothing and keep the target small.
      expect(joined).not.toMatch(/(^|;|\s)height:\s*calc\((30|32) \* var\(--dp\)\)/)
    }
  })

  /**
   * The 28dp floor still makes the dock wider than a small phone.
   *
   * A viewer's dock can hold seven controls (sound, mic, control, share, chat,
   * notes, fullscreen) at 28dp each, and interface scaling multiplies the whole
   * pill. The dock is absolutely positioned and centred, and it is a SIBLING of
   * the stage — so the stage's `overflow: hidden` never clips it. Without a
   * ceiling and wrapping it spilled symmetrically past both edges and the
   * outermost buttons (brand, fullscreen) could not be tapped at all. Wrapping
   * is the fix; shrinking the buttons further is not.
   */
  it('lets the dock wrap instead of overflowing the viewport', () => {
    const bodies = bodiesOf('src/share/LiveDock.vue', '.live-dock')
    expect(bodies, 'the .live-dock rule is missing').toHaveLength(1)
    const body = bodies[0]

    expect(body).toMatch(/max-width:\s*calc\(100% - 2 \* var\(--space-3, 6px\)\)/)
    expect(body).toMatch(/flex-wrap:\s*wrap/)
    // Without `max-content` the ceiling above is unreachable: an absolutely
    // positioned box with `left: 50%` sizes into the space remaining to its
    // right, i.e. half the column — measured 160dp wide on a 320dp phone, which
    // wrapped the pill into THREE rows instead of two.
    expect(body).toMatch(/width:\s*max-content/)
    // A wrapped pill lays each row out from the start without this, so the
    // shorter row would sit against the left edge of the longest one.
    expect(body).toMatch(/justify-content:\s*center/)
  })

  /**
   * The control pill ("等待主持人批准…" / "结束控制") floats just above the dock.
   * Once the dock may take two rows, a fixed offset would put the pill in the
   * middle of it — covering the very buttons the viewer is reaching for, and
   * the pill has the higher z-index.
   *
   * The height is MEASURED now, not guessed from a breakpoint. It used to be
   * `34dp`, or `66dp` under `(max-width: 520px)`, describing "one row" / "two
   * rows" — and that guess broke as soon as the interface scale was applied to
   * the share page: the buttons are 28dp, so at 1.33× the seven of them stop
   * fitting on a window the breakpoint still calls wide. The dock wraps, the
   * breakpoint says one row, and the pill lands on the second row. No constant
   * can describe where a wrap happens, because it depends on the scale too.
   *
   * So the guard is now about the MEASUREMENT: the pill must read the variable,
   * and the variable must come from the dock's own box.
   */
  it('positions the control pill from the dock’s measured height', () => {
    const css = styleBlock('src/share/LiveShareView.vue')

    const banner = rules(css).filter((r) => /\.live-control-banner/.test(r.selector))
    expect(banner.length).toBeGreaterThan(0)
    expect(banner.some((r) => /bottom:[^;]*var\(--live-dock-height\)/.test(r.body))).toBe(true)

    // The constant-based guess must be GONE: a breakpoint that reserves a
    // two-row height would now fight the measurement (an inline style wins, so
    // it would be dead weight that only misleads the next reader).
    expect(css, 'the fixed two-row breakpoint must be gone').not.toMatch(
      /@media \(max-width: 520px\)[^}]*\{[^}]*--live-dock-height/,
    )

    // The value is written from the dock element's own height, so a wrap at any
    // scale is accounted for.
    const src = readWebFile('src/share/LiveShareView.vue')
    expect(src).toMatch(/dockHeight\.value\s*=\s*dock\.getBoundingClientRect\(\)\.height/)
    // ...and it is applied to the containing block the pill is positioned in.
    expect(src).toMatch(/'--live-dock-height':\s*`\$\{dockHeight\}px`/)
    // Observed, not sampled once: a wrap can happen without a window resize
    // (scale change, font loading, a label appearing inside a button).
    expect(src).toContain('new ResizeObserver(sync)')
    expect(src).toContain('dockResizeObserver?.disconnect()')
  })
})

describe('interface-scale units', () => {
  // The rules the review flagged: a 6px status dot, a 20px logo, a 7px live
  // dot and the unread badge's 16px box.
  const cases: Array<[string, string]> = [
    ['src/share/LiveShareView.vue', '.live-quality__dot'],
    ['src/share/LiveShareView.vue', '.live-speaker__dot'],
    ['src/share/LiveDock.vue', '.live-dock-logo'],
    ['src/share/LiveDock.vue', '.live-dock-dot'],
    [DOCK_BUTTON, '.live-dock-badge'],
  ]

  it.each(cases)('%s %s has no unscaled length', (file, selector) => {
    const bodies = bodiesOf(file, selector)
    expect(bodies.length, `${selector} not found in ${file}`).toBeGreaterThan(0)

    for (const body of bodies) {
      // Token fallbacks (`var(--radius-full, 999px)`) are defaults for a
      // missing token, not a hard-coded size: only literal lengths count.
      const bare: string[] = []
      for (const m of body.replace(/var\([^)]*\)/g, '').matchAll(/(\d*\.?\d+)px\b/g)) {
        // 1px is a hairline (and the live dot's 1px offset), deliberately not
        // scaled; everything else must go through --dp.
        if (m[1] !== '1') bare.push(m[0])
      }
      expect(bare, `${selector} in ${file} still uses ${bare.join(', ')}`).toEqual([])
    }
  })
})
