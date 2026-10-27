import { describe, expect, it, vi, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import {
  applyThemeWithReveal,
  originFromElement,
  prefersReducedMotion,
  revealClipPercent,
  _resetThemeRevealForTest,
} from '@/utils/themeReveal'

type FakeTransition = {
  ready: Promise<void>
  finished: Promise<void>
  callback: () => void | Promise<void>
}

/** Install a `document.startViewTransition` stub. Returns the recorded calls. */
function stubViewTransition(): FakeTransition[] {
  const calls: FakeTransition[] = []
  ;(document as unknown as Record<string, unknown>).startViewTransition = (cb: () => void | Promise<void>) => {
    const entry: FakeTransition = { ready: Promise.resolve(), finished: Promise.resolve(), callback: cb }
    calls.push(entry)
    return entry
  }
  return calls
}

function stubReducedMotion(matches: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

const ORIGIN = { x: 100, y: 50 }

afterEach(() => {
  _resetThemeRevealForTest()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  delete (document as unknown as Record<string, unknown>).startViewTransition
})

describe('revealClipPercent', () => {
  it('expresses the centre as a viewport percentage', () => {
    const c = revealClipPercent(100, 50, 800, 600)
    expect(c.cx).toBeCloseTo((100 / 800) * 100)
    expect(c.cy).toBeCloseTo((50 / 600) * 100)
  })

  it('sizes the radius so the circle reaches the farthest corner', () => {
    // circle() resolves a percentage radius against hypot(w, h) / sqrt(2).
    const c = revealClipPercent(100, 50, 800, 600)
    const reference = Math.hypot(800, 600) / Math.SQRT2
    expect(c.radius).toBeCloseTo((Math.hypot(700, 550) / reference) * 100)
  })

  it('produces the same fractions regardless of a uniform scale', () => {
    // A viewport scaled 2x must yield identical percentages: this is the whole
    // point — the pseudo-element's clip box may differ from window.innerWidth.
    const a = revealClipPercent(100, 50, 800, 600)
    const b = revealClipPercent(200, 100, 1600, 1200)
    expect(b.cx).toBeCloseTo(a.cx)
    expect(b.cy).toBeCloseTo(a.cy)
    expect(b.radius).toBeCloseTo(a.radius)
  })

  it('handles an origin past the bottom-right edge', () => {
    const c = revealClipPercent(900, 700, 800, 600)
    const reference = Math.hypot(800, 600) / Math.SQRT2
    expect(c.radius).toBeCloseTo((Math.hypot(900, 700) / reference) * 100)
  })

  it('does not divide by zero on a degenerate viewport', () => {
    const c = revealClipPercent(0, 0, 0, 0)
    expect(Number.isFinite(c.cx)).toBe(true)
    expect(Number.isFinite(c.cy)).toBe(true)
    expect(Number.isFinite(c.radius)).toBe(true)
  })
})

describe('originFromElement', () => {
  it('returns the element centre', () => {
    const el = document.createElement('button')
    el.getBoundingClientRect = () => ({ left: 10, top: 20, width: 30, height: 40 }) as DOMRect
    expect(originFromElement(el)).toEqual({ x: 25, y: 40 })
  })

  it('returns null for a zero-sized (hidden/unmounted) element', () => {
    const el = document.createElement('button')
    el.getBoundingClientRect = () => ({ left: 0, top: 0, width: 0, height: 0 }) as DOMRect
    expect(originFromElement(el)).toBeNull()
  })

  it('returns null for null', () => {
    expect(originFromElement(null)).toBeNull()
  })
})

describe('prefersReducedMotion', () => {
  it('is true when the media query matches', () => {
    stubReducedMotion(true)
    expect(prefersReducedMotion()).toBe(true)
  })

  it('is false when it does not match', () => {
    stubReducedMotion(false)
    expect(prefersReducedMotion()).toBe(false)
  })
})

describe('applyThemeWithReveal', () => {
  it('applies instantly when the View Transitions API is unavailable', () => {
    stubReducedMotion(false)
    const apply = vi.fn()
    const result = applyThemeWithReveal(apply, { origin: ORIGIN })
    expect(result).toBeNull()
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('applies instantly under reduced motion even when the API exists', () => {
    stubReducedMotion(true)
    stubViewTransition()
    const apply = vi.fn()
    expect(applyThemeWithReveal(apply, { origin: ORIGIN })).toBeNull()
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('applies instantly when there is no origin', () => {
    stubReducedMotion(false)
    const start = stubViewTransition()
    const apply = vi.fn()
    expect(applyThemeWithReveal(apply, { origin: null })).toBeNull()
    expect(start).toHaveLength(0)
    expect(apply).toHaveBeenCalledTimes(1)
  })

  it('runs the change inside the transition and animates a circle from the origin', async () => {
    stubReducedMotion(false)
    const start = stubViewTransition()
    const animate = vi.fn()
    ;(Element.prototype as unknown as Record<string, unknown>).animate = animate
    // jsdom has no layout; pin the viewport the radius is computed against.
    vi.stubGlobal('innerWidth', 800)
    vi.stubGlobal('innerHeight', 600)

    const apply = vi.fn()
    const transition = applyThemeWithReveal(apply, { origin: ORIGIN, durationMs: 123 })

    // The DOM mutation must happen inside the transition callback, not before.
    expect(apply).not.toHaveBeenCalled()
    expect(start).toHaveLength(1)
    expect(document.documentElement.classList.contains('theme-reveal-active')).toBe(true)

    start[0].callback()
    expect(apply).toHaveBeenCalledTimes(1)

    await transition!.ready
    await Promise.resolve()

    expect(animate).toHaveBeenCalledTimes(1)
    const [keyframes, opts] = animate.mock.calls[0]
    const clip = revealClipPercent(ORIGIN.x, ORIGIN.y, 800, 600)
    expect(keyframes.clipPath).toEqual([
      `circle(0% at ${clip.cx}% ${clip.cy}%)`,
      `circle(${clip.radius}% at ${clip.cx}% ${clip.cy}%)`,
    ])
    expect(opts.pseudoElement).toBe('::view-transition-new(root)')
    expect(opts.duration).toBe(123)
    expect(opts.easing).toBe('ease-in-out')

    // Cleanup once the transition settles.
    await transition!.finished
    await Promise.resolve()
    expect(document.documentElement.classList.contains('theme-reveal-active')).toBe(false)
  })

  it('suppresses the instant switch while a reveal is still running', async () => {
    stubReducedMotion(false)
    const start = stubViewTransition()
    ;(Element.prototype as unknown as Record<string, unknown>).animate = vi.fn()

    const first = applyThemeWithReveal(vi.fn(), { origin: ORIGIN })
    expect(first).not.toBeNull()
    expect(start).toHaveLength(1)

    // Second switch mid-flight must not be swallowed by the browser's
    // "transition already active" skip — apply it immediately.
    const secondApply = vi.fn()
    expect(applyThemeWithReveal(secondApply, { origin: ORIGIN })).toBeNull()
    expect(secondApply).toHaveBeenCalledTimes(1)
    expect(start).toHaveLength(1)

    await first!.finished
    await Promise.resolve()
  })

  it('passes the callback return value through so async changes are awaited', () => {
    stubReducedMotion(false)
    const start = stubViewTransition()
    ;(Element.prototype as unknown as Record<string, unknown>).animate = vi.fn()

    let resolveChange: () => void = () => {}
    const apply = vi.fn(() => new Promise<void>(r => { resolveChange = r }))
    applyThemeWithReveal(apply, { origin: ORIGIN })

    // The browser only waits for the "new" snapshot if the callback returns a
    // thenable; Vue's nextTick relies on this.
    const returned = start[0].callback()
    expect(returned).toBeInstanceOf(Promise)
    resolveChange()
    return returned
  })

  it('falls back to an instant switch when startViewTransition throws', () => {
    stubReducedMotion(false)
    ;(document as unknown as Record<string, unknown>).startViewTransition = () => {
      throw new Error('not allowed')
    }
    const apply = vi.fn()
    expect(applyThemeWithReveal(apply, { origin: ORIGIN })).toBeNull()
    expect(apply).toHaveBeenCalledTimes(1)
    expect(document.documentElement.classList.contains('theme-reveal-active')).toBe(false)
  })
})

/**
 * Source guards for the quick theme picker.
 *
 * The picker's behaviour (deferred reveal, persistence, wipe origin) is shared
 * by its hosts through `composables/useThemePicker.ts`, because it carries three
 * rules that each fail silently:
 *
 *   1. The reveal must NOT start in `selectTheme` — it starts from the popup's
 *      `after-leave`, or the still-fading menu is captured into the transition's
 *      "new" snapshot and the wipe visibly overlaps the close.
 *   2. The change must go through `setLocalConfig('theme', …)`; writing
 *      localStorage directly leaves the document on the old palette.
 *   3. The wipe must start from the button (falling back to the app header's
 *      top-right corner) so it grows out of what the user pressed.
 *
 * None of this is observable in jsdom (no View Transitions), so it is pinned at
 * the source level — and every host must actually USE the shared module, or it
 * will drift back to a hand-rolled copy.
 */
describe('quick theme picker wiring', () => {
  const picker = readFileSync(
    resolve(__dirname, '../../composables/useThemePicker.ts'),
    'utf8',
  )

  it('closes the menu in selectTheme and defers the reveal to after-leave', () => {
    const body = picker.match(/function selectTheme\(value: string\) \{([\s\S]*?)\n  \}/)
    expect(body, 'selectTheme not found').not.toBeNull()
    // selectTheme only closes the menu; it must NOT start the reveal itself.
    expect(body![1]).toContain('close()')
    expect(body![1]).not.toContain('applyThemeWithReveal')
  })

  it('runs the reveal from the after-leave handler using the shared origin helper', () => {
    const body = picker.match(/function onMenuAfterLeave\(\) \{([\s\S]*?)\n  \}/)
    expect(body, 'onMenuAfterLeave not found').not.toBeNull()
    expect(body![1]).toContain('applyThemeWithReveal')
    expect(body![1]).toContain("setLocalConfig('theme', value)")
    // The wipe starts from the trigger; headerThemeOrigin() is the fallback for
    // a host whose button has no box. The settings grid uses the same helper.
    expect(body![1]).toContain('headerThemeOrigin()')
    expect(body![1]).toContain('originFromElement(btnRef.value)')
  })

  it('every host renders the shared picker and wires after-leave to it', () => {
    for (const rel of [
      '../../components/common/AppHeader.vue',
      '../../share/ShareThemeButton.vue',
    ]) {
      const host = readFileSync(resolve(__dirname, rel), 'utf8')
      expect(host, `${rel} must use useThemePicker`).toContain("from '@/composables/useThemePicker'")
      expect(host, `${rel} must defer the reveal to after-leave`).toMatch(
        /<PopupMenu[\s\S]*?@after-leave="on(Theme)?MenuAfterLeave"/,
      )
      // A hand-rolled picker would call setLocalConfig directly on click.
      expect(host, `${rel} must not apply the theme itself`).not.toContain('applyThemeWithReveal')
    }
  })

  it('shares headerThemeOrigin() with the settings theme grid', () => {
    // The settings grid is the other entry point; both must resolve the origin
    // through the same helper or the two reveals would start from different
    // spots.
    const settingsItem = readFileSync(
      resolve(__dirname, '../../components/settings/SettingsItem.vue'),
      'utf8',
    )
    expect(picker).toContain('headerThemeOrigin()')
    expect(settingsItem).toContain('headerThemeOrigin()')
  })
})
