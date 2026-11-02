import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Guards for two layout/appearance defects in the viewer dock.
 *
 * 1. The control-state pill ("等待主持人批准…" / "结束控制") is the SAME element as
 *    the "who is presenting" pill (`class="live-screen-banner live-control-banner"`).
 *    The base rule sets `top`, and the control rule overrides it with
 *    `top: auto` plus a `bottom`. Both are single-class selectors, so the winner
 *    is decided by source order — and the base rule is declared LATER in the
 *    file. `top: auto` therefore lost: the pill stretched from `top` to
 *    `bottom`, its flex content was centred vertically, and it appeared as a
 *    full-height strip in the middle of the picture that also swallowed pointer
 *    events there. The override must win on specificity, not on order.
 *
 * 2. Buttons that this platform cannot use are `:disabled` in the template
 *    (microphone over plain HTTP, browser screen capture, remote control
 *    outside the client). Without a `:disabled` rule they render exactly like
 *    usable ones, so the only feedback a press gives is nothing at all.
 */
describe('LiveShareView dock', () => {
  const FILE = 'src/share/LiveShareView.vue'

  /** The component's own stylesheet, comments stripped (they mention the very
   *  selectors asserted below, and would make a rule-less file pass). */
  function styleBlock(): string {
    const src = readWebFile(FILE)
    const m = src.match(/<style scoped>([\s\S]*)<\/style>/)
    expect(m).not.toBeNull()
    return (m as RegExpMatchArray)[1].replace(/\/\*[\s\S]*?\*\//g, '')
  }

  /** Every `selector { ... }` block, keeping the selector text. */
  function rules(css: string): Array<{ selector: string; body: string }> {
    const out: Array<{ selector: string; body: string }> = []
    const re = /([^{}]+)\{([^{}]*)\}/g
    let m: RegExpExecArray | null
    while ((m = re.exec(css))) {
      out.push({ selector: m[1].trim().replace(/\s+/g, ' '), body: m[2] })
    }
    return out
  }

  it('positions the control pill above the dock regardless of source order', () => {
    const css = styleBlock()
    const all = rules(css)

    // The conflict is real: the base pill pins itself to the top.
    const base = all.filter((r) => r.selector === '.live-screen-banner')
    expect(base).toHaveLength(1)
    expect(base[0].body).toMatch(/(^|;)\s*top:/)

    const overrides = all.filter((r) => r.selector.includes('.live-control-banner'))
    expect(overrides.length).toBeGreaterThan(0)

    const pinned = overrides.filter((r) => /(^|;)\s*top:\s*auto/.test(r.body))
    expect(pinned.length).toBeGreaterThan(0)
    for (const rule of pinned) {
      // Two classes beat the base rule's one, whatever the order.
      const classes = rule.selector.match(/\.[\w-]+/g) ?? []
      expect(classes.length).toBeGreaterThanOrEqual(2)
      expect(rule.body).toMatch(/(^|;)\s*bottom:/)
    }
  })

  it('dims dock buttons this platform cannot use', () => {
    const all = rules(styleBlock())
    const disabled = all.filter((r) => /\.live-dock-btn:disabled\b/.test(r.selector))
    expect(disabled.length).toBeGreaterThan(0)
    expect(disabled.some((r) => /opacity:\s*var\(--opacity-disabled/.test(r.body))).toBe(true)
  })
})
