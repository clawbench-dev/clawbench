import { describe, it, expect, vi, beforeEach } from 'vitest'
import { RunningSweepDirective } from '../runningSweep'

/**
 * The sweep's correctness is almost entirely "what did we ask the browser for",
 * because the phase alignment comes from ONE property: the animation's
 * `startTime`. Pinning it to the timeline origin is what makes every running
 * session's comet sweep in step (see the directive header), so these tests
 * assert the options precisely rather than smoke-testing that *an* animation
 * exists.
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

  it('asks for one infinite eased pass matching the comet geometry', () => {
    const { el, animate } = makeElement()
    RunningSweepDirective.mounted(el)

    const [keyframes, options] = animate.mock.calls[0]
    // Travel of one comet as a % of its own width. The design specifies the
    // endpoints against the TRACK (-40% to 102%); the comet is 38% of the track
    // wide, so those become -105.26% and 268.42% of the comet. Both ends are
    // fully clear of the track, which makes the wrap invisible: the next pass
    // starts the instant the previous one leaves, with no overlap and no gap.
    // These numbers are tied to the 38% width in SessionList.vue.
    expect(keyframes).toEqual([
      { transform: 'translateX(-105.26%)' },
      { transform: 'translateX(268.42%)' },
    ])
    expect(options).toMatchObject({
      duration: 1500,
      iterations: Infinity,
      // The gentle S-curve the design uses. Equal slope at both ends (~0.111),
      // so the wrap does not produce a velocity jump.
      easing: 'cubic-bezier(.45,.05,.55,.95)',
      // fill:both applies the first keyframe while pending, so the comet does
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
