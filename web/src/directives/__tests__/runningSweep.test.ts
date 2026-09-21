import { describe, it, expect, vi, beforeEach } from 'vitest'
import { RunningSweepDirective } from '../runningSweep'

/**
 * The sweep's correctness is almost entirely "what did we ask the browser for",
 * because the phase alignment comes from ONE property: the animation's
 * `startTime`. Pinning it to the timeline origin is what makes every running
 * session sweep in step (see the directive header), so these tests assert the
 * options precisely rather than smoke-testing that *an* animation exists.
 *
 * jsdom has no Web Animations API, so `Element.animate` is stubbed. That is not
 * a workaround — the directive's own contract is to no-op without it, which is
 * tested separately.
 */

interface FakeAnim {
  startTime: number | null
  cancel: ReturnType<typeof vi.fn>
}

function makeElement() {
  const el = document.createElement('i') as HTMLElement & { animate?: unknown; _sweepAnim?: unknown }
  const anim: FakeAnim = { startTime: null, cancel: vi.fn() }
  const animate = vi.fn(() => anim)
  el.animate = animate
  return { el, anim, animate }
}

describe('RunningSweepDirective', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('pins the animation to the shared timeline origin', () => {
    // The whole feature rests on this: every band gets the SAME startTime, so
    // their phase is a pure function of the shared document clock. If this
    // regressed to "let the browser choose", rows would drift apart again —
    // exactly the bug this directive exists to fix.
    const { el, anim, animate } = makeElement()
    RunningSweepDirective.mounted(el)

    expect(animate).toHaveBeenCalledTimes(1)
    expect(anim.startTime, 'startTime must be pinned to the timeline origin').toBe(0)
  })

  it('asks for one infinite linear pass matching the band geometry', () => {
    const { el, animate } = makeElement()
    RunningSweepDirective.mounted(el)

    const [keyframes, options] = animate.mock.calls[0]
    // Travel of one band as a % of its own width: -100% parks it off the left
    // edge, 125% (= 100/0.8) carries it off the right. These are tied to the
    // band's 80% width — see the SWEEP_FROM/SWEEP_TO comment.
    expect(keyframes).toEqual([
      { transform: 'translateX(-100%)' },
      { transform: 'translateX(125%)' },
    ])
    expect(options).toMatchObject({
      duration: 2000,
      iterations: Infinity,
      easing: 'linear',
      // fill:both applies the first keyframe while pending, so the band does
      // not flash at translateX(0) for a frame before the clock takes over.
      fill: 'both',
    })
  })

  it('cancels the animation on unmount', () => {
    const { el, anim } = makeElement()
    RunningSweepDirective.mounted(el)
    RunningSweepDirective.unmounted(el)

    expect(anim.cancel).toHaveBeenCalledTimes(1)
    // The handle must be dropped too, or a later mount would cancel a stale
    // animation and the new one would leak.
    expect((el as unknown as { _sweepAnim?: unknown })._sweepAnim).toBeUndefined()
  })

  it('no-ops without Element.animate instead of throwing', () => {
    // jsdom in unit tests, and any engine predating the Web Animations API.
    // The sweep is decoration — a missing API must not break rendering a list.
    const el = document.createElement('i')
    expect(() => RunningSweepDirective.mounted(el)).not.toThrow()
    expect(() => RunningSweepDirective.unmounted(el)).not.toThrow()
  })
})
