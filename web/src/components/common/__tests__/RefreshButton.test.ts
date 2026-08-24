/**
 * RefreshButton rotation tests.
 *
 * Rotation is driven by the Web Animations API (svg.animate). When `loading`
 * flips to false the animation is cancelled right away — no whole-revolution
 * wait — and the icon swaps to the green check confirmation (check-in bounce,
 * 0.4s). The bounce always plays to completion: the swap-back is driven by the
 * check-in animation's `animationend`, with a fallback timer for environments
 * where CSS animations never run (jsdom).
 */
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import RefreshButton from '@/components/common/RefreshButton.vue'

// ── WAAPI stub ──
// jsdom has no Web Animations API. Fake Element.prototype.animate so the
// component can start/read/cancel animations. cancel() mimics real WAAPI:
// it nulls currentTime and records that the animation is no longer running.
interface MockAnimation {
  currentTime: number | null
  cancel: ReturnType<typeof vi.fn>
  duration: number
}

const animateMock = vi.fn()
const animations: MockAnimation[] = []
// The svg element each animation was started on (the `this` of svg.animate).
const animatedEls: (Element | null)[] = []

function installAnimateMock() {
  animateMock.mockReset()
  animations.length = 0
  animatedEls.length = 0
  ;(Element.prototype as any).animate = animateMock.mockImplementation(function (this: Element, _keyframes: any, opts: any) {
    const anim: MockAnimation = {
      currentTime: 0,
      cancel: vi.fn(() => { anim.currentTime = null }),
      duration: opts?.duration ?? 0,
    }
    animations.push(anim)
    animatedEls.push(this)
    return anim
  })
}

function latestAnim(): MockAnimation | undefined {
  return animations[animations.length - 1]
}

describe('RefreshButton', () => {
  beforeEach(() => {
    installAnimateMock()
  })

  afterEach(() => {
    vi.useRealTimers()
    delete (Element.prototype as any).animate
  })

  function mountBtn(props = {}) {
    return mount(RefreshButton, {
      props: { icon: 'RefreshCw', ...props },
    })
  }

  it('starts an infinite 0.5s-per-revolution animation while loading', async () => {
    const wrapper = mountBtn()
    expect(animateMock).not.toHaveBeenCalled()

    await wrapper.setProps({ loading: true })
    await nextTick()

    expect(animateMock).toHaveBeenCalledTimes(1)
    const [keyframes, opts] = animateMock.mock.calls[0] as any[]
    expect(keyframes).toHaveLength(2)
    expect(keyframes[0].transform).toBe('rotate(0deg)')
    expect(keyframes[1].transform).toBe('rotate(360deg)')
    expect(opts.duration).toBe(500)
    expect(opts.iterations).toBe(Infinity)
    expect(opts.easing).toBe('linear')
  })

  it('cancels the spin immediately when loading stops (no whole-revolution wait)', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()

    const anim = latestAnim()
    expect(anim).toBeTruthy()

    // Load stops after 200ms of a 500ms turn (40% — mid-rotation). The spin is
    // cancelled right away; nothing is scheduled to complete the remaining turn.
    // (currentTime is set for realism only — the component no longer reads it.)
    anim!.currentTime = 200
    await wrapper.setProps({ loading: false })
    await nextTick()

    expect(anim!.cancel).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1000)
    expect(anim!.cancel).toHaveBeenCalledTimes(1)
  })

  it('re-starts the spin cleanly across a rapid re-toggle', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(animateMock).toHaveBeenCalledTimes(1)

    // loading false → true again quickly
    await wrapper.setProps({ loading: false })
    await nextTick()
    await wrapper.setProps({ loading: true })
    await nextTick()
    await nextTick() // resolve the loading-watch's internal DOM await

    // The stopped animation was cancelled; the restart starts a fresh spin
    expect(animateMock).toHaveBeenCalledTimes(2)
    expect(latestAnim()!.cancel).not.toHaveBeenCalled()

    // Final stop cancels the running spin exactly once
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(latestAnim()!.cancel).toHaveBeenCalledTimes(1)
  })

  it('shows a green circled check confirmation as soon as the spin stops', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')
    const originalIcon = () => wrapper.find('svg:not([data-confirm])')

    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
    expect(originalIcon().exists()).toBe(true)

    // Load stops mid-turn → spin cancelled → check shows immediately
    latestAnim()!.currentTime = 200 // realism only; currentTime is no longer read
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // Confirm icon is the green circled check (CheckCircle2 → a circle with a
    // check path) with the bounce-in animation (forwards fill keeps the pose)
    const iconSvg = confirmIcon().element as SVGElement
    expect(iconSvg.style.color).toBe('var(--color-green, #16a34a)')
    expect(iconSvg.style.animation).toContain('check-in')
    expect(iconSvg.style.animation).toContain('forwards')
    // CircleCheck renders a <circle> plus the check <path>, unlike the bare
    // Check icon which has no circle
    expect(iconSvg.querySelector('circle')).not.toBeNull()

    // The bounce's animationend swaps the icon back to the original refresh icon.
    // (dispatchEvent is used directly — vue-test-utils trigger() builds a plain
    // Event whose animationName is always undefined, which the handler ignores.)
    const animEnd = new Event('animationend')
    ;(animEnd as any).animationName = 'check-in'
    confirmIcon().element.dispatchEvent(animEnd)
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
    expect(originalIcon().exists()).toBe(true)
  })

  it('reverts the check via the fallback timer when animationend never fires', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')
    const originalIcon = () => wrapper.find('svg:not([data-confirm])')

    await wrapper.setProps({ loading: true })
    await nextTick()
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // jsdom never runs CSS animations → no animationend → the fallback timer
    // clears the Check after CONFIRM_MS so the button never stays green forever
    vi.advanceTimersByTime(399)
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)
    vi.advanceTimersByTime(1)
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
    expect(originalIcon().exists()).toBe(true)
  })

  it('skips the Check confirmation when a new refresh starts immediately', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')

    await wrapper.setProps({ loading: true })
    await nextTick()
    latestAnim()!.currentTime = 250 // realism only; currentTime is no longer read
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // Restart resets the confirm state; the pending fallback timer (if any)
    // later fires as a no-op and must not re-show the Check
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)

    vi.advanceTimersByTime(1000)
    await nextTick()
    expect(confirmIcon().exists()).toBe(false)
  })

  it('starts spinning when mounted with loading=true (in-flight refresh)', async () => {
    const wrapper = mountBtn({ loading: true })
    await nextTick()
    await nextTick() // watch's DOM-update await resolves after mount

    expect(animateMock).toHaveBeenCalledTimes(1)
  })

  it('animates the visible icon when restarting during the confirm window', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    const confirmIcon = () => wrapper.find('svg[data-confirm="true"]')
    const originalIcon = () => wrapper.find('svg:not([data-confirm])')

    // First refresh completes → confirm Check shows
    await wrapper.setProps({ loading: true })
    await nextTick()
    latestAnim()!.currentTime = 200
    await wrapper.setProps({ loading: false })
    await nextTick()
    expect(confirmIcon().exists()).toBe(true)

    // Restart during the confirm window: the new spin must run on the icon the
    // user sees (the refresh icon), not on the soon-to-be-replaced Check.
    const animsBefore = animateMock.mock.calls.length
    await wrapper.setProps({ loading: true })
    await nextTick()
    await nextTick()
    expect(confirmIcon().exists()).toBe(false) // check swapped back to refresh icon
    expect(animateMock.mock.calls.length).toBe(animsBefore + 1) // new spin started
    // The animated element is the currently-rendered (non-confirm) svg
    const animatedSvg = animatedEls[animsBefore]
    const renderedSvg = originalIcon().element
    expect(animatedSvg).toBe(renderedSvg)
  })

  it('emits click and stays disabled while loading', async () => {
    const wrapper = mountBtn()
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)

    await wrapper.setProps({ loading: true })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1) // blocked while loading
    expect((wrapper.find('button').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('re-targets the spin animation when the icon prop swaps mid-load', async () => {
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    expect(animateMock).toHaveBeenCalledTimes(1)
    // The animation is running on the original RefreshCw svg.
    const firstAnimSvg = animatedEls[0]
    expect(firstAnimSvg).toBe(wrapper.find('svg').element)

    // Swap the icon while still loading: the old animation targets a detached
    // node, so a fresh spin must start on the newly-rendered RotateCcw svg.
    await wrapper.setProps({ icon: 'RotateCcw' })
    await nextTick()
    await nextTick()

    expect(animateMock).toHaveBeenCalledTimes(2)
    // The new animation is bound to the currently-rendered (RotateCcw) svg.
    const secondAnimSvg = animatedEls[1]
    const renderedSvg = wrapper.find('svg').element
    expect(secondAnimSvg).toBe(renderedSvg)
    // The superseded animation was cancelled.
    expect(animations[0].cancel).toHaveBeenCalledTimes(1)
  })

  it('cancels the spin on unmount and leaves no pending timers behind', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    const anim = latestAnim()!

    // Stop the spin, which schedules the confirm fallback timer
    await wrapper.setProps({ loading: false })
    await nextTick()

    wrapper.unmount()
    expect(anim.cancel).toHaveBeenCalledTimes(1)
    // The confirm fallback timer was cleared on unmount — no timers remain
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps spinning uninterrupted on a same-tick loading true→false→true toggle', async () => {
    vi.useFakeTimers()
    const wrapper = mountBtn()
    await wrapper.setProps({ loading: true })
    await nextTick()
    const anim = latestAnim()!
    expect(animateMock).toHaveBeenCalledTimes(1)

    // Synchronous toggle within one tick: both prop writes queue before any
    // flush, so the watch runs once with the latest value (true) — stopSpin
    // never fires and the same animation keeps running, no Check flash, no
    // restart.
    wrapper.setProps({ loading: false })
    wrapper.setProps({ loading: true })
    await nextTick()
    await nextTick()
    expect(animateMock).toHaveBeenCalledTimes(1)
    expect(anim.cancel).not.toHaveBeenCalled()
  })
})
