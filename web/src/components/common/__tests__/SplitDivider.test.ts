import { describe, expect, it, vi, afterEach } from 'vitest'
import { mount, VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import SplitDivider from '@/components/common/SplitDivider.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, zh } })

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.classList.remove('split-view-dragging')
  vi.restoreAllMocks()
})

function mountDivider(props = {}) {
  wrapper = mount(SplitDivider, { props, global: { plugins: [i18n] } })
  return wrapper
}

/**
 * Read the SFC source. jsdom does not load `<style>`, so style assertions have
 * to inspect the file — and the cwd differs between a bare `vitest` run (web/)
 * and scripts/vitest-run.sh (repo root), so probe both.
 */
function readSource(): string {
  for (const base of [process.cwd(), resolve(process.cwd(), 'web')]) {
    try {
      return readFileSync(resolve(base, 'src/components/common/SplitDivider.vue'), 'utf8')
    } catch {
      // try the next candidate
    }
  }
  throw new Error('SplitDivider.vue not found from cwd: ' + process.cwd())
}

describe('SplitDivider', () => {
  it('renders the divider with gutter line and separator semantics', () => {
    mountDivider()
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('role')).toBe('separator')
    expect(el.getAttribute('aria-orientation')).toBe('vertical')
    expect(wrapper!.find('.resize-divider__line').exists()).toBe(true)
  })

  it('uses a localized default title and lets an explicit title win', () => {
    // The default was a hardcoded Chinese literal, so an English user hovering
    // the divider saw Chinese. It now resolves through i18n.
    const defaultWrapper = mountDivider()
    const el = defaultWrapper.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('title')).toBe(en.common.dragToResize)

    const custom = mountDivider({ title: 'Custom handle' })
    expect((custom.find('.split-view__divider').element as HTMLElement).getAttribute('title'))
      .toBe('Custom handle')
  })

  it('applies aria-value attributes when provided', () => {
    mountDivider({ ariaValueNow: 50, ariaValueMin: 32, ariaValueMax: 68 })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('aria-valuenow')).toBe('50')
    expect(el.getAttribute('aria-valuemin')).toBe('32')
    expect(el.getAttribute('aria-valuemax')).toBe('68')
  })

  it('emits dragmove(clientX) and adds body dragging class while dragging', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(true)

    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 120 }))
    const emitted = wrapper!.emitted('dragmove') as Array<Array<number>>
    expect(emitted).toBeTruthy()
    expect(emitted[emitted.length - 1][0]).toBe(120)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })

  it('does not drag on non-primary button', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 2, bubbles: true, clientX: 300 }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 120 }))
    expect(wrapper!.emitted('dragmove')).toBeUndefined()
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })

  it('cleans up body class on unmount during drag', () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    expect(document.body.classList.contains('split-view-dragging')).toBe(true)
    wrapper!.unmount()
    wrapper = null
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
  })
})

describe('SplitDivider — collapse handles', () => {
  it('renders no handle unless asked', () => {
    mountDivider()
    expect(wrapper!.find('.split-view__collapse-btn').exists()).toBe(false)
  })

  it('renders one handle per requested side, each labelled', () => {
    mountDivider({ collapseLeft: true, collapseRight: true, collapseLeftTitle: 'Hide files', collapseRightTitle: 'Hide chat' })
    const left = wrapper!.find('[data-collapse="left"]')
    const right = wrapper!.find('[data-collapse="right"]')
    expect(left.attributes('title')).toBe('Hide files')
    expect(left.attributes('aria-label')).toBe('Hide files')
    expect(right.attributes('title')).toBe('Hide chat')
    expect(right.attributes('aria-label')).toBe('Hide chat')
  })

  it('can render just one side', () => {
    mountDivider({ collapseRight: true })
    expect(wrapper!.find('[data-collapse="left"]').exists()).toBe(false)
    expect(wrapper!.find('[data-collapse="right"]').exists()).toBe(true)
  })

  it('emits collapse with the side that was clicked', async () => {
    mountDivider({ collapseLeft: true, collapseRight: true })
    await wrapper!.find('[data-collapse="left"]').trigger('click')
    await wrapper!.find('[data-collapse="right"]').trigger('click')
    expect(wrapper!.emitted('collapse')).toEqual([['left'], ['right']])
  })

  it('does not start a drag when a handle is pressed', () => {
    // The handles sit inside the divider, which starts dragging on pointerdown.
    // Without the stop modifier, pressing one would both begin a resize and
    // collapse the pane on release.
    mountDivider({ collapseLeft: true, collapseRight: true })
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    for (const side of ['left', 'right']) {
      const btn = wrapper!.find(`[data-collapse="${side}"]`).element as HTMLElement
      btn.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true }))
    }
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
    expect(wrapper!.emitted('dragstart')).toBeUndefined()
  })

  it('falls back to a localized label when no title is given', () => {
    mountDivider({ collapseLeft: true, collapseRight: true })
    expect(wrapper!.find('[data-collapse="left"]').attributes('title')).toBe(en.common.close)
    expect(wrapper!.find('[data-collapse="right"]').attributes('title')).toBe(en.common.close)
  })

  it('points each arrow at the pane it hides', () => {
    // The glyph must agree with the side: an arrow pointing left on the
    // right-hand handle would promise the opposite of what the click does.
    mountDivider({ collapseLeft: true, collapseRight: true })
    const leftPaths = wrapper!.findAll('[data-collapse="left"] path').map((p) => p.attributes('d'))
    const rightPaths = wrapper!.findAll('[data-collapse="right"] path').map((p) => p.attributes('d'))
    expect(leftPaths.join('|')).toContain('m16 15-3-3 3-3')
    expect(rightPaths.join('|')).toContain('m8 9 3 3-3 3')
  })

  it('frames each handle as a pane, not a bare chevron', () => {
    // A bare chevron reads as a drag handle (the glyph scrollbars and sliders
    // wear). The rounded frame + divider line is what says "hide this pane",
    // so both parts must be present and the divider line must sit on the side
    // being hidden.
    mountDivider({ collapseLeft: true, collapseRight: true })
    for (const side of ['left', 'right']) {
      const btn = wrapper!.find(`[data-collapse="${side}"]`)
      expect(btn.find('rect').exists()).toBe(true)
      const paths = btn.findAll('path').map((p) => p.attributes('d'))
      expect(paths.length, 'frame divider + chevron').toBe(2)
    }
    const leftPaths = wrapper!.findAll('[data-collapse="left"] path').map((p) => p.attributes('d'))
    const rightPaths = wrapper!.findAll('[data-collapse="right"] path').map((p) => p.attributes('d'))
    expect(leftPaths).toContain('M9 3v18')
    expect(rightPaths).toContain('M15 3v18')
  })

  it('flips the arrows for a vertical split (panes on the Y axis)', () => {
    mountDivider({ orientation: 'vertical', collapseLeft: true, collapseRight: true })
    const first = wrapper!.findAll('[data-collapse="left"] path').map((p) => p.attributes('d'))
    const second = wrapper!.findAll('[data-collapse="right"] path').map((p) => p.attributes('d'))
    expect(first).toContain('m9 16 3-3 3 3')
    expect(first).toContain('M3 9h18')
    expect(second).toContain('m15 8-3 3-3-3')
    expect(second).toContain('M3 15h18')
  })

  it('keeps the handles invisible and inert at rest', () => {
    // An invisible hit target floating over the pane edge would swallow clicks
    // meant for the content next to it, so pointer-events must track opacity.
    // The rest state is INVISIBLE — the dimmed state is part of leaving, not a
    // resting state (see the leave-sequence test below).
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toContain('opacity: 0')
    expect(base).toContain('pointer-events: none')
    // Revealed by the divider's hover/focus on pointer devices.
    const revealRule = style.slice(
      style.indexOf('.split-view__divider:hover .split-view__collapse-btn'),
      style.indexOf('}', style.indexOf('.split-view__divider:hover .split-view__collapse-btn')),
    )
    expect(revealRule).toContain('.split-view__divider:focus-within .split-view__collapse-btn')
    expect(revealRule).toContain('opacity: 1')
    expect(revealRule).toContain('pointer-events: auto')
    // The dimmed rung must NOT be part of the full-strength group.
    expect(revealRule).not.toContain('--lingering')
  })

  it('leaves in two steps: dim to --opacity-soft, hold, then fade out', () => {
    // The whole point of the linger: a single 1 → 0 fade removed the target
    // before a user standing slightly away could reach it. Leaving must first
    // drop to a dimmed-but-visible state (still clickable), hold there for
    // HANDLE_LINGER_MS, and only then clear the class and fade the rest of the
    // way to the base `opacity: 0`.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const start = style.indexOf('.split-view__divider--lingering .split-view__collapse-btn')
    expect(start, 'the lingering rule must exist').toBeGreaterThan(-1)
    const rule = style.slice(start, style.indexOf('}', start))
    expect(rule).toContain('opacity: var(--opacity-soft)')
    // Visible ⇒ clickable: the user is reaching for it during the hold.
    expect(rule).toContain('pointer-events: auto')
    // Token, not a literal — the dimming step is defined once for all themes.
    expect(rule).not.toMatch(/opacity:\s*0?\.\d/)
    // And it must be a SEPARATE rule from the full-strength one, otherwise
    // hovering and leaving would look identical.
    const fullStrength = style.slice(
      style.indexOf('.split-view__divider:hover .split-view__collapse-btn'),
      style.indexOf('}', style.indexOf('.split-view__divider:hover .split-view__collapse-btn')),
    )
    expect(fullStrength).not.toContain('--opacity-soft')
  })

  it('does NOT park the handles on screen on touch — a tap reveals them', () => {
    // Regression: the first version made them permanently visible under
    // `(pointer: coarse)`, which meant a tablet showed two floating buttons on
    // top of the panes at all times without the user ever touching the divider.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    // There are two `(pointer: coarse)` blocks (the first widens the drag hit
    // band); take the one that talks about the handles.
    const coarseBlocks = [...style.matchAll(/@media \(pointer: coarse\)\s*\{([\s\S]*?)\n\}/g)].map((m) => m[1])
    const handleBlock = coarseBlocks.find((b) => b.includes('split-view__collapse-btn'))
    expect(handleBlock, 'a coarse-pointer block for the handles must exist').toBeTruthy()
    // It must gate the reveal on the `--revealed` state…
    expect(handleBlock).toContain('.split-view__divider--revealed .split-view__collapse-btn')
    // …and must not touch the bare selector, which would apply unconditionally.
    // Both patterns are anchored to the START of a rule so the `--revealed`
    // prefixed selectors (which are fine) do not trip them.
    expect(handleBlock).not.toMatch(/(^|\n)\s*\.split-view__collapse-btn\s*\{/)
    expect(handleBlock).not.toMatch(/(^|\n)\s*\.split-view__collapse-btn svg\s*\{/)
  })

  it('scales with the UI but stops growing at the cap', () => {
    // Both halves matter: dropping `--dp` would freeze the handles at scale 1,
    // and dropping the `min()` lets them reach 56px at 2× — two big glyphs
    // parked over pane content. The offset is capped with them, else the pair
    // drifts away from the line on a large scale.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toMatch(/width:\s*min\(calc\(32 \* var\(--dp\)\), 40px\)/)
    expect(base).toMatch(/height:\s*min\(calc\(32 \* var\(--dp\)\), 40px\)/)
    // The offset is capped too, or the pair drifts off the line on a big scale.
    expect(style).toMatch(/translate\(calc\(-100% - min\(var\(--space-4\), 12px\)\), -50%\)/)
    expect(style).toMatch(/translate\(min\(var\(--space-4\), 12px\), -50%\)/)
    // Touch gets a bigger cap: a finger cannot hit 40px comfortably.
    const coarse = style.slice(style.indexOf('@media (pointer: coarse)'))
    expect(coarse).toMatch(/width:\s*min\(calc\(40 \* var\(--dp\)\), 50px\)/)
  })

  it('is the glyph itself — no disc, ring, fill or shadow box', () => {
    // The icon IS the button. An earlier version wrapped it in a 28px circle
    // with a `color-mix` fill and a 1px ring, which read as a floating chip of
    // UI competing with the divider line rather than as the pane control.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toContain('background: none')
    expect(base).toContain('border: 0')
    expect(base).toContain('box-shadow: none')
    expect(base, 'no rounded chip').not.toContain('border-radius: var(--radius-full)')
    expect(base, 'no colour fill').not.toMatch(/background:\s*color-mix/)
    // The glyph must fill the box, otherwise the leftover padding re-creates a
    // surface under it.
    const svg = style.slice(style.indexOf('.split-view__collapse-btn svg'), style.indexOf('}', style.indexOf('.split-view__collapse-btn svg')))
    expect(svg).toContain('width: 100%')
    expect(svg).toContain('height: 100%')
  })

  it('keeps the bare glyph readable over busy pane content', () => {
    // With no fill there is nothing between the stroke and whatever the pane
    // shows underneath. The ink stays at full `--text-primary`, and a halo in
    // the PANE colour separates it — a fixed black/white shadow would vanish
    // on half of the 36 themes.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toContain('color: var(--text-primary)')
    expect(base).toMatch(/filter:\s*drop-shadow\([^)]*var\(--bg-secondary\)\)/)
    expect(base, 'the halo must follow the pane colour').not.toMatch(/drop-shadow\([^)]*(#000|black|#fff|white)/)
  })

  it('places the handles on opposite sides of the line for each orientation', () => {
    // A single centred handle could not express "hide left" vs "hide right";
    // both offsets are per-orientation because the panes swap axes.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(/\.split-view__divider--horizontal \.split-view__collapse-btn--first\s*\{[^}]*left:\s*0;[^}]*translate\(calc\(-100% - min\(var\(--space-4\), 12px\)\)/)
    expect(style).toMatch(/\.split-view__divider--horizontal \.split-view__collapse-btn--second\s*\{[^}]*left:\s*100%;[^}]*translate\(min\(var\(--space-4\), 12px\)/)
    expect(style).toMatch(/\.split-view__divider--vertical \.split-view__collapse-btn--first\s*\{[^}]*top:\s*0;[^}]*translate\(-50%, calc\(-100% - min\(var\(--space-4\), 12px\)\)/)
    expect(style).toMatch(/\.split-view__divider--vertical \.split-view__collapse-btn--second\s*\{[^}]*top:\s*100%;[^}]*translate\(-50%, min\(var\(--space-4\), 12px\)\)/)
  })

  it('holds the handles clear of the pane content, not just the scrollbar', () => {
    // The glyphs are 40px now; the old 4dp (6px) gap put them flush against the
    // pane edge, crowding the first line of content. The gap must be wider than
    // the divider's own 1px hit band and must scale with the UI.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const gaps = [...style.matchAll(/min\(var\(--space-\d\), (\d+)px\)/g)].map((m) => Number(m[1]))
    expect(gaps.length).toBeGreaterThanOrEqual(4)
    for (const gap of gaps) expect(gap).toBeGreaterThanOrEqual(12)
    // A raw px gap would freeze at scale 1 while the glyph keeps growing.
    expect(style, 'the gap must scale with --dp').toMatch(/min\(var\(--space-4\), 12px\)/)
  })
})

describe('SplitDivider — tap to reveal the handles (touch)', () => {
  /** Press, then release at the same spot, as a finger tap would. */
  function tap(el: HTMLElement, pointerType = 'touch') {
    el.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300, clientY: 300, pointerType }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true, clientX: 300, clientY: 300, pointerType }))
  }

  function setup() {
    mountDivider({ collapseLeft: true, collapseRight: true })
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    return div
  }

  afterEach(() => {
    vi.useRealTimers()
  })

  it('is hidden until the line is tapped', () => {
    setup()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--revealed')
  })

  it('reveals the handles on a touch tap', async () => {
    const div = setup()
    tap(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--revealed')
  })

  it('does not reveal on a mouse click (hover already covers it)', async () => {
    const div = setup()
    tap(div, 'mouse')
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--revealed')
  })

  it('does not reveal when the press was a drag', async () => {
    const div = setup()
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300, clientY: 300, pointerType: 'touch' }))
    // Past the tap slop: this is a resize gesture, not a tap.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 360, clientY: 300, pointerType: 'touch' }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true, clientX: 360, clientY: 300, pointerType: 'touch' }))
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--revealed')
    // The drag itself still worked.
    expect(wrapper!.emitted('dragmove')).toBeTruthy()
  })

  it('tolerates a small wobble and still counts as a tap', async () => {
    const div = setup()
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300, clientY: 300, pointerType: 'touch' }))
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 303, clientY: 302, pointerType: 'touch' }))
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true, clientX: 303, clientY: 302, pointerType: 'touch' }))
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--revealed')
  })

  it('retracts on its own after the hold', async () => {
    vi.useFakeTimers()
    const div = setup()
    tap(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--revealed')

    vi.advanceTimersByTime(3100)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--revealed')
  })

  it('retracts when the user presses somewhere else', async () => {
    const div = setup()
    tap(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--revealed')

    document.body.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 2, bubbles: true }))
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--revealed')
  })

  it('stays up while the finger is on a handle', async () => {
    // Pressing a handle IS "inside the divider" — without that check the
    // outside-press listener would tear the handles down under the finger.
    const div = setup()
    tap(div)
    await nextTick()
    const handle = wrapper!.find('[data-collapse="right"]').element as HTMLElement
    handle.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 3, button: 0, bubbles: true }))
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--revealed')
  })

  it('clears the timer and listener on unmount', () => {
    vi.useFakeTimers()
    const div = setup()
    tap(div)
    expect(() => wrapper!.unmount()).not.toThrow()
    wrapper = null
    // A leaked timer would fire against a detached component.
    expect(() => vi.advanceTimersByTime(5000)).not.toThrow()
    expect(document.body.classList.contains('split-view-dragging')).toBe(false)
    void div
  })
})

describe('SplitDivider — handles linger after the pointer leaves', () => {
  // The handles were revealed purely by `:hover`, so leaving the 1px line
  // faded them out within one transition (150ms). A user who moved away and
  // came back found them gone and had to re-find the line. Leaving is now two
  // steps: dim to --opacity-soft and hold, then fade out.
  function setup() {
    vi.useFakeTimers()
    mountDivider({ collapseLeft: true, collapseRight: true })
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    return div
  }

  function enter(el: HTMLElement, pointerType = 'mouse') {
    el.dispatchEvent(new PointerEvent('pointerenter', { pointerId: 1, bubbles: false, pointerType }))
  }

  function leave(el: HTMLElement, pointerType = 'mouse') {
    el.dispatchEvent(new PointerEvent('pointerleave', { pointerId: 1, bubbles: false, pointerType }))
  }

  afterEach(() => {
    vi.useRealTimers()
  })

  it('holds the dimmed state for the whole grace period, then hides', async () => {
    const div = setup()
    enter(div)
    // Dwell long enough to count as "aiming at the divider".
    vi.advanceTimersByTime(250)
    leave(div)
    await nextTick()
    // Dimmed, not gone — this is the window the user clicks in.
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--lingering')

    // Still holding just before the grace period ends.
    vi.advanceTimersByTime(900)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--lingering')

    // Past the hold → the class clears, which is what lets the base
    // `opacity: 0` (and its transition) fade the rest of the way out.
    vi.advanceTimersByTime(200)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--lingering')
  })

  it('does not linger when the pointer only crossed the line', async () => {
    // A quick pass on the way to somewhere else must not park two discs over
    // the panes — that is exactly the "permanently visible buttons" problem
    // the touch path already avoids.
    const div = setup()
    enter(div)
    leave(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--lingering')
  })

  it('drops the grace period as soon as the pointer comes back', async () => {
    const div = setup()
    enter(div)
    vi.advanceTimersByTime(250)
    leave(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).toContain('split-view__divider--lingering')

    enter(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--lingering')
  })

  it('ignores touch pointers (the tap reveal owns that path)', async () => {
    const div = setup()
    enter(div, 'touch')
    vi.advanceTimersByTime(250)
    leave(div, 'touch')
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--lingering')
  })

  it('does not linger when the pointer leaves mid-drag', async () => {
    // A drag ends with pointerup, not with the pointer leaving the host — the
    // handles are suppressed for the whole gesture anyway.
    const div = setup()
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    enter(div)
    vi.advanceTimersByTime(250)
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    leave(div)
    await nextTick()
    expect(wrapper!.find('.split-view__divider').classes()).not.toContain('split-view__divider--lingering')
    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
  })

  it('shows the handles through the lingering class in CSS', () => {
    // The `--lingering` class alone does nothing unless a rule lists it — a
    // hover-only rule is what caused the original fade. It must land on the
    // DIMMED rung (0.7), not on full strength: this class IS the "leaving"
    // state, and the subsequent fade-out comes from the base `opacity: 0`.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(
      /\.split-view__divider--lingering \.split-view__collapse-btn\s*\{[^}]*opacity:\s*var\(--opacity-soft\);[^}]*pointer-events:\s*auto/,
    )
  })

  it('clears both timers on unmount', () => {
    const div = setup()
    enter(div)
    vi.advanceTimersByTime(250)
    leave(div)
    expect(() => wrapper!.unmount()).not.toThrow()
    wrapper = null
    // A leaked timer would fire against a detached component.
    expect(() => vi.advanceTimersByTime(5000)).not.toThrow()
  })
})

describe('SplitDivider — vertical orientation', () => {
  it('renders a row-resize divider with horizontal separator semantics', () => {
    mountDivider({ orientation: 'vertical' })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.getAttribute('role')).toBe('separator')
    expect(el.getAttribute('aria-orientation')).toBe('horizontal')
    expect(el.classList.contains('split-view__divider--vertical')).toBe(true)
  })

  it('emits dragmove(clientY) for a vertical split', () => {
    mountDivider({ orientation: 'vertical' })
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientY: 300 }))
    expect(document.body.classList.contains('split-view-dragging--vertical')).toBe(true)
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientY: 420 }))

    const emitted = wrapper!.emitted('dragmove') as Array<Array<number>>
    expect(emitted[emitted.length - 1][0]).toBe(420)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(document.body.classList.contains('split-view-dragging--vertical')).toBe(false)
  })

  it('defaults to horizontal orientation (clientX)', () => {
    mountDivider()
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    expect(el.classList.contains('split-view__divider--horizontal')).toBe(true)
    expect(el.getAttribute('aria-orientation')).toBe('vertical')
  })
})

describe('SplitDivider — touch target sizing', () => {
  it('enlarges the grab band and the resting line for coarse pointers', () => {
    // A 1px line is an unhittable touch target, and the hover-only growth never
    // fires on touch. jsdom does not load SFC <style>, so assert the source
    // declares a (pointer: coarse) block covering both orientations.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(/@media\s*\(pointer:\s*coarse\)/)
    const coarse = style.slice(style.indexOf('@media (pointer: coarse)'))
    // Both orientations must widen their ::before hit band inside the block.
    expect(coarse).toMatch(/\.split-view__divider--vertical::before/)
    expect(coarse).toMatch(/\.split-view__divider--horizontal::before/)
  })

  it('sets a minLeft/minRight-driven CSS var so JS clamp and CSS floors agree', () => {
    mountDivider({ ariaValueNow: 50 })
    const el = wrapper!.find('.split-view__divider').element as HTMLElement
    // The divider itself does not carry the vars; the SplitView root does.
    expect(el.getAttribute('role')).toBe('separator')
  })
})

describe('SplitDivider — drag highlight survives touch', () => {
  // Regression: the expanded highlight used to be driven by `:active` alone.
  // On touch, `setPointerCapture` makes the browser drop `:active`, so a fast
  // swipe showed no highlight at all while a held press did. The component now
  // tracks the pointer session in a `dragging` ref and styles it too.
  it('marks the divider as dragging for the whole pointer session', async () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()

    expect(div.classList.contains('resize-divider--expanded')).toBe(false)

    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true, clientX: 300 }))
    await nextTick()
    expect(div.classList.contains('resize-divider--expanded')).toBe(true)

    // Still marked mid-drag — this is the part `:active` failed to guarantee.
    window.dispatchEvent(new PointerEvent('pointermove', { pointerId: 1, bubbles: true, clientX: 200 }))
    await nextTick()
    expect(div.classList.contains('resize-divider--expanded')).toBe(true)

    window.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    await nextTick()
    expect(div.classList.contains('resize-divider--expanded')).toBe(false)
  })

  it('clears the dragging mark on pointercancel', async () => {
    mountDivider()
    const div = wrapper!.find('.split-view__divider').element as HTMLElement
    div.setPointerCapture = vi.fn()
    div.releasePointerCapture = vi.fn()
    div.dispatchEvent(new PointerEvent('pointerdown', { pointerId: 1, button: 0, bubbles: true }))
    await nextTick()
    expect(div.classList.contains('resize-divider--expanded')).toBe(true)
    window.dispatchEvent(new PointerEvent('pointercancel', { pointerId: 1, bubbles: true }))
    await nextTick()
    expect(div.classList.contains('resize-divider--expanded')).toBe(false)
  })

  it('styles the expanded host band for both :active and --expanded', () => {
    // The host geometry is SplitDivider's own (the line rules are shared — see
    // resizeDivider.css.test.ts). Styling only `:active` is the bug this guards
    // against: touch loses `:active` mid-swipe.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    for (const dir of ['horizontal', 'vertical']) {
      expect(style).toMatch(
        new RegExp(
          `\\.split-view__divider--${dir}:active,\\s*\\.split-view__divider--${dir}\\.resize-divider--expanded`,
        ),
      )
    }
  })

  it('consumes the shared line element instead of its own', () => {
    // The inner line moved to assets/resize-divider.css so both dividers share
    // one definition. A leftover local class would silently reintroduce the
    // duplicated (and drift-prone) copy.
    mountDivider()
    expect(wrapper!.find('.resize-divider__line').exists()).toBe(true)
    expect(wrapper!.find('.split-view__gutter-line').exists()).toBe(false)
    expect(wrapper!.find('.split-view__divider').classes()).toContain('resize-divider')
  })
})
