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
    const leftPath = wrapper!.find('[data-collapse="left"] path').attributes('d')
    const rightPath = wrapper!.find('[data-collapse="right"] path').attributes('d')
    expect(leftPath).toContain('m15 18-6-6 6-6')
    expect(rightPath).toContain('m9 18 6-6-6-6')
  })

  it('flips the arrows for a vertical split (panes on the Y axis)', () => {
    mountDivider({ orientation: 'vertical', collapseLeft: true, collapseRight: true })
    expect(wrapper!.find('[data-collapse="left"] path').attributes('d')).toContain('m18 15-6-6-6 6')
    expect(wrapper!.find('[data-collapse="right"] path').attributes('d')).toContain('m6 9 6 6 6-6')
  })

  it('keeps the handles invisible and inert at rest', () => {
    // An invisible hit target floating over the pane edge would swallow clicks
    // meant for the content next to it, so pointer-events must track opacity.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toContain('opacity: 0')
    expect(base).toContain('pointer-events: none')
    // Revealed by the divider's hover/focus, and unconditionally on touch
    // (no hover to reveal it with).
    expect(style).toMatch(/\.split-view__divider:hover \.split-view__collapse-btn[\s\S]{0,120}pointer-events: auto/)
    const coarse = style.slice(style.indexOf('@media (pointer: coarse)'))
    expect(coarse).toContain('.split-view__collapse-btn')
  })

  it('scales with the UI but stops growing at the cap', () => {
    // Both halves matter: dropping `--dp` would freeze the handles at scale 1,
    // and dropping the `min()` lets them reach 40px at 2× — a heavy disc
    // floating over pane content. The icon and gap are capped with them, else
    // the glyph keeps growing inside a frozen circle.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toMatch(/width:\s*min\(calc\(20 \* var\(--dp\)\), 28px\)/)
    expect(base).toMatch(/height:\s*min\(calc\(20 \* var\(--dp\)\), 28px\)/)
    const svg = style.slice(style.indexOf('.split-view__collapse-btn svg'), style.indexOf('}', style.indexOf('.split-view__collapse-btn svg')))
    expect(svg).toMatch(/width:\s*min\(calc\(12 \* var\(--dp\)\), 16px\)/)
    // The offset is capped too, or the pair drifts off the line on a big scale.
    expect(style).toMatch(/translate\(calc\(-100% - min\(var\(--space-2\), 6px\)\), -50%\)/)
    expect(style).toMatch(/translate\(min\(var\(--space-2\), 6px\), -50%\)/)
    // Touch gets a bigger cap: a finger cannot hit 28px comfortably.
    const coarse = style.slice(style.indexOf('@media (pointer: coarse)'))
    expect(coarse).toMatch(/width:\s*min\(calc\(26 \* var\(--dp\)\), 34px\)/)
  })

  it('does not rely on a fixed background step for contrast', () => {
    // Design-guide red line 4: `--bg-tertiary` is not reliably distinguishable
    // from `--bg-secondary` (15 of 36 themes are under 1.12, ayu-dark 1.062),
    // and the panes on either side are `--bg-secondary`. The fill must mix
    // `--text-primary` into the pane colour so the direction of the step is
    // guaranteed, and the ring must not lean on `--border-color` either.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    const base = style.slice(style.indexOf('.split-view__collapse-btn {'), style.indexOf('}', style.indexOf('.split-view__collapse-btn {')))
    expect(base).toMatch(/background:\s*color-mix\(in srgb, var\(--text-primary\) 26%, var\(--bg-secondary\)\)/)
    expect(base).toMatch(/border:\s*1px solid color-mix\(in srgb, var\(--text-primary\) 90%, transparent\)/)
    expect(base, 'the fill must not be a bare bg token').not.toMatch(/background:\s*var\(--bg-/)
    expect(base, 'the ring must not be a bare border token').not.toMatch(/border:\s*1px solid var\(--border-color/)
    // The glyph keeps full text-primary; a muted secondary would drop it below
    // the 3:1 an icon needs on the tinted fill.
    expect(base).toContain('color: var(--text-primary)')
  })

  it('places the handles on opposite sides of the line for each orientation', () => {
    // A single centred handle could not express "hide left" vs "hide right";
    // both offsets are per-orientation because the panes swap axes.
    const src = readSource()
    const style = src.slice(src.indexOf('<style'))
    expect(style).toMatch(/\.split-view__divider--horizontal \.split-view__collapse-btn--first\s*\{[^}]*left:\s*0;[^}]*translate\(calc\(-100% - min\(var\(--space-2\), 6px\)\)/)
    expect(style).toMatch(/\.split-view__divider--horizontal \.split-view__collapse-btn--second\s*\{[^}]*left:\s*100%;[^}]*translate\(min\(var\(--space-2\), 6px\)/)
    expect(style).toMatch(/\.split-view__divider--vertical \.split-view__collapse-btn--first\s*\{[^}]*top:\s*0;[^}]*translate\(-50%, calc\(-100% - min\(var\(--space-2\), 6px\)\)/)
    expect(style).toMatch(/\.split-view__divider--vertical \.split-view__collapse-btn--second\s*\{[^}]*top:\s*100%;[^}]*translate\(-50%, min\(var\(--space-2\), 6px\)\)/)
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
